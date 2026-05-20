package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Point struct {
	ID         string `json:"id"`
	Project    string `json:"project"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Location   string `json:"location"`
	TemplateID string `json:"templateId"`
}

type Record struct {
	ID                string       `json:"id"`
	Project           string       `json:"project"`
	PointID           string       `json:"pointId"`
	PointName         string       `json:"pointName"`
	Type              string       `json:"type"`
	TemplateID        string       `json:"templateId"`
	TemplateName      string       `json:"templateName"`
	Inspector         string       `json:"inspector"`
	CreatedAt         time.Time    `json:"createdAt"`
	Images            []ImageInfo  `json:"images"`
	CaptureAttempts   int          `json:"captureAttempts"`
	ManualRequired    bool         `json:"manualRequired"`
	RecognitionStatus string       `json:"recognitionStatus"`
	RetakeReason      string       `json:"retakeReason,omitempty"`
	TaskID            string       `json:"taskId,omitempty"`
	Fields            []FieldValue `json:"fields"`
	CheckItems        []CheckItem  `json:"checkItems"`
	Report            string       `json:"report"`
	AISummary         string       `json:"aiSummary"`
	Submitted         bool         `json:"submitted"`
	SubmittedAt       *time.Time   `json:"submittedAt,omitempty"`
}

type ImageInfo struct {
	ID          string `json:"id"`
	FileName    string `json:"fileName"`
	Path        string `json:"path"`
	ImageType   string `json:"imageType"`
	Size        int64  `json:"size"`
	ContentHash string `json:"contentHash"`
}

type AITask struct {
	ID        string      `json:"id"`
	RecordID  string      `json:"recordId"`
	Status    string      `json:"status"`
	Progress  Progress    `json:"progress"`
	ErrorCode string      `json:"errorCode,omitempty"`
	Error     string      `json:"errorMessage,omitempty"`
	Analysis  *AIAnalysis `json:"analysis,omitempty"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type Progress struct {
	Processed int `json:"processed"`
	Total     int `json:"total"`
}

type AIAnalysis struct {
	SchemaVersion     string            `json:"schemaVersion"`
	AnalysisID        string            `json:"analysisId"`
	RecordID          string            `json:"recordId"`
	Model             ModelInfo         `json:"model"`
	ProcessedAt       string            `json:"processedAt"`
	DurationMs        int               `json:"durationMs"`
	TokenUsage        TokenUsage        `json:"tokenUsage"`
	PartialFail       bool              `json:"partialFailure"`
	FailedImageID     []string          `json:"failedImageIds"`
	Images            []AIImageResult   `json:"images"`
	RecognizedFields  []RecognizedField `json:"recognizedFields"`
	RecognitionStatus string            `json:"recognitionStatus"`
	RetakeReason      string            `json:"retakeReason,omitempty"`
	Warnings          []string          `json:"warnings"`
}

type ModelInfo struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Version  string `json:"version"`
}

type TokenUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	ImageCount int `json:"imageCount"`
}

type AIImageResult struct {
	ImageID         string                 `json:"imageId"`
	DetectedType    string                 `json:"detectedImageType"`
	DetectedScene   string                 `json:"detectedScene"`
	Observations    []string               `json:"observations"`
	ExtractedFields map[string]interface{} `json:"extractedFields"`
	VisualFindings  []VisualFinding        `json:"visualFindings"`
	Quality         Quality                `json:"quality"`
	Confidence      float64                `json:"confidence"`
	NeedReview      bool                   `json:"needReview"`
}

