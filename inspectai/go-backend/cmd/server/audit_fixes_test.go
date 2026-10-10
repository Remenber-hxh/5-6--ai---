package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===== 2026-09-29 设计审计的修复 =====
//
// 审计里点名的几处:工具路径没有动作提议、依据是另外猜的、超时两边对不上、
// find_asset 的总数被改成截断后的条数、分类信模型自报、识别被重启打断后卡住、
// AI 接口没有限流、没人用的 /api/ai/chat 还开着。每条一个测试钉住。

// fakeAnalytics 假的 ai-service:按顺序吐出预设的每一轮回复,并记下收到的请求。
type fakeAnalytics struct {
	mu       sync.Mutex
	replies  []map[string]any
	requests []map[string]any
}

func (f *fakeAnalytics) server(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		var out map[string]any
		if len(f.replies) > 0 {
			out = f.replies[0]
			f.replies = f.replies[1:]
		} else {
			out = map[string]any{"finish": "error", "message": "no more replies"}
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func toolCall(id, name, args string) map[string]any {
	return map[string]any{"id": id, "name": name, "arguments": args}
}

func newAgentServer(t *testing.T, fake *fakeAnalytics) (*Server, *http.Request, *MemStore) {
	t.Helper()
	srv, r, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	srv.analyticsClient = NewAnalyticsClient(fake.server(t).URL)
	if err := store.CreateAsset(&AssetEntry{ID: "会议中心::e::K07", TenantID: defaultTenantID,
		Project: "会议中心", AssetKey: "K07", AssetName: "K07", LastStatus: "正常"}); err != nil {
		t.Fatal(err)
	}
	return srv, r, store
}

// 依据只来自模型实际查过的东西 —— 查的是 K07,依据就是 K07。
func TestAgentEvidenceComesFromToolCalls(t *testing.T) {
	fake := &fakeAnalytics{replies: []map[string]any{
		{"finish": "tool_calls", "assistantMessage": map[string]any{"role": "assistant"},
			"toolCalls": []any{toolCall("c1", "get_asset_history", `{"assetId":"会议中心::e::K07"}`)}},
		{"finish": "stop", "reply": "K07 最近一个月都正常。"},
	}}
	srv, r, _ := newAgentServer(t, fake)

	reply, used, evidence, ok := srv.agentChat(r, "sys", "{}", "K07 最近怎么样", "", nil, time.Now().Add(managementChatBudget))
	if !ok || reply == "" {
		t.Fatalf("工具路径应该成功,得到 ok=%v reply=%q", ok, reply)
	}
	if len(used) != 1 || len(evidence) != 1 || evidence[0].AssetID != "会议中心::e::K07" {
		t.Fatalf("应记下查过 K07:used=%v evidence=%+v", used, evidence)
	}
	src := srv.sourcesFromEvidence(defaultTenantID, "K07 最近怎么样", evidence)
	if len(src) == 0 || src[0]["assetId"] != "会议中心::e::K07" {
		t.Fatalf("依据应指向查过的 K07,得到 %v", src)
	}
	// 每一轮都带着时间预算,而且不超过单轮上限
	for i, req := range fake.requests {
		b, _ := req["budgetSeconds"].(float64)
		if b <= 0 || b > agentRoundBudget.Seconds() {
			t.Errorf("第 %d 轮的预算是 %v 秒,应在 (0, %v] 之间", i, b, agentRoundBudget.Seconds())
		}
	}
}

// 剩下的时间不够一轮、又要给老路留余量时,一轮都不开 —— 直接交给老路。
func TestAgentGivesUpWhenTimeIsShort(t *testing.T) {
	fake := &fakeAnalytics{}
	srv, r, _ := newAgentServer(t, fake)
	_, _, _, ok := srv.agentChat(r, "sys", "{}", "问题", "", nil, time.Now().Add(agentFallbackReserve+time.Second))
	if ok {
		t.Fatal("时间不够时应该放弃工具路径")
	}
	if len(fake.requests) != 0 {
		t.Fatalf("时间不够还发了 %d 次请求 —— 老路会被挤得没时间", len(fake.requests))
	}
}

// 一轮要 6 个查询:只执行前 4 个,但 6 个 tool_call_id 都要有回应(协议要求)。
func TestAgentCapsCallsPerRound(t *testing.T) {
	calls := []any{}
	for i := 0; i < 6; i++ {
		calls = append(calls, toolCall(fmt.Sprintf("c%d", i), "get_status_events", `{"assetId":"会议中心::e::K07"}`))
	}
	fake := &fakeAnalytics{replies: []map[string]any{
		{"finish": "tool_calls", "assistantMessage": map[string]any{"role": "assistant"}, "toolCalls": calls},
		{"finish": "stop", "reply": "好的"},
	}}
	srv, r, _ := newAgentServer(t, fake)
	_, used, _, ok := srv.agentChat(r, "sys", "{}", "问题", "", nil, time.Now().Add(managementChatBudget))
	if !ok {
		t.Fatal("应该成功")
	}
	if len(used) != agentMaxCallsPerRound {
		t.Fatalf("应只执行 %d 个查询,执行了 %d 个", agentMaxCallsPerRound, len(used))
	}
	msgs, _ := fake.requests[1]["messages"].([]any)
	toolMsgs := 0
	for _, m := range msgs {
		if mm, _ := m.(map[string]any); mm["role"] == "tool" {
			toolMsgs++
		}
	}
	if toolMsgs != 6 {
		t.Fatalf("6 个调用都要有回应,只回了 %d 个 —— 模型那边会对不上", toolMsgs)
	}
}

// 工具路径是聊天的主路,动作提议的规则必须在它的提示词里,而且要带设备 id。
func TestToolPromptCarriesActionRule(t *testing.T) {
	for _, want := range []string{"<<ACTION>>", "<<END>>", "create_recheck_task", `"assetId"`} {
		if !strings.Contains(MANAGEMENT_CHAT_SYSTEM_HINT, want) {
			t.Errorf("工具路径提示词里没有 %s —— 一键派单在主路上永远不会出现", want)
		}
	}
}

// 受限账号查到 15 台:count 是 15,不是截断后的 10;别的项目的同名设备不算数。
func TestFindAssetCountHonestForScopedUser(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	for i := 1; i <= 15; i++ {
		name := fmt.Sprintf("DT%02d", i)
		_ = store.CreateAsset(&AssetEntry{ID: "会议中心::e::" + name, TenantID: defaultTenantID,
			Project: "会议中心", AssetKey: name, AssetName: name})
	}
	for i := 1; i <= 3; i++ {
		name := fmt.Sprintf("DT%02d", i)
		_ = store.CreateAsset(&AssetEntry{ID: "别的楼::e::" + name, TenantID: defaultTenantID,
			Project: "别的楼", AssetKey: name, AssetName: name})
	}
	vis := dataVisibility{Projects: []string{"会议中心", "紫菡雅集"}}
	found, err := srv.findAssetsForAgent(defaultTenantID, "", "DT", vis.allowsProject)
	if err != nil {
		t.Fatal(err)
	}
	found = limitAgentAssetsToProjects(found, vis)
	if found["count"] != 15 {
		t.Fatalf("会议中心有 15 台,count 得到 %v(别的楼那 3 台不该算,截断也不该改掉总数)", found["count"])
	}
	if list, _ := found["assets"].([]map[string]any); len(list) != 10 {
		t.Fatalf("列表应截成 10 台,得到 %d", len(list))
	}
}

// 回答没点名任何设备时,不再挂"风险最高的那台"当依据 —— 那是猜的。
func TestChatSourcesDoNotGuessADevice(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	att := []*AttentionItem{{AssetID: "a1", AssetName: "HYZX-WJ-DT01", LastRecordID: "r1"}}
	src := srv.buildChatSources("最近哪些设备要重点关注", "整体平稳,没有需要特别处理的。", att)
	for _, x := range src {
		if x["type"] == "asset" || x["type"] == "record" {
			t.Fatalf("回答没点名设备,却挂了设备依据:%v", src)
		}
	}
}

// 分类:模型说"不用人工选",但只给 0.4 的把握 —— 还是要人选。
func TestSceneLowConfidenceNeedsManualPick(t *testing.T) {
	candidates := []SceneCandidate{{TemplateID: "zihan_energy", TemplateName: "能耗抄表"}}
	r := &SceneClassifyResult{TemplateID: "zihan_energy", Confidence: 0.4, NeedsManualPick: false}
	resolveSceneResult(r, candidates)
	if !r.NeedsManualPick {
		t.Fatal("置信度 0.4 仍然自动选了模板")
	}
	sure := &SceneClassifyResult{TemplateID: "zihan_energy", Confidence: 0.9}
	resolveSceneResult(sure, candidates)
	if sure.NeedsManualPick {
		t.Fatal("有把握的结果不该被强制人工选")
	}
}

func checkInterruptedReset(t *testing.T, store Store) {
	t.Helper()
	mk := func(id, status string, submitted bool) {
		if err := store.CreateRecord(&Record{ID: id, TenantID: defaultTenantID, TemplateID: "zihan_energy",
			RecognitionStatus: status, Submitted: submitted}); err != nil {
			t.Fatal(err)
		}
	}
	mk("r_proc", "processing", false)
	mk("r_queue", "queued", false)
	mk("r_ok", "recognized", false)
	mk("r_sub", "processing", true) // 已提交的不碰
	_ = store.CreateTask(&AITask{ID: "t1", RecordID: "r_proc", Status: "processing", CreatedAt: time.Now(), UpdatedAt: time.Now()})

	n, err := store.ResetInterruptedRecognitions(interruptedRecognitionReason)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("应收尾 2 条(识别中 + 排队中),得到 %d", n)
	}
	for id, want := range map[string]string{"r_proc": "retake_required", "r_queue": "retake_required", "r_ok": "recognized", "r_sub": "processing"} {
		rec, err := store.GetRecord(defaultTenantID, id)
		if err != nil {
			t.Fatal(err)
		}
		if rec.RecognitionStatus != want {
			t.Errorf("%s 应为 %s,得到 %s", id, want, rec.RecognitionStatus)
		}
	}
	task, _ := store.GetTask("t1")
	if task == nil || task.Status != "failed" || task.ErrorCode != "interrupted" {
		t.Fatalf("被打断的识别任务应标成失败:%+v", task)
	}
}

func TestResetInterruptedRecognitionsMem(t *testing.T) {
	checkInterruptedReset(t, NewMemStore())
}

func TestResetInterruptedRecognitionsSQLite(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "recover.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	checkInterruptedReset(t, store)
}

func TestAIRateLimiter(t *testing.T) {
	var l aiRateLimiter
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !l.allow("chat|u1", 3, time.Minute, now) {
			t.Fatalf("第 %d 次不该被拦", i+1)
		}
	}
	if l.allow("chat|u1", 3, time.Minute, now) {
		t.Fatal("超过上限没被拦")
	}
	if !l.allow("chat|u2", 3, time.Minute, now) {
		t.Fatal("一个人刷不该连累别人")
	}
	if !l.allow("chat|u1", 3, time.Minute, now.Add(61*time.Second)) {
		t.Fatal("过了一分钟应该恢复")
	}
}

