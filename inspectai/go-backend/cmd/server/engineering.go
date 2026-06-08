package main

import (
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	engineeringSeedSource = "seed:guohui-2026"

	engPlanStatusPending    = "待执行"
	engPlanStatusRunning    = "执行中"
	engPlanStatusDone       = "已完成"
	engPlanStatusUnplanned  = "未排期"
	engTaskStatusPending    = "待执行"
	engTaskStatusProcessing = "进行中"
	engTaskStatusDone       = "已完成"
	engTaskStatusOverdue    = "逾期"
	engTaskStatusCanceled   = "已取消"
)

// EngineeringPlanItem 是工程年度计划事项，不等同于每日巡检点位。
// 它承接真实 Excel 里的「强制检测 / 重点外委 / 零星工程 / CAPEX」等计划。
type EngineeringPlanItem struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	SequenceNo   string    `json:"sequenceNo"`
	BusinessType string    `json:"businessType"`
	Project      string    `json:"project"`
	Category     string    `json:"category"`
	SubType      string    `json:"subType"`
	WorkContent  string    `json:"workContent"`
	ScopeDesc    string    `json:"scopeDesc"`
	BudgetAmount float64   `json:"budgetAmount"`
	BudgetText   string    `json:"budgetText"`
	PlanStart    string    `json:"planStart"`
	PlanEnd      string    `json:"planEnd"`
	OwnerName    string    `json:"ownerName"`
	CycleText    string    `json:"cycleText"`
	Remark       string    `json:"remark"`
	Status       string    `json:"status"`
	RiskLevel    string    `json:"riskLevel"`
	LatestTaskID string    `json:"latestTaskId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type EngineeringTask struct {
	ID             string    `json:"id"`
	PlanItemID     string    `json:"planItemId"`
	Source         string    `json:"source"`
	TaskType       string    `json:"taskType"`
	Title          string    `json:"title"`
	Project        string    `json:"project"`
	Category       string    `json:"category"`
	WorkContent    string    `json:"workContent"`
	AssigneeName   string    `json:"assigneeName"`
	ReviewerName   string    `json:"reviewerName"`
	VendorName     string    `json:"vendorName"`
	DueAt          string    `json:"dueAt"`
	StartedAt      string    `json:"startedAt"`
	CompletedAt    string    `json:"completedAt"`
	Status         string    `json:"status"`
	EvidenceStatus string    `json:"evidenceStatus"`
	AIStatus       string    `json:"aiStatus"`
	RecordID       string    `json:"recordId"`
	ReportID       string    `json:"reportId"`
	AssetID        string    `json:"assetId"`
	BudgetAmount   float64   `json:"budgetAmount"`
	CloseResult    string    `json:"closeResult"`
	CloseNote      string    `json:"closeNote"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type EngineeringPlanFilter struct {
	Project  string
	Category string
	Status   string
	Owner    string
	Keyword  string
}

type EngineeringTaskFilter struct {
	Project    string
	Category   string
	Status     string
	Assignee   string
	PlanItemID string
	Keyword    string
}

