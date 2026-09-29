package main

import (
	"fmt"
	"sort"
)

// ===== 工作日历的存储层 =====

// migWorkCalendar — 040:工作日历(法定节假日 + 调休上班日)。
//
// 【一天一行,只存特殊的日子】平常的周一到周五、周末都不存 ——
// 一年只有二三十行,没录的日子就按周几算。
// 【不分租户】法定节假日全国统一,见 work_calendar.go。
func (s *SQLiteStore) migWorkCalendar() error {
	stmt := `CREATE TABLE IF NOT EXISTS work_calendar (
		day        TEXT PRIMARY KEY,
		kind       TEXT NOT NULL DEFAULT '',
		name       TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS work_calendar (
			day        VARCHAR(10) NOT NULL PRIMARY KEY,
			kind       VARCHAR(8)  NOT NULL DEFAULT '',
			name       VARCHAR(32) NOT NULL DEFAULT '',
			updated_at VARCHAR(40) NOT NULL DEFAULT '',
			updated_by VARCHAR(64) NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create work_calendar: %w", err)
	}
	return nil
}

// migPlanFollowCalendar — 041:每日计划可以"跳过法定节假日"。
//
// 【默认 0 = 不跳过】存量计划一条都不变,节假日照常。第一版存成
// weekdays="workday" 的,读的时候由 normalizeDayRule 换算,不在这里回填 ——
// 回填和读取各算一遍,规则稍有出入两边就会不一致。
func (s *SQLiteStore) migPlanFollowCalendar() error {
	exists, err := s.hasColumn("engineering_plan_items", "follow_calendar")
	if err != nil {
		return fmt.Errorf("inspect engineering_plan_items.follow_calendar: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(
		`ALTER TABLE engineering_plan_items ADD COLUMN follow_calendar INTEGER NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add engineering_plan_items.follow_calendar: %w", err)
	}
	return nil
}

// ListWorkCalendar from/to 都是 "2006-01-02",含两头。日期按字符串比较就是按时间比较。
func (s *SQLiteStore) ListWorkCalendar(from, to string) ([]WorkCalendarDay, error) {
	rows, err := s.db.Query(
		`SELECT day, kind, name FROM work_calendar WHERE day >= ? AND day <= ? ORDER BY day`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkCalendarDay{}
	for rows.Next() {
		var d WorkCalendarDay
		if err := rows.Scan(&d.Date, &d.Kind, &d.Name); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// SaveWorkCalendar clearFrom 非空 = 先清空这段日子;kind 为空的 = 删掉那一天。
//
// 【一个事务】粘贴整份通知是"清空一年再写 30 行",中途失败留下半年的日历,
// 比原封不动更糟 —— 看起来录过了,其实少了国庆。
func (s *SQLiteStore) SaveWorkCalendar(clearFrom, clearTo string, days []WorkCalendarDay, actor string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if clearFrom != "" {
		if _, err := tx.Exec(`DELETE FROM work_calendar WHERE day >= ? AND day <= ?`, clearFrom, clearTo); err != nil {
			return err
		}
	}
	upsert := `INSERT INTO work_calendar (day, kind, name, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(day) DO UPDATE SET kind=excluded.kind, name=excluded.name, updated_at=excluded.updated_at, updated_by=excluded.updated_by`
	if s.dialect == "mysql" {
		upsert = `INSERT INTO work_calendar (day, kind, name, updated_at, updated_by) VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE kind=VALUES(kind), name=VALUES(name), updated_at=VALUES(updated_at), updated_by=VALUES(updated_by)`
	}
	now := nowStamp()
	for _, d := range days {
		if d.Kind == "" {
			if _, err := tx.Exec(`DELETE FROM work_calendar WHERE day = ?`, d.Date); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(upsert, d.Date, d.Kind, d.Name, now, actor); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ===== MemStore =====

func (s *MemStore) ListWorkCalendar(from, to string) ([]WorkCalendarDay, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []WorkCalendarDay{}
	for day, d := range s.workCalendar {
		if day >= from && day <= to {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (s *MemStore) SaveWorkCalendar(clearFrom, clearTo string, days []WorkCalendarDay, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if clearFrom != "" {
		for day := range s.workCalendar {
			if day >= clearFrom && day <= clearTo {
				delete(s.workCalendar, day)
			}
		}
	}
	for _, d := range days {
		if d.Kind == "" {
			delete(s.workCalendar, d.Date)
			continue
		}
		s.workCalendar[d.Date] = d
	}
	return nil
}
