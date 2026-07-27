package main

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func uploadShot(t *testing.T, srv *Server, token, idemKey, fileName string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("files", fileName)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(body); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_ = mw.WriteField("capturedAt", "2026-07-23T10:20:30Z")
	_ = mw.WriteField("lat", "31.2304")
	_ = mw.WriteField("lng", "121.4737")
	_ = mw.WriteField("accuracy", "12.5")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/inspection/offline-shots", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-InspectAI-Token", token)
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 离线照片上传:幂等键必填;同键重传不产生重复行;拍摄时间与服务器收到时间分开存。
func TestOfflineShotUploadIsIdempotent(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	jpg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x11}, 2048)...)

	// 缺幂等键 → 400,不接受"可能重复"的上传
	if got := uploadShot(t, srv, tokens["inspector"], "", "a.jpg", jpg); got.Code != http.StatusBadRequest {
		t.Fatalf("无幂等键 = %d, want 400; body=%s", got.Code, got.Body.String())
	}

	// 首次上传
	first := uploadShot(t, srv, tokens["inspector"], "idem-1", "a.jpg", jpg)
	if first.Code != http.StatusOK {
		t.Fatalf("首次上传 = %d; body=%s", first.Code, first.Body.String())
	}
	var r1 struct {
		ID       string       `json:"id"`
		Replayed bool         `json:"replayed"`
		Shot     *OfflineShot `json:"shot"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &r1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if r1.Replayed {
		t.Error("首次上传不应标记为 replayed")
	}
	if r1.Shot == nil || r1.Shot.CapturedAt == "" || r1.Shot.ReceivedAt == "" {
		t.Fatalf("拍摄时间与收到时间都应落库: %+v", r1.Shot)
	}
	if r1.Shot.CapturedAt == r1.Shot.ReceivedAt {
		t.Error("拍摄时间与服务器收到时间应分别记录,不应相同")
	}
	if r1.Shot.Lat == nil || r1.Shot.Lng == nil {
		t.Error("定位应落库")
	}

	// 同键重传(模拟弱网下"传成功了但响应没回来")→ 仍 200,标记 replayed,不产生新行
	second := uploadShot(t, srv, tokens["inspector"], "idem-1", "a.jpg", jpg)
	if second.Code != http.StatusOK {
		t.Fatalf("同键重传 = %d; body=%s", second.Code, second.Body.String())
	}
	var r2 struct {
		ID       string `json:"id"`
		Replayed bool   `json:"replayed"`
	}
	_ = json.Unmarshal(second.Body.Bytes(), &r2)
	if !r2.Replayed {
		t.Error("同键重传应标记 replayed")
	}
	if r2.ID != r1.ID {
		t.Errorf("同键重传应返回同一条: %q vs %q", r2.ID, r1.ID)
	}

	// 不同键 = 另一张照片,应新增
	third := uploadShot(t, srv, tokens["inspector"], "idem-2", "b.jpg", jpg)
	if third.Code != http.StatusOK {
		t.Fatalf("第二张 = %d; body=%s", third.Code, third.Body.String())
	}

	shots, err := srv.store.ListOfflineShots(defaultTenantID, "", 100)
	if err != nil {
		t.Fatalf("ListOfflineShots: %v", err)
	}
	if len(shots) != 2 {
		t.Errorf("共应有 2 张(重传不计),实际 %d", len(shots))
	}
}

// 幂等在 SQLite 层由唯一约束保证 —— MemStore 的循环判重不能代表真实行为,单独验。
func TestOfflineShotIdempotencyOnSQLite(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "oshot.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	mk := func(key string) (*OfflineShot, bool) {
		t.Helper()
		got, replayed, err := store.CreateOfflineShot(&OfflineShot{
			IdempotencyKey: key, FileName: "x.jpg", CapturedAt: "2026-07-23T10:00:00+08:00",
		})
		if err != nil {
			t.Fatalf("CreateOfflineShot(%s): %v", key, err)
		}
		return got, replayed
	}

	a, replayedA := mk("k1")
	if replayedA {
		t.Error("首次不应 replayed")
	}
	b, replayedB := mk("k1")
	if !replayedB {
		t.Error("同键第二次应 replayed")
	}
	if a.ID != b.ID {
		t.Errorf("同键应返回同一行: %q vs %q", a.ID, b.ID)
	}

	shots, _ := store.ListOfflineShots(defaultTenantID, "", 100)
	if len(shots) != 1 {
		t.Errorf("唯一约束应挡住重复,实际 %d 行", len(shots))
	}
}

// 跨租户不可见:A 租户列不出 B 租户的离线照片。
func TestOfflineShotTenantIsolation(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "oshot_iso.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	for _, tc := range []struct{ tenant, key string }{{"t_a", "ka"}, {"t_b", "kb"}} {
		if _, _, err := store.CreateOfflineShot(&OfflineShot{
			TenantID: tc.tenant, IdempotencyKey: tc.key, FileName: "x.jpg",
		}); err != nil {
			t.Fatalf("CreateOfflineShot(%s): %v", tc.tenant, err)
		}
	}
	a, _ := store.ListOfflineShots("t_a", "", 100)
	if len(a) != 1 || a[0].TenantID != "t_a" {
		t.Errorf("t_a 应只见自己的 1 张,实际 %d", len(a))
	}
	b, _ := store.ListOfflineShots("t_b", "", 100)
	if len(b) != 1 || b[0].TenantID != "t_b" {
		t.Errorf("t_b 应只见自己的 1 张,实际 %d", len(b))
	}
}
