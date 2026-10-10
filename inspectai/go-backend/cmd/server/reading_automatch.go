package main

import (
	"log"
	"strings"
)

// ===== 识别后先认表:读数和哪块表对得上,就放进哪块表那一格 =====
//
// 【为什么】AI 按拍照顺序把照片填进 Z1~Z4 —— 照片上没有表号,拍照顺序一变,
// 读数就落进了别的表的格子。2026-10-11 紫菡:Z4、Z3 的读数读对了,却落在 Z1、Z2 两格,
// 和那两块表一比全"对不上",连同两个小数点错位的一起被清空 —— 四格全空,读对的也没了。
//
// 【按量级认得出来】紫菡几块表量级各不相同(电表 20 万、11 万、8 万、6 万;水表 2 千、1 百),
// 一个读的对的数只会和一块表对得上。"对得上"用的是量级检查同一个口径(readingProblem)。
//
// 【是对调进那块表自己的格子,不是给格子改个设备名】日报按格子名列读数("Z4 能耗表读数"),
// 手机端选设备也是把读数搬进那块表自己的那一格(RecordPage pickAssetForPhoto)。
// 只改设备名的话,"Z1 能耗表读数"那一格里装着 Z4 的数,日报写错表;人再手动选表时
// 还要来回对调好几次 —— 2026-10-11 第一版就是这么错的。照片、置信度、理由跟着读数一起走。
//
// 规则:
//  1. 这一格的读数和这一格的表对得上 → 不动。人选过的、人填的、那块表没有历史判断不了的,也不动
//  2. 对不上,同类表里只有一块对得上、那块表的格子还没被占住 → 和那一格对调;换出来的数再认一遍
//  3. 最后还对不上的 → 清空读数、清掉设备,请人看照片选设备(多半连数也读错了,比如小数点)
//  4. 不止一块对得上,或者同类表都没有历史 → 不猜,交给后面的量级检查
//
// 只在识别后跑一次。人改过设备之后不再替他改 —— 换表后的重查走 recheckReadingSanity。
func autoMatchMeters(store Store, rec *Record) []readingSanityIssue {
	if store == nil || rec == nil {
		return nil
	}
	tpl, ok := templateByID(rec.TemplateID)
	if !ok {
		return nil
	}
	labelOf := map[string]string{}
	typeOf := map[string]string{}
	for _, f := range tpl.Fields {
		if f.Kind == "number" && f.JudgeMode == ModeReadText && strings.TrimSpace(f.AssetType) != "" {
			labelOf[f.Code] = f.Label
			typeOf[f.Code] = strings.TrimSpace(f.AssetType)
		}
	}
	if len(typeOf) == 0 {
		return nil
	}
	candidates := meterCandidates(store, rec)
	if len(candidates) == 0 {
		return nil
	}
	stats := map[string]meterBaseline{} // 表名 -> 上一次读数和走法
	names := map[string][]string{}      // 类型 -> 表名
	for typ, list := range candidates {
		for _, mb := range list {
			stats[mb.AssetName] = mb
			names[typ] = append(names[typ], mb.AssetName)
		}
	}

	// 每一格是哪块表的家(和确认页、日报同一个口径:按格子名认)
	slots := map[string]int{}   // 表名 -> 它自己那一格
	meterOf := map[int]string{} // 格 -> 这一格现在算哪块表
	locked := map[int]bool{}    // 不许再动的格
	for i := range rec.Fields {
		f := &rec.Fields[i]
		typ, ok := typeOf[f.Code]
		if !ok {
			continue
		}
		if home := defaultAssetForField(f.Label, names[typ]); home != "" {
			slots[home] = i
			meterOf[i] = home
		}
		// 【人选过的表不替他改】识别不会写设备名,格子上有、又不是猜的默认值,就是人选的
		if name := strings.TrimSpace(f.AssetName); name != "" && !f.AssetDefaulted {
			meterOf[i] = name
			locked[i] = true
		}
		if !isAIReading(f) {
			locked[i] = true // 人填的数不动
		}
	}
	fitsMeter := func(v float64, name string) (fit, judged bool) {
		mb, ok := stats[name]
		if !ok {
			return false, false
		}
		reason, judged := readingProblem(v, mb.Value, mb.Has, mb, true, "", readingDays(rec, mb))
		return judged && reason == "", judged
	}
	// 这一格的数和这一格的表对得上(或者判断不了)→ 锁住
	settle := func(i int) bool {
		f := &rec.Fields[i]
		v, ok := parseReading(f.Value)
		if !ok {
			return false
		}
		fit, judged := fitsMeter(v, meterOf[i])
		if fit || !judged {
			locked[i] = true
			return true
		}
		return false
	}

	var queue []int
	for i := range rec.Fields {
		if _, ok := typeOf[rec.Fields[i].Code]; !ok || locked[i] {
			continue
		}
		if !settle(i) {
			if _, ok := parseReading(rec.Fields[i].Value); ok {
				queue = append(queue, i)
			}
		}
	}

	// 对不上的,在同类表里找唯一一块对得上、格子还空着手的表,和那一格对调
	var leftover []int
	for steps := 0; len(queue) > 0 && steps < 4*len(rec.Fields); steps++ {
		i := queue[0]
		queue = queue[1:]
		f := &rec.Fields[i]
		v, ok := parseReading(f.Value)
		if !ok {
			continue // 换进来的是个空格子
		}
		hit, hits := "", 0
		for _, name := range names[typeOf[f.Code]] {
			j, has := slots[name]
			if !has || j == i || locked[j] {
				continue
			}
			if fit, _ := fitsMeter(v, name); fit {
				hit, hits = name, hits+1
			}
		}
		if hits != 1 {
			leftover = append(leftover, i)
			continue
		}
		j := slots[hit]
		log.Printf("自动认表 record=%s 读数 %s 和 %s 对得上:%s ↔ %s", rec.ID, strings.TrimSpace(f.Value), hit, f.Code, rec.Fields[j].Code)
		swapReadingPayloadKeepSource(&rec.Fields[i], &rec.Fields[j])
		locked[j] = true
		// 换到这一格来的数,按这一格的表再认一遍
		if !settle(i) {
			if _, ok := parseReading(rec.Fields[i].Value); ok {
				queue = append(queue, i)
			}
		}
	}

	// 和哪块表都对不上:读数多半也错了,设备也不知道是哪块 —— 都清掉,请人看照片选设备
	var issues []readingSanityIssue
	anyHistory := map[string]bool{}
	for typ, list := range candidates {
		for _, mb := range list {
			if mb.Has {
				anyHistory[typ] = true
			}
		}
	}
	for _, i := range leftover {
		f := &rec.Fields[i]
		v, ok := parseReading(f.Value)
		if !ok || locked[i] || !anyHistory[typeOf[f.Code]] {
			continue
		}
		if fit, judged := fitsMeter(v, meterOf[i]); fit || !judged {
			continue // 换来换去之后反而对上了
		}
		raw := strings.TrimSpace(f.Value)
		issues = append(issues, readingSanityIssue{Code: f.Code, Label: labelOf[f.Code], Value: v, Reason: "和哪块表都对不上"})
		f.Value = ""
		f.NeedsReview = true
		f.Confidence = 0
		f.AssetName, f.AssetDefaulted, f.AssetCleared = "", false, true
		f.Reason = sanityNoteMark + "AI 读作 " + raw + ",请选择设备"
	}
	return issues
}

