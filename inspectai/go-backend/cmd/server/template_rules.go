package main

import (
	"fmt"
	"net/http"
	"strings"
)

// ===== 模板字段的必填/选填配置 =====
//
// 模板本身写死在 templates.go 里(10 个模板、120 个字段)。要调整某个字段的
// 必填/选填,原来只能改代码重新部署 —— 而这是业务规则,不该每次都排一次上线。
//
// 【只覆盖"必填"这一项,不把模板整体搬进数据库】
// 整体搬库要动字段类型、选项、AI 提示词、顺序……是一次大改,风险高。
// 而实际的诉求就是"这一项到底填不填"。所以做一层薄薄的覆盖:
// 模板照旧从代码里来,只有 required 允许被数据库里的配置覆写。
//
// 【收口在 reportTemplates() 一个地方】全仓只有它和 templateByID 两处取模板,
// 而后者也走它。覆盖做在这里,下游十几处调用自动生效 —— 不用一处处改,
// 也就不会漏一处(权限那一轮的教训:散着改必然漏,而漏掉的那处不报错)。

// lockedRequiredFields 不允许改成选填的字段。
//
// asset_no 是资产台账的主键:记录靠它认归属。放开成选填之后,提交时不填 →
// 这条记录挂不到任何设备上,台账里既看不到这次巡检、也不知道少了谁。
// 而且错误发生在提交那一刻,要过很久对账时才发现。
//
// 这不是"建议",是硬约束 —— 后台不给开关,接口也拒绝。
var lockedRequiredFields = map[string]bool{
	"asset_no": true,
}

// ===== 覆盖层已撤(迁移 027)=====
//
// 这里原来有一套 templateRuleCache / applyTemplateRules / loadTemplateRules:
// 模板从代码里来,只有 required 和 minImages 允许被数据库里的配置覆写。
//
// 【为什么整套删掉而不是留着不用】它存在的前提是"模板写死在代码里,搬库风险高",
// 而搬库已经做完(025/026)。留着一段不再被调用的覆盖逻辑,下一个人排查
// "必填不生效"时很可能顺手把它接回去 —— 而两个存储一旦并存,
// 覆盖层就会盖住底表,在模板页改的必填被静默盖回去。那个 bug 刚修完。
//
// template_field_rules / template_settings 两张表还在写(只为回退时对照),
// 但【不再有任何读取路径】。

// TemplateFieldRule 一条配置。存库用。
type TemplateFieldRule struct {
	TemplateID string `json:"templateId"`
	FieldCode  string `json:"fieldCode"`
	Required   bool   `json:"required"`
	UpdatedAt  string `json:"updatedAt,omitempty"`
	UpdatedBy  string `json:"updatedBy,omitempty"`
}

// TemplateRuleStore — 模板字段规则的读写
type TemplateRuleStore interface {
	ListTemplateFieldRules() ([]*TemplateFieldRule, error)
	// ListTemplateSettings 模板级设置:templateID -> 每单最少几张照片。
	ListTemplateSettings() (map[string]int, error)
	SetTemplateMinImages(templateID string, minImages int, operator string) error
	// ReplaceTemplateFieldRules 覆盖某个模板的全部字段规则(一次保存一个模板)。
	ReplaceTemplateFieldRules(templateID string, rules []*TemplateFieldRule) error
}


// ===== SQLiteStore =====

