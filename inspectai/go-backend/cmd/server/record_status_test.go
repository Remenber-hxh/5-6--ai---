package main

import "testing"

func fieldsWith(vals ...string) []FieldValue {
	out := make([]FieldValue, 0, len(vals))
	for i, v := range vals {
		out = append(out, FieldValue{Code: "f" + string(rune('a'+i)), Label: "字段", Value: v})
	}
	return out
}

// ===== 界面口径 =====
//
// 这份是 admin-web/src/lib/status.ts 的逐行翻译。搬过来的目的是让后端也能
// 按"页面上显示的那个状态"做事(导出、看板聚合),而不是各写一份。
//
// 【为什么要逐条钉】状态标签是人判断"要不要管这条记录"的唯一依据。
// 算错了不会报错、不会崩,只会让人放过该管的、或者去管不用管的。
func TestRecordBusinessStatusUIRule(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
		want string
	}{
		{
			// 流程状态压过内容判断:还等着人工填的记录,即使已填字段
			// 全是好的,也不能显示成「正常」—— 那会让人以为它巡完了。
			name: "标了人工填写的,优先显示人工填写",
			rec:  Record{ManualRequired: true, Fields: fieldsWith("正常")},
			want: "人工填写",
		},
		{
			name: "识别结果要求人工介入,同样是人工填写",
			rec:  Record{RecognitionStatus: "manual_required"},
			want: "人工填写",
		},
		{
			name: "要求补图的,显示需补图",
			rec:  Record{RecognitionStatus: "retake_required", Fields: fieldsWith("正常")},
			want: "需补图",
		},
		{
			name: "字段值里出现异常词 → 异常",
			rec:  Record{RecognitionStatus: "recognized", Fields: fieldsWith("正常", "压力表指针在红区,报警")},
			want: "异常",
		},
		{
			// 【只看 Value 不看 AIValue】人已经把 AI 的判断改过来了,
			// 状态就该跟着改。否则订正过的记录永远顶着"异常"。
			name: "AI 说异常但人已改成正常 → 不算异常",
			rec: Record{RecognitionStatus: "recognized", Fields: []FieldValue{
				{Code: "a", Value: "正常", AIValue: "破损", Source: "human-edited"},
			}},
			want: "正常",
		},
		{
			name: "有字段待复核且未提交 → 待复核",
			rec: Record{RecognitionStatus: "recognized", Fields: []FieldValue{
				{Code: "a", Value: "正常", NeedsReview: true},
			}},
			want: "待复核",
		},
		{
			// 提交 = 人看过并认可了,待复核的理由不再成立
			name: "已提交的,待复核标记不再让它停在待复核",
			rec: Record{Submitted: true, RecognitionStatus: "recognized", Fields: []FieldValue{
				{Code: "a", Value: "正常", NeedsReview: true},
			}},
			want: "已完成",
		},
		{
			name: "识别完成、有值、还没提交 → 正常",
			rec:  Record{RecognitionStatus: "recognized", Fields: fieldsWith("正常")},
			want: "正常",
		},
		{
			// 【最关键的一条兜底】什么都没有的记录不能叫「正常」——
			// 那等于告诉人"这里巡过了,没问题",而实际上一个字段都没填。
			name: "什么都没有的空记录 → 待复核,不是正常",
			rec:  Record{RecognitionStatus: "not_started"},
			want: "待复核",
		},
		{
			name: "已提交且一切正常 → 已完成",
			rec:  Record{Submitted: true, RecognitionStatus: "recognized", Fields: fieldsWith("正常")},
			want: "已完成",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			if got := recordBusinessStatus(&rec); got != tc.want {
				t.Errorf("界面口径:想要 %q,实际 %q", tc.want, got)
			}
		})
	}
}

