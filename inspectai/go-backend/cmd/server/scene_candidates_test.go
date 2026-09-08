package main

import (
	"encoding/json"
	"testing"
)

// 候选表从库里来 —— 这是「后台新建的模板 AI 认不出来」的正面用例。
//
// 以前候选清单写死在 ai-service 的提示词里,后台建的模板不在清单里,
// 模型返回不了它的 id。
func TestSceneCandidatesIncludeNewTemplates(t *testing.T) {
	isolateTemplateCache(t)
	setReportTemplateCache([]ReportTemplate{
		{ID: "elevator_machine_room", Name: "电梯巡检（有机房）", SceneFeatures: "独立机房:曳引机、控制柜"},
		{ID: "tpl_new", Name: "生活水泵房巡检", SceneFeatures: "蓝色压力罐 + 银色管道 + 压力表"},
	})

	got := sceneCandidates()
	if len(got) != 2 {
		t.Fatalf("两个模板都该进候选,实际 %d 个:%+v", len(got), got)
	}
	var found *SceneCandidate
	for i := range got {
		if got[i].TemplateID == "tpl_new" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatal("后台新建的模板必须出现在候选里 —— 不在的话现场拍完照永远匹配不到它")
	}
	if found.Features == "" || found.TemplateName != "生活水泵房巡检" {
		t.Errorf("候选要带上名字和识别特征,实际 %+v", *found)
	}
}

// 没有识别特征时退而用场景描述;两个都空就不进候选。
//
// 【为什么空的不能进】模型挑场景靠的是"照片上能看到什么"。一个连描述都
// 没有的模板塞进清单,只会让它去抢别的场景的照片 —— 而抢错了不报错。
func TestSceneCandidatesFallBackToSceneAndSkipEmpty(t *testing.T) {
	isolateTemplateCache(t)
	setReportTemplateCache([]ReportTemplate{
		{ID: "with_feat", Name: "有特征", SceneFeatures: "红色泵体 + 绿色地坪"},
		{ID: "only_scene", Name: "只有场景描述", Scene: "热水机房:控制柜与水箱"},
		{ID: "empty", Name: "什么都没填"},
	})

	got := sceneCandidates()
	byID := map[string]SceneCandidate{}
	for _, c := range got {
		byID[c.TemplateID] = c
	}
	if _, ok := byID["empty"]; ok {
		t.Error("没有任何描述的模板不该进候选 —— 它会去抢别的场景的照片")
	}
	if c, ok := byID["only_scene"]; !ok {
		t.Error("没填识别特征时应退而用场景描述,不该直接排除")
	} else if c.Features != "热水机房:控制柜与水箱" {
		t.Errorf("回退时应带上场景描述,实际 %q", c.Features)
	}
	if c := byID["with_feat"]; c.Features != "红色泵体 + 绿色地坪" {
		t.Errorf("有识别特征时应优先用它,实际 %q", c.Features)
	}
}

// 模型返回的 id 必须在这次下发的候选里。
//
// 【原来只是拿 id 去库里查名字,查得到就认】于是模型凭记忆吐出一个
// 【存在但这次没给它】的 id(停用的、别的项目的、提示词举例用的),
// 会被当成有效结果 —— 界面显示"识别成功",现场按另一套判定规则巡检。
func TestResolveSceneResultRejectsIDOutsideCandidates(t *testing.T) {
	candidates := []SceneCandidate{
		{TemplateID: "a", TemplateName: "甲模板"},
		{TemplateID: "b", TemplateName: "乙模板"},
	}

	t.Run("候选内的照常认,名字以库里为准", func(t *testing.T) {
		r := &SceneClassifyResult{TemplateID: "a", TemplateName: "模型自己编的名字", Confidence: 0.9}
		resolveSceneResult(r, candidates)
		if r.TemplateID != "a" {
			t.Errorf("候选内的应保留,实际 %q", r.TemplateID)
		}
		if r.TemplateName != "甲模板" {
			t.Errorf("名字要以库里为准(这个名字会显示给现场看),实际 %q", r.TemplateName)
		}
		if r.NeedsManualPick {
			t.Error("认出来了就不该再要求手动选")
		}
	})

	t.Run("候选外的一律当没认出来", func(t *testing.T) {
		r := &SceneClassifyResult{TemplateID: "zihan_energy", TemplateName: "能耗抄表", Confidence: 0.95}
		resolveSceneResult(r, candidates)
		if r.TemplateID != "unknown" {
			t.Errorf("候选外的 id 必须当成没认出来,实际 %q —— "+
				"放行的话现场会按另一套规则巡检,而且没有任何提示", r.TemplateID)
		}
		if !r.NeedsManualPick {
			t.Error("没认出来就必须让人手动选")
		}
	})

	t.Run("模型返回 unknown 时也要求手动选", func(t *testing.T) {
		r := &SceneClassifyResult{TemplateID: "unknown"}
		resolveSceneResult(r, candidates)
		if !r.NeedsManualPick {
			t.Error("unknown 必须转人工")
		}
	})
}

// 代码里的默认模板也要带上识别特征。
//
// 【为什么单独钉】全新部署时模板是按代码默认值种进库的,不走迁移回填那条路。
// 漏了的话:新装的环境一个场景都认不出来,而老环境是好的 ——
// 极难想到是这个原因。
func TestDefaultTemplatesCarrySceneFeatures(t *testing.T) {
	isolateTemplateCache(t)
	for _, tpl := range defaultReportTemplates() {
		want, ok := builtinSceneFeatures[tpl.ID]
		if !ok {
			continue // 不在内置清单里的模板,没有特征是正常的
		}
		if tpl.SceneFeatures != want {
			t.Errorf("模板 %s 的识别特征应为内置那份,实际 %q", tpl.ID, tpl.SceneFeatures)
		}
	}
}

// 候选表下发给 ai-service 的 JSON 字段名。
//
// 【为什么值得单独钉】ai-service 那边按 templateId / templateName / features
// 三个键读(run.py 的 render_scene_prompt)。这边把哪个键改个名,那边读到的
// 就全是空 —— 而它的处理是【静默回退到内置的那张写死的表】。
//
// 于是表现成:一切正常,只是后台新建的模板又认不出来了。没有报错、
// 没有日志、测试也全绿 —— 正是这一整件事要修的那个 bug 原样长回来。
func TestSceneCandidateJSONKeysMatchAIService(t *testing.T) {
	b, err := json.Marshal(SceneCandidate{
		TemplateID: "x", TemplateName: "甲", Features: "红泵+绿地坪",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	// 改这三个名字的话,ai-service/run.py 的 render_scene_prompt 要一起改
	for _, key := range []string{"templateId", "templateName", "features"} {
		if _, ok := got[key]; !ok {
			t.Errorf("下发的 JSON 缺 %q —— ai-service 会读到空值并静默回退到"+
				"内置的写死候选表,新建的模板又认不出来了(实际键:%v)", key, got)
		}
	}
	if len(got) != 3 {
		t.Errorf("多出了字段:%v —— ai-service 只认那三个,多的会被忽略,"+
			"以为加上了其实没生效", got)
	}
}
