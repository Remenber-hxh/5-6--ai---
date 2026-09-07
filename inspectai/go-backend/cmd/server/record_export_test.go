package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// exportCSV 发一次导出请求,把 CSV 解析成表头 + 数据行。
func exportCSV(t *testing.T, server *Server, token, query string) ([]string, [][]string) {
	t.Helper()
	res := requestWithToken(server, http.MethodGet, "/api/inspection/records/export"+query, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET export%s: code=%d body=%s", query, res.Code, res.Body.String())
	}
	body := res.Body.Bytes()
	// 【BOM 必须在】没有它 Excel 双击打开中文列头就是乱码,
	// 而人只会以为"导出功能坏了"。
	if !bytes.HasPrefix(body, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("CSV 开头缺 UTF-8 BOM —— Excel 打开会乱码")
	}
	rows, err := csv.NewReader(bytes.NewReader(body[3:])).ReadAll()
	if err != nil {
		t.Fatalf("解析 CSV: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("CSV 里连表头都没有")
	}
	return rows[0], rows[1:]
}

func colIndex(t *testing.T, headers []string, name string) int {
	t.Helper()
	for i, h := range headers {
		if h == name {
			return i
		}
	}
	t.Fatalf("表头里没有「%s」列(实际:%v)", name, headers)
	return -1
}

// 造一批记录,全部同一个模板。
func seedExportRecords(t *testing.T, server *Server, n int) {
	t.Helper()
	now := time.Now().In(cnLoc)
	for i := 0; i < n; i++ {
		rec := &Record{
			ID:                fmt.Sprintf("rec_ex_%04d", i),
			RecordNo:          fmt.Sprintf("ZX-EX-%04d", i),
			Project:           "会议中心",
			PointName:         "1号梯 机房",
			Inspector:         "巡检员A",
			InspectorUserID:   "user_a",
			TemplateID:        "zihan_energy",
			TemplateName:      "能耗抄表",
			RecognitionStatus: "recognized",
			Fields:            []FieldValue{{Code: "site", Label: "巡检地点", Value: "正常"}},
			CreatedAt:         now.Add(-time.Duration(i) * time.Minute),
		}
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord: %v", err)
		}
	}
}

// 【这是这个接口存在的全部理由】原来导出的是页面手里那个数组,而它有条数
// 上限 —— 导出的 CSV 也就只有那么多,【文件里没有任何地方说它不完整】。
// 所以这里造的记录必须远超那个上限,否则测试通过说明不了问题。
func TestExportNotCappedByListLimit(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	const n = 600 // > recordListMaxLimit(500)
	seedExportRecords(t, server, n)

	_, rows := exportCSV(t, server, tokens["admin"], "")
	if len(rows) < n {
		t.Errorf("导出应至少包含 %d 行,实际 %d —— 说明导出仍然受列表上限影响", n, len(rows))
	}
}

// 单模板时字段摊成列;多模板时退回单格。
//
// 挤在一格的"字段明细"在 Excel 里筛不了也排不了序,一张几百行的表
// 只能证明"巡过了",没法用来发现问题。
func TestExportExpandsFieldsForSingleTemplate(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	seedExportRecords(t, server, 3)

	// 夹具自带 3 条 zihan_energy 的记录,所以此时仍是单模板
	headers, _ := exportCSV(t, server, tokens["admin"], "")
	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Skip("默认模板里没有 zihan_energy,跳过")
	}
	if len(tpl.Fields) == 0 {
		t.Skip("模板没有字段,摊不开")
	}
	if idx := indexOf(headers, "字段明细"); idx >= 0 {
		t.Errorf("只有一个模板时应把字段摊成列,实际还是挤在「字段明细」一格里(表头:%v)", headers)
	}
	// 模板的每个字段都应该有自己的列
	for _, f := range tpl.Fields {
		want := firstNonEmpty(f.Label, f.Code)
		if indexOf(headers, want) < 0 {
			t.Errorf("表头里缺字段列「%s」(实际:%v)", want, headers)
		}
	}

	// 掺一条别的模板的记录进来 → 退回单格
	other := &Record{
		ID: "rec_other", Project: "会议中心", Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_daily", TemplateName: "综合巡检",
		RecognitionStatus: "recognized", CreatedAt: time.Now().In(cnLoc),
	}
	if err := server.store.CreateRecord(other); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	headers2, _ := exportCSV(t, server, tokens["admin"], "")
	if indexOf(headers2, "字段明细") < 0 {
		t.Errorf("混着多个模板时应退回「字段明细」单格(摊开会变成上百列的稀疏表),实际表头:%v", headers2)
	}
}

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// 【筛选必须和页面一致】页面上筛完再点导出、导出的却是全部 ——
// 那比少给数据更糟:人拿到的是一份他以为已经筛过的表。
func TestExportRespectsFilters(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	now := time.Now().In(cnLoc)
	mk := func(id, proj, tplName, point string) {
		rec := &Record{
			ID: id, RecordNo: id, Project: proj, PointName: point,
			Inspector: "巡检员A", InspectorUserID: "user_a",
			TemplateID: "zihan_energy", TemplateName: tplName,
			RecognitionStatus: "recognized",
			Fields:            []FieldValue{{Code: "site", Label: "巡检地点", Value: "正常"}},
			CreatedAt:         now,
		}
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord(%s): %v", id, err)
		}
	}
	mk("f1", "会议中心", "能耗抄表", "A 点位")
	mk("f2", "会议中心", "能耗抄表", "B 点位")
	mk("f3", "紫菡雅集", "能耗抄表", "C 点位")

	base := len(mustRows(t, server, tokens["admin"], ""))

	byProject := mustRows(t, server, tokens["admin"], "?project=%E7%B4%AB%E8%8F%A1%E9%9B%85%E9%9B%86")
	if len(byProject) != 1 {
		t.Errorf("按项目筛选应得 1 行,实际 %d(不筛是 %d 行)", len(byProject), base)
	}
	byKeyword := mustRows(t, server, tokens["admin"], "?keyword=B+%E7%82%B9%E4%BD%8D")
	if len(byKeyword) != 1 {
		t.Errorf("按关键词筛选应得 1 行,实际 %d", len(byKeyword))
	}
}

