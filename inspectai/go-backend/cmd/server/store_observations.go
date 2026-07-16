package main

import (
	"database/sql"
	"encoding/json"
	"sort"
	"time"
)

// 本文件实现 §3（资产快照 / 字段观测）与 §4（字段确认留痕）的存储读写，
// MemStore（fallback/测试）与 SQLiteStore（SQLite + MySQL 共用）各一套。

// ===== MemStore =====

func (s *MemStore) WriteAssetSnapshots(snaps []*AssetSnapshot, obs []*FieldObservation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sn := range snaps {
		exists := false
		for _, ex := range s.assetSnapshots {
			if ex.AssetID == sn.AssetID && ex.RecordID == sn.RecordID {
				exists = true
				break
			}
		}
		if !exists {
			s.assetSnapshots = append(s.assetSnapshots, sn)
		}
	}
	for _, o := range obs {
		exists := false
		for _, ex := range s.fieldObs {
			if ex.AssetID == o.AssetID && ex.RecordID == o.RecordID && ex.FieldKey == o.FieldKey {
				exists = true
				break
			}
		}
		if !exists {
			s.fieldObs = append(s.fieldObs, o)
		}
	}
	return nil
}

func (s *MemStore) ListAssetSnapshots(assetID string, limit, offset int) ([]*AssetSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*AssetSnapshot{}
	for _, sn := range s.assetSnapshots {
		if sn.AssetID == assetID {
			out = append(out, sn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if offset < 0 {
		offset = 0
	}
	if offset >= len(out) {
		return []*AssetSnapshot{}, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemStore) CountAssetSnapshots(assetID string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, sn := range s.assetSnapshots {
		if sn.AssetID == assetID {
			n++
		}
	}
	return n, nil
}

func (s *MemStore) ListFieldObservations(assetID, fieldKey string, limit int) ([]*FieldObservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*FieldObservation{}
	for _, o := range s.fieldObs {
		if o.AssetID == assetID && (fieldKey == "" || o.FieldKey == fieldKey) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *MemStore) CreateFieldConfirmLog(e *FieldConfirmLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = newID("fcl")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	s.confirmLogs = append(s.confirmLogs, e)
	return nil
}

func (s *MemStore) ListFieldConfirmLogs(recordID string) ([]*FieldConfirmLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*FieldConfirmLog{}
	for _, e := range s.confirmLogs {
		if e.RecordID == recordID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// ===== SQLiteStore（SQLite + MySQL 共用） =====

// insertIgnore 按方言返回幂等插入前缀，配合唯一索引让回填/重复提交不报错。
func (s *SQLiteStore) insertIgnore() string {
	if s.dialect == "mysql" {
		return "INSERT IGNORE INTO"
	}
	return "INSERT OR IGNORE INTO"
}

// writeAssetSnapshotsExec —— 事务内可复用的快照/观测写入,无 Begin/Commit。
// 让 SubmitRecordWithAssets 把日报/资产/快照/观测放进同一个事务,失败整体回滚。
func writeAssetSnapshotsExec(exec sqlExecutor, dialect string, snaps []*AssetSnapshot, obs []*FieldObservation) error {
	if len(snaps) == 0 && len(obs) == 0 {
		return nil
	}
	prefix := "INSERT OR IGNORE INTO"
	if dialect == "mysql" {
		prefix = "INSERT IGNORE INTO"
	}
	snapSQL := prefix + ` asset_snapshots
		(id, asset_id, record_id, status, status_level, summary, inspector, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	for _, sn := range snaps {
		if sn.ID == "" {
			sn.ID = newID("snap")
		}
		if _, err := exec.Exec(snapSQL,
			sn.ID, sn.AssetID, sn.RecordID, sn.Status, sn.StatusLevel, sn.Summary, sn.Inspector,
			fmtStamp(sn.CreatedAt)); err != nil {
			return err
		}
	}
	obsSQL := prefix + ` field_observations
		(id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, o := range obs {
		if o.ID == "" {
			o.ID = newID("obs")
		}
		var num any
		if o.ValueNumber != nil {
			num = *o.ValueNumber
		}
		if _, err := exec.Exec(obsSQL,
			o.ID, o.AssetID, o.RecordID, o.FieldKey, o.FieldLabel, o.ValueText, num, o.Source, o.Confidence,
			fmtStamp(o.CreatedAt)); err != nil {
			return err
		}
	}
	return nil
}

// WriteAssetSnapshots —— 独立调用,用于启动回填等非提交流程。
func (s *SQLiteStore) WriteAssetSnapshots(snaps []*AssetSnapshot, obs []*FieldObservation) error {
	if len(snaps) == 0 && len(obs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := writeAssetSnapshotsExec(tx, s.dialect, snaps, obs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListAssetSnapshots(assetID string, limit, offset int) ([]*AssetSnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.Query(`
		SELECT id, asset_id, record_id, status, status_level, summary, inspector, created_at
		FROM asset_snapshots WHERE asset_id=?
		ORDER BY created_at DESC LIMIT ? OFFSET ?`, assetID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*AssetSnapshot{}
	for rows.Next() {
		sn := &AssetSnapshot{}
		var created string
		if err := rows.Scan(&sn.ID, &sn.AssetID, &sn.RecordID, &sn.Status, &sn.StatusLevel, &sn.Summary, &sn.Inspector, &created); err != nil {
			return nil, err
		}
		sn.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, sn)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CountAssetSnapshots(assetID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM asset_snapshots WHERE asset_id=?`, assetID).Scan(&n)
	return n, err
}

func (s *SQLiteStore) ListFieldObservations(assetID, fieldKey string, limit int) ([]*FieldObservation, error) {
	if limit <= 0 {
		limit = 500
	}
	var (
		rows *sql.Rows
		err  error
	)
	// 子查询先按时间倒序取最近 N 条,外层正序返回给趋势图 ——
	// 直接 ORDER BY ASC LIMIT 会把最早 N 条拿出来,数据涨过 limit 后趋势图永远是历史死数据。
	if fieldKey == "" {
		rows, err = s.db.Query(`
			SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at FROM (
				SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at
				FROM field_observations WHERE asset_id=?
				ORDER BY created_at DESC LIMIT ?
			) t ORDER BY created_at ASC`, assetID, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at FROM (
				SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at
				FROM field_observations WHERE asset_id=? AND field_key=?
				ORDER BY created_at DESC LIMIT ?
			) t ORDER BY created_at ASC`, assetID, fieldKey, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*FieldObservation{}
	for rows.Next() {
		o := &FieldObservation{}
		var created string
		var num sql.NullFloat64
		if err := rows.Scan(&o.ID, &o.AssetID, &o.RecordID, &o.FieldKey, &o.FieldLabel, &o.ValueText, &num, &o.Source, &o.Confidence, &created); err != nil {
			return nil, err
		}
		if num.Valid {
			v := num.Float64
			o.ValueNumber = &v
		}
		o.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) CreateFieldConfirmLog(e *FieldConfirmLog) error {
	if e.ID == "" {
		e.ID = newID("fcl")
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	viewed := 0
	if e.ViewedPhoto {
		viewed = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO field_confirm_logs
		(id, record_id, field_key, field_label, ai_value, original_value, final_value, ai_confidence, action, operator, duration_ms, viewed_photo, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.RecordID, e.FieldKey, e.FieldLabel, e.AIValue, e.OriginalValue, e.FinalValue,
		e.AIConfidence, e.Action, e.Operator, e.DurationMs, viewed, fmtStamp(e.CreatedAt))
	return err
}

func (s *SQLiteStore) ListFieldConfirmLogs(recordID string) ([]*FieldConfirmLog, error) {
	rows, err := s.db.Query(`
		SELECT id, record_id, field_key, field_label, ai_value, original_value, final_value, ai_confidence, action, operator, duration_ms, viewed_photo, created_at
		FROM field_confirm_logs WHERE record_id=? ORDER BY created_at ASC`, recordID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConfirmLogs(rows)
}

// ListRecentFieldConfirmLogs 给巡检员质量榜 / 复核惰性指标用,跨记录查最近 N 条。
func (s *SQLiteStore) ListRecentFieldConfirmLogs(limit int) ([]*FieldConfirmLog, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(`
		SELECT id, record_id, field_key, field_label, ai_value, original_value, final_value, ai_confidence, action, operator, duration_ms, viewed_photo, created_at
		FROM field_confirm_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConfirmLogs(rows)
}

func scanConfirmLogs(rows *sql.Rows) ([]*FieldConfirmLog, error) {
	out := []*FieldConfirmLog{}
	for rows.Next() {
		e := &FieldConfirmLog{}
		var created string
		var viewed int
		if err := rows.Scan(&e.ID, &e.RecordID, &e.FieldKey, &e.FieldLabel, &e.AIValue, &e.OriginalValue, &e.FinalValue, &e.AIConfidence, &e.Action, &e.Operator, &e.DurationMs, &viewed, &created); err != nil {
			return nil, err
		}
		e.ViewedPhoto = viewed != 0
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ===== 管理 AI 报告缓存 =====

func (s *MemStore) SaveManagementAIReport(r *ManagementAIReport) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == "" {
		r.ID = newID("mar")
	}
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now()
	}
	cp := *r
	s.mgmtReports = append(s.mgmtReports, &cp)
	return nil
}

func (s *MemStore) GetLatestManagementAIReport(reportType, project, rangeKey string) (*ManagementAIReport, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var best *ManagementAIReport
	for _, r := range s.mgmtReports {
		if r.ReportType != reportType || r.Project != project || r.RangeKey != rangeKey {
			continue
		}
		if best == nil || r.GeneratedAt.After(best.GeneratedAt) {
			best = r
		}
	}
	if best == nil {
		return nil, sql.ErrNoRows
	}
	cp := *best
	return &cp, nil
}

func (s *MemStore) DeleteExpiredManagementAIReports(now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.mgmtReports[:0]
	dropped := 0
	for _, r := range s.mgmtReports {
		if r.ExpiresAt.Before(now) {
			dropped++
			continue
		}
		kept = append(kept, r)
	}
	s.mgmtReports = kept
	return dropped, nil
}

func (s *MemStore) ListRecentFieldConfirmLogs(limit int) ([]*FieldConfirmLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]*FieldConfirmLog{}, s.confirmLogs...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// JSON 字段序列化的小工具(报告里多列都是 JSON 字符串)
func mustJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s *SQLiteStore) SaveManagementAIReport(r *ManagementAIReport) error {
	if r.ID == "" {
		r.ID = newID("mar")
	}
	if r.GeneratedAt.IsZero() {
		r.GeneratedAt = time.Now()
	}
	_, err := s.db.Exec(`
		INSERT INTO management_ai_reports
		(id, report_type, project, range_key, facts_json, summary, attention_json,
		 recommendations, evidence_json, model, prompt_version, duration_ms,
		 generated_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ReportType, r.Project, r.RangeKey,
		mustJSON(r.Facts), r.Summary, mustJSON(r.Attention),
		mustJSON(r.Recommendations), mustJSON(r.Evidence),
		r.Model, r.PromptVersion, r.DurationMs,
		fmtStamp(r.GeneratedAt),
		fmtStamp(r.ExpiresAt),
	)
	return err
}

func (s *SQLiteStore) GetLatestManagementAIReport(reportType, project, rangeKey string) (*ManagementAIReport, error) {
	row := s.db.QueryRow(`
		SELECT id, report_type, project, range_key, facts_json, summary, attention_json,
		       recommendations, evidence_json, model, prompt_version, duration_ms,
		       generated_at, expires_at
		FROM management_ai_reports
		WHERE report_type=? AND project=? AND range_key=?
		ORDER BY generated_at DESC LIMIT 1`, reportType, project, rangeKey)
	r := &ManagementAIReport{}
	var factsJSON, attentionJSON, recosJSON, evidenceJSON string
	var generatedStr, expiresStr string
	err := row.Scan(
		&r.ID, &r.ReportType, &r.Project, &r.RangeKey,
		&factsJSON, &r.Summary, &attentionJSON,
		&recosJSON, &evidenceJSON,
		&r.Model, &r.PromptVersion, &r.DurationMs,
		&generatedStr, &expiresStr,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(factsJSON), &r.Facts)
	_ = json.Unmarshal([]byte(attentionJSON), &r.Attention)
	_ = json.Unmarshal([]byte(recosJSON), &r.Recommendations)
	_ = json.Unmarshal([]byte(evidenceJSON), &r.Evidence)
	r.GeneratedAt, _ = time.Parse(time.RFC3339Nano, generatedStr)
	r.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresStr)
	return r, nil
}

func (s *SQLiteStore) DeleteExpiredManagementAIReports(now time.Time) (int, error) {
	res, err := s.db.Exec(`DELETE FROM management_ai_reports WHERE expires_at < ?`, fmtStamp(now))
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
