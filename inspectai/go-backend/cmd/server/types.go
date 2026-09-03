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
	ID        string `json:"id"`
	Name      string `json:"name"`
	Project   string `json:"project"`
	AssetType string `json:"assetType"`
	MaxImages int    `json:"maxImages"` // 单次上传图片上限（能耗抄表 6，其他 3）
	// MinImages 每单最少几张照片。0 = 不限。
	//
	// 【为什么做成模板级而不是全局写死 5】不同设备该拍几张差别很大:一台小水泵
	// 两张就说明问题,变电所可能要拍十几个柜。全局硬性 5 张会逼人凑数 ——
	// 而凑出来的照片是台账污染:以后翻历史看到的是几张糊的地面和天花板。
	// 默认给 5(周计划的要求),需要时逐个模板调。
	MinImages int             `json:"minImages"`
	Featured  bool            `json:"featured"`
	Fields    []TemplateField `json:"fields"`
	HasAI     bool            `json:"hasAI"`              // 是否接入了 AI prompt（决定是否调 ai-service）
	AIPrompt  string          `json:"aiPrompt,omitempty"` // 对应的 prompt 文件名（无后缀）

	// ===== 提示词(原来存在 prompt_templates 里的模板头)=====

	// Scene 场景一句话,写进提示词开头
	Scene string `json:"scene,omitempty"`
	// ExpectedPhotos 期望拍到哪些照片
	ExpectedPhotos []string `json:"expectedPhotos,omitempty"`
	// PromptMode structured(按字段表渲染)/ raw(直接写整段正文)。
	// 空值按 structured 处理 —— 老数据没有这个字段,默认必须落在"和以前一样"。
	PromptMode string `json:"promptMode,omitempty"`
	// RawText 仅 raw 模式使用。留空 = 没配,运行时回退内置 .md。
	RawText string `json:"rawText,omitempty"`
}

