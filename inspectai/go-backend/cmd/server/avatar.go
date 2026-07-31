package main

import (
	"net/http"
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

	// 头像按用户分目录:换头像时旧文件留在原地,不做删除 ——
	// 删除要处理"正在被 CDN/浏览器缓存引用"的旧路径,收益不抵复杂度。
	// 单个用户的头像历史最多几十 KB,不构成存储压力。
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

	writeJSON(w, http.StatusOK, map[string]any{"avatar": rel})
}