// 限流接到了真实接口上:第 21 次问答直接 429,不调模型。
func TestManagementChatIsRateLimited(t *testing.T) {
	fake := &fakeAnalytics{}
	srv, r, _ := newAgentServer(t, fake)
	for i := 0; i < aiLimitManagementChat; i++ {
		if !srv.allowAICall(httptest.NewRecorder(), r, "mgmt-chat", aiLimitManagementChat) {
			t.Fatalf("第 %d 次不该被拦", i+1)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/api/management-ai/chat", strings.NewReader(`{"message":"你好"}`))
	req.Header = r.Header.Clone()
	w := httptest.NewRecorder()
	srv.handleManagementChat(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限应返回 429,得到 %d", w.Code)
	}
	if len(fake.requests) != 0 {
		t.Fatal("被限流的请求不该再去调模型")
	}
}

// 没人用、不按项目裁数据、谁都能调的 /api/ai/chat 已经拿掉。
func TestOrphanAIChatRouteRemoved(t *testing.T) {
	for _, rt := range apiRoutes {
		if rt.path == "/api/ai/chat" {
			t.Fatal("/api/ai/chat 还在路由表里")
		}
	}
}

// 管理聊天两条路的共用规矩只有一份:主路的系统提示里有它,主路失败走备用路时也随请求发过去。
// 原来两边各写一份,一键派单的规则只加进了其中一份,走另一条路时派单按钮就没了。
func TestManagementChatRulesLiveInOnePlace(t *testing.T) {
	for _, want := range []string{"数字不能编", "<<ACTION>>", "create_recheck_task", "50-120 字", "风险等级翻成中文"} {
		if !strings.Contains(managementChatSharedRules, want) {
			t.Errorf("共用规矩里少了「%s」", want)
		}
	}
	if !strings.HasSuffix(MANAGEMENT_CHAT_SYSTEM_HINT, managementChatSharedRules) {
		t.Error("主路的系统提示没有带上共用规矩")
	}

	// 主路一轮就失败 → 走备用路
	fake := &fakeAnalytics{replies: []map[string]any{
		{"finish": "error", "message": "boom"},
		{"reply": "好的", "model": "deepseek-chat"},
	}}
	srv, r, _ := newAgentServer(t, fake)
	req := httptest.NewRequest(http.MethodPost, "/api/management-ai/chat", strings.NewReader(`{"message":"今天重点关注什么"}`))
	req.Header = r.Header.Clone()
	w := httptest.NewRecorder()
	srv.handleManagementChat(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("聊天失败:%d %s", w.Code, w.Body.String())
	}
	var toolSys, fallbackRules string
	for _, body := range fake.requests {
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if m, _ := msgs[0].(map[string]any); m["role"] == "system" {
				toolSys, _ = m["content"].(string)
			}
		}
		if rules, ok := body["sharedRules"].(string); ok {
			fallbackRules = rules
		}
	}
	if !strings.Contains(toolSys, managementChatSharedRules) {
		t.Error("主路请求的系统提示里没有共用规矩")
	}
	if fallbackRules != managementChatSharedRules {
		t.Errorf("备用路请求没带上同一份共用规矩(收到 %d 字)", len([]rune(fallbackRules)))
	}
}
