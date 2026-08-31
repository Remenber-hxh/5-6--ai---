package main

import (
	"strings"
	"testing"
	"time"
)

// 一句话结论。
//
// 这块只要错一次,人就再也不看它了 —— 报了不该报的(狼来了),
// 或漏了该报的(更糟)。所以每一档都钉住,而且要检查【依据是否给出】:
// 只给结论不给依据的话,人既无法认同也无法反驳,最后只会忽略它。

func verdictAsset() *AssetEntry {
	return &AssetEntry{ID: "a1", AssetName: "K01", LastStatus: "正常"}
}

func TestVerdictOKWhenNothingWrong(t *testing.T) {
	v := buildAssetVerdict(verdictAsset(), assetOpenItems{Tasks: []openTaskBrief{}}, assetTrendResp{}, 3, 0)
	if v.Level != verdictOK {
		t.Errorf("一切正常时应为 ok,实际 %s(%v)", v.Level, v.Reasons)
	}
}

// 【最要紧的一条】出了问题却没人接手 —— 必须报,而且要说清是这一种。
func TestVerdictActOnAbnormalWithoutTask(t *testing.T) {
	a := verdictAsset()
	a.LastStatus = "异常"
	v := buildAssetVerdict(a, assetOpenItems{
		Tasks: []openTaskBrief{}, AbnormalWithoutTask: true, LastStatus: "异常",
	}, assetTrendResp{}, 2, 0)
	if v.Level != verdictAct {
		t.Fatalf("异常且无人接手应为 act,实际 %s", v.Level)
	}
	if len(v.Reasons) == 0 {
		t.Fatal("必须给出依据 —— 只给结论的话人既不能认同也不能反驳")
	}
	found := false
	for _, r := range v.Reasons {
		if strings.Contains(r, "没有任何在办任务") {
			found = true
		}
	}
	if !found {
		t.Errorf("依据里要点明「没人接手」这件事,实际 %v", v.Reasons)
	}
}

// 【久没巡 ≠ 有问题】混成同一档的话,真正出问题的设备会淹没在
// 一堆「很久没巡」里面。
func TestVerdictScheduleIsSeparateFromAct(t *testing.T) {
	v := buildAssetVerdict(verdictAsset(), assetOpenItems{Tasks: []openTaskBrief{}}, assetTrendResp{}, 60, 0)
	if v.Level != verdictSchedule {
		t.Errorf("只是久没巡应为 schedule 而不是 act,实际 %s", v.Level)
	}
	// 从未巡检同样归这一档
	v = buildAssetVerdict(verdictAsset(), assetOpenItems{Tasks: []openTaskBrief{}}, assetTrendResp{}, -1, 0)
	if v.Level != verdictSchedule {
		t.Errorf("从未巡检应为 schedule,实际 %s", v.Level)
	}
	if len(v.Reasons) == 0 || !strings.Contains(v.Reasons[0], "从未巡检") {
		t.Errorf("依据应说明是从未巡检,实际 %v", v.Reasons)
	}
}

// 读数漂移要能独立触发 —— 那正是趋势功能存在的理由。
func TestVerdictActOnDriftingReading(t *testing.T) {
	dev := -33.0
	v := buildAssetVerdict(verdictAsset(), assetOpenItems{Tasks: []openTaskBrief{}}, assetTrendResp{
		Series: []trendSeries{{FieldLabel: "水箱水位", Drifting: true, Deviation: &dev}},
	}, 2, 0)
	if v.Level != verdictAct {
		t.Fatalf("读数漂移应为 act,实际 %s", v.Level)
	}
	ok := false
	for _, r := range v.Reasons {
		if strings.Contains(r, "水箱水位") && strings.Contains(r, "33") {
			ok = true
		}
	}
	if !ok {
		t.Errorf("依据要写清是哪个读数、偏了多少,实际 %v", v.Reasons)
	}
}

// 逾期和在办要分开数 —— 合在一起报的话,"3 项未了结"里有没有逾期的
// 看不出来,而那正是要不要今天就动手的分界。
func TestVerdictSeparatesOverdueFromOpen(t *testing.T) {
	v := buildAssetVerdict(verdictAsset(), assetOpenItems{
		Tasks: []openTaskBrief{
			{ID: "t1", Overdue: true},
			{ID: "t2"},
			{ID: "t3"},
		},
	}, assetTrendResp{}, 2, 0)
	joined := strings.Join(v.Reasons, " | ")
	if !strings.Contains(joined, "1 项整改已逾期") {
		t.Errorf("应单独说出逾期条数,实际 %v", v.Reasons)
	}
	if !strings.Contains(joined, "2 项在办未了结") {
		t.Errorf("在办条数应扣掉逾期的那些,实际 %v", v.Reasons)
	}
}

// ===== 维保周期 =====

func TestMaintenanceOverdueNeedsBothCycleAndDate(t *testing.T) {
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

	// 【0 = 还没填,不是"不用维保"】当成 0 天周期的话,
	// 每台没填周期的设备都会被报成"已超期",满屏红字之后没人再看这条提示。
	if d := maintenanceOverdueDays(&AssetEntry{LastMaintainedAt: "2020-01-01"}, now); d != 0 {
		t.Errorf("没填周期不该判超期,实际超期 %d 天", d)
	}
	// 只填了周期、没填上次维保时间 —— 同样算不出来
	if d := maintenanceOverdueDays(&AssetEntry{MaintenanceCycleDays: 365}, now); d != 0 {
		t.Errorf("没填上次维保时间不该判超期,实际 %d", d)
	}
	// 日期格式不对(比如手填了"2020年")也不能瞎算
	if d := maintenanceOverdueDays(&AssetEntry{
		MaintenanceCycleDays: 365, LastMaintainedAt: "2020年",
	}, now); d != 0 {
		t.Errorf("日期解析不了时不该判超期,实际 %d", d)
	}
}

func TestMaintenanceOverdueCounts(t *testing.T) {
	now := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	// 2025-08-31 维保过,周期 365 天 → 刚好到期,不算超
	if d := maintenanceOverdueDays(&AssetEntry{
		MaintenanceCycleDays: 365, LastMaintainedAt: "2025-08-31",
	}, now); d != 0 {
		t.Errorf("刚好到期不该算超期,实际 %d", d)
	}
	// 2025-01-01 维保过,周期 365 → 已过 607 天,超 242 天
	d := maintenanceOverdueDays(&AssetEntry{
		MaintenanceCycleDays: 365, LastMaintainedAt: "2025-01-01",
	}, now)
	if d < 240 || d > 245 {
		t.Errorf("超期天数应在 242 天上下,实际 %d", d)
	}
}

// 维保超期要能【独立】把结论抬到"需要处理" —— 它和"久没巡"不是一回事:
// 久没巡是没人去看,维保超期是该做的保养没做。
func TestVerdictActOnMaintenanceOverdue(t *testing.T) {
	v := buildAssetVerdict(verdictAsset(), assetOpenItems{Tasks: []openTaskBrief{}},
		assetTrendResp{}, 2 /* 两天前刚巡过 */, 242)
	if v.Level != verdictAct {
		t.Fatalf("维保超期应为 act,实际 %s(%v)", v.Level, v.Reasons)
	}
	if !strings.Contains(strings.Join(v.Reasons, " "), "242") {
		t.Errorf("依据要写清超了多少天,实际 %v", v.Reasons)
	}
}
