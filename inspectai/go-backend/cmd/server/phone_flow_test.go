package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ===== 现场整套操作,按手机实际调的接口原样走一遍 =====
//
// 2026-10-11 紫菡:拍照顺序打乱(Z4、Z3、Z1、Z2、生活水表、消防水表),Z1、Z2 的小数点又各错一位。
// 期望:读对的两张自动进 Z4、Z3 的格子;读错的两张清空、请人选设备;
// 人选好设备、照着照片填上数,格子就变白,不用再点确认,一次提交成功。
func TestPhoneFlowScrambledMeterPhotos(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	// 提交时要调 AI 写总结:空地址 = 调用失败走兜底,和其它提交测试一样(不影响提交本身)
	s.aiClient, s.analyticsClient = NewAIClient(""), NewAnalyticsClient("")
	seedDefaultNamedMeters(t, s, "Z1", "Z2", "Z3", "Z4", "生活水表", "消防水表")
	d := func(day int) time.Time { return time.Date(2026, 10, day, 10, 0, 0, 0, time.UTC) }
	for name, vals := range map[string][2]float64{
		"Z1": {206171.68, 206317.78}, "Z2": {116969.15, 116978.84},
		"Z3": {86025.608, 86122.408}, "Z4": {61873.044, 61904.564},
		"生活水表": {2119, 2190}, "消防水表": {104, 104},
	} {
		for i, v := range vals {
			v := v
			_ = s.store.WriteAssetSnapshots(nil, []*FieldObservation{{AssetID: "紫菡雅集::zihan_energy::" + name,
				RecordID: "h" + name + strconv.Itoa(i), FieldKey: "z1_reading", ValueNumber: &v, CreatedAt: d(8 + i)}})
		}
	}

	// ---- 识别:AI 按拍照顺序填格子(和线上 analyze 之后同一步:flagImplausibleReadings) ----
	rec := zihanMeterRecord(t, "rec_flow", map[string]string{
		"z1_reading": "61922.564", "z2_reading": "86217.840", "z3_reading": "20647.632", "z4_reading": "11698.725",
		"living_water_reading": "2225", "fire_water_reading": "104",
	})
	rec.CreatedAt, rec.RecognitionStatus, rec.Inspector = d(11), "recognized", "胡晓悱"
	// 巡检地点:确认页顶上已经带出来的那一格(真实页面上它是填好的)
	site := recField(t, rec, "site")
	site.Value, site.Source, site.NeedsReview = "紫菡雅集", "human-confirmed", false
	order := []string{"z1_reading", "z2_reading", "z3_reading", "z4_reading", "living_water_reading", "fire_water_reading"}
	for i, code := range order {
		id := "img" + strconv.Itoa(i+1)
		rec.Images = append(rec.Images, ImageInfo{ID: id, FileName: id + ".jpg", Path: "/tmp/" + id + ".jpg", CreatedAt: d(11)})
		recField(t, rec, code).SourceImageID = id
	}
	flagImplausibleReadings(s.store, rec)
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}

	load := func() *Record {
		got, err := s.store.GetRecord(defaultTenantID, rec.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	slotOf := func(img string) *FieldValue {
		for _, f := range load().Fields {
			if f.SourceImageID == img {
				f := f
				return &f
			}
		}
		t.Fatalf("找不到照片 %s 所在的格子", img)
		return nil
	}
	ok := func(w *httptest.ResponseRecorder, what string) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s 失败:%d %s", what, w.Code, w.Body.String())
		}
	}

	// 识别完:第 1、2 张已在 Z4、Z3 的格子里
	if f := slotOf("img1"); f.Code != "z4_reading" || f.Value != "61922.564" {
		t.Fatalf("第 1 张(Z4)应自动进 Z4 那一格:%+v", f)
	}
	if f := slotOf("img2"); f.Code != "z3_reading" || f.Value != "86217.840" {
		t.Fatalf("第 2 张(Z3)应自动进 Z3 那一格:%+v", f)
	}
	for img, ai := range map[string]string{"img3": "20647.632", "img4": "11698.725"} {
		if f := slotOf(img); f.Value != "" || f.Reason != sanityNoteMark+"AI 读作 "+ai+",请选择设备" {
			t.Fatalf("%s 应清空、请人选设备:%+v", img, f)
		}
	}

	// ---- 人:第 3 张选 Z1(手机端:Z1 自己那一格装着别的照片 → 对调,再把名字落定)----
	cur := slotOf("img3")
	if cur.Code != "z1_reading" {
		ok(postSwap(t, s, tok, rec.ID, `{"aCode":"`+cur.Code+`","bCode":"z1_reading"}`), "第 3 张对调进 Z1")
	}
	ok(patchField(t, s, tok, rec.ID, "z1_reading", `{"assetName":"Z1"}`), "第 3 张选 Z1")
	ok(patchField(t, s, tok, rec.ID, "z1_reading", `{"value":"206476.32"}`), "第 3 张照照片填数")

	// ---- 人:第 4 张选 Z2 ----
	cur = slotOf("img4")
	if cur.Code != "z2_reading" {
		ok(postSwap(t, s, tok, rec.ID, `{"aCode":"`+cur.Code+`","bCode":"z2_reading"}`), "第 4 张对调进 Z2")
	}
	ok(patchField(t, s, tok, rec.ID, "z2_reading", `{"assetName":"Z2"}`), "第 4 张选 Z2")
	ok(patchField(t, s, tok, rec.ID, "z2_reading", `{"value":"116987.25"}`), "第 4 张照照片填数")

	// ---- 一键确认:只剩 AI 读的、没人动过的那几格 ----
	for _, f := range load().Fields {
		if f.Source == "ai" && f.Value != "" && f.Confidence < 0.95 {
			ok(patchField(t, s, tok, rec.ID, f.Code, `{"value":"`+f.Value+`","action":"confirm"}`), "一键确认 "+f.Code)
		}
	}

	// 人动过的格子都是白的
	for _, f := range load().Fields {
		if f.NeedsReview {
			t.Errorf("%s 还标着需确认(人已经处理过了):value=%q reason=%q", f.Code, f.Value, f.Reason)
		}
	}
	want := map[string]string{"z1_reading": "206476.32", "z2_reading": "116987.25", "z3_reading": "86217.840", "z4_reading": "61922.564"}
	for code, v := range want {
		if f := recField(t, load(), code); f.Value != v {
			t.Errorf("%s 最后应是 %s,得到 %q", code, v, f.Value)
		}
	}

	// ---- 提交 ----
	req := httptest.NewRequest(http.MethodPost, "/api/inspection/records/"+rec.ID+"/submit", strings.NewReader(""))
	req.Header.Set("X-InspectAI-Token", tok)
	req.Header.Set("Idempotency-Key", "k_flow")
	w := httptest.NewRecorder()
	s.router(w, req)
	if w.Code != http.StatusOK {
		var e map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &e)
		t.Fatalf("提交被拦:%d %v", w.Code, e)
	}
}
