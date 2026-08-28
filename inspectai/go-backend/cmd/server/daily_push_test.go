package main

import (
	"strings"
	"testing"
)

// 每日提醒的文案。
//
// 【为什么这块一定要有测试】定时任务本身很难测(总不能等到 17 点),
// 所以能挪进纯函数的判断都挪了进来 —— 那这里就得把它们钉住。
// 而且这条消息是发给领导的群的:错一次,以后没人会再信这个数字。

func pushBoard(plans ...DailyPlanStatus) *TodayInspectionBoard {
	b := &TodayInspectionBoard{Date: "2026-08-27", Weekday: 3, Plans: plans}
	seen := map[string]bool{}
	for _, p := range plans {
		for _, a := range p.Assets {
			if seen[a.AssetID] {
				continue
			}
			seen[a.AssetID] = true
			b.Total++
			if a.Done {
				b.Done++
			}
		}
	}
	return b
}

func TestDailyPushGroupsByProjectAndOwner(t *testing.T) {
	d := buildDailyPushDigest(pushBoard(
		DailyPlanStatus{
			Project: "会议中心", OwnerName: "胡晓悱",
			Assets: []DailyAssetStatus{
				{AssetID: "a1", AssetName: "K01", Project: "会议中心"},
				{AssetID: "a2", AssetName: "K07", Project: "会议中心"},
			},
		},
		DailyPlanStatus{
			Project: "会议中心", OwnerName: "朱佳伟",
			Assets: []DailyAssetStatus{
				{AssetID: "a3", AssetName: "KT-3", Project: "会议中心", Done: true},
			},
		},
	), false)

	if d.Pending != 2 || d.Total != 3 || d.Done != 1 {
		t.Fatalf("统计不对:pending=%d total=%d done=%d", d.Pending, d.Total, d.Done)
	}
	if len(d.Groups) != 1 || d.Groups[0].Project != "会议中心" {
		t.Fatalf("应只有一个项目分组:%+v", d.Groups)
	}
	// 已完成的那个人不该出现 —— 消息里只放需要行动的信息
	if len(d.Groups[0].Lines) != 1 || d.Groups[0].Lines[0].OwnerName != "胡晓悱" {
		t.Fatalf("只该列出还有欠的人:%+v", d.Groups[0].Lines)
	}
	if !strings.Contains(d.Text, "K01、K07") {
		t.Errorf("文案里应把同一个人的设备并成一行:\n%s", d.Text)
	}
	if strings.Contains(d.Text, "KT-3") {
		t.Errorf("已完成的设备不该出现在文案里:\n%s", d.Text)
	}
}

// 【按设备去重】两条计划点了同一台时只算一次。
// 不去重的话总数会虚高,而"还差 5 台"里有 2 台是同一台 —— 现场会白跑。
func TestDailyPushDeduplicatesAssetAcrossPlans(t *testing.T) {
	d := buildDailyPushDigest(pushBoard(
		DailyPlanStatus{
			Project: "会议中心", OwnerName: "胡晓悱",
			Assets: []DailyAssetStatus{{AssetID: "a1", AssetName: "K01", Project: "会议中心"}},
		},
		DailyPlanStatus{
			Project: "会议中心", OwnerName: "朱佳伟",
			Assets: []DailyAssetStatus{{AssetID: "a1", AssetName: "K01", Project: "会议中心"}},
		},
	), false)
	if d.Pending != 1 {
		t.Errorf("同一台设备只该算一次,实际 pending=%d", d.Pending)
	}
	if strings.Count(d.Text, "K01") != 1 {
		t.Errorf("K01 在文案里出现了不止一次:\n%s", d.Text)
	}
}

// 台账里已删除的设备要单独说 —— 它永远算不完,完成率永远到不了 100%,
// 而没人会想到是因为计划里挂着几台已经删掉的设备。
func TestDailyPushCallsOutMissingAssets(t *testing.T) {
	d := buildDailyPushDigest(pushBoard(
		DailyPlanStatus{
			Project: "会议中心", OwnerName: "胡晓悱",
			Assets: []DailyAssetStatus{{AssetID: "gone", AssetName: "gone", Missing: true}},
		},
	), false)
	if d.Missing != 1 {
		t.Fatalf("Missing=%d,应为 1", d.Missing)
	}
	if !strings.Contains(d.Text, "已从台账删除") {
		t.Errorf("文案里应点明这件事:\n%s", d.Text)
	}
}

// 【空推送要能关掉】没排计划、或者全巡完了,不该往群里发一条没信息量的消息。
func TestDailyPushSkipRules(t *testing.T) {
	// 今天没有每日计划
	if d := buildDailyPushDigest(pushBoard(), false); d.WouldSend {
		t.Errorf("没有计划时不该发:%+v", d)
	}
	allDone := pushBoard(DailyPlanStatus{
		Project: "会议中心", OwnerName: "胡晓悱",
		Assets: []DailyAssetStatus{{AssetID: "a1", AssetName: "K01", Done: true}},
	})
	// 全部完成 + 设置为"完成也发" → 发,而且是报喜的那句
	d := buildDailyPushDigest(allDone, false)
	if !d.WouldSend {
		t.Error("完成也发的设置下应该发")
	}
	if !strings.Contains(d.Text, "全部完成") {
		t.Errorf("全部完成时应换成报喜的文案:\n%s", d.Text)
	}
	// 全部完成 + 设置为"完成不发" → 不发
	if d := buildDailyPushDigest(allDone, true); d.WouldSend {
		t.Errorf("设置为完成不发时不该发:%+v", d)
	}
}

// 没写负责人的计划也要有人认领这几台,不能因为没名字就从消息里消失。
func TestDailyPushKeepsAssetsWithoutOwner(t *testing.T) {
	d := buildDailyPushDigest(pushBoard(
		DailyPlanStatus{
			Project: "会议中心",
			Assets:  []DailyAssetStatus{{AssetID: "a1", AssetName: "K01", Project: "会议中心"}},
		},
	), false)
	if d.Pending != 1 {
		t.Fatalf("没负责人的设备也要算进待巡,实际 %d", d.Pending)
	}
	if !strings.Contains(d.Text, "未指定负责人") {
		t.Errorf("应显式写出没人负责,而不是留空:\n%s", d.Text)
	}
}
