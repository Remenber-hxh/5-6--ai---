package main

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
)

// ===== 抄表读数的合理性兜底 =====
//
// 【为什么光靠提示词不够】2026-09-10 那条紫菡能耗记录:模型把 LCD 上下两行
// 直接拼成一个整数(60197924),真值是 60197.924 —— 差 1000 倍。而提示词里
// 白纸黑字写着「看不到真实小数点时 confidence 不得高于 0.65」,它给了 0.92。
// 规则给了、模型没执行,这种事没法靠再写一遍规则解决。
//
// 更要命的是后面那一步:值带着 92% 的置信度进了确认页,人点了「确认」。
// 因为 60197924 看上去就"像个读数" —— 人眼没有基准,分辨不出多了三位。
//
// 【所以兜底必须放在后端,而且必须有基准】累计读数是单调的:上一次抄的
// 那个数就是天然基准。和它一比,量级错误立刻现形,不需要任何额外配置,
// 也不用为每块表维护一个"合理区间"。
//
// 【这里只降级,不改值、不拦提交】判断依据是历史数据和经验阈值,不是真理。
// 改值等于用一个猜测覆盖另一个猜测;拦提交会让现场在换表当天交不了工。
// 能做且够用的是:把置信度压下来、强制人工复核、把理由写清楚 ——
// 让那一格在确认页上跳出来,而不是顶着 92% 混过去。

// 读数比上一次涨这么多倍就当异常。
//
// 【为什么是 10 倍】丢一位小数点是 10 倍、丢三位是 1000 倍,都在网里;
// 而真实的抄表增量,就算漏抄几个月、或者中间换过表清零重计,也很难到 10 倍。
// 定得再紧(比如 2 倍)会把"换表后重新计数""长假后集中用电"误伤成异常。
const readingJumpRatio = 10

// 基准往回找几条。够覆盖"上一两次没填这个字段"的情况,又不至于翻到太老的表。
const readingBaselineScan = 20

// readingSanityIssue 一条读数异常。
type readingSanityIssue struct {
	Code     string
	Label    string
	Value    float64
	Baseline float64 // 上一次的读数;负数场景下无意义
	Reason   string
}

// flagImplausibleReadings 给刚被 AI 填上的抄表读数做合理性检查。
//
// 只看【AI 刚写的】那些格子:人手工改过的不碰 —— 人比这套规则更有发言权,
// 系统把人改的值打回"需复核"就成了"改了又被系统改回去"。
func flagImplausibleReadings(store Store, rec *Record) []readingSanityIssue {
	if rec == nil {
		return nil
	}
	tpl, ok := templateByID(rec.TemplateID)
	if !ok {
		return nil
	}
	// 只查"读数"类字段:number + 读取文本。选项题、日期、备注不适用。
	watched := map[string]string{} // code -> label
	for _, f := range tpl.Fields {
		if f.Kind == "number" && f.JudgeMode == ModeReadText {
			watched[f.Code] = f.Label
		}
	}
	if len(watched) == 0 {
		return nil
	}

	baseline := latestReadings(store, rec, watched)

	var issues []readingSanityIssue
	for i := range rec.Fields {
		f := &rec.Fields[i]
		label, watchedField := watched[f.Code]
		if !watchedField || f.Source != "ai" {
			continue
		}
		v, ok := parseReading(f.Value)
		if !ok {
			continue
		}
		reason := ""
		prev, hasPrev := baseline[f.Code]
		switch {
		case v < 0:
			// 累计读数没有负数。这条不需要基准,任何时候都成立。
			reason = "累计读数不可能是负数"
		case !hasPrev:
			// 没有基准就不下结论 —— 首次抄表、新装的表都会走到这里。
			continue
		case v < prev:
			reason = fmt.Sprintf("比上一次的 %s 还小(累计读数只会往上走);要么读错了,要么这块表换过、需要人工确认", trimNum(prev))
		case prev > 0 && v > prev*readingJumpRatio:
			reason = fmt.Sprintf("是上一次 %s 的 %.0f 倍,量级对不上 —— 最常见的原因是小数点丢了(比如把 LCD 上下两行拼成一个整数)", trimNum(prev), v/prev)
		default:
			continue
		}
		issues = append(issues, readingSanityIssue{
			Code: f.Code, Label: label, Value: v, Baseline: prev, Reason: reason,
		})
		// 【压置信度 + 强制复核,但保留值】留着人才知道 AI 读成了什么,
		// 也才对得上照片去判断该改成多少;清空只会让人从头猜。
		f.NeedsReview = true
		if f.Confidence > 0.5 {
			f.Confidence = 0.5
		}
		f.Reason = strings.TrimSpace(f.Reason)
		if f.Reason != "" {
			f.Reason += ";"
		}
		f.Reason += "【读数存疑】" + reason
	}
	return issues
}

// latestReadings 找每个字段最近一次【已提交】记录里的值。
//
// 【逐字段各找各的,不是找"上一条记录"】上一条记录里这个字段可能是空的
// (那块表没拍到、屏没亮)。只看最近一条的话,一次漏拍就让基准断掉,
// 而断掉是静默的 —— 表现成"这个校验时灵时不灵"。
func latestReadings(store Store, rec *Record, watched map[string]string) map[string]float64 {
	out := map[string]float64{}
	if store == nil {
		return out
	}
	history, err := store.ListSubmittedByTemplate(rec.TenantID, rec.TemplateID, readingBaselineScan)
	if err != nil {
		// 【查不到历史不是错误,是"这次没有基准"】抄表照样能交,
		// 只是少一层兜底。为它中断识别流程是本末倒置。
		log.Printf("读数校验:取 %s 的历史失败,本次跳过基准比对: %v", rec.TemplateID, err)
		return out
	}
	for _, h := range history {
		if h == nil || h.ID == rec.ID {
			continue // 别拿自己当自己的基准
		}
		// 【点位要对得上】同一个模板可能挂在多个点位上(几栋楼各一套表),
		// 拿 A 楼的读数校验 B 楼,两边都会被判成"量级对不上"。
		if !samePoint(h, rec) {
			continue
		}
		for _, f := range h.Fields {
			if _, watchedField := watched[f.Code]; !watchedField {
				continue
			}
			if _, done := out[f.Code]; done {
				continue // history 已按时间倒序,先遇到的就是最近的
			}
			if v, ok := parseReading(f.Value); ok && v >= 0 {
				out[f.Code] = v
			}
		}
		if len(out) == len(watched) {
			break
		}
	}
	return out
}

// samePoint 两条记录是不是同一个点位。
//
// 点位 id 为空的老数据退回比点位名 —— 都为空时算同一个点位:
// 那是"这个模板只有一套表"的情形,也是绝大多数场景。
func samePoint(a, b *Record) bool {
	if strings.TrimSpace(a.PointID) != "" || strings.TrimSpace(b.PointID) != "" {
		return a.PointID == b.PointID
	}
	return strings.TrimSpace(a.PointName) == strings.TrimSpace(b.PointName)
}

// parseReading 把字段值解析成数字。空值、非数字、Inf/NaN 一律当"没有读数"。
func parseReading(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// 现场偶尔会带上单位,去掉再解析
	s = strings.TrimSuffix(s, "kWh")
	s = strings.TrimSuffix(s, "m³")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// trimNum 把数字打印成人看的样子:去掉没意义的末尾 0。
func trimNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}
