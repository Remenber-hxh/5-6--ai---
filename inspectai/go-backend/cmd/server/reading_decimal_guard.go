package main

import "strings"

// ===== 小数点是"猜"出来的,就必须让人看一眼 =====
//
// 【这道闸为什么非有不可】能耗表的 LCD 上常常根本没有小数点:屏上是
// 「EP / 6019 / 7924 K」八位数字分两行,小数点在哪要靠表型格式补。
// 场景提示词里写明了"按固定格式补上时 confidence 不超过 0.75",
// 但 2026-09-22 实测:模型在 reason 里老老实实写了"按固定格式切分",
// 转头给了 0.95 —— 而 NeedsReview 是拿 confidence 算的(<0.85),
// 于是这一行不会被标成待复核,人也就不会去核那个小数点。
//
// 后果不是"读错一个数字",是【稳定地、以高置信度给出差十倍的读数,还不提示】。
// 表型换了、或者那个固定格式本来就不对时,整批读数一起偏,而界面上一切正常。
//
// 所以:不靠模型自觉。它在理由里承认是猜的,后端就把置信度压下去、强制人工复核。

// assumedDecimalConfidence 猜出来的小数位最多给这么高。
//
// 【为什么是 0.6 不是提示词里那个 0.75】NeedsReview 的阈值是 0.85,
// 0.75 也能触发复核;但 0.6 还会落进"识别置信偏低"那一组,
// 确认页顶部会把它们聚在一起提示——猜出来的小数点正该进那一组。
const assumedDecimalConfidence = 0.6

// decimalHint 补在理由后面、说给现场人听的那句话;decimalHintMarker 是它的
// 去重标记 —— 两者必须同源,否则每识别一次就再追加一遍。
const (
	decimalHintMarker = "请对着表补小数点"
	decimalHint       = " —— 屏上看不到小数点," + decimalHintMarker
)

// assumedDecimalMarkers AI 在理由里承认"小数位不是从屏幕上读到的"时会出现的说法。
//
// 【为什么是一组关键词而不是一个结构化字段】结构化字段同样要模型自觉去填,
// 而这次的教训恰恰是它会填一半。理由是自由文本、模型写得很稳,
// 多列几种说法比赌一个布尔位可靠。提示词那边也要求写明,两边对着来。
// 【A 组:小数点是按格式补出来的】这类说法只有在读数【真的带小数点】时才算数 ——
// 机械水表的理由里也会写"按固定格式读取黑色字轮",那是整数读数,不该被拖进复核。
var assumedDecimalMarkers = []string{
	"固定格式",
	"按格式",
	"格式假设",
	"假设小数",
	"按表型",
}

// 【B 组:小数点压根没定下来】提示词现在要求"看不见小数点就原样输出数字串、
// 并在理由里写明"。那种情况下 value 里【没有】小数点,A 组那个判断就漏了 ——
// 而这恰恰是最需要人去补的一种:一串 60197924 交上去,差的是一千倍。
// 所以这一组不看 value 有没有小数点。
var undeterminedDecimalMarkers = []string{
	"看不到小数点",
	"看不清小数点",
	"小数点不可见",
	"未见小数点",
	"没有小数点",
	"请按实际度数补",
}

func hasAnyMarker(s string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// decimalWasAssumed 这个读数的小数位是猜的、或者压根还没定下来。
func decimalWasAssumed(value, reason string) bool {
	r := strings.TrimSpace(reason)
	if r == "" {
		return false
	}
	if hasAnyMarker(r, undeterminedDecimalMarkers) {
		return true
	}
	// 按格式补的:只有读数真带小数点时才算
	return strings.Contains(value, ".") && hasAnyMarker(r, assumedDecimalMarkers)
}

// guardAssumedDecimal 小数位是猜出来的就压低置信度并强制人工复核。
//
// 【只压不抬】模型自己给得比这还低时保持原样 —— 它可能有别的理由不确信,
// 抬上去等于替它把话说满了。
func guardAssumedDecimal(f *FieldValue) {
	if f == nil || !decimalWasAssumed(f.Value, f.Reason) {
		return
	}
	if f.Confidence > assumedDecimalConfidence {
		f.Confidence = assumedDecimalConfidence
	}
	f.NeedsReview = true
	// 【把话说给人听】理由里原本是"按固定格式切分"这种内部说法,
	// 现场看不懂那意味着什么。补一句说清要他做什么。
	//
	// 去重要认这句话里的固定片段,不能各写各的 —— 判断和追加用的不是同一串时,
	// 每识别一次就会再追加一遍,理由越滚越长。
	if !strings.Contains(f.Reason, decimalHintMarker) {
		f.Reason = strings.TrimSpace(f.Reason + decimalHint)
	}
}
