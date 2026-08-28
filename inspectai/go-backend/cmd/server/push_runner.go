package main

import (
	"context"
	"errors"
	"log"
	"time"
)

// ===== 定时推送的执行者 =====
//
// 【这是项目里第一个后台 goroutine】所以退出、panic 兜底、日志噪音
// 这些都得在这里自己搭好 —— 没有现成的框架接。

// pushCatchUpMinutes 补发窗口。
//
// 容器 17:30 才起来、17:00 那次没发 —— 当天内补一次,"今天谁没巡"
// 到晚上仍然有用。但给个上界:23:59 起来的容器补一条谁也来不及处理的
// 提醒,只是噪音。5 小时 = 从 17:00 到 22:00。
const pushCatchUpMinutes = 300

// startDailyPushLoop 每分钟看一眼该不该发。
//
// 【为什么是轮询而不是"睡到 17:00"】睡到某个时刻的写法要处理改配置、
// 改时区、系统休眠唤醒这些情况,每一种都得重算下次唤醒时间;而漏算一次
// 的表现是"那天没发",没有任何报错。每分钟问一句便宜得多,也不会错过。
//
// 真正防重复的不是这个循环,是 push_log 的唯一键(见 ClaimPushSlot)。
func (s *Server) startDailyPushLoop(ctx context.Context) {
	go func() {
		// 【panic 兜底】这个 goroutine 崩了不该带走整个进程,但也不能
		// 静悄悄地死掉 —— 那会表现成"从某天起就不推送了",没人知道为什么。
		defer func() {
			if r := recover(); r != nil {
				log.Printf("ERROR: 每日推送循环崩溃并退出: %v", r)
			}
		}()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		log.Printf("  每日未巡提醒:循环已启动(时区 %s)", pushTZ)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDailyPushOnce(time.Now().In(pushTZ))
			}
		}
	}()
}

// runDailyPushOnce 跑一轮。now 必须已经是东八区的时间。
func (s *Server) runDailyPushOnce(now time.Time) {
	kv, err := s.store.ListAppSettings()
	if err != nil {
		log.Printf("WARN: 读推送设置失败: %v", err)
		return
	}
	cfg := dailyPushConfigFrom(kv)
	if !cfg.Enabled {
		return // 【默认关】不打日志:每分钟一条"未启用"会把日志刷爆
	}

	tenants, err := s.store.ListTenants()
	if err != nil {
		log.Printf("WARN: 列租户失败: %v", err)
		return
	}
	for _, t := range tenants {
		if t == nil || t.Status != "active" {
			continue
		}
		// 【一个租户失败不能中断其他租户】否则排在前面的那个一出问题,
		// 后面所有客户当天都收不到提醒,而日志里只有一条错误。
		s.pushOneTenant(t.ID, cfg, now)
	}
}

func (s *Server) pushOneTenant(tenantID string, cfg dailyPushConfig, now time.Time) {
	lastDay, err := s.store.LastPushDay(tenantID, pushKindDailyUndone)
	if err != nil {
		log.Printf("WARN: [%s] 读推送流水失败: %v", tenantID, err)
		return
	}
	if ok, _ := shouldFireDailyPush(cfg, now, lastDay, pushCatchUpMinutes); !ok {
		return
	}

	// 【先占位再发送】占位失败 = 别人已经发过(上一轮、另一个副本、手动触发)。
	// 这是正常路径,安静跳过 —— 打成 ERROR 的话日志里全是它,
	// 真正的失败反而被淹没。
	day := now.Format("2006-01-02")
	slot, err := s.store.ClaimPushSlot(tenantID, pushKindDailyUndone, day)
	if err != nil {
		if !errors.Is(err, errPushAlreadySent) {
			log.Printf("WARN: [%s] 抢占推送名额失败: %v", tenantID, err)
		}
		return
	}

	// 【调度器用系统视角算,不是某个人的可见范围】它代表系统本身 ——
	// 而且它没有请求、没有登录用户。页面上那份是按人裁过的,
	// 两者用的是同一个内核(buildTodayBoardFor),口径不会分叉。
	board, err := s.buildTodayBoardFor(tenantID, dataVisibility{AllData: true}, now)
	if err != nil {
		log.Printf("ERROR: [%s] 算今日看板失败: %v", tenantID, err)
		_ = s.store.FinishPushSlot(slot, "failed", "算看板失败: "+err.Error())
		return
	}
	digest := buildDailyPushDigest(board, cfg.SilentWhenDone)
	if !digest.WouldSend {
		// 【今天不发也要记账】不记的话下一分钟又会重新判一次,
		// 而"今天没什么可发的"这个结论一天算一次就够。
		_ = s.store.FinishPushSlot(slot, "skipped", digest.SkipReason)
		return
	}

	if s.weworkBot == nil || !s.weworkBot.Enabled() {
		// 【说清楚是没配,不是没数据】否则运维看到"没推送"会去查计划和设备,
		// 而问题只是 WEWORK_BOT_WEBHOOK 没设。
		log.Printf("WARN: [%s] 有 %d 台待巡但企微群机器人未配置,提醒发不出去",
			tenantID, digest.Pending)
		_ = s.store.FinishPushSlot(slot, "failed", "企业微信群机器人未配置(WEWORK_BOT_WEBHOOK)")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := s.weworkBot.SendMarkdown(ctx, digest.Text); err != nil {
		log.Printf("ERROR: [%s] 推送发送失败: %v", tenantID, err)
		_ = s.store.FinishPushSlot(slot, "failed", err.Error())
		return
	}
	log.Printf("每日未巡提醒已发送 [%s] %s:待巡 %d 台", tenantID, day, digest.Pending)
	_ = s.store.FinishPushSlot(slot, "sent", "")
}
