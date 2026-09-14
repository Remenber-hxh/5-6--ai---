package main

import "testing"

// 巡检地点必须由系统填,不能留给 AI 猜。
//
// 【这条测的是一次真实事故】2026-09-10 17:30 那条紫菡能耗记录:AI 把四块表
// 都读对了(60197.924 / 84363.520 / 20184.018 / 1992),但没返回 site ——
// 而 site 是 zihan_energy 里唯一 required+source=ai 的字段,
// checkRecognitionFailure 一看 gotRequired==0 就判"关键字段全部为空,请重拍",
// 把读对的四个读数连同整条记录一起作废。
//
// 根子在"让 AI 去猜一个系统已知的常量":照片里根本没有"紫菡雅集"这几个字,
// 模型返回它靠的是从提示词正文里拼,时灵时不灵。
func TestSiteIsFilledBySystemNotAI(t *testing.T) {
	for _, id := range []string{"zihan_energy", "zihan_daily"} {
		tpl, ok := templateByID(id)
		if !ok {
			t.Fatalf("找不到模板 %s", id)
		}
		fields := initialFieldValues(tpl, "巡检员", "紫菡雅集")
		f, _ := fieldByCode(fields, "site")
		if f == nil {
			t.Fatalf("%s 没有 site 字段", id)
		}
		if f.Value != "紫菡雅集" {
			t.Errorf("%s 的巡检地点没被预填,值=%q", id, f.Value)
		}
		if f.Source == "ai" {
			t.Errorf("%s 的巡检地点还挂在 AI 名下 —— 它一次没返回就会作废整条记录", id)
		}
		if f.NeedsReview {
			t.Errorf("%s 系统填好的巡检地点不该再要人工复核", id)
		}
	}
}

// 没传地点时(老调用方、测试)要退回原来的样子,不能凭空把字段标成已填。
func TestSiteWithoutProjectStaysEmpty(t *testing.T) {
	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("找不到 zihan_energy")
	}
	fields := initialFieldValues(tpl, "巡检员")
	f, _ := fieldByCode(fields, "site")
	if f == nil {
		t.Fatal("没有 site 字段")
	}
	if f.Value != "" {
		t.Errorf("没给地点却填了值:%q", f.Value)
	}
}

// 预填地点之后,zihan_energy 不该再有"一个字段没返回就全盘作废"的单点。
//
// 【为什么单独守这条】checkRecognitionFailure 的判据是
// "required && source==ai 的字段一个都没返回 → 重拍"。
// 这类字段只要还剩一个、而且它不是从照片读的,同样的事故就会复现。
func TestNoNonVisualFieldGatesTheWholeRecord(t *testing.T) {
	for _, id := range []string{"zihan_energy", "zihan_daily"} {
		tpl, ok := templateByID(id)
		if !ok {
			t.Fatalf("找不到模板 %s", id)
		}
		for _, f := range tpl.Fields {
			if f.Required && f.Source == "ai" && f.Code == "site" {
				t.Errorf("%s 的巡检地点仍是 required+ai —— 它读不出来会作废整条记录", id)
			}
		}
	}
}
