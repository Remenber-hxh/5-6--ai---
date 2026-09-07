package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type listResp struct {
	Records []struct {
		ID       string `json:"id"`
		RecordNo string `json:"recordNo"`
	} `json:"records"`
	Total      int  `json:"total"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	HasMore    bool `json:"hasMore"`
	FocusIndex int  `json:"focusIndex"`
}

func listRecordsAPI(t *testing.T, server *Server, token, query string) listResp {
	t.Helper()
	res := requestWithToken(server, http.MethodGet, "/api/inspection/records"+query, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET records%s: code=%d body=%s", query, res.Code, res.Body.String())
	}
	var out listResp
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// 造 n 条记录,时间依次往前推,便于按顺序核对分页。
func seedListRecords(t *testing.T, server *Server, n int) {
	t.Helper()
	now := time.Now().In(cnLoc)
	for i := 0; i < n; i++ {
		rec := &Record{
			ID:                fmt.Sprintf("rec_bulk_%04d", i),
			RecordNo:          fmt.Sprintf("ZX-BULK-%04d", i),
			Inspector:         "巡检员A",
			InspectorUserID:   "user_a",
			TemplateID:        "zihan_energy",
			TemplateName:      "能耗抄表",
			RecognitionStatus: "recognized",
			Fields:            []FieldValue{{Code: "site", Label: "巡检地点", Value: "正常"}},
			// 每条差一分钟:i 越小越新,所以倒序列表里 0000 排最前
			CreatedAt: now.Add(-time.Duration(i) * time.Minute),
		}
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord: %v", err)
		}
	}
}

// 记录列表的分页契约。
//
// 【这里钉的是一个活了将近一个月、没人看得出来的 bug】后端默认只回 100 条,
// 前端又从来不传 limit,于是后台「巡检记录」页底下永远写着"共 100 条" ——
// 而那个数是前端数自己手里数组的长度,看上去就是"这个系统只存了 100 条"。
//
// 现在 total 由后端给,是【筛选后的真实总数】,和这一页有多少条无关。
func TestRecordListPagingContract(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	const bulk = 600
	seedListRecords(t, server, bulk)
	total := bulk + 3 // 夹具自带 3 条

	t.Run("total 是总数,不是这一页的条数", func(t *testing.T) {
		got := listRecordsAPI(t, server, tokens["admin"], "?limit=15")
		if len(got.Records) != 15 {
			t.Errorf("这一页应有 15 条,实际 %d", len(got.Records))
		}
		if got.Total != total {
			t.Errorf("total 应是筛选后的总数 %d,实际 %d —— 页面底下的「共 N 条」靠它", total, got.Total)
		}
		if !got.HasMore {
			t.Error("后面还有几百条,hasMore 应为 true")
		}
	})

	t.Run("offset 真的翻页,不重复不跳条", func(t *testing.T) {
		p1 := listRecordsAPI(t, server, tokens["admin"], "?limit=15&offset=0")
		p2 := listRecordsAPI(t, server, tokens["admin"], "?limit=15&offset=15")
		if len(p1.Records) != 15 || len(p2.Records) != 15 {
			t.Fatalf("两页都该是 15 条,实际 %d / %d", len(p1.Records), len(p2.Records))
		}
		seen := map[string]bool{}
		for _, r := range p1.Records {
			seen[r.ID] = true
		}
		for _, r := range p2.Records {
			if seen[r.ID] {
				t.Errorf("第 2 页重复了第 1 页的记录 %s —— 翻页时看到重复条目,人会以为数据错了", r.ID)
			}
		}
		if p2.Offset != 15 {
			t.Errorf("响应里的 offset 应回显 15,实际 %d", p2.Offset)
		}
	})

	t.Run("最后一页不越界", func(t *testing.T) {
		got := listRecordsAPI(t, server, tokens["admin"], fmt.Sprintf("?limit=15&offset=%d", total-2))
		if len(got.Records) != 2 {
			t.Errorf("最后一页应剩 2 条,实际 %d", len(got.Records))
		}
		if got.HasMore {
			t.Error("已经是最后一页,hasMore 应为 false")
		}
	})

	t.Run("offset 超过总数时给空页,不报错也不回绕", func(t *testing.T) {
		// 【不能回绕到第一页】人手改地址栏或数据刚被删时会走到这里。
		// 回绕的话他看到的是第一页却以为是最后一页。
		got := listRecordsAPI(t, server, tokens["admin"], "?limit=15&offset=999999")
		if len(got.Records) != 0 {
			t.Errorf("越界应给空页,实际 %d 条", len(got.Records))
		}
		if got.Total != total {
			t.Errorf("越界时 total 仍应是 %d,实际 %d", total, got.Total)
		}
	})

	t.Run("单页条数超过上限会被压回来,而不是报错", func(t *testing.T) {
		// 一条记录带 fields_json / images_json,线上实测全量 654 KB。
		// ?limit=999999 照单全收的话,一个请求就能让后端序列化整库。
		// 但直接 400 也不对 —— 调用方只是想"多要点",封顶给它就是了。
		got := listRecordsAPI(t, server, tokens["admin"], "?limit=999999")
		if got.Limit != recordListMaxLimit {
			t.Errorf("超过上限应压回 %d,实际 %d", recordListMaxLimit, got.Limit)
		}
		if len(got.Records) != recordListMaxLimit {
			t.Errorf("超过上限时应给 %d 条,实际 %d", recordListMaxLimit, len(got.Records))
		}
	})

	t.Run("不传 limit 时给默认值", func(t *testing.T) {
		got := listRecordsAPI(t, server, tokens["admin"], "")
		if got.Limit != recordListDefaultLimit {
			t.Errorf("不传 limit 应给 %d,实际 %d", recordListDefaultLimit, got.Limit)
		}
	})
}

// 深链定位:后端要把目标记录所在的那一页直接给出来。
//
// 【为什么这件事非后端做不可】从台账 / 审批 / AI 洞察点进来带的是某条具体
// 记录。服务端分页之后它可能在第 7 页,而前端手里只有当前这一页 ——
// 自己算不出它在第几页。算不出来的后果不是"看不到",而是【看到错的那条】:
// 右侧详情面板会退回列表第一条,显示的是完全另一次巡检,且没有任何提示。
func TestRecordListFocusJumpsToItsPage(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	seedListRecords(t, server, 200)

	// 第 100 条(0 基),按倒序它排在第 100 位
	const targetNo = "ZX-BULK-0100"
	got := listRecordsAPI(t, server, tokens["admin"], "?limit=15&focusNo="+targetNo)
	if got.FocusIndex < 0 {
		t.Fatalf("应能定位到 %s,实际 focusIndex=%d", targetNo, got.FocusIndex)
	}
	if got.Offset != (got.FocusIndex/15)*15 {
		t.Errorf("应直接翻到目标所在的那一页:focusIndex=%d 时 offset 应是 %d,实际 %d",
			got.FocusIndex, (got.FocusIndex/15)*15, got.Offset)
	}
	found := false
	for _, r := range got.Records {
		if r.RecordNo == targetNo {
			found = true
		}
	}
	if !found {
		t.Errorf("返回的这一页里应该就有 %s(offset=%d)", targetNo, got.Offset)
	}

	t.Run("按 id 也能定位", func(t *testing.T) {
		got := listRecordsAPI(t, server, tokens["admin"], "?limit=15&focus=rec_bulk_0100")
		if got.FocusIndex < 0 {
			t.Errorf("按 id 也应能定位,实际 focusIndex=%d", got.FocusIndex)
		}
	})

	t.Run("被筛选挡住时回 -1,让前端去清筛选", func(t *testing.T) {
		// 【不能装作找到了】找不到时如果照常返回第一页,人看到的是
		// 另一条记录的详情,而他以为那就是他点的那条。
		got := listRecordsAPI(t, server, tokens["admin"],
			"?limit=15&focusNo="+targetNo+"&status=%E5%BC%82%E5%B8%B8")
		if got.FocusIndex != -1 {
			t.Errorf("目标被状态筛选挡住时应回 -1,实际 %d", got.FocusIndex)
		}
	})
}

// 筛选要在后端做,而且 total 要跟着筛选走。
//
// 原来筛选是在"已载入的那批"里做的客户端过滤:搜一台设备搜不到时,
// 界面说的是"没有结果",而真相是"更早的那些根本没载进来"。
func TestRecordListFiltersOnServer(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	seedListRecords(t, server, 300)
	// 造一条排在很后面、只有靠服务端筛选才找得到的记录
	rec := &Record{
		ID: "needle", RecordNo: "ZX-NEEDLE", PointName: "针尖点位",
		Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表",
		RecognitionStatus: "recognized",
		Fields:            []FieldValue{{Code: "site", Label: "巡检地点", Value: "正常"}},
		CreatedAt:         time.Now().In(cnLoc).Add(-500 * time.Minute), // 排在 300 条之后
	}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	got := listRecordsAPI(t, server, tokens["admin"], "?limit=15&keyword=%E9%92%88%E5%B0%96")
	if got.Total != 1 {
		t.Errorf("按关键词筛选后 total 应是 1,实际 %d —— 说明筛选没在服务端做,"+
			"或者只在已载入的那批里筛", got.Total)
	}
	if len(got.Records) != 1 || got.Records[0].RecordNo != "ZX-NEEDLE" {
		t.Errorf("应只返回那一条,实际 %+v", got.Records)
	}
}

// 没有时间戳的记录也必须出现在列表里。
//
// 【为什么单独钉一条】列表现在按"某个时间点之后"取数。零值时间早于任何
// 起点,一不小心就会被过滤掉 —— 而后果是这条记录【从列表里彻底消失】,
// 页面上没有任何提示,人只会以为这次巡检没做过。
// 早年的数据、外部导入的数据都可能缺 created_at。
func TestRecordListKeepsRecordsWithoutTimestamp(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	rec := &Record{
		ID: "no_time", RecordNo: "ZX-NOTIME",
		Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_energy", TemplateName: "能耗抄表",
		// CreatedAt 故意留零值
	}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	got := listRecordsAPI(t, server, tokens["admin"], "?limit=100&keyword=ZX-NOTIME")
	if got.Total != 1 || len(got.Records) != 1 {
		t.Fatalf("缺时间戳的记录也该能查到,实际 total=%d 条数=%d", got.Total, len(got.Records))
	}
	if got.Records[0].RecordNo != "ZX-NOTIME" {
		t.Errorf("拿错了记录:%+v", got.Records[0])
	}
}
