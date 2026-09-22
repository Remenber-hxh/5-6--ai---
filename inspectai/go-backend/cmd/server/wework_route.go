package main

import (
	"log"
	"strings"
)

// ===== 提醒发到哪个群:按项目路由 =====
//
// 【这个缺口是怎么留下的】"一个项目一个群"当初只做了每日未巡提醒那一条路
// (push_runner.go)。异常提醒、修改申请这几条走的是 sendWeWorkBotMarkdownAsync,
// 里面一直写死 s.weworkBot —— 那是第 1 个群,线上绑的是会议中心。
// 于是 2026-09-22 现场看到:紫菡雅集的异常提醒发进了会议中心的群。
//
// 这不只是"发错群"。群里能看到的是别的项目的设备名、巡检人、点位 ——
// 权限那套按项目隔离的口径,在推送这条路上等于没有。

// weworkBotAvailable 这套部署里有没有任何一个能发消息的群机器人。
//
// 【不能只看 s.weworkBot】那是第 1 个群(WEWORK_BOT_WEBHOOK)。只配了第 2 个群、
// 没配第 1 个的部署,按它判断会一条提醒都发不出去,而日志里什么都没有。
func (s *Server) weworkBotAvailable() bool {
	if s.weworkBot != nil && s.weworkBot.Enabled() {
		return true
	}
	for _, b := range s.weworkBots {
		if b.Client != nil && b.Client.Enabled() {
			return true
		}
	}
	return false
}

// botsForProject 这个项目的提醒该发给哪些群机器人。
//
// 规则和 visibilityForBot 一致:机器人没绑项目 = 收全部项目。
//
// 返回空切片 = 【没有合适的群】。调用方必须当成"别发",不要退回第 1 个群 ——
// 退回去就是把这个项目的内容送进别人的群,正是这次要修的事。
func (s *Server) botsForProject(project string) []weworkBotTarget {
	project = strings.TrimSpace(project)
	out := make([]weworkBotTarget, 0, len(s.weworkBots))
	for _, b := range s.weworkBots {
		if b.Client == nil || !b.Client.Enabled() {
			continue
		}
		// 没绑项目的群收全部 —— 也包括"不知道属于哪个项目"的那些提醒
		if len(b.Projects) == 0 {
			out = append(out, b)
			continue
		}
		if project == "" {
			continue // 项目不明,不往只收特定项目的群里发
		}
		for _, p := range b.Projects {
			if strings.TrimSpace(p) == project {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// weworkTargetsFor 一条提醒实际要发到的那几个群。
//
// 【一个群都没配时退回旧行为】线上有一阵子只有 WEWORK_BOT_WEBHOOK、
// 没有分项目配置,那时 weworkBots 是空的。这种部署不该因为这次改动
// 就再也收不到提醒 —— 退回原来那个单群。
//
// 【但配了分项目却没有一个匹配时不退回】那说明项目名和配置对不上
// (打错字、项目改名、新项目没登记)。这时退回第 1 个群就是把 A 项目的内容
// 发进 B 项目的群 —— 宁可不发,并在日志里喊出来。
func (s *Server) weworkTargetsFor(event, project string) []*WeWorkBotClient {
	if len(s.weworkBots) == 0 {
		if s.weworkBot != nil && s.weworkBot.Enabled() {
			return []*WeWorkBotClient{s.weworkBot}
		}
		return nil
	}
	hits := s.botsForProject(project)
	if len(hits) == 0 {
		// 【说清是哪个项目没群】不说的话运维只会看到"提醒没发",
		// 然后去查 webhook、查网络 —— 而原因是配置里没有这个项目。
		log.Printf("WARN: [%s] 项目「%s」没有对应的群机器人,这条提醒没有发送 —— "+
			"检查 WEWORK_BOT_[N]_PROJECTS 是否写了这个项目名", event, project)
		return nil
	}
	out := make([]*WeWorkBotClient, 0, len(hits))
	for _, b := range hits {
		out = append(out, b.Client)
	}
	return out
}

// changeRequestProject 这条修改申请是哪个项目的。
//
// 【资产 ID 的第一段就是项目】"会议中心::elevator_no_room::KT-5"。
// 记录要查一次库 —— 提醒是异步发的,多一次查询换不发错群,值。
// 拿不到就返回空:上面那层会当成"项目不明",只发给收全部项目的群。
func (s *Server) changeRequestProject(cr *ChangeRequest) string {
	if cr == nil {
		return ""
	}
	switch cr.TargetType {
	case "asset":
		if parts := strings.SplitN(cr.TargetID, "::", 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0])
		}
	case "record":
		if rec, err := s.store.GetRecord(defaultTenantID, cr.TargetID); err == nil && rec != nil {
			return strings.TrimSpace(rec.Project)
		}
	}
	return ""
}
