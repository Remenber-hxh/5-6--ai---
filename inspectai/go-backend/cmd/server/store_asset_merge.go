package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ===== 合并重复登记的设备 =====
//
// 同一台设备在台账里登记了两次(K07 和 K7、Z3 和 Z3能耗表):巡检记录和趋势被拆成两半,
// 一键派单遇到同名还会被拒。合并 = 把 from 名下的一切改挂到 into,再删掉 from。
//
// 【改挂的东西要列全】漏一样,那一样就挂在一台已经不存在的设备上,而且不报错:
//   巡检历史(asset_snapshots)、读数历史(field_observations)、工程任务、
//   计划里的设备清单(asset_ids_json)、修改申请、离线照片。
// 操作日志不动 —— 那是"当时发生了什么"的证据。
//
// 【同一条巡检记录两边都有历史时留 into 的】旧编号、新编号各补过一次的情况,
// 两份其实是同一次巡检,留一份就够。
//
// 【from 最近一次巡检更新的话,把"最近一次"带过去】否则台账卡片上显示的是旧状态。
//
// 不让删掉的 from 在重启回填时被老记录重建出来,靠的是认设备时也认"看着是同一台"的,
// 见 resolveAssetIdentityLoose。

var errMergeSameAsset = errors.New("不能把设备并到它自己")

// assetLastCols 台账卡片上"最近一次巡检"的那几列 —— 合并时整组带过去
const assetLastCols = `last_record_id, last_status, status_level, status_order, last_summary,
	last_inspected_at, last_inspector, last_photo_path`

