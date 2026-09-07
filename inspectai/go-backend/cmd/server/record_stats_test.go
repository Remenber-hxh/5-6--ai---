package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type statsResp struct {
	Days []struct {
		Date     string         `json:"date"`
		Total    int            `json:"total"`
		ByStatus map[string]int `json:"byStatus"`
	} `json:"days"`
	From string `json:"from"`
	To   string `json:"to"`
}

func getStats(t *testing.T, server *Server, token, query string) statsResp {
	t.Helper()
	res := requestWithToken(server, http.MethodGet, "/api/inspection/stats/daily"+query, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET stats%s: code=%d body=%s", query, res.Code, res.Body.String())
	}
	var out statsResp
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func (s statsResp) day(t *testing.T, date string) (int, map[string]int) {
	t.Helper()
	for _, d := range s.Days {
		if d.Date == date {
			return d.Total, d.ByStatus
		}
	}
	t.Fatalf("结果里没有 %s 这一天(from=%s to=%s)", date, s.From, s.To)
	return 0, nil
}

// 【这是这个接口存在的全部理由】看板的图原来是前端拿记录列表自己数的,
// 而那个列表有上限。记录一超过上限,较早的那些天就全数成 0 —— 图画出来
// 是一条平滑的下降线,像巡检量在下滑,而且不报任何错。
//
// 所以这里造的记录数【必须远超记录列表的上限】,否则测试通过也说明不了问题。
func TestDailyStatsNotCappedByRecordListLimit(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)

	// 30 天,每天 25 条 = 750 条,远超 recordListMaxLimit(500)。
	// 前端老做法在这个量下,最早那 10 天会全是 0。
	now := time.Now().In(cnLoc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, cnLoc)
	const perDay = 25
	for d := 0; d < 30; d++ {
		for i := 0; i < perDay; i++ {
			rec := &Record{
				ID:                fmt.Sprintf("rec_d%02d_%02d", d, i),
				Inspector:         "巡检员A",
				InspectorUserID:   "user_a",
				TemplateID:        "zihan_energy",
				RecognitionStatus: "recognized",
				Fields:            []FieldValue{{Code: "a", Value: "正常"}},
				CreatedAt:         today.AddDate(0, 0, -d),
			}
			if err := server.store.CreateRecord(rec); err != nil {
				t.Fatalf("CreateRecord: %v", err)
			}
		}
	}

	got := getStats(t, server, tokens["admin"], "?days=30")
	if len(got.Days) != 30 {
		t.Fatalf("应返回 30 天,实际 %d", len(got.Days))
	}
	// 最早那一天最能说明问题:它在"最新 500 条"之外。
	earliest := today.AddDate(0, 0, -29).Format("2006-01-02")
	total, _ := got.day(t, earliest)
	if total != perDay {
		t.Errorf("最早一天应有 %d 条,实际 %d —— 说明聚合仍然受条数上限影响,"+
			"较早的日子被数成了偏小的值(这正是图会画错的原因)", perDay, total)
	}
	sum := 0
	for _, d := range got.Days {
		sum += d.Total
	}
	if want := 30 * perDay; sum < want {
		t.Errorf("30 天合计应至少 %d 条,实际 %d", want, sum)
	}
}

// 【日界线按东八区,不是 UTC】前端原来用 toISOString().slice(0,10) 取日期,
// 那是 UTC 的日期。北京时间早上 7:30 = 前一天 23:30 UTC ——
// 于是【早班巡检一直被记在前一天】,而且两张图都错,没人看得出来。
func TestDailyStatsUsesLocalDayBoundary(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)

	now := time.Now().In(cnLoc)
	// 昨天早上 7:30(东八区)。用昨天而不是今天,避免测试在 00:00-08:00
	// 之间跑时,"今天早上 7:30"还没到、变成未来时间。
	early := time.Date(now.Year(), now.Month(), now.Day(), 7, 30, 0, 0, cnLoc).AddDate(0, 0, -1)
	rec := &Record{
		ID: "rec_early", Inspector: "巡检员A", InspectorUserID: "user_a",
		TemplateID: "zihan_energy", RecognitionStatus: "recognized",
		Fields:    []FieldValue{{Code: "a", Value: "正常"}},
		CreatedAt: early,
	}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	got := getStats(t, server, tokens["admin"], "?days=7")
	localDay := early.Format("2006-01-02")
	utcDay := early.UTC().Format("2006-01-02")
	if localDay == utcDay {
		t.Fatalf("测试前提不成立:这个时刻在两个时区里是同一天(%s),换不出差异", localDay)
	}
	total, _ := got.day(t, localDay)
	if total != 1 {
		t.Errorf("早上 7:30 的巡检应记在本地日期 %s,实际那天是 %d 条", localDay, total)
	}
	if n, _ := got.day(t, utcDay); n != 0 {
		t.Errorf("不该记到 UTC 日期 %s 上(那是前一天),实际 %d 条", utcDay, n)
	}
}