type VisualFinding struct {
	Code       string  `json:"code"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
}

type Quality struct {
	IsBlurry      bool    `json:"isBlurry"`
	IsOverExposed bool    `json:"isOverExposed"`
	IsRotated     bool    `json:"isRotated"`
	Score         float64 `json:"score"`
}

type CheckItem struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	JudgeType    string   `json:"judgeType"`
	Status       string   `json:"status"`
	FinalStatus  string   `json:"finalStatus"`
	Basis        []string `json:"basis"`
	Confidence   float64  `json:"confidence"`
	ReviewStatus string   `json:"reviewStatus"`
	Version      int      `json:"version"`
}

type Store struct {
	mu      sync.RWMutex
	points  []Point
	records map[string]*Record
	tasks   map[string]*AITask
	assets  []AssetEntry
}

type Server struct {
	store       *Store
	aiURL       string
	storageDir  string
	frontendDir string
	static      http.Handler
	client      *http.Client
}

func main() {
	store := &Store{
		points:  seedPoints(),
		records: map[string]*Record{},
		tasks:   map[string]*AITask{},
		assets:  []AssetEntry{},
	}

	frontendDir := getenv("FRONTEND_DIR", "../frontend")
	server := &Server{
		store:       store,
		aiURL:       strings.TrimRight(getenv("AI_SERVICE_URL", "http://127.0.0.1:9100"), "/"),
		storageDir:  getenv("STORAGE_DIR", "../storage"),
		frontendDir: frontendDir,
		static:      http.FileServer(http.Dir(frontendDir)),
		client:      &http.Client{Timeout: 30 * time.Second},
	}

	if err := os.MkdirAll(server.storageDir, 0755); err != nil {
		log.Fatalf("create storage dir: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.health)
	mux.HandleFunc("/api/", server.api)
	mux.HandleFunc("/", server.frontend)

	addr := getenv("BACKEND_ADDR", ":8080")
	log.Printf("Go backend listening on %s", addr)
	log.Printf("Frontend dir: %s", frontendDir)
	log.Printf("AI service URL: %s", server.aiURL)
	log.Fatal(http.ListenAndServe(addr, requestLog(mux)))
}

func seedPoints() []Point {
	return []Point{
		{ID: "point_ups_room", Project: "会议中心", Name: "UPS机房", Type: "日常巡检", Location: "B1 设备区", TemplateID: "ups_room"},
		{ID: "point_power_room", Project: "会议中心", Name: "变电所", Type: "日常巡检", Location: "B1 强电间", TemplateID: "power_room"},
		{ID: "point_fire_pump", Project: "会议中心", Name: "消防泵房", Type: "日常巡检", Location: "B1 水泵房", TemplateID: "fire_pump"},
		{ID: "point_hot_water", Project: "会议中心", Name: "热水机房", Type: "日常巡检", Location: "B1 机房", TemplateID: "hot_water_room"},
		{ID: "point_water_pump", Project: "会议中心", Name: "生活水泵房", Type: "日常巡检", Location: "B1 生活水泵房", TemplateID: "water_pump"},
		{ID: "point_escalator", Project: "会议中心", Name: "扶梯", Type: "日常巡检", Location: "公共区域扶梯", TemplateID: "escalator"},
		{ID: "point_elevator_no_room", Project: "会议中心", Name: "无机房电梯", Type: "日常巡检", Location: "电梯轿厢", TemplateID: "elevator_no_room"},
		{ID: "point_elevator_mr", Project: "会议中心", Name: "有机房电梯", Type: "日常巡检", Location: "电梯机房", TemplateID: "elevator_machine_room"},
		{ID: "point_zihan_daily", Project: "紫涵雅集", Name: "每日综合巡检", Type: "综合巡检", Location: "园区", TemplateID: "zihan_daily"},
		{ID: "point_zihan_energy", Project: "紫涵雅集", Name: "能耗抄表", Type: "能耗抄表", Location: "水电表点位", TemplateID: "zihan_energy"},
	}
}

func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":       "ok",
		"service":      "go-backend",
		"aiServiceUrl": s.aiURL,
		"time":         time.Now(),
	})
}

func (s *Server) frontend(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(s.frontendDir, filepath.Clean(r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		s.static.ServeHTTP(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.frontendDir, "index.html"))
}

func (s *Server) api(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/inspection/points":
		s.listPoints(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/report/templates":
		s.listTemplates(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/assets":
		s.listAssets(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/inspection/records":
		s.listRecords(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/inspection/records":
		s.createRecord(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/inspection/records/"):
		s.recordRoutes(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/ai/tasks/") && r.Method == http.MethodGet:
		s.getTask(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
	}
}

func (s *Server) listPoints(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"points": s.store.points})
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{"templates": reportTemplates()})
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"assets": s.store.assets})
}

func (s *Server) listRecords(w http.ResponseWriter, r *http.Request) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()

	records := make([]*Record, 0, len(s.store.records))
	for _, rec := range s.store.records {
		records = append(records, rec)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"records": records})
}

func (s *Server) createRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PointID    string `json:"pointId"`
		TemplateID string `json:"templateId"`
		Type       string `json:"type"`
		Inspector  string `json:"inspector"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Inspector == "" {
		req.Inspector = "巡检员"
	}

	point, ok := s.findPoint(req.PointID)
	if !ok {
		writeError(w, http.StatusBadRequest, "point_not_found", "巡检点位不存在")
		return
	}
	templateID := firstNonEmpty(req.TemplateID, point.TemplateID)
	tpl, ok := templateByID(templateID)
	if !ok {
		writeError(w, http.StatusBadRequest, "template_not_found", "日报模板不存在")
		return
	}

	rec := &Record{
		ID:                newID("rec"),
		Project:           point.Project,
		PointID:           point.ID,
		PointName:         point.Name,
		Type:              firstNonEmpty(req.Type, point.Type),
		TemplateID:        tpl.ID,
		TemplateName:      tpl.Name,
		Inspector:         req.Inspector,
		CreatedAt:         time.Now(),
		Images:            []ImageInfo{},
		RecognitionStatus: "not_started",
		Fields:            initialFieldValues(tpl, req.Inspector),
	}

	s.store.mu.Lock()
	s.store.records[rec.ID] = rec
	s.store.mu.Unlock()

	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) recordRoutes(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/inspection/records/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
		return
	}
	recordID := parts[0]

	if len(parts) == 2 && parts[1] == "images" && r.Method == http.MethodPost {
		s.uploadImages(w, r, recordID)
		return
	}
	if len(parts) == 2 && parts[1] == "ai-tasks" && r.Method == http.MethodPost {
		s.startAnalysis(w, r, recordID)
		return
	}
	if len(parts) == 3 && parts[1] == "ai" && parts[2] == "latest" && r.Method == http.MethodGet {
		s.getLatestTask(w, r, recordID)
		return
	}
	if len(parts) == 4 && parts[1] == "check-items" && r.Method == http.MethodPatch {
		s.patchCheckItem(w, r, recordID, parts[2])
		return
	}
	if len(parts) == 3 && parts[1] == "fields" && r.Method == http.MethodPatch {
		s.patchField(w, r, recordID, parts[2])
		return
	}
	if len(parts) == 2 && parts[1] == "manual" && r.Method == http.MethodPost {
		s.enableManualMode(w, r, recordID)
		return
	}
	if len(parts) == 2 && parts[1] == "submit" && r.Method == http.MethodPost {
		s.submitRecord(w, r, recordID)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.getRecord(w, r, recordID)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
}

