package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===== AI 账号出故障时推企业微信 =====
//
// 【为什么要推】拍照识别用的阿里云账户开着"只用免费额度",额度用完那一刻,
// 全员拍照识别一起停;管理问答用的 DeepSeek 欠费时,每一句回答都退化成台账摘要。
// 系统页的 AI 状态卡能看出来(ai_health.go),但得有人正好去看 ——
// 实际情况是巡检员一个个撞上"请手动填写",谁也不知道是整体停了。
//
// 【怎么推】每两分钟问一次 AI 服务的 /health(和系统页同一个口径):
//   - 出了故障:推一条,写明哪个功能停了、为什么、去哪处理;
//   - 一直没好:每天提醒一次,不刷屏;
//   - 恢复了:推一条"已恢复",写明停了多久。
// AI 服务整个连不上也算一种故障,但连着三次(约 6 分钟)才算 —— 部署重启时会断一小会儿。
//
// 【推到哪个群由人选】后台推送设置里每个群有一个"AI 故障提醒"开关,默认全关:
// 现在一个项目一个群,群里多是巡检员,不该默认去打扰。一个都没开就只记日志。

const (
	keyAIAlertBots  = "ai_alert.bots"  // 收 AI 故障提醒的群:"1,2"
	keyAIAlertState = "ai_alert.state" // 各功能当前的故障状态(JSON),重启后接着用
	aiAlertInterval = 2 * time.Minute
	// AI 服务连不上要连着几次才算故障:部署重启时会断一两分钟
	aiAlertUnreachableTimes = 3
)

// aiAlertChannel 一个能单独停掉的功能。
type aiAlertChannel struct {
	Key    string // 存状态用
	Name   string // 给人看
	Impact string // 停了之后现场会怎样
	Fix    string // 去哪处理
}

var aiAlertChannels = []aiAlertChannel{
	{Key: "vision", Name: "拍照识别", Impact: "现场拍照后只能人工填写",
		Fix: "到阿里云百炼控制台查看余额、免费额度和密钥"},
	{Key: "chat", Name: "管理问答", Impact: "后台 AI 问答只能给台账摘要",
		Fix: "到 DeepSeek 控制台查看余额和密钥"},
	{Key: "service", Name: "AI 服务", Impact: "拍照识别和管理问答都不可用",
		Fix: "到服务器上看 ai-service 容器是否在运行"},
}

// aiChannelState 一个功能的故障状态。Code 为空 = 正常。
type aiChannelState struct {
	Code      string `json:"code,omitempty"`
	Since     string `json:"since,omitempty"`     // 这次故障从什么时候开始(RFC3339)
	PushedDay string `json:"pushedDay,omitempty"` // 最近一次推送是哪天(东八区),一天只推一次
	Misses    int    `json:"misses,omitempty"`    // 还没算成故障的连续失败次数(只给 service 用)
}

// decideAIAlert 这一次检查之后,这个功能的状态怎么变、要不要推。
//
// code 是这次查到的故障码,空 = 正常;threshold 是连着几次才算故障(1 = 一次就算)。
// action:alert 新故障 / remind 还没好、今天还没提醒过 / recover 恢复了 / 空 = 不用推。
func decideAIAlert(prev aiChannelState, code string, threshold int, now time.Time) (aiChannelState, string) {
	today := dayStamp(now)
	if code == "" {
		if prev.Code == "" {
			return aiChannelState{}, "" // 一直正常;没攒够次数的失败也清零
		}
		if prev.PushedDay == "" {
			return aiChannelState{}, "" // 故障期间一条都没推出去过,恢复也不用说
		}
		return aiChannelState{}, "recover"
	}
	if prev.Code == "" {
		misses := prev.Misses + 1
		if misses < threshold {
			return aiChannelState{Misses: misses}, ""
		}
		return aiChannelState{Code: code, Since: now.Format(time.RFC3339), PushedDay: today}, "alert"
	}
	next := prev
	next.Code = code // 故障原因可能变了(先限流后欠费),以最新的为准
	if prev.PushedDay != today {
		next.PushedDay = today
		return next, "remind"
	}
	return next, ""
}

// aiAlertCodes 从 /health 的结果里取出三个功能各自的故障码。
// 服务连不上时只有 service 一项 —— 问不到账户状态,另外两项保持原样,不能当成"恢复了"。
func aiAlertCodes(h map[string]any, reachable bool) map[string]string {
	if !reachable {
		return map[string]string{"service": "unreachable"}
	}
	return map[string]string{
		"service": "",
		"vision":  errCode(h, "accountError"),
		"chat":    errCode(h, "chatError"),
	}
}