// 状态分布要和记录页一个口径 —— 两处对同一条记录说法不同,人只会觉得数据错了。
func TestDailyStatsUsesUIStatusRule(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	now := time.Now().In(cnLoc)
	// 【挪到 3 天前】newRecordAccessTestServer 自带 3 条"今天"的记录,
	// 混在一起就分不清数出来的是我造的还是夹具带的 —— 用例会因为
	// 别处改了夹具而莫名其妙地红。
	day3 := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, cnLoc).AddDate(0, 0, -3)

	mk := func(id string, rec Record) {
		rec.ID, rec.CreatedAt = id, day3
		rec.Inspector, rec.InspectorUserID, rec.TemplateID = "巡检员A", "user_a", "zihan_energy"
		if err := server.store.CreateRecord(&rec); err != nil {
			t.Fatalf("CreateRecord(%s): %v", id, err)
		}
	}
	mk("s_abnormal", Record{RecognitionStatus: "recognized", Fields: []FieldValue{{Code: "a", Value: "阀门破损"}}})
	mk("s_done", Record{Submitted: true, RecognitionStatus: "recognized", Fields: []FieldValue{{Code: "a", Value: "正常"}}})
	// 空记录:界面口径是「待复核」,日报口径会算成「正常」。
	// 这里必须跟界面走 —— 否则看板把没巡的算成巡好了。
	mk("s_empty", Record{RecognitionStatus: "not_started"})

	_, byStatus := getStats(t, server, tokens["admin"], "?days=7").day(t, day3.Format("2006-01-02"))
	for status, want := range map[string]int{"异常": 1, "已完成": 1, "待复核": 1} {
		if byStatus[status] != want {
			t.Errorf("「%s」应为 %d,实际 %d(全部:%v)", status, want, byStatus[status], byStatus)
		}
	}
	if byStatus["正常"] != 0 {
		t.Errorf("空记录不该被算进「正常」—— 那等于说这里巡过了没问题(全部:%v)", byStatus)
	}
}

// 数据范围要生效。【这一档判错不会报错,只会静默越权】——
// 在看板上数到别的项目的巡检量,数字偏大,而且看不出来源。
func TestDailyStatsRespectsProjectFilter(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	now := time.Now().In(cnLoc)
	// 同上:避开夹具自带的那 3 条"今天"的记录
	day3 := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, cnLoc).AddDate(0, 0, -3)
	for i, proj := range []string{"会议中心", "会议中心", "紫菡雅集"} {
		rec := &Record{
			ID: fmt.Sprintf("rec_p%d", i), Project: proj,
			Inspector: "巡检员A", InspectorUserID: "user_a", TemplateID: "zihan_energy",
			RecognitionStatus: "recognized", Fields: []FieldValue{{Code: "a", Value: "正常"}},
			CreatedAt: day3,
		}
		if err := server.store.CreateRecord(rec); err != nil {
			t.Fatalf("CreateRecord: %v", err)
		}
	}
	day := day3.Format("2006-01-02")

	all, _ := getStats(t, server, tokens["admin"], "?days=7").day(t, day)
	if all != 3 {
		t.Fatalf("不筛项目应为 3 条,实际 %d", all)
	}
	one, _ := getStats(t, server, tokens["admin"], "?days=7&project=%E7%B4%AB%E8%8F%A1%E9%9B%85%E9%9B%86").day(t, day)
	if one != 1 {
		t.Errorf("按项目筛选后应为 1 条,实际 %d", one)
	}
}

// 非管理角色不该拿到全租户的聚合量 —— 路由表上是 guardSupervisor。
func TestDailyStatsRefusesInspector(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	res := requestWithToken(server, http.MethodGet, "/api/inspection/stats/daily", tokens["inspector_a"])
	if res.Code == http.StatusOK {
		t.Errorf("巡检员不该拿到看板聚合数据,实际 code=%d body=%s", res.Code, res.Body.String())
	}
}
