package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func moveReading(t *testing.T, srv *Server, tok, recID, from, to string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"fromCode":"` + from + `","toCode":"` + to + `"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/inspection/records/"+recID+"/fields/move", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 搬家要搬整组:读数 + 那张照片 + 置信度 + AI 原值。
//
// 【只搬读数不搬照片会怎样】确认页上那一行下面还摆着原来那张照片,
// 人看着 Z1 的照片核 Z3 的数 —— 而两个数字本身都是对的,看不出错在哪。
func TestMoveReadingCarriesPhotoAndProvenance(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	tpl, _ := templateByID("zihan_energy")
	rec := &Record{
		ID: "rec_move", TenantID: defaultTenantID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		Inspector: "胡晓悱", Fields: initialFieldValues(tpl, "胡晓悱", "紫菡雅集"),
		Images: []ImageInfo{{ID: "img_a"}, {ID: "img_b"}},
	}
	f, _ := fieldByCode(rec.Fields, "z1_reading")
	f.Value, f.AIValue = "60197.924", "60197.924"
	f.Confidence, f.Source = 0.75, "ai"
	f.SourceImageID, f.Bbox = "img_b", []float64{0.1, 0.2, 0.3, 0.4}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}

	got := moveReading(t, server, tokens["admin"], rec.ID, "z1_reading", "z3_reading")
	if got.Code != http.StatusOK {
		t.Fatalf("挪不动 code=%d body=%s", got.Code, got.Body.String())
	}

	after, _ := server.store.GetRecord(defaultTenantID, rec.ID)
	z1, _ := fieldByCode(after.Fields, "z1_reading")
	z3, _ := fieldByCode(after.Fields, "z3_reading")

	if z3.Value != "60197.924" {
		t.Errorf("读数没搬过去:%q", z3.Value)
	}
	if z3.SourceImageID != "img_b" {
		t.Errorf("照片没跟着搬:%q —— 那一行会摆着别人的照片", z3.SourceImageID)
	}
	if len(z3.Bbox) != 4 {
		t.Errorf("读数区位置没跟着搬:%v", z3.Bbox)
	}
	if z3.AIValue != "60197.924" {
		t.Errorf("AI 原值没搬:%q —— 那是「AI 当时读成什么」的证据", z3.AIValue)
	}
	if z3.Source != "human-edited" {
		t.Errorf("挪这一下是人做的判断,source 该说实话,得到 %q", z3.Source)
	}

	if z1.Value != "" || z1.SourceImageID != "" || z1.Bbox != nil {
		t.Errorf("原来那格没清干净:value=%q img=%q bbox=%v", z1.Value, z1.SourceImageID, z1.Bbox)
	}
	// AssetName 是"这一格属于哪台设备"的配置,不随某一次读数搬家
	if z1.AssetName != "" && z1.AssetName == z3.AssetName {
		t.Error("两格的设备归属变成一样的了")
	}
}

// 目标格已经有读数就拦住 —— 不拦会把那台表刚抄的数冲掉,
// 而且日报上出现两个一样的读数、少一台表,现场看不出来。
func TestMoveReadingRefusesOccupiedTarget(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	tpl, _ := templateByID("zihan_energy")
	rec := &Record{
		ID: "rec_occupied", TenantID: defaultTenantID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		Inspector: "胡晓悱", Fields: initialFieldValues(tpl, "胡晓悱", "紫菡雅集"),
	}
	a, _ := fieldByCode(rec.Fields, "z1_reading")
	a.Value = "60197.924"
	b, _ := fieldByCode(rec.Fields, "z3_reading")
	b.Value = "84363.520"
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}

	got := moveReading(t, server, tokens["admin"], rec.ID, "z1_reading", "z3_reading")
	if got.Code != http.StatusConflict {
		t.Fatalf("目标格有值却放行了 code=%d body=%s", got.Code, got.Body.String())
	}
	after, _ := server.store.GetRecord(defaultTenantID, rec.ID)
	z1, _ := fieldByCode(after.Fields, "z1_reading")
	z3, _ := fieldByCode(after.Fields, "z3_reading")
	if z1.Value != "60197.924" || z3.Value != "84363.520" {
		t.Errorf("拦下了却还是改了数据:z1=%q z3=%q", z1.Value, z3.Value)
	}
}

// 已提交的记录不许这么改 —— 那条路要走修改申请,那里有审批留痕。
func TestMoveReadingRefusesSubmitted(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	tpl, _ := templateByID("zihan_energy")
	rec := &Record{
		ID: "rec_submitted", TenantID: defaultTenantID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		Inspector: "胡晓悱", Submitted: true,
		Fields: initialFieldValues(tpl, "胡晓悱", "紫菡雅集"),
	}
	a, _ := fieldByCode(rec.Fields, "z1_reading")
	a.Value = "60197.924"
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	got := moveReading(t, server, tokens["admin"], rec.ID, "z1_reading", "z3_reading")
	if got.Code != http.StatusConflict {
		t.Fatalf("已提交的记录被直接改了 code=%d", got.Code)
	}
}

// 挪到同一格、字段不存在 —— 这类请求要明确报错,不能默默当成功。
func TestMoveReadingRejectsNonsense(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	tpl, _ := templateByID("zihan_energy")
	rec := &Record{
		ID: "rec_nonsense", TenantID: defaultTenantID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		Inspector: "胡晓悱", Fields: initialFieldValues(tpl, "胡晓悱", "紫菡雅集"),
	}
	if err := server.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, from, to string }{
		{"挪到同一格", "z1_reading", "z1_reading"},
		{"源字段不存在", "nope", "z1_reading"},
		{"目标字段不存在", "z1_reading", "nope"},
		{"空的标识", "", "z1_reading"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := moveReading(t, server, tokens["admin"], rec.ID, c.from, c.to)
			if got.Code < 400 {
				t.Errorf("这种请求该报错,得到 code=%d", got.Code)
			}
		})
	}
}
