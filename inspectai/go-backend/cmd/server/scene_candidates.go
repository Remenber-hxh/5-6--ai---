package main

import "strings"

// ===== 场景分类的候选表 =====
//
// 拍完照自动认出"这是哪一类巡检",然后自动选模板。候选清单以前写死在
// ai-service/prompts/scene_classifier.md 里,提示词明写着"必须从这个清单选
// 一个" —— 后台新建的模板不在清单里,模型返回不了它的 id。
//
// 【更糟的不是认不出,是认错】模型被要求必须选一个,于是它会挑一个最像的
// 旧模板返回。界面显示"识别成功",用的却是另一套判定规则 ——
// 没有任何地方提示这次匹配是错的。
//
// 现在候选表从库里生成、随请求下发。后台建模板 = 现场立刻认得出来,
// 不用改代码、不用发版。

// sceneCandidates 当前所有能被自动匹配的场景。
//
// 【没有任何描述的模板不进候选】模型挑场景靠的是"照片上能看到什么"。
// 一个连场景描述都没有的模板塞进清单,只会让它去抢别人的照片 ——
// 而抢错了不会报错。留空就是"暂不参与自动匹配",这是个合理的默认。
func sceneCandidates() []SceneCandidate {
	tpls := reportTemplates()
	out := make([]SceneCandidate, 0, len(tpls))
	for _, t := range tpls {
		feat := strings.TrimSpace(t.SceneFeatures)
		if feat == "" {
			// 【退而用场景描述】它写的是"要去哪、查什么",不如专门的识别
			// 特征准(消防泵房和生活水泵房要拍的东西几乎一样,真正能分开的
			// 是颜色组合),但总比这个模板永远匹配不上强。
			feat = strings.TrimSpace(t.Scene)
		}
		if feat == "" {
			continue
		}
		out = append(out, SceneCandidate{
			TemplateID:   t.ID,
			TemplateName: t.Name,
			Features:     feat,
		})
	}
	return out
}

// resolveSceneResult 把模型的返回收进"这次到底给了哪个模板"。
//
// 【必须校验返回的 id 在这次下发的候选里】原来只是拿 id 去库里查名字:
// 查得到就认。于是模型凭记忆吐出一个【存在但这次没给它】的 id
// (停用的、别的项目的、甚至提示词里举例用的),会被当成有效结果 ——
// 界面显示"识别成功",现场按另一套规则巡检。
//
// 不在候选里 = 当作没认出来,让人手动选。少一次自动化,好过一次错的自动化。
func resolveSceneResult(result *SceneClassifyResult, candidates []SceneCandidate) {
	if result == nil {
		return
	}
	allowed := make(map[string]string, len(candidates))
	for _, c := range candidates {
		allowed[c.TemplateID] = c.TemplateName
	}
	name, ok := allowed[result.TemplateID]
	if !ok {
		result.TemplateID = "unknown"
		result.TemplateName = "无法识别"
		result.NeedsManualPick = true
		return
	}
	// 名字以库里的为准 —— 模型可能把中文名说岔,而这个名字会显示给现场看
	result.TemplateName = name
}
