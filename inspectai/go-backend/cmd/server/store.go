package main

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

//go:embed schema_mysql.sql
var schemaMySQL string

// ===== 数据访问接口:按业务域拆分 =====
// 各业务模块应依赖所需的最小域接口(如资产模块只收 AssetStore),
// Store 为全量组合,供装配层(main)与既有代码平滑过渡。
// MemStore(测试/fallback)与 SQLiteStore(SQLite+MySQL)实现全部域。

// RecordStore — 巡检记录
type RecordStore interface {
	CreateRecord(rec *Record) error
	GetRecord(tenantID, id string) (*Record, error)
	UpdateRecord(rec *Record) error
	ListRecords(tenantID string, limit int) ([]*Record, error)
	ListRecordsByOwner(tenantID, inspectorUserID, displayName, username string, limit int) ([]*Record, error)
	// DeleteDraftRecord 删除【未提交】的记录及其关联行,并把当初认领的离线照片
	// 放回待处理。已提交的记录是台账证据,这里一律拒绝 —— 要撤销走审批流。
	// 记录已提交时返回 errRecordSubmitted,不存在/跨租户返回 sql.ErrNoRows。
	DeleteDraftRecord(tenantID, id string) error
}

// TenantStore — 客户租户(仅平台超管可写)
type TenantStore interface {
	CreateTenant(t *Tenant) error
	ListTenants() ([]*Tenant, error)
}

// AITaskStore — 视觉识别任务
type AITaskStore interface {
	CreateTask(task *AITask) error
	GetTask(id string) (*AITask, error)
	UpdateTask(id string, mutate func(*AITask)) error
	LatestTaskByRecord(recordID string) (*AITask, error)
}

// AssetStore — 资产台账 + 快照/字段观测(§3 趋势底座)
type AssetStore interface {
	// UpsertAsset 巡检驱动的自动建档/滚动更新(巡检数 +1)。
	UpsertAsset(asset *AssetEntry) error
	// CreateAsset 手工建档(巡检数从 0 起,重复 ID 报错)。
	CreateAsset(asset *AssetEntry) error
	// SubmitRecordWithAssets —— 原子提交:日报、资产台账、资产快照、字段观测同事务写入,
	// 失败整体回滚,避免出现"日报已提交但快照没记录"的数据缺口。
	SubmitRecordWithAssets(rec *Record, assets []*AssetEntry, snaps []*AssetSnapshot, obs []*FieldObservation) error
	ListAssets(tenantID string) ([]*AssetEntry, error)
	GetAsset(tenantID, id string) (*AssetEntry, error)
	UpdateAssetMeta(tenantID, id, assetName, lastStatus, lastSummary string) (*AssetEntry, error)
	// UpdateAssetCover 仅更新主管指定的封面图路径 cover_image_path。
	UpdateAssetCover(tenantID, id, coverImagePath string) (*AssetEntry, error)
	// DeleteAsset 删除资产及其快照/字段观测(巡检记录保留作历史证据)。
	DeleteAsset(tenantID, id string) error
	// 幂等写入,按 asset_id 完整翻历史
	WriteAssetSnapshots(snapshots []*AssetSnapshot, observations []*FieldObservation) error
	ListAssetSnapshots(assetID string, limit, offset int) ([]*AssetSnapshot, error)
	CountAssetSnapshots(assetID string) (int, error)
	ListFieldObservations(assetID, fieldKey string, limit int) ([]*FieldObservation, error)
}

// ConfirmLogStore — 字段人工确认留痕(§4 防惰性闭环)
type ConfirmLogStore interface {
	CreateFieldConfirmLog(entry *FieldConfirmLog) error
	ListFieldConfirmLogs(recordID string) ([]*FieldConfirmLog, error)
	ListRecentFieldConfirmLogs(limit int) ([]*FieldConfirmLog, error)
}

// ReportCacheStore — 管理 AI 报告持久化缓存(30 min + 异常触发刷新)
type ReportCacheStore interface {
	SaveManagementAIReport(report *ManagementAIReport) error
	GetLatestManagementAIReport(reportType, project, rangeKey string) (*ManagementAIReport, error)
	DeleteExpiredManagementAIReports(now time.Time) (int, error)
}

// ApprovalStore — 修改申请审批流
type ApprovalStore interface {
	CreateChangeRequest(cr *ChangeRequest) error
	ListChangeRequests(filter ChangeRequestFilter) ([]*ChangeRequest, error)
	GetChangeRequest(id string) (*ChangeRequest, error)
	UpdateChangeRequest(cr *ChangeRequest) error
}

// IdentityStore — 账号/会话/角色/部门 + 操作审计
type IdentityStore interface {
	EnsureIdentitySeed(seed IdentitySeed) error
	AuthenticateUser(username, password string) (*User, *LoginSession, error)
	GetUserBySession(token string) (*User, error)
	GetUser(id string) (*User, error)
	ListUsers() ([]*User, error)
	CreateUser(user *User, password string) error
	UpdateUserProfile(id string, mutate func(*User)) error
	SetUserPassword(id, password string) error
	SetUserStatus(id, status string) error
	DeleteSession(token string) error
	DeleteUserSessions(userID string) error
	ListRoles() ([]*Role, error)
	GetRoleByCode(code string) (*Role, bool, error)
	// CreateRole / UpdateRole / DeleteRole — 自定义角色管理;
	// DeleteRole 有用户引用时报错,且事务内连带清理该角色的权限矩阵行。
	CreateRole(role *Role) error
	UpdateRole(id, name, description string) (*Role, error)
	DeleteRole(id string) error
	ListDepartments() ([]*Department, error)
	CreateOperationLog(log *OperationLog) error
	ListOperationLogs(limit int) ([]*OperationLog, error)
}

// EngineeringStore — 工程巡检计划与任务(闭环状态机的数据层)
type EngineeringStore interface {
	ListEngineeringPlans(filter EngineeringPlanFilter) ([]*EngineeringPlanItem, error)
	GetEngineeringPlan(id string) (*EngineeringPlanItem, error)
	UpsertEngineeringPlan(item *EngineeringPlanItem) error
	UpdateEngineeringPlanLatestTask(planID, taskID string) error
	ListEngineeringTasks(filter EngineeringTaskFilter) ([]*EngineeringTask, error)
	GetEngineeringTask(id string) (*EngineeringTask, error)
	CreateEngineeringTask(task *EngineeringTask) error
	UpdateEngineeringTask(id string, mutate func(*EngineeringTask)) error
}

// SubmissionStore — 提交幂等锁
type SubmissionStore interface {
	ClaimSubmission(recordID, idemKey string) (string, error)
	CompleteSubmission(recordID, idemKey string) error
	ReleaseSubmission(recordID, idemKey string) error
}

// PromptTemplateStore — 模块化提示词(后台可视化编辑、即时生效)
type PromptTemplateStore interface {
	ListPromptTemplates() ([]PromptTemplate, error)
	GetPromptTemplate(id string) (PromptTemplate, bool, error)
	UpsertPromptTemplate(t PromptTemplate) error
}

// PermissionStore — 角色×能力 权限矩阵(后台可视化配置)
type PermissionStore interface {
	// ListRolePermissions 返回 permKey -> 允许的角色 code 列表
	ListRolePermissions() (map[string][]string, error)
	// ReplaceRolePermissions 全量覆盖矩阵(事务)
	ReplaceRolePermissions(matrix map[string][]string) error
}

// Store — 全量组合(装配层用;业务模块请依赖上面的最小域接口)
type Store interface {
	RecordStore
	TenantStore
	OfflineShotStore
	AITaskStore
	AssetStore
	ConfirmLogStore
	ReportCacheStore
	ApprovalStore
	IdentityStore
	EngineeringStore
	SubmissionStore
	PromptTemplateStore
	PermissionStore

	Close() error
}

// 编译期断言:两个实现必须覆盖全部域,漏实现在编译时即暴露
var (
	_ Store = (*MemStore)(nil)
	_ Store = (*SQLiteStore)(nil)
)

