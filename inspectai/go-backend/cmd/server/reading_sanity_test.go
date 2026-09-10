package main

import (
	"strings"
	"testing"
	"time"
)

// 造一条紫菡能耗记录:只填抄表那几个数字字段。
func zihanEnergyRecord(t *testing.T, id string, submitted bool, vals map[string]string) *Record {
	t.Helper()
	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("找不到 zihan_energy")
	}
	rec := &Record{
		ID: id, TenantID: defaultTenantID, TemplateID: tpl.ID, TemplateName: tpl.Name,
		PointID: "pt_zihan_energy", PointName: "能耗抄表点位",
		Submitted: submitted, CreatedAt: time.Now(),
		Fields: initialFieldValues(tpl, "巡检员"),
	}
	for i := range rec.Fields {
		if v, ok := vals[rec.Fields[i].Code]; ok {
			rec.Fields[i].Value = v
			rec.Fields[i].AIValue = v
			rec.Fields[i].Source = "ai"
			rec.Fields[i].Confidence = 0.92
		}
	}
	return rec
}

// recField 取记录里的某个字段(util.go 的 fieldByCode 收的是切片,这里包一层)
func recField(t *testing.T, rec *Record, code string) *FieldValue {
	t.Helper()
	f, _ := fieldByCode(rec.Fields, code)
	if f == nil {
		t.Fatalf("记录里没有字段 %s", code)
	}
	return f
}

// 这就是 2026-09-10 那条记录真实发生的事:模型把 LCD 上下两行拼成一个整数,
// 60197.924 变成 60197924,还给了 92% —— 然后顶着 92% 被人点了确认。
func TestConcatenatedMeterReadingGetsFlagged(t *testing.T) {
	store := NewMemStore()
	prev := zihanEnergyRecord(t, "rec_prev", true, map[string]string{
		"z1_reading": "55967.844",
	})
	prev.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
	if err := store.CreateRecord(prev); err != nil {
		t.Fatal(err)
	}

	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{
		"z1_reading": "60197924",
	})
	issues := flagImplausibleReadings(store, rec)
	if len(issues) != 1 {
		t.Fatalf("拼接出来的读数没被抓住,issues=%v", issues)
	}
	f := recField(t, rec, "z1_reading")
	if !f.NeedsReview {
		t.Error("没有强制人工复核 —— 那它还是会顶着高置信度混过确认页")
	}
	if f.Confidence > 0.5 {
		t.Errorf("置信度没压下来,还是 %v", f.Confidence)
	}
	if f.Value != "60197924" {
		t.Errorf("值被改了(%q)—— 只该降级不该改值,人得看见 AI 读成了什么", f.Value)
	}
	if !strings.Contains(f.Reason, "读数存疑") {
		t.Errorf("理由没写进字段,确认页上看不出为什么要复核:%q", f.Reason)
	}
}

// 正常的月度增长不能被误报,否则这个校验会被当成噪音关掉。
func TestNormalMeterGrowthNotFlagged(t *testing.T) {
	store := NewMemStore()
	prev := zihanEnergyRecord(t, "rec_prev", true, map[string]string{
		"z1_reading": "55967.844", "living_water_reading": "722",
	})
	prev.CreatedAt = time.Now().Add(-30 * 24 * time.Hour)
	if err := store.CreateRecord(prev); err != nil {
		t.Fatal(err)
	}
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{
		"z1_reading": "60197.924", "living_water_reading": "1992",
	})
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("正常增长被误报了:%v", issues)
	}
	if f := recField(t, rec, "z1_reading"); f.NeedsReview || f.Confidence != 0.92 {
		t.Errorf("正常值不该被降级:needsReview=%v confidence=%v", f.NeedsReview, f.Confidence)
	}
}

// 负数不需要基准 —— 任何时候都不可能。库里那条 Z4=-2 就是这么来的。
func TestNegativeReadingFlaggedWithoutBaseline(t *testing.T) {
	store := NewMemStore()
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z4_reading": "-2"})
	issues := flagImplausibleReadings(store, rec)
	if len(issues) != 1 || !strings.Contains(issues[0].Reason, "负数") {
		t.Fatalf("负读数没被抓住:%v", issues)
	}
}

