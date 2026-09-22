package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ===== 「给项目选群」那个下拉的数据源 =====
//
// 【这条测试的重点是它不返回什么】webhook 等价于往那个群发消息的权限。
// 这个接口是给浏览器的:返回值会进控制台、进截图、进任何一次"帮我看看"的粘贴。
// 地址只存在服务器的 secrets 里,后台只该知道"第几个群、叫什么、配没配好"。

func TestWeWorkBotsNeverLeakWebhook(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	const secret = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SECRET-KEY-DO-NOT-LEAK"
	s.weworkBots = []weworkBotTarget{
		{Index: 1, Name: "机器人1(会议中心)", Projects: []string{"会议中心"},
			Client: NewWeWorkBotClient(secret)},
		{Index: 2, Name: "机器人2(紫菡雅集)", Projects: []string{"紫菡雅集"},
			Client: NewWeWorkBotClient("")}, // 地址没配好
	}

	req := httptest.NewRequest(http.MethodGet, "/api/wework/bots", nil)
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	s.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("取群列表失败 status=%d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// 【最要紧的一条】整段响应里不许出现地址的任何一部分
	for _, leak := range []string{secret, "SECRET-KEY-DO-NOT-LEAK", "qyapi.weixin.qq.com", "webhook"} {
		if strings.Contains(body, leak) {
			t.Fatalf("响应里泄露了 webhook(%q):\n%s", leak, body)
		}
	}

	var out struct {
		Bots []struct {
			Index       int      `json:"index"`
			Name        string   `json:"name"`
			EnvProjects []string `json:"envProjects"`
			Ready       bool     `json:"ready"`
		} `json:"bots"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(out.Bots) != 2 {
		t.Fatalf("应该有 2 个群,实得 %d", len(out.Bots))
	}
	if !out.Bots[0].Ready {
		t.Error("配了地址的群报成未就绪")
	}
	// 地址没配好的要标出来 —— 让人选之前就知道选了也发不出去
	if out.Bots[1].Ready {
		t.Error("没配地址的群报成已就绪,选了它提醒会静默丢失")
	}
	if len(out.Bots[1].EnvProjects) != 1 || out.Bots[1].EnvProjects[0] != "紫菡雅集" {
		t.Errorf("没给出服务器上配的项目,前端无法显示「跟随服务器配置」发哪个群:%v",
			out.Bots[1].EnvProjects)
	}
}

// 随手填一个不存在的群序号要当场拦住 —— 存进去的表现是"提醒静默不发",
// 而后台显示保存成功。
func TestSetProjectBotRejectsUnknownIndex(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	s.weworkBots = []weworkBotTarget{
		{Index: 1, Name: "机器人1", Client: NewWeWorkBotClient("https://example.invalid/a")},
	}
	p := &Project{ID: "p_zihan", TenantID: defaultTenantID, Name: "紫菡雅集"}
	if err := s.store.CreateProject(p); err != nil {
		t.Fatal(err)
	}

	put := func(body string) int {
		req := httptest.NewRequest(http.MethodPut, "/api/projects/"+p.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-InspectAI-Token", tok)
		rec := httptest.NewRecorder()
		s.router(rec, req)
		return rec.Code
	}

	if got := put(`{"disabled":false,"botIndex":9}`); got != http.StatusBadRequest {
		t.Errorf("选了不存在的第 9 个群却存下了,status=%d", got)
	}
	// 0 是合法的:交回环境变量决定
	if got := put(`{"disabled":false,"botIndex":0}`); got != http.StatusOK {
		t.Errorf("「跟随服务器配置」被拒了,status=%d", got)
	}
	if got := put(`{"disabled":false,"botIndex":1}`); got != http.StatusOK {
		t.Errorf("选真实存在的群被拒了,status=%d", got)
	}

	list, err := s.store.ListProjects(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range list {
		if x.Name == "紫菡雅集" && x.BotIndex != 1 {
			t.Errorf("选的群没存上:%d", x.BotIndex)
		}
	}
}

// 【改备注不能把已经选好的群清掉】botIndex 不传 = 这次不改它。
func TestUpdateWithoutBotIndexKeepsChoice(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	s.weworkBots = []weworkBotTarget{
		{Index: 2, Name: "机器人2", Client: NewWeWorkBotClient("https://example.invalid/b")},
	}
	p := &Project{ID: "p_zihan", TenantID: defaultTenantID, Name: "紫菡雅集"}
	if err := s.store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := s.store.SetProjectBotIndex(defaultTenantID, p.ID, 2); err != nil {
		t.Fatal(err)
	}

	// 老前端/改备注:请求里没有 botIndex
	req := httptest.NewRequest(http.MethodPut, "/api/projects/"+p.ID,
		strings.NewReader(`{"note":"改个备注","disabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	s.router(httptest.NewRecorder(), req)

	list, _ := s.store.ListProjects(defaultTenantID)
	for _, x := range list {
		if x.Name == "紫菡雅集" && x.BotIndex != 2 {
			t.Errorf("改备注把选好的群清掉了:%d", x.BotIndex)
		}
	}
}