const (
	submissionClaimed    = "claimed"
	submissionDuplicate  = "duplicate"
	submissionInProgress = "in_progress"
	submissionBusy       = "busy"

	submissionProcessingTTL = 15 * time.Minute
)

// ===== MemStore（测试 / fallback） =====

type submissionState struct {
	IdemKey   string
	Status    string
	UpdatedAt time.Time
}

type MemStore struct {
	mu             sync.RWMutex
	tenants        map[string]*Tenant
	offlineShots   map[string]*OfflineShot
	records        map[string]*Record
	tasks          map[string]*AITask
	assets         map[string]*AssetEntry
	changeRequests map[string]*ChangeRequest
	submissions    map[string]submissionState
	users          map[string]*memUser
	sessions       map[string]*LoginSession
	operationLogs  map[string]*OperationLog
	assetSnapshots []*AssetSnapshot
	fieldObs       []*FieldObservation
	confirmLogs    []*FieldConfirmLog
	mgmtReports    []*ManagementAIReport
	engPlans       map[string]*EngineeringPlanItem
	engTasks       map[string]*EngineeringTask
	promptTpls     map[string]PromptTemplate
	rolePerms      map[string][]string
	roles          map[string]*Role
}

type memUser struct {
	user         *User
	passwordHash string
}

func NewMemStore() *MemStore {
	return &MemStore{
		tenants:        map[string]*Tenant{},
		offlineShots:   map[string]*OfflineShot{},
		records:        map[string]*Record{},
		tasks:          map[string]*AITask{},
		assets:         map[string]*AssetEntry{},
		changeRequests: map[string]*ChangeRequest{},
		submissions:    map[string]submissionState{},
		users:          map[string]*memUser{},
		sessions:       map[string]*LoginSession{},
		operationLogs:  map[string]*OperationLog{},
		engPlans:       map[string]*EngineeringPlanItem{},
		engTasks:       map[string]*EngineeringTask{},
		promptTpls:     map[string]PromptTemplate{},
		rolePerms:      defaultPermMatrix(),
	}
}

// ===== 模块化提示词:MemStore 实现 =====

func (s *MemStore) ListPromptTemplates() ([]PromptTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]PromptTemplate, 0, len(s.promptTpls))
	for _, t := range s.promptTpls {
		out = append(out, t)
	}
	return out, nil
}

func (s *MemStore) GetPromptTemplate(id string) (PromptTemplate, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.promptTpls[id]
	return t, ok, nil
}

func (s *MemStore) UpsertPromptTemplate(t PromptTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.promptTpls[t.ID] = t
	return nil
}

func (s *MemStore) CreateRecord(rec *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.TenantID == "" {
		rec.TenantID = defaultTenantID
	}
	if strings.TrimSpace(rec.RecordNo) == "" {
		rec.RecordNo = businessRecordNo(rec.ID, rec.Project, rec.PointID, rec.PointName, rec.CreatedAt)
	}
	s.records[rec.ID] = rec
	return nil
}

func (s *MemStore) GetRecord(tenantID, id string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[id]
	if !ok || rec.TenantID != tenantID {
		return nil, sql.ErrNoRows // 跨租户等同不存在
	}
	return rec, nil
}

func (s *MemStore) UpdateRecord(rec *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenant := rec.TenantID
	if tenant == "" {
		tenant = defaultTenantID
		rec.TenantID = tenant
	}
	// 与 SQLite 侧一致:只允许命中同租户的行
	if cur, ok := s.records[rec.ID]; !ok || cur.TenantID != tenant {
		return sql.ErrNoRows
	}
	rec.UpdatedAt = time.Now()
	s.records[rec.ID] = rec
	return nil
}