func aiAlertCard(ch aiAlertChannel, action, code string, since, now time.Time) string {
	why := codeToText(code)
	if code == "unreachable" {
		why = "连不上"
	}
	switch action {
	case "recover":
		return buildNotifyCard("智巡 AI 已恢复",
			cardRow("功能", cardStrong(ch.Name)),
			cardRow("中断", aiAlertDuration(now.Sub(since))),
		)
	case "remind":
		return buildNotifyCard("智巡 AI 故障仍未恢复",
			cardRow("功能", cardStrong(ch.Name)),
			cardRow("问题", cardWarn(why)),
			cardRow("已持续", aiAlertDuration(now.Sub(since))),
			cardRow("影响", ch.Impact),
			cardRow("处理", ch.Fix),
		)
	}
	return buildNotifyCard("智巡 AI 故障",
		cardRow("功能", cardStrong(ch.Name)),
		cardRow("问题", cardWarn(why)),
		cardRow("影响", ch.Impact),
		cardRow("处理", ch.Fix),
	)
}

func aiAlertDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("约 %d 分钟", max(1, int(d.Minutes())))
	case d < 48*time.Hour:
		return fmt.Sprintf("约 %.0f 小时", d.Hours())
	}
	return fmt.Sprintf("约 %.0f 天", d.Hours()/24)
}

// aiAlertBotSet 收 AI 故障提醒的群。
func aiAlertBotSet(kv map[string]string) map[int]bool {
	out := map[int]bool{}
	for _, part := range strings.Split(kv[keyAIAlertBots], ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil && n > 0 {
			out[n] = true
		}
	}
	return out
}

func encodeAIAlertBots(set map[int]bool) string {
	idx := make([]int, 0, len(set))
	for n, on := range set {
		if on {
			idx = append(idx, n)
		}
	}
	sort.Ints(idx)
	parts := make([]string, len(idx))
	for i, n := range idx {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// fetchAIHealth 问 AI 服务的 /health —— 和系统页 AI 状态卡(handleAIHealth)看的是同一份,
// 故障码也用同一套翻译(errCode / codeToText)。
//
// 【超时压到 5 秒】ai-service 卡住的时候,不能连带把调用方也拖住。
func fetchAIHealth(baseURL string) (map[string]any, error) {
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Get(baseURL + "/health")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var h map[string]any
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return h, nil
}

// startAIAlertLoop 每两分钟查一次。和每日推送同一个写法:panic 不带走进程,也不静默退出。
func (s *Server) startAIAlertLoop(ctx context.Context) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("ERROR: AI 故障提醒循环崩溃并退出: %v", r)
			}
		}()
		ticker := time.NewTicker(aiAlertInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runAIAlertOnce(time.Now().In(cnLoc))
			}
		}
	}()
}

func (s *Server) runAIAlertOnce(now time.Time) {
	if s.aiClient == nil || strings.TrimSpace(s.aiClient.baseURL) == "" {
		return
	}
	h, err := fetchAIHealth(s.aiClient.baseURL)
	codes := aiAlertCodes(h, err == nil)

	kv, kerr := s.store.ListAppSettings()
	if kerr != nil {
		log.Printf("WARN: AI 故障提醒读设置失败: %v", kerr)
		return
	}
	state := map[string]aiChannelState{}
	_ = json.Unmarshal([]byte(kv[keyAIAlertState]), &state)

	var cards []string
	changed := false
	for _, ch := range aiAlertChannels {
		prev := state[ch.Key]
		code, checked := codes[ch.Key]
		if !checked {
			continue // 这次没查到这一项(服务连不上时问不到账户状态):保持原样
		}
		threshold := 1
		if ch.Key == "service" {
			threshold = aiAlertUnreachableTimes
		}
		next, action := decideAIAlert(prev, code, threshold, now)
		if next != prev {
			changed = true
		}
		if next.Code == "" {
			delete(state, ch.Key)
		} else {
			state[ch.Key] = next
		}
		if action == "" {
			continue
		}
		since, _ := time.Parse(time.RFC3339, firstNonEmpty(prev.Since, next.Since))
		if since.IsZero() {
			since = now
		}
		log.Printf("AI 故障提醒:%s %s(%s)", ch.Name, action, firstNonEmpty(code, prev.Code))
		cards = append(cards, aiAlertCard(ch, action, firstNonEmpty(code, prev.Code), since, now))
	}
	if changed {
		raw, _ := json.Marshal(state)
		if err := s.store.SetAppSettings(map[string]string{keyAIAlertState: string(raw)}, "system"); err != nil {
			log.Printf("WARN: AI 故障提醒存状态失败: %v", err)
		}
	}
	if len(cards) == 0 {
		return
	}
	s.sendAIAlertCards(kv, cards)
}

// sendAIAlertCards 发给选了"AI 故障提醒"的那几个群。一个都没选就只记日志。
func (s *Server) sendAIAlertCards(kv map[string]string, cards []string) {
	want := aiAlertBotSet(kv)
	sent := 0
	for _, b := range s.weworkBots {
		if !want[b.Index] || b.Client == nil || !b.Client.Enabled() {
			continue
		}
		for _, c := range cards {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if _, err := b.Client.SendMarkdown(ctx, c); err != nil {
				log.Printf("WARN: AI 故障提醒发到 %s 失败: %v", b.Name, err)
			} else {
				sent++
			}
			cancel()
		}
	}
	if sent == 0 {
		log.Printf("WARN: AI 故障提醒没有发出去 —— 推送设置里没有哪个群打开了「AI 故障提醒」,或者群地址没配好")
	}
}
