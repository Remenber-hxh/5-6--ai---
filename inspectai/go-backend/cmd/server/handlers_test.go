package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// P0-1 修复后的回归保护:不能再让"不正常 / 不合格 / 看不清"被误判为"正常"。
func TestNormalizeChoiceValue(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		options []string
		want    string
	}{
		// 精确匹配最优先
		{"exact 正常", "正常", []string{"正常", "异常"}, "正常"},
		{"exact 异常", "异常", []string{"正常", "异常"}, "异常"},

		// P0 修复点 —— 否定词不能误判
		{"否定: 不正常 → 异常", "不正常", []string{"正常", "异常"}, "异常"},
		{"否定: 不合格 → 异常", "不合格", []string{"正常", "异常"}, "异常"},
		{"否定: 不通过 → 异常", "不通过", []string{"正常", "异常"}, "异常"},

		// 模糊词 → 待复核
		{"模糊: 看不清 → 待复核", "看不清", []string{"正常", "异常", "待复核"}, "待复核"},
		{"模糊: 无法判定 → 待复核", "无法判定", []string{"正常", "异常", "待复核"}, "待复核"},

		// 正向同义词
		{"同义: 无异常 → 正常", "无异常", []string{"正常", "异常"}, "正常"},
		{"同义: 良好 → 正常", "良好", []string{"正常", "异常"}, "正常"},
		{"同义: 完好 → 完好", "良好", []string{"完好", "破损", "缺失"}, "完好"},

		// 状态字段
		{"破损 → 破损", "有破裂", []string{"完好", "破损", "缺失"}, "破损"},
		{"缺失 → 缺失", "丢失", []string{"完好", "破损", "缺失"}, "缺失"},

		// 没有合适选项时,留原值
		{"无匹配: 异常但选项无异常", "不合格", []string{"良好", "完好"}, "不合格"},

		// 空字符串
		{"空", "", []string{"正常", "异常"}, ""},

		// 是 / 否
		{"yes → 是", "yes", []string{"是", "否"}, "是"},
		{"no → 否", "no", []string{"是", "否"}, "否"},

		// 报警类异常词
		{"有报警 → 异常", "有报警", []string{"正常", "异常"}, "异常"},
		{"漏水 → 异常", "存在漏水", []string{"正常", "异常"}, "异常"},

		// === 扩展边界 (P3) ===

		// 空格残留
		{"前后空格 正常 → 正常", "  正常  ", []string{"正常", "异常"}, "正常"},
		// 注:内部空格(如 "不 合格")当前不归一化,留作已知限制 — AI 模型输出几乎不会出现这种空格

		// 大小写混合
		{"YES → 是", "YES", []string{"是", "否"}, "是"},
		{"Ok 大写混合 → 正常", "Ok", []string{"正常", "异常"}, "正常"},

		// 否定+异常词组合 (无XXX)
		{"无故障 → 正常", "无故障", []string{"正常", "异常"}, "正常"},
		{"未发现破损 → 正常", "未发现破损", []string{"正常", "异常"}, "正常"},
		{"没问题 → 正常", "没问题", []string{"正常", "异常"}, "正常"},
		{"不存在异常 → 正常", "不存在异常", []string{"正常", "异常"}, "正常"},

		// 状态字段组合
		{"无破损 → 完好", "无破损", []string{"完好", "破损"}, "完好"},
		{"丢失了 → 缺失", "丢失了", []string{"完好", "破损", "缺失"}, "缺失"},
		{"裂纹 → 破损", "存在裂纹", []string{"完好", "破损"}, "破损"},

		// 多关键词混合 (优先级测试)
		{"既正常又异常 → 异常 (异常词优先)", "正常但有报警", []string{"正常", "异常"}, "异常"},
		{"无异常且正常 → 正常", "无异常且运行正常", []string{"正常", "异常"}, "正常"},

		// 烧毁/跳闸/焦糊/燃气 等强异常词
		{"焦糊味 → 异常", "有焦糊味", []string{"正常", "异常"}, "异常"},
		{"跳闸 → 异常", "频繁跳闸", []string{"正常", "异常"}, "异常"},

		// 数值字符串被错误塞 choice (留原值,等人工)
		{"数值字符串无匹配 → raw", "3.14", []string{"正常", "异常"}, "3.14"},

		// 否定+正向(双重否定)
		{"不是异常 → 异常 (Contains 异常 优先)", "不是异常", []string{"正常", "异常"}, "异常"},

		// 选项无 异常 时,异常词退到待复核
		{"无异常选项 → 待复核 fallback", "破损", []string{"完好", "良好", "待复核"}, "待复核"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeChoiceValue(tc.raw, tc.options)
			if got != tc.want {
				t.Errorf("normalizeChoiceValue(%q, %v) = %q, want %q", tc.raw, tc.options, got, tc.want)
			}
		})
	}
}

