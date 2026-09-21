package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// ===== 后台那个"分项目设置"接口 =====
//
// 合并逻辑本身在 push_bot_config_test.go 里测过了。这里测的是另一半:
// 后台页面存下去的东西,读回来是不是同一份 —— 中间隔着 JSON、
// 指针和"这一项没传"这三件最容易出岔子的事。

func newPushConfigServer(t *testing.T) (*Server, string) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "push_cfg.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := &Server{store: store, storeKind: "sqlite", storageDir: t.TempDir(),
		frontendDir: t.TempDir(), corsAllowedOrigins: map[string]bool{},
		// 线上的形状:一个群收会议中心,一个群收紫菡雅集。
		// 地址用 example.invalid —— 这些测试不发消息,也不该有发得出去的可能。
		weworkBots: []weworkBotTarget{
			{Index: 1, Name: "机器人1(会议中心)", Projects: []string{"会议中心"},
				Client: NewWeWorkBotClient("https://example.invalid/a")},
			{Index: 2, Name: "机器人2(紫菡雅集)", Projects: []string{"紫菡雅集"},
				Client: NewWeWorkBotClient("https://example.invalid/b")},
		},
	}
	if err := s.loadPermissions(); err != nil {
		t.Fatalf("loadPermissions: %v", err)
	}
	// 【库里要有真项目】页面显示的项目名是拿配置去库里核出来的,
	// 不是照抄配置 —— 没有项目的话每个群都会显示成"配错了"。
	for _, name := range []string{"会议中心", "紫菡雅集"} {
		if err := store.CreateProject(&Project{
			ID: "p_" + name, TenantID: defaultTenantID, Name: name,
		}); err != nil {
			t.Fatalf("CreateProject %s: %v", name, err)
		}
	}
	admin := &User{ID: "user_admin", Username: "admin", DisplayName: "系统管理员", RoleCode: roleAdmin}
	if err := store.CreateUser(admin, "test-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, session, err := store.AuthenticateUser("admin", "test-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	return s, session.Token
}

const pushCfgPath = "/api/engineering/plans/daily-push/config"

func putPushConfig(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, pushCfgPath, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", token)
	rec := httptest.NewRecorder()
	s.router(rec, req)
	return rec
}

type pushCfgView struct {
	Enabled bool `json:"enabled"`
	Bots    []struct {
		Index           int      `json:"index"`
		Projects        []string `json:"projects"`
		UnknownProjects []string `json:"unknownProjects"`
		Ready           bool     `json:"ready"`
		Follows         bool     `json:"follows"`
		Override        struct {
			Enabled  *bool   `json:"enabled"`
			Time     *string `json:"time"`
			Weekdays *string `json:"weekdays"`
		} `json:"override"`
		Effective struct {
			Enabled  bool   `json:"enabled"`
			Time     string `json:"time"`
			Weekdays string `json:"weekdays"`
		} `json:"effective"`
	} `json:"bots"`
}

func getPushConfig(t *testing.T, s *Server, token string) pushCfgView {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, pushCfgPath, nil)
	req.Header.Set("X-InspectAI-Token", token)
	rec := httptest.NewRecorder()
	s.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("读设置失败 status=%d %s", rec.Code, rec.Body.String())
	}
	var out pushCfgView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析返回失败: %v\n%s", err, rec.Body.String())
	}
	return out
}

// 一次都没设过的时候:两个群都跟随全局,实际时间就是全局那个。
func TestPushConfigListsBotsFollowingGlobal(t *testing.T) {
	s, tok := newPushConfigServer(t)
	got := getPushConfig(t, s, tok)
	if len(got.Bots) != 2 {
		t.Fatalf("应该有 2 个群,实得 %d", len(got.Bots))
	}
	for _, b := range got.Bots {
		if !b.Follows {
			t.Errorf("第 %d 个群没设过却不是「跟随全局」", b.Index)
		}
		if b.Effective.Time != "17:00" {
			t.Errorf("第 %d 个群的实际时间不是全局的 17:00,而是 %q", b.Index, b.Effective.Time)
		}
		if !b.Ready {
			t.Errorf("第 %d 个群配了地址却报未就绪", b.Index)
		}
	}
	if got.Bots[1].Projects[0] != "紫菡雅集" {
		t.Errorf("项目名对不上:%v", got.Bots[1].Projects)
	}
}

// 【配置里的项目名库里没有 → 必须喊出来】这是个安静的故障:
// 那个群照常"按时发送成功",只是内容里一台设备都没有。
// 2026-09-20 本地就是这么坏的(读 .env 的编码把项目名变成了乱码)。
func TestPushConfigFlagsProjectsMissingFromDB(t *testing.T) {
	s, tok := newPushConfigServer(t)
	// 模拟配错/编码坏掉:配的是一个库里不存在的名字
	s.weworkBots[1].Projects = []string{"绱彙闆呴泦"}

	got := getPushConfig(t, s, tok)
	bad := got.Bots[1]
	if len(bad.Projects) != 0 {
		t.Errorf("库里没有的项目不该当成真项目显示:%v", bad.Projects)
	}
	if len(bad.UnknownProjects) != 1 || bad.UnknownProjects[0] != "绱彙闆呴泦" {
		t.Errorf("对不上的项目名没被拎出来:%v", bad.UnknownProjects)
	}
	// 另一个群不受影响 —— 一个配错了不该让整页都不可信
	if len(got.Bots[0].Projects) != 1 || got.Bots[0].Projects[0] != "会议中心" {
		t.Errorf("好的那个群被带坏了:%+v", got.Bots[0])
	}
}

