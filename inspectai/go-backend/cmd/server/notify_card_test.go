package main

import (
	"strings"
	"testing"
	"time"
)

// ===== 群卡片的版式 =====
//
// 现场要求:加上项目名、排得好看一点。企微 markdown 只有标题/粗体/引用/
// 链接/三种字色,所以"好看"只能靠信息顺序、粗细对比和克制用色。

func cardTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	s, _, rid := newSwapAPIServer(t)
	if err := s.store.CreateAsset(&AssetEntry{
		ID: "紫菡雅集::zihan_energy::消防水表", TenantID: defaultTenantID,
		Project: "紫菡雅集", TemplateID: "zihan_energy", AssetType: "水表",
		AssetName: "消防水表", AssetKey: "消防水表", LastStatus: "待复核",
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	return s, rid
}

// 【项目名必须在,而且排第一】两个项目的群要是发混了,这一行就是纠错的锚点。
func TestCardStartsWithProject(t *testing.T) {
	s, _ := cardTestServer(t)
	cr := &ChangeRequest{
		ID: "cr_1", TargetType: "asset", TargetID: "紫菡雅集::zihan_energy::消防水表",
		RequestedBy: "胡栖月", Reason: "AI 识别有误", RequestedAt: time.Now(),
	}
	project := s.changeRequestProject(cr)
	card := buildNotifyCard("智巡修改申请",
		cardRow("项目", project),
		cardRow("对象", s.changeTargetLabel(cr)),
		cardRow("申请人", cr.RequestedBy),
		cardRow("原因", truncate(cr.Reason, 100)),
		"> "+markdownLink("进入审批详情", s.adminChangeRequestURL(cr.ID)),
	)
	t.Logf("修改申请卡片:\n%s", card)

	if !strings.Contains(card, "紫菡雅集") {
		t.Fatal("没有项目名 —— 收到的人不知道是哪个现场")
	}
	lines := strings.Split(card, "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "紫菡雅集") {
		t.Errorf("项目不在第一行:%q", lines)
	}
	if strings.Contains(card, "::") {
		t.Errorf("卡片里漏了内部主键:%s", card)
	}
}

// 空值整行不出现 —— 摆一行「未填写」既占地方又没信息。
func TestCardDropsEmptyRows(t *testing.T) {
	card := buildNotifyCard("标题",
		cardRow("项目", "紫菡雅集"),
		cardRow("巡检人", ""),   // 没有就别占一行
		cardRow("说明", "   "), // 全空白同理
		cardRow("状态", cardWarn("待复核")),
	)
	if strings.Contains(card, "巡检人") || strings.Contains(card, "说明") {
		t.Errorf("空行没被丢掉:\n%s", card)
	}
	if n := len(strings.Split(card, "\n")); n != 3 {
		t.Errorf("应该只剩 标题+项目+状态 三行,实得 %d 行:\n%s", n, card)
	}
}

// 【整条消息只有设备名最粗】粗的东西一多就等于都不粗。
func TestOnlyOneBoldValue(t *testing.T) {
	card := buildNotifyCard("智巡异常提醒",
		cardRow("项目", "紫菡雅集"),
		cardRow("设备", cardStrong("消防水表")),
		cardRow("状态", cardWarn("待复核")),
		cardRow("巡检人", "苑文涛"),
	)
	if n := strings.Count(card, "**"); n != 2 {
		t.Errorf("加粗不止一处(** 出现 %d 次):\n%s", n, card)
	}
}

// 标签一律灰,值自己跳出来;warning 只给需要判断轻重的那一个。
func TestColorsAreRestrained(t *testing.T) {
	card := buildNotifyCard("智巡异常提醒",
		cardRow("项目", "紫菡雅集"),
		cardRow("设备", cardStrong("消防水表")),
		cardRow("状态", cardWarn("待复核")),
	)
	if n := strings.Count(card, `color="warning"`); n != 1 {
		t.Errorf("warning 用了 %d 处,超过一处就不像通知像广告:\n%s", n, card)
	}
	// 每个标签都该是灰的
	if n := strings.Count(card, `color="comment"`); n != 3 {
		t.Errorf("标签没有统一压灰(%d 处):\n%s", n, card)
	}
}