func TestRecentImagesForAnalysis(t *testing.T) {
	images := []ImageInfo{
		{ID: "first"},
		{ID: "second"},
		{ID: "third"},
		{ID: "retake"},
	}

	got := recentImagesForAnalysis(images, 3)
	if len(got) != 3 {
		t.Fatalf("recentImagesForAnalysis returned %d images, want 3", len(got))
	}
	if got[0].ID != "second" || got[1].ID != "third" || got[2].ID != "retake" {
		t.Fatalf("recentImagesForAnalysis returned %#v, want the latest three images", got)
	}
}

func TestRecentImagesForAnalysisUsesDefaultLimit(t *testing.T) {
	images := []ImageInfo{
		{ID: "first"},
		{ID: "second"},
		{ID: "third"},
		{ID: "retake"},
	}

	got := recentImagesForAnalysis(images, 0)
	if len(got) != 3 || got[0].ID != "second" {
		t.Fatalf("recentImagesForAnalysis default limit returned %#v, want latest three images", got)
	}
}

func newRecordAccessTestServer(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	store := NewMemStore()
	users := []*User{
		{ID: "user_a", Username: "inspector_a", DisplayName: "巡检员A", RoleCode: roleInspector},
		{ID: "user_b", Username: "inspector_b", DisplayName: "巡检员B", RoleCode: roleInspector},
		{ID: "user_supervisor", Username: "supervisor", DisplayName: "复核主管", RoleCode: roleSupervisor},
		{ID: "user_manager", Username: "manager", DisplayName: "管理人员", RoleCode: roleManager},
		{ID: "user_admin", Username: "admin", DisplayName: "系统管理员", RoleCode: roleAdmin},
	}
	tokens := map[string]string{}
	for _, user := range users {
		if err := store.CreateUser(user, "test-password"); err != nil {
			t.Fatalf("CreateUser(%s): %v", user.Username, err)
		}
		_, session, err := store.AuthenticateUser(user.Username, "test-password")
		if err != nil {
			t.Fatalf("AuthenticateUser(%s): %v", user.Username, err)
		}
		tokens[user.Username] = session.Token
	}
	now := time.Now()
	records := []*Record{
		{ID: "rec_a", Inspector: "巡检员A", InspectorUserID: "user_a", TemplateID: "zihan_energy", CreatedAt: now.Add(-time.Minute)},
		{ID: "rec_b", Inspector: "巡检员B", InspectorUserID: "user_b", TemplateID: "zihan_energy", CreatedAt: now},
		// Old records created before inspector_user_id existed remain readable by their legacy owner name.
		{ID: "rec_legacy_a", Inspector: "巡检员A", TemplateID: "zihan_energy", CreatedAt: now.Add(-2 * time.Minute)},
	}
	for _, rec := range records {
		if err := store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord(%s): %v", rec.ID, err)
		}
	}
	if err := store.CreateTask(&AITask{ID: "task_b", RecordID: "rec_b", CreatedAt: now}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	storageDir := t.TempDir()
	uploadDir := filepath.Join(storageDir, "uploads", "rec_b")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploadDir, "proof.jpg"), []byte("photo"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return &Server{
		store:              store,
		storageDir:         storageDir,
		frontendDir:        t.TempDir(),
		corsAllowedOrigins: map[string]bool{},
	}, tokens
}

