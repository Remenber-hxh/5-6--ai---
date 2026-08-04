package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 造一张左半红、右半蓝的图,便于验证缩放后颜色落在该落的地方 ——
// 只看"文件变小了"证明不了缩放是对的,像素花了也会变小。
func writeTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.RGBA{220, 30, 30, 255})
			} else {
				img.Set(x, y, color.RGBA{30, 30, 220, 255})
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureThumbShrinksAndKeepsColors(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "shot.jpg")
	writeTestJPEG(t, orig, 900, 1600)

	thumb, err := ensureThumb(orig, 240)
	if err != nil {
		t.Fatalf("生成缩略图失败: %v", err)
	}

	oi, _ := os.Stat(orig)
	ti, err := os.Stat(thumb)
	if err != nil {
		t.Fatalf("缩略图不存在: %v", err)
	}
	if ti.Size() >= oi.Size() {
		t.Fatalf("缩略图没有变小:原图 %d,缩略图 %d", oi.Size(), ti.Size())
	}

	f, err := os.Open(thumb)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("缩略图解不开: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 240 {
		t.Fatalf("宽度不对:要 240,得到 %d", b.Dx())
	}
	// 900:1600 的比例缩到宽 240,高应当是 426
	if want := 1600 * 240 / 900; b.Dy() != want {
		t.Fatalf("高度没按比例:要 %d,得到 %d", want, b.Dy())
	}
	// 左四分之一应当仍是红的,右四分之一仍是蓝的
	lr, lg, lb, _ := img.At(b.Dx()/4, b.Dy()/2).RGBA()
	rr, rg, rb, _ := img.At(b.Dx()*3/4, b.Dy()/2).RGBA()
	if lr>>8 < 150 || lg>>8 > 90 || lb>>8 > 90 {
		t.Fatalf("左半边不是红的: %d,%d,%d", lr>>8, lg>>8, lb>>8)
	}
	if rb>>8 < 150 || rr>>8 > 90 || rg>>8 > 90 {
		t.Fatalf("右半边不是蓝的: %d,%d,%d", rr>>8, rg>>8, rb>>8)
	}
}

func TestEnsureThumbReusesCache(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "shot.jpg")
	writeTestJPEG(t, orig, 600, 600)

	first, err := ensureThumb(orig, 120)
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(first)

	second, err := ensureThumb(orig, 120)
	if err != nil {
		t.Fatal(err)
	}
	si, _ := os.Stat(second)
	if first != second {
		t.Fatalf("两次路径不同: %s vs %s", first, second)
	}
	// 复用缓存的话不会重写文件,修改时间应当一致
	if !si.ModTime().Equal(fi.ModTime()) {
		t.Fatal("第二次请求重新生成了缩略图,缓存没起作用")
	}
}

func TestThumbWidthWhitelist(t *testing.T) {
	// 不在白名单里的宽度一律当作"发原图",否则任何人都能用 ?w=1..2000
	// 让服务器生成两千个变体把磁盘塞满
	cases := map[string]int{"": 0, "240": 240, "120": 120, "480": 480, "241": 0, "0": 0, "-1": 0, "abc": 0, "99999": 0}
	for q, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/x?w="+q, nil)
		if got := thumbWidthFromRequest(r); got != want {
			t.Fatalf("?w=%q 期望 %d,得到 %d", q, want, got)
		}
	}
}

func TestServeImageSetsPrivateCacheHeaders(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "shot.jpg")
	writeTestJPEG(t, orig, 800, 600)

	s := &Server{}
	rec := httptest.NewRecorder()
	s.serveImage(rec, httptest.NewRequest(http.MethodGet, "/storage/uploads/x/shot.jpg", nil), orig)

	res := rec.Result()
	defer res.Body.Close()
	// 巡检照片是受权限控制的内容:必须 private,不能让中间代理缓存后发给别人
	if cc := res.Header.Get("Cache-Control"); cc != "private, max-age=604800" {
		t.Fatalf("Cache-Control 不对: %q", cc)
	}
	if res.Header.Get("ETag") == "" {
		t.Fatal("没有 ETag —— 重访会重下而不是走 304")
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("状态码 %d", res.StatusCode)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("没有图片内容")
	}
}

func TestServeImageThumbIsSmallerThanOriginal(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "shot.jpg")
	writeTestJPEG(t, orig, 900, 1600)
	s := &Server{}

	get := func(q string) int {
		rec := httptest.NewRecorder()
		s.serveImage(rec, httptest.NewRequest(http.MethodGet, "/img"+q, nil), orig)
		return rec.Body.Len()
	}
	full, thumb := get(""), get("?w=240")
	if thumb == 0 || full == 0 {
		t.Fatal("有一路返回了空内容")
	}
	if thumb >= full {
		t.Fatalf("?w=240 没有更小:原图 %d,缩略 %d", full, thumb)
	}
}

func TestServeImageFallsBackWhenThumbFails(t *testing.T) {
	// 不是图片的文件生成缩略图会失败 —— 这时必须发原文件,
	// 而不是报错。照片是巡检的证据,宁可多花流量也不能打不开。
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.jpg")
	if err := os.WriteFile(p, []byte("这不是一张图"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	rec := httptest.NewRecorder()
	s.serveImage(rec, httptest.NewRequest(http.MethodGet, "/img?w=240", nil), p)
	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("状态码 %d,应当回落发原文件", rec.Result().StatusCode)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("这不是一张图")) {
		t.Fatal("没有回落到原文件")
	}
}