// 显示的名字来自库,不是照抄配置 —— 所以库里改了项目名,页面跟着变。
func TestPushConfigProjectNamesComeFromDB(t *testing.T) {
	s, tok := newPushConfigServer(t)
	if got := getPushConfig(t, s, tok); got.Bots[0].Projects[0] != "会议中心" {
		t.Fatalf("起点就不对:%v", got.Bots[0].Projects)
	}
	// 把库里那个项目删掉:配置一个字没动,页面就该说"这个名字现在不存在"
	list, err := s.store.ListProjects(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.Name == "会议中心" {
			if err := s.store.DeleteProject(defaultTenantID, p.ID); err != nil {
				t.Fatalf("DeleteProject: %v", err)
			}
		}
	}
	got := getPushConfig(t, s, tok)
	if len(got.Bots[0].Projects) != 0 || len(got.Bots[0].UnknownProjects) != 1 {
		t.Errorf("项目从库里没了,页面还在照抄配置:%+v", got.Bots[0])
	}
}

// 【这次要的东西】只给紫菡那个群改成 18:30,会议中心不受影响。
func TestPushConfigSavesPerBotTime(t *testing.T) {
	s, tok := newPushConfigServer(t)
	rec := putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"time":"18:30"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败 status=%d %s", rec.Code, rec.Body.String())
	}
	got := getPushConfig(t, s, tok)
	if got.Bots[0].Effective.Time != "17:00" || !got.Bots[0].Follows {
		t.Errorf("没动的那个群被带偏了:%+v", got.Bots[0])
	}
	if got.Bots[1].Effective.Time != "18:30" || got.Bots[1].Follows {
		t.Errorf("改了时间的群没存上:%+v", got.Bots[1])
	}
	if got.Bots[1].Override.Time == nil || *got.Bots[1].Override.Time != "18:30" {
		t.Errorf("覆盖值读回来不是 18:30:%v", got.Bots[1].Override.Time)
	}
}

// 【老后台的请求里没有 bots 这个字段】升级那天不能把已有的单独设置静默抹掉。
func TestPushConfigWithoutBotsKeepsOverrides(t *testing.T) {
	s, tok := newPushConfigServer(t)
	putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"time":"18:30"}]}`)
	// 老版本后台:只发四个全局字段
	rec := putPushConfig(t, s, tok,
		`{"enabled":true,"time":"16:00","weekdays":"1,2,3,4,5","silentWhenDone":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败 status=%d %s", rec.Code, rec.Body.String())
	}
	got := getPushConfig(t, s, tok)
	if got.Bots[1].Effective.Time != "18:30" {
		t.Errorf("没传 bots 却把单独设置抹掉了,现在是 %q", got.Bots[1].Effective.Time)
	}
	if got.Bots[0].Effective.Time != "16:00" {
		t.Errorf("跟随全局的群没跟上新的全局时间:%q", got.Bots[0].Effective.Time)
	}
}

// 【整份替换是有意的】只发 time 的话,这个群原来设的"暂停"会被清掉。
// 前端因此必须把现有覆盖一起发回来 —— 把这条钉在测试里,
// 免得以后有人照着接口猜行为。
func TestPushConfigReplacesWholeOverride(t *testing.T) {
	s, tok := newPushConfigServer(t)
	putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"enabled":false,"time":"18:30"}]}`)
	if got := getPushConfig(t, s, tok); got.Bots[1].Effective.Enabled {
		t.Fatal("设了暂停却还开着")
	}
	putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"time":"19:00"}]}`)
	got := getPushConfig(t, s, tok)
	if !got.Bots[1].Effective.Enabled {
		t.Error("只发 time 之后,暂停应该被一起清掉(接口是整份替换)")
	}
	if got.Bots[1].Effective.Time != "19:00" {
		t.Errorf("新时间没存上:%q", got.Bots[1].Effective.Time)
	}
}

// 把"跟随全局"设回去:四项都发 null。
func TestPushConfigCanFollowGlobalAgain(t *testing.T) {
	s, tok := newPushConfigServer(t)
	putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"time":"18:30"}]}`)
	putPushConfig(t, s, tok, `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5",
		"silentWhenDone":true,"bots":[{"index":2,"enabled":null,"time":null,
		"weekdays":null,"silentWhenDone":null}]}`)
	got := getPushConfig(t, s, tok)
	if !got.Bots[1].Follows || got.Bots[1].Effective.Time != "17:00" {
		t.Errorf("设回跟随全局没生效:%+v", got.Bots[1])
	}
}

// 坏值当场拦住,别存进去再说 —— 存进去的表现是"那个群从此不发了"。
func TestPushConfigRejectsBadBotInput(t *testing.T) {
	s, tok := newPushConfigServer(t)
	base := `{"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5","silentWhenDone":true,`
	for name, body := range map[string]string{
		"时间格式不对": base + `"bots":[{"index":2,"time":"下午六点"}]}`,
		"执行日越界":  base + `"bots":[{"index":2,"weekdays":"1,9"}]}`,
		"没有这个群":  base + `"bots":[{"index":7,"time":"18:30"}]}`,
	} {
		if rec := putPushConfig(t, s, tok, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s:应该被拦下,实得 status=%d %s", name, rec.Code, rec.Body.String())
		}
	}
	// 被拦下之后什么都不该留下
	if got := getPushConfig(t, s, tok); !got.Bots[1].Follows {
		t.Error("请求被拒了,却已经往库里写了一半")
	}
}
