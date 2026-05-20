package main

import "time"

// Point — 巡检点位（静态种子数据）
type Point struct {
	ID         string `json:"id"`
	Project    string `json:"project"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Location   string `json:"location"`
	TemplateID string `json:"templateId"`
	Featured   bool   `json:"featured"` // 首页是否突出展示
}

// ReportTemplate — 日报模板定义
type ReportTemplate struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Project   string          `json:"project"`
	AssetType string          `json:"assetType"`
	MaxImages int             `json:"maxImages"` // 单次上传图片上限（能耗抄表 6，其他 3）
	Featured  bool            `json:"featured"`
	Fields    []TemplateField `json:"fields"`
	HasAI     bool            `json:"hasAI"`              // 是否接入了 AI prompt（决定是否调 ai-service）
	AIPrompt  string          `json:"aiPrompt,omitempty"` // 对应的 prompt 文件名（无后缀）
}

// TemplateField — 字段定义（模板里写死的）
type TemplateField struct {
	Code     string   `json:"code"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"` // text / number / choice
	Required bool     `json:"required"`
	Source   string   `json:"source"` // ai / manual
	Options  []string `json:"options,omitempty"`
}

// FieldValue — 字段实例（每条记录里的一项）
type FieldValue struct {
	Code        string   `json:"code"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Required    bool     `json:"required"`
	Value       string   `json:"value"`
	AIValue     string   `json:"aiValue"`
	Source      string   `json:"source"` // manual / ai / human-confirmed / human-edited
	Options     []string `json:"options,omitempty"`
	Confidence  float64  `json:"confidence"`
	NeedsReview bool     `json:"needsReview"`
	Reason      string   `json:"reason,omitempty"`
	Version     int      `json:"version"`
}

// ImageInfo — 上传图片元数据
type ImageInfo struct {
	ID          string    `json:"id"`
	FileName    string    `json:"fileName"`
	Path        string    `json:"path"`
	ContentHash string    `json:"contentHash"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Record — 巡检记录
type Record struct {
	ID                string           `json:"id"`
	Project           string           `json:"project"`
	PointID           string           `json:"pointId"`
	PointName         string           `json:"pointName"`
	TemplateID        string           `json:"templateId"`
	TemplateName      string           `json:"templateName"`
	Type              string           `json:"type"`
	Inspector         string           `json:"inspector"`
	CaptureAttempts   int              `json:"captureAttempts"`
	ManualRequired    bool             `json:"manualRequired"`
	RecognitionStatus string           `json:"recognitionStatus"` // not_started / processing / recognized / retake_required / manual_required
	RetakeReason      string           `json:"retakeReason,omitempty"`
	TaskID            string           `json:"taskId,omitempty"`
	Images            []ImageInfo      `json:"images"`
	Fields            []FieldValue     `json:"fields"`
	Report            string           `json:"report"`
	AISummary         string           `json:"aiSummary"`
	AISummaryTags     []string         `json:"aiSummaryTags"`
	AIRecommendations []Recommendation `json:"aiRecommendations"`
	AISummaryError    string           `json:"aiSummaryError,omitempty"`
	Submitted         bool             `json:"submitted"`
	SubmittedAt       *time.Time       `json:"submittedAt,omitempty"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
}

// Recommendation — AI 行动建议
type Recommendation struct {
	Priority string `json:"priority"` // high / medium / low
	Category string `json:"category"` // 异常处理 / 趋势预警 / 数据补全 / 下次巡检关注
	Text     string `json:"text"`
	Basis    string `json:"basis"`
}

// AITask — AI 分析任务
type AITask struct {
	ID           string         `json:"id"`
	RecordID     string         `json:"recordId"`
	Status       string         `json:"status"` // queued / processing / succeeded / failed / superseded
	Progress     Progress       `json:"progress"`
	ErrorCode    string         `json:"errorCode,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	Analysis     map[string]any `json:"analysis,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type Progress struct {
	Processed int `json:"processed"`
	Total     int `json:"total"`
}

// AssetEntry — 资产台账条目
type AssetEntry struct {
	ID              string    `json:"id"` // {project}::{templateId}::{assetIdent}
	ProjectCode     string    `json:"projectCode,omitempty"`
	Project         string    `json:"project"`
	PointID         string    `json:"pointId,omitempty"`
	TemplateID      string    `json:"templateId,omitempty"`
	AssetType       string    `json:"assetType"`
	AssetKey        string    `json:"assetKey,omitempty"`
	AssetName       string    `json:"assetName"`
	LastRecordID    string    `json:"lastRecordId"`
	LastStatus      string    `json:"lastStatus"`
	StatusLevel     string    `json:"statusLevel,omitempty"` // normal / warning / danger / repair / unknown
	StatusOrder     int       `json:"statusOrder,omitempty"`
	LastSummary     string    `json:"lastSummary"`
	LastInspectedAt time.Time `json:"lastInspectedAt"`
	LastInspector   string    `json:"lastInspector,omitempty"`
	LastPhotoPath   string    `json:"lastPhotoPath,omitempty"`
	InspectionCount int       `json:"inspectionCount"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`

	// CoverImage 仅 list 接口动态填充，不入库。取自最近一次巡检的第一张图。
	CoverImage *ImageInfo `json:"coverImage,omitempty"`
}

// AssetListSummary — 台账列表页展示用的聚合数据
type AssetListSummary struct {
	Total           int                 `json:"total"`
	Normal          int                 `json:"normal"`
	Warning         int                 `json:"warning"`
	Danger          int                 `json:"danger"`
	Repair          int                 `json:"repair"`
	Unknown         int                 `json:"unknown"`
	ByStatus        map[string]int      `json:"byStatus"`
	ByProject       []AssetGroupSummary `json:"byProject"`
	ByAssetType     []AssetGroupSummary `json:"byAssetType"`
	RecentlyUpdated int                 `json:"recentlyUpdated"`
}

// AssetGroupSummary — 后端按项目 / 资产类型聚合后的展示分组
type AssetGroupSummary struct {
	Key      string         `json:"key"`
	Label    string         `json:"label"`
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"byStatus"`
}

// ChangeRequest — 数据修改申请（审批流主体）
// 所有对 asset / record 的字段变更必须先入这张表，主管审批通过才落库。
type ChangeRequest struct {
	ID          string         `json:"id"`
	TargetType  string         `json:"targetType"` // "asset" / "record"
	TargetID    string         `json:"targetId"`
	Patch       map[string]any `json:"patch"`  // 待应用的字段补丁
	Reason      string         `json:"reason"` // 申请理由（必填）
	Status      string         `json:"status"` // pending / approved / rejected / withdrawn
	RequestedBy string         `json:"requestedBy"`
	RequestedAt time.Time      `json:"requestedAt"`
	ReviewedBy  string         `json:"reviewedBy,omitempty"`
	ReviewedAt  *time.Time     `json:"reviewedAt,omitempty"`
	ReviewNote  string         `json:"reviewNote,omitempty"`
	AppliedAt   *time.Time     `json:"appliedAt,omitempty"`
}

// ChangeRequestFilter — ListChangeRequests 的查询条件
type ChangeRequestFilter struct {
	Status      string // 空 = 全部
	RequestedBy string // 空 = 全部（用于巡检员只看自己的）
	TargetType  string
	Limit       int // 0 = 默认 200
}

// SceneClassifyResult — AI 场景分类结果
type SceneClassifyResult struct {
	TemplateID      string           `json:"templateId"`
	TemplateName    string           `json:"templateName"`
	Confidence      float64          `json:"confidence"`
	Reason          string           `json:"reason"`
	Alternatives    []map[string]any `json:"alternatives"`
	NeedsManualPick bool             `json:"needsManualPick"`
	TmpDir          string           `json:"tmpDir"`
	Error           string           `json:"error,omitempty"`
}

// User is the first deliverable version of the admin identity model.
// It stays small so the existing inspection workflow can keep using inspector
// display names while the backend starts tracking real accounts.
type User struct {
	ID             string     `json:"id"`
	Username       string     `json:"username"`
	DisplayName    string     `json:"displayName"`
	Phone          string     `json:"phone,omitempty"`
	Avatar         string     `json:"avatar,omitempty"`
	RoleID         string     `json:"roleId"`
	RoleCode       string     `json:"roleCode"`
	RoleName       string     `json:"roleName"`
	DepartmentID   string     `json:"departmentId,omitempty"`
	DepartmentName string     `json:"departmentName,omitempty"`
	WeworkUserID   string     `json:"weworkUserId,omitempty"`
	Status         string     `json:"status"`
	LastLoginAt    *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type Role struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Department struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ParentID  string    `json:"parentId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type LoginSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Token     string    `json:"token,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type OperationLog struct {
	ID         string         `json:"id"`
	UserID     string         `json:"userId,omitempty"`
	ActorName  string         `json:"actorName"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type IdentitySeed struct {
	Username    string
	Password    string
	DisplayName string
}
