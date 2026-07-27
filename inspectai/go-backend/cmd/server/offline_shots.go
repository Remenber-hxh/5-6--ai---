package main

import (
	"database/sql"
	"errors"
	"net/http"
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
}

const offlineShotCols = `id, tenant_id, user_id, inspector, idempotency_key,
	image_path, file_name, size_bytes, captured_at, received_at,
	lat, lng, accuracy, record_id, status`

func scanOfflineShot(row scanner) (*OfflineShot, error) {
	s := &OfflineShot{}
	var lat, lng, acc sql.NullFloat64
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.UserID, &s.Inspector, &s.IdempotencyKey,
		&s.ImagePath, &s.FileName, &s.SizeBytes, &s.CapturedAt, &s.ReceivedAt,
		&lat, &lng, &acc, &s.RecordID, &s.Status,
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
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		shot.ID, shot.TenantID, shot.UserID, shot.Inspector, shot.IdempotencyKey,
		shot.ImagePath, shot.FileName, shot.SizeBytes, shot.CapturedAt, shot.ReceivedAt,
		shot.Lat, shot.Lng, shot.Accuracy, shot.RecordID, shot.Status,
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

func (s *SQLiteStore) ListOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		`SELECT `+offlineShotCols+` FROM offline_shots
		 WHERE tenant_id=? AND (?='' OR user_id=?)
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

func (m *MemStore) ListOfflineShots(tenantID, userID string, limit int) ([]*OfflineShot, error) {
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
		out = append(out, s)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
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
func (s *Server) handleListOfflineShots(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if !s.hasSupervisorAccess(r) {
		if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
			userID = user.ID // 巡检员只看自己的
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	shots, err := s.store.ListOfflineShots(s.tenantForRequest(r), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shots": shots})
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