func requestWithToken(server *Server, method, target, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("X-InspectAI-Token", token)
	recorder := httptest.NewRecorder()
	server.router(recorder, req)
	return recorder
}

func TestInspectorCannotAccessAnotherInspectorsRecord(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	forbidden := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/inspection/records/rec_b"},
		{http.MethodPost, "/api/inspection/records/rec_b/images"},
		{http.MethodPost, "/api/inspection/records/rec_b/ai-tasks"},
		{http.MethodPatch, "/api/inspection/records/rec_b/fields/site"},
		{http.MethodPost, "/api/inspection/records/rec_b/manual"},
		{http.MethodPost, "/api/inspection/records/rec_b/submit"},
		{http.MethodGet, "/api/inspection/records/rec_b/confirm-logs"},
		{http.MethodGet, "/api/ai/tasks/task_b"},
		{http.MethodGet, "/storage/uploads/rec_b/proof.jpg"},
	}
	for _, tc := range forbidden {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got := requestWithToken(server, tc.method, tc.path, tokens["inspector_a"])
			if got.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", got.Code, http.StatusForbidden, got.Body.String())
			}
		})
	}
}

func TestInspectorRecordListIsFilteredByOwner(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet, "/api/inspection/records", tokens["inspector_a"])
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", got.Code, http.StatusOK, got.Body.String())
	}
	var payload struct {
		Records []*Record `json:"records"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("record count = %d, want 2; body=%s", len(payload.Records), got.Body.String())
	}
	for _, rec := range payload.Records {
		if rec.ID == "rec_b" {
			t.Fatalf("list leaked another inspector's record: %s", got.Body.String())
		}
	}
}

func TestManagementRolesReadGloballyButManagerCannotWriteRecordDirectly(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	read := requestWithToken(server, http.MethodGet, "/api/inspection/records/rec_b", tokens["manager"])
	if read.Code != http.StatusOK {
		t.Fatalf("manager read status = %d, want %d; body=%s", read.Code, http.StatusOK, read.Body.String())
	}
	write := requestWithToken(server, http.MethodPost, "/api/inspection/records/rec_b/manual", tokens["manager"])
	if write.Code != http.StatusForbidden {
		t.Fatalf("manager write status = %d, want %d; body=%s", write.Code, http.StatusForbidden, write.Body.String())
	}
	adminWrite := requestWithToken(server, http.MethodPost, "/api/inspection/records/rec_b/manual", tokens["admin"])
	if adminWrite.Code != http.StatusOK {
		t.Fatalf("admin write status = %d, want %d; body=%s", adminWrite.Code, http.StatusOK, adminWrite.Body.String())
	}
}

func TestAssetRoutesDoNotBypassInspectorRecordIsolation(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	store := server.store.(*MemStore)
	recB, err := store.GetRecord(defaultTenantID, "rec_b")
	if err != nil {
		t.Fatalf("GetRecord(rec_b): %v", err)
	}
	recB.Submitted = true
	recB.Images = []ImageInfo{{ID: "img_b", Path: filepath.Join(server.storageDir, "uploads", "rec_b", "proof.jpg")}}
	if err := store.UpdateRecord(recB); err != nil {
		t.Fatalf("UpdateRecord(rec_b): %v", err)
	}
	asset := &AssetEntry{
		ID:            "asset_b",
		Project:       "测试项目",
		AssetType:     "测试设备",
		AssetName:     "巡检员B的设备",
		LastRecordID:  "rec_b",
		LastPhotoPath: recB.Images[0].Path,
	}
	if err := store.UpsertAsset(asset); err != nil {
		t.Fatalf("UpsertAsset: %v", err)
	}
	store.assetSnapshots = append(store.assetSnapshots, &AssetSnapshot{
		ID: "snap_b", AssetID: asset.ID, RecordID: "rec_b", Summary: "private summary", CreatedAt: time.Now(),
	})

	detail := requestWithToken(server, http.MethodGet, "/api/assets/asset_b", tokens["inspector_a"])
	if detail.Code != http.StatusOK {
		t.Fatalf("asset detail status = %d, want %d; body=%s", detail.Code, http.StatusOK, detail.Body.String())
	}
	var detailPayload struct {
		Asset   *AssetEntry `json:"asset"`
		History []*Record   `json:"history"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("decode asset detail: %v", err)
	}
	if len(detailPayload.History) != 0 {
		t.Fatalf("asset detail leaked another inspector's history: %s", detail.Body.String())
	}
	if detailPayload.Asset.LastPhotoPath != "" || detailPayload.Asset.CoverImage != nil {
		t.Fatalf("asset detail leaked another inspector's photo path: %s", detail.Body.String())
	}

	history := requestWithToken(server, http.MethodGet, "/api/assets/asset_b/records", tokens["inspector_a"])
	if history.Code != http.StatusOK {
		t.Fatalf("asset records status = %d, want %d; body=%s", history.Code, http.StatusOK, history.Body.String())
	}
	var historyPayload struct {
		Records []*AssetSnapshot `json:"records"`
		Total   int              `json:"total"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &historyPayload); err != nil {
		t.Fatalf("decode asset history: %v", err)
	}
	if historyPayload.Total != 0 || len(historyPayload.Records) != 0 {
		t.Fatalf("asset records leaked another inspector's snapshots: %s", history.Body.String())
	}

	for _, path := range []string{"/api/assets/asset_b/report", "/api/assets/asset_b/status-events"} {
		got := requestWithToken(server, http.MethodGet, path, tokens["inspector_a"])
		if got.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d, want %d; body=%s", path, got.Code, http.StatusForbidden, got.Body.String())
		}
	}
}

func TestSQLiteRecordOwnershipPersistence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "inspectai.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	rec := &Record{
		ID: "rec_owner", Inspector: "巡检员A", InspectorUserID: "user_a",
		Project: "测试项目", PointID: "point", PointName: "点位",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表", Type: "测试",
		CreatedAt: time.Now(),
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	got, err := store.GetRecord(defaultTenantID, rec.ID)
	if err != nil {
		t.Fatalf("GetRecord: %v", err)
	}
	if got.InspectorUserID != "user_a" {
		t.Fatalf("InspectorUserID = %q, want user_a", got.InspectorUserID)
	}
	list, err := store.ListRecordsByOwner(defaultTenantID, "user_a", "巡检员A", "inspector_a", 10)
	if err != nil {
		t.Fatalf("ListRecordsByOwner: %v", err)
	}
	if len(list) != 1 || list[0].ID != rec.ID {
		t.Fatalf("ListRecordsByOwner = %#v, want rec_owner", list)
	}
}

func TestInspectorCannotReadAnotherInspectorsChangeRequest(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	store := server.store.(*MemStore)
	if err := store.CreateChangeRequest(&ChangeRequest{
		ID: "cr_b", TargetType: "record", TargetID: "rec_b",
		RequestedBy: "巡检员B", RequestedAt: time.Now(), Status: "pending",
	}); err != nil {
		t.Fatalf("CreateChangeRequest: %v", err)
	}
	got := requestWithToken(server, http.MethodGet, "/api/change-requests/cr_b", tokens["inspector_a"])
	if got.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", got.Code, http.StatusForbidden, got.Body.String())
	}
	manager := requestWithToken(server, http.MethodGet, "/api/change-requests/cr_b", tokens["manager"])
	if manager.Code != http.StatusOK {
		t.Fatalf("manager status = %d, want %d; body=%s", manager.Code, http.StatusOK, manager.Body.String())
	}
}

func TestSharedInspectorTokenCannotCreateUnownedProductionRecord(t *testing.T) {
	server, _ := newRecordAccessTestServer(t)
	server.authToken = "legacy-shared-token"
	req := httptest.NewRequest(http.MethodPost, "/api/inspection/records", strings.NewReader(`{"templateId":"zihan_energy"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", "legacy-shared-token")
	recorder := httptest.NewRecorder()
	server.router(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
}

func TestInspectorCannotAccessManagementAI(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	tests := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/management-ai/snapshot", ""},
		{http.MethodGet, "/api/management-ai/attention", ""},
		{http.MethodPost, "/api/management-ai/chat", `{"message":"今日有哪些异常资产？"}`},
	}
	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("X-InspectAI-Token", tokens["inspector_a"])
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			recorder := httptest.NewRecorder()
			server.router(recorder, req)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
			}
		})
	}
}