// 读数倒退要报,但不能拦着 —— 换表清零是真实存在的情况,只是要人确认。
func TestReadingWentBackwardsFlagged(t *testing.T) {
	store := NewMemStore()
	prev := zihanEnergyRecord(t, "rec_prev", true, map[string]string{"z1_reading": "60197.924"})
	prev.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(prev); err != nil {
		t.Fatal(err)
	}
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "1002.5"})
	issues := flagImplausibleReadings(store, rec)
	if len(issues) != 1 {
		t.Fatalf("倒退的读数没被抓住:%v", issues)
	}
	if f := recField(t, rec, "z1_reading"); f.Value != "1002.5" {
		t.Errorf("值被改动了:%q —— 换表清零是真实情况,只该提醒不该改", f.Value)
	}
}

// 首次抄表没有基准,不能瞎报 —— 报了等于新装一块表就先给人一个红标。
func TestFirstReadingNoBaselineNoFlag(t *testing.T) {
	store := NewMemStore()
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "60197.924"})
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("没有基准时不该下结论:%v", issues)
	}
}

// 人改过的值不碰 —— 人比这套阈值更有发言权,
// 把人填的打回「需复核」就成了「改了又被系统改回去」。
func TestHumanEditedReadingNotFlagged(t *testing.T) {
	store := NewMemStore()
	prev := zihanEnergyRecord(t, "rec_prev", true, map[string]string{"z1_reading": "1000"})
	prev.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(prev); err != nil {
		t.Fatal(err)
	}
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "99999999"})
	f := recField(t, rec, "z1_reading")
	f.Source = "human-edited"
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("人改的值被系统打回了:%v", issues)
	}
	if f.NeedsReview {
		t.Error("人改的值不该被强制复核")
	}
}

// 草稿不能当基准:草稿里存的是没核对过的 AI 原始值,
// 拿它校验下一次,等于用一个可能本身就错的数当尺子。
func TestDraftIsNotUsedAsBaseline(t *testing.T) {
	store := NewMemStore()
	draft := zihanEnergyRecord(t, "rec_draft", false, map[string]string{"z1_reading": "60197924"})
	draft.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(draft); err != nil {
		t.Fatal(err)
	}
	// 真值远小于那条草稿;如果草稿被当成基准,这次就会被误判成「倒退」
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "60197.924"})
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("草稿被当成基准了:%v", issues)
	}
}

// 别的点位的读数不能当基准 —— 几栋楼各一套表时,互相校验两边都会报异常。
func TestOtherPointNotUsedAsBaseline(t *testing.T) {
	store := NewMemStore()
	other := zihanEnergyRecord(t, "rec_other", true, map[string]string{"z1_reading": "100"})
	other.PointID = "pt_another_building"
	other.PointName = "另一栋"
	other.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(other); err != nil {
		t.Fatal(err)
	}
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "60197.924"})
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("拿了别的点位当基准:%v", issues)
	}
}

// 上一条没填这个字段(那块表没拍到)时,要继续往前找,不能就此断掉。
func TestBaselineSkipsRecordsMissingTheField(t *testing.T) {
	store := NewMemStore()
	old := zihanEnergyRecord(t, "rec_old", true, map[string]string{"z1_reading": "55967.844"})
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	if err := store.CreateRecord(old); err != nil {
		t.Fatal(err)
	}
	blank := zihanEnergyRecord(t, "rec_blank", true, map[string]string{"z2_reading": "84363.520"})
	blank.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(blank); err != nil {
		t.Fatal(err)
	}
	rec := zihanEnergyRecord(t, "rec_now", false, map[string]string{"z1_reading": "60197924"})
	if issues := flagImplausibleReadings(store, rec); len(issues) != 1 {
		t.Fatalf("上一条缺这个字段就把基准弄丢了:%v", issues)
	}
}

func TestParseReading(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"60197.924", 60197.924, true},
		{" 1992 ", 1992, true},
		{"1992m³", 1992, true},
		{"60197.924kWh", 60197.924, true},
		{"", 0, false},
		{"待抄", 0, false},
		{"NaN", 0, false},
	}
	for _, c := range cases {
		got, ok := parseReading(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseReading(%q) = %v,%v;期望 %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
