package main

import (
	"image"
	"image/jpeg"
	_ "image/png" // 允许解码 png 原图,输出统一 jpeg
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// ===== 缩略图 =====
//
// 【为什么要有】
// 待处理页是 20 个 110×110 的格子,每个格子引的却是 900×1600 的原图 ——
// 实测一页 1.1 MB。按 2 倍屏算需要的是 220×220,下的像素是需要的 8 倍。
// 巡检员在机房弱网下翻这一页,等的就是这些白下的字节。
//
// 【为什么自己写而不引库】
// 缩略图这一档,盒式平均(把源图一块区域求平均色)已经足够 —— 大比例缩小
// 时它的效果和高级滤波差不多,而且不用引 golang.org/x/image。
// 少一个依赖,换机器构建就少一件会出错的事。
//
// 【为什么落盘缓存】
// 一次页面加载 20 张图,每次都现解 900×1600 的 JPEG 是纯浪费。第一次请求
// 生成后写到原图旁边的 .thumb/ 里,之后直接发文件。照片一旦上传就不会变,
// 所以缓存不需要失效逻辑 —— 只在原图更新时(比对 modtime)重生成。

// 只接受这几档宽度。不限制的话,任何人都能用 ?w=1..2000 让服务器生成
// 两千个变体把磁盘塞满 —— 这是个便宜但真实的放大攻击。
var allowedThumbWidths = map[int]bool{120: true, 240: true, 480: true}

// 源图超过这个尺寸就不做缩略(直接发原图)。防的是有人上传一张
// 30000×30000 的图,解码时按 4 字节/像素算要 3.6 GB 内存。
const maxThumbSourcePixels = 40 * 1000 * 1000

var thumbLocks sync.Map // 原图路径 -> *sync.Mutex,避免同一张图被并发重复生成

// thumbWidthFromRequest 读 ?w=,不合法或不在白名单里返回 0 表示"发原图"。
func thumbWidthFromRequest(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("w"))
	if raw == "" {
		return 0
	}
	w, err := strconv.Atoi(raw)
	if err != nil || !allowedThumbWidths[w] {
		return 0
	}
	return w
}

// serveImage 统一的图片出口:缓存头 + 可选缩略图。
// 两个图片入口(/storage/... 和 离线照片接口)都走这里,免得各写一套。
func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, origPath string) {
	// 巡检照片属于某个租户、某条记录,是受权限控制的内容 ——
	// 必须 private,绝不能让中间代理或 CDN 缓存后发给别人。
	// 照片一旦上传就不会再变,所以可以放心给长有效期。
	w.Header().Set("Cache-Control", "private, max-age=604800")
	if st, err := os.Stat(origPath); err == nil {
		// ServeFile 只给 Last-Modified;补一个 ETag,让重访走 304 而不是重下。
		w.Header().Set("ETag", strconv.FormatInt(st.ModTime().UnixNano(), 36)+"-"+strconv.FormatInt(st.Size(), 36))
	}

	if width := thumbWidthFromRequest(r); width > 0 {
		if thumb, err := ensureThumb(origPath, width); err == nil {
			http.ServeFile(w, r, thumb)
			return
		}
		// 生成失败(格式不支持、图太大、磁盘满)就发原图。
		// 宁可多花流量,也不能让照片打不开 —— 照片是巡检的证据。
	}
	http.ServeFile(w, r, origPath)
}

func thumbPathFor(orig string, width int) string {
	dir := filepath.Join(filepath.Dir(orig), ".thumb")
	base := filepath.Base(orig)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return filepath.Join(dir, base+".w"+strconv.Itoa(width)+".jpg")
}

// ensureThumb 返回可直接发送的缩略图路径,必要时先生成。
func ensureThumb(orig string, width int) (string, error) {
	dst := thumbPathFor(orig, width)

	srcInfo, err := os.Stat(orig)
	if err != nil {
		return "", err
	}
	// 已有且不比原图旧就直接用
	if di, err := os.Stat(dst); err == nil && !di.ModTime().Before(srcInfo.ModTime()) {
		return dst, nil
	}

	muAny, _ := thumbLocks.LoadOrStore(orig, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	// 拿到锁后再看一次:等锁期间可能已经被别的请求生成好了
	if di, err := os.Stat(dst); err == nil && !di.ModTime().Before(srcInfo.ModTime()) {
		return dst, nil
	}

	f, err := os.Open(orig)
	if err != nil {
		return "", err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return "", err
	}
	b := src.Bounds()
	if b.Dx()*b.Dy() > maxThumbSourcePixels || b.Dx() <= width {
		return "", os.ErrInvalid // 太大不处理;本来就比目标窄也没必要缩
	}

	height := max(b.Dy()*width/b.Dx(), 1)
	out := boxDownscale(src, width, height)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	// 先写临时文件再改名:中途崩了也不会留下一个半截的缩略图,
	// 那种文件下次会被当成"已生成"直接发出去。
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".thumb-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if err := jpeg.Encode(tmp, out, &jpeg.Options{Quality: 80}); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return dst, nil
}

// boxDownscale 盒式平均缩小:目标每个像素取源图对应矩形的平均色。
// 只用于缩小 —— 放大时这个算法退化成最近邻,不要拿它放大。
func boxDownscale(src image.Image, dw, dh int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for dy := range dh {
		y0 := b.Min.Y + dy*b.Dy()/dh
		y1 := b.Min.Y + (dy+1)*b.Dy()/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for dx := range dw {
			x0 := b.Min.X + dx*b.Dx()/dw
			x1 := b.Min.X + (dx+1)*b.Dx()/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var rs, gs, bs, n uint64
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					// RGBA() 返回 16 位预乘分量,右移 8 位回到 0-255
					cr, cg, cb, _ := src.At(x, y).RGBA()
					rs += uint64(cr >> 8)
					gs += uint64(cg >> 8)
					bs += uint64(cb >> 8)
					n++
				}
			}
			i := dst.PixOffset(dx, dy)
			dst.Pix[i] = uint8(rs / n)
			dst.Pix[i+1] = uint8(gs / n)
			dst.Pix[i+2] = uint8(bs / n)
			dst.Pix[i+3] = 0xff
		}
	}
	return dst
}
