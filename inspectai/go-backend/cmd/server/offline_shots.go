package main

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ===== 离线照片 =====
//
// 弱网现场先存手机、联网后上传的照片。移动端每张带一个幂等键,
// "其实传成功了但响应没回来"的重放不会产生重复行。

// OfflineShot 一张已上传的离线照片。
type OfflineShot struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenantId,omitempty"`
	UserID    string `json:"userId,omitempty"`
	Inspector string `json:"inspector,omitempty"`
	// IdempotencyKey 客户端生成,唯一约束保证重放不产生重复
	IdempotencyKey string `json:"-"`
	ImagePath      string `json:"-"`
	FileName       string `json:"fileName"`
	SizeBytes      int64  `json:"sizeBytes"`
	// CapturedAt 手机声称的拍摄时间(可伪造,仅供参考)
	CapturedAt string `json:"capturedAt"`
	// ReceivedAt 服务器收到时间(权威)。与 CapturedAt 的差 = 离线时长,公开展示
	ReceivedAt string   `json:"receivedAt"`
	Lat        *float64 `json:"lat,omitempty"`
	Lng        *float64 `json:"lng,omitempty"`
	Accuracy   *float64 `json:"accuracy,omitempty"`
	// AssetID 这张照片拍的是哪台设备(扫码时带上)。空 = 手动路径拍的,没指定。
	//
	// 【为什么记在照片上而不是成单时再定】照片原来是"无主"的,归属由成单那一刻的
	// 扫码上下文决定 —— 一次巡多台就会串:扫 A 拍几张、走到 B 扫 B 再拍几张,
	// 进"选照片"时全混在一起,而上下文是 B,全选就全落到 B 上,A 等于没巡。
	AssetID string `json:"assetId,omitempty"`
	// RecordID 归档到某条巡检记录后回填;空 = 尚未成单
	RecordID string `json:"recordId,omitempty"`
	Status   string `json:"status"`
}

// OfflineShotStore — 离线照片
type OfflineShotStore interface {
	// CreateOfflineShot 幂等写入:同一 idempotencyKey 重复提交返回既有行,
	// 第二个返回值表示本次是否命中了已存在的记录(重放)。
	CreateOfflineShot(shot *OfflineShot) (*OfflineShot, bool, error)
	ListOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error)
	// ListPendingOfflineShots 只列未成单的。
	//
	// 【必须在 SQL 里过滤,不能取回来再筛】ListOfflineShots 的 limit 作用在
	// 【全部】照片上(而且 limit<=0 会被当成默认 100),先截断再筛的话,
	// 最新 100 条里成单的多,未成单的就被截没了 —— 实测过:页面显示 20 张,
	// 按这个口径只数出 6 张。
	ListPendingOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error)
	// CountPendingOfflineShots 未成单照片数。走 COUNT(*),不把行拉回来 ——
	// 底栏角标只要一个数字。
	CountPendingOfflineShots(tenantID, userID string) (int, error)
	// OfflineShotsByIDs 按 ID 精确取照片。
	//
	// 【为什么不能"先列一批再筛"】原来的做法是 ListOfflineShots(…, 500) 拉最近
	// 500 张建索引再查 —— 想要的照片只要不在这 500 张里就【静默丢掉】:
	// 巡检员选了 13 张,成单后记录里只有 9 张,不报错、没日志。
	// 照片是巡检的证据,少一张都不能靠"大概率在窗口里"来保证。
	OfflineShotsByIDs(tenantID string, ids []string) ([]*OfflineShot, error)
	// MarkOfflineShotConsumed 标记照片已并入某条巡检记录,避免重复成单
	MarkOfflineShotConsumed(tenantID, id, recordID string) error
	// DeleteOfflineShots 批量删除未成单的照片,返回被删掉的行(供调用方清理磁盘文件)。
	// 已并入记录的(record_id 非空)不删 —— 那是巡检记录的证据,不能从这里抹掉。
	DeleteOfflineShots(tenantID string, ids []string) ([]*OfflineShot, error)
}

const offlineShotCols = `id, tenant_id, user_id, inspector, idempotency_key,
	image_path, file_name, size_bytes, captured_at, received_at,
	lat, lng, accuracy, record_id, status, asset_id`