func (s *Server) getRecord(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, ok := s.record(recordID)
	if !ok {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) uploadImages(w http.ResponseWriter, r *http.Request, recordID string) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	rec, ok := s.record(recordID)
	if !ok {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "至少上传一张图片")
		return
	}
	if len(rec.Images)+len(files) > 3 {
		writeError(w, http.StatusBadRequest, "too_many_files", "第一版单次巡检最多上传3张图片")
		return
	}

	saved := []ImageInfo{}
	for _, header := range files {
		img, err := s.saveUploadedFile(recordID, header)
		if err != nil {
			writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
			return
		}
		saved = append(saved, img)
	}

	s.store.mu.Lock()
	rec.Images = append(rec.Images, saved...)
	s.store.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]interface{}{"images": saved, "record": rec})
}

func (s *Server) saveUploadedFile(recordID string, header *multipart.FileHeader) (ImageInfo, error) {
	if header.Size > 10<<20 {
		return ImageInfo{}, errors.New("单张图片不能超过10MB")
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

	data, err := io.ReadAll(io.LimitReader(file, 10<<20+1))
	if err != nil {
		return ImageInfo{}, err
	}
	if int64(len(data)) > 10<<20 {
		return ImageInfo{}, errors.New("单张图片不能超过10MB")
	}

	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	imageID := newID("img")
	dir := filepath.Join(s.storageDir, "uploads", recordID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ImageInfo{}, err
	}

	safeName := sanitizeFileName(header.Filename)
	target := filepath.Join(dir, imageID+"_"+safeName)
	if err := os.WriteFile(target, data, 0644); err != nil {
		return ImageInfo{}, err
	}

	return ImageInfo{
		ID:          imageID,
		FileName:    header.Filename,
		Path:        target,
		ImageType:   "unknown",
		Size:        int64(len(data)),
		ContentHash: hash,
	}, nil
}

func (s *Server) startAnalysis(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, ok := s.record(recordID)
	if !ok {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	if len(rec.Images) == 0 {
		writeError(w, http.StatusBadRequest, "no_images", "请先上传巡检图片")
		return
	}

	now := time.Now()
	task := &AITask{
		ID:        newID("task"),
		RecordID:  recordID,
		Status:    "queued",
		Progress:  Progress{Processed: 0, Total: len(rec.Images)},
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.store.mu.Lock()
	rec.CaptureAttempts++
	rec.RecognitionStatus = "processing"
	rec.RetakeReason = ""
	if rec.TaskID != "" {
		if old := s.store.tasks[rec.TaskID]; old != nil && old.Status != "succeeded" && old.Status != "failed" {
			old.Status = "superseded"
			old.UpdatedAt = time.Now()
		}
	}
	rec.TaskID = task.ID
	s.store.tasks[task.ID] = task
	s.store.mu.Unlock()

	go s.runAnalysis(task.ID)
	writeJSON(w, http.StatusAccepted, task)
}

func (s *Server) runAnalysis(taskID string) {
	s.updateTask(taskID, func(task *AITask) {
		task.Status = "processing"
		task.UpdatedAt = time.Now()
	})

	task, ok := s.task(taskID)
	if !ok {
		return
	}
	rec, ok := s.record(task.RecordID)
	if !ok {
		s.failTask(taskID, "record_not_found", "巡检记录不存在")
		return
	}

	point, _ := s.findPoint(rec.PointID)
	payload := map[string]interface{}{
		"recordId": rec.ID,
		"point":    point,
		"template": mustTemplate(rec.TemplateID),
		"images":   rec.Images,
	}

	analysis, err := s.callAI(payload)
	if err != nil {
		analysis = s.localFallbackAnalysis(rec, point, err)
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()

	rec = s.store.records[task.RecordID]
	if rec == nil {
		return
	}
	failed, reason := recognitionFailed(analysis, rec)
	if failed {
		rec.RetakeReason = reason
		if rec.CaptureAttempts >= 3 {
			rec.ManualRequired = true
			rec.RecognitionStatus = "manual_required"
			rec.Report = buildDailyPreview(rec)
		} else {
			rec.RecognitionStatus = "retake_required"
		}
		if current := s.store.tasks[taskID]; current != nil {
			current.Status = "failed"
			current.ErrorCode = rec.RecognitionStatus
			current.Error = reason
			current.Analysis = analysis
			current.UpdatedAt = time.Now()
		}
		return
	}

	applyRecognizedFields(rec, analysis.RecognizedFields)
	rec.RecognitionStatus = "recognized"
	rec.ManualRequired = false
	rec.RetakeReason = ""
	rec.CheckItems = buildCheckItemsFromFields(rec.Fields)
	rec.Report = buildDailyPreview(rec)

	if current := s.store.tasks[taskID]; current != nil {
		current.Status = "succeeded"
		current.Progress.Processed = current.Progress.Total
		current.Analysis = analysis
		current.UpdatedAt = time.Now()
	}
}

func (s *Server) callAI(payload map[string]interface{}) (*AIAnalysis, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, s.aiURL+"/analyze", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ai service status %d: %s", resp.StatusCode, string(raw))
	}

	var analysis AIAnalysis
	if err := json.NewDecoder(resp.Body).Decode(&analysis); err != nil {
		return nil, err
	}
	return &analysis, nil
}

func (s *Server) localFallbackAnalysis(rec *Record, point Point, cause error) *AIAnalysis {
	warnings := []string{"AI_SERVICE_FALLBACK", cause.Error()}
	results := make([]AIImageResult, 0, len(rec.Images))
	for _, img := range rec.Images {
		results = append(results, AIImageResult{
			ImageID:       img.ID,
			DetectedType:  "environment_scene",
			DetectedScene: point.Name,
			Observations: []string{
				"本地演示降级结果：现场图片已接收",
				"需由人工复核确认具体异常",
			},
			ExtractedFields: map[string]interface{}{},
			VisualFindings:  []VisualFinding{},
			Quality:         Quality{Score: 0.55},
			Confidence:      0.55,
			NeedReview:      true,
		})
	}
	return &AIAnalysis{
		SchemaVersion:     "1.0",
		AnalysisID:        newID("ana"),
		RecordID:          rec.ID,
		Model:             ModelInfo{Provider: "go-fallback", Name: "local-mock", Version: "local"},
		ProcessedAt:       time.Now().Format(time.RFC3339),
		TokenUsage:        TokenUsage{ImageCount: len(results)},
		Images:            results,
		RecognizedFields:  []RecognizedField{},
		RecognitionStatus: "retake_required",
		RetakeReason:      "AI服务暂不可用，请重新拍照或转人工填写",
		Warnings:          warnings,
	}
}

func recognitionFailed(analysis *AIAnalysis, rec *Record) (bool, string) {
	if analysis == nil {
		return true, "AI未返回识别结果"
	}
	if analysis.RecognitionStatus == "retake_required" || analysis.RecognitionStatus == "failed" {
		return true, firstNonEmpty(analysis.RetakeReason, "图片无法稳定识别，请重新拍摄")
	}
	if len(analysis.RecognizedFields) == 0 {
		return true, "未识别到日报字段，请重新拍摄"
	}
	for _, image := range analysis.Images {
		if image.Quality.Score < 0.4 || image.Quality.IsBlurry || image.Quality.IsOverExposed {
			return true, "图片质量不足，请重新拍摄"
		}
	}
	requiredAICount := 0
	recognizedRequired := 0
	fieldsByCode := map[string]RecognizedField{}
	for _, field := range analysis.RecognizedFields {
		fieldsByCode[field.Code] = field
	}
	tpl, _ := templateByID(rec.TemplateID)
	for _, field := range tpl.Fields {
		if field.Required && field.Source == "ai" {
			requiredAICount++
			if got := fieldsByCode[field.Code]; strings.TrimSpace(got.Value) != "" {
				recognizedRequired++
			}
		}
	}
	if requiredAICount > 0 && recognizedRequired == 0 {
		return true, "关键字段全部为空，请重新拍摄"
	}
	return false, ""
}

func applyRecognizedFields(rec *Record, recognized []RecognizedField) {
	byCode := map[string]RecognizedField{}
	for _, field := range recognized {
		byCode[field.Code] = field
	}
	for i := range rec.Fields {
		got, ok := byCode[rec.Fields[i].Code]
		if !ok {
			continue
		}
		rec.Fields[i].AIValue = got.Value
		rec.Fields[i].Value = got.Value
		rec.Fields[i].Source = "ai"
		rec.Fields[i].Confidence = got.Confidence
		rec.Fields[i].Reason = got.Reason
		rec.Fields[i].NeedsReview = got.Confidence < 0.82 || rec.Fields[i].Required
		rec.Fields[i].Version++
	}
}

func buildCheckItemsFromFields(fields []FieldValue) []CheckItem {
	items := []CheckItem{}
	for _, field := range fields {
		if !field.Required {
			continue
		}
		status := "normal"
		if field.NeedsReview {
			status = "review"
		}
		if strings.Contains(field.Label, "异常") && strings.TrimSpace(field.Value) != "无" && strings.TrimSpace(field.Value) != "" {
			status = "abnormal"
		}
		items = append(items, CheckItem{
			Code:         field.Code,
			Name:         field.Label,
			JudgeType:    "field",
			Status:       status,
			FinalStatus:  status,
			Basis:        []string{firstNonEmpty(field.Reason, "由日报字段识别结果生成")},
			Confidence:   field.Confidence,
			ReviewStatus: "pending",
			Version:      field.Version,
		})
	}
	return items
}

func buildDailyPreview(rec *Record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【%s】\n", rec.TemplateName)
	fmt.Fprintf(&b, "项目：%s\n点位：%s\n巡检人：%s\n", rec.Project, rec.PointName, rec.Inspector)
	for _, field := range rec.Fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			value = "待填写"
		}
		fmt.Fprintf(&b, "%s：%s\n", field.Label, value)
	}
	if rec.AISummary != "" {
		fmt.Fprintf(&b, "\n【AI总结】%s", rec.AISummary)
	}
	return strings.TrimSpace(b.String())
}

func buildAISummary(rec *Record) string {
	status := inferOverallStatus(rec.Fields)
	key := []string{}
	for _, field := range rec.Fields {
		if strings.TrimSpace(field.Value) == "" {
			continue
		}
		if field.Kind == "number" || strings.Contains(field.Label, "报警") || strings.Contains(field.Label, "异常") {
			key = append(key, field.Label+"="+field.Value)
		}
	}
	if len(key) == 0 {
		key = append(key, "本次巡检字段已由人工确认")
	}
	return fmt.Sprintf("本次%s巡检状态为%s。关键记录：%s。以上总结基于人工确认后的日报字段生成，未覆盖原始字段。", rec.PointName, status, strings.Join(key, "；"))
}

func buildCheckItems(analysis *AIAnalysis) []CheckItem {
	allText := strings.Join(flattenObservations(analysis.Images), "\n")
	findings := map[string]VisualFinding{}
	for _, image := range analysis.Images {
		for _, finding := range image.VisualFindings {
			findings[finding.Code] = finding
		}
	}

	items := []CheckItem{
		checkFromFinding("ENV_WATER", "地面积水检查", "vision", findings["NO_VISIBLE_WATER"], "未见明显积水", allText),
		checkFromFinding("ENV_CLUTTER", "通道杂物检查", "vision", findings["NO_VISIBLE_CLUTTER"], "未见明显杂物堆放", allText),
	}

	items = append(items, CheckItem{
		Code:         "IMAGE_QUALITY",
		Name:         "图片质量复核",
		JudgeType:    "hybrid",
		Status:       imageQualityStatus(analysis.Images),
		FinalStatus:  imageQualityStatus(analysis.Images),
		Basis:        []string{"根据图片清晰度、曝光和模型置信度生成建议"},
		Confidence:   minConfidence(analysis.Images),
		ReviewStatus: "pending",
		Version:      1,
	})

	items = append(items, CheckItem{
		Code:         "MANUAL_CONFIRM",
		Name:         "人工确认结论",
		JudgeType:    "manual_only",
		Status:       "review",
		FinalStatus:  "review",
		Basis:        []string{"第一版要求人工确认作为最终结果来源"},
		Confidence:   1,
		ReviewStatus: "pending",
		Version:      1,
	})

	return items
}

func checkFromFinding(code, name, judgeType string, finding VisualFinding, normalBasis, allText string) CheckItem {
	status := "review"
	basis := []string{"未能形成稳定视觉证据，建议人工复核"}
	confidence := 0.58

	if finding.Code != "" {
		status = "normal"
		basis = []string{finding.Label}
		confidence = finding.Confidence
	} else if strings.Contains(allText, strings.TrimPrefix(normalBasis, "未见明显")) {
		status = "review"
		basis = []string{"图片事实描述中存在相关线索，但缺少结构化证据"}
		confidence = 0.62
	}

	return CheckItem{
		Code:         code,
		Name:         name,
		JudgeType:    judgeType,
		Status:       status,
		FinalStatus:  status,
		Basis:        basis,
		Confidence:   confidence,
		ReviewStatus: "pending",
		Version:      1,
	}
}

func buildReport(rec *Record, analysis *AIAnalysis, items []CheckItem) string {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.FinalStatus]++
	}
	return fmt.Sprintf(
		"本次巡检项目为%s，点位为%s，巡检类型为%s。系统共分析%d张图片，基于AI事实提取和后端规则引擎生成检查项建议：正常%d项，异常%d项，待复核%d项。最终结论以巡检人员人工确认结果为准。",
		rec.Project,
		rec.PointName,
		rec.Type,
		len(analysis.Images),
		counts["normal"],
		counts["abnormal"],
		counts["review"]+counts["unknown"],
	)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimPrefix(r.URL.Path, "/api/ai/tasks/")
	task, ok := s.task(taskID)
	if !ok {
		writeError(w, http.StatusNotFound, "task_not_found", "分析任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) getLatestTask(w http.ResponseWriter, r *http.Request, recordID string) {
	rec, ok := s.record(recordID)
	if !ok {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	if rec.TaskID == "" {
		writeError(w, http.StatusNotFound, "task_not_found", "当前记录尚未发起分析")
		return
	}
	task, ok := s.task(rec.TaskID)
	if !ok {
		writeError(w, http.StatusNotFound, "task_not_found", "分析任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (s *Server) patchCheckItem(w http.ResponseWriter, r *http.Request, recordID, code string) {
	var req struct {
		FinalStatus string `json:"finalStatus"`
		Version     int    `json:"version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if !validStatus(req.FinalStatus) {
		writeError(w, http.StatusBadRequest, "bad_status", "状态必须为 normal、abnormal、review 或 unknown")
		return
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	rec := s.store.records[recordID]
	if rec == nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	for i := range rec.CheckItems {
		if rec.CheckItems[i].Code == code {
			if req.Version != 0 && req.Version != rec.CheckItems[i].Version {
				writeError(w, http.StatusConflict, "version_conflict", "检查项已被更新，请刷新后重试")
				return
			}
			rec.CheckItems[i].FinalStatus = req.FinalStatus
			rec.CheckItems[i].ReviewStatus = "reviewed_change"
			rec.CheckItems[i].Version++
			rec.Report = buildReport(rec, latestAnalysis(s.store.tasks, rec.TaskID), rec.CheckItems)
			writeJSON(w, http.StatusOK, rec.CheckItems[i])
			return
		}
	}
	writeError(w, http.StatusNotFound, "check_item_not_found", "检查项不存在")
}

func (s *Server) patchField(w http.ResponseWriter, r *http.Request, recordID, code string) {
	var req struct {
		Value   string `json:"value"`
		Version int    `json:"version"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	rec := s.store.records[recordID]
	if rec == nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	for i := range rec.Fields {
		if rec.Fields[i].Code == code {
			if req.Version != 0 && req.Version != rec.Fields[i].Version {
				writeError(w, http.StatusConflict, "version_conflict", "字段已被更新，请刷新后重试")
				return
			}
			rec.Fields[i].Value = req.Value
			rec.Fields[i].Source = "human"
			rec.Fields[i].NeedsReview = false
			rec.Fields[i].Version++
			rec.CheckItems = buildCheckItemsFromFields(rec.Fields)
			rec.Report = buildDailyPreview(rec)
			writeJSON(w, http.StatusOK, rec.Fields[i])
			return
		}
	}
	writeError(w, http.StatusNotFound, "field_not_found", "日报字段不存在")
}

func (s *Server) enableManualMode(w http.ResponseWriter, r *http.Request, recordID string) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	rec := s.store.records[recordID]
	if rec == nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	rec.ManualRequired = true
	rec.RecognitionStatus = "manual_required"
	rec.RetakeReason = "已切换为人工填写"
	rec.Report = buildDailyPreview(rec)
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) submitRecord(w http.ResponseWriter, r *http.Request, recordID string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key", "提交记录需要 Idempotency-Key")
		return
	}
	now := time.Now()

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	rec := s.store.records[recordID]
	if rec == nil {
		writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
		return
	}
	rec.AISummary = buildAISummary(rec)
	rec.Report = buildDailyPreview(rec)
	rec.Submitted = true
	rec.SubmittedAt = &now
	s.upsertAssetLocked(rec, now)
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) upsertAssetLocked(rec *Record, now time.Time) {
	tpl, _ := templateByID(rec.TemplateID)
	assetName := firstNonEmpty(fieldValue(rec.Fields, "asset_no"), fieldValue(rec.Fields, "site"), rec.PointName)
	entry := AssetEntry{
		ID:           rec.TemplateID + "_" + sanitizeAssetName(assetName),
		AssetType:    tpl.AssetType,
		AssetName:    assetName,
		Project:      rec.Project,
		LastRecordID: rec.ID,
		Status:       inferOverallStatus(rec.Fields),
		Summary:      rec.AISummary,
		UpdatedAt:    now.Format(time.RFC3339),
	}
	for i := range s.store.assets {
		if s.store.assets[i].ID == entry.ID {
			s.store.assets[i] = entry
			return
		}
	}
	s.store.assets = append(s.store.assets, entry)
}

func (s *Server) findPoint(id string) (Point, bool) {
	for _, point := range s.store.points {
		if point.ID == id {
			return point, true
		}
	}
	return Point{}, false
}

func (s *Server) record(id string) (*Record, bool) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	rec := s.store.records[id]
	return rec, rec != nil
}

func (s *Server) task(id string) (*AITask, bool) {
	s.store.mu.RLock()
	defer s.store.mu.RUnlock()
	task := s.store.tasks[id]
	return task, task != nil
}

func (s *Server) updateTask(id string, update func(*AITask)) {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if task := s.store.tasks[id]; task != nil {
		update(task)
	}
}

func (s *Server) failTask(id, code, message string) {
	s.updateTask(id, func(task *AITask) {
		task.Status = "failed"
		task.ErrorCode = code
		task.Error = message
		task.UpdatedAt = time.Now()
	})
}

func latestAnalysis(tasks map[string]*AITask, taskID string) *AIAnalysis {
	if task := tasks[taskID]; task != nil && task.Analysis != nil {
		return task.Analysis
	}
	return &AIAnalysis{Images: []AIImageResult{}}
}

func imageQualityStatus(images []AIImageResult) string {
	if minConfidence(images) < 0.65 {
		return "review"
	}
	return "normal"
}

func minConfidence(images []AIImageResult) float64 {
	if len(images) == 0 {
		return 0
	}
	min := 1.0
	for _, image := range images {
		if image.Confidence < min {
			min = image.Confidence
		}
	}
	return min
}

func flattenObservations(images []AIImageResult) []string {
	var out []string
	for _, image := range images {
		out = append(out, image.Observations...)
	}
	return out
}

func validStatus(status string) bool {
	switch status {
	case "normal", "abnormal", "review", "unknown":
		return true
	default:
		return false
	}
}

func decodeJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

func newID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_")
	return replacer.Replace(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func fieldValue(fields []FieldValue, code string) string {
	for _, field := range fields {
		if field.Code == code {
			return strings.TrimSpace(field.Value)
		}
	}
	return ""
}

func sanitizeAssetName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "：", "_")
	return replacer.Replace(name)
}

func mustTemplate(id string) ReportTemplate {
	tpl, ok := templateByID(id)
	if ok {
		return tpl
	}
	templates := reportTemplates()
	if len(templates) > 0 {
		return templates[0]
	}
	return ReportTemplate{}
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