func ensureEngineeringPlanSeeds(store Store) error {
	for _, plan := range engineeringPlanSeeds() {
		if _, err := store.GetEngineeringPlan(plan.ID); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := store.UpsertEngineeringPlan(plan); err != nil {
			return err
		}
	}
	for _, task := range engineeringTaskSeeds() {
		if _, err := store.GetEngineeringTask(task.ID); err == nil {
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := store.CreateEngineeringTask(task); err != nil {
			return err
		}
		if task.PlanItemID != "" {
			_ = store.UpdateEngineeringPlanLatestTask(task.PlanItemID, task.ID)
		}
	}
	return nil
}

func engineeringPlanSeeds() []*EngineeringPlanItem {
	now := time.Now()
	rows := []EngineeringPlanItem{
		{ID: "eng_plan_gh_001", SequenceNo: "1", Project: "国会中心", Category: "强制检测", WorkContent: "建筑消防设施检验报告", ScopeDesc: "年度消防设施检测与报告归档", BudgetAmount: 57456, BudgetText: "57456", PlanStart: "2026-12-01", PlanEnd: "2026-12-31", OwnerName: "丛洪忠", CycleText: "每年一次", Status: engPlanStatusPending, RiskLevel: "normal"},
		{ID: "eng_plan_gh_002", SequenceNo: "2", Project: "国会中心", Category: "强制检测", WorkContent: "水质检测报告", ScopeDesc: "生活水质检测与报告留档", BudgetAmount: 4500, BudgetText: "4500", PlanStart: "2026-01", PlanEnd: "2026-07", OwnerName: "董圣军", CycleText: "每年1月、7月检测", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_003", SequenceNo: "3", Project: "国会中心", Category: "强制检测", WorkContent: "军团菌检测", ScopeDesc: "重点涉水系统军团菌检测", BudgetAmount: 6400, BudgetText: "6400", PlanStart: "2026-07", PlanEnd: "2027-07", OwnerName: "丛洪忠", CycleText: "每年1月、4月、7月、10月检测", Status: engPlanStatusPending, RiskLevel: "warning"},
		{ID: "eng_plan_gh_004", SequenceNo: "4", Project: "国会中心", Category: "强制检测", WorkContent: "电梯安全年度检验报告", ScopeDesc: "电梯年度安全检验、问题整改与报告归档", BudgetAmount: 23000, BudgetText: "23000", PlanStart: "2026-04", PlanEnd: "2026-10", OwnerName: "朱佳伟", CycleText: "每年4月、7月检测", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_005", SequenceNo: "5", Project: "国会中心", Category: "强制检测", WorkContent: "绝缘工器具检验报告", ScopeDesc: "高压操作工具、验电器、绝缘用品检测", BudgetAmount: 12000, BudgetText: "12000", PlanStart: "2026-06", PlanEnd: "2026-12", OwnerName: "新博", CycleText: "半年/年度检测", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_006", SequenceNo: "6", Project: "国会中心", Category: "强制检测", WorkContent: "防雷装置定期检查报告", ScopeDesc: "防雷装置检测与隐患整改跟踪", BudgetAmount: 30000, BudgetText: "30000", PlanStart: "2026-09", PlanEnd: "2026-11", OwnerName: "杜宪奎", CycleText: "每年一次", Status: engPlanStatusPending, RiskLevel: "normal"},
		{ID: "eng_plan_gh_007", SequenceNo: "7", Project: "国会中心", Category: "强制检测", WorkContent: "排水系统排污检测", ScopeDesc: "排污许可证有效期、排放检测与资料归档", BudgetAmount: 0, BudgetText: "", PlanStart: "2024-06-19", PlanEnd: "2029-06-18", OwnerName: "董圣军", CycleText: "关注许可证到期", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_008", SequenceNo: "8", Project: "国会中心", Category: "重点外委", WorkContent: "电梯维保", ScopeDesc: "电梯维保、故障响应与年度资料闭环", BudgetAmount: 196920, BudgetText: "196920", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "朱佳伟", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_009", SequenceNo: "9", Project: "国会中心", Category: "重点外委", WorkContent: "中央空调主机维保", ScopeDesc: "中央空调主机年度维保与运行保障", BudgetAmount: 180000, BudgetText: "180000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "新博", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_010", SequenceNo: "10", Project: "国会中心", Category: "重点外委", WorkContent: "智能化系统维保", ScopeDesc: "网络、信息发布、灯光投影等智能化软硬件维保", BudgetAmount: 290000, BudgetText: "290000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "汪佳澜", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_011", SequenceNo: "11", Project: "国会中心", Category: "重点外委", WorkContent: "消防系统维保合同", ScopeDesc: "消防系统维保、报警联动与故障闭环", BudgetAmount: 220428.44, BudgetText: "220428.44", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "杜宪奎", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_012", SequenceNo: "12", Project: "国会中心", Category: "重点外委", WorkContent: "锅炉维保", ScopeDesc: "锅炉设备维保、巡检和运行保障", BudgetAmount: 110000, BudgetText: "110000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "新博", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
	}
	out := make([]*EngineeringPlanItem, 0, len(rows))
	for i := range rows {
		rows[i].Source = engineeringSeedSource
		rows[i].BusinessType = "工程年度计划"
		rows[i].CreatedAt = now
		rows[i].UpdatedAt = now
		out = append(out, &rows[i])
	}
	return out
}

func engineeringTaskSeeds() []*EngineeringTask {
	now := time.Now()
	rows := []EngineeringTask{
		{ID: "eng_task_gh_002_202607", PlanItemID: "eng_plan_gh_002", Title: "水质检测报告 7 月执行", Project: "国会中心", Category: "强制检测", WorkContent: "水质检测报告", AssigneeName: "董圣军", DueAt: "2026-07-31", Status: engTaskStatusPending, EvidenceStatus: "待上传", AIStatus: "待分析", BudgetAmount: 4500},
		{ID: "eng_task_gh_004_202607", PlanItemID: "eng_plan_gh_004", Title: "电梯安全年度检验 7 月跟踪", Project: "国会中心", Category: "强制检测", WorkContent: "电梯安全年度检验报告", AssigneeName: "朱佳伟", DueAt: "2026-07-31", Status: engTaskStatusProcessing, EvidenceStatus: "待补充", AIStatus: "待分析", BudgetAmount: 23000},
		{ID: "eng_task_gh_008_202606", PlanItemID: "eng_plan_gh_008", Title: "电梯维保月度资料复核", Project: "国会中心", Category: "重点外委", WorkContent: "电梯维保", AssigneeName: "朱佳伟", DueAt: "2026-06-30", Status: engTaskStatusProcessing, EvidenceStatus: "部分上传", AIStatus: "待分析", BudgetAmount: 196920},
		{ID: "eng_task_gh_010_202606", PlanItemID: "eng_plan_gh_010", Title: "智能化系统维保月度跟踪", Project: "国会中心", Category: "重点外委", WorkContent: "智能化系统维保", AssigneeName: "汪佳澜", DueAt: "2026-06-30", Status: engTaskStatusPending, EvidenceStatus: "待上传", AIStatus: "待分析", BudgetAmount: 290000},
	}
	out := make([]*EngineeringTask, 0, len(rows))
	for i := range rows {
		rows[i].Source = engineeringSeedSource
		rows[i].TaskType = "工程计划执行"
		rows[i].CreatedAt = now
		rows[i].UpdatedAt = now
		out = append(out, &rows[i])
	}
	return out
}

func normalizeEngineeringPlan(item *EngineeringPlanItem) {
	now := time.Now()
	if item.ID == "" {
		item.ID = newID("eng_plan")
	}
	item.Source = firstNonEmpty(item.Source, "manual")
	item.Project = firstNonEmpty(item.Project, "默认项目")
	item.Category = firstNonEmpty(item.Category, "工程计划")
	item.WorkContent = strings.TrimSpace(item.WorkContent)
	item.Status = firstNonEmpty(item.Status, inferEngineeringPlanStatus(item.PlanStart, item.PlanEnd))
	item.RiskLevel = firstNonEmpty(item.RiskLevel, "normal")
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
}

func normalizeEngineeringTask(task *EngineeringTask) {
	now := time.Now()
	if task.ID == "" {
		task.ID = newID("eng_task")
	}
	task.Source = firstNonEmpty(task.Source, "manual")
	task.TaskType = firstNonEmpty(task.TaskType, "工程计划执行")
	task.Project = firstNonEmpty(task.Project, "默认项目")
	task.Category = firstNonEmpty(task.Category, "工程任务")
	task.Title = firstNonEmpty(task.Title, task.WorkContent, "工程执行任务")
	task.Status = firstNonEmpty(task.Status, engTaskStatusPending)
	task.EvidenceStatus = firstNonEmpty(task.EvidenceStatus, "待上传")
	task.AIStatus = firstNonEmpty(task.AIStatus, "待分析")
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
}

func inferEngineeringPlanStatus(start, end string) string {
	if strings.TrimSpace(start) == "" && strings.TrimSpace(end) == "" {
		return engPlanStatusUnplanned
	}
	return engPlanStatusPending
}

func (s *MemStore) ListEngineeringPlans(filter EngineeringPlanFilter) ([]*EngineeringPlanItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*EngineeringPlanItem, 0, len(s.engPlans))
	for _, item := range s.engPlans {
		if engineeringPlanMatches(item, filter) {
			cp := *item
			out = append(out, &cp)
		}
	}
	sortEngineeringPlans(out)
	return out, nil
}

func (s *MemStore) GetEngineeringPlan(id string) (*EngineeringPlanItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.engPlans[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *item
	return &cp, nil
}

func (s *MemStore) UpsertEngineeringPlan(item *EngineeringPlanItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeEngineeringPlan(item)
	cp := *item
	s.engPlans[item.ID] = &cp
	return nil
}

func (s *MemStore) UpdateEngineeringPlanLatestTask(planID, taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.engPlans[planID]
	if !ok {
		return sql.ErrNoRows
	}
	item.LatestTaskID = taskID
	item.UpdatedAt = time.Now()
	return nil
}

func (s *MemStore) ListEngineeringTasks(filter EngineeringTaskFilter) ([]*EngineeringTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*EngineeringTask, 0, len(s.engTasks))
	for _, task := range s.engTasks {
		if engineeringTaskMatches(task, filter) {
			cp := *task
			out = append(out, &cp)
		}
	}
	sortEngineeringTasks(out)
	return out, nil
}

func (s *MemStore) GetEngineeringTask(id string) (*EngineeringTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.engTasks[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *task
	return &cp, nil
}

func (s *MemStore) CreateEngineeringTask(task *EngineeringTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	normalizeEngineeringTask(task)
	cp := *task
	s.engTasks[task.ID] = &cp
	return nil
}

func (s *MemStore) UpdateEngineeringTask(id string, mutate func(*EngineeringTask)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.engTasks[id]
	if !ok {
		return sql.ErrNoRows
	}
	mutate(task)
	normalizeEngineeringTask(task)
	return nil
}

func (s *SQLiteStore) ListEngineeringPlans(filter EngineeringPlanFilter) ([]*EngineeringPlanItem, error) {
	rows, err := s.db.Query(`
		SELECT id, source, sequence_no, business_type, project, category, sub_type,
		       work_content, scope_desc, budget_amount, budget_text, plan_start,
		       plan_end, owner_name, cycle_text, remark, status, risk_level,
		       latest_task_id, created_at, updated_at
		FROM engineering_plan_items
		ORDER BY plan_end ASC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EngineeringPlanItem
	for rows.Next() {
		item, err := scanEngineeringPlan(rows)
		if err != nil {
			return nil, err
		}
		if engineeringPlanMatches(item, filter) {
			out = append(out, item)
		}
	}
	sortEngineeringPlans(out)
	return out, rows.Err()
}

func (s *SQLiteStore) GetEngineeringPlan(id string) (*EngineeringPlanItem, error) {
	row := s.db.QueryRow(`
		SELECT id, source, sequence_no, business_type, project, category, sub_type,
		       work_content, scope_desc, budget_amount, budget_text, plan_start,
		       plan_end, owner_name, cycle_text, remark, status, risk_level,
		       latest_task_id, created_at, updated_at
		FROM engineering_plan_items WHERE id=?`, id)
	return scanEngineeringPlan(row)
}

func (s *SQLiteStore) UpsertEngineeringPlan(item *EngineeringPlanItem) error {
	normalizeEngineeringPlan(item)
	created := item.CreatedAt.Format(time.RFC3339Nano)
	updated := item.UpdatedAt.Format(time.RFC3339Nano)
	var query string
	if s.dialect == "mysql" {
		query = `
			INSERT INTO engineering_plan_items (
				id, source, sequence_no, business_type, project, category, sub_type,
				work_content, scope_desc, budget_amount, budget_text, plan_start,
				plan_end, owner_name, cycle_text, remark, status, risk_level,
				latest_task_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				source=VALUES(source), sequence_no=VALUES(sequence_no), business_type=VALUES(business_type),
				project=VALUES(project), category=VALUES(category), sub_type=VALUES(sub_type),
				work_content=VALUES(work_content), scope_desc=VALUES(scope_desc), budget_amount=VALUES(budget_amount),
				budget_text=VALUES(budget_text), plan_start=VALUES(plan_start), plan_end=VALUES(plan_end),
				owner_name=VALUES(owner_name), cycle_text=VALUES(cycle_text), remark=VALUES(remark),
				status=VALUES(status), risk_level=VALUES(risk_level), latest_task_id=VALUES(latest_task_id),
				updated_at=VALUES(updated_at)`
	} else {
		query = `
			INSERT INTO engineering_plan_items (
				id, source, sequence_no, business_type, project, category, sub_type,
				work_content, scope_desc, budget_amount, budget_text, plan_start,
				plan_end, owner_name, cycle_text, remark, status, risk_level,
				latest_task_id, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				source=excluded.source, sequence_no=excluded.sequence_no, business_type=excluded.business_type,
				project=excluded.project, category=excluded.category, sub_type=excluded.sub_type,
				work_content=excluded.work_content, scope_desc=excluded.scope_desc, budget_amount=excluded.budget_amount,
				budget_text=excluded.budget_text, plan_start=excluded.plan_start, plan_end=excluded.plan_end,
				owner_name=excluded.owner_name, cycle_text=excluded.cycle_text, remark=excluded.remark,
				status=excluded.status, risk_level=excluded.risk_level, latest_task_id=excluded.latest_task_id,
				updated_at=excluded.updated_at`
	}
	_, err := s.db.Exec(query,
		item.ID, item.Source, item.SequenceNo, item.BusinessType, item.Project, item.Category, item.SubType,
		item.WorkContent, item.ScopeDesc, item.BudgetAmount, item.BudgetText, item.PlanStart,
		item.PlanEnd, item.OwnerName, item.CycleText, item.Remark, item.Status, item.RiskLevel,
		item.LatestTaskID, created, updated,
	)
	return err
}

func (s *SQLiteStore) UpdateEngineeringPlanLatestTask(planID, taskID string) error {
	now := time.Now().Format(time.RFC3339Nano)
	res, err := s.db.Exec(`UPDATE engineering_plan_items SET latest_task_id=?, updated_at=? WHERE id=?`, taskID, now, planID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) ListEngineeringTasks(filter EngineeringTaskFilter) ([]*EngineeringTask, error) {
	rows, err := s.db.Query(`
		SELECT id, plan_item_id, source, task_type, title, project, category,
		       work_content, assignee_name, reviewer_name, vendor_name, due_at,
		       started_at, completed_at, status, evidence_status, ai_status,
		       record_id, report_id, asset_id, budget_amount, close_result,
		       close_note, created_at, updated_at
		FROM engineering_tasks
		ORDER BY due_at ASC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*EngineeringTask
	for rows.Next() {
		task, err := scanEngineeringTask(rows)
		if err != nil {
			return nil, err
		}
		if engineeringTaskMatches(task, filter) {
			out = append(out, task)
		}
	}
	sortEngineeringTasks(out)
	return out, rows.Err()
}

func (s *SQLiteStore) GetEngineeringTask(id string) (*EngineeringTask, error) {
	row := s.db.QueryRow(`
		SELECT id, plan_item_id, source, task_type, title, project, category,
		       work_content, assignee_name, reviewer_name, vendor_name, due_at,
		       started_at, completed_at, status, evidence_status, ai_status,
		       record_id, report_id, asset_id, budget_amount, close_result,
		       close_note, created_at, updated_at
		FROM engineering_tasks WHERE id=?`, id)
	return scanEngineeringTask(row)
}

func (s *SQLiteStore) CreateEngineeringTask(task *EngineeringTask) error {
	normalizeEngineeringTask(task)
	created := task.CreatedAt.Format(time.RFC3339Nano)
	updated := task.UpdatedAt.Format(time.RFC3339Nano)
	_, err := s.db.Exec(`
		INSERT INTO engineering_tasks (
			id, plan_item_id, source, task_type, title, project, category,
			work_content, assignee_name, reviewer_name, vendor_name, due_at,
			started_at, completed_at, status, evidence_status, ai_status,
			record_id, report_id, asset_id, budget_amount, close_result,
			close_note, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.PlanItemID, task.Source, task.TaskType, task.Title, task.Project, task.Category,
		task.WorkContent, task.AssigneeName, task.ReviewerName, task.VendorName, task.DueAt,
		task.StartedAt, task.CompletedAt, task.Status, task.EvidenceStatus, task.AIStatus,
		task.RecordID, task.ReportID, task.AssetID, task.BudgetAmount, task.CloseResult,
		task.CloseNote, created, updated,
	)
	return err
}

func (s *SQLiteStore) UpdateEngineeringTask(id string, mutate func(*EngineeringTask)) error {
	task, err := s.GetEngineeringTask(id)
	if err != nil {
		return err
	}
	mutate(task)
	normalizeEngineeringTask(task)
	updated := task.UpdatedAt.Format(time.RFC3339Nano)
	res, err := s.db.Exec(`
		UPDATE engineering_tasks SET
			plan_item_id=?, source=?, task_type=?, title=?, project=?, category=?,
			work_content=?, assignee_name=?, reviewer_name=?, vendor_name=?, due_at=?,
			started_at=?, completed_at=?, status=?, evidence_status=?, ai_status=?,
			record_id=?, report_id=?, asset_id=?, budget_amount=?, close_result=?,
			close_note=?, updated_at=?
		WHERE id=?`,
		task.PlanItemID, task.Source, task.TaskType, task.Title, task.Project, task.Category,
		task.WorkContent, task.AssigneeName, task.ReviewerName, task.VendorName, task.DueAt,
		task.StartedAt, task.CompletedAt, task.Status, task.EvidenceStatus, task.AIStatus,
		task.RecordID, task.ReportID, task.AssetID, task.BudgetAmount, task.CloseResult,
		task.CloseNote, updated, task.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanEngineeringPlan(row scanner) (*EngineeringPlanItem, error) {
	item := &EngineeringPlanItem{}
	var created, updated string
	err := row.Scan(
		&item.ID, &item.Source, &item.SequenceNo, &item.BusinessType, &item.Project, &item.Category, &item.SubType,
		&item.WorkContent, &item.ScopeDesc, &item.BudgetAmount, &item.BudgetText, &item.PlanStart,
		&item.PlanEnd, &item.OwnerName, &item.CycleText, &item.Remark, &item.Status, &item.RiskLevel,
		&item.LatestTaskID, &created, &updated,
	)
	if err != nil {
		return nil, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return item, nil
}

func scanEngineeringTask(row scanner) (*EngineeringTask, error) {
	task := &EngineeringTask{}
	var created, updated string
	err := row.Scan(
		&task.ID, &task.PlanItemID, &task.Source, &task.TaskType, &task.Title, &task.Project, &task.Category,
		&task.WorkContent, &task.AssigneeName, &task.ReviewerName, &task.VendorName, &task.DueAt,
		&task.StartedAt, &task.CompletedAt, &task.Status, &task.EvidenceStatus, &task.AIStatus,
		&task.RecordID, &task.ReportID, &task.AssetID, &task.BudgetAmount, &task.CloseResult,
		&task.CloseNote, &created, &updated,
	)
	if err != nil {
		return nil, err
	}
	task.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	task.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return task, nil
}

func engineeringPlanMatches(item *EngineeringPlanItem, filter EngineeringPlanFilter) bool {
	if item == nil {
		return false
	}
	if filter.Project != "" && item.Project != filter.Project {
		return false
	}
	if filter.Category != "" && item.Category != filter.Category {
		return false
	}
	if filter.Status != "" && item.Status != filter.Status {
		return false
	}
	if filter.Owner != "" && item.OwnerName != filter.Owner {
		return false
	}
	if filter.Keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join([]string{
		item.ID, item.SequenceNo, item.BusinessType, item.Project, item.Category, item.SubType,
		item.WorkContent, item.ScopeDesc, item.BudgetText, item.PlanStart, item.PlanEnd,
		item.OwnerName, item.CycleText, item.Remark, item.Status, item.RiskLevel,
	}, " ")), strings.ToLower(filter.Keyword))
}

func engineeringTaskMatches(task *EngineeringTask, filter EngineeringTaskFilter) bool {
	if task == nil {
		return false
	}
	if filter.Project != "" && task.Project != filter.Project {
		return false
	}
	if filter.Category != "" && task.Category != filter.Category {
		return false
	}
	if filter.Status != "" && task.Status != filter.Status {
		return false
	}
	if filter.Assignee != "" && task.AssigneeName != filter.Assignee {
		return false
	}
	if filter.PlanItemID != "" && task.PlanItemID != filter.PlanItemID {
		return false
	}
	if filter.Keyword == "" {
		return true
	}
	return strings.Contains(strings.ToLower(strings.Join([]string{
		task.ID, task.PlanItemID, task.TaskType, task.Title, task.Project, task.Category,
		task.WorkContent, task.AssigneeName, task.ReviewerName, task.VendorName,
		task.DueAt, task.Status, task.EvidenceStatus, task.AIStatus, task.CloseNote,
	}, " ")), strings.ToLower(filter.Keyword))
}

func sortEngineeringPlans(items []*EngineeringPlanItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].PlanEnd == items[j].PlanEnd {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		if items[i].PlanEnd == "" {
			return false
		}
		if items[j].PlanEnd == "" {
			return true
		}
		return items[i].PlanEnd < items[j].PlanEnd
	})
}

func sortEngineeringTasks(items []*EngineeringTask) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].DueAt == items[j].DueAt {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		if items[i].DueAt == "" {
			return false
		}
		if items[j].DueAt == "" {
			return true
		}
		return items[i].DueAt < items[j].DueAt
	})
}

func engineeringPlanSummary(items []*EngineeringPlanItem) map[string]any {
	byStatus := map[string]int{}
	byCategory := map[string]int{}
	byOwner := map[string]int{}
	var budget float64
	for _, item := range items {
		if item == nil {
			continue
		}
		byStatus[firstNonEmpty(item.Status, "未定义")]++
		byCategory[firstNonEmpty(item.Category, "未分类")]++
		byOwner[firstNonEmpty(item.OwnerName, "未指派")]++
		budget += item.BudgetAmount
	}
	return map[string]any{
		"total":      len(items),
		"budget":     budget,
		"byStatus":   byStatus,
		"byCategory": byCategory,
		"byOwner":    byOwner,
	}
}

func engineeringTaskSummary(items []*EngineeringTask) map[string]any {
	byStatus := map[string]int{}
	byCategory := map[string]int{}
	byAssignee := map[string]int{}
	for _, task := range items {
		if task == nil {
			continue
		}
		byStatus[firstNonEmpty(task.Status, "未定义")]++
		byCategory[firstNonEmpty(task.Category, "未分类")]++
		byAssignee[firstNonEmpty(task.AssigneeName, "未指派")]++
	}
	return map[string]any{
		"total":      len(items),
		"byStatus":   byStatus,
		"byCategory": byCategory,
		"byAssignee": byAssignee,
	}
}

func engineeringPlanFilterFromRequest(r *http.Request) EngineeringPlanFilter {
	q := r.URL.Query()
	return EngineeringPlanFilter{
		Project:  strings.TrimSpace(q.Get("project")),
		Category: strings.TrimSpace(q.Get("category")),
		Status:   strings.TrimSpace(q.Get("status")),
		Owner:    strings.TrimSpace(q.Get("owner")),
		Keyword:  strings.TrimSpace(q.Get("q")),
	}
}

func engineeringTaskFilterFromRequest(r *http.Request) EngineeringTaskFilter {
	q := r.URL.Query()
	return EngineeringTaskFilter{
		Project:    strings.TrimSpace(q.Get("project")),
		Category:   strings.TrimSpace(q.Get("category")),
		Status:     strings.TrimSpace(q.Get("status")),
		Assignee:   strings.TrimSpace(q.Get("assignee")),
		PlanItemID: strings.TrimSpace(q.Get("planItemId")),
		Keyword:    strings.TrimSpace(q.Get("q")),
	}
}

func (s *Server) handleListEngineeringPlans(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListEngineeringPlans(engineeringPlanFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_engineering_plans_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plans":   items,
		"summary": engineeringPlanSummary(items),
	})
}

func (s *Server) handleCreateEngineeringPlan(w http.ResponseWriter, r *http.Request) {
	var req EngineeringPlanItem
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	req.WorkContent = strings.TrimSpace(req.WorkContent)
	if req.WorkContent == "" {
		writeError(w, http.StatusBadRequest, "missing_work_content", "工程计划必须填写工作内容")
		return
	}
	if req.ID == "" {
		req.ID = newID("eng_plan")
	}
	req.Source = firstNonEmpty(req.Source, "manual")
	if err := s.store.UpsertEngineeringPlan(&req); err != nil {
		writeError(w, http.StatusInternalServerError, "save_engineering_plan_failed", err.Error())
		return
	}
	s.recordOperation(r, "engineering_plan_save", "engineering_plan", req.ID, map[string]any{
		"workContent": req.WorkContent,
		"category":    req.Category,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"plan": req})
}

func (s *Server) handleListEngineeringTasks(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListEngineeringTasks(engineeringTaskFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_engineering_tasks_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":   items,
		"summary": engineeringTaskSummary(items),
	})
}

func (s *Server) handleCreateEngineeringTask(w http.ResponseWriter, r *http.Request) {
	var req EngineeringTask
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.PlanItemID != "" {
		if plan, err := s.store.GetEngineeringPlan(req.PlanItemID); err == nil && plan != nil {
			req.Project = firstNonEmpty(req.Project, plan.Project)
			req.Category = firstNonEmpty(req.Category, plan.Category)
			req.WorkContent = firstNonEmpty(req.WorkContent, plan.WorkContent)
			req.Title = firstNonEmpty(req.Title, plan.WorkContent+" 执行任务")
			req.AssigneeName = firstNonEmpty(req.AssigneeName, plan.OwnerName)
			req.DueAt = firstNonEmpty(req.DueAt, plan.PlanEnd)
			if req.BudgetAmount == 0 {
				req.BudgetAmount = plan.BudgetAmount
			}
		}
	}
	normalizeEngineeringTask(&req)
	if err := s.store.CreateEngineeringTask(&req); err != nil {
		writeError(w, http.StatusInternalServerError, "create_engineering_task_failed", err.Error())
		return
	}
	if req.PlanItemID != "" {
		_ = s.store.UpdateEngineeringPlanLatestTask(req.PlanItemID, req.ID)
	}
	s.recordOperation(r, "engineering_task_create", "engineering_task", req.ID, map[string]any{
		"title":    req.Title,
		"assignee": req.AssigneeName,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"task": req})
}

func (s *Server) handleEngineeringTaskRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/engineering/tasks/")
	if rest == "" {
		writeError(w, http.StatusNotFound, "not_found", "task route not found")
		return
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	id := parts[0]
	if len(parts) == 2 && parts[1] == "status" && r.Method == http.MethodPost {
		s.handleUpdateEngineeringTaskStatus(w, r, id)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		task, err := s.store.GetEngineeringTask(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "engineering_task_not_found", "工程任务不存在")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": task})
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "task route not found")
}

func (s *Server) handleUpdateEngineeringTaskStatus(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Status      string `json:"status"`
		CloseNote   string `json:"closeNote"`
		CloseResult string `json:"closeResult"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		writeError(w, http.StatusBadRequest, "missing_status", "必须提供任务状态")
		return
	}
	now := time.Now().Format(time.RFC3339Nano)
	err := s.store.UpdateEngineeringTask(id, func(task *EngineeringTask) {
		task.Status = status
		if status == engTaskStatusProcessing && task.StartedAt == "" {
			task.StartedAt = now
		}
		if status == engTaskStatusDone {
			task.CompletedAt = now
			task.CloseResult = firstNonEmpty(req.CloseResult, "完成")
		}
		if status == engTaskStatusPending {
			task.StartedAt = ""
			task.CompletedAt = ""
			task.CloseResult = ""
		}
		if req.CloseNote != "" {
			task.CloseNote = strings.TrimSpace(req.CloseNote)
		}
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "engineering_task_not_found", "工程任务不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "update_engineering_task_failed", err.Error())
		return
	}
	task, _ := s.store.GetEngineeringTask(id)
	s.recordOperation(r, "engineering_task_status", "engineering_task", id, map[string]any{
		"status": status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) closeEngineeringTaskFromRecord(rec *Record, assets []*AssetEntry, closedAt time.Time) error {
	if rec == nil || strings.TrimSpace(rec.EngineeringTaskID) == "" {
		return nil
	}
	taskID := strings.TrimSpace(rec.EngineeringTaskID)
	assetID := ""
	if len(assets) > 0 && assets[0] != nil {
		assetID = assets[0].ID
	}
	closeResult := "完成"
	if len(assets) > 0 && assets[0] != nil && assets[0].LastStatus != "" {
		closeResult = assets[0].LastStatus
	}
	closeNote := firstNonEmpty(rec.AISummary, rec.Report, "巡检记录已提交，工程任务自动闭环")
	err := s.store.UpdateEngineeringTask(taskID, func(task *EngineeringTask) {
		task.Status = engTaskStatusDone
		task.RecordID = rec.ID
		task.AssetID = assetID
		task.CompletedAt = closedAt.Format(time.RFC3339Nano)
		task.EvidenceStatus = "已提交"
		task.AIStatus = firstNonEmpty(rec.RecognitionStatus, "已完成")
		task.CloseResult = closeResult
		task.CloseNote = closeNote
		if task.AssigneeName == "" {
			task.AssigneeName = rec.Inspector
		}
	})
	if err != nil {
		return err
	}
	task, err := s.store.GetEngineeringTask(taskID)
	if err == nil && task != nil && task.PlanItemID != "" {
		_ = s.store.UpdateEngineeringPlanLatestTask(task.PlanItemID, task.ID)
	}
	return nil
}

func parseFloatOrZero(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}
