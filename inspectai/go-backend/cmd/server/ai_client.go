package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AnalyzeResponse — ai-service /analyze 返回
type AnalyzeResponse struct {
	SchemaVersion     string            `json:"schemaVersion"`
	AnalysisID        string            `json:"analysisId"`
	RecordID          string            `json:"recordId"`
	Model             map[string]string `json:"model"`
	ProcessedAt       string            `json:"processedAt"`
	DurationMs        int               `json:"durationMs"`
	RecognizedFields  []RecognizedField `json:"recognizedFields"`
	RecognitionStatus string            `json:"recognitionStatus"`
	RetakeReason      string            `json:"retakeReason,omitempty"`
	Observations      []string          `json:"observations"`
	Warnings          []string          `json:"warnings"`
	Raw               map[string]any    `json:"-"`
}

type RecognizedField struct {
	Code       string  `json:"code"`
	Label      string  `json:"label,omitempty"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`

	// ImageIndex 这个读数是从第几张图读出来的(1 开始,按上传顺序)。
	// Bbox 读数区在那张图里的位置,归一化 [左,上,右,下]。
	//
	// 【留着是给现场看的,不只是给二次复核用】确认页上一个光秃秃的数字,
	// 人没有参照物就只能凭记忆去对六张照片 —— 实际发生的是不对,直接确认。
	// 有了这两个值,那一行旁边就能摆出读数区的特写,一眼看得出配没配错。
	ImageIndex int       `json:"imageIndex,omitempty"`
	Bbox       []float64 `json:"bbox,omitempty"`
}

// SummarizeResponse — ai-service /summarize 返回
type SummarizeResponse struct {
	Summary         string           `json:"summary"`
	Tags            []string         `json:"tags"`
	Recommendations []Recommendation `json:"recommendations"`
	Model           string           `json:"model"`
}

// ===== 等多久:Go 和 ai-service 用同一份预算 =====
//
// 【Go 等的时间必须比 ai-service 花的时间长】原来两边各设各的:Go 等识别 90 秒,
// ai-service 第一遍自己就允许 90 秒、再重试、再做放大复核 —— Go 早放弃了,
// 那边还在调模型、还在花钱,结果没人收;分类是 Go 等 35 秒、那边 90 秒起步。
// 现在 Go 把预算随请求带过去(budgetSeconds),ai-service 按它决定还来不来得及
// 重试、复核;Go 自己在预算之上多等一点余量。
const (
	// 移动端最多轮询 80 秒(mobile-web RecordPage 的 POLL_MAX)—— 识别必须在那之前出结果
	analyzeBudget   = 75 * time.Second
	classifyBudget  = 30 * time.Second // 现场拍完照同步等
	summarizeBudget = 25 * time.Second
	draftBudget     = 80 * time.Second
	// 本机到 ai-service 的往返、JSON 编解码
	aiBudgetMargin = 8 * time.Second
)

// AIClient — 调 ai-service 的客户端
type AIClient struct {
	baseURL string
}

func NewAIClient(baseURL string) *AIClient {
	return &AIClient{baseURL: strings.TrimRight(baseURL, "/")}
}