func mustRows(t *testing.T, server *Server, token, query string) [][]string {
	t.Helper()
	_, rows := exportCSV(t, server, token, query)
	return rows
}

// 「不合格项」这一列:管理者打开表格第一件事就是找哪几项出了问题。
func TestExportListsNonconformingFields(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	rec := &Record{
		ID: "rec_bad", RecordNo: "ZX-BAD", Project: "会议中心",
		Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表",
		RecognitionStatus: "recognized",
		// 【必须用模板里真实存在的 code】导出会剔掉模板外的字段(和页面
		// 同口径)。用假 code 的话这两个字段会被静默丢掉,「不合格项」
		// 恒为空,而测试看上去还是"跑通了"。
		Fields: []FieldValue{
			{Code: "site", Label: "巡检地点", Value: "正常"},
			{Code: "z1_reading", Label: "Z1 能耗表读数", Value: "表面破损"},
		},
		CreatedAt: time.Now().In(cnLoc),
	}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	headers, rows := exportCSV(t, server, tokens["admin"], "?keyword=ZX-BAD")
	if len(rows) != 1 {
		t.Fatalf("应只导出那一条,实际 %d 行", len(rows))
	}
	got := rows[0][colIndex(t, headers, "不合格项")]
	if !strings.Contains(got, "Z1") {
		t.Errorf("「不合格项」应列出命中异常词的字段名,实际 %q", got)
	}
	if strings.Contains(got, "巡检地点") {
		t.Errorf("值正常的字段不该出现在「不合格项」里,实际 %q", got)
	}
}

// 【导出的范围必须等于这个人本来就能看见的范围】换了个出口就放宽,
// 等于给了一条绕过数据范围的路 —— 而且不会报错,只会多出几百行别人的记录。
func TestExportRespectsDataScope(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	now := time.Now().In(cnLoc)
	mine := &Record{
		ID: "own_1", RecordNo: "OWN-1", Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表", CreatedAt: now,
	}
	theirs := &Record{
		ID: "other_1", RecordNo: "OTHER-1", Inspector: "巡检员B", InspectorUserID: "user_b",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表", CreatedAt: now,
	}
	for _, rec := range []*Record{mine, theirs} {
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord: %v", err)
		}
	}
	headers, rows := exportCSV(t, server, tokens["inspector_a"], "")
	noCol := colIndex(t, headers, "记录编号")
	for _, row := range rows {
		if strings.HasPrefix(row[noCol], "OTHER-") {
			t.Fatalf("巡检员 A 导出的文件里出现了巡检员 B 的记录:%s", row[noCol])
		}
	}
	seen := false
	for _, row := range rows {
		if row[noCol] == "OWN-1" {
			seen = true
		}
	}
	if !seen {
		t.Error("巡检员 A 应该能导出自己的记录,实际没有")
	}
}

// 中文文件名要按 RFC 5987 编码下发,否则部分浏览器存出来是乱码文件名。
func TestExportSetsDownloadHeaders(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	res := requestWithToken(server, http.MethodGet, "/api/inspection/records/export", tokens["admin"])
	cd := res.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Errorf("应作为附件下载,实际 Content-Disposition=%q", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("中文文件名要用 RFC 5987 编码,实际 Content-Disposition=%q", cd)
	}
	if ct := res.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type 应是 text/csv,实际 %q", ct)
	}
}
