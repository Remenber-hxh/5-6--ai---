package main

import "net/http"

// ===== 模板也要按项目范围裁 =====
//
// 【原来漏的是什么】设备、记录、审批都按可见项目裁过了,只有模板没有:
//   - 列模板的接口(GET /api/report/templates)返回全部 10 个
//   - 场景分类的候选表(sceneCandidates)也是全部 10 个
//
// 后果不是"多看见几个名字"。现场拍完照,AI 认不出就转手动选,列表里摆着
// 别的项目的模板;而记录的项目跟着模板走 —— 只看紫菡的人选了「热水机房」,
// 这条记录就落到会议中心名下,然后【他自己看不见了】。
// 提交成功,然后消失,而且哪儿都不报错。
//
// AI 那一侧更隐蔽:紫菡的照片和会议中心那 8 个模板一起比对,认成别的项目的
// 模板时界面显示"识别成功",现场按另一套判定规则巡检。
//
// 【为什么收成一个函数】这两处的判据必须一模一样。分开写的话,以后加一个
// 入口(比如扫码进模板)又会漏掉第三处 —— 而漏掉的那处不报错。

// templatesVisibleTo 这个请求能用的模板。
//
// 【看不到任何项目时返回空,不是返回全部】fail-closed:判断不了他属于哪个
// 项目,就不该让他往任何项目里写数据。给空列表界面会提示"没有可用模板",
// 那是个能查的现象;放行全部则是一条安静的越权通道。
func (s *Server) templatesVisibleTo(r *http.Request, all []ReportTemplate) []ReportTemplate {
	vis := s.visibilityFor(r)
	if vis.AllData {
		return all
	}
	if vis.Blocked {
		return nil
	}
	// 【没配项目范围 = 不按项目限】和设备那边同一口径(limitAssetsToVisibleProjects):
	// 只按"自己提交的"限的人,项目不设限,否则他连拍照都开始不了。
	if len(vis.Projects) == 0 {
		return all
	}
	out := make([]ReportTemplate, 0, len(all))
	for _, t := range all {
		// 【没写项目的模板放行】它不属于任何项目,裁掉的话等于凭空消失,
		// 而现场看不出是被过滤了还是模板没配好。
		if t.Project == "" || vis.allowsProject(t.Project) {
			out = append(out, t)
		}
	}
	return out
}

// canUseTemplate 这个请求能不能拿这个模板建记录。
//
// 【和列表用同一个判据,但拦在更里面】客户端能直接把 templateID 发过来,
// 界面裁过了不等于这里安全。同一件事有两个入口时,边界要落在最里面那个。
func (s *Server) canUseTemplate(r *http.Request, tpl ReportTemplate) bool {
	for _, t := range s.templatesVisibleTo(r, []ReportTemplate{tpl}) {
		if t.ID == tpl.ID {
			return true
		}
	}
	return false
}

// sceneCandidatesFor 这个请求能参与自动识别的场景。
//
// 和手选那份用【同一个】过滤 —— 两边口径不一致的话会出现
// "AI 认出来了,但手选列表里没有这个模板"这种自相矛盾的状态。
func (s *Server) sceneCandidatesFor(r *http.Request) []SceneCandidate {
	allowed := map[string]bool{}
	for _, t := range s.templatesVisibleTo(r, reportTemplates()) {
		allowed[t.ID] = true
	}
	all := sceneCandidates()
	out := make([]SceneCandidate, 0, len(all))
	for _, c := range all {
		if allowed[c.TemplateID] {
			out = append(out, c)
		}
	}
	return out
}