func scanOfflineShot(row scanner) (*OfflineShot, error) {
	s := &OfflineShot{}
	var lat, lng, acc sql.NullFloat64
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.UserID, &s.Inspector, &s.IdempotencyKey,
		&s.ImagePath, &s.FileName, &s.SizeBytes, &s.CapturedAt, &s.ReceivedAt,
		&lat, &lng, &acc, &s.RecordID, &s.Status, &s.AssetID,
	); err != nil {
		return nil, err
	}
	if lat.Valid {
		s.Lat = &lat.Float64
	}
	if lng.Valid {
		s.Lng = &lng.Float64
	}
	if acc.Valid {
		s.Accuracy = &acc.Float64
	}
	return s, nil
}

func (s *SQLiteStore) CreateOfflineShot(shot *OfflineShot) (*OfflineShot, bool, error) {
	if shot.TenantID == "" {
		shot.TenantID = defaultTenantID
	}
	if shot.ID == "" {
		shot.ID = newID("oshot")
	}
	if shot.Status == "" {
		shot.Status = "uploaded"
	}
	shot.ReceivedAt = nowStamp() // 权威时间由服务端盖,不接受客户端传入

	_, err := s.db.Exec(
		`INSERT INTO offline_shots (`+offlineShotCols+`)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		shot.ID, shot.TenantID, shot.UserID, shot.Inspector, shot.IdempotencyKey,
		shot.ImagePath, shot.FileName, shot.SizeBytes, shot.CapturedAt, shot.ReceivedAt,
		shot.Lat, shot.Lng, shot.Accuracy, shot.RecordID, shot.Status, shot.AssetID,
	)
	if err != nil {
		// 幂等命中:唯一键冲突说明这张已经传过,返回既有行让客户端安心删本地副本
		if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "Duplicate") {
			existing, getErr := s.getOfflineShotByKey(shot.IdempotencyKey)
			if getErr != nil {
				return nil, false, getErr
			}
			return existing, true, nil
		}
		return nil, false, err
	}
	return shot, false, nil
}

func (s *SQLiteStore) getOfflineShotByKey(key string) (*OfflineShot, error) {
	row := s.db.QueryRow(
		`SELECT `+offlineShotCols+` FROM offline_shots WHERE idempotency_key=?`, key)
	return scanOfflineShot(row)
}

func (s *SQLiteStore) MarkOfflineShotConsumed(tenantID, id, recordID string) error {
	_, err := s.db.Exec(
		`UPDATE offline_shots SET record_id=?, status='consumed'
		 WHERE id=? AND tenant_id=?`, recordID, id, tenantID)
	return err
}

func (s *SQLiteStore) DeleteOfflineShots(tenantID string, ids []string) ([]*OfflineShot, error) {
	deleted := make([]*OfflineShot, 0, len(ids))
	for _, id := range ids {
		row := s.db.QueryRow(
			`SELECT `+offlineShotCols+` FROM offline_shots
			 WHERE id=? AND tenant_id=? AND record_id=''`, id, tenantID)
		shot, err := scanOfflineShot(row)
		if err != nil {
			continue // 不存在/跨租户/已成单 → 跳过,不报错也不删
		}
		if _, err := s.db.Exec(
			`DELETE FROM offline_shots WHERE id=? AND tenant_id=? AND record_id=''`,
			id, tenantID); err != nil {
			return deleted, err
		}
		deleted = append(deleted, shot)
	}
	return deleted, nil
}

func (s *SQLiteStore) ListOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
	return s.listShots(tenantID, userID, limit, false)
}

func (s *SQLiteStore) ListPendingOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
	return s.listShots(tenantID, userID, limit, true)
}

func (s *SQLiteStore) listShots(tenantID, userID string, limit int, pendingOnly bool) ([]*OfflineShot, error) {
	if limit <= 0 {
		limit = 100
	}
	// record_id='' 这一条必须进 WHERE,不能取回来再筛 —— LIMIT 是在筛之前
	// 生效的,先截断就会把靠后的未成单照片截没。
	where := ""
	if pendingOnly {
		where = " AND record_id=''"
	}
	rows, err := s.db.Query(
		`SELECT `+offlineShotCols+` FROM offline_shots
		 WHERE tenant_id=? AND (?='' OR user_id=?)`+where+`
		 ORDER BY captured_at DESC LIMIT ?`,
		tenantID, userID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OfflineShot
	for rows.Next() {
		shot, err := scanOfflineShot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, shot)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) OfflineShotsByIDs(tenantID string, ids []string) ([]*OfflineShot, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// 占位符按 ID 个数动态拼。ID 本身仍然走参数绑定,不进 SQL 文本。
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids)+1)
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, tenantID)
	rows, err := s.db.Query(
		`SELECT `+offlineShotCols+` FROM offline_shots
		 WHERE id IN (`+ph+`) AND tenant_id=?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]*OfflineShot, len(ids))
	for rows.Next() {
		shot, err := scanOfflineShot(rows)
		if err != nil {
			return nil, err
		}
		byID[shot.ID] = shot
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 按传入顺序返回 —— 调用方(成单、分类)依赖这个顺序,
	// 数据库不保证 IN 查询的返回次序。
	out := make([]*OfflineShot, 0, len(ids))
	for _, id := range ids {
		if shot, ok := byID[id]; ok {
			out = append(out, shot)
		}
	}
	return out, nil
}

