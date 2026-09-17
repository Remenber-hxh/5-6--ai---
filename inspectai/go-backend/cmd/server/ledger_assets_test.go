package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===== 抄表提交写台账:读数记到现场选的那台表上,不凭空建假设备 =====
//
// 【线上的数据长这样,测试就照这个造】台账里的电表叫「Z1」…「Z5」,
// 不是模板默认的「Z1能耗表」;而且编号和名称不一定一样。
// 原来的写法一律写死默认名字:现场选的 Z1 收不到读数,台账里还多出一批
// 「Z1能耗表」这样的重复设备 —— 本地数据恰好叫默认名字,所以一直没暴露。

func newLedgerTestServer(t *testing.T) (*Server, *SQLiteStore) {
	t.Helper()
	st, err := NewSQLiteStore(filepath.Join(t.TempDir(), "ledger_assets.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Server{store: st, storeKind: "sqlite"}, st
}

// 线上形状的台账:Z1(编号=名称)、Z2(编号 DB-02 ≠ 名称 Z2)、Z3、生活水表
func seedProdShapedMeters(t *testing.T, st *SQLiteStore) {
	t.Helper()
	for _, a := range []*AssetEntry{
		{ID: "紫菡雅集::zihan_energy::Z1", AssetKey: "Z1", AssetName: "Z1", AssetType: "电表"},
		{ID: "紫菡雅集::zihan_energy::DB-02", AssetKey: "DB-02", AssetName: "Z2", AssetType: "电表"},
		{ID: "紫菡雅集::zihan_energy::Z3", AssetKey: "Z3", AssetName: "Z3", AssetType: "电表"},
		{ID: "紫菡雅集::zihan_energy::生活水表", AssetKey: "生活水表", AssetName: "生活水表", AssetType: "水表"},
	} {
		a.TenantID, a.Project, a.TemplateID, a.LastStatus = defaultTenantID, "紫菡雅集", "zihan_energy", "未巡检"
		if err := st.CreateAsset(a); err != nil {
			t.Fatalf("CreateAsset %s: %v", a.AssetName, err)
		}
	}
}

func meterRecord(fields map[string][2]string) *Record {
	rec := &Record{
		ID: "rec_meter", TenantID: defaultTenantID, TemplateID: "zihan_energy",
		Project: "紫菡雅集", CreatedAt: time.Now(),
	}
	for _, code := range []string{"z1_reading", "z2_reading", "z3_reading", "z4_reading",
		"living_water_reading", "fire_water_reading"} {
		v := fields[code] // [读数, 选的设备]
		rec.Fields = append(rec.Fields, FieldValue{Code: code, Label: code, Value: v[0], AssetName: v[1]})
	}
	return rec
}

func namesAndIDs(assets []*AssetEntry) map[string]string {
	out := map[string]string{}
	for _, a := range assets {
		out[a.AssetName] = a.ID
	}
	return out
}

// 【最要紧的一条】选了哪台,读数就记到哪台的真实 ID 上 ——
// 包括编号和名称不一样的那台(Z2 的编号是 DB-02)。
func TestMeterReadingGoesToChosenDevice(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	seedProdShapedMeters(t, st)
	rec := meterRecord(map[string][2]string{
		"z1_reading": {"60197.9", "Z1"},
		"z2_reading": {"60200.1", "Z2"},
	})
	got := namesAndIDs(srv.buildLedgerAssets(rec, time.Now()))
	if got["Z1"] != "紫菡雅集::zihan_energy::Z1" {
		t.Errorf("Z1 没记到台账那台上,ID=%q", got["Z1"])
	}
	if got["Z2"] != "紫菡雅集::zihan_energy::DB-02" {
		t.Errorf("Z2(编号 DB-02)没对上真实设备,ID=%q —— 会另建一台", got["Z2"])
	}
}

// 【不凭空建假设备】台账里有电表、但这一格没选是哪台 → 这一格不写台账。
func TestUnchosenMeterSlotCreatesNoPhantom(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	seedProdShapedMeters(t, st)
	rec := meterRecord(map[string][2]string{
		"z1_reading": {"60197.9", "Z1"},
		"z3_reading": {"60300.0", ""}, // AI 读出了数,但现场没选是哪台
	})
	got := namesAndIDs(srv.buildLedgerAssets(rec, time.Now()))
	for name := range got {
		if strings.HasSuffix(name, "能耗表") || name == "消防水表" {
			t.Errorf("凭模板默认名字建出了假设备「%s」—— 台账里没有叫这个名字的表", name)
		}
	}
	if _, ok := got["Z1"]; !ok {
		t.Error("选了设备的那一格反而没写台账")
	}
}

// 名字和模板默认一字不差时,不用选也会自动对上(本地/老数据就是这样,不能改坏)。
func TestDefaultNameStillMatches(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	seedProdShapedMeters(t, st)
	rec := meterRecord(map[string][2]string{
		"living_water_reading": {"1995", ""}, // 台账里正好有「生活水表」
	})
	// 真实记录里字段名是「生活水表读数」—— 默认设备按它去掉"读数"来匹配
	for i := range rec.Fields {
		if rec.Fields[i].Code == "living_water_reading" {
			rec.Fields[i].Label = "生活水表读数"
		}
	}
	got := namesAndIDs(srv.buildLedgerAssets(rec, time.Now()))
	if got["生活水表"] != "紫菡雅集::zihan_energy::生活水表" {
		t.Errorf("默认名字对得上的表没自动记上,得到 %+v", got)
	}
}

// 台账里一台表都还没有(全新部署)时,照原来按模板默认名字建 —— 这是自动建台账的起点。
func TestNoMetersInLedgerKeepsBootstrapBehaviour(t *testing.T) {
	srv, _ := newLedgerTestServer(t)
	rec := meterRecord(map[string][2]string{"z1_reading": {"100", ""}})
	got := namesAndIDs(srv.buildLedgerAssets(rec, time.Now()))
	if _, ok := got["Z1能耗表"]; !ok {
		t.Errorf("空台账时应按默认名字建档,得到 %+v", got)
	}
}

// 【候选设备不能写进记录】候选是按台账现算的,存下来会过期。
func TestLedgerBuildDoesNotMutateRecord(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	seedProdShapedMeters(t, st)
	rec := meterRecord(map[string][2]string{"z1_reading": {"1", "Z1"}})
	_ = srv.buildLedgerAssets(rec, time.Now())
	for _, f := range rec.Fields {
		if len(f.AssetOptions) > 0 {
			t.Fatalf("%s 的候选设备被写进了记录本身 —— 会随提交落库", f.Code)
		}
	}
}

// 【启动回填和提交同一套规则】提交时不建的假设备,回填要是照建,
// 每次重启都会补回来。
func TestBackfillCreatesNoPhantomMeters(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	seedProdShapedMeters(t, st)
	rec := meterRecord(map[string][2]string{
		"z1_reading": {"60197.9", "Z1"},
		"z4_reading": {"60400.0", ""},
	})
	rec.Submitted = true
	now := time.Now()
	rec.SubmittedAt = &now
	if err := st.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	if err := srv.ensureAssetLedgerFromRecords(); err != nil {
		t.Fatalf("回填: %v", err)
	}
	all, err := st.ListAssets(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if strings.HasSuffix(a.AssetName, "能耗表") || a.AssetName == "消防水表" {
			t.Errorf("回填建出了假设备「%s」(%s)", a.AssetName, a.ID)
		}
	}
	if len(all) != 4 {
		t.Errorf("台账应仍是原来 4 台,现在 %d 台", len(all))
	}
}