// ===== 口径统一(2026-09-07)=====
//
// 后端曾经另有一份"日报口径"(management_ai.go 里),和界面那套【同名不同义】,
// 给管理 AI 的日报/周报和「重点关注」供数。同一条记录,后台页面上写着一个
// 状态、AI 日报里说的是另一个,而两处都叫"业务状态" —— 没人会去查代码,
// 只会当成数据出错。
//
// 现在已经统一到界面口径,那份实现删掉了。这个测试钉住当年差得最狠的
// 五种情形:它们现在只有一个答案。
//
// 【为什么选界面口径而不是反过来】最要命的是空记录那条 —— 一个字段都没填
// 的记录,日报口径算进「正常」,读日报的人以为这里巡过了没问题。
func TestStatusRuleUnifiedToUIRule(t *testing.T) {
	cases := []struct {
		name     string
		rec      Record
		want     string
		wasDaily string // 统一之前日报会算成什么
	}{
		{
			name: "空记录", rec: Record{RecognitionStatus: "not_started"},
			want: "待复核", wasDaily: "正常",
		},
		{
			name: "已提交且正常",
			rec:  Record{Submitted: true, RecognitionStatus: "recognized", Fields: fieldsWith("正常")},
			want: "已完成", wasDaily: "正常",
		},
		{
			name: "标了人工填写、字段里又有异常词",
			rec:  Record{ManualRequired: true, Fields: fieldsWith("阀门破损")},
			want: "人工填写", wasDaily: "异常",
		},
		{
			name: "已提交但仍有待复核字段",
			rec: Record{Submitted: true, RecognitionStatus: "recognized", Fields: []FieldValue{
				{Code: "a", Value: "正常", NeedsReview: true},
			}},
			want: "已完成", wasDaily: "待复核",
		},
		{
			name: "识别结果要求人工介入",
			rec:  Record{RecognitionStatus: "manual_required"},
			want: "人工填写", wasDaily: "待复核",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			if got := recordBusinessStatus(&rec); got != tc.want {
				t.Errorf("统一后应是 %q,实际 %q(统一前日报会算成 %q)", tc.want, got, tc.wasDaily)
			}
		})
	}
}

// 出站的每一条记录都必须带上界面状态 —— 前端不再自己算,漏一条就是空标签。
func TestSanitizeFillsBusinessStatus(t *testing.T) {
	rec := &Record{
		ID: "r1", TemplateID: "definitely_not_a_real_template",
		RecognitionStatus: "recognized", Fields: fieldsWith("正常"),
	}
	out := sanitizeRecordForCurrentTemplate(rec)
	if out.BusinessStatus != "正常" {
		t.Errorf("模板查不到时也要填状态,实际 %q", out.BusinessStatus)
	}
	// 【不能改到入参】模板查不到那条分支原来直接把入参还回去了。
	// 真在上面赋值的话,写的是 MemStore 里那条记录本身。
	if rec.BusinessStatus != "" {
		t.Errorf("序列化不该改动原记录,实际把 BusinessStatus 写成了 %q", rec.BusinessStatus)
	}
}

// 日报的"已巡检 / 没问题"两个数,不能因为多出一个状态就静默偏小。
//
// 【这正是统一口径时差点踩进去的坑】原来那行写的是
// 正常+异常+待复核+需补图+人工填写。统一之后多出「已完成」,而绝大多数
// 已提交的记录都会落到这个状态上 —— 不改的话,日报里的「已巡检」会突然
// 掉一大截,「正常」几乎归零,而系统一切正常、没有任何报错。
func TestSplitStatusCountsSurvivesNewStatus(t *testing.T) {
	counts := map[string]int{
		"已完成": 40, "正常": 5, "人工填写": 3,
		"异常": 2, "待复核": 4, "需补图": 1,
	}
	inspected, troubled, ok := splitStatusCounts(counts)
	if inspected != 55 {
		t.Errorf("已巡检应是全部 55 条,实际 %d", inspected)
	}
	if troubled != 7 {
		t.Errorf("有问题的应是 异常2+待复核4+需补图1=7,实际 %d", troubled)
	}
	if ok != 48 {
		t.Errorf("没问题的应是 55-7=48,实际 %d", ok)
	}

	// 【关键性质】将来再加一个状态,它必须自动进「已巡检」,
	// 而不是从总数里消失。
	counts["某个将来才有的状态"] = 6
	inspected2, troubled2, ok2 := splitStatusCounts(counts)
	if inspected2 != 61 {
		t.Errorf("新增状态必须自动计入已巡检(应 61),实际 %d —— "+
			"说明这里又变回了'把状态一个个加起来',以后每加一个状态都会漏", inspected2)
	}
	if troubled2 != troubled {
		t.Errorf("新状态不该被算成有问题的,实际 %d", troubled2)
	}
	if ok2 != 54 {
		t.Errorf("没问题的应是 61-7=54,实际 %d", ok2)
	}
}
