package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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
}

// SummarizeResponse — ai-service /summarize 返回
type SummarizeResponse struct {
	Summary         string           `json:"summary"`
	Tags            []string         `json:"tags"`
	Recommendations []Recommendation `json:"recommendations"`
	Model           string           `json:"model"`
}

// AIClient — 调 ai-service 的客户端
type AIClient struct {
	baseURL string
	http    *http.Client
}

func NewAIClient(baseURL string) *AIClient {
	return &AIClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 90 * time.Second, // 千问 vl-max 慢图能 30s+
		},
	}
}

// Analyze — 调 /analyze（识别字段）
func (c *AIClient) Analyze(payload map[string]any) (*AnalyzeResponse, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-service /analyze status %d: %s", resp.StatusCode, string(raw))
	}
	var out AnalyzeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode analyze: %w", err)
	}
	return &out, nil
}

// Summarize — 调 /summarize（生成总结+建议）
func (c *AIClient) Summarize(payload map[string]any) (*SummarizeResponse, error) {
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/summarize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-service /summarize status %d: %s", resp.StatusCode, string(raw))
	}
	var out SummarizeResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode summarize: %w", err)
	}
	if out.Recommendations == nil {
		out.Recommendations = []Recommendation{}
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	return &out, nil
}

// Classify — 调 /classify（场景分类，multipart 上传图）
func (c *AIClient) Classify(imagePaths []string) (*SceneClassifyResult, error) {
	// 内部调用走 JSON（ai-service 收到 paths 自己读图），不用 multipart
	payload := map[string]any{
		"imagePaths": imagePaths,
	}
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 35 * time.Second}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/classify", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-service /classify status %d: %s", resp.StatusCode, string(raw))
	}
	var out SceneClassifyResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode classify: %w", err)
	}
	return &out, nil
}

// Chat — 调 /chat（后台 AI 对话）
func (c *AIClient) Chat(payload map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	client := &http.Client{Timeout: 18 * time.Second}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ai-service /chat status %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode chat: %w", err)
	}
	return out, nil
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
