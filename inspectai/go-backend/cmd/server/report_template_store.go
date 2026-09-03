package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ===== 巡检模板的读写 =====
//
// 模板原来写死在 templates.go(10 个、120 个字段),加一个模板或改一个中文
// 标签都要改代码重新部署 —— 而这些是业务定义,不是程序逻辑。
//
// 【读取仍然收口在 reportTemplates()】全仓十几处取模板都走它,所以"从哪来"
// 只在那一处切换。散着改必然漏,而漏掉的那处不报错(权限那一轮的教训)。

// ReportTemplateStore — 巡检模板的持久化
type ReportTemplateStore interface {
	// ListReportTemplates 取全部模板(含停用的,由调用方决定要不要过滤)。
	ListReportTemplates() ([]ReportTemplate, error)
	// UpsertReportTemplate 整份覆盖:模板头 + 字段表。
	//
	// 【字段用整份替换而不是逐条 diff】字段有顺序,逐条更新要额外维护
	// "谁被删了、谁挪了位",而顺序错了表单就乱了。整份替换只有一种结果。
	UpsertReportTemplate(t ReportTemplate) error
	DeleteReportTemplate(id string) error
	// CountRecordsUsingTemplate 有多少条巡检记录用了这个模板。
	//
	// 【决定哪些改动还允许做】字段的 code 是记录里字段值的键:有记录之后
	// 改 code 或删字段,历史记录里那一项就再也读不出来 —— 而且不报错,
	// 表现是"以前填过的内容不见了"。所以有记录的模板只允许改中文标签、
	// 加新字段,不允许改 code、不允许删字段。
	CountRecordsUsingTemplate(templateID string) (int, error)
}

// ===== SQLiteStore(SQLite + MySQL) =====

func (s *SQLiteStore) ListReportTemplates() ([]ReportTemplate, error) {
	rows, err := s.db.Query(`SELECT id, name, project, asset_type, max_images,
		min_images, featured, has_ai, ai_prompt FROM report_templates
		WHERE disabled = 0 ORDER BY sort_no ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReportTemplate{}
	order := []string{}
	byID := map[string]*ReportTemplate{}
	for rows.Next() {
		var t ReportTemplate
		var featured, hasAI int
		if err := rows.Scan(&t.ID, &t.Name, &t.Project, &t.AssetType, &t.MaxImages,
			&t.MinImages, &featured, &hasAI, &t.AIPrompt); err != nil {
			return nil, err
		}
		t.Featured = featured != 0
		t.HasAI = hasAI != 0
		t.Fields = []TemplateField{}
		out = append(out, t)
		order = append(order, t.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	if len(order) == 0 {
		return out, nil
	}

	// 【一次查全部字段,不逐个模板查】十个模板就是十次往返,而这是启动
	// 和每次保存后都要跑的路径。
	fRows, err := s.db.Query(`SELECT template_id, code, label, kind, required,
		source, options, default_val, manual_only FROM report_template_fields
		ORDER BY template_id ASC, sort_no ASC`)
	if err != nil {
		return nil, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var tplID, optionsJSON string
		var f TemplateField
		var required, manualOnly int
		if err := fRows.Scan(&tplID, &f.Code, &f.Label, &f.Kind, &required,
			&f.Source, &optionsJSON, &f.Default, &manualOnly); err != nil {
			return nil, err
		}
		f.Required = required != 0
		f.ManualOnly = manualOnly != 0
		if strings.TrimSpace(optionsJSON) != "" {
			// 解不出来就当没有选项 —— 一条脏数据不该让整份模板加载失败,
			// 那会让全系统建不了记录。
			_ = json.Unmarshal([]byte(optionsJSON), &f.Options)
		}
		if tpl, ok := byID[tplID]; ok {
			tpl.Fields = append(tpl.Fields, f)
		}
	}
	return out, fRows.Err()
}

func (s *SQLiteStore) UpsertReportTemplate(t ReportTemplate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt := `INSERT INTO report_templates
		(id,tenant_id,name,project,asset_type,max_images,min_images,featured,has_ai,ai_prompt,disabled,sort_no,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,0,0,?)
		ON CONFLICT(id) DO UPDATE SET name=excluded.name, project=excluded.project,
		asset_type=excluded.asset_type, max_images=excluded.max_images,
		min_images=excluded.min_images, featured=excluded.featured,
		has_ai=excluded.has_ai, ai_prompt=excluded.ai_prompt, updated_at=excluded.updated_at`
	if s.dialect == "mysql" {
		stmt = `INSERT INTO report_templates
			(id,tenant_id,name,project,asset_type,max_images,min_images,featured,has_ai,ai_prompt,disabled,sort_no,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,0,0,?)
			ON DUPLICATE KEY UPDATE name=VALUES(name), project=VALUES(project),
			asset_type=VALUES(asset_type), max_images=VALUES(max_images),
			min_images=VALUES(min_images), featured=VALUES(featured),
			has_ai=VALUES(has_ai), ai_prompt=VALUES(ai_prompt), updated_at=VALUES(updated_at)`
	}
	if _, err := tx.Exec(stmt, t.ID, defaultTenantID, t.Name, t.Project, t.AssetType,
		t.MaxImages, t.MinImages, boolToInt(t.Featured), boolToInt(t.HasAI),
		t.AIPrompt, nowStamp()); err != nil {
		return fmt.Errorf("upsert report_template %s: %w", t.ID, err)
	}

	if _, err := tx.Exec(`DELETE FROM report_template_fields WHERE template_id=?`, t.ID); err != nil {
		return fmt.Errorf("clear fields of %s: %w", t.ID, err)
	}
	for i, f := range t.Fields {
		optionsJSON := ""
		if len(f.Options) > 0 {
			b, _ := json.Marshal(f.Options)
			optionsJSON = string(b)
		}
		if _, err := tx.Exec(`INSERT INTO report_template_fields
			(id,template_id,code,label,kind,required,source,options,default_val,manual_only,sort_no)
			VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID+"__"+f.Code, t.ID, f.Code, f.Label, f.Kind, boolToInt(f.Required),
			f.Source, optionsJSON, f.Default, boolToInt(f.ManualOnly), i); err != nil {
			return fmt.Errorf("insert field %s.%s: %w", t.ID, f.Code, err)
		}
	}
	return tx.Commit()
}

func (s *SQLiteStore) CountRecordsUsingTemplate(templateID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM records WHERE template_id=?`, templateID).Scan(&n)
	return n, err
}

func (s *SQLiteStore) DeleteReportTemplate(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM report_template_fields WHERE template_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM report_templates WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ===== MemStore(测试 / 无库回落) =====

func (m *MemStore) ListReportTemplates() ([]ReportTemplate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ReportTemplate, 0, len(m.reportTemplates))
	for _, id := range m.reportTemplateOrder {
		if t, ok := m.reportTemplates[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemStore) UpsertReportTemplate(t ReportTemplate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.reportTemplates[t.ID]; !exists {
		m.reportTemplateOrder = append(m.reportTemplateOrder, t.ID)
	}
	m.reportTemplates[t.ID] = t
	return nil
}

func (m *MemStore) CountRecordsUsingTemplate(templateID string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, rec := range m.records {
		if rec != nil && rec.TemplateID == templateID {
			n++
		}
	}
	return n, nil
}

func (m *MemStore) DeleteReportTemplate(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.reportTemplates, id)
	for i, x := range m.reportTemplateOrder {
		if x == id {
			m.reportTemplateOrder = append(m.reportTemplateOrder[:i], m.reportTemplateOrder[i+1:]...)
			break
		}
	}
	return nil
}
