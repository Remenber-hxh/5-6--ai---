package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 最小合法 PNG(1x1),够走通格式校验与落盘
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func avatarUploadReq(t *testing.T, token, filename string, body []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/me/avatar", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		r.Header.Set("X-InspectAI-Token", token)
	}
	return r
}

// 巡检员能改自己的头像 —— 这正是 PUT /api/users/<id> 做不到的
// (那条整体 hasAdminAccess 门控)。
func TestUpdateMyAvatar_InspectorCanSetOwn(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	dir := t.TempDir()
	srv.storageDir = dir

	w := httptest.NewRecorder()
	srv.router(w, avatarUploadReq(t, tokens["inspector"], "me.png", tinyPNG))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200;body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Avatar string `json:"avatar"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	// 必须是相对路径:存绝对磁盘路径会在换机器/换部署目录后全部失效
	if resp.Avatar == "" || strings.HasPrefix(resp.Avatar, "/") || strings.Contains(resp.Avatar, ":") {
		t.Errorf("avatar = %q, 期望 storage 根下的相对路径", resp.Avatar)
	}
	if !strings.HasPrefix(resp.Avatar, "avatars/") {
		t.Errorf("avatar = %q, 期望落在 avatars/ 下", resp.Avatar)
	}
	// VARCHAR(512) 是硬上限
	if len(resp.Avatar) > 512 {
		t.Errorf("avatar 路径 %d 字符,超出列宽 512", len(resp.Avatar))
	}
	// 文件真的落盘了
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(resp.Avatar))); err != nil {
		t.Errorf("文件未落盘: %v", err)
	}

	// 会话用户的 Avatar 已更新
	u, ok := srv.userFromSessionToken(tokens["inspector"])
	if !ok || u.Avatar != resp.Avatar {
		t.Errorf("用户 Avatar = %q, 期望 %q", u.Avatar, resp.Avatar)
	}
}

// 无会话必须挡住 —— 否则任何人都能往存储里写文件
func TestUpdateMyAvatar_RejectsAnonymous(t *testing.T) {
	srv, _ := newClosedLoopServer(t)
	srv.storageDir = t.TempDir()

	w := httptest.NewRecorder()
	srv.router(w, avatarUploadReq(t, "", "x.png", tinyPNG))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("匿名上传状态码 = %d, 期望 401", w.Code)
	}
}

// 非图片格式挡住(saveMultipartFile 的白名单)
func TestUpdateMyAvatar_RejectsNonImage(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	srv.storageDir = t.TempDir()

	w := httptest.NewRecorder()
	srv.router(w, avatarUploadReq(t, tokens["inspector"], "payload.svg", []byte("<svg/>")))
	if w.Code != http.StatusBadRequest {
		t.Errorf("svg 上传状态码 = %d, 期望 400", w.Code)
	}
}

// 两个用户各改各的,互不覆盖 —— 接口不收 userID,天然按会话隔离
func TestUpdateMyAvatar_PerUserIsolation(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	srv.storageDir = t.TempDir()

	for _, who := range []string{"inspector", "supervisor"} {
		w := httptest.NewRecorder()
		srv.router(w, avatarUploadReq(t, tokens[who], who+".png", tinyPNG))
		if w.Code != http.StatusOK {
			t.Fatalf("%s 上传失败 %d: %s", who, w.Code, w.Body.String())
		}
	}

	a, _ := srv.userFromSessionToken(tokens["inspector"])
	b, _ := srv.userFromSessionToken(tokens["supervisor"])
	if a.Avatar == "" || b.Avatar == "" || a.Avatar == b.Avatar {
		t.Errorf("两人头像应各自独立:inspector=%q supervisor=%q", a.Avatar, b.Avatar)
	}
}