// postJSON 带预算发一次请求,把响应解进 out。
func (c *AIClient) postJSON(path string, payload map[string]any, budget time.Duration, out any) error {
	payload["budgetSeconds"] = int(budget / time.Second)
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: budget + aiBudgetMargin}
	resp, err := client.Post(c.baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ai-service %s status %d: %s", path, resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// Analyze — 调 /analyze（识别字段）
func (c *AIClient) Analyze(payload map[string]any) (*AnalyzeResponse, error) {
	var out AnalyzeResponse
	if err := c.postJSON("/analyze", payload, analyzeBudget, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Summarize — 调 /summarize（生成总结+建议）
func (c *AIClient) Summarize(payload map[string]any) (*SummarizeResponse, error) {
	var out SummarizeResponse
	if err := c.postJSON("/summarize", payload, summarizeBudget, &out); err != nil {
		return nil, err
	}
	if out.Recommendations == nil {
		out.Recommendations = []Recommendation{}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return &out, nil
}

// BuiltinPrompt — 取 ai-service 内置的那份提示词正文(prompts/*.md)。
//
// 【只在后台点开编辑器时调,不在识别链路上】识别走的是 promptText 下发,
// 不依赖这个接口 —— 所以它挂了顶多是"编辑器载不出底稿",识别照跑。
func (c *AIClient) BuiltinPrompt(templateID string) (string, error) {
	u := c.baseURL + "/prompt-source?template=" + url.QueryEscape(templateID)
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ai-service /prompt-source status %d", resp.StatusCode)
	}
	var out struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode prompt-source: %w", err)
	}
	return out.Prompt, nil
}

// SceneCandidate 一个可供选择的场景。
//
// 【候选表从库里来,不再写死在 ai-service 里】以前那张表写死在
// prompts/scene_classifier.md,提示词还明写着"必须从这个清单选一个" ——
// 后台新建的模板不在清单里,模型返回不了它的 id。
type SceneCandidate struct {
	TemplateID   string `json:"templateId"`
	TemplateName string `json:"templateName"`
	Features     string `json:"features"` // 照片上一眼能认出来的东西
}

// Classify — 调 /classify（场景分类）
//
// candidates 为空时 ai-service 会回退用内置的那张表 —— 和 promptText 一样的
// 灰度策略:下发出问题时,退回到今天这个能跑的状态,而不是一个都认不出来。
func (c *AIClient) Classify(imagePaths []string, candidates []SceneCandidate) (*SceneClassifyResult, error) {
	// 内部调用走 JSON（ai-service 收到 paths 自己读图），不用 multipart
	payload := map[string]any{
		"imagePaths": imagePaths,
		"candidates": candidates,
	}
	var out SceneClassifyResult
	if err := c.postJSON("/classify", payload, classifyBudget, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// === 工具 ===

// saveMultipartFile — 保存 multipart 文件到目标目录，返回 ImageInfo（不计算 EXIF 等）
func saveMultipartFile(targetDir string, header *multipart.FileHeader, maxSize int64) (ImageInfo, error) {
	if header.Size > maxSize {
		return ImageInfo{}, fmt.Errorf("文件超过 %d MB 限制", maxSize/(1<<20))
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(header.Filename), "."))
	switch ext {
	case "jpg", "jpeg", "png", "webp":
	default:
		return ImageInfo{}, fmt.Errorf("不支持的图片格式：%s", ext)
	}
	file, err := header.Open()
	if err != nil {
		return ImageInfo{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return ImageInfo{}, err
	}
	if int64(len(data)) > maxSize {
		return ImageInfo{}, fmt.Errorf("文件超过 %d MB 限制", maxSize/(1<<20))
	}

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return ImageInfo{}, err
	}
	imageID := newID("img")
	safeName := sanitizeFileName(header.Filename)
	target := filepath.Join(targetDir, imageID+"_"+safeName)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{
		ID:        imageID,
		FileName:  header.Filename,
		Path:      target,
		Size:      int64(len(data)),
		CreatedAt: time.Now(),
	}, nil
}

// DraftFields — 需求文字 → 字段表(调 ai-service 的 /prompt/draft-fields)。
//
// 【只在后台点"生成"时调,不在识别链路上】它挂了顶多是这一次生成不出来,
// 现场巡检照常。
func (c *AIClient) DraftFields(requirement, templateName, assetType string) (
	fields []map[string]any, sceneFeatures, model string, err error,
) {
	payload := map[string]any{
		"requirement":   requirement,
		"templateName":  templateName,
		"assetType":     assetType,
		"budgetSeconds": int(draftBudget / time.Second),
	}
	body, _ := json.Marshal(payload)
	// 生成要跑一次大模型,比识别还慢一些 —— 超时给足,否则人点了"生成"
	// 转半天最后告诉他超时,他只会以为功能坏了。
	client := &http.Client{Timeout: draftBudget + aiBudgetMargin}
	resp, err := client.Post(c.baseURL+"/prompt/draft-fields", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Fields        []map[string]any `json:"fields"`
		SceneFeatures string           `json:"sceneFeatures"`
		Model         string           `json:"model"`
		Error         string           `json:"error"`
		Message       string           `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, "", "", fmt.Errorf("解析生成结果失败: %w", err)
	}
	if out.Error != "" {
		// ai-service 那边的话已经是人话,直接往上传 —— 换成"生成失败"
		// 会让人完全不知道是没配密钥、还是需求写得太含糊。
		return nil, "", "", errors.New(firstNonEmpty(out.Message, out.Error))
	}
	return out.Fields, out.SceneFeatures, out.Model, nil
}
