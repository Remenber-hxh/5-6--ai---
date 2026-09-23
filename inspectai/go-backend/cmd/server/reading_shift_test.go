package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===== 整体错位一格,现有的合理性检查抓不抓得到 =====
//
// 2026-09-22 线上实际发生的:三块电表的读数各自挪到了相邻那块表上。
//   9/21  Z1=116748.24  Z2=60634.484  Z3=85119.928
//   9/22  Z1=60730.248  Z2=85202.560  Z3=1167636.5
// 每个数字本身都对,只是挂错了表 —— 任何"范围校验"都抓不到。
// 唯一能抓到的信号是"和这一格上次的值比":累计电表只会小幅上涨。

func shiftTestServer(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := NewSQLiteStore(filepath.Join(t.TempDir(), "shift.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func meterRec(id string, submitted bool, vals map[string]string) *Record {
	rec := &Record{
		ID: id, TenantID: defaultTenantID, TemplateID: "zihan_energy",
		Project: "紫菡雅集", CreatedAt: time.Now(), Submitted: submitted,
	}
	if submitted {
		now := time.Now()
		rec.SubmittedAt = &now
	}
	for _, code := range []string{"z1_reading", "z2_reading", "z3_reading"} {
		rec.Fields = append(rec.Fields, FieldValue{
			Code: code, Label: code, Value: vals[code], Source: "ai",
		})
	}
	return rec
}

// 【最要紧的一条】错位之后,至少要有字段被标出来。
func TestShiftedReadingsAreFlagged(t *testing.T) {
	st := shiftTestServer(t)
	// 9/21 的真实值,作为基准
	if err := st.CreateRecord(meterRec("rec_0921", true, map[string]string{
		"z1_reading": "116748.24", "z2_reading": "60634.484", "z3_reading": "85119.928",
	})); err != nil {
		t.Fatal(err)
	}
	// 9/22:整体错位一格,其中一块还丢了小数点
	now := meterRec("rec_0922", false, map[string]string{
		"z1_reading": "60730.248", "z2_reading": "85202.560", "z3_reading": "1167636.5",
	})

	issues := flagImplausibleReadings(st, now)
	if len(issues) == 0 {
		t.Fatal("整体错位一格,一个字段都没被标出来 —— 现场没有任何提示")
	}
	got := map[string]string{}
	for _, is := range issues {
		got[is.Code] = is.Reason
	}
	t.Logf("被标出来的:%v", got)

	// Z1 从 116748 掉到 60730:累计表不可能变小
	if r, ok := got["z1_reading"]; !ok {
		t.Error("Z1 读数变小了却没被标出来")
	} else if !strings.Contains(r, "还小") {
		t.Errorf("Z1 的理由不对:%q", r)
	}
	// Z3 从 85119 跳到 1167636:量级对不上(小数点)
	if r, ok := got["z3_reading"]; !ok {
		t.Error("Z3 读数跳了十几倍却没被标出来")
	} else if !strings.Contains(r, "量级对不上") {
		t.Errorf("Z3 的理由不对:%q", r)
	}

	// 被标出来的字段必须同时置上待复核 —— 否则台账那边照样判成正常
	for i := range now.Fields {
		f := &now.Fields[i]
		if _, bad := got[f.Code]; bad && !f.NeedsReview {
			t.Errorf("%s 标了存疑却没置 NeedsReview,台账会判成正常", f.Code)
		}
	}
}

// 【那句解释必须能传到人眼前】sanity 写进 Reason 的话,要被认成"异常线索",
// 否则台账摘要和设备详情都不会显示它,只剩一句笼统的"AI 识别把握不大"。
func TestSanityReasonReachesTheUser(t *testing.T) {
	for _, s := range []string{
		"【读数存疑】比上一次的 116748.24 还小(累计读数只会往上走)",
		"【读数存疑】是上一次 85119.928 的 14 倍,量级对不上 —— 最常见的原因是小数点丢了",
		"【读数存疑】累计读数不可能是负数",
	} {
		if !containsAnomalyKeyword(s) {
			t.Errorf("这句话没被认成异常线索,会被埋掉:%s", s)
		}
	}
	// 正常的理由不该被误判成异常
	for _, s := range []string{"放大复核一致", "屏幕小数点清晰可见"} {
		if containsAnomalyKeyword(s) {
			t.Errorf("正常理由被误判成异常:%s", s)
		}
	}
}
