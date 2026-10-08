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
//
// 【比较用 >=,不用 >】小数点右移一位,得到的正好是 10 倍出头:同一天重抄、
// 表没走字时就是整整 10 倍 —— 用 > 的话这一种恰好漏过去。
const readingJumpRatio = 10

// sanityNoteMark 写进理由里的标记。重新检查时靠它把旧的那一段摘掉。
const sanityNoteMark = "【读数存疑】"

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
// 只看【AI 读出来的那个数】:人手工改过的不碰 —— 人比这套规则更有发言权,
// 系统把人改的值打回"需复核"就成了"改了又被系统改回去"。
//
// 【基准是"这块表"上一次的读数,不是"这一格"上一次的值】抄表的格子可以挂任意
// 一块表:线上 Z1 的读数先后记在 Z4、Z2、Z1 三格里。原来按格子取基准,比的常常是
// 另一块表的数 —— 该报的不报(9/30 Z1 读成 11689.519,和那一格上次挂的表比,
// 没比出"比 Z1 自己的 203904.5 小"),不该报的乱报(大表让给小表的那一格,
// 每次都"比上一次小")。乱报多了人就习惯性点确认,真错的也跟着过去了。
// 现在先按这一格当前挂的是哪块表,取那块表自己最近一次的读数;
// 台账里对不上设备的格子(没配设备类型的模板、或者台账里没有那台)才退回按格子比。
func flagImplausibleReadings(store Store, rec *Record) []readingSanityIssue {
	return checkReadings(store, rec, nil)
}

// recheckReadingSanity 读数换了表之后(挪格子、对调、改选设备),按新的那块表重新查这几格。
//
// 【先摘掉旧的那段理由】旧理由写的是"比 Z1 上一次的 … 还小" —— 读数挪到 Z2 那一格之后
// 还挂着这句话,人会去核一块根本不相干的表。
//
// 【查出没问题时不撤"需复核"】这一格可能还因为别的原因待复核(小数点没看清等),
// 这里分不清;多看一眼的代价远小于漏看一眼。
func recheckReadingSanity(store Store, rec *Record, codes ...string) []readingSanityIssue {
	if rec == nil || len(codes) == 0 {
		return nil
	}
	only := map[string]bool{}
	for _, c := range codes {
		only[c] = true
	}
	for i := range rec.Fields {
		if only[rec.Fields[i].Code] {
			rec.Fields[i].Reason = stripSanityNote(rec.Fields[i].Reason)
		}
	}
	return checkReadings(store, rec, only)
}

// stripSanityNote 把理由里这道检查写的那一段摘掉。它总是追加在最后。
func stripSanityNote(reason string) string {
	i := strings.Index(reason, sanityNoteMark)
	if i < 0 {
		return reason
	}
	return strings.TrimRight(strings.TrimSpace(reason[:i]), ";；")
}

// isAIReading 这一格的值还是不是 AI 读出来的那个数。
//
// 【不能只看 Source == "ai"】挪格子、对调会把 source 记成人改的(那一下确实是人做的),
// 可读数本身没人碰过 —— 照样要按新的那块表查一遍。
func isAIReading(f *FieldValue) bool {
	if f.Source == "ai" {
		return true
	}
	ai := strings.TrimSpace(f.AIValue)
	return ai != "" && strings.TrimSpace(f.Value) == ai
}

