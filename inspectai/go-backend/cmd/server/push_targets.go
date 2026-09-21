package main

import (
	"fmt"
	"os"
	"strings"
)

// ===== 一个项目一个群:推送目标 =====
//
// 【原来是什么样】一个全局 webhook,一条消息把所有项目的分组拼在一起发出去。
// 收消息的人得自己从里面挑出跟自己有关的那几行 —— 而现场是在手机上、
// 赶着下班的时候看这条。
//
// 现在每个群机器人自己绑一批项目,各发各的。
//
// 【为什么不做成数据库配置】webhook 是凭证。放进库里意味着它会出现在备份、
// 在后台页面上、在任何一次导出里;而它等价于"往这个群发消息的权限"。
// 跟着现有的加密配置走(本机 DPAPI / 线上环境变量),和 API key 同一个待遇。

// weworkBotTarget 一个群机器人 + 它负责哪些项目。
type weworkBotTarget struct {
	// Index 第几个群(从 1 开始),和 WEWORK_BOT_[N]_WEBHOOK 的编号一致。
	// 它是这个群在"单独设置"里的身份(见 push_bot_config.go)——
	// 不能用项目名当身份:项目改个名,这个群的时间设置就跟着丢了。
	Index int
	// Name 只用于日志和推送记账。【绝不放 webhook】—— 日志会被复制到工单、
	// 截图发到群里,凭证一旦进日志就等于公开了。
	Name   string
	Client *WeWorkBotClient
	// Projects 空 = 全部项目(就是今天的行为)。
	Projects []string
	// SlotKind 推送记账用的 kind。
	//
	// 【必须每个机器人一个】push_log 的唯一键是(租户+kind+日期)。
	// 所有机器人共用一个 kind 的话,发完第一个群就记成"今天已发",
	// 第二个群永远收不到 —— 而日志显示"已发送",谁都查不出问题在哪。
	SlotKind string
}

// 最多认几个机器人。给个上界纯粹是为了让"配错了键名"表现成"少一个群",
// 而不是无限往下找一个永远不存在的编号。
const maxWeWorkBots = 8

// loadWeWorkBotTargets 从配置里读出所有群机器人。
//
// 配置形状(第一个沿用原来的键名,线上不用改任何东西):
//
//	WEWORK_BOT_WEBHOOK      第 1 个群的地址
//	WEWORK_BOT_PROJECTS     第 1 个群收哪些项目,逗号隔开;留空 = 全部
//	WEWORK_BOT_2_WEBHOOK    第 2 个群
//	WEWORK_BOT_2_PROJECTS
//	...最多到 8
//
// 【为什么第一个不叫 _1_】线上那台已经配好了 WEWORK_BOT_WEBHOOK。改名意味着
// 那天起推送静默失效,而健康检查照样显示正常 —— 这种升级事故不值得为了
// 命名整齐去冒。
func loadWeWorkBotTargets(read func(key, def string) string) []weworkBotTarget {
	var out []weworkBotTarget
	for i := 1; i <= maxWeWorkBots; i++ {
		hookKey, projKey, slot := "WEWORK_BOT_WEBHOOK", "WEWORK_BOT_PROJECTS", pushKindDailyUndone
		if i > 1 {
			hookKey = fmt.Sprintf("WEWORK_BOT_%d_WEBHOOK", i)
			projKey = fmt.Sprintf("WEWORK_BOT_%d_PROJECTS", i)
			slot = fmt.Sprintf("%s#%d", pushKindDailyUndone, i)
		}
		hook := strings.TrimSpace(read(hookKey, ""))
		if hook == "" {
			continue // 没配就跳过,不中断 —— 中间空一个编号不该让后面的失效
		}
		projects := splitProjects(read(projKey, ""))
		out = append(out, weworkBotTarget{
			Index:    i,
			Name:     botDisplayName(i, projects),
			Client:   NewWeWorkBotClient(hook),
			Projects: projects,
			SlotKind: slot,
		})
	}
	return out
}

// botDisplayName 日志里怎么称呼这个机器人。
// 用项目名而不是编号 —— 运维看到"紫菡雅集群发送失败"能直接行动,
// 看到"机器人 2 发送失败"还得先去翻配置。
func botDisplayName(i int, projects []string) string {
	if len(projects) == 0 {
		return fmt.Sprintf("机器人%d(全部项目)", i)
	}
	return fmt.Sprintf("机器人%d(%s)", i, strings.Join(projects, "、"))
}

func splitProjects(raw string) []string {
	out := []string{}
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '，' || r == ';' || r == '；'
	}) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// visibilityForBot 这个机器人该看到哪些数据。
//
// 【复用权限那套口径,不另起一套】推送的项目过滤和页面上的项目过滤
// 如果各写各的,迟早出现"群里报了 3 台,页面上只有 2 台"——
// 而两边都说自己是对的。
func (b weworkBotTarget) visibility() dataVisibility {
	if len(b.Projects) == 0 {
		return dataVisibility{AllData: true}
	}
	return dataVisibility{Projects: append([]string(nil), b.Projects...)}
}

// envOrSecret 给 loadWeWorkBotTargets 用的默认读取器(本机 DPAPI / 线上环境变量)。
func envOrSecret(key, def string) string {
	if v := getenvWithSecret(key, ""); strings.TrimSpace(v) != "" {
		return v
	}
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