func (s *MemStore) ListRecords(tenantID string, limit int) ([]*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Record, 0, len(s.records))
	for _, r := range s.records {
		if r.TenantID != tenantID {
			continue // 租户隔离
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemStore) DeleteDraftRecord(tenantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok || rec.TenantID != tenantID {
		return sql.ErrNoRows // 跨租户等同不存在,不泄露"这条 id 存在"
	}
	if rec.Submitted {
		return errRecordSubmitted
	}
	delete(s.records, id)
	for tid, t := range s.tasks {
		if t != nil && t.RecordID == id {
			delete(s.tasks, tid)
		}
	}
	// 照片当初是【复制】进记录目录的,原始离线照片还在 —— 放回待处理,
	// 不销毁。现场拍的东西不能因为草稿被删就没了。
	for _, shot := range s.offlineShots {
		if shot != nil && shot.RecordID == id {
			shot.RecordID = ""
			shot.Status = "uploaded"
		}
	}
	return nil
}

func (s *MemStore) ListRecordsByOwner(tenantID, inspectorUserID, displayName, username string, limit int) ([]*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Record, 0, len(s.records))
	for _, rec := range s.records {
		if rec.TenantID != tenantID {
			continue // 租户隔离先于归属判断
		}
		if recordOwnedBy(rec, inspectorUserID, displayName, username) {
			out = append(out, rec)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemStore) CreateTask(task *AITask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *MemStore) GetTask(id string) (*AITask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return t, nil
}

func (s *MemStore) UpdateTask(id string, mutate func(*AITask)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return sql.ErrNoRows
	}
	mutate(t)
	t.UpdatedAt = time.Now()
	return nil
}

func (s *MemStore) LatestTaskByRecord(recordID string) (*AITask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *AITask
	for _, t := range s.tasks {
		if t.RecordID != recordID {
			continue
		}
		if latest == nil || t.CreatedAt.After(latest.CreatedAt) {
			latest = t
		}
	}
	if latest == nil {
		return nil, sql.ErrNoRows
	}
	return latest, nil
}

func (s *MemStore) UpsertAsset(asset *AssetEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upsertAssetLocked(asset)
}

func (s *MemStore) upsertAssetLocked(asset *AssetEntry) error {
	now := time.Now()
	if asset.TenantID == "" {
		// 与 SQLiteStore.UpsertAsset 一致:未指定归默认租户(SQLite 侧靠列默认值)
		asset.TenantID = defaultTenantID
	}
	if asset.LastInspectedAt.IsZero() {
		asset.LastInspectedAt = now
	}
	if asset.ProjectCode == "" || asset.TemplateID == "" || asset.AssetKey == "" {
		asset.ProjectCode, asset.TemplateID, asset.AssetKey = deriveAssetDisplayKeys(asset)
	}
	if asset.StatusLevel == "" {
		asset.StatusLevel = statusLevel(asset.LastStatus)
	}
	if asset.StatusOrder == 0 {
		asset.StatusOrder = statusOrder(asset.LastStatus)
	}
	if existing, ok := s.assets[asset.ID]; ok {
		existing.ProjectCode = asset.ProjectCode
		existing.PointID = asset.PointID
		existing.TemplateID = asset.TemplateID
		existing.AssetType = asset.AssetType
		existing.AssetKey = asset.AssetKey
		existing.LastRecordID = asset.LastRecordID
		existing.LastStatus = asset.LastStatus
		existing.StatusLevel = asset.StatusLevel
		existing.StatusOrder = asset.StatusOrder
		existing.LastSummary = asset.LastSummary
		existing.LastInspectedAt = asset.LastInspectedAt
		existing.AssetName = asset.AssetName
		existing.LastInspector = asset.LastInspector
		existing.LastPhotoPath = asset.LastPhotoPath
		if asset.CoverImagePath != "" {
			existing.CoverImagePath = asset.CoverImagePath
		}
		existing.InspectionCount++
		existing.UpdatedAt = now
	} else {
		asset.CreatedAt = now
		asset.UpdatedAt = now
		asset.InspectionCount = 1
		s.assets[asset.ID] = asset
	}
	return nil
}

func (s *MemStore) SubmitRecordWithAssets(rec *Record, assets []*AssetEntry, snaps []*AssetSnapshot, obs []*FieldObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.records[rec.ID]; !ok {
		return sql.ErrNoRows
	}
	rec.UpdatedAt = time.Now()
	s.records[rec.ID] = rec
	for _, asset := range assets {
		if err := s.upsertAssetLocked(asset); err != nil {
			return err
		}
	}
	// 在同一锁内顺序写快照/观测,保持与 SQL 版本"全成功或全失败"的语义。
	for _, sn := range snaps {
		dup := false
		for _, ex := range s.assetSnapshots {
			if ex.AssetID == sn.AssetID && ex.RecordID == sn.RecordID {
				dup = true
				break
			}
		}
		if !dup {
			s.assetSnapshots = append(s.assetSnapshots, sn)
		}
	}
	for _, o := range obs {
		dup := false
		for _, ex := range s.fieldObs {
			if ex.AssetID == o.AssetID && ex.RecordID == o.RecordID && ex.FieldKey == o.FieldKey {
				dup = true
				break
			}
		}
		if !dup {
			s.fieldObs = append(s.fieldObs, o)
		}
	}
	return nil
}

func (s *MemStore) ListAssets(tenantID string) ([]*AssetEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*AssetEntry, 0, len(s.assets))
	for _, a := range s.assets {
		if a.TenantID != tenantID {
			continue // 租户隔离:只返回本租户资产
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *MemStore) GetAsset(tenantID, id string) (*AssetEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.assets[id]
	if !ok || a.TenantID != tenantID {
		return nil, sql.ErrNoRows // 跨租户等同不存在
	}
	return a, nil
}

// ===== 权限矩阵:MemStore 实现 =====

func (s *MemStore) ListRolePermissions() (map[string][]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string][]string{}
	for k, v := range s.rolePerms {
		out[k] = append([]string{}, v...)
	}
	return out, nil
}

func (s *MemStore) ReplaceRolePermissions(matrix map[string][]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rolePerms = map[string][]string{}
	for k, v := range matrix {
		s.rolePerms[k] = append([]string{}, v...)
	}
	return nil
}

func (s *MemStore) CreateAsset(asset *AssetEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.assets[asset.ID]; ok {
		return errors.New("asset already exists")
	}
	if asset.TenantID == "" {
		asset.TenantID = defaultTenantID // 与 SQLiteStore 一致:未指定归默认租户
	}
	now := time.Now()
	asset.CreatedAt = now
	asset.UpdatedAt = now
	asset.InspectionCount = 0
	s.assets[asset.ID] = asset
	return nil
}

func (s *MemStore) DeleteAsset(tenantID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.assets[id]; !ok || a.TenantID != tenantID {
		return sql.ErrNoRows // 跨租户等同不存在
	}
	delete(s.assets, id)
	snaps := s.assetSnapshots[:0]
	for _, sn := range s.assetSnapshots {
		if sn.AssetID != id {
			snaps = append(snaps, sn)
		}
	}
	s.assetSnapshots = snaps
	obs := s.fieldObs[:0]
	for _, o := range s.fieldObs {
		if o.AssetID != id {
			obs = append(obs, o)
		}
	}
	s.fieldObs = obs
	return nil
}

func (s *MemStore) UpdateAssetCover(tenantID, id, coverImagePath string) (*AssetEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok || a.TenantID != tenantID {
		return nil, sql.ErrNoRows // 跨租户等同不存在
	}
	a.CoverImagePath = coverImagePath
	a.UpdatedAt = time.Now()
	return a, nil
}

func (s *MemStore) UpdateAssetMeta(tenantID, id, name, status, summary string) (*AssetEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok || a.TenantID != tenantID {
		return nil, sql.ErrNoRows // 跨租户等同不存在
	}
	if name != "" {
		a.AssetName = name
	}
	if status != "" {
		a.LastStatus = status
		a.StatusLevel = statusLevel(status)
		a.StatusOrder = statusOrder(status)
	}
	if summary != "" {
		a.LastSummary = summary
	}
	a.UpdatedAt = time.Now()
	return a, nil
}

func (s *MemStore) Close() error { return nil }

func (s *MemStore) CreateChangeRequest(cr *ChangeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.changeRequests[cr.ID] = cr
	return nil
}

func (s *MemStore) GetChangeRequest(id string) (*ChangeRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cr, ok := s.changeRequests[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return cr, nil
}

func (s *MemStore) UpdateChangeRequest(cr *ChangeRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.changeRequests[cr.ID]; !ok {
		return sql.ErrNoRows
	}
	s.changeRequests[cr.ID] = cr
	return nil
}

func (s *MemStore) ListChangeRequests(filter ChangeRequestFilter) ([]*ChangeRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ChangeRequest, 0, len(s.changeRequests))
	for _, cr := range s.changeRequests {
		if filter.Status != "" && cr.Status != filter.Status {
			continue
		}
		if filter.RequestedBy != "" && cr.RequestedBy != filter.RequestedBy {
			continue
		}
		if filter.TargetType != "" && cr.TargetType != filter.TargetType {
			continue
		}
		out = append(out, cr)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].RequestedAt.After(out[j].RequestedAt)
	})
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ===== SQLiteStore（SQLite + MySQL 共用，dialect 字段区分 SQL 方言） =====

func (s *MemStore) ClaimSubmission(recordID, idemKey string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[recordID]
	if !ok {
		return "", sql.ErrNoRows
	}
	if existing, ok := s.submissions[recordID]; ok {
		if existing.Status != "submitted" && time.Since(existing.UpdatedAt) > submissionProcessingTTL {
			s.submissions[recordID] = submissionState{IdemKey: idemKey, Status: "processing", UpdatedAt: time.Now()}
			return submissionClaimed, nil
		}
		if existing.IdemKey == idemKey {
			if rec.Submitted {
				return submissionDuplicate, nil
			}
			return submissionInProgress, nil
		}
		return submissionBusy, nil
	}
	s.submissions[recordID] = submissionState{IdemKey: idemKey, Status: "processing", UpdatedAt: time.Now()}
	return submissionClaimed, nil
}

func (s *MemStore) CompleteSubmission(recordID, idemKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.submissions[recordID]; ok && existing.IdemKey == idemKey {
		existing.Status = "submitted"
		existing.UpdatedAt = time.Now()
		s.submissions[recordID] = existing
		return nil
	}
	return sql.ErrNoRows
}

func (s *MemStore) ReleaseSubmission(recordID, idemKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.submissions[recordID]; ok && existing.IdemKey == idemKey {
		if rec := s.records[recordID]; rec == nil || !rec.Submitted {
			delete(s.submissions, recordID)
		}
	}
	return nil
}

type SQLiteStore struct {
	db      *sql.DB
	dialect string // "sqlite" | "mysql"
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // sqlite 写串行
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	store := &SQLiteStore{db: db, dialect: "sqlite"}
	if err := store.runMigrations(); err != nil {
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return store, nil
}

// NewMySQLStore 用同一个 SQLiteStore 结构，driver 走 mysql。
func NewMySQLStore(dsn string) (*SQLiteStore, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping mysql: %w (检查 DSN / 服务是否启动 / database 是否存在)", err)
	}
	store := &SQLiteStore{db: db, dialect: "mysql"}
	if err := store.runMigrations(); err != nil {
		return nil, fmt.Errorf("migrate mysql: %w", err)
	}
	return store, nil
}

// ===== 模块化提示词:SQLiteStore(SQLite + MySQL)实现 =====

