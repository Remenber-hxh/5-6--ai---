package main

import (
	"net/http"
	"strings"
	"testing"
)

// 未知 /api 路径必须回 404 JSON,绝不能落进静态兜底回 200 + 前端 HTML。
//
// 起因:移动端离线上传队列按 res.ok 判定上传成功、成功即删本地原图。
// 后端当时对未知 /api 路径返回 200 + HTML,导致"照片没传上去却被本地删掉"
// 的静默数据丢失。API 与静态资源必须分流,本用例锁死。
func TestUnknownAPIPathReturns404JSON(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)

	paths := []string{
		"/api/inspection/offline-shots",
		"/api/nonexistent",
		"/api/assets", // 存在的前缀但方法/路径不匹配也不该回 HTML
	}
	for _, p := range paths {
		got := requestJSON(srv, http.MethodPost, p, tokens["inspector"], `{}`)
		if got.Code == http.StatusOK {
			body := got.Body.String()
			if len(body) > 80 {
				body = body[:80]
			}
			t.Errorf("POST %s = 200(疑似落进静态兜底);body 前 80 字节=%q", p, body)
			continue
		}
		if ct := got.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("POST %s Content-Type = %q, 期望 JSON", p, ct)
		}
		if strings.Contains(got.Body.String(), "<!doctype html") ||
			strings.Contains(got.Body.String(), "<html") {
			t.Errorf("POST %s 返回了 HTML,客户端会误判为成功", p)
		}
	}
}