func (s *SQLiteStore) CountPendingOfflineShots(tenantID, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM offline_shots
		 WHERE tenant_id=? AND (?='' OR user_id=?) AND record_id=''`,
		tenantID, userID, userID).Scan(&n)
	return n, err
}

// ===== MemStore 实现 =====

func (m *MemStore) CreateOfflineShot(shot *OfflineShot) (*OfflineShot, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if shot.TenantID == "" {
		shot.TenantID = defaultTenantID
	}
	if shot.ID == "" {
		shot.ID = newID("oshot")
	}
	if shot.Status == "" {
		shot.Status = "uploaded"
	}
	for _, cur := range m.offlineShots {
		if cur.IdempotencyKey == shot.IdempotencyKey {
			return cur, true, nil // 幂等命中
		}
	}
	shot.ReceivedAt = nowStamp()
	m.offlineShots[shot.ID] = shot
	return shot, false, nil
}

func (m *MemStore) MarkOfflineShotConsumed(tenantID, id, recordID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	shot, ok := m.offlineShots[id]
	if !ok || shot.TenantID != tenantID {
		return nil // 跨租户等同不存在
	}
	shot.RecordID = recordID
	shot.Status = "consumed"
	return nil
}

func (m *MemStore) DeleteOfflineShots(tenantID string, ids []string) ([]*OfflineShot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	deleted := make([]*OfflineShot, 0, len(ids))
	for _, id := range ids {
		shot, ok := m.offlineShots[id]
		if !ok || shot.TenantID != tenantID || shot.RecordID != "" {
			continue
		}
		delete(m.offlineShots, id)
		deleted = append(deleted, shot)
	}
	return deleted, nil
}

func (m *MemStore) ListOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
	return m.listShots(tenantID, userID, limit, false)
}

func (m *MemStore) ListPendingOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
	return m.listShots(tenantID, userID, limit, true)
}

func (m *MemStore) listShots(tenantID, userID string, limit int, pendingOnly bool) ([]*OfflineShot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OfflineShot, 0, len(m.offlineShots))
	for _, s := range m.offlineShots {
		if s.TenantID != tenantID {
			continue // 租户隔离
		}
		if userID != "" && s.UserID != userID {
			continue
		}
		// 截断前先筛,和 SQL 版一致 —— 先截后筛会把靠后的未成单照片漏掉
		if pendingOnly && !shotIsPending(s) {
			continue
		}
		out = append(out, s)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemStore) OfflineShotsByIDs(tenantID string, ids []string) ([]*OfflineShot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*OfflineShot, 0, len(ids))
	for _, id := range ids {
		if shot, ok := m.offlineShots[id]; ok && shot != nil && shot.TenantID == tenantID {
			out = append(out, shot)
		}
	}
	return out, nil
}

func (m *MemStore) CountPendingOfflineShots(tenantID, userID string) (int, error) {
	list, err := m.listShots(tenantID, userID, 0, true)
	return len(list), err
}

// ===== HTTP =====

// handleUploadOfflineShot 收一张离线照片。
// 幂等键走 Idempotency-Key 头,与提交流用同一约定。
func (s *Server) handleUploadOfflineShot(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing_idempotency_key",
			"缺少 Idempotency-Key,无法保证重传不产生重复")
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		writeError(w, http.StatusBadRequest, "no_files", "没有收到照片")
		return
	}

	tenantID := s.tenantForRequest(r)
	// 按租户分目录:多客户后互不混放,也便于按租户导出/清理
	dir := filepath.Join(s.storageDir, "offline", sanitizeAssetIdent(tenantID))
	img, err := saveMultipartFile(dir, headers[0], 15<<20)
	if err != nil {
		writeError(w, http.StatusBadRequest, "save_failed", err.Error())
		return
	}

	shot := &OfflineShot{
		TenantID:       tenantID,
		Inspector:      s.currentUserName(r),
		IdempotencyKey: key,
		ImagePath:      img.Path,
		FileName:       img.FileName,
		SizeBytes:      img.Size,
		CapturedAt:     normalizeCapturedAt(r.FormValue("capturedAt")),
		Lat:            parseFloatPtr(r.FormValue("lat")),
		Lng:            parseFloatPtr(r.FormValue("lng")),
		Accuracy:       parseFloatPtr(r.FormValue("accuracy")),
		// 扫码拍的会带上设备 id —— 让照片自己记住拍的是哪台,
		// 而不是到成单那一步再靠"当前上下文"猜(一次巡多台会串)。
		AssetID: strings.TrimSpace(r.FormValue("assetId")),
	}
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		shot.UserID = user.ID
	}

	saved, replayed, err := s.store.CreateOfflineShot(shot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      saved.ID,
		"imageId": saved.ID,
		"shot":    saved,
		// replayed=true 表示这张之前已成功入库,客户端同样可以放心删本地副本
		"replayed": replayed,
	})
}

// handleListOfflineShots 列出本人(管理角色则本租户全部)已上传的离线照片。
// shotIsPending 未成单的照片才算"待处理"。
//
// 移动端一直在【客户端】做这个过滤(listOfflineShots 里的 filter(!s.recordId)),
// 也就是说已成单的照片被完整传了一遍再扔掉 —— 白花的流量。挪到服务端来,
// 顺带保证角标计数和列表页口径一致:两边都用这一个判断。
func shotIsPending(s *OfflineShot) bool {
	return s != nil && strings.TrimSpace(s.RecordID) == ""
}

func (s *Server) handleListOfflineShots(w http.ResponseWriter, r *http.Request) {
	userID := ""
	// 数据范围:看不了全部的人只列自己的照片
	if !s.canSeeAllData(r) {
		if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
			userID = user.ID // 巡检员只看自己的
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	// 默认保持原样(admin-web 要看全量);移动端传 ?pending=1 只要未成单的。
	// 过滤走 SQL,不是取回来再筛 —— LIMIT 在筛之前生效,先截断会漏。
	tenantID := s.tenantForRequest(r)
	var shots []*OfflineShot
	var err error
	if r.URL.Query().Get("pending") == "1" {
		shots, err = s.store.ListPendingOfflineShots(tenantID, userID, limit)
	} else {
		shots, err = s.store.ListOfflineShots(tenantID, userID, limit)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shots": shots})
}

// handleOfflineShotImage 提供离线照片原图。
// 路径:/api/inspection/offline-shots/{id}/image
// 只允许读本租户的照片 —— 跨租户直接 404,不泄露存在性。
func (s *Server) handleOfflineShotImage(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/inspection/offline-shots/")
	id := strings.TrimSuffix(rest, "/image")
	if id == "" || id == rest {
		writeError(w, http.StatusNotFound, "not_found", "路径不正确")
		return
	}
	shots, err := s.shotsByIDs(s.tenantForRequest(r), []string{id})
	if err != nil || len(shots) == 0 {
		writeError(w, http.StatusNotFound, "shot_not_found", "照片不存在")
		return
	}
	// 防路径穿越:只允许 storage 子树内的文件
	clean := filepath.Clean(shots[0].ImagePath)
	if !strings.HasPrefix(clean, filepath.Clean(s.storageDir)) {
		writeError(w, http.StatusForbidden, "forbidden", "非法路径")
		return
	}
	// 走统一图片出口:带缓存头,支持 ?w= 出缩略图(见 image_thumb.go)
	s.serveImage(w, r, clean)
}

// handleClassifyOfflineShots 用已上传的离线照片做场景识别。
// 照片已在服务器上,不重传 —— 弱网现场刚传完就再传一遍是浪费。
func (s *Server) handleClassifyOfflineShots(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShotIDs []string `json:"shotIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(req.ShotIDs) == 0 {
		writeError(w, http.StatusBadRequest, "no_shots", "请先选择照片")
		return
	}

	tenantID := s.tenantForRequest(r)
	shots, err := s.shotsByIDs(tenantID, req.ShotIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "load_failed", err.Error())
		return
	}
	if len(shots) == 0 {
		writeError(w, http.StatusNotFound, "shots_not_found", "照片不存在或无权访问")
		return
	}

	// 前 6 张送分类:机房照可能排在第 4-6 张,只看前 3 张会漏判
	paths := make([]string, 0, 6)
	for _, shot := range shots {
		if len(paths) >= 6 {
			break
		}
		paths = append(paths, shot.ImagePath)
	}

	result, err := s.aiClient.Classify(paths)
	if err != nil {
		// 识别失败不阻断流程:转人工选模板,照片仍在服务器上不会丢
		writeJSON(w, http.StatusOK, map[string]any{
			"classify": &SceneClassifyResult{
				TemplateID: "unknown", TemplateName: "无法识别",
				NeedsManualPick: true, Error: truncate(err.Error(), 120),
			},
			"shots": shots,
		})
		return
	}
	if tpl, ok := templateByID(result.TemplateID); ok {
		result.TemplateName = tpl.Name
	} else {
		result.TemplateName = "无法识别"
		result.NeedsManualPick = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"classify": result, "shots": shots})
}

