package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===== 三条核心链路回归:闭环状态机 / 提交幂等 / 审批状态同步 =====
// 这三处业务逻辑最复杂、跨函数联动最多,是重构前必须有的安全网。

func newClosedLoopServer(t *testing.T) (*Server, map[string]string) {
	t.Helper()
	store := NewMemStore()
	users := []*User{
		{ID: "user_i", Username: "inspector", DisplayName: "巡检员", RoleCode: roleInspector},
		{ID: "user_s", Username: "supervisor", DisplayName: "复核主管", RoleCode: roleSupervisor},
	}
	tokens := map[string]string{}
	for _, u := range users {
		if err := store.CreateUser(u, "test-password"); err != nil {
			t.Fatalf("CreateUser(%s): %v", u.Username, err)
		}
		_, sess, err := store.AuthenticateUser(u.Username, "test-password")
		if err != nil {
			t.Fatalf("AuthenticateUser(%s): %v", u.Username, err)
		}
		tokens[u.Username] = sess.Token
	}
	srv := &Server{
		store:              store,
		storageDir:         t.TempDir(),
		frontendDir:        t.TempDir(),
		corsAllowedOrigins: map[string]bool{},
		aiSem:              make(chan struct{}, 1),
		loginGuard:         newLoginGuard(),
		// 空 baseURL:提交后触发的 Summarize 会返回错误而非 panic,
		// 提交主流程对 AI 总结失败已降级(不影响记录落库)。
		aiClient:        NewAIClient(""),
		analyticsClient: NewAnalyticsClient(""),
	}
	// 权限矩阵取默认值(=固化行为:主管三档全能力)
	srv.permCache.set(defaultPermMatrix())
	return srv, tokens
}

