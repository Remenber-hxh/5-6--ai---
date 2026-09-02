package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AI 健康状态。
//
// 【这块判错的代价是不对称的】误报"不可用"顶多让人多看一眼;
// 而误报"正常"会让人继续相信一屏兜底文案 —— 这正是它被做出来要解决的
// 那件事(账户欠费,系统页全绿,聊天窗口不吭声,只能靠"答得不太对"去猜)。
// 所以每一条都往"不确定就别说正常"的方向钉。

func health(m map[string]any) map[string]any { return m }

func TestReasonEmptyWhenEverythingFine(t *testing.T) {
	r := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true, "accountError": nil,
	}))
	if r != "" {
		t.Errorf("一切正常时不该给理由,实际 %q", r)
	}
}

// 【最要紧的一条】余额耗尽:key 配着、服务通着,但每一句回答都是兜底。
// 这一条不说出来,界面就和出问题之前一模一样。
func TestReasonCallsOutExhaustedBalance(t *testing.T) {
	r := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true,
		"chatError": map[string]any{"code": "InsufficientBalance"},
	}))
	if r == "" {
		t.Fatal("余额耗尽必须给出理由 —— key 都在,不说的话看不出任何异常")
	}
	if !strings.Contains(r, "余额") && !strings.Contains(r, "额度") {
		t.Errorf("理由要点明是余额/额度问题,实际 %q", r)
	}
	// 【不能把上游英文原文摆到界面上】对着甲方演示时是减分项
	if strings.Contains(strings.ToLower(r), "insufficient") {
		t.Errorf("不该把上游英文错误码直接抛给用户: %q", r)
	}
}

// 额度用尽(免费额度耗尽)和余额不足是同一类,都要报出来。
func TestReasonCoversQuotaExhausted(t *testing.T) {
	r := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true,
		"accountError": map[string]any{"code": "AllocationQuota"},
	}))
	if r == "" {
		t.Error("额度耗尽同样必须报出来")
	}
}

// 两条链路用的是不同账户 —— 缺哪个要说清哪个,
// 笼统说一句"AI 异常"会把"拍照识别还能用"这条关键信息抹掉。
func TestReasonDistinguishesVisionFromChat(t *testing.T) {
	visionOnly := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": false,
	}))
	if !strings.Contains(visionOnly, "问答") {
		t.Errorf("只缺问答密钥时要点明是问答,实际 %q", visionOnly)
	}
	chatOnly := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": false, "hasDeepSeekKey": true,
	}))
	if !strings.Contains(chatOnly, "识别") {
		t.Errorf("只缺视觉密钥时要点明是识别,实际 %q", chatOnly)
	}
	if visionOnly == chatOnly {
		t.Error("两种缺失给出同一句话 —— 等于没说")
	}
}

// 未知的账户故障码也要报,不能因为不认识就当作没事。
func TestReasonReportsUnknownAccountError(t *testing.T) {
	r := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true,
		"chatError": map[string]any{"code": "some_new_code"},
	}))
	if r == "" {
		t.Error("不认识的账户故障码也必须报 —— 沉默等于说「正常」")
	}
}

// 【两条链路必须互不牵连】视觉走 DashScope、问答走 DeepSeek,是两个账户。
//
// 出过事的那次正是 DeepSeek 欠费:如果把它判成"AI 全挂了",现场会以为
// 拍照识别也用不了 —— 而那时候识别其实是好的。反过来更糟:如果 DeepSeek
// 的故障根本不被记录(最初就是这样),问答全是兜底文案而界面一片绿。
func TestChannelsDoNotDragEachOtherDown(t *testing.T) {
	// 只有问答欠费
	r := aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true,
		"chatError": map[string]any{"code": "InsufficientBalance"},
	}))
	if !strings.Contains(r, "问答") {
		t.Errorf("只有问答挂了要点明是问答,实际 %q", r)
	}
	if strings.Contains(r, "拍照只能人工") {
		t.Errorf("问答欠费不该说识别也不能用: %q", r)
	}

	// 只有视觉欠费
	r = aiHealthReason(health(map[string]any{
		"hasDashscopeKey": true, "hasDeepSeekKey": true,
		"accountError": map[string]any{"code": "Arrearage"},
	}))
	if !strings.Contains(r, "识别") {
		t.Errorf("只有识别挂了要点明是识别,实际 %q", r)
	}
	if strings.Contains(r, "台账摘要") {
		t.Errorf("识别欠费不该说问答也不能用: %q", r)
	}
}

// ===== 整条链路 =====

// 用一个假 ai-service 复现真实场景:key 都在、服务通着、账户欠费。
// 【这正是出过事的那一次】而当时系统页是全绿的。
func TestAIHealthHandlerOnExhaustedAccount(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok","hasDashscopeKey":true,"hasDeepSeekKey":true,
			"accountError":null,"chatError":{"code":"InsufficientBalance"}}`))
	}))
	defer fake.Close()

	server, tokens := newRecordAccessTestServer(t)
	server.aiClient = NewAIClient(fake.URL)

	got := requestWithToken(server, http.MethodGet, "/api/system/ai-health", tokens["admin"])
	if got.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", got.Code, got.Body.String())
	}
	var out aiHealthResp
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Reachable {
		t.Error("服务通着,reachable 应为 true")
	}
	if out.Chat {
		t.Error("问答账户欠费时 chat 必须判为不可用 —— 这正是出过事的那一次")
	}
	if !out.Vision {
		t.Error("问答欠费不该连累视觉:那是另一个账户,拍照识别当时是好的")
	}
	if out.Reason == "" {
		t.Error("必须给出理由 —— 不给的话界面和出问题之前一模一样")
	}
}

// ai-service 连不上:不能因为拿不到状态就当作正常。
func TestAIHealthHandlerWhenUnreachable(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	// 指向一个不会有人监听的地址
	server.aiClient = NewAIClient("http://127.0.0.1:1")

	got := requestWithToken(server, http.MethodGet, "/api/system/ai-health", tokens["admin"])
	var out aiHealthResp
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Reachable || out.Vision || out.Chat {
		t.Error("连不上时不能报成可用")
	}
	if out.Reason == "" {
		t.Error("连不上要说出来")
	}
}

// 一切正常时要真的报正常 —— 恒黄和恒绿一样没用。
func TestAIHealthHandlerWhenHealthy(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","hasDashscopeKey":true,"hasDeepSeekKey":true,"accountError":null,"chatError":null}`))
	}))
	defer fake.Close()

	server, tokens := newRecordAccessTestServer(t)
	server.aiClient = NewAIClient(fake.URL)

	got := requestWithToken(server, http.MethodGet, "/api/system/ai-health", tokens["admin"])
	var out aiHealthResp
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Reachable || !out.Vision || !out.Chat || out.Reason != "" {
		t.Errorf("一切正常时应全绿且无理由,实际 %+v", out)
	}
}

// 巡检员看不到 —— 密钥配置/账户状态属于运营信息。
func TestAIHealthRequiresManagementRole(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	// 【必须用真实存在的巡检员账号】用 tokens["inspector"](不存在的键)
	// 拿到的是空串,那测的是"没登录",不是"登录了但角色不够" ——
	// 接口就算对所有登录用户敞开,这条也照样绿。
	tok := tokens["inspector_a"]
	if tok == "" {
		t.Fatal("测试骨架里没有 inspector_a,这条断言就失去意义了")
	}
	got := requestWithToken(server, http.MethodGet, "/api/system/ai-health", tok)
	if got.Code == http.StatusOK {
		t.Errorf("巡检员不该看得到 AI 运营状态,实际 code=%d", got.Code)
	}
}
