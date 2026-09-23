package main

import (
	"fmt"
	"strings"
)

// ===== 群卡片的统一版式 =====
//
// 企业微信的 markdown 只认这么几样:标题、**粗体**、引用块、链接,
// 以及 <font color="info|comment|warning">。没有表格、没有字号。
// 所以"好看"只能靠三件事:【信息顺序】【粗细对比】【克制用色】。
//
// 【顺序按人扫消息的顺序排】收到一条提醒,人在两秒内要回答三个问题:
//   1. 哪个现场?   → 项目排第一。两个项目的群要是发混了,这一行就是纠错的锚点
//   2. 哪台设备?   → 加粗,整条消息只有它最粗
//   3. 我要做什么? → 链接收尾,拇指正好落在那儿
// 原来三张卡片各排各的(修改申请把"申请人"放第一,异常提醒把"设备"放第一),
// 同一个群里混着出现,每条都得重新找一遍。
//
// 【用色只用一处】标签一律灰(comment),让值自己跳出来;
// 只有"状态/结果"这种需要判断轻重的用 warning。三种颜色并排就不像通知,
// 像广告 —— 而这类消息一天要发好几条。

// cardLabelWidth 标签栏对齐用。中文标签两到三个字,补到三个字宽,
// 冒号就能竖着对齐一列 —— 企微没有表格,这是唯一能让它整齐的办法。
func cardLabel(label string) string {
	// 全角空格补位:企微的等宽处理对半角空格不稳,全角稳定。
	for len([]rune(label)) < 3 {
		label += "　"
	}
	return fmt.Sprintf(`<font color="comment">%s</font>`, label)
}

// cardRow 一行"标签 值"。值为空时整行不出现 —— 摆一行「未填写」
// 既占地方又没信息,人还得确认一遍"哦这条是空的"。
func cardRow(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return "> " + cardLabel(label) + "  " + value
}

// cardStrong 整条消息里最该被看见的那一个值(设备名)。
func cardStrong(v string) string { return "**" + strings.TrimSpace(v) + "**" }

// cardWarn 需要判断轻重的值(状态、结果)。
func cardWarn(v string) string {
	return fmt.Sprintf(`<font color="warning">%s</font>`, strings.TrimSpace(v))
}

// buildNotifyCard 拼一张卡片。空行自动丢掉。
func buildNotifyCard(title string, rows ...string) string {
	out := make([]string, 0, len(rows)+1)
	out = append(out, "### "+title)
	for _, r := range rows {
		if strings.TrimSpace(r) != "" {
			out = append(out, r)
		}
	}
	return strings.Join(out, "\n")
}
