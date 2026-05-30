package main

import (
	"database/sql"
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

func (s *SQLiteStore) WriteAssetSnapshots(snaps []*AssetSnapshot, obs []*FieldObservation) error {
	if len(snaps) == 0 && len(obs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	snapSQL := s.insertIgnore() + ` asset_snapshots
		(id, asset_id, record_id, status, status_level, summary, inspector, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	for _, sn := range snaps {
		if sn.ID == "" {
			sn.ID = newID("snap")
		}
		if _, err := tx.Exec(snapSQL,
			sn.ID, sn.AssetID, sn.RecordID, sn.Status, sn.StatusLevel, sn.Summary, sn.Inspector,
			sn.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}

	obsSQL := s.insertIgnore() + ` field_observations
		(id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, o := range obs {
		if o.ID == "" {
			o.ID = newID("obs")
		}
		var num any // nil → NULL（非数值字段）
		if o.ValueNumber != nil {
			num = *o.ValueNumber
		}
		if _, err := tx.Exec(obsSQL,
			o.ID, o.AssetID, o.RecordID, o.FieldKey, o.FieldLabel, o.ValueText, num, o.Source, o.Confidence,
			o.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
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
	if fieldKey == "" {
		rows, err = s.db.Query(`
			SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at
			FROM field_observations WHERE asset_id=?
			ORDER BY created_at ASC LIMIT ?`, assetID, limit)
	} else {
		rows, err = s.db.Query(`
			SELECT id, asset_id, record_id, field_key, field_label, value_text, value_number, source, confidence, created_at
			FROM field_observations WHERE asset_id=? AND field_key=?
			ORDER BY created_at ASC LIMIT ?`, assetID, fieldKey, limit)
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
		e.AIConfidence, e.Action, e.Operator, e.DurationMs, viewed, e.CreatedAt.Format(time.RFC3339Nano))
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