// TemplateField — 字段定义（模板里写死的）
type TemplateField struct {
	Code     string   `json:"code"`
	Label    string   `json:"label"`
	Kind     string   `json:"kind"` // text / number / choice
	Required bool     `json:"required"`
	Source   string   `json:"source"` // ai / manual
	Options  []string `json:"options,omitempty"`
	Default  string   `json:"default,omitempty"` // 默认值，移动端表单可预填
	// ManualOnly 人工填写，AI 不代填也不覆盖。
	//
	// 给设备编号这类【决定数据归属】的字段用:asset_no 是资产台账的主键
	// (buildAsset 拿它当资产名),AI 认错一个字符就会把两台设备并成一台，
	// 或者凭空多出一台。这种错在台账里很难发现，更难回滚。
	ManualOnly bool `json:"manualOnly,omitempty"`

	// ===== 判定规则(原来存在 prompt_templates 里的那一份)=====
	//
	// 【为什么并进来】同一个字段原来被描述了两遍:这里存"怎么填"
	// (类型/选项/必填),提示词那张表存"怎么判"(判是看什么、判否看什么)。
	// 两边的 code 一个不差地重合 —— 它们本来就是同一批字段,分成两张表
	// 纯粹是历史原因(提示词那套是后加的,没敢动模板)。
	//
	// 分着放的代价是永久的:每加一个功能都要问"这个改动要不要推到另一边",
	// 而漏掉的那次不报错 —— 表现是两个页面对同一个字段说不一样的话。
	// 并进来之后,提示词页和模板页只是同一张字段表的两个视角。

	// JudgeMode 这一项"怎么判"的固定套路(visual / read_text / summary …)
	JudgeMode string `json:"judgeMode,omitempty"`
	// JudgeGroup 分组:头部 / 机房 / 轿厢层站 / 汇总。只影响提示词里的排版。
	JudgeGroup string `json:"judgeGroup,omitempty"`
	YesWhen    string `json:"yesWhen,omitempty"`
	NoWhen     string `json:"noWhen,omitempty"`
	// SkipWhen 什么情况不返回(留给人工)
	SkipWhen  string `json:"skipWhen,omitempty"`
	JudgeNote string `json:"judgeNote,omitempty"`
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
	TenantID          string           `json:"tenantId,omitempty"`
	RecordNo          string           `json:"recordNo,omitempty"`
	Project           string           `json:"project"`
	PointID           string           `json:"pointId"`
	PointName         string           `json:"pointName"`
	TemplateID        string           `json:"templateId"`
	TemplateName      string           `json:"templateName"`
	Type              string           `json:"type"`
	Inspector         string           `json:"inspector"`
	InspectorUserID   string           `json:"inspectorUserId,omitempty"`
	CaptureAttempts   int              `json:"captureAttempts"`
	ManualRequired    bool             `json:"manualRequired"`
	RecognitionStatus string           `json:"recognitionStatus"` // not_started / processing / recognized / retake_required / manual_required
	RetakeReason      string           `json:"retakeReason,omitempty"`
	TaskID            string           `json:"taskId,omitempty"`
	EngineeringTaskID string           `json:"engineeringTaskId,omitempty"`
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

// AssetSnapshot — 一台资产在某次已提交巡检后的状态快照（§3 长期台账）。
// 每条已提交记录 × 其触及的每台资产写一行，按资产可完整翻历史，不受记录列表窗口限制。
type AssetSnapshot struct {
	ID          string    `json:"id"`
	AssetID     string    `json:"assetId"`
	RecordID    string    `json:"recordId"`
	Status      string    `json:"status"`
	StatusLevel string    `json:"statusLevel"`
	Summary     string    `json:"summary"`
	Inspector   string    `json:"inspector"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FieldObservation — 字段级观测明细（§3 趋势分析的数据底座）。
// 数值字段额外存 ValueNumber，便于后端算变化率/均值/阈值。
type FieldObservation struct {
	ID          string    `json:"id"`
	AssetID     string    `json:"assetId"`
	RecordID    string    `json:"recordId"`
	FieldKey    string    `json:"fieldKey"`
	FieldLabel  string    `json:"fieldLabel"`
	ValueText   string    `json:"valueText"`
	ValueNumber *float64  `json:"valueNumber,omitempty"`
	Source      string    `json:"source"`
	Confidence  float64   `json:"confidence"`
	CreatedAt   time.Time `json:"createdAt"`
}

// FieldConfirmLog — 字段人工确认留痕（§4 防惰性闭环）。
// 记录 AI 原值 → 最终值、置信度、动作（confirm/correct/uncertain）、操作人、停留时长、是否看图。
type FieldConfirmLog struct {
	ID            string    `json:"id"`
	RecordID      string    `json:"recordId"`
	FieldKey      string    `json:"fieldKey"`
	FieldLabel    string    `json:"fieldLabel"`
	AIValue       string    `json:"aiValue"`
	OriginalValue string    `json:"originalValue"`
	FinalValue    string    `json:"finalValue"`
	AIConfidence  float64   `json:"aiConfidence"`
	Action        string    `json:"action"`
	Operator      string    `json:"operator"`
	DurationMs    int       `json:"durationMs"`
	ViewedPhoto   bool      `json:"viewedPhoto"`
	CreatedAt     time.Time `json:"createdAt"`
}

// ===== 管理 AI(DeepSeek)的类型 — 阶段一只先建数据骨架 =====

// AttentionItem — Top-N 重点关注列表里的一条(后端 risk_score 算出来,AI 加摘要)。
type AttentionItem struct {
	AssetID         string       `json:"assetId"`
	AssetName       string       `json:"assetName"`
	AssetType       string       `json:"assetType,omitempty"`
	Project         string       `json:"project,omitempty"`
	RiskScore       int          `json:"riskScore"`
	RiskLevel       string       `json:"riskLevel"` // normal / warning / danger
	Title           string       `json:"title"`
	Reasons         []string     `json:"reasons"`
	Breakdown       []RiskFactor `json:"breakdown,omitempty"` // 百分制评分卡:各维度得分/满分/依据
	Action          string       `json:"action,omitempty"`
	LastRecordID    string       `json:"lastRecordId,omitempty"`
	LastInspectedAt string       `json:"lastInspectedAt,omitempty"`
	Evidence        []ItemRef    `json:"evidence,omitempty"`
}

// RiskFactor — 风险分的单个维度(评分卡一行)。
type RiskFactor struct {
	Label string `json:"label"`
	Score int    `json:"score"`
	Max   int    `json:"max"`
	Basis string `json:"basis,omitempty"`
}

// ItemRef — 可跳转的证据引用(资产 / 巡检记录 / 字段观测)。
type ItemRef struct {
	Type  string `json:"type"` // asset / record / observation
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
	Time  string `json:"time,omitempty"`
}

// OverviewSummary — 看板顶部数字汇总。
type OverviewSummary struct {
	AssetTotal       int     `json:"assetTotal"`
	AssetNormal      int     `json:"assetNormal"`
	AssetWarning     int     `json:"assetWarning"`
	AssetDanger      int     `json:"assetDanger"`
	RecordTotal      int     `json:"recordTotal"`
	RecordRecent     int     `json:"recordRecent"` // 本期内
	RecordPrev       int     `json:"recordPrev"`   // 上期内
	AbnormalRecent   int     `json:"abnormalRecent"`
	AbnormalPrev     int     `json:"abnormalPrev"`
	PendingApprovals int     `json:"pendingApprovals"`
	PendingReviews   int     `json:"pendingReviews"`
	DriftFieldCount  int     `json:"driftFieldCount"`
	LazyConfirmRate  float64 `json:"lazyConfirmRate"` // 未看图就确认占比(0-1)
	RangeKey         string  `json:"rangeKey"`
	RangeStart       string  `json:"rangeStart"`
	RangeEnd         string  `json:"rangeEnd"`
}

// InspectorQualityRow — 巡检员质量榜一行(§4 防惰性主管视角)。
type InspectorQualityRow struct {
	Operator         string `json:"operator"`
	Total            int    `json:"total"`
	RetakeCount      int    `json:"retakeCount"`
	UncertainCount   int    `json:"uncertainCount"`
	NoPhotoConfirm   int    `json:"noPhotoConfirm"`
	FastConfirmCount int    `json:"fastConfirmCount"` // 停留 <2s 的确认数
	AvgDurationMs    int    `json:"avgDurationMs"`
	Comment          string `json:"comment,omitempty"` // AI 加,阶段一暂空
}

// RepeatedIssue — 同一资产/字段重复出现的问题。
type RepeatedIssue struct {
	AssetID    string `json:"assetId"`
	AssetName  string `json:"assetName"`
	FieldKey   string `json:"fieldKey,omitempty"`
	FieldLabel string `json:"fieldLabel,omitempty"`
	Count      int    `json:"count"`
	LastTime   string `json:"lastTime"`
	Issue      string `json:"issue"`
}

// ManagementAIReport — 持久化缓存表行,30 min 刷新一次。
// summary / attention / recommendations 都是 AI 出的,facts 是后端聚合的事实。
type ManagementAIReport struct {
	ID              string           `json:"id"`
	ReportType      string           `json:"reportType"` // attention / overview-chat
	Project         string           `json:"project"`
	RangeKey        string           `json:"rangeKey"`
	Facts           map[string]any   `json:"facts"`
	Summary         string           `json:"summary"`
	Attention       []*AttentionItem `json:"attention"`
	Recommendations []Recommendation `json:"recommendations"`
	Evidence        []ItemRef        `json:"evidence"`
	Model           string           `json:"model"`
	PromptVersion   string           `json:"promptVersion"`
	DurationMs      int              `json:"durationMs"`
	GeneratedAt     time.Time        `json:"generatedAt"`
	ExpiresAt       time.Time        `json:"expiresAt"`
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
	TenantID        string    `json:"tenantId,omitempty"`
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
	CoverImagePath  string    `json:"coverImagePath,omitempty"` // 主管指定的封面图路径，入库
	InspectionCount int       `json:"inspectionCount"`
	// ===== 静态档案(向甲方索要的那批资料)=====
	//
	// 【为什么要有它们:趋势只有相对值,没有绝对判断】供水压力 0.55 MPa
	// 是低还是正常?看曲线只知道"比平时低 8%",而"是不是低到该报修"
	// 要对着设计值才说得出来。一台 2011 年投运的电梯和 2024 年的,
	// 同样的读数含义也完全不同。
	//
	// 全部可空:拿到多少填多少,没填的在界面上直接不显示。
	Manufacturer     string `json:"manufacturer,omitempty"`
	Model            string `json:"model,omitempty"`
	CommissionedAt   string `json:"commissionedAt,omitempty"`   // YYYY-MM-DD 投运日期
	LastMaintainedAt string `json:"lastMaintainedAt,omitempty"` // YYYY-MM-DD 上次维保
	// MaintenanceCycleDays 维保周期(天)。
	//
	// 【0 = 没填,不是"不用维保"】判断那边遇到 0 要直接跳过 ——
	// 当成"周期为 0 天"的话,每台没填周期的设备都会被报成"已超期"。
	MaintenanceCycleDays int    `json:"maintenanceCycleDays,omitempty"`
	AssetNote            string `json:"assetNote,omitempty"`
	// countedInspections 内部标记:列表路径已批量算过巡检次数,
	// enrichAssetForDisplay 就不必再逐台查一遍。不出现在 JSON 里。
	countedInspections bool
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`

	// CoverImage 仅 API 展示时动态填充，不入库。优先取 CoverImagePath，回退到最近一次巡检的第一张图。
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

	// TargetName 仅 API 展示时动态填充,不入库。
	// 【为什么需要】TargetID 对资产是 "会议中心::elevator_no_room::KT-5",
	// 对记录是 "rec_1754..." —— 审批的人看这两串东西判断不了在改哪台设备。
	// 后台审批列表的「设备」列一度整列空白,就是因为只有 ID 没有名字。
	TargetName string `json:"targetName,omitempty"`
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
// 账号状态。【值是英文,不是中文】—— 界面上显示「停用」,库里存的是 "disabled"。
//
// 定成常量是因为这个坑已经踩过一次:写 `u.Status == "停用"` 编译得过、
// 静态检查也过,但那个判断【永远不成立】—— 停用的账号照样被当成有效负责人。
// 这类错不会报,只会表现成"某个规则好像没生效"。
const (
	userStatusActive   = "active"
	userStatusDisabled = "disabled"
)

type User struct {
	ID       string `json:"id"`
	TenantID string `json:"tenantId"`
	// IsPlatformAdmin 平台超管:唯一能跨租户(建/停客户、跨租户运维)。
	// 与租户归属正交 —— 超管自身的业务数据仍归其所属租户。
	IsPlatformAdmin bool   `json:"isPlatformAdmin,omitempty"`
	Username        string `json:"username"`
	DisplayName     string `json:"displayName"`
	Phone           string `json:"phone,omitempty"`
	Avatar          string `json:"avatar,omitempty"`
	RoleID          string `json:"roleId"`
	RoleCode        string `json:"roleCode"`
	RoleName        string `json:"roleName"`
	// DataScope 数据范围:这个人能看到多少数据(与 RoleCode "能做什么"正交)。
	// 空 = 按角色推导(管理角色看全部、其余看自己的),即现有行为。
	// 取值见 dataScopeXxx 常量。
	DataScope      string     `json:"dataScope,omitempty"`
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
