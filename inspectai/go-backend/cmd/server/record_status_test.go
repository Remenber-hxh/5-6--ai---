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

// ===== 两套口径的差异 =====
//
// 后端一直另有一份 recordDailyReportStatus(管理 AI 的日报/周报和重点关注
// 都用它),口径和界面那份【不一样】,而且以前两个函数同名。
//
// 后果是:同一条记录,后台页面上写着一个状态,AI 日报里说的是另一个 ——
// 而两处都叫"业务状态"。这种分歧没人会去查代码,只会当成数据出错。
//
// 【这个测试不是在批准这种分歧,是把它摆到明面上】现在它是有意为之的
// (统一口径会让日报里的历史数字集体变一遍,那是产品决定,不该夹在一次
// 重构里做掉)。谁将来去统一,这个测试会失败,逼他先看清楚差在哪几条。
func TestTwoStatusRulesDisagreeOnPurpose(t *testing.T) {
	cases := []struct {
		name         string
		rec          Record
		wantUI       string
		wantDaily    string
		whyItMatters string
	}{
		{
			name:         "空记录",
			rec:          Record{RecognitionStatus: "not_started"},
			wantUI:       "待复核",
			wantDaily:    "正常",
			whyItMatters: "日报会把一条都没填的记录算进「正常」,读日报的人以为这里巡过了",
		},
		{
			name:         "已提交且正常",
			rec:          Record{Submitted: true, RecognitionStatus: "recognized", Fields: fieldsWith("正常")},
			wantUI:       "已完成",
			wantDaily:    "正常",
			whyItMatters: "日报没有「已完成」这个状态,分不出「巡完并认可」和「还没人看过」",
		},
		{
			name:         "标了人工填写、字段里又有异常词",
			rec:          Record{ManualRequired: true, Fields: fieldsWith("阀门破损")},
			wantUI:       "人工填写",
			wantDaily:    "异常",
			whyItMatters: "同一条记录页面说「人工填写」、日报说「异常」,对账时对不上",
		},
		{
			name: "已提交但仍有待复核字段",
			rec: Record{Submitted: true, RecognitionStatus: "recognized", Fields: []FieldValue{
				{Code: "a", Value: "正常", NeedsReview: true},
			}},
			wantUI:       "已完成",
			wantDaily:    "待复核",
			whyItMatters: "日报的「待复核」里混进了已经复核完的,数字偏高",
		},
		{
			name:         "识别结果要求人工介入",
			rec:          Record{RecognitionStatus: "manual_required"},
			wantUI:       "人工填写",
			wantDaily:    "待复核",
			whyItMatters: "同一批记录在两处被归到不同的桶里",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			ui := recordBusinessStatus(&rec)
			daily := recordDailyReportStatus(&rec)
			if ui != tc.wantUI || daily != tc.wantDaily {
				t.Errorf("两套口径的结果变了(界面 %q→%q,日报 %q→%q)。\n"+
					"如果这是有意统一口径,请连同这个用例一起改,并确认日报里的历史数字会跟着变。\n"+
					"这条差异原本的影响:%s",
					tc.wantUI, ui, tc.wantDaily, daily, tc.whyItMatters)
			}
			if ui == daily {
				t.Errorf("这条用例本来就是用来记录差异的,现在两边一致了 —— "+
					"要么口径已统一(那就删掉这条用例),要么用例失去意义。影响:%s", tc.whyItMatters)
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
