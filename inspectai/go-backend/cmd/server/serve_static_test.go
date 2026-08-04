package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 2026-08-04 起：新版移动端占根路径，旧版整体挪到 /old/。
// 这个测试盯住的是"两套前端不能串台"——串了的表现是白屏或者拿到另一套的
// 资源，而且很难一眼看出来。
func TestServeStaticRoutesNewAppAtRootAndLegacyUnderOld(t *testing.T) {
	dir := t.TempDir()
	newDir := filepath.Join(dir, "mobile-web", "dist")
	oldDir := filepath.Join(dir, "frontend")
	for _, d := range []string{filepath.Join(newDir, "assets"), oldDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, body string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(newDir, "index.html"), "NEW_APP_INDEX")
	write(filepath.Join(newDir, "assets", "index-abc.js"), "NEW_APP_JS")
	write(filepath.Join(oldDir, "index.html"), "OLD_APP_INDEX")
	write(filepath.Join(oldDir, "app.js"), "OLD_APP_JS")

	s := &Server{frontendDir: oldDir, mobileWebDir: newDir, storageDir: filepath.Join(dir, "storage")}
	get := func(p string) (*http.Response, string) {
		t.Helper()
		rec := httptest.NewRecorder()
		s.serveStatic(rec, httptest.NewRequest(http.MethodGet, p, nil))
		res := rec.Result()
		return res, rec.Body.String()
	}

	cases := []struct{ path, want string }{
		{"/", "NEW_APP_INDEX"},
		{"/assets/index-abc.js", "NEW_APP_JS"},
		// HashRouter 的路径不带扩展名，要回落 index.html 而不是 404
		{"/record/abc", "NEW_APP_INDEX"},
		{"/old/", "OLD_APP_INDEX"},
		{"/old/app.js", "OLD_APP_JS"},
	}
	for _, c := range cases {
		res, body := get(c.path)
		if res.StatusCode != http.StatusOK {
			t.Errorf("%s 状态码 %d", c.path, res.StatusCode)
			continue
		}
		if !strings.Contains(body, c.want) {
			t.Errorf("%s 发的不是 %s(拿到 %q)", c.path, c.want, body)
		}
	}

	// /old 少了尾斜杠必须重定向：旧版的资源是相对路径引的，在 /old 下会解析到
	// 根目录去（那是新版的地盘），结果就是白屏
	res, _ := get("/old")
	if res.StatusCode != http.StatusMovedPermanently {
		t.Errorf("/old 应当 301 重定向，得到 %d", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/old/" {
		t.Errorf("/old 应当跳到 /old/，得到 %q", loc)
	}

	// 新版的 index 不能被强缓存，否则用户永远拿不到新版本
	res, _ = get("/")
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("index.html 的 Cache-Control 是 %q，必须 no-store", cc)
	}

	// 路径穿越
	for _, bad := range []string{"/../frontend/app.js", "/old/../../etc/passwd"} {
		res, body := get(bad)
		if strings.Contains(body, "OLD_APP_JS") || res.StatusCode == http.StatusOK && strings.Contains(body, "root:") {
			t.Errorf("%s 逃出了目录: %q", bad, body)
		}
	}
}

// 忘了 npm run build 时不能是一片空白 —— 那种时候人第一反应是"后端挂了"，
// 能白查很久。必须给一句能照着做的话。
func TestServeStaticTellsYouWhenNotBuilt(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "frontend")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "index.html"), []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{frontendDir: oldDir, mobileWebDir: filepath.Join(dir, "not-built")}

	rec := httptest.NewRecorder()
	s.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if rec.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("没构建时应当 503，得到 %d", rec.Result().StatusCode)
	}
	for _, want := range []string{"npm run build", "/old"} {
		if !strings.Contains(body, want) {
			t.Errorf("提示里缺少 %q:%s", want, body)
		}
	}

	// 新版没构建也不能影响旧版 —— 这是回滚时的活路
	rec = httptest.NewRecorder()
	s.serveStatic(rec, httptest.NewRequest(http.MethodGet, "/old/", nil))
	if !strings.Contains(rec.Body.String(), "OLD") {
		t.Error("新版没构建时旧版也打不开了")
	}
}
