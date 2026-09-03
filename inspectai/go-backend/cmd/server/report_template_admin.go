package main

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
)

// ===== 模板编辑的校验 =====
//
// 【为什么校验必须在后端,不能只靠界面】改模板改的是"所有巡检员填什么、
// AI 提取什么、记录怎么存"。这里放过去的每一条,后果都落在历史数据上,
// 而且【全都不报错】—— 要等有人翻旧记录发现内容不见了才知道。
//
// 界面上禁用一个按钮只是提示;真正的约束在这一层。

var (
	errTplIDFormat    = errors.New("模板标识只能用小写字母、数字和下划线,且以字母开头")
	errTplNameEmpty   = errors.New("模板名称不能为空")
	errTplNoFields    = errors.New("模板至少要有一个字段")
	errTplDupCode     = errors.New("字段标识重复")
	errTplFieldCode   = errors.New("字段标识只能用小写字母、数字和下划线,且以字母开头")
	errTplFieldLabel  = errors.New("字段名称不能为空")
	errTplBadKind     = errors.New("字段类型只能是 text / number / choice")
	errTplNoOptions   = errors.New("单选字段必须至少有两个选项")
	errTplNoAssetNo   = errors.New("模板必须有「设备编号」字段(标识 asset_no),否则提交的记录挂不到任何设备上")
	errTplCodeChanged = errors.New("这个模板已经有巡检记录,字段标识不能再改、也不能删除 —— 改了历史记录里那一项就查不出来了")
)

var tplIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// assetBuilderTemplates 这几个模板不按 asset_no 认设备,而是代码里特判、
// 从读数字段拆出多台设备(见 buildZihanEnergyAssets / buildZihanDailyAssets)。
//
// 【所以它们不能要求有 asset_no】否则一保存就被校验拦下,而它们本来就没有
// 这个字段。第三阶段把"怎么认设备"也做成模板配置之后,这张表就能去掉。
var assetBuilderTemplates = map[string]bool{
	"zihan_energy": true,
	"zihan_daily":  true,
}

const assetNoFieldCode = "asset_no"

var validFieldKinds = map[string]bool{"text": true, "number": true, "choice": true}

// validateReportTemplate 保存前的静态校验(不看历史数据)。
func validateReportTemplate(t ReportTemplate) error {
	if !tplIDPattern.MatchString(t.ID) {
		return errTplIDFormat
	}
	if strings.TrimSpace(t.Name) == "" {
		return errTplNameEmpty
	}
	if len(t.Fields) == 0 {
		return errTplNoFields
	}

	seen := map[string]bool{}
	hasAssetNo := false
	for _, f := range t.Fields {
		if !tplIDPattern.MatchString(f.Code) {
			return errTplFieldCode
		}
		if seen[f.Code] {
			// 【重复的 code 是静默数据丢失】两个字段共用一个键,后填的会
			// 盖掉先填的,而表单上明明是两栏。
			return errTplDupCode
		}
		seen[f.Code] = true
		if strings.TrimSpace(f.Label) == "" {
			return errTplFieldLabel
		}
		if !validFieldKinds[f.Kind] {
			return errTplBadKind
		}
		if f.Kind == "choice" && len(f.Options) < 2 {
			// 只有一个选项的单选等于没得选,而它会被当成"填了"
			return errTplNoOptions
		}
		if f.Code == assetNoFieldCode {
			hasAssetNo = true
		}
	}
	if !hasAssetNo && !assetBuilderTemplates[t.ID] {
		return errTplNoAssetNo
	}
	return nil
}

// validateTemplateChange 有历史记录时,哪些改动还允许做。
//
// 允许:改中文标签、改必填、改选项、加新字段、调顺序。
// 不允许:改已有字段的 code、删已有字段 —— 记录里的字段值是按 code 存的,
// 这两样会让历史记录里那一项永远读不出来,而且不报错。
func validateTemplateChange(old, next ReportTemplate, recordCount int) error {
	if recordCount == 0 {
		return nil // 还没人用过,随便改
	}
	nextCodes := map[string]bool{}
	for _, f := range next.Fields {
		nextCodes[f.Code] = true
	}
	for _, f := range old.Fields {
		if !nextCodes[f.Code] {
			return errTplCodeChanged
		}
	}
	return nil
}

// ===== HTTP =====