// shotsByIDs 按 ID 取本租户的离线照片,顺序与传入 ID 一致。
// 跨租户 ID 直接被过滤掉(等同不存在),不泄露存在性。
func (s *Server) shotsByIDs(tenantID string, ids []string) ([]*OfflineShot, error) {
	// 【曾经是"先拉最近 500 张再从里面找"】那样一来,选中的照片只要不在
	// 这 500 张里就被静默丢掉 —— 巡检员选了 13 张,成单后记录里只剩 9 张,
	// 不报错、不留日志。照片是巡检的证据,不能靠"大概率在窗口里"来保证。
	// 现在按 ID 直接查库,有多少张就是多少张。
	return s.store.OfflineShotsByIDs(tenantID, ids)
}

// adoptOfflineShots 把离线照片并入某条巡检记录:复制进记录目录,并回填 record_id
// 标记已消费(避免同一张照片被重复成单)。
func (s *Server) adoptOfflineShots(recordID, tenantID string, ids []string) ([]ImageInfo, error) {
	shots, err := s.shotsByIDs(tenantID, ids)
	if err != nil {
		return nil, err
	}
	dstDir := filepath.Join(s.storageDir, "uploads", recordID)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return nil, err
	}

	out := make([]ImageInfo, 0, len(shots))
	for _, shot := range shots {
		if shot.RecordID != "" {
			continue // 已成单,跳过防重复
		}
		data, err := os.ReadFile(shot.ImagePath)
		if err != nil {
			continue // 单张读失败不该毁掉整条记录
		}
		imageID := newID("img")
		dst := filepath.Join(dstDir, imageID+"_"+sanitizeFileName(shot.FileName))
		if err := os.WriteFile(dst, data, 0644); err != nil {
			continue
		}
		out = append(out, ImageInfo{
			ID: imageID, FileName: shot.FileName, Path: dst,
			Size: shot.SizeBytes, CreatedAt: time.Now(),
		})
		_ = s.store.MarkOfflineShotConsumed(tenantID, shot.ID, recordID)
	}
	return out, nil
}

