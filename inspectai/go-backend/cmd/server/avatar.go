package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ===== 自助头像 =====
//
// 为什么单开一个接口,而不是复用 PUT /api/users/<id>:
// 那条路由整体 hasAdminAccess 门控(仅系统管理员可管理账号),巡检员改不了自己的。
// 这里【不接收 userID】—— 只认会话里的当前用户,天然没有越权改他人头像的面。
//
// 为什么存文件而不是 data URL 直接塞库:
// users.avatar 是 VARCHAR(512)(schema_mysql.sql:129),放不下 base64。
// 512 存一个 /storage/ 相对路径绰绰有余,和巡检照片同一套存法。

const avatarMaxSize = 2 << 20 // 2MB。客户端会先压到 256px,这里只是兜底

// handleUpdateMyAvatar POST /api/auth/me/avatar (multipart, 字段名 file)
func (s *Server) handleUpdateMyAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := s.userFromSessionToken(s.tokenFromRequest(r))
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}

	if err := r.ParseMultipartForm(avatarMaxSize); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "表单解析失败:"+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少图片字段 file")
		return
	}
	_ = file.Close()

	// 头像按用户分目录 —— 换头像时清掉这个人的旧文件(见函数末尾)。
	// 分目录的意义就在这里:清理时只需扫自己这一个目录,不会误伤别人。
	targetDir := filepath.Join(s.storageDir, "avatars", sanitizeFileName(user.ID))
	info, err := saveMultipartFile(targetDir, header, avatarMaxSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	// 库里存相对 storage 根的路径,前端拼 /storage/ 前缀访问。
	// 存绝对磁盘路径会让数据在换机器/换部署目录后全部失效。
	rel := strings.TrimPrefix(filepath.ToSlash(info.Path), filepath.ToSlash(s.storageDir))
	rel = strings.TrimPrefix(rel, "/")
	if len(rel) > 512 {
		writeError(w, http.StatusInternalServerError, "path_too_long", "头像路径超长")
		return
	}

	if err := s.store.UpdateUserProfile(user.ID, func(u *User) { u.Avatar = rel }); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	// 【顺序要紧】先落库、成功了再删旧文件。反过来的话,一旦更新失败,
	// 这个人就既没有新头像记录、旧文件也没了 —— 直接掉成无头像。
	pruneOldAvatars(targetDir, filepath.Base(info.Path))

	writeJSON(w, http.StatusOK, map[string]any{"avatar": rel})
}

// pruneOldAvatars 删掉该用户目录下除 keep 之外的所有文件。
//
// 只扫这一个用户自己的目录,不会误伤别人。清理失败一律忽略:
// 残留一个几 KB 的旧文件不影响任何功能,为它把一次成功的换头像判成失败不值得。
//
// 注意:删除会让其他设备上仍缓存着旧路径的页面拿到 404。前端 Avatar 组件
// 对图片加载失败会回落到文字头像,所以最坏是"看到首字"而不是裂图。
func pruneOldAvatars(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == keep {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
}
