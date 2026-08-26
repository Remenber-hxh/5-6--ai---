package main

import (
	"database/sql"
	"encoding/json"
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
	engPlanStatusRectify    = "待整改" // 计划下有待整改任务 → 归"需跟进"，与移动端异常资产口径一致
	engPlanStatusDone       = "已完成"
	engPlanStatusUnplanned  = "未排期"
	engTaskStatusDraft      = "待下发" // 管理员已建、尚未下发到移动端，巡检员看不到
	engTaskStatusPending    = "待执行"
	engTaskStatusProcessing = "进行中"
	engTaskStatusDone       = "已完成"
	engTaskStatusOverdue    = "逾期"
	engTaskStatusCanceled   = "已取消"
	engTaskStatusRectify    = "待整改" // 执行已完成但资产异常，待整改闭环
)

// EngineeringPlanItem 是工程年度计划事项，不等同于每日巡检点位。
// 它承接真实 Excel 里的「强制检测 / 重点外委 / 零星工程 / CAPEX」等计划。
type EngineeringPlanItem struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	SequenceNo   string  `json:"sequenceNo"`
	BusinessType string  `json:"businessType"`
	Project      string  `json:"project"`
	Category     string  `json:"category"`
	SubType      string  `json:"subType"`
	WorkContent  string  `json:"workContent"`
	ScopeDesc    string  `json:"scopeDesc"`
	BudgetAmount float64 `json:"budgetAmount"`
	BudgetText   string  `json:"budgetText"`
	PlanStart    string  `json:"planStart"`
	PlanEnd      string  `json:"planEnd"`
	OwnerName    string  `json:"ownerName"`
	CycleText    string  `json:"cycleText"`
	Remark       string  `json:"remark"`
	Status       string  `json:"status"`
	RiskLevel    string  `json:"riskLevel"`
	// PlanType 计划类型:yearly / monthly / weekly / daily / adhoc(临时,对外部项目组)。
	// 空 = 临时 —— 存量数据没有这个字段,而它们本来就是零散录进来的。
	PlanType string `json:"planType"`
	// Weekdays 每日计划专用:一周哪几天执行,形如 "1,2,3,4,5"(1=周一 … 7=周日)。
	// 空 = 每天。其他类型的计划忽略这个字段。
	Weekdays string `json:"weekdays"`
	// AssetIDs 这条计划要巡哪些设备。【每日计划必须有】——
	// "完成"是自动判定的(这些设备今天有没有巡检快照),没有清单就无从判起。
	AssetIDs     []string  `json:"assetIds,omitempty"`
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
	// TenantID 空 = 回落默认租户。
	//
	// 【原来这张表根本不过滤租户】SQL 里没有 tenant_id、engineeringTaskMatches
	// 里也没有 —— 而记录、资产、离线照片全都过滤。009 迁移明明给
	// engineering_tasks 加了 tenant_id,只是查询一直没跟上。
	// 现在单租户看不出来,多客户一铺开就是跨租户串数据。
	// 空值回落默认租户,与 009 的既定做法一致(漏设的落到默认租户 = 单租户安全行为)。
	TenantID   string
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
		{ID: "eng_plan_gh_001", SequenceNo: "1", Project: "会议中心", Category: "强制检测", WorkContent: "建筑消防设施检验报告", ScopeDesc: "年度消防设施检测与报告归档", BudgetAmount: 57456, BudgetText: "57456", PlanStart: "2026-12-01", PlanEnd: "2026-12-31", OwnerName: "丛洪忠", CycleText: "每年一次", Status: engPlanStatusPending, RiskLevel: "normal"},
		{ID: "eng_plan_gh_002", SequenceNo: "2", Project: "会议中心", Category: "强制检测", WorkContent: "水质检测报告", ScopeDesc: "生活水质检测与报告留档", BudgetAmount: 4500, BudgetText: "4500", PlanStart: "2026-01", PlanEnd: "2026-07", OwnerName: "董圣军", CycleText: "每年1月、7月检测", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_003", SequenceNo: "3", Project: "会议中心", Category: "强制检测", WorkContent: "军团菌检测", ScopeDesc: "重点涉水系统军团菌检测", BudgetAmount: 6400, BudgetText: "6400", PlanStart: "2026-07", PlanEnd: "2027-07", OwnerName: "丛洪忠", CycleText: "每年1月、4月、7月、10月检测", Status: engPlanStatusPending, RiskLevel: "warning"},
		{ID: "eng_plan_gh_004", SequenceNo: "4", Project: "会议中心", Category: "强制检测", WorkContent: "电梯安全年度检验报告", ScopeDesc: "电梯年度安全检验、问题整改与报告归档", BudgetAmount: 23000, BudgetText: "23000", PlanStart: "2026-04", PlanEnd: "2026-10", OwnerName: "朱佳伟", CycleText: "每年4月、7月检测", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_005", SequenceNo: "5", Project: "会议中心", Category: "强制检测", WorkContent: "绝缘工器具检验报告", ScopeDesc: "高压操作工具、验电器、绝缘用品检测", BudgetAmount: 12000, BudgetText: "12000", PlanStart: "2026-06", PlanEnd: "2026-12", OwnerName: "新博", CycleText: "半年/年度检测", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_006", SequenceNo: "6", Project: "会议中心", Category: "强制检测", WorkContent: "防雷装置定期检查报告", ScopeDesc: "防雷装置检测与隐患整改跟踪", BudgetAmount: 30000, BudgetText: "30000", PlanStart: "2026-09", PlanEnd: "2026-11", OwnerName: "杜宪奎", CycleText: "每年一次", Status: engPlanStatusPending, RiskLevel: "normal"},
		{ID: "eng_plan_gh_007", SequenceNo: "7", Project: "会议中心", Category: "强制检测", WorkContent: "排水系统排污检测", ScopeDesc: "排污许可证有效期、排放检测与资料归档", BudgetAmount: 0, BudgetText: "", PlanStart: "2024-06-19", PlanEnd: "2029-06-18", OwnerName: "董圣军", CycleText: "关注许可证到期", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_008", SequenceNo: "8", Project: "会议中心", Category: "重点外委", WorkContent: "电梯维保", ScopeDesc: "电梯维保、故障响应与年度资料闭环", BudgetAmount: 196920, BudgetText: "196920", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "朱佳伟", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_009", SequenceNo: "9", Project: "会议中心", Category: "重点外委", WorkContent: "中央空调主机维保", ScopeDesc: "中央空调主机年度维保与运行保障", BudgetAmount: 180000, BudgetText: "180000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "新博", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_010", SequenceNo: "10", Project: "会议中心", Category: "重点外委", WorkContent: "智能化系统维保", ScopeDesc: "网络、信息发布、灯光投影等智能化软硬件维保", BudgetAmount: 290000, BudgetText: "290000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "汪佳澜", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
		{ID: "eng_plan_gh_011", SequenceNo: "11", Project: "会议中心", Category: "重点外委", WorkContent: "消防系统维保合同", ScopeDesc: "消防系统维保、报警联动与故障闭环", BudgetAmount: 220428.44, BudgetText: "220428.44", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "杜宪奎", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "warning"},
		{ID: "eng_plan_gh_012", SequenceNo: "12", Project: "会议中心", Category: "重点外委", WorkContent: "锅炉维保", ScopeDesc: "锅炉设备维保、巡检和运行保障", BudgetAmount: 110000, BudgetText: "110000", PlanStart: "2026-01-01", PlanEnd: "2026-12-31", OwnerName: "新博", CycleText: "全年维保", Status: engPlanStatusRunning, RiskLevel: "normal"},
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
		{ID: "eng_task_gh_002_202607", PlanItemID: "eng_plan_gh_002", Title: "水质检测报告 7 月执行", Project: "会议中心", Category: "强制检测", WorkContent: "水质检测报告", AssigneeName: "董圣军", DueAt: "2026-07-31", Status: engTaskStatusPending, EvidenceStatus: "待上传", AIStatus: "待分析", BudgetAmount: 4500},
		{ID: "eng_task_gh_004_202607", PlanItemID: "eng_plan_gh_004", Title: "电梯安全年度检验 7 月跟踪", Project: "会议中心", Category: "强制检测", WorkContent: "电梯安全年度检验报告", AssigneeName: "朱佳伟", DueAt: "2026-07-31", Status: engTaskStatusProcessing, EvidenceStatus: "待补充", AIStatus: "待分析", BudgetAmount: 23000},
		{ID: "eng_task_gh_008_202606", PlanItemID: "eng_plan_gh_008", Title: "电梯维保月度资料复核", Project: "会议中心", Category: "重点外委", WorkContent: "电梯维保", AssigneeName: "朱佳伟", DueAt: "2026-06-30", Status: engTaskStatusProcessing, EvidenceStatus: "部分上传", AIStatus: "待分析", BudgetAmount: 196920},
		{ID: "eng_task_gh_010_202606", PlanItemID: "eng_plan_gh_010", Title: "智能化系统维保月度跟踪", Project: "会议中心", Category: "重点外委", WorkContent: "智能化系统维保", AssigneeName: "汪佳澜", DueAt: "2026-06-30", Status: engTaskStatusPending, EvidenceStatus: "待上传", AIStatus: "待分析", BudgetAmount: 290000},
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
		       latest_task_id, created_at, updated_at, plan_type, weekdays,
		       COALESCE(asset_ids_json, '[]')
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
		       latest_task_id, created_at, updated_at, plan_type, weekdays,
		       COALESCE(asset_ids_json, '[]')
		FROM engineering_plan_items WHERE id=?`, id)
	return scanEngineeringPlan(row)
}

func (s *SQLiteStore) UpsertEngineeringPlan(item *EngineeringPlanItem) error {
	normalizeEngineeringPlan(item)
	created := fmtStamp(item.CreatedAt)
	updated := fmtStamp(item.UpdatedAt)
	var query string
	if s.dialect == "mysql" {
		query = `
			INSERT INTO engineering_plan_items (
				id, source, sequence_no, business_type, project, category, sub_type,
				work_content, scope_desc, budget_amount, budget_text, plan_start,
				plan_end, owner_name, cycle_text, remark, status, risk_level,
				latest_task_id, created_at, updated_at, plan_type, weekdays, asset_ids_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				source=VALUES(source), sequence_no=VALUES(sequence_no), business_type=VALUES(business_type),
				project=VALUES(project), category=VALUES(category), sub_type=VALUES(sub_type),
				work_content=VALUES(work_content), scope_desc=VALUES(scope_desc), budget_amount=VALUES(budget_amount),
				budget_text=VALUES(budget_text), plan_start=VALUES(plan_start), plan_end=VALUES(plan_end),
				owner_name=VALUES(owner_name), cycle_text=VALUES(cycle_text), remark=VALUES(remark),
				status=VALUES(status), risk_level=VALUES(risk_level), latest_task_id=VALUES(latest_task_id),
				updated_at=VALUES(updated_at), plan_type=VALUES(plan_type), weekdays=VALUES(weekdays),
				asset_ids_json=VALUES(asset_ids_json)`
	} else {
		query = `
			INSERT INTO engineering_plan_items (
				id, source, sequence_no, business_type, project, category, sub_type,
				work_content, scope_desc, budget_amount, budget_text, plan_start,
				plan_end, owner_name, cycle_text, remark, status, risk_level,
				latest_task_id, created_at, updated_at, plan_type, weekdays, asset_ids_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				source=excluded.source, sequence_no=excluded.sequence_no, business_type=excluded.business_type,
				project=excluded.project, category=excluded.category, sub_type=excluded.sub_type,
				work_content=excluded.work_content, scope_desc=excluded.scope_desc, budget_amount=excluded.budget_amount,
				budget_text=excluded.budget_text, plan_start=excluded.plan_start, plan_end=excluded.plan_end,
				owner_name=excluded.owner_name, cycle_text=excluded.cycle_text, remark=excluded.remark,
				status=excluded.status, risk_level=excluded.risk_level, latest_task_id=excluded.latest_task_id,
				updated_at=excluded.updated_at, plan_type=excluded.plan_type, weekdays=excluded.weekdays,
				asset_ids_json=excluded.asset_ids_json`
	}
	// 设备清单存 JSON。序列化失败也要给个合法的空数组 —— 存进 NULL 或空串
	// 会让下次读取时解析报错,而那时候已经查不出是哪一条写坏的了。
	assetIDsJSON := "[]"
	if raw, mErr := json.Marshal(item.AssetIDs); mErr == nil && item.AssetIDs != nil {
		assetIDsJSON = string(raw)
	}
	_, err := s.db.Exec(query,
		item.ID, item.Source, item.SequenceNo, item.BusinessType, item.Project, item.Category, item.SubType,
		item.WorkContent, item.ScopeDesc, item.BudgetAmount, item.BudgetText, item.PlanStart,
		item.PlanEnd, item.OwnerName, item.CycleText, item.Remark, item.Status, item.RiskLevel,
		item.LatestTaskID, created, updated, item.PlanType, item.Weekdays, assetIDsJSON,
	)
	return err
}

func (s *SQLiteStore) UpdateEngineeringPlanLatestTask(planID, taskID string) error {
	now := nowStamp()
	res, err := s.db.Exec(`UPDATE engineering_plan_items SET latest_task_id=?, updated_at=? WHERE id=?`, taskID, now, planID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListEngineeringTasks 读整张表再在 Go 里筛。
//
// 【已知的剩余口子】几个只拿到 project 字符串、拿不到 *http.Request 的调用点
// (周报/日报/计划重算)会走 filter.TenantID 为空 → 回落默认租户这条路。
// 单租户下是对的;真要做多客户,那几处得把租户一路传进来。
// 至少现在 SQL 里【有】tenant_id 这个条件了 —— 之前是完全不过滤,
// 一开第二个客户就是直接串数据。
func (s *SQLiteStore) ListEngineeringTasks(filter EngineeringTaskFilter) ([]*EngineeringTask, error) {
	rows, err := s.db.Query(`
		SELECT id, plan_item_id, source, task_type, title, project, category,
		       work_content, assignee_name, reviewer_name, vendor_name, due_at,
		       started_at, completed_at, status, evidence_status, ai_status,
		       record_id, report_id, asset_id, budget_amount, close_result,
		       close_note, created_at, updated_at
		FROM engineering_tasks
		WHERE tenant_id = ?
		ORDER BY due_at ASC, updated_at DESC`, tenantOrDefault(filter.TenantID))
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
	created := fmtStamp(task.CreatedAt)
	updated := fmtStamp(task.UpdatedAt)
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
	updated := fmtStamp(task.UpdatedAt)
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
	var created, updated, assetIDsJSON string
	err := row.Scan(
		&item.ID, &item.Source, &item.SequenceNo, &item.BusinessType, &item.Project, &item.Category, &item.SubType,
		&item.WorkContent, &item.ScopeDesc, &item.BudgetAmount, &item.BudgetText, &item.PlanStart,
		&item.PlanEnd, &item.OwnerName, &item.CycleText, &item.Remark, &item.Status, &item.RiskLevel,
		&item.LatestTaskID, &created, &updated, &item.PlanType, &item.Weekdays, &assetIDsJSON,
	)
	if err != nil {
		return nil, err
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	// 【空类型一律当临时】存量计划没有这个字段,而它们本来就是零散录进来的。
	// 在这里补齐,前端就不用到处判空 —— 判空散在各处必然漏一处。
	if item.PlanType == "" {
		item.PlanType = planTypeAdhoc
	}
	// 解析失败就当空清单:一条计划的设备清单坏了,不该让整页计划打不开
	_ = json.Unmarshal([]byte(assetIDsJSON), &item.AssetIDs)
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

func (s *Server) engineeringTaskFilterFromRequest(r *http.Request) EngineeringTaskFilter {
	q := r.URL.Query()
	return EngineeringTaskFilter{
		TenantID:   s.tenantForRequest(r),
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
	// 【计划和任务都带项目名,按项目筛】汇总数一起筛掉 ——
	// 列表干净了但顶上还写着总数,一样把别的项目有多少条透出去。
	items = filterPlansByScope(items, s.projectScopeFor(r, ""))
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
	items, err := s.store.ListEngineeringTasks(s.engineeringTaskFilterFromRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_engineering_tasks_failed", err.Error())
		return
	}
	items = filterTasksByScope(items, s.projectScopeFor(r, ""))
	// ?scope=open-mine:只要"我的在办任务",与底栏角标共用同一套规则
	// (见 open_tasks.go)。不传时保持项目范围内的全量 —— admin-web 要看全量。
	if r.URL.Query().Get("scope") == "open-mine" {
		items = openTasksFor(items, s.taskScopeName(r))
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
	// 去重守卫：一个计划同一时间只应有一个在途任务，避免重复下发产生“幽灵任务”
	// 把计划永久卡在执行中。已有未完成（待下发/待执行/进行中/待整改/逾期）任务时，直接复用。
	if req.PlanItemID != "" {
		if existing, err := s.store.ListEngineeringTasks(EngineeringTaskFilter{PlanItemID: req.PlanItemID}); err == nil {
			for _, t := range existing {
				if t == nil {
					continue
				}
				switch t.Status {
				case engTaskStatusDraft, engTaskStatusPending, engTaskStatusProcessing, engTaskStatusRectify, engTaskStatusOverdue:
					writeJSON(w, http.StatusOK, map[string]any{"task": t, "reused": true})
					return
				}
			}
		}
	}
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
	now := nowStamp()
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
	// 后台手动改任务状态后，同步重算挂钩计划状态（完成→已完成 / 重开→执行中等）。
	// 注意：手动操作不触发循环滚动，避免点一下就凭空多出下期任务；滚动只在移动端提交闭环时发生。
	if task != nil && task.PlanItemID != "" {
		s.recomputeEngineeringPlanStatus(task.PlanItemID, id)
	}
	s.recordOperation(r, "engineering_task_status", "engineering_task", id, map[string]any{
		"status": status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

// isAnomalyAssetStatus 判定资产是否处于"未闭环异常"状态。
func isAnomalyAssetStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "异常", "待复核", "待维修":
		return true
	}
	return false
}

func (s *Server) closeEngineeringTaskFromRecord(rec *Record, assets []*AssetEntry, closedAt time.Time) error {
	if rec == nil || strings.TrimSpace(rec.EngineeringTaskID) == "" {
		return nil
	}
	taskID := strings.TrimSpace(rec.EngineeringTaskID)
	// 选定关联资产与闭环结果：优先指向第一个异常资产，否则取第一个资产。
	assetID := ""
	closeResult := "完成"
	anomaly := false
	for _, a := range assets {
		if a == nil {
			continue
		}
		if assetID == "" {
			assetID = a.ID
			if a.LastStatus != "" {
				closeResult = a.LastStatus
			}
		}
		if isAnomalyAssetStatus(a.LastStatus) {
			anomaly = true
			assetID = a.ID
			closeResult = a.LastStatus
			break
		}
	}
	// 正常 → 已完成；异常 → 待整改（执行已完成，但异常未闭环）
	finalStatus := engTaskStatusDone
	if anomaly {
		finalStatus = engTaskStatusRectify
	}
	closeNote := firstNonEmpty(rec.AISummary, rec.Report, "巡检记录已提交，工程任务自动闭环")
	err := s.store.UpdateEngineeringTask(taskID, func(task *EngineeringTask) {
		task.Status = finalStatus
		task.RecordID = rec.ID
		task.AssetID = assetID
		task.CompletedAt = fmtStamp(closedAt)
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
	if task, err := s.store.GetEngineeringTask(taskID); err == nil && task != nil && task.PlanItemID != "" {
		s.afterEngineeringTaskDone(task.PlanItemID, task.ID)
	}
	return nil
}

// recomputeEngineeringPlanStatus 任务状态变化后，按该计划下所有任务重算计划状态。
// 规则：有待整改 → 执行中(留异常)；任务都已闭环且无在途 → 已完成；有在途 → 执行中；否则待执行。
func (s *Server) recomputeEngineeringPlanStatus(planID, latestTaskID string) {
	if strings.TrimSpace(planID) == "" {
		return
	}
	plan, err := s.store.GetEngineeringPlan(planID)
	if err != nil || plan == nil {
		return
	}
	tasks, _ := s.store.ListEngineeringTasks(EngineeringTaskFilter{PlanItemID: planID})
	// running：真正在推进/有待闭环异常的任务；pending：建好但还没开始（待下发/待执行）；done：已完成
	openAnomaly, running, pending, done := 0, 0, 0, 0
	for _, t := range tasks {
		switch t.Status {
		case engTaskStatusRectify:
			openAnomaly++
			running++
		case engTaskStatusProcessing, engTaskStatusOverdue:
			running++
		case engTaskStatusDraft, engTaskStatusPending:
			pending++
		case engTaskStatusDone:
			done++
		}
	}
	var status string
	switch {
	case openAnomaly > 0:
		status = engPlanStatusRectify // 有待整改 → 需跟进（与移动端异常资产口径一致）
	case running > 0:
		status = engPlanStatusRunning // 进行中/逾期（无待整改）→ 执行中
	case pending > 0 && done == 0:
		status = engPlanStatusPending // 只有待下发/待执行、尚无完成 → 待执行
	case pending > 0 && done > 0:
		status = engPlanStatusRunning // 做完一部分还有下一期待执行 → 执行中
	case done > 0:
		status = engPlanStatusDone // 全部完成、无在途无待办 → 已完成
	default:
		status = engPlanStatusPending
	}
	plan.Status = status
	if strings.TrimSpace(latestTaskID) != "" {
		plan.LatestTaskID = latestTaskID
	}
	plan.UpdatedAt = time.Now()
	_ = s.store.UpsertEngineeringPlan(plan)
}

// ensureFollowupTaskForAnomalies 巡检检出异常（需跟进）但没有在途任务覆盖该资产时，
// 自动建一条「待整改」任务挂到资产，使移动端「我的任务」立即出现可跟进项。
// 计划挂钩的巡检已由 closeEngineeringTaskFromRecord 处理，这里只兜底临时/非计划巡检。
func (s *Server) ensureFollowupTaskForAnomalies(rec *Record, assets []*AssetEntry, now time.Time) {
	if rec == nil {
		return
	}
	existing, _ := s.store.ListEngineeringTasks(EngineeringTaskFilter{})
	for _, a := range assets {
		if a == nil || !isAnomalyAssetStatus(a.LastStatus) {
			continue
		}
		covered := false
		for _, t := range existing {
			if t == nil || t.AssetID != a.ID {
				continue
			}
			switch t.Status {
			case engTaskStatusDraft, engTaskStatusPending, engTaskStatusProcessing, engTaskStatusRectify, engTaskStatusOverdue:
				covered = true
			}
		}
		if covered {
			continue
		}
		task := &EngineeringTask{
			ID:             newID("eng_task"),
			Source:         "auto-followup",
			TaskType:       "异常复查",
			Title:          firstNonEmpty(a.AssetName, "设备") + " 异常复查",
			Project:        a.Project,
			Category:       a.AssetType,
			AssetID:        a.ID,
			RecordID:       rec.ID,
			AssigneeName:   rec.Inspector,
			DueAt:          now.Format("2006-01-02"),
			Status:         engTaskStatusRectify,
			EvidenceStatus: "待复查",
			AIStatus:       firstNonEmpty(rec.RecognitionStatus, "已检出异常"),
			CloseResult:    a.LastStatus,
			CloseNote:      firstNonEmpty(rec.AISummary, "巡检检出异常，待复查闭环"),
		}
		normalizeEngineeringTask(task)
		task.Status = engTaskStatusRectify
		_ = s.store.CreateEngineeringTask(task)
	}
}

// onAssetResolvedNormal 资产被改回"正常"（标记正常 / 修改审批通过）后，
// 把指向该资产、仍处于"待整改"的任务闭环，并重算其计划状态——异常闭环回写。
func (s *Server) onAssetResolvedNormal(assetID string) {
	if strings.TrimSpace(assetID) == "" {
		return
	}
	tasks, err := s.store.ListEngineeringTasks(EngineeringTaskFilter{})
	if err != nil {
		return
	}
	for _, t := range tasks {
		if t == nil || t.AssetID != assetID || t.Status != engTaskStatusRectify {
			continue
		}
		planID := t.PlanItemID
		_ = s.store.UpdateEngineeringTask(t.ID, func(task *EngineeringTask) {
			task.Status = engTaskStatusDone
			task.CloseResult = "整改闭环"
		})
		if planID != "" {
			s.afterEngineeringTaskDone(planID, t.ID)
		}
	}
}

// afterEngineeringTaskDone 任务转为已完成后：循环计划生成下期任务，然后重算计划状态。
func (s *Server) afterEngineeringTaskDone(planID, taskID string) {
	if strings.TrimSpace(planID) == "" {
		return
	}
	if plan, err := s.store.GetEngineeringPlan(planID); err == nil && plan != nil {
		if task, e := s.store.GetEngineeringTask(taskID); e == nil && task != nil && task.Status == engTaskStatusDone {
			s.maybeRolloverPlanTask(plan, task)
		}
	}
	s.recomputeEngineeringPlanStatus(planID, taskID)
}

// inferCycleIntervalMonths 从计划周期自由文本粗推间隔月数；0 表示一次性（不滚动）。
func inferCycleIntervalMonths(cycle string) int {
	c := strings.TrimSpace(cycle)
	if c == "" {
		return 0
	}
	switch {
	case strings.Contains(c, "每月") || strings.Contains(c, "全年"):
		return 1
	case strings.Contains(c, "每季") || strings.Contains(c, "季度"):
		return 3
	case strings.Contains(c, "半年"):
		return 6
	}
	// 列出多个检测月份 → 按年内次数推间隔（如“每年1月、7月”=2次=半年）
	switch n := strings.Count(c, "月"); {
	case n >= 4:
		return 3
	case n >= 2:
		return 6
	}
	// “每年一次” / “关注许可证到期” / 无法识别 → 一次性
	return 0
}

// nextDueDate 在原到期日基础上 +months 个月；原日期解析失败则从当前时间推算。
func nextDueDate(dueAt string, months int) string {
	d := strings.TrimSpace(dueAt)
	var base time.Time
	parsed := false
	for _, layout := range []string{"2006-01-02", "2006-01", "2006/01/02", "2006/01"} {
		if t, err := time.Parse(layout, d); err == nil {
			base = t
			parsed = true
			break
		}
	}
	if !parsed {
		base = time.Now()
	}
	return base.AddDate(0, months, 0).Format("2006-01-02")
}

// rolloverTaskTitle 生成下期任务标题，如“电梯维保 · 2026年8月期”。
func rolloverTaskTitle(plan *EngineeringPlanItem, nextDue string) string {
	label := nextDue
	if t, err := time.Parse("2006-01-02", nextDue); err == nil {
		label = t.Format("2006年1月")
	}
	return firstNonEmpty(plan.WorkContent, "巡检") + " · " + label + "期"
}

// maybeRolloverPlanTask 循环计划做完一期后生成下一期待执行任务（含去重与在途守卫）。
func (s *Server) maybeRolloverPlanTask(plan *EngineeringPlanItem, done *EngineeringTask) {
	if plan == nil || done == nil || done.Status != engTaskStatusDone {
		return
	}
	months := inferCycleIntervalMonths(plan.CycleText)
	if months <= 0 {
		return // 一次性计划：不滚动，交由 recompute 标记完成
	}
	tasks, _ := s.store.ListEngineeringTasks(EngineeringTaskFilter{PlanItemID: plan.ID})
	for _, t := range tasks {
		if t == nil || t.ID == done.ID {
			continue
		}
		switch t.Status {
		case engTaskStatusPending, engTaskStatusProcessing, engTaskStatusRectify, engTaskStatusOverdue:
			return // 已有在途任务，不重复生成
		}
	}
	nextDue := nextDueDate(done.DueAt, months)
	for _, t := range tasks {
		if t != nil && t.DueAt == nextDue {
			return // 同到期日任务已存在
		}
	}
	now := time.Now()
	next := &EngineeringTask{
		ID:             newID("eng_task"),
		PlanItemID:     plan.ID,
		Source:         "rollover",
		TaskType:       done.TaskType,
		Title:          rolloverTaskTitle(plan, nextDue),
		Project:        plan.Project,
		Category:       plan.Category,
		WorkContent:    firstNonEmpty(done.WorkContent, plan.WorkContent),
		AssigneeName:   done.AssigneeName,
		ReviewerName:   done.ReviewerName,
		VendorName:     done.VendorName,
		DueAt:          nextDue,
		Status:         engTaskStatusPending,
		EvidenceStatus: "待上传",
		AIStatus:       "待分析",
		BudgetAmount:   plan.BudgetAmount,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_ = s.store.CreateEngineeringTask(next)
}

func parseFloatOrZero(raw string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return v
}

// filterPlansByScope 巡检计划按项目范围裁。
//
// 计划条目自带 Project,所以这里能做真正的项目过滤,不用像修改申请那样
// 退回"只看自己的"。
func filterPlansByScope(in []*EngineeringPlanItem, scope projectScope) []*EngineeringPlanItem {
	if scope.Requested == "" && len(scope.Allowed) == 0 && !scope.Blocked {
		return in
	}
	out := make([]*EngineeringPlanItem, 0, len(in))
	for _, p := range in {
		if p != nil && scope.allows(p.Project) {
			out = append(out, p)
		}
	}
	return out
}