// handleReportTemplateAdmin —— /api/report/templates/{id}
//
//	GET    取一份模板(带"能不能改 code"的判断依据)
//	PUT    保存(id 以路径为准,不认 body 里的)
//	DELETE 删除 —— 只删没被任何记录用过的
func (s *Server) handleReportTemplateAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "template_manage") {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/report/templates/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusBadRequest, "bad_id", "缺少模板标识")
		return
	}

	switch r.Method {
	case http.MethodGet:
		tpl, ok := templateByID(id)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "模板不存在")
			return
		}
		used, _ := s.store.CountRecordsUsingTemplate(id)
		writeJSON(w, http.StatusOK, map[string]any{
			"template": tpl,
			// recordCount 交给前端,是为了让界面【提前】把不能改的地方锁掉,
			// 而不是等人改完点保存才被拒 —— 那时候他已经白填一轮了。
			"recordCount": used,
			// 这个模板不按 asset_no 认设备(代码里特判),界面别提示缺字段
			"assetByBuilder": assetBuilderTemplates[id],
		})

	case http.MethodPut:
		var next ReportTemplate
		if err := decodeJSON(r, &next); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		next.ID = id // 【以路径为准】body 里带个别的 id 会存成另一个模板
		if err := validateReportTemplate(next); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_template", err.Error())
			return
		}
		old, exists := templateByID(id)
		if exists {
			used, err := s.store.CountRecordsUsingTemplate(id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "count_failed", err.Error())
				return
			}
			if err := validateTemplateChange(old, next, used); err != nil {
				writeError(w, http.StatusConflict, "unsafe_change", err.Error())
				return
			}
			// 【提交约束不归这个接口管】必填和照片张数是「提交规则」页的,
			// 这里一律沿用原值,不看请求里传了什么。
			//
			// 光在界面上把输入框藏起来不够:同一份数据有两个写入口,
			// 迟早会有一个把另一个的改动冲掉,而且不报错 —— 表现是
			// "我在那边改好的,过一会儿又变回去了",查起来极难。
			// 所以这条规则落在接口上。
			next = keepSubmissionRules(old, next)
		}
		if err := s.saveReportTemplate(next); err != nil {
			writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		s.recordOperation(r, "report_template_update", "report_template", id, map[string]any{
			"name": next.Name, "fields": len(next.Fields),
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "template": next})

	case http.MethodDelete:
		used, err := s.store.CountRecordsUsingTemplate(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "count_failed", err.Error())
			return
		}
		if used > 0 {
			// 【用过的只能停用,不能删】记录还指着这个模板,删了之后那些
			// 记录的字段定义就没了 —— 详情页会变成一堆没有名字的值。
			// 和用户、项目的删除是同一套规矩。
			writeError(w, http.StatusConflict, "template_in_use",
				"这个模板已经有巡检记录,不能删除。可以在编辑里改名或调整字段")
			return
		}
		if err := s.store.DeleteReportTemplate(id); err != nil {
			writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if err := loadReportTemplates(s.store); err != nil {
			logTemplateReloadFailure(err)
		}
		s.recordOperation(r, "report_template_delete", "report_template", id, nil)
		w.WriteHeader(http.StatusNoContent)

	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的方法")
	}
}

// handleCreateReportTemplate —— POST /api/report/templates
func (s *Server) handleCreateReportTemplate(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "template_manage") {
		return
	}
	var next ReportTemplate
	if err := decodeJSON(r, &next); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	next.ID = strings.TrimSpace(next.ID)
	if err := validateReportTemplate(next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_template", err.Error())
		return
	}
	if _, exists := templateByID(next.ID); exists {
		// 【重名的 id 会静默覆盖】upsert 是按 id 的,不拦的话新建一个同名
		// 模板等于把原来那个改掉,而原来那些记录的字段定义跟着变。
		writeError(w, http.StatusConflict, "template_exists", "这个模板标识已经被占用了")
		return
	}
	if err := s.saveReportTemplate(next); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	s.recordOperation(r, "report_template_create", "report_template", next.ID, map[string]any{
		"name": next.Name, "fields": len(next.Fields),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "template": next})
}

// saveReportTemplate 写库并立刻刷新缓存。
//
// 【刷新必须和保存绑在一起】漏了刷新的表现是"保存成功了但没生效",
// 人会反复点保存、反复确认自己填对了 —— 而问题在别处。
// 模板规则那一套(templateRuleCache)已经栽过这个坑。
func (s *Server) saveReportTemplate(t ReportTemplate) error {
	if err := s.store.UpsertReportTemplate(t); err != nil {
		return err
	}
	return loadReportTemplates(s.store)
}

func logTemplateReloadFailure(err error) {
	// 【刷新失败要留痕但不挡操作】缓存里还是上一份模板,系统照常能用;
	// 而把它当错误抛回去的话,人会以为删除没成功、再删一次。
	log.Printf("WARN: 模板缓存刷新失败,仍在使用上一份: %v", err)
}

// keepSubmissionRules 保留「提交规则」页管的那几项:必填、照片张数。
//
// 按 code 对应;新加的字段没有原值,保持请求里的样子(新字段默认非必填,
// 之后到提交规则页去配)。
func keepSubmissionRules(old, next ReportTemplate) ReportTemplate {
	next.MinImages = old.MinImages
	next.MaxImages = old.MaxImages
	wasRequired := map[string]bool{}
	for _, f := range old.Fields {
		wasRequired[f.Code] = f.Required
	}
	fields := make([]TemplateField, len(next.Fields))
	copy(fields, next.Fields)
	for i := range fields {
		if r, ok := wasRequired[fields[i].Code]; ok {
			fields[i].Required = r
		}
	}
	next.Fields = fields
	return next
}