func (s *SQLiteStore) ensurePromptTemplateSchema() error {
	stmt := "CREATE TABLE IF NOT EXISTS prompt_templates (id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', data TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')"
	if s.dialect == "mysql" {
		stmt = "CREATE TABLE IF NOT EXISTS prompt_templates (id VARCHAR(64) PRIMARY KEY, name VARCHAR(255) NOT NULL DEFAULT '', data LONGTEXT NOT NULL, updated_at VARCHAR(40) NOT NULL DEFAULT '') ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("ensure prompt_templates: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListPromptTemplates() ([]PromptTemplate, error) {
	rows, err := s.db.Query("SELECT data FROM prompt_templates ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PromptTemplate{}
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var t PromptTemplate
		if err := json.Unmarshal([]byte(data), &t); err != nil {
			continue
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetPromptTemplate(id string) (PromptTemplate, bool, error) {
	var data string
	err := s.db.QueryRow("SELECT data FROM prompt_templates WHERE id = ?", id).Scan(&data)
	if err == sql.ErrNoRows {
		return PromptTemplate{}, false, nil
	}
	if err != nil {
		return PromptTemplate{}, false, err
	}
	var t PromptTemplate
	if err := json.Unmarshal([]byte(data), &t); err != nil {
		return PromptTemplate{}, false, err
	}
	return t, true, nil
}

func (s *SQLiteStore) UpsertPromptTemplate(t PromptTemplate) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	now := nowStamp()
	stmt := "INSERT INTO prompt_templates(id,name,data,updated_at) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, data=excluded.data, updated_at=excluded.updated_at"
	if s.dialect == "mysql" {
		stmt = "INSERT INTO prompt_templates(id,name,data,updated_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name), data=VALUES(data), updated_at=VALUES(updated_at)"
	}
	_, err = s.db.Exec(stmt, t.ID, t.Name, string(data), now)
	return err
}

type assetColumnMigration struct {
	name   string
	mysql  string
	sqlite string
}

func (s *SQLiteStore) ensureAssetDisplaySchema() error {
	columns := []assetColumnMigration{
		{"project_code", "ALTER TABLE assets ADD COLUMN project_code VARCHAR(64) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN project_code TEXT NOT NULL DEFAULT ''"},
		{"point_id", "ALTER TABLE assets ADD COLUMN point_id VARCHAR(64) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN point_id TEXT NOT NULL DEFAULT ''"},
		{"template_id", "ALTER TABLE assets ADD COLUMN template_id VARCHAR(64) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN template_id TEXT NOT NULL DEFAULT ''"},
		{"asset_key", "ALTER TABLE assets ADD COLUMN asset_key VARCHAR(128) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN asset_key TEXT NOT NULL DEFAULT ''"},
		{"status_level", "ALTER TABLE assets ADD COLUMN status_level VARCHAR(32) NOT NULL DEFAULT 'unknown'", "ALTER TABLE assets ADD COLUMN status_level TEXT NOT NULL DEFAULT 'unknown'"},
		{"status_order", "ALTER TABLE assets ADD COLUMN status_order INT NOT NULL DEFAULT 99", "ALTER TABLE assets ADD COLUMN status_order INTEGER NOT NULL DEFAULT 99"},
		{"last_inspector", "ALTER TABLE assets ADD COLUMN last_inspector VARCHAR(64) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN last_inspector TEXT NOT NULL DEFAULT ''"},
		{"last_photo_path", "ALTER TABLE assets ADD COLUMN last_photo_path VARCHAR(512) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN last_photo_path TEXT NOT NULL DEFAULT ''"},
		{"cover_image_path", "ALTER TABLE assets ADD COLUMN cover_image_path VARCHAR(512) NOT NULL DEFAULT ''", "ALTER TABLE assets ADD COLUMN cover_image_path TEXT NOT NULL DEFAULT ''"},
	}
	for _, col := range columns {
		exists, err := s.hasColumn("assets", col.name)
		if err != nil {
			return fmt.Errorf("inspect assets.%s: %w", col.name, err)
		}
		if exists {
			continue
		}
		stmt := col.sqlite
		if s.dialect == "mysql" {
			stmt = col.mysql
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("add assets.%s: %w", col.name, err)
		}
	}
	if err := s.ensureAssetDisplayIndexes(); err != nil {
		return err
	}
	return s.backfillAssetDisplayColumns()
}

func (s *SQLiteStore) ensureRecordOwnershipSchema() error {
	columns := []struct {
		name   string
		sqlite string
		mysql  string
	}{
		{
			name:   "inspector_user_id",
			sqlite: "ALTER TABLE records ADD COLUMN inspector_user_id TEXT NOT NULL DEFAULT ''",
			mysql:  "ALTER TABLE records ADD COLUMN inspector_user_id VARCHAR(64) NOT NULL DEFAULT ''",
		},
		{
			name:   "record_no",
			sqlite: "ALTER TABLE records ADD COLUMN record_no TEXT NOT NULL DEFAULT ''",
			mysql:  "ALTER TABLE records ADD COLUMN record_no VARCHAR(64) NOT NULL DEFAULT ''",
		},
		{
			name:   "engineering_task_id",
			sqlite: "ALTER TABLE records ADD COLUMN engineering_task_id TEXT NOT NULL DEFAULT ''",
			mysql:  "ALTER TABLE records ADD COLUMN engineering_task_id VARCHAR(64) NOT NULL DEFAULT ''",
		},
	}
	for _, col := range columns {
		exists, err := s.hasColumn("records", col.name)
		if err != nil {
			return fmt.Errorf("inspect records.%s: %w", col.name, err)
		}
		if !exists {
			stmt := col.sqlite
			if s.dialect == "mysql" {
				stmt = col.mysql
			}
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("add records.%s: %w", col.name, err)
			}
		}
	}
	const indexName = "idx_records_inspector_user_id"
	stmt := "CREATE INDEX IF NOT EXISTS " + indexName + " ON records(inspector_user_id, created_at)"
	if s.dialect == "mysql" {
		hasIndex, err := s.hasIndex("records", indexName)
		if err != nil {
			return fmt.Errorf("inspect index %s: %w", indexName, err)
		}
		if hasIndex {
			stmt = ""
		} else {
			stmt = "CREATE INDEX " + indexName + " ON records(inspector_user_id, created_at)"
		}
	}
	if stmt != "" {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create index %s: %w", indexName, err)
		}
	}
	recordNoIndex := "idx_records_record_no"
	recordNoStmt := "CREATE INDEX IF NOT EXISTS " + recordNoIndex + " ON records(record_no)"
	if s.dialect == "mysql" {
		hasIndex, err := s.hasIndex("records", recordNoIndex)
		if err != nil {
			return fmt.Errorf("inspect index %s: %w", recordNoIndex, err)
		}
		if hasIndex {
			recordNoStmt = ""
		} else {
			recordNoStmt = "CREATE INDEX " + recordNoIndex + " ON records(record_no)"
		}
	}
	if recordNoStmt != "" {
		if _, err := s.db.Exec(recordNoStmt); err != nil {
			return fmt.Errorf("create index %s: %w", recordNoIndex, err)
		}
	}
	return nil
}

func (s *SQLiteStore) hasColumn(table, column string) (bool, error) {
	if s.dialect == "mysql" {
		var n int
		err := s.db.QueryRow(`
			SELECT COUNT(*)
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE()
			  AND TABLE_NAME = ?
			  AND COLUMN_NAME = ?`, table, column).Scan(&n)
		return n > 0, err
	}
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLiteStore) ensureAssetDisplayIndexes() error {
	indexes := map[string]string{
		"idx_assets_status":         "CREATE INDEX idx_assets_status ON assets(last_status, status_level)",
		"idx_assets_project_status": "CREATE INDEX idx_assets_project_status ON assets(project, last_status)",
		"idx_assets_project_code":   "CREATE INDEX idx_assets_project_code ON assets(project_code, status_order)",
		"idx_assets_asset_key":      "CREATE INDEX idx_assets_asset_key ON assets(asset_key)",
		"idx_assets_updated_at":     "CREATE INDEX idx_assets_updated_at ON assets(updated_at)",
	}
	for name, stmt := range indexes {
		if s.dialect == "sqlite" {
			stmt = strings.Replace(stmt, "CREATE INDEX ", "CREATE INDEX IF NOT EXISTS ", 1)
		}
		if s.dialect == "mysql" {
			exists, err := s.hasIndex("assets", name)
			if err != nil {
				return fmt.Errorf("inspect index %s: %w", name, err)
			}
			if exists {
				continue
			}
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("create index %s: %w", name, err)
		}
	}
	return nil
}

func (s *SQLiteStore) hasIndex(table, indexName string) (bool, error) {
	if s.dialect != "mysql" {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = ?
		  AND INDEX_NAME = ?`, table, indexName).Scan(&n)
	return n > 0, err
}

func (s *SQLiteStore) backfillAssetDisplayColumns() error {
	// 历史列回填发生在 migration 002 期,此刻 tenant_id 列尚不存在,
	// 故只取本函数派生所需的列、不经 scanAsset(避免引用后续迁移才加的 tenant_id)。
	rows, err := s.db.Query(`SELECT id, project, template_id, asset_key, asset_name, last_status FROM assets`)
	if err != nil {
		return err
	}
	var assets []*AssetEntry
	for rows.Next() {
		a := &AssetEntry{}
		if scanErr := rows.Scan(&a.ID, &a.Project, &a.TemplateID, &a.AssetKey, &a.AssetName, &a.LastStatus); scanErr != nil {
			rows.Close()
			return scanErr
		}
		assets = append(assets, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, a := range assets {
		projectCode, templateID, assetKey := deriveAssetDisplayKeys(a)
		level := statusLevel(a.LastStatus)
		order := statusOrder(a.LastStatus)
		if _, err := s.db.Exec(`
			UPDATE assets SET
				project_code = CASE WHEN project_code='' THEN ? ELSE project_code END,
				template_id  = CASE WHEN template_id=''  THEN ? ELSE template_id  END,
				asset_key    = CASE WHEN asset_key=''    THEN ? ELSE asset_key    END,
				status_level = ?,
				status_order = ?
			WHERE id = ?`,
			projectCode, templateID, assetKey, level, order, a.ID,
		); err != nil {
			return fmt.Errorf("backfill asset %s: %w", a.ID, err)
		}
	}
	return nil
}

func sortRoles(rs []*Role) {
	sort.Slice(rs, func(i, j int) bool { return rs[i].ID < rs[j].ID })
}

// splitSQLStatements 按 ; 切分，忽略行内注释 -- ...
func splitSQLStatements(sqlText string) []string {
	var clean strings.Builder
	for _, line := range strings.Split(sqlText, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "--") {
			continue
		}
		clean.WriteString(line)
		clean.WriteString("\n")
	}
	return strings.Split(clean.String(), ";")
}

func truncateStmt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) ClaimSubmission(recordID, idemKey string) (string, error) {
	now := nowStamp()
	_, err := s.db.Exec(`
		INSERT INTO submission_idempotency (record_id, idem_key, status, created_at, updated_at)
		VALUES (?, ?, 'processing', ?, ?)`, recordID, idemKey, now, now)
	if err == nil {
		return submissionClaimed, nil
	}
	var existingKey, status, updatedAt string
	if scanErr := s.db.QueryRow(
		`SELECT idem_key, status, updated_at FROM submission_idempotency WHERE record_id=?`,
		recordID,
	).Scan(&existingKey, &status, &updatedAt); scanErr != nil {
		return "", err
	}
	if status != "submitted" && submissionStateStale(updatedAt) {
		res, updateErr := s.db.Exec(`
			UPDATE submission_idempotency
			SET idem_key=?, status='processing', updated_at=?
			WHERE record_id=? AND status<>'submitted' AND updated_at=?`,
			idemKey, now, recordID, updatedAt,
		)
		if updateErr != nil {
			return "", updateErr
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return submissionClaimed, nil
		}
	}
	if existingKey == idemKey {
		if status == "submitted" {
			return submissionDuplicate, nil
		}
		return submissionInProgress, nil
	}
	return submissionBusy, nil
}

func submissionStateStale(updatedAt string) bool {
	t, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return false
	}
	return time.Since(t) > submissionProcessingTTL
}

func (s *SQLiteStore) CompleteSubmission(recordID, idemKey string) error {
	res, err := s.db.Exec(`
		UPDATE submission_idempotency
		SET status='submitted', updated_at=?
		WHERE record_id=? AND idem_key=?`,
		nowStamp(), recordID, idemKey,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) ReleaseSubmission(recordID, idemKey string) error {
	_, err := s.db.Exec(`
		DELETE FROM submission_idempotency
		WHERE record_id=? AND idem_key=? AND status<>'submitted'`,
		recordID, idemKey,
	)
	return err
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
}

type sqlQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (s *SQLiteStore) CreateRecord(rec *Record) error {
	if rec.TenantID == "" {
		rec.TenantID = defaultTenantID // 未指定则归默认租户(单租户过渡期)
	}
	fieldsJSON, _ := json.Marshal(rec.Fields)
	imagesJSON, _ := json.Marshal(rec.Images)
	tagsJSON, _ := json.Marshal(rec.AISummaryTags)
	recosJSON, _ := json.Marshal(rec.AIRecommendations)
	now := nowStamp()
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO records (
			id, record_no, project, point_id, point_name, template_id, template_name,
			type, inspector, inspector_user_id, capture_attempts, manual_required,
			recognition_status, retake_reason, task_id, engineering_task_id,
			fields_json, images_json, report,
			ai_summary, ai_summary_tags, ai_recommendations, ai_summary_error,
			submitted, submitted_at, created_at, updated_at, tenant_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.RecordNo, rec.Project, rec.PointID, rec.PointName, rec.TemplateID, rec.TemplateName,
		rec.Type, rec.Inspector, rec.InspectorUserID, rec.CaptureAttempts, boolToInt(rec.ManualRequired),
		rec.RecognitionStatus, rec.RetakeReason, rec.TaskID, rec.EngineeringTaskID,
		string(fieldsJSON), string(imagesJSON), rec.Report,
		rec.AISummary, string(tagsJSON), string(recosJSON), rec.AISummaryError,
		boolToInt(rec.Submitted), nullableTime(rec.SubmittedAt),
		fmtStamp(rec.CreatedAt), now, rec.TenantID,
	)
	return err
}

func (s *SQLiteStore) UpdateRecord(rec *Record) error {
	return updateRecordExec(s.db, rec)
}

func updateRecordExec(exec sqlExecutor, rec *Record) error {
	// 租户随记录自身携带:更新只允许命中同租户的行(跨租户影响 0 行)。
	// 记录来自 GetRecord 时天然带对租户;历史/内部构造未带则回落默认租户。
	tenant := rec.TenantID
	if tenant == "" {
		tenant = defaultTenantID
	}
	fieldsJSON, _ := json.Marshal(rec.Fields)
	imagesJSON, _ := json.Marshal(rec.Images)
	tagsJSON, _ := json.Marshal(rec.AISummaryTags)
	recosJSON, _ := json.Marshal(rec.AIRecommendations)
	updatedAt := time.Now()
	rec.UpdatedAt = updatedAt
	_, err := exec.Exec(`
		UPDATE records SET
			record_no=?, project=?, point_id=?, point_name=?, template_id=?, template_name=?,
			type=?, inspector=?, inspector_user_id=?, capture_attempts=?, manual_required=?,
			recognition_status=?, retake_reason=?, task_id=?, engineering_task_id=?,
			fields_json=?, images_json=?, report=?,
			ai_summary=?, ai_summary_tags=?, ai_recommendations=?, ai_summary_error=?,
			submitted=?, submitted_at=?, updated_at=?
		WHERE id=? AND tenant_id=?`,
		rec.RecordNo, rec.Project, rec.PointID, rec.PointName, rec.TemplateID, rec.TemplateName,
		rec.Type, rec.Inspector, rec.InspectorUserID, rec.CaptureAttempts, boolToInt(rec.ManualRequired),
		rec.RecognitionStatus, rec.RetakeReason, rec.TaskID, rec.EngineeringTaskID,
		string(fieldsJSON), string(imagesJSON), rec.Report,
		rec.AISummary, string(tagsJSON), string(recosJSON), rec.AISummaryError,
		boolToInt(rec.Submitted), nullableTime(rec.SubmittedAt), fmtStamp(updatedAt),
		rec.ID, tenant,
	)
	return err
}

// recordSelectCols —— 记录查询列清单单一真源(原本 3 处 SELECT 各抄一遍,易漂移)。
// 顺序必须与 scanRecord 的 Scan 目标严格一致。
const recordSelectCols = `id, record_no, project, point_id, point_name, template_id, template_name,
	type, inspector, inspector_user_id, capture_attempts, manual_required,
	recognition_status, retake_reason, task_id, engineering_task_id,
	fields_json, images_json, report,
	ai_summary, ai_summary_tags, ai_recommendations, ai_summary_error,
	submitted, submitted_at, created_at, updated_at, tenant_id`

func (s *SQLiteStore) GetRecord(tenantID, id string) (*Record, error) {
	return getRecordExec(s.db, tenantID, id)
}

func getRecordExec(queryer sqlQueryer, tenantID, id string) (*Record, error) {
	row := queryer.QueryRow(
		`SELECT `+recordSelectCols+` FROM records WHERE id=? AND tenant_id=?`, id, tenantID)
	return scanRecord(row)
}

// getRecordByID —— 不带租户过滤的按 id 读取,仅供内部「写后读回」/跨租户维护。
// 对外读取一律走 GetRecord(tenantID, id)。
func getRecordByID(queryer sqlQueryer, id string) (*Record, error) {
	row := queryer.QueryRow(`SELECT `+recordSelectCols+` FROM records WHERE id=?`, id)
	return scanRecord(row)
}

func (s *SQLiteStore) ListRecords(tenantID string, limit int) ([]*Record, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT `+recordSelectCols+` FROM records WHERE tenant_id=? ORDER BY created_at DESC LIMIT ?`,
		tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// errRecordSubmitted 已提交的记录不允许删除。它是台账证据,而且已经写进了
// 资产快照和字段观测 —— 删掉会让台账对不上账。要撤销请走审批流。
var errRecordSubmitted = errors.New("record already submitted")

func (s *SQLiteStore) DeleteDraftRecord(tenantID, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var submitted int
	// 先在事务里锁定并确认状态,避免"查完到删之间被提交"的竞态 ——
	// 那会删掉一条已经进了台账的记录。
	row := tx.QueryRow(`SELECT submitted FROM records WHERE id=? AND tenant_id=?`, id, tenantID)
	if err := row.Scan(&submitted); err != nil {
		return err // 包含 sql.ErrNoRows:不存在或跨租户
	}
	if submitted != 0 {
		return errRecordSubmitted
	}

	if _, err := tx.Exec(`DELETE FROM records WHERE id=? AND tenant_id=?`, id, tenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ai_tasks WHERE record_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM submission_idempotency WHERE record_id=?`, id); err != nil {
		return err
	}
	// 照片当初是【复制】进记录目录的,原始离线照片还在 —— 放回待处理,不销毁。
	// 现场拍的东西不能因为草稿被删就没了。
	if _, err := tx.Exec(
		`UPDATE offline_shots SET record_id='', status='uploaded' WHERE record_id=? AND tenant_id=?`,
		id, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListRecordsByOwner(tenantID, inspectorUserID, displayName, username string, limit int) ([]*Record, error) {
	if limit <= 0 {
		limit = 100
	}
	// 租户过滤置于最外层 AND,与归属条件是「且」关系,不被 OR 短路
	rows, err := s.db.Query(
		`SELECT `+recordSelectCols+` FROM records
		WHERE tenant_id = ?
		  AND ( (? <> '' AND inspector_user_id = ?)
		     OR (inspector_user_id = '' AND inspector IN (?, ?)) )
		ORDER BY created_at DESC LIMIT ?`,
		tenantID, inspectorUserID, inspectorUserID,
		strings.TrimSpace(displayName), strings.TrimSpace(username), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(...any) error
}

func scanRecord(row scanner) (*Record, error) {
	rec := &Record{}
	var fieldsJSON, imagesJSON, tagsJSON, recosJSON string
	var submittedAt sql.NullString
	var createdStr, updatedStr string
	var manualInt, submittedInt int
	err := row.Scan(
		&rec.ID, &rec.RecordNo, &rec.Project, &rec.PointID, &rec.PointName, &rec.TemplateID, &rec.TemplateName,
		&rec.Type, &rec.Inspector, &rec.InspectorUserID, &rec.CaptureAttempts, &manualInt,
		&rec.RecognitionStatus, &rec.RetakeReason, &rec.TaskID, &rec.EngineeringTaskID,
		&fieldsJSON, &imagesJSON, &rec.Report,
		&rec.AISummary, &tagsJSON, &recosJSON, &rec.AISummaryError,
		&submittedInt, &submittedAt, &createdStr, &updatedStr, &rec.TenantID,
	)
	if err != nil {
		return nil, err
	}
	rec.ManualRequired = manualInt != 0
	rec.Submitted = submittedInt != 0
	_ = json.Unmarshal([]byte(fieldsJSON), &rec.Fields)
	_ = json.Unmarshal([]byte(imagesJSON), &rec.Images)
	_ = json.Unmarshal([]byte(tagsJSON), &rec.AISummaryTags)
	_ = json.Unmarshal([]byte(recosJSON), &rec.AIRecommendations)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	if strings.TrimSpace(rec.RecordNo) == "" {
		rec.RecordNo = businessRecordNo(rec.ID, rec.Project, rec.PointID, rec.PointName, rec.CreatedAt)
	}
	if submittedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, submittedAt.String)
		rec.SubmittedAt = &t
	}
	if rec.Fields == nil {
		rec.Fields = []FieldValue{}
	}
	if rec.Images == nil {
		rec.Images = []ImageInfo{}
	}
	if rec.AISummaryTags == nil {
		rec.AISummaryTags = []string{}
	}
	if rec.AIRecommendations == nil {
		rec.AIRecommendations = []Recommendation{}
	}
	return rec, nil
}

func (s *SQLiteStore) CreateTask(task *AITask) error {
	analysisJSON, _ := json.Marshal(task.Analysis)
	now := nowStamp()
	_, err := s.db.Exec(`
		INSERT INTO ai_tasks (id, record_id, status, progress_done, progress_total,
		                     error_code, error_message, analysis_json,
		                     created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.RecordID, task.Status, task.Progress.Processed, task.Progress.Total,
		task.ErrorCode, task.ErrorMessage, string(analysisJSON),
		fmtStamp(task.CreatedAt), now,
	)
	return err
}

func (s *SQLiteStore) GetTask(id string) (*AITask, error) {
	row := s.db.QueryRow(`
		SELECT id, record_id, status, progress_done, progress_total,
		       error_code, error_message, analysis_json, created_at, updated_at
		FROM ai_tasks WHERE id=?`, id)
	return scanTask(row)
}

func (s *SQLiteStore) UpdateTask(id string, mutate func(*AITask)) error {
	task, err := s.GetTask(id)
	if err != nil {
		return err
	}
	mutate(task)
	analysisJSON, _ := json.Marshal(task.Analysis)
	_, err = s.db.Exec(`
		UPDATE ai_tasks SET status=?, progress_done=?, progress_total=?,
		                   error_code=?, error_message=?, analysis_json=?, updated_at=?
		WHERE id=?`,
		task.Status, task.Progress.Processed, task.Progress.Total,
		task.ErrorCode, task.ErrorMessage, string(analysisJSON),
		nowStamp(), id,
	)
	return err
}

func (s *SQLiteStore) LatestTaskByRecord(recordID string) (*AITask, error) {
	row := s.db.QueryRow(`
		SELECT id, record_id, status, progress_done, progress_total,
		       error_code, error_message, analysis_json, created_at, updated_at
		FROM ai_tasks WHERE record_id=? ORDER BY created_at DESC LIMIT 1`, recordID)
	return scanTask(row)
}

func scanTask(row scanner) (*AITask, error) {
	t := &AITask{}
	var analysisJSON string
	var createdStr, updatedStr string
	err := row.Scan(
		&t.ID, &t.RecordID, &t.Status, &t.Progress.Processed, &t.Progress.Total,
		&t.ErrorCode, &t.ErrorMessage, &analysisJSON, &createdStr, &updatedStr,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(analysisJSON), &t.Analysis)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	return t, nil
}

func (s *SQLiteStore) UpsertAsset(asset *AssetEntry) error {
	// TODO 多租户:tenant_id 暂由列默认值('tenant_default')落定,故 upsert 出的资产
	// 恒归默认租户。提交流(SubmitRecordWithAssets)携带真实租户时,需把 asset.TenantID
	// 显式写进本 INSERT 并在 ON CONFLICT 中保持不变。单客户过渡期行为正确。
	return upsertAssetExec(s.db, s.dialect, asset)
}

func upsertAssetExec(exec sqlExecutor, dialect string, asset *AssetEntry) error {
	if asset.LastInspectedAt.IsZero() {
		asset.LastInspectedAt = time.Now()
	}
	if asset.ProjectCode == "" || asset.TemplateID == "" || asset.AssetKey == "" {
		asset.ProjectCode, asset.TemplateID, asset.AssetKey = deriveAssetDisplayKeys(asset)
	}
	if asset.StatusLevel == "" {
		asset.StatusLevel = statusLevel(asset.LastStatus)
	}
	if asset.StatusOrder == 0 {
		asset.StatusOrder = statusOrder(asset.LastStatus)
	}
	now := nowStamp()
	lastInspected := fmtStamp(asset.LastInspectedAt)
	var query string
	if dialect == "mysql" {
		query = `
			INSERT INTO assets (id, project_code, project, point_id, template_id,
			                    asset_type, asset_key, asset_name, last_record_id,
			                    last_status, status_level, status_order, last_summary,
			                    last_inspected_at, last_inspector, last_photo_path,
			                    cover_image_path, inspection_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON DUPLICATE KEY UPDATE
				project_code     = VALUES(project_code),
				project          = VALUES(project),
				point_id         = VALUES(point_id),
				template_id      = VALUES(template_id),
				asset_type       = VALUES(asset_type),
				asset_key        = VALUES(asset_key),
				asset_name       = VALUES(asset_name),
				last_record_id    = VALUES(last_record_id),
				last_status       = VALUES(last_status),
				status_level      = VALUES(status_level),
				status_order      = VALUES(status_order),
				last_summary      = VALUES(last_summary),
				last_inspected_at = VALUES(last_inspected_at),
				last_inspector    = VALUES(last_inspector),
				last_photo_path   = VALUES(last_photo_path),
				cover_image_path  = CASE WHEN VALUES(cover_image_path) <> '' THEN VALUES(cover_image_path) ELSE cover_image_path END,
				inspection_count  = inspection_count + 1,
				updated_at        = VALUES(updated_at)`
	} else {
		query = `
			INSERT INTO assets (id, project_code, project, point_id, template_id,
			                    asset_type, asset_key, asset_name, last_record_id,
			                    last_status, status_level, status_order, last_summary,
			                    last_inspected_at, last_inspector, last_photo_path,
			                    cover_image_path, inspection_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				project_code     = excluded.project_code,
				project          = excluded.project,
				point_id         = excluded.point_id,
				template_id      = excluded.template_id,
				asset_type       = excluded.asset_type,
				asset_key        = excluded.asset_key,
				asset_name       = excluded.asset_name,
				last_record_id    = excluded.last_record_id,
				last_status       = excluded.last_status,
				status_level      = excluded.status_level,
				status_order      = excluded.status_order,
				last_summary      = excluded.last_summary,
				last_inspected_at = excluded.last_inspected_at,
				last_inspector    = excluded.last_inspector,
				last_photo_path   = excluded.last_photo_path,
				cover_image_path  = CASE WHEN excluded.cover_image_path <> '' THEN excluded.cover_image_path ELSE cover_image_path END,
				inspection_count  = inspection_count + 1,
				updated_at        = excluded.updated_at`
	}
	_, err := exec.Exec(query,
		asset.ID, asset.ProjectCode, asset.Project, asset.PointID, asset.TemplateID,
		asset.AssetType, asset.AssetKey, asset.AssetName, asset.LastRecordID,
		asset.LastStatus, asset.StatusLevel, asset.StatusOrder, asset.LastSummary,
		lastInspected, asset.LastInspector, asset.LastPhotoPath, asset.CoverImagePath, now, now,
	)
	return err
}

func (s *SQLiteStore) SubmitRecordWithAssets(rec *Record, assets []*AssetEntry, snaps []*AssetSnapshot, obs []*FieldObservation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := updateRecordExec(tx, rec); err != nil {
		return err
	}
	for _, asset := range assets {
		if err := upsertAssetExec(tx, s.dialect, asset); err != nil {
			return err
		}
	}
	// 快照/观测在同事务里,失败一起回滚,确保「日报已提交但快照漏写」不会发生。
	if err := writeAssetSnapshotsExec(tx, s.dialect, snaps, obs); err != nil {
		return err
	}
	return tx.Commit()
}

// assetSelectCols —— 资产查询列清单单一真源,三处 SELECT 共用,防列序漂移。
// 顺序必须与 scanAsset 的 Scan 目标严格一致。
const assetSelectCols = `id, project_code, project, point_id, template_id,
	asset_type, asset_key, asset_name, last_record_id,
	last_status, status_level, status_order, last_summary,
	last_inspected_at, last_inspector, last_photo_path,
	cover_image_path, inspection_count, created_at, updated_at, tenant_id`

func (s *SQLiteStore) ListAssets(tenantID string) ([]*AssetEntry, error) {
	rows, err := s.db.Query(
		`SELECT `+assetSelectCols+` FROM assets WHERE tenant_id=? ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AssetEntry
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetAsset(tenantID, id string) (*AssetEntry, error) {
	return getAssetExec(s.db, tenantID, id)
}

func getAssetExec(queryer sqlQueryer, tenantID, id string) (*AssetEntry, error) {
	row := queryer.QueryRow(
		`SELECT `+assetSelectCols+` FROM assets WHERE id=? AND tenant_id=?`, id, tenantID)
	return scanAsset(row)
}

// getAssetByID —— 不带租户过滤的按 id 读取,仅供内部「写后读回」(update 后返回最新行)
// 和跨租户维护任务用。对外读取一律走 GetAsset(tenantID, id)。
func getAssetByID(queryer sqlQueryer, id string) (*AssetEntry, error) {
	row := queryer.QueryRow(`SELECT `+assetSelectCols+` FROM assets WHERE id=?`, id)
	return scanAsset(row)
}

// UpdateAssetMeta 仅允许编辑 assetName / lastStatus / lastSummary。
// 空字符串视为不改动该字段（partial update 语义）。
func (s *SQLiteStore) UpdateAssetMeta(tenantID, id, name, status, summary string) (*AssetEntry, error) {
	if err := updateAssetMetaExec(s.db, tenantID, id, name, status, summary); err != nil {
		return nil, err
	}
	return getAssetByID(s.db, id) // 写后读回:租户已在上面的 UPDATE 中校验过
}

// ===== 权限矩阵:SQLiteStore(SQLite + MySQL)实现 =====

func (s *SQLiteStore) ListRolePermissions() (map[string][]string, error) {
	rows, err := s.db.Query(`SELECT perm_key, role_code FROM role_permissions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var k, role string
		if err := rows.Scan(&k, &role); err != nil {
			return nil, err
		}
		out[k] = append(out[k], role)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ReplaceRolePermissions(matrix map[string][]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM role_permissions`); err != nil {
		return err
	}
	for k, roles := range matrix {
		for _, role := range roles {
			if _, err := tx.Exec(`INSERT INTO role_permissions (perm_key, role_code) VALUES (?, ?)`, k, role); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// CreateAsset 手工建档:纯 INSERT,重复主键即报错(与巡检驱动的 UpsertAsset 区分,巡检数从 0 起)。
func (s *SQLiteStore) CreateAsset(asset *AssetEntry) error {
	if asset.TenantID == "" {
		asset.TenantID = defaultTenantID // 未指定则归默认租户(单租户过渡期)
	}
	now := nowStamp()
	_, err := s.db.Exec(`
		INSERT INTO assets (id, project_code, project, point_id, template_id,
		                    asset_type, asset_key, asset_name, last_record_id,
		                    last_status, status_level, status_order, last_summary,
		                    last_inspected_at, last_inspector, last_photo_path,
		                    cover_image_path, inspection_count, created_at, updated_at, tenant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, NULL, '', '', '', 0, ?, ?, ?)`,
		asset.ID, asset.ProjectCode, asset.Project, asset.PointID, asset.TemplateID,
		asset.AssetType, asset.AssetKey, asset.AssetName,
		asset.LastStatus, asset.StatusLevel, asset.StatusOrder, asset.LastSummary,
		now, now, asset.TenantID,
	)
	if err != nil && (strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE")) {
		return errors.New("asset already exists")
	}
	return err
}

// DeleteAsset 事务删除资产 + 快照 + 字段观测;巡检记录保留(历史证据不随资产消失)。
func (s *SQLiteStore) DeleteAsset(tenantID, id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 跨租户删除影响 0 行 → ErrNoRows → 上层 404,等同不存在
	res, err := tx.Exec(`DELETE FROM assets WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM asset_snapshots WHERE asset_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM field_observations WHERE asset_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateAssetCover 仅更新封面图路径 cover_image_path，其余字段不动。
func (s *SQLiteStore) UpdateAssetCover(tenantID, id, coverImagePath string) (*AssetEntry, error) {
	if err := updateAssetCoverExec(s.db, tenantID, id, coverImagePath); err != nil {
		return nil, err
	}
	return getAssetByID(s.db, id) // 写后读回:租户已在上面的 UPDATE 中校验过
}

func updateAssetCoverExec(exec sqlExecutor, tenantID, id, coverImagePath string) error {
	now := nowStamp()
	res, err := exec.Exec(`
		UPDATE assets SET cover_image_path = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`, coverImagePath, now, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func updateAssetMetaExec(exec sqlExecutor, tenantID, id, name, status, summary string) error {
	now := nowStamp()
	res, err := exec.Exec(`
		UPDATE assets SET
			asset_name   = CASE WHEN ?='' THEN asset_name   ELSE ? END,
			last_status  = CASE WHEN ?='' THEN last_status  ELSE ? END,
			status_level = CASE WHEN ?='' THEN status_level ELSE ? END,
			status_order = CASE WHEN ?='' THEN status_order ELSE ? END,
			last_summary = CASE WHEN ?='' THEN last_summary ELSE ? END,
			updated_at   = ?
		WHERE id = ? AND tenant_id = ?`,
		name, name,
		status, status,
		status, statusLevel(status),
		status, statusOrder(status),
		summary, summary,
		now, id, tenantID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanAsset(row scanner) (*AssetEntry, error) {
	a := &AssetEntry{}
	var lastRecordID sql.NullString
	var lastInspectedStr sql.NullString
	var createdStr, updatedStr string
	err := row.Scan(
		&a.ID, &a.ProjectCode, &a.Project, &a.PointID, &a.TemplateID,
		&a.AssetType, &a.AssetKey, &a.AssetName, &lastRecordID,
		&a.LastStatus, &a.StatusLevel, &a.StatusOrder, &a.LastSummary,
		&lastInspectedStr, &a.LastInspector, &a.LastPhotoPath, &a.CoverImagePath,
		&a.InspectionCount, &createdStr, &updatedStr, &a.TenantID,
	)
	if err != nil {
		return nil, err
	}
	if lastRecordID.Valid {
		a.LastRecordID = lastRecordID.String
	}
	if lastInspectedStr.Valid {
		a.LastInspectedAt, _ = time.Parse(time.RFC3339Nano, lastInspectedStr.String)
	}
	a.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedStr)
	if a.ProjectCode == "" || a.TemplateID == "" || a.AssetKey == "" {
		a.ProjectCode, a.TemplateID, a.AssetKey = deriveAssetDisplayKeys(a)
	}
	if a.StatusLevel == "" {
		a.StatusLevel = statusLevel(a.LastStatus)
	}
	if a.StatusOrder == 0 {
		a.StatusOrder = statusOrder(a.LastStatus)
	}
	return a, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return fmtStamp(*t)
}

// ===== ChangeRequest (SQLite + MySQL，同一份 SQL，方言差异极小) =====

func (s *SQLiteStore) CreateChangeRequest(cr *ChangeRequest) error {
	patchJSON, _ := json.Marshal(cr.Patch)
	_, err := s.db.Exec(`
		INSERT INTO change_requests (
			id, target_type, target_id, patch_json, reason,
			status, requested_by, requested_at,
			reviewed_by, reviewed_at, review_note, applied_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		cr.ID, cr.TargetType, cr.TargetID, string(patchJSON), cr.Reason,
		cr.Status, cr.RequestedBy, fmtStamp(cr.RequestedAt),
		nullableString(cr.ReviewedBy), nullableTime(cr.ReviewedAt), cr.ReviewNote, nullableTime(cr.AppliedAt),
	)
	return err
}

func (s *SQLiteStore) GetChangeRequest(id string) (*ChangeRequest, error) {
	return getChangeRequestExec(s.db, id)
}

func getChangeRequestExec(queryer sqlQueryer, id string) (*ChangeRequest, error) {
	row := queryer.QueryRow(`
		SELECT id, target_type, target_id, patch_json, reason,
		       status, requested_by, requested_at,
		       reviewed_by, reviewed_at, review_note, applied_at
		FROM change_requests WHERE id=?`, id)
	return scanChangeRequest(row)
}

func (s *SQLiteStore) UpdateChangeRequest(cr *ChangeRequest) error {
	return updateChangeRequestExec(s.db, cr)
}

func updateChangeRequestExec(exec sqlExecutor, cr *ChangeRequest) error {
	patchJSON, _ := json.Marshal(cr.Patch)
	res, err := exec.Exec(`
		UPDATE change_requests SET
			target_type=?, target_id=?, patch_json=?, reason=?,
			status=?, requested_by=?, requested_at=?,
			reviewed_by=?, reviewed_at=?, review_note=?, applied_at=?
		WHERE id=?`,
		cr.TargetType, cr.TargetID, string(patchJSON), cr.Reason,
		cr.Status, cr.RequestedBy, fmtStamp(cr.RequestedAt),
		nullableString(cr.ReviewedBy), nullableTime(cr.ReviewedAt), cr.ReviewNote, nullableTime(cr.AppliedAt),
		cr.ID,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) ListChangeRequests(filter ChangeRequestFilter) ([]*ChangeRequest, error) {
	q := `SELECT id, target_type, target_id, patch_json, reason,
		       status, requested_by, requested_at,
		       reviewed_by, reviewed_at, review_note, applied_at
		FROM change_requests WHERE 1=1`
	args := []any{}
	if filter.Status != "" {
		q += " AND status=?"
		args = append(args, filter.Status)
	}
	if filter.RequestedBy != "" {
		q += " AND requested_by=?"
		args = append(args, filter.RequestedBy)
	}
	if filter.TargetType != "" {
		q += " AND target_type=?"
		args = append(args, filter.TargetType)
	}
	q += " ORDER BY requested_at DESC"
	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	q += fmt.Sprintf(" LIMIT %d", limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ChangeRequest
	for rows.Next() {
		cr, err := scanChangeRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cr)
	}
	return out, rows.Err()
}

func scanChangeRequest(row scanner) (*ChangeRequest, error) {
	cr := &ChangeRequest{}
	var patchJSON, reviewedBy, reviewNote sql.NullString
	var requestedStr string
	var reviewedStr, appliedStr sql.NullString
	err := row.Scan(
		&cr.ID, &cr.TargetType, &cr.TargetID, &patchJSON, &cr.Reason,
		&cr.Status, &cr.RequestedBy, &requestedStr,
		&reviewedBy, &reviewedStr, &reviewNote, &appliedStr,
	)
	if err != nil {
		return nil, err
	}
	if patchJSON.Valid && patchJSON.String != "" {
		_ = json.Unmarshal([]byte(patchJSON.String), &cr.Patch)
	}
	if cr.Patch == nil {
		cr.Patch = map[string]any{}
	}
	if reviewedBy.Valid {
		cr.ReviewedBy = reviewedBy.String
	}
	if reviewNote.Valid {
		cr.ReviewNote = reviewNote.String
	}
	cr.RequestedAt, _ = time.Parse(time.RFC3339Nano, requestedStr)
	if reviewedStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, reviewedStr.String)
		cr.ReviewedAt = &t
	}
	if appliedStr.Valid {
		t, _ := time.Parse(time.RFC3339Nano, appliedStr.String)
		cr.AppliedAt = &t
	}
	return cr, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