func (s *SQLiteStore) ListTemplateFieldRules() ([]*TemplateFieldRule, error) {
	rows, err := s.db.Query(
		`SELECT template_id, field_code, required, updated_at, updated_by FROM template_field_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*TemplateFieldRule{}
	for rows.Next() {
		r := &TemplateFieldRule{}
		var req int
		if err := rows.Scan(&r.TemplateID, &r.FieldCode, &req, &r.UpdatedAt, &r.UpdatedBy); err != nil {
			return nil, err
		}
		r.Required = req != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceTemplateFieldRules 覆盖某个模板的全部字段规则。
//
// 【整份覆盖而不是逐条 upsert】后台一次保存一个模板的全部字段。逐条更新的话,
// "把某个字段改回默认"就得靠删除,少删一条就留下一条幽灵配置 ——
// 而幽灵配置的表现是"我明明改回来了,线上还是必填"。先删后插,不会有残留。
func (s *SQLiteStore) ReplaceTemplateFieldRules(templateID string, rules []*TemplateFieldRule) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM template_field_rules WHERE template_id = ?`, templateID); err != nil {
		return err
	}
	now := nowStamp()
	for _, r := range rules {
		if r == nil || strings.TrimSpace(r.FieldCode) == "" || lockedRequiredFields[r.FieldCode] {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO template_field_rules (template_id, field_code, required, updated_at, updated_by)
			 VALUES (?, ?, ?, ?, ?)`,
			templateID, r.FieldCode, boolToInt(r.Required), now, r.UpdatedBy); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ===== MemStore =====

func (s *MemStore) ListTemplateFieldRules() ([]*TemplateFieldRule, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*TemplateFieldRule{}
	for _, byField := range s.templateRules {
		for _, r := range byField {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (s *MemStore) ReplaceTemplateFieldRules(templateID string, rules []*TemplateFieldRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]*TemplateFieldRule{}
	for _, r := range rules {
		if r == nil || strings.TrimSpace(r.FieldCode) == "" || lockedRequiredFields[r.FieldCode] {
			continue
		}
		cp := *r
		cp.TemplateID = templateID
		next[r.FieldCode] = &cp
	}
	if len(next) == 0 {
		delete(s.templateRules, templateID)
		return nil
	}
	s.templateRules[templateID] = next
	return nil
}

// ===== 接口 =====

// handleSaveTemplateFields 保存某个模板的必填/选填配置。
// PUT /api/templates/<id>/fields
func (s *Server) handleSaveTemplateFields(w http.ResponseWriter, r *http.Request, templateID string) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可修改模板规则")
		return
	}
	// 模板必须存在 —— 否则前端传错 id 会静默存下一份永远不生效的配置
	if _, ok := templateByID(templateID); !ok {
		writeError(w, http.StatusNotFound, "template_not_found", "模板不存在")
		return
	}
	var req struct {
		// Required: 字段编码 -> 是否必填。只传要覆盖的字段;没传的走代码默认值。
		Required map[string]bool `json:"required"`
		// MinImages: 每单最少几张照片。nil = 不改;0 = 改回模板默认值。
		MinImages *int `json:"minImages"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// 只接受这个模板里真实存在的字段。传个不存在的编码进来,存下去也永远不生效,
	// 而后台会显示"已保存" —— 这种静默不一致最难查。
	tpl, tplOK := templateByID(templateID)
	if !tplOK {
		writeError(w, http.StatusNotFound, "not_found", "模板不存在")
		return
	}
	known := map[string]bool{}
	for _, f := range tpl.Fields {
		known[f.Code] = true
	}
	operator := s.currentUserName(r)
	rules := make([]*TemplateFieldRule, 0, len(req.Required))
	for code, required := range req.Required {
		if !known[code] {
			writeError(w, http.StatusBadRequest, "unknown_field", "模板里没有这个字段:"+code)
			return
		}
		if lockedRequiredFields[code] {
			writeError(w, http.StatusBadRequest, "field_locked",
				"「"+code+"」是台账认归属的字段,必须保持必填 —— 放开之后这条记录会挂不到任何设备上")
			return
		}
		rules = append(rules, &TemplateFieldRule{
			TemplateID: templateID, FieldCode: code, Required: required, UpdatedBy: operator,
		})
	}
	if req.MinImages != nil {
		n := *req.MinImages
		// 上限挡一下手滑:模板本身有单次上传上限(MaxImages),最少张数超过它
		// 就永远提交不了 —— 而巡检员在现场只会看到"还差 N 张",拍到死也够不着。
		if n < 0 || (tpl.MaxImages > 0 && n > tpl.MaxImages) {
			writeError(w, http.StatusBadRequest, "bad_min_images",
				fmt.Sprintf("最少张数要在 0 到 %d 之间(0 表示不限)", tpl.MaxImages))
			return
		}
		tpl.MinImages = n
	}
	// 【写底表,不再写覆盖层】覆盖层已经撤了(迁移 027)。
	// 继续写它的话保存成功但完全不生效 —— 而且不报错。
	byCode := map[string]bool{}
	for code, required := range req.Required {
		byCode[code] = required
	}
	fields := make([]TemplateField, len(tpl.Fields))
	copy(fields, tpl.Fields)
	for i := range fields {
		if lockedRequiredFields[fields[i].Code] {
			fields[i].Required = true // 锁定字段无视配置,永远必填
			continue
		}
		if want, ok := byCode[fields[i].Code]; ok {
			fields[i].Required = want
		}
	}
	tpl.Fields = fields
	if err := s.saveReportTemplate(tpl); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	// 旧表继续记一份,只为回退时能对照,不再被读取
	_ = s.store.SetTemplateMinImages(templateID, tpl.MinImages, operator)
	_ = s.store.ReplaceTemplateFieldRules(templateID, rules)
	// 缓存由 saveReportTemplate 一并刷了 —— 存完不刷的表现是
	// "保存成功但线上照旧",而且不报错。
	s.recordOperation(r, "template.fields", "template", templateID, map[string]any{
		"count": len(rules),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTemplateRoutes 处理 /api/templates/<id>/fields
func (s *Server) handleTemplateRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/report/templates/")
	id := strings.TrimSuffix(rest, "/fields")
	if id == rest {
		// 【同一个前缀只留一个分发器】不带 /fields 的是模板本体的增删改,
		// 交给编辑那一套。分成两个 case 的话,谁先匹配到就由 switch 的
		// 书写顺序决定 —— 那种耦合改起来必然踩。
		s.handleReportTemplateAdmin(w, r)
		return
	}
	if id == "" {
		writeError(w, http.StatusNotFound, "not_found", "未匹配的模板路由")
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 PUT")
		return
	}
	s.handleSaveTemplateFields(w, r, id)
}

// ===== 模板级设置(最少照片数) =====

func (s *SQLiteStore) ListTemplateSettings() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT template_id, min_images FROM template_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetTemplateMinImages(templateID string, minImages int, operator string) error {
	now := nowStamp()
	// 先删后插:省掉两种方言的 upsert 差异,而且"改回默认"就是传 0 → 删掉这一行,
	// 不会留下一条 min_images=0 的幽灵配置。
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM template_settings WHERE template_id = ?`, templateID); err != nil {
		return err
	}
	if minImages > 0 {
		if _, err := tx.Exec(
			`INSERT INTO template_settings (template_id, min_images, updated_at, updated_by)
			 VALUES (?, ?, ?, ?)`, templateID, minImages, now, operator); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MemStore) ListTemplateSettings() (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]int{}
	for k, v := range s.templateMinImages {
		out[k] = v
	}
	return out, nil
}

func (s *MemStore) SetTemplateMinImages(templateID string, minImages int, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if minImages > 0 {
		s.templateMinImages[templateID] = minImages
	} else {
		delete(s.templateMinImages, templateID)
	}
	return nil
}
