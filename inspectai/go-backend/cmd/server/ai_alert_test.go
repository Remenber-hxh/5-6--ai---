package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDecideAIAlert(t *testing.T) {
	day1 := time.Date(2026, 10, 10, 9, 0, 0, 0, cnLoc)
	st, act := decideAIAlert(aiChannelState{}, "Arrearage", 1, day1)
	if act != "alert" || st.Code != "Arrearage" {
		t.Fatalf("新故障应推一条:%s %+v", act, st)
	}
	if _, act := decideAIAlert(st, "Arrearage", 1, day1.Add(time.Hour)); act != "" {
		t.Errorf("同一天不该再推:%s", act)
	}
	st2, act := decideAIAlert(st, "Arrearage", 1, day1.Add(24*time.Hour))
	if act != "remind" {
		t.Errorf("第二天还没好应提醒一次:%s", act)
	}
	if _, act := decideAIAlert(st2, "", 1, day1.Add(25*time.Hour)); act != "recover" {
		t.Errorf("恢复了应推一条:%s", act)
	}
	if _, act := decideAIAlert(aiChannelState{}, "", 1, day1); act != "" {
		t.Errorf("一直正常不该推:%s", act)
	}
	// 服务连不上要连着三次才算 —— 部署重启时会断一小会儿
	s1, a1 := decideAIAlert(aiChannelState{}, "unreachable", 3, day1)
	s2, a2 := decideAIAlert(s1, "unreachable", 3, day1)
	_, a3 := decideAIAlert(s2, "unreachable", 3, day1)
	if a1 != "" || a2 != "" || a3 != "alert" {
		t.Errorf("连不上第三次才推,得到 %q %q %q", a1, a2, a3)
	}
	if st, act := decideAIAlert(s2, "", 3, day1); act != "" || st != (aiChannelState{}) {
		t.Errorf("没攒够次数就恢复了,不推、清零:%s %+v", act, st)
	}
}

// 整条链路:AI 服务报欠费 → 选了提醒的群收到一条;没选的群收不到;第二天提醒一次;恢复后再推一条。
func TestAIAlertLoopEndToEnd(t *testing.T) {
	var mu sync.Mutex
	health := map[string]any{"ok": true, "hasDashscopeKey": true, "hasDeepSeekKey": true}
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewEncoder(w).Encode(health)
	}))
	t.Cleanup(ai.Close)
	got := map[string][]string{}
	botServer := func(name string) *httptest.Server {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Markdown struct {
					Content string `json:"content"`
				} `json:"markdown"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			got[name] = append(got[name], body.Markdown.Content)
			mu.Unlock()
			_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		}))
		t.Cleanup(ts.Close)
		return ts
	}
	store := NewMemStore()
	s := &Server{store: store, aiClient: NewAIClient(ai.URL), weworkBots: []weworkBotTarget{
		{Index: 1, Name: "管理群", Client: NewWeWorkBotClient(botServer("管理群").URL)},
		{Index: 2, Name: "巡检群", Client: NewWeWorkBotClient(botServer("巡检群").URL)},
	}}
	if err := store.SetAppSettings(map[string]string{keyAIAlertBots: "1"}, "test"); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 10, 9, 0, 0, 0, cnLoc)
	setVision := func(err any) { mu.Lock(); health["accountError"] = err; mu.Unlock() }

	s.runAIAlertOnce(t0)
	setVision(map[string]any{"code": "Arrearage"})
	s.runAIAlertOnce(t0.Add(2 * time.Minute))
	s.runAIAlertOnce(t0.Add(4 * time.Minute))
	s.runAIAlertOnce(t0.Add(24 * time.Hour))
	setVision(nil)
	s.runAIAlertOnce(t0.Add(26 * time.Hour))

	msgs := got["管理群"]
	if len(msgs) != 3 {
		t.Fatalf("管理群应收到 故障、仍未恢复、已恢复 三条,收到 %d 条:%v", len(msgs), msgs)
	}
	for i, want := range []string{"智巡 AI 故障", "仍未恢复", "已恢复"} {
		if !strings.Contains(msgs[i], want) || !strings.Contains(msgs[i], "拍照识别") {
			t.Errorf("第 %d 条应是「%s」并写明拍照识别:%s", i+1, want, msgs[i])
		}
	}
	if !strings.Contains(msgs[0], "额度已用尽") {
		t.Errorf("要写明是什么问题:%s", msgs[0])
	}
	if len(got["巡检群"]) != 0 {
		t.Errorf("没打开 AI 故障提醒的群不该收到:%v", got["巡检群"])
	}
}

// 推送设置里"AI 故障提醒"开关:存得下、读得回;老版本后台不带这个字段时不能把它清掉。
func TestAIAlertSwitchSavedPerBot(t *testing.T) {
	s, tok := newPushConfigServer(t)
	base := `"enabled":true,"time":"17:00","weekdays":"1,2,3,4,5","silentWhenDone":false,"followCalendar":false`
	if w := putPushConfig(t, s, tok, `{`+base+`,"bots":[{"index":2,"aiAlerts":true}]}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	alerts := func() map[float64]bool {
		req := httptest.NewRequest(http.MethodGet, pushCfgPath, nil)
		req.Header.Set("X-InspectAI-Token", tok)
		w := httptest.NewRecorder()
		s.router(w, req)
		var v struct {
			Bots []map[string]any `json:"bots"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		out := map[float64]bool{}
		for _, b := range v.Bots {
			on, _ := b["aiAlerts"].(bool)
			out[b["index"].(float64)] = on
		}
		return out
	}
	if got := alerts(); got[1] || !got[2] {
		t.Fatalf("应只有第 2 个群打开:%v", got)
	}
	// 老版本后台:bots 里没有 aiAlerts 字段 → 不动
	if w := putPushConfig(t, s, tok, `{`+base+`,"bots":[{"index":2}]}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	if got := alerts(); !got[2] {
		t.Errorf("没带 aiAlerts 的请求把开关清掉了:%v", got)
	}
	if w := putPushConfig(t, s, tok, `{`+base+`,"bots":[{"index":2,"aiAlerts":false}]}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	if got := alerts(); got[2] {
		t.Errorf("关掉之后应读回 false:%v", got)
	}
}