func checkReadings(store Store, rec *Record, only map[string]bool) []readingSanityIssue {
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
	meters := meterBaselines(store, rec, tpl, watched)

	var issues []readingSanityIssue
	for i := range rec.Fields {
		f := &rec.Fields[i]
		label, watchedField := watched[f.Code]
		if !watchedField || (only != nil && !only[f.Code]) || !isAIReading(f) {
			continue
		}
		v, ok := parseReading(f.Value)
		if !ok {
			continue
		}
		// 基准:能对上台账里那块表的,用那块表自己的上一次;对不上的退回按格子
		prev, hasPrev := baseline[f.Code]
		whose := "上一次"
		if mb, isMeter := meters[f.Code]; isMeter {
			prev, hasPrev = mb.Value, mb.Has
			whose = mb.AssetName + " 上一次"
		}
		reason := ""
		switch {
		case v < 0:
			// 累计读数没有负数。这条不需要基准,任何时候都成立。
			reason = "累计读数不可能是负数"
		case !hasPrev:
			// 没有基准就不下结论 —— 首次抄表、新装的表都会走到这里。
			continue
		case v < prev:
			reason = fmt.Sprintf("比%s的 %s 还小(累计读数只会往上走);要么读错了(小数点、或者拍的是另一块表),要么这块表换过、需要人工确认", whose, trimNum(prev))
		case prev > 0 && v >= prev*readingJumpRatio:
			reason = fmt.Sprintf("是%s %s 的 %.0f 倍,量级对不上 —— 最常见的原因是小数点丢了(比如把 LCD 上下两行拼成一个整数)", whose, trimNum(prev), v/prev)
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
		f.Reason += sanityNoteMark + reason
	}
	return issues
}

// meterBaseline 一格读数当前挂的是哪块表,那块表上一次读了多少。
type meterBaseline struct {
	AssetName string
	Value     float64
	Has       bool
}

// meterBaselines 给"自带设备类型"的读数格找基准:这一格当前挂的那块表,它自己最近一次的读数。
//
// 只返回【能在台账里唯一对上一台设备】的格子;对不上的不在结果里,调用方退回按格子比。
// "这一格挂的是谁"的规则和确认页一致(fillReadingAssetOptions):
// 人选过的按人选的,没选过的按格子名对上台账里那一台,人点了「清除」的就不猜。
func meterBaselines(store Store, rec *Record, tpl ReportTemplate, watched map[string]string) map[string]meterBaseline {
	out := map[string]meterBaseline{}
	if store == nil {
		return out
	}
	assetTypeOf := map[string]string{}
	for _, f := range tpl.Fields {
		if _, w := watched[f.Code]; w && strings.TrimSpace(f.AssetType) != "" {
			assetTypeOf[f.Code] = strings.TrimSpace(f.AssetType)
		}
	}
	if len(assetTypeOf) == 0 {
		return out
	}
	assets, err := store.ListAssets(rec.TenantID)
	if err != nil {
		log.Printf("读数校验:取台账失败,本次按格子比对: %v", err)
		return out
	}
	byType := map[string][]string{}
	byName := map[string][]*AssetEntry{} // "类型|名字" -> 台账里叫这个名字的设备
	for _, a := range assets {
		if a == nil {
			continue
		}
		at, name := strings.TrimSpace(a.AssetType), strings.TrimSpace(a.AssetName)
		if at == "" || name == "" {
			continue
		}
		if rec.Project != "" && a.Project != "" && a.Project != rec.Project {
			continue
		}
		byType[at] = append(byType[at], name)
		byName[at+"|"+name] = append(byName[at+"|"+name], a)
	}
	for at := range byType {
		byType[at] = dedupSorted(byType[at])
	}
	explicit := map[string]bool{}
	for i := range rec.Fields {
		f := &rec.Fields[i]
		if _, ok := assetTypeOf[f.Code]; ok && strings.TrimSpace(f.AssetName) != "" && !f.AssetDefaulted {
			explicit[strings.TrimSpace(f.AssetName)] = true
		}
	}
	readingFields := readingFieldsOf(rec.TemplateID)
	for i := range rec.Fields {
		f := &rec.Fields[i]
		at, ok := assetTypeOf[f.Code]
		if !ok {
			continue
		}
		name := strings.TrimSpace(f.AssetName)
		if name == "" && !f.AssetCleared {
			if d := defaultAssetForField(f.Label, byType[at]); d != "" && !explicit[d] {
				name = d
			}
		}
		hits := byName[at+"|"+name]
		if name == "" || len(hits) != 1 {
			continue // 对不上、或者同名两台:不知道是哪块表,退回按格子比
		}
		mb := meterBaseline{AssetName: name}
		mb.Value, mb.Has = lastMeterReading(store, hits[0].ID, rec.ID, readingFields)
		out[f.Code] = mb
	}
	return out
}

// lastMeterReading 这块表最近一次的读数(不管当时记在哪一格)。
// 观测本来就按设备记(asset_id),抄表那几格按设备合成一条,和读数趋势同一口径。
func lastMeterReading(store Store, assetID, excludeRecord string, readingFields map[string]bool) (float64, bool) {
	obs, err := store.ListFieldObservations(assetID, "", 50)
	if err != nil {
		return 0, false
	}
	for i := len(obs) - 1; i >= 0; i-- { // 按时间正序返回,从后往前找最近的
		o := obs[i]
		if o == nil || o.ValueNumber == nil || o.RecordID == excludeRecord || !readingFields[o.FieldKey] {
			continue
		}
		if *o.ValueNumber < 0 {
			continue
		}
		return *o.ValueNumber, true
	}
	return 0, false
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
