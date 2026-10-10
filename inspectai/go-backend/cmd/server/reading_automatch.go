package main

import (
	"log"
	"strings"
)

// ===== 识别后先认表:读数和哪块表对得上,就挂到哪块表上 =====
//
// 【为什么】AI 按拍照顺序把照片填进 Z1~Z4 —— 照片上没有表号,拍照顺序一变,
// 读数就落进了别的表的格子。2026-10-11 紫菡:Z4、Z3 的读数读对了,却落在 Z1、Z2 两格,
// 和那两块表一比全"对不上",连同两个小数点错位的一起被清空 —— 四格全空,读对的也没了。
//
// 【按量级认得出来】紫菡几块表量级各不相同(电表 20 万、11 万、8 万、6 万;水表 2 千、1 百),
// 一个读的对的数只会和一块表对得上。"对得上"用的是量级检查同一个口径(readingProblem):
// 不比上一次小、不到 10 倍、涨幅不超过这块表平时的走法。
//
// 规则:
//  1. 这一格当前挂的表对得上 → 不动。人选过的、人填的、那块表没有历史判断不了的,也不动
//  2. 对不上,同类表里只有一块对得上、而且还没被别的格占着 → 挂到那块表上
//  3. 和哪块都对不上 → 清空读数、清掉设备,请人看照片选设备(这种多半连数也读错了,比如小数点)
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
	watched := map[string]string{}
	assetTypeOf := map[string]string{}
	for _, f := range tpl.Fields {
		if f.Kind == "number" && f.JudgeMode == ModeReadText && strings.TrimSpace(f.AssetType) != "" {
			watched[f.Code] = f.Label
			assetTypeOf[f.Code] = strings.TrimSpace(f.AssetType)
		}
	}
	if len(assetTypeOf) == 0 {
		return nil
	}
	candidates := meterCandidates(store, rec)
	if len(candidates) == 0 {
		return nil
	}
	current := meterBaselines(store, rec, tpl, watched)
	fits := func(v float64, mb meterBaseline) (fit, judged bool) {
		reason, judged := readingProblem(v, mb.Value, mb.Has, mb, true, "", readingDays(rec, mb))
		return judged && reason == "", judged
	}

	// 第一遍:当前挂的表对得上的、人动过的、判断不了的,都留在原地,占住那块表
	used := map[string]bool{}
	var pending []int
	for i := range rec.Fields {
		f := &rec.Fields[i]
		if _, ok := assetTypeOf[f.Code]; !ok {
			continue
		}
		cur, hasCur := current[f.Code]
		v, okv := parseReading(f.Value)
		// 【人选过的表不替他改】识别不会写设备名(applyRecognizedFields 不碰它),
		// 格子上有设备名、又不是按格子名猜的默认值,就是人选的 —— 对不上由后面的量级检查清空读数
		humanChosen := strings.TrimSpace(f.AssetName) != "" && !f.AssetDefaulted
		if !isAIReading(f) || !okv || humanChosen {
			if hasCur {
				used[cur.AssetName] = true
			}
			continue
		}
		if hasCur {
			if fit, judged := fits(v, cur); fit || !judged {
				used[cur.AssetName] = true
				continue
			}
		}
		pending = append(pending, i)
	}

	// 第二遍:对不上的,在同类表里找唯一一块对得上的
	var issues []readingSanityIssue
	for _, i := range pending {
		f := &rec.Fields[i]
		v, _ := parseReading(f.Value)
		typ := assetTypeOf[f.Code]
		hit, hits, anyHistory := "", 0, false
		for _, c := range candidates[typ] {
			if c.Has {
				anyHistory = true
			}
			if used[c.AssetName] {
				continue
			}
			if fit, _ := fits(v, c); fit {
				hit, hits = c.AssetName, hits+1
			}
		}
		raw := strings.TrimSpace(f.Value)
		switch {
		case hits == 1:
			log.Printf("自动认表 record=%s %s:读数 %s 和 %s 对得上,挂到 %s", rec.ID, f.Code, raw, hit, hit)
			f.AssetName, f.AssetDefaulted, f.AssetCleared = hit, false, false
			used[hit] = true
		case hits == 0 && anyHistory:
			// 和哪块表都对不上:读数多半也错了(小数点),设备也不知道是哪块 —— 都清掉,请人看照片
			issues = append(issues, readingSanityIssue{Code: f.Code, Label: watched[f.Code], Value: v,
				Reason: "和哪块表都对不上"})
			f.Value = ""
			f.NeedsReview = true
			f.Confidence = 0
			f.AssetName, f.AssetDefaulted, f.AssetCleared = "", false, true
			f.Reason = sanityNoteMark + "AI 读作 " + raw + ",请选择设备"
		}
	}
	return issues
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