// swapReadingPayloadKeepSource 两格对调"这一次抄到的东西"(读数、照片、置信度、理由……),
// 来源跟着读数走 —— 这是系统按读数认表,不是人做的修改,不能记成人改的。
func swapReadingPayloadKeepSource(a, b *FieldValue) {
	sa, sb := a.Source, b.Source
	swapFieldPayload(a, b)
	a.Source, b.Source = sb, sa
}

// meterCandidates 这条记录所在项目里,每种表各有哪几块、各自上一次读数和平时的走法。
// 同名两台的跳过 —— 不知道是哪一台,宁可不认。
func meterCandidates(store Store, rec *Record) map[string][]meterBaseline {
	assets, err := store.ListAssets(rec.TenantID)
	if err != nil {
		return nil
	}
	byKey := map[string][]*AssetEntry{}
	for _, a := range assets {
		if a == nil {
			continue
		}
		at, name := strings.TrimSpace(a.AssetType), strings.TrimSpace(a.AssetName)
		if at == "" || name == "" || (rec.Project != "" && a.Project != "" && a.Project != rec.Project) {
			continue
		}
		byKey[at+"|"+name] = append(byKey[at+"|"+name], a)
	}
	readingFields := readingFieldsOf(rec.TemplateID)
	out := map[string][]meterBaseline{}
	for key, list := range byKey {
		if len(list) != 1 {
			continue
		}
		mb := lastMeterReading(store, list[0].ID, rec.ID, readingFields)
		mb.AssetName = strings.TrimSpace(list[0].AssetName)
		typ := key[:strings.Index(key, "|")]
		out[typ] = append(out[typ], mb)
	}
	return out
}