func TestManagementRolesCanAccessManagementAI(t *testing.T) {
	analytics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answer":"ok","summary":"ok","model":"test-model","isMock":false}`))
	}))
	defer analytics.Close()

	server, tokens := newRecordAccessTestServer(t)
	server.analyticsClient = NewAnalyticsClient(analytics.URL)
	for _, username := range []string{"supervisor", "manager", "admin"} {
		t.Run(username, func(t *testing.T) {
			for _, path := range []string{"/api/management-ai/snapshot", "/api/management-ai/attention"} {
				got := requestWithToken(server, http.MethodGet, path, tokens[username])
				if got.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want %d; body=%s", path, got.Code, http.StatusOK, got.Body.String())
				}
			}
			req := httptest.NewRequest(http.MethodPost, "/api/management-ai/chat", strings.NewReader(`{"message":"今日有哪些异常资产？"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-InspectAI-Token", tokens[username])
			recorder := httptest.NewRecorder()
			server.router(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("chat status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
		})
	}
}

func TestPromptTemplateEndpoints(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	if err := ensurePromptTemplateSeeds(server.store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok := tokens["admin"]

	// 列表
	got := requestWithToken(server, http.MethodGet, "/api/prompt/templates", tok)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "elevator_machine_room") {
		t.Fatalf("list code=%d body=%s", got.Code, got.Body.String())
	}
	// 详情
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/elevator_machine_room", tok)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "door_window_sign") {
		t.Fatalf("get code=%d body=%s", got.Code, got.Body.String())
	}
	// 预览渲染
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/elevator_machine_room/render", tok)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "字段映射") {
		t.Fatalf("render code=%d body=%s", got.Code, got.Body.String())
	}
	// 保存(改名 + 改字段)→ 再取应反映
	body := `{"id":"elevator_machine_room","name":"改名测试","scene":"x","fields":[{"code":"door_window_sign","label":"门改过了","group":"机房","mode":"visual","yesWhen":"y"}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/prompt/templates/elevator_machine_room", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put code=%d body=%s", rec.Code, rec.Body.String())
	}
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/elevator_machine_room", tok)
	if !strings.Contains(got.Body.String(), "改名测试") || !strings.Contains(got.Body.String(), "门改过了") {
		t.Fatalf("after save not reflected: %s", got.Body.String())
	}
	// 渲染应反映新内容(即时生效)
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/elevator_machine_room/render", tok)
	if !strings.Contains(got.Body.String(), "门改过了") {
		t.Fatalf("render after save not reflected: %s", got.Body.String())
	}
}

func TestPromptTemplateRequiresAuth(t *testing.T) {
	server, _ := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet, "/api/prompt/templates", "")
	if got.Code == http.StatusOK {
		t.Fatalf("无 token 不应返回 200,got %d", got.Code)
	}
}
