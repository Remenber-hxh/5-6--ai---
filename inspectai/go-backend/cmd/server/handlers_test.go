package main

import "testing"

// P0-1 修复后的回归保护:不能再让"不正常 / 不合格 / 看不清"被误判为"正常"。
func TestNormalizeChoiceValue(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		options []string
		want    string
	}{
		// 精确匹配最优先
		{"exact 正常", "正常", []string{"正常", "异常"}, "正常"},
		{"exact 异常", "异常", []string{"正常", "异常"}, "异常"},

		// P0 修复点 —— 否定词不能误判
		{"否定: 不正常 → 异常", "不正常", []string{"正常", "异常"}, "异常"},
		{"否定: 不合格 → 异常", "不合格", []string{"正常", "异常"}, "异常"},
		{"否定: 不通过 → 异常", "不通过", []string{"正常", "异常"}, "异常"},

		// 模糊词 → 待复核
		{"模糊: 看不清 → 待复核", "看不清", []string{"正常", "异常", "待复核"}, "待复核"},
		{"模糊: 无法判定 → 待复核", "无法判定", []string{"正常", "异常", "待复核"}, "待复核"},

		// 正向同义词
		{"同义: 无异常 → 正常", "无异常", []string{"正常", "异常"}, "正常"},
		{"同义: 良好 → 正常", "良好", []string{"正常", "异常"}, "正常"},
		{"同义: 完好 → 完好", "良好", []string{"完好", "破损", "缺失"}, "完好"},

		// 状态字段
		{"破损 → 破损", "有破裂", []string{"完好", "破损", "缺失"}, "破损"},
		{"缺失 → 缺失", "丢失", []string{"完好", "破损", "缺失"}, "缺失"},

		// 没有合适选项时,留原值
		{"无匹配: 异常但选项无异常", "不合格", []string{"良好", "完好"}, "不合格"},

		// 空字符串
		{"空", "", []string{"正常", "异常"}, ""},

		// 是 / 否
		{"yes → 是", "yes", []string{"是", "否"}, "是"},
		{"no → 否", "no", []string{"是", "否"}, "否"},

		// 报警类异常词
		{"有报警 → 异常", "有报警", []string{"正常", "异常"}, "异常"},
		{"漏水 → 异常", "存在漏水", []string{"正常", "异常"}, "异常"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeChoiceValue(tc.raw, tc.options)
			if got != tc.want {
				t.Errorf("normalizeChoiceValue(%q, %v) = %q, want %q", tc.raw, tc.options, got, tc.want)
			}
		})
	}
}
