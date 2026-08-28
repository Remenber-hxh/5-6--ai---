package main

import (
	"database/sql"
	"errors"
	"strings"
)

// ===== 推送的存储层:设置 + 去重流水 =====

// errPushAlreadySent 今天这类推送已经发过了。
//
// 【这是正常路径,不是错误】容器重启、多副本、手动触发都会撞上它。
// 调用方看到它应该安静地跳过,不要写 ERROR 日志 —— 否则日志里全是它,
// 真正的失败反而被淹没。
var errPushAlreadySent = errors.New("push already sent today")

// ListAppSettings 读全部运营参数。
//
// 【这张表没有 tenant_id】迁移 019 建的时候就是全局键值对。目前只有一个租户,
// 而且这些参数(推送时间、开关)本来就是部署级的。真要按租户分,
// 得先加列 —— 那时候这个函数的签名会变,调用方会被编译器逼着改,
// 不会静默串租户。
func (s *SQLiteStore) ListAppSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT k, v FROM app_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *SQLiteStore) SetAppSettings(kv map[string]string, actor string) error {
	if len(kv) == 0 {
		return nil
	}
	now := nowStamp()
	stmt := `INSERT INTO app_settings (k, v, updated_at, updated_by) VALUES (?, ?, ?, ?)
		ON CONFLICT(k) DO UPDATE SET v=excluded.v, updated_at=excluded.updated_at, updated_by=excluded.updated_by`
	if s.dialect == "mysql" {
		stmt = `INSERT INTO app_settings (k, v, updated_at, updated_by) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE v=VALUES(v), updated_at=VALUES(updated_at), updated_by=VALUES(updated_by)`
	}
	for k, v := range kv {
		if _, err := s.db.Exec(stmt, k, v, now, actor); err != nil {
			return err
		}
	}
	return nil
}

// ClaimPushSlot 抢占"今天这类推送"的名额。
//
// 【先占位再发送,不能反过来】"先发再记"的话,发成功但记失败,下一轮会重发;
// 而先占位的话,最坏情况是占了没发出去 —— 那是个可以看见、可以重试的状态,
// 比群里收到两条好得多。
//
// 返回 errPushAlreadySent 表示别人(上一次定时、另一个副本、手动触发)
// 已经占过了 —— 这是正常路径,安静跳过。
func (s *SQLiteStore) ClaimPushSlot(tenantID, kind, day string) (string, error) {
	id := newID("push")
	stmt := `INSERT INTO push_log (id, tenant_id, kind, day, status, detail, created_at)
		VALUES (?, ?, ?, ?, 'sending', '', ?)`
	_, err := s.db.Exec(stmt, id, tenantOrDefault(tenantID), kind, day, nowStamp())
	if err != nil {
		// 唯一键冲突 = 已经发过。两个方言的报错文案不同,只能按关键字认。
		//
		// 【认不出来时当成"已发过"】把未知错误当成"没发过"会导致重发,
		// 而重发正是这张表要防的那件事 —— 宁可漏一次也不要多发一次。
		return "", errPushAlreadySent
	}
	return id, nil
}

// FinishPushSlot 记下这次推送的结果。
//
// status: sent / failed。失败的要留着 —— "发失败"和"没发过"在界面上必须
// 分得清:前者要人去看企微配置,后者只是还没到点。
func (s *SQLiteStore) FinishPushSlot(id, status, detail string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	if len(detail) > 2000 {
		detail = detail[:2000] // 出错信息可能很长,别把一行日志写成一本书
	}
	_, err := s.db.Exec(`UPDATE push_log SET status=?, detail=? WHERE id=?`, status, detail, id)
	return err
}

// LastPushDay 这类推送最近一次是哪天占的位。调度器用它判断"今天发过没有"。
func (s *SQLiteStore) LastPushDay(tenantID, kind string) (string, error) {
	var day string
	err := s.db.QueryRow(
		`SELECT day FROM push_log WHERE tenant_id=? AND kind=? ORDER BY day DESC LIMIT 1`,
		tenantOrDefault(tenantID), kind).Scan(&day)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return day, err
}

// ===== MemStore:测试与回落 =====

func (s *MemStore) ListAppSettings() (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	for k, v := range s.appSettings {
		out[k] = v
	}
	return out, nil
}

func (s *MemStore) SetAppSettings(kv map[string]string, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range kv {
		s.appSettings[k] = v
	}
	return nil
}

func (s *MemStore) ClaimPushSlot(tenantID, kind, day string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantOrDefault(tenantID) + "|" + kind + "|" + day
	if _, ok := s.pushLog[key]; ok {
		return "", errPushAlreadySent
	}
	id := newID("push")
	s.pushLog[key] = id
	return id, nil
}

func (s *MemStore) FinishPushSlot(_, _, _ string) error { return nil }

func (s *MemStore) LastPushDay(tenantID, kind string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	prefix := tenantOrDefault(tenantID) + "|" + kind + "|"
	last := ""
	for k := range s.pushLog {
		if strings.HasPrefix(k, prefix) {
			if d := strings.TrimPrefix(k, prefix); d > last {
				last = d
			}
		}
	}
	return last, nil
}