// handleDeleteOfflineShots 批量删除未成单的离线照片。
// 已并入巡检记录的照片不在此删除 —— 那是记录的证据,要删得走记录本身。
func (s *Server) handleDeleteOfflineShots(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShotIDs []string `json:"shotIds"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(req.ShotIDs) == 0 {
		writeError(w, http.StatusBadRequest, "no_shots", "请先选择要删除的照片")
		return
	}
	tenantID := s.tenantForRequest(r)
	deleted, err := s.store.DeleteOfflineShots(tenantID, req.ShotIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	// 磁盘文件跟着删,避免留下没人引用的孤儿文件占空间。
	// 删文件失败不影响结果:库里已经没有了,文件顶多是垃圾。
	for _, shot := range deleted {
		clean := filepath.Clean(shot.ImagePath)
		if strings.HasPrefix(clean, filepath.Clean(s.storageDir)) {
			_ = os.Remove(clean)
		}
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     "offline_shot.delete",
		TargetType: "offline_shot",
		Detail:     map[string]any{"count": len(deleted)},
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": len(deleted)})
}

// normalizeCapturedAt 客户端时间只做格式校验,不信任其准确性。
// 解析不了就落服务器当前时间,避免脏数据破坏按时间排序。
func normalizeCapturedAt(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nowStamp()
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nowStamp()
	}
	return fmtStamp(t.In(cnLoc))
}

func parseFloatPtr(raw string) *float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &v
}

var _ = errors.New // 保留 import 以备扩展