func (s *SQLiteStore) MergeAsset(tenantID, fromID, intoID string) error {
	if fromID == intoID {
		return errMergeSameAsset
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type lastRow struct {
		recordID, status, level, summary, at, inspector, photo sql.NullString
		order                                                  sql.NullInt64
	}
	readLast := func(id string) (lastRow, error) {
		var r lastRow
		err := tx.QueryRow(`SELECT `+assetLastCols+` FROM assets WHERE id=? AND tenant_id=?`, id, tenantID).
			Scan(&r.recordID, &r.status, &r.level, &r.order, &r.summary, &r.at, &r.inspector, &r.photo)
		return r, err // 跨租户/不存在 → ErrNoRows → 上层 404
	}
	from, err := readLast(fromID)
	if err != nil {
		return err
	}
	into, err := readLast(intoID)
	if err != nil {
		return err
	}

	steps := []struct {
		sql  string
		args []any
	}{
		// 同一条记录两边都有的,先删掉 from 那份(套一层子查询:MySQL 不许在 DELETE 的子查询里直接读同一张表)
		{`DELETE FROM asset_snapshots WHERE asset_id=? AND record_id IN
			(SELECT record_id FROM (SELECT record_id FROM asset_snapshots WHERE asset_id=?) t)`, []any{fromID, intoID}},
		{`UPDATE asset_snapshots SET asset_id=? WHERE asset_id=?`, []any{intoID, fromID}},
		{`DELETE FROM field_observations WHERE asset_id=? AND record_id IN
			(SELECT record_id FROM (SELECT DISTINCT record_id FROM field_observations WHERE asset_id=?) t)`, []any{fromID, intoID}},
		{`UPDATE field_observations SET asset_id=? WHERE asset_id=?`, []any{intoID, fromID}},
		{`UPDATE engineering_tasks SET asset_id=? WHERE asset_id=?`, []any{intoID, fromID}},
		{`UPDATE offline_shots SET asset_id=? WHERE asset_id=?`, []any{intoID, fromID}},
		{`UPDATE change_requests SET target_id=? WHERE target_type='asset' AND target_id=?`, []any{intoID, fromID}},
	}
	for _, st := range steps {
		if _, err := tx.Exec(st.sql, st.args...); err != nil {
			return err
		}
	}

	// 计划里的设备清单是一段 JSON 数组:逐条改写,换掉 from、去掉重复
	needle, _ := json.Marshal(fromID)
	rows, err := tx.Query(`SELECT id, COALESCE(asset_ids_json, '[]') FROM engineering_plan_items WHERE asset_ids_json LIKE ?`,
		"%"+string(needle)+"%")
	if err != nil {
		return err
	}
	type planIDs struct{ id, raw string }
	var plans []planIDs
	for rows.Next() {
		var p planIDs
		if err := rows.Scan(&p.id, &p.raw); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, p)
	}
	rows.Close()
	for _, p := range plans {
		var ids []string
		if json.Unmarshal([]byte(p.raw), &ids) != nil {
			continue
		}
		next, changed := replaceAssetID(ids, fromID, intoID)
		if !changed {
			continue // LIKE 里的 _ 是通配符,可能捞到不相干的计划
		}
		raw, _ := json.Marshal(next)
		if _, err := tx.Exec(`UPDATE engineering_plan_items SET asset_ids_json=? WHERE id=?`, string(raw), p.id); err != nil {
			return err
		}
	}

	// 按时间比,不按字符串比:时区写法不一样("Z" / "+08:00")时字符串比会差出几个小时。
	// 和 scanAsset 同一个解析;into 从没巡检过(空)时 from 只要有就算更新
	fromAt, _ := time.Parse(time.RFC3339Nano, from.at.String)
	intoAt, _ := time.Parse(time.RFC3339Nano, into.at.String)
	if from.at.Valid && fromAt.After(intoAt) {
		if _, err := tx.Exec(`UPDATE assets SET last_record_id=?, last_status=?, status_level=?, status_order=?,
			last_summary=?, last_inspected_at=?, last_inspector=?, last_photo_path=? WHERE id=? AND tenant_id=?`,
			from.recordID, from.status, from.level, from.order, from.summary, from.at, from.inspector, from.photo,
			intoID, tenantID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM assets WHERE id=? AND tenant_id=?`, fromID, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

// replaceAssetID 把清单里的 from 换成 into,去掉换完后的重复。
func replaceAssetID(ids []string, fromID, intoID string) ([]string, bool) {
	changed := false
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == fromID {
			id, changed = intoID, true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, changed
}

func (s *MemStore) MergeAsset(tenantID, fromID, intoID string) error {
	if fromID == intoID {
		return errMergeSameAsset
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	from, ok1 := s.assets[fromID]
	into, ok2 := s.assets[intoID]
	if !ok1 || !ok2 || from.TenantID != tenantID || into.TenantID != tenantID {
		return sql.ErrNoRows
	}
	intoRecords := map[string]bool{}
	for _, sn := range s.assetSnapshots {
		if sn.AssetID == intoID {
			intoRecords[sn.RecordID] = true
		}
	}
	snaps := s.assetSnapshots[:0]
	for _, sn := range s.assetSnapshots {
		if sn.AssetID == fromID {
			if intoRecords[sn.RecordID] {
				continue
			}
			sn.AssetID = intoID
		}
		snaps = append(snaps, sn)
	}
	s.assetSnapshots = snaps
	intoObsRecords := map[string]bool{}
	for _, o := range s.fieldObs {
		if o.AssetID == intoID {
			intoObsRecords[o.RecordID] = true
		}
	}
	obs := s.fieldObs[:0]
	for _, o := range s.fieldObs {
		if o.AssetID == fromID {
			if intoObsRecords[o.RecordID] {
				continue
			}
			o.AssetID = intoID
		}
		obs = append(obs, o)
	}
	s.fieldObs = obs
	for _, t := range s.engTasks {
		if t.AssetID == fromID {
			t.AssetID = intoID
		}
	}
	for _, sh := range s.offlineShots {
		if sh.AssetID == fromID {
			sh.AssetID = intoID
		}
	}
	for _, cr := range s.changeRequests {
		if cr.TargetType == "asset" && cr.TargetID == fromID {
			cr.TargetID = intoID
		}
	}
	for _, p := range s.engPlans {
		if next, changed := replaceAssetID(p.AssetIDs, fromID, intoID); changed {
			p.AssetIDs = next
		}
	}
	if from.LastInspectedAt.After(into.LastInspectedAt) {
		into.LastRecordID, into.LastStatus, into.StatusLevel, into.StatusOrder = from.LastRecordID, from.LastStatus, from.StatusLevel, from.StatusOrder
		into.LastSummary, into.LastInspectedAt, into.LastInspector, into.LastPhotoPath = from.LastSummary, from.LastInspectedAt, from.LastInspector, from.LastPhotoPath
	}
	delete(s.assets, fromID)
	return nil
}
