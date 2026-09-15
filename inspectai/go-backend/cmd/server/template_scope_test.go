package main

import (
	"net/http"
	"testing"
)

// ===== 模板必须跟着项目范围走 =====
//
// 【这组测试守的是什么】记录的项目跟着模板走。让人看到别的项目的模板,
// 等于让他往一个自己看不见的项目里提交 —— 提交成功,然后那条记录消失,
// 而且哪儿都不报错。这是最难查的一类:现场说"我明明交了",后台说"没有这条"。

// scopedToProject 造一个"只负责 project 这一个项目"的人。
// 另一个项目也建出来 —— 只有存在别的项目,"裁掉了没有"才测得出来。
func scopedToProject(t *testing.T, project, other string) (*Server, *http.Request) {
	t.Helper()
	srv, r, store, userID := newScopeRequestWithStore(t, roleSupervisor, dataScopeProject)
	mine := &Project{TenantID: defaultTenantID, Name: project}
	rest := &Project{TenantID: defaultTenantID, Name: other}
	for _, p := range []*Project{mine, rest} {
		if err := store.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetUserProjects(defaultTenantID, userID, []string{mine.ID}); err != nil {
		t.Fatal(err)
	}
	return srv, r
}

// 只负责一个项目的人,模板列表里不该出现别的项目的。
func TestTemplatesLimitedToVisibleProjects(t *testing.T) {
	isolateTemplateCache(t)
	srv, r := scopedToProject(t, "会议中心", "紫菡雅集")
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatal(err)
	}

	got := srv.templatesVisibleTo(r, reportTemplates())
	if len(got) == 0 {
		t.Fatal("一个模板都没有 —— 那他连拍照都开始不了")
	}
	for _, tpl := range got {
		if tpl.Project == "紫菡雅集" {
			t.Errorf("列出了别的项目的模板:%s —— 选了它,记录就落到看不见的项目里", tpl.ID)
		}
	}
	// 自己项目的必须还在
	var hasOwn bool
	for _, tpl := range got {
		if tpl.ID == "hot_water_room" {
			hasOwn = true
		}
	}
	if !hasOwn {
		t.Error("自己项目的模板被裁掉了 —— 拦错了比不拦更糟")
	}
}

// 主管/管理员看全部 —— 裁得太狠会让人干不了活。
func TestTemplatesFullWhenScopeIsAll(t *testing.T) {
	isolateTemplateCache(t)
	srv, r := newScopeRequest(t, roleAdmin, dataScopeAll)
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatal(err)
	}
	if got := srv.templatesVisibleTo(r, reportTemplates()); len(got) < 10 {
		t.Errorf("管理员只看到 %d 个模板,应该是全部", len(got))
	}
}

// 【最要紧的一条】界面裁过了不等于接口安全:客户端能直接把 templateID 发过来。
func TestCanUseTemplateBlocksOtherProject(t *testing.T) {
	isolateTemplateCache(t)
	srv, r := scopedToProject(t, "会议中心", "紫菡雅集")
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatal(err)
	}
	zihan, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("找不到 zihan_energy")
	}
	if srv.canUseTemplate(r, zihan) {
		t.Error("越项目建记录被放行了 —— 这条记录提交完就从他眼前消失了")
	}
	own, _ := templateByID("hot_water_room")
	if !srv.canUseTemplate(r, own) {
		t.Error("自己项目的模板被拦了")
	}
}

// 自动识别的候选和手选列表必须是同一份。
//
// 【不一致会怎样】AI 认出一个手选列表里没有的模板,界面显示"识别成功",
// 而人想换一个却找不到那一项 —— 两种状态互相矛盾,现场只会觉得系统坏了。
func TestSceneCandidatesMatchVisibleTemplates(t *testing.T) {
	isolateTemplateCache(t)
	srv, r := scopedToProject(t, "会议中心", "紫菡雅集")
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatal(err)
	}
	visible := map[string]bool{}
	for _, tpl := range srv.templatesVisibleTo(r, reportTemplates()) {
		visible[tpl.ID] = true
	}
	cands := srv.sceneCandidatesFor(r)
	if len(cands) == 0 {
		t.Fatal("一个候选都没有 —— 自动识别会整个失效")
	}
	for _, c := range cands {
		if !visible[c.TemplateID] {
			t.Errorf("候选里有个手选列表看不到的模板:%s", c.TemplateID)
		}
	}
}

// 配了项目范围却一个项目都没分到 → 什么都不给用(fail-closed)。
//
// 【为什么不是放行全部】判断不了他属于哪个项目,就不该让他往任何项目写数据。
// 给空列表界面会提示"没有可用模板",那是个能查的现象;
// 放行全部则是一条安静的越权通道。
func TestTemplatesBlockedWhenNoProjectAssigned(t *testing.T) {
	isolateTemplateCache(t)
	srv, r := newScopeRequest(t, roleSupervisor, dataScopeProject)
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatal(err)
	}
	if got := srv.templatesVisibleTo(r, reportTemplates()); len(got) != 0 {
		t.Errorf("一个项目都没分到却给了 %d 个模板", len(got))
	}
}
