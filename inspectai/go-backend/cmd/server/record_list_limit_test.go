package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// 记录列表的条数契约。
//
// 【为什么值得钉住】这个 bug 活了将近一个月,而且没人看得出来:
// 后端 2026-08-10 就支持 ?limit= 了,前端却一直裸调用,于是永远只拿到
// 最新 100 条。后台「巡检记录」页底下显示「共 100 条」—— 那个数是前端
// 数自己手里数组的长度,看上去就是"这个系统只存了 100 条巡检记录"。
//
// 静默是关键:接口没报错、页面没报错、数据也没丢,只是少了一大半,
// 而且更早的记录连搜都搜不到(筛选是在已载入的这批里做的客户端过滤)。
// 更糟的是数据看板的近 30 天趋势图是拿这批明细在前端聚合的 —— 截断之后
// 较早的日子全画成 0,图是错的却很好看。
//
// 所以这里钉三件事:默认多少、能不能加、加到天上会不会被压回来。
func TestRecordListLimitContract(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)

	// helper 里自带 3 条。补到 500 以上,才试得出"上限会不会真的封顶" ——
	// 只造 120 条的话,limit=999999 一样返回全部,测试会假绿。
	now := time.Now()
	const bulk = 600
	for i := 0; i < bulk; i++ {
		rec := &Record{
			ID:              fmt.Sprintf("rec_bulk_%04d", i),
			Inspector:       "巡检员A",
			InspectorUserID: "user_a",
			TemplateID:      "zihan_energy",
			CreatedAt:       now.Add(-time.Duration(i) * time.Minute),
		}
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord(%s): %v", rec.ID, err)
		}
	}
	total := bulk + 3

	get := func(t *testing.T, query string) (count, limit int, truncated bool) {
		t.Helper()
		res := requestWithToken(server, http.MethodGet, "/api/inspection/records"+query, tokens["admin"])
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s: code=%d body=%s", query, res.Code, res.Body.String())
		}
		var out struct {
			Records   []json.RawMessage `json:"records"`
			Limit     int               `json:"limit"`
			Truncated bool              `json:"truncated"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", query, err)
		}
		return len(out.Records), out.Limit, out.Truncated
	}

	t.Run("不传 limit 时给默认值,并明说被截断了", func(t *testing.T) {
		count, limit, truncated := get(t, "")
		if count != recordListDefaultLimit {
			t.Errorf("不传 limit 应给 %d 条,实际 %d", recordListDefaultLimit, count)
		}
		if limit != recordListDefaultLimit {
			t.Errorf("响应里的 limit 应回 %d,实际 %d", recordListDefaultLimit, limit)
		}
		// 【这一条最重要】库里有 603 条却只回 100 条时,必须自己说出来。
		// 不说的话,调用方只能看到一个 100,而 100 既可能是"就这么多"
		// 也可能是"被砍了" —— 前端上次正是把它当成了前者。
		if !truncated {
			t.Error("库里远不止 100 条,truncated 必须为 true —— 否则调用方无从知道自己拿到的是残缺数据")
		}
	})

	t.Run("传了 limit 就按它给", func(t *testing.T) {
		count, limit, truncated := get(t, "?limit=500")
		if count != 500 {
			t.Errorf("limit=500 应给 500 条,实际 %d", count)
		}
		if limit != 500 {
			t.Errorf("响应里的 limit 应回 500,实际 %d", limit)
		}
		if !truncated {
			t.Errorf("库里 %d 条 > 500,仍应标记截断", total)
		}
	})

	t.Run("超过上限会被压回来,而不是报错", func(t *testing.T) {
		// 【故意不报错】一条记录带 fields_json / images_json,线上实测全量
		// 654 KB。?limit=999999 要是照单全收,一个请求就能让后端序列化整库。
		// 但直接 400 也不对 —— 调用方只是想"要全部",封顶给它就是了。
		count, limit, _ := get(t, "?limit=999999")
		if limit != recordListMaxLimit {
			t.Errorf("超过上限应压回 %d,实际 %d", recordListMaxLimit, limit)
		}
		if count != recordListMaxLimit {
			t.Errorf("超过上限时应给 %d 条,实际 %d", recordListMaxLimit, count)
		}
	})

	t.Run("没被截断时不能谎报截断", func(t *testing.T) {
		// truncated 的定义是 len(records) >= limit。要是这里判错,
		// 界面会常年挂着"数据不完整"的提示,人很快就不看它了 ——
		// 等真截断的那天,提示还在那儿,已经没人当回事。
		count, _, truncated := get(t, "?limit=5")
		if count != 5 || !truncated {
			t.Fatalf("前置条件不成立:count=%d truncated=%v", count, truncated)
		}
		// 用一个比库里总数还大、又没超上限的值:应当拿到全部且不标截断
		server2, tokens2 := newRecordAccessTestServer(t)
		res := requestWithToken(server2, http.MethodGet, "/api/inspection/records?limit=100", tokens2["admin"])
		var out struct {
			Records   []json.RawMessage `json:"records"`
			Truncated bool              `json:"truncated"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(out.Records) >= 100 {
			t.Fatalf("前置条件不成立:干净的测试服务器不该有 100 条记录,实际 %d", len(out.Records))
		}
		if out.Truncated {
			t.Errorf("只有 %d 条、上限 100 时不该标记截断", len(out.Records))
		}
	})
}
