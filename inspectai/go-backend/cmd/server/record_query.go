package main

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// ===== 取记录:数据范围 + 筛选条件 =====
//
// 【为什么抽出来】列表和导出问的是同一个问题:"这个人、在这些筛选条件下,
// 能看见哪些记录"。写成两份的话迟早对不上 —— 而对不上的表现是
// 【页面上 12 条、导出的文件里 15 条】,两边都不报错,谁也说不清哪个是对的。
//
// 数据范围判错更糟:它不会报错,只会静默多出几百行别人的记录。

// errRecordScopeUnknown 只能看自己的记录、却认不出是谁。
//
// 【这个分支必须拒绝,不能放行】放行就等于把全租户的记录交出去,
// 而调用方看到的只是"数据比平时多",没有任何异常。
var errRecordScopeUnknown = errors.New("认不出当前用户,而该账号只能查看自己的记录")

// recordFilter 页面上那几个下拉。空 = 不筛。
type recordFilter struct {
	Project  string
	Template string
	Status   string // 业务状态,界面口径(见 record_status.go)
	Keyword  string // 点位 / 编号 / 巡检人
}

func recordFilterFromQuery(r *http.Request) recordFilter {
	q := r.URL.Query()
	return recordFilter{
		Project:  strings.TrimSpace(q.Get("project")),
		Template: strings.TrimSpace(q.Get("template")),
		Status:   strings.TrimSpace(q.Get("status")),
		Keyword:  strings.TrimSpace(q.Get("keyword")),
	}
}

func (f recordFilter) matches(rec *Record) bool {
	if f.Project != "" && rec.Project != f.Project {
		return false
	}
	if f.Template != "" && rec.TemplateName != f.Template {
		return false
	}
	if f.Status != "" && rec.BusinessStatus != f.Status {
		return false
	}
	if f.Keyword != "" && !recordMatchesKeyword(rec, f.Keyword) {
		return false
	}
	return true
}

// selectRecords 取这个请求能看见、且符合筛选条件的全部记录,按时间倒序。
//
// 【不设条数上限,由调用方决定切多少】上限放在这里的话,筛选就变成了
// "从最新 N 条里挑" —— 本项目稍微冷清一点就一条都不剩,而界面显示的是
// "没有结果"。这个坑这个项目已经踩过好几次。
//
// 【性能边界】每次调用会把时间窗口内的记录全读进内存。原因是业务状态
// 要靠扫字段值算出来,SQL 里做不到 —— 想在 SQL 里筛状态就得把它落成列,
// 而派生值落库会和事实不同步。
// 当前量级(单租户几百到几千条)完全够用。【什么时候要改】单租户上万条
// 之后,列表翻页会变慢:那时再拆成"无状态筛选走 SQL LIMIT/OFFSET,
// 带状态筛选才扫全表"两条路。
func (s *Server) selectRecords(r *http.Request, f recordFilter, since time.Time) ([]*Record, error) {
	vis := s.visibilityFor(r)
	if vis.Blocked {
		// 配了项目范围却一个项目都没分到 —— 给空,不给全部
		return nil, nil
	}
	var owner *User
	if vis.OwnOnly {
		u, ok := s.userFromSessionToken(s.tokenFromRequest(r))
		if !ok {
			if s.localNoAuthAllowed(r) {
				// 本地免鉴权:按请求里带的名字认归属,和记录列表原来的做法一致
				owner = &User{DisplayName: userName(r)}
			} else {
				return nil, errRecordScopeUnknown
			}
		} else {
			owner = u
		}
	}
	all, err := s.store.ListRecordsSince(s.tenantForRequest(r), since)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, p := range vis.Projects {
		allowed[p] = true
	}
	out := make([]*Record, 0, len(all))
	for _, rec := range all {
		if rec == nil {
			continue
		}
		if !vis.AllData && len(allowed) > 0 && !allowed[rec.Project] {
			continue
		}
		if owner != nil && !recordOwnedBy(rec, owner.ID, owner.DisplayName, owner.Username) {
			continue
		}
		// 【先 sanitize 再筛】剔掉已从模板删除的字段之后,业务状态才和
		// 页面上看到的一致。不做的话,一个已经删掉的字段还能让这条记录
		// 被筛成「异常」—— 而页面上它显示的是正常。
		clean := sanitizeRecordForCurrentTemplate(rec)
		if !f.matches(clean) {
			continue
		}
		out = append(out, clean)
	}
	return out, nil
}

// findRecordIndex 目标记录排在第几位(按 id 或记录编号找)。找不到返回 -1。
//
// 【为什么后端来找】从台账、审批、AI 洞察点进来带的是某条具体记录。
// 服务端分页之后,它可能在第 7 页 —— 前端手里只有当前这一页,没法自己算。
func findRecordIndex(records []*Record, id, recordNo string) int {
	if id == "" && recordNo == "" {
		return -1
	}
	for i, rec := range records {
		if id != "" && rec.ID == id {
			return i
		}
		if recordNo != "" && rec.RecordNo == recordNo {
			return i
		}
	}
	return -1
}