func requestJSON(server *Server, method, target, token, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("X-InspectAI-Token", token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.router(rec, req)
	return rec
}

// —— 链路一:异常资产标记正常 → 关联「待整改」任务自动销账 ——
func TestAssetMarkNormalClosesRectifyTasks(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	if err := srv.store.CreateAsset(&AssetEntry{ID: "A1", Project: "会议中心", AssetName: "电梯X", LastStatus: "异常"}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	mustCreateTask := func(id, assetID, status string) {
		t.Helper()
		if err := srv.store.CreateEngineeringTask(&EngineeringTask{ID: id, AssetID: assetID, Status: status, Title: id}); err != nil {
			t.Fatalf("CreateEngineeringTask(%s): %v", id, err)
		}
	}
	mustCreateTask("t_rectify", "A1", engTaskStatusRectify) // 应销账
	mustCreateTask("t_doing", "A1", "进行中")                  // 不该动
	mustCreateTask("t_other", "A9", engTaskStatusRectify)   // 别的资产,不该动

	got := requestJSON(srv, http.MethodPatch, "/api/assets/A1", tokens["supervisor"], `{"lastStatus":"正常"}`)
	if got.Code != http.StatusOK {
		t.Fatalf("PATCH asset = %d body=%s", got.Code, got.Body.String())
	}

	wantStatus := map[string]string{
		"t_rectify": engTaskStatusDone,
		"t_doing":   "进行中",
		"t_other":   engTaskStatusRectify,
	}
	for id, want := range wantStatus {
		task, err := srv.store.GetEngineeringTask(id)
		if err != nil {
			t.Fatalf("GetEngineeringTask(%s): %v", id, err)
		}
		if task.Status != want {
			t.Errorf("task %s status = %q, want %q", id, task.Status, want)
		}
	}
	if task, _ := srv.store.GetEngineeringTask("t_rectify"); task.CloseResult != "整改闭环" {
		t.Errorf("t_rectify closeResult = %q, want 整改闭环", task.CloseResult)
	}
	if asset, _ := srv.store.GetAsset(defaultTenantID, "A1"); asset.LastStatus != "正常" {
		t.Errorf("asset status = %q, want 正常", asset.LastStatus)
	}
}

// —— 链路二:提交幂等(Idempotency-Key 状态机) ——
func TestSubmitIdempotency(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	newRecord := func(id string) {
		t.Helper()
		err := srv.store.CreateRecord(&Record{
			ID: id, Inspector: "巡检员", InspectorUserID: "user_i",
			TemplateID: "zihan_energy", RecognitionStatus: "recognized",
			CreatedAt: time.Now(),
			Fields: []FieldValue{{
				Code: "reading", Label: "读数", Required: true,
				Value: "123", Source: "human-confirmed", Confidence: 0.97,
			}},
			// 【模板要求每单至少 5 张照片】这条用例验的是提交幂等,不是照片数,
			// 但记录本身必须是能提交的 —— 否则第一步就被 400 挡住,后面全测不到。
			Images: fixtureImages(5),
		})
		if err != nil {
			t.Fatalf("CreateRecord(%s): %v", id, err)
		}
	}
	newRecord("rs1")

	// 无 Idempotency-Key → 400
	req := httptest.NewRequest(http.MethodPost, "/api/inspection/records/rs1/submit", strings.NewReader(""))
	req.Header.Set("X-InspectAI-Token", tokens["inspector"])
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no key: code = %d, want 400", rec.Code)
	}

	submit := func(recordID, key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/inspection/records/"+recordID+"/submit", strings.NewReader(""))
		req.Header.Set("X-InspectAI-Token", tokens["inspector"])
		req.Header.Set("Idempotency-Key", key)
		out := httptest.NewRecorder()
		srv.router(out, req)
		return out
	}

	// 首次提交成功
	if got := submit("rs1", "k1"); got.Code != http.StatusOK {
		t.Fatalf("first submit = %d body=%s", got.Code, got.Body.String())
	}
	r1, _ := srv.store.GetRecord(defaultTenantID, "rs1")
	if !r1.Submitted {
		t.Fatalf("record not marked submitted")
	}
	// 同 key 重放 → 200 且不报错(幂等命中)
	if got := submit("rs1", "k1"); got.Code != http.StatusOK {
		t.Fatalf("replay same key = %d body=%s", got.Code, got.Body.String())
	}
	// 已提交后换 key 重放 → 仍 200 幂等短路(rec.Submitted 直接返回)
	if got := submit("rs1", "k2"); got.Code != http.StatusOK {
		t.Fatalf("replay new key after submitted = %d body=%s", got.Code, got.Body.String())
	}

	// 占坑中的记录,不同 key 提交 → 409(他人提交处理中)
	newRecord("rs2")
	if claim, err := srv.store.ClaimSubmission("rs2", "kA"); err != nil || claim != submissionClaimed {
		t.Fatalf("pre-claim: claim=%v err=%v", claim, err)
	}
	if got := submit("rs2", "kB"); got.Code != http.StatusConflict {
		t.Fatalf("busy submit = %d, want 409; body=%s", got.Code, got.Body.String())
	}
	// 同 key(kA)且未完成 → 409 in_progress
	if got := submit("rs2", "kA"); got.Code != http.StatusConflict {
		t.Fatalf("in-progress submit = %d, want 409; body=%s", got.Code, got.Body.String())
	}
}

// —— 链路三:审批通过 → 资产字段应用 + 异常自动销账;驳回不动数据 ——
func TestApproveChangeRequestSyncsAssetAndClosesLoop(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	if err := srv.store.CreateAsset(&AssetEntry{ID: "A2", Project: "会议中心", AssetName: "水泵Y", LastStatus: "异常"}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if err := srv.store.CreateEngineeringTask(&EngineeringTask{ID: "t_a2", AssetID: "A2", Status: engTaskStatusRectify, Title: "复查A2"}); err != nil {
		t.Fatalf("CreateEngineeringTask: %v", err)
	}
	mkCR := func(id string) {
		t.Helper()
		err := srv.store.CreateChangeRequest(&ChangeRequest{
			ID: id, TargetType: "asset", TargetID: "A2",
			Patch:  map[string]any{"lastStatus": "正常"},
			Reason: "复核确认恢复正常", Status: "pending", RequestedBy: "巡检员",
			RequestedAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("CreateChangeRequest(%s): %v", id, err)
		}
	}

	// 驳回:数据不动
	mkCR("cr_reject")
	if got := requestJSON(srv, http.MethodPost, "/api/change-requests/cr_reject/reject", tokens["supervisor"], `{"reviewNote":"证据不足"}`); got.Code != http.StatusOK {
		t.Fatalf("reject = %d body=%s", got.Code, got.Body.String())
	}
	if asset, _ := srv.store.GetAsset(defaultTenantID, "A2"); asset.LastStatus != "异常" {
		t.Fatalf("after reject asset status = %q, want 异常", asset.LastStatus)
	}
	if cr, _ := srv.store.GetChangeRequest("cr_reject"); cr.Status != "rejected" {
		t.Fatalf("cr_reject status = %q, want rejected", cr.Status)
	}

	// 通过:patch 应用 + 状态同步 + 销账
	mkCR("cr_ok")
	if got := requestJSON(srv, http.MethodPost, "/api/change-requests/cr_ok/approve", tokens["supervisor"], `{"reviewNote":"同意"}`); got.Code != http.StatusOK {
		t.Fatalf("approve = %d body=%s", got.Code, got.Body.String())
	}
	cr, err := srv.store.GetChangeRequest("cr_ok")
	if err != nil {
		t.Fatalf("GetChangeRequest: %v", err)
	}
	if cr.Status != "approved" || cr.AppliedAt == nil {
		t.Errorf("cr_ok status=%q appliedAt=%v, want approved + non-nil", cr.Status, cr.AppliedAt)
	}
	if asset, _ := srv.store.GetAsset(defaultTenantID, "A2"); asset.LastStatus != "正常" {
		t.Errorf("after approve asset status = %q, want 正常", asset.LastStatus)
	}
	if task, _ := srv.store.GetEngineeringTask("t_a2"); task.Status != engTaskStatusDone {
		t.Errorf("rectify task status = %q, want %q(审批通过销账路径)", task.Status, engTaskStatusDone)
	}

	// 巡检员不能审批(纵深防御回归)
	mkCR("cr_priv")
	if got := requestJSON(srv, http.MethodPost, "/api/change-requests/cr_priv/approve", tokens["inspector"], `{}`); got.Code != http.StatusForbidden {
		t.Errorf("inspector approve = %d, want 403", got.Code)
	}
}

var _ = json.Marshal // 保留 import 以备扩展断言

// fixtureImages 造 n 张占位照片,让记录满足模板的最少张数要求。
func fixtureImages(n int) []ImageInfo {
	out := make([]ImageInfo, 0, n)
	for i := range n {
		out = append(out, ImageInfo{
			ID: "img_" + itoaSafe(i), FileName: "p" + itoaSafe(i) + ".jpg",
			Path: "/tmp/p" + itoaSafe(i) + ".jpg", Size: 1024, CreatedAt: time.Now(),
		})
	}
	return out
}
