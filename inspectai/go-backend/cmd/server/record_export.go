package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ===== 巡检记录导出 =====
//
// 【为什么导出必须在后端做】原来是前端把页面手里那个数组写成 CSV。
// 那个数组有条数上限,于是导出的文件也只有那么多 —— 而【文件里没有任何
// 地方写着"这只是最近 500 条"】。页面上还有分页能看出来,文件发出去之后
// 只会被当成全量:贴进汇报、发给甲方、拿去对账。
//
// 这里直接从库里按筛选条件取,不设条数上限。

// exportEpoch 不限时间时的起点。
//
// 用 2000 年而不是零值:created_at 是字符串列,零值格式化出来是空串,
// 而 `created_at >= ”` 会把 created_at 为空的脏数据也捞进来。
// 台账里不可能有 2000 年以前的巡检(这个系统那时候还不存在)。
var exportEpoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// exportBaseHeaders 每次导出都有的列。
//
// 【顺序是按"读的人怎么用"排的,不是按数据结构】打开表格先看时间和点位,
// 再看状态,然后才是细节。记录编号排第二是因为它是对账时的唯一凭据。
var exportBaseHeaders = []string{
	"巡检时间", "记录编号", "所属项目", "巡检点位", "模板", "巡检人",
	"业务状态", "不合格项", "照片张数", "拍照次数",
}

// handleExportRecords —— GET /api/inspection/records/export
//
// 筛选参数和记录页上那几个下拉一一对应:project / template / status / keyword。
// 【必须对应】页面上筛完再点导出,导出的却是全部 —— 那比少给数据更糟:
// 人拿到的是一份他以为已经筛过的表。
func (s *Server) handleExportRecords(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	since := exportEpoch
	if raw := strings.TrimSpace(q.Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			now := time.Now().In(cnLoc)
			since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cnLoc).
				AddDate(0, 0, -(n - 1))
		}
	}

	// 【和列表用同一个取数函数】导出和列表问的是同一个问题。写成两份的话,
	// 页面上 12 条、导出的文件里 15 条,两边都不报错,谁也说不清哪个对。
	records, err := s.selectRecords(r, recordFilterFromQuery(r), since)
	if err != nil {
		if errors.Is(err, errRecordScopeUnknown) {
			writeError(w, http.StatusForbidden, "forbidden", "请使用账号登录后再导出")
			return
		}
		writeError(w, http.StatusInternalServerError, "export_failed", err.Error())
		return
	}

	// 【一份模板就把字段摊成列】挤在一格里的"字段明细"在 Excel 里没法用:
	// 筛不了、排不了序、也读不出哪一项不合格。摊开之后每一列就是一个检查项。
	//
	// 混着多个模板时不摊:十个模板摊出来是上百列的稀疏表,比挤在一格更难看。
	// 这时候提示人先按模板筛一下。
	fieldCodes, fieldLabels := singleTemplateFieldColumns(records)

	headers := append([]string{}, exportBaseHeaders...)
	if len(fieldCodes) > 0 {
		headers = append(headers, fieldLabels...)
	} else {
		headers = append(headers, "字段明细")
	}
	headers = append(headers, "AI 总结")

	filename := fmt.Sprintf("智巡-巡检记录-%s.csv", time.Now().In(cnLoc).Format("2006-01-02"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	// filename* 用 RFC 5987 编码,否则中文文件名在部分浏览器上会变成乱码或被丢弃
	w.Header().Set("Content-Disposition",
		"attachment; filename=records.csv; filename*=UTF-8''"+url.PathEscape(filename))
	// 【BOM 不能少】没有它,Excel 双击打开中文列头就是乱码 ——
	// 而人第一反应是"导出功能坏了",不会想到是编码。
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write(headers)

	for _, rec := range records {
		row := []string{
			rec.CreatedAt.In(cnLoc).Format("2006-01-02 15:04"),
			firstNonEmpty(rec.RecordNo, rec.ID),
			rec.Project,
			rec.PointName,
			rec.TemplateName,
			rec.Inspector,
			rec.BusinessStatus,
			strings.Join(nonconformingFieldLabels(rec), "、"),
			strconv.Itoa(len(rec.Images)),
			strconv.Itoa(rec.CaptureAttempts),
		}
		if len(fieldCodes) > 0 {
			byCode := map[string]string{}
			for _, f := range rec.Fields {
				byCode[f.Code] = f.Value
			}
			for _, code := range fieldCodes {
				row = append(row, byCode[code])
			}
		} else {
			parts := make([]string, 0, len(rec.Fields))
			for _, f := range rec.Fields {
				parts = append(parts, firstNonEmpty(f.Label, f.Code)+"="+f.Value)
			}
			row = append(row, strings.Join(parts, ";"))
		}
		// 【总结不截断】原来切到 200 字。导出是留档用的,截断之后
		// 那份档案永远缺一块,而且看不出来缺了。
		row = append(row, firstNonEmpty(rec.AISummary, rec.Report))
		_ = cw.Write(row)
	}
}

// recordMatchesKeyword 和记录页的关键词筛选同口径:点位 / 编号 / 巡检人。
func recordMatchesKeyword(rec *Record, kw string) bool {
	return strings.Contains(rec.PointName, kw) ||
		strings.Contains(rec.RecordNo, kw) ||
		strings.Contains(rec.Inspector, kw)
}

// nonconformingFieldLabels 值命中异常词的那些字段名。
//
// 【为什么单独给一列】管理者打开表格第一件事是找"哪几项不合格"。
// 没有这一列的话,他得逐条去字段明细里翻 —— 几百行没人翻得完,
// 于是这份表只能用来证明"巡过了",没法用来发现问题。
func nonconformingFieldLabels(rec *Record) []string {
	var out []string
	for _, f := range rec.Fields {
		if abnormalValueRe.MatchString(f.Value) {
			out = append(out, firstNonEmpty(f.Label, f.Code))
		}
	}
	return out
}

// singleTemplateFieldColumns 全都是同一个模板时,给出该模板的字段列。
//
// 返回 (nil, nil) 表示不该摊开:没有记录、或者混着多个模板。
func singleTemplateFieldColumns(records []*Record) (codes, labels []string) {
	tplID := ""
	for _, rec := range records {
		if tplID == "" {
			tplID = rec.TemplateID
			continue
		}
		if rec.TemplateID != tplID {
			return nil, nil // 混着多个模板
		}
	}
	if tplID == "" {
		return nil, nil
	}
	// 【列取自模板定义,不是取自记录】按记录里出现过的字段取的话,
	// 每次导出的列会随这一批数据变 —— 两次导出的表对不上,没法合并。
	tpl, ok := templateByID(tplID)
	if !ok {
		return nil, nil
	}
	for _, f := range tpl.Fields {
		codes = append(codes, f.Code)
		labels = append(labels, firstNonEmpty(f.Label, f.Code))
	}
	return codes, labels
}
