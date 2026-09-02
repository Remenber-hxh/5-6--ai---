package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 没提交完的记录。
//
// 【这一屏存在的理由】提交被打断之后,记录留在库里没提交,而手机上
// 一个入口都没有 —— 更糟的是照片也跟着消失(建记录时已从「待处理」认领走)。
// 人既回不到那条记录,也拿不回照片重做,现场白跑一趟。
//
// 所以下面盯的是三件事:找得到、别人看不到、能删掉。

func draftRecord(id, inspector string, submitted bool, at time.Time) *Record {
	return &Record{
		ID: id, TenantID: defaultTenantID, Inspector: inspector,
		TemplateID: "elevator_no_room", TemplateName: "电梯巡检（无机房）",
		Submitted: submitted, CreatedAt: at,
	}
}

// 【最要紧的一条】草稿不能被"最近 N 条"挤掉。
//
// 先取最新 N 条再在内存里挑未提交的,人多干几天活草稿就掉出窗口 ——
// 表现是"我明明有一条没提交完的,列表里就是不显示",而且越忙越容易发生。
func TestDraftsNotCrowdedOutByNewerSubmitted(t *testing.T) {
	store := NewMemStore()
	base := time.Now().Add(-72 * time.Hour)
	// 一条很老的草稿
	if err := store.CreateRecord(draftRecord("draft_old", "朱佳伟", false, base)); err != nil {
		t.Fatal(err)
	}
	// 后面压上 60 条已提交的
	for i := 0; i < 60; i++ {
		rec := draftRecord("done_"+strconv.Itoa(i), "朱佳伟", true, base.Add(time.Duration(i+1)*time.Hour))
		if err := store.CreateRecord(rec); err != nil {
			t.Fatal(err)
		}
	}
	drafts, err := store.ListDraftsByOwner(defaultTenantID, "", "朱佳伟", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != "draft_old" {
		t.Fatalf("老草稿被新提交的挤掉了 —— 越忙越找不回来。实际 %d 条 %v",
			len(drafts), draftIDs(drafts))
	}
}

// 已提交的不算草稿 —— 混进来的话,人会去"删"一条已经进了台账的记录。
func TestDraftsExcludeSubmitted(t *testing.T) {
	store := NewMemStore()
	now := time.Now()
	_ = store.CreateRecord(draftRecord("d1", "朱佳伟", false, now))
	_ = store.CreateRecord(draftRecord("s1", "朱佳伟", true, now))
	drafts, _ := store.ListDraftsByOwner(defaultTenantID, "", "朱佳伟", "", 10)
	if len(drafts) != 1 || drafts[0].ID != "d1" {
		t.Errorf("只该返回未提交的,实际 %v", draftIDs(drafts))
	}
}

// 只看得到自己的。草稿是没做完的活,不是给别人看的数据。
func TestDraftsScopedToOwner(t *testing.T) {
	store := NewMemStore()
	now := time.Now()
	_ = store.CreateRecord(draftRecord("mine", "朱佳伟", false, now))
	_ = store.CreateRecord(draftRecord("theirs", "胡晓恺", false, now))
	drafts, _ := store.ListDraftsByOwner(defaultTenantID, "", "朱佳伟", "", 10)
	if len(drafts) != 1 || drafts[0].ID != "mine" {
		t.Errorf("不该看到别人的草稿,实际 %v", draftIDs(drafts))
	}
}

// 租户隔离先于归属判断 —— 同名巡检员在两个客户那里都存在时不能串。
func TestDraftsRespectTenant(t *testing.T) {
	store := NewMemStore()
	now := time.Now()
	mine := draftRecord("mine", "朱佳伟", false, now)
	other := draftRecord("other_tenant", "朱佳伟", false, now)
	other.TenantID = "tenant_other"
	_ = store.CreateRecord(mine)
	_ = store.CreateRecord(other)
	drafts, _ := store.ListDraftsByOwner(defaultTenantID, "", "朱佳伟", "", 10)
	if len(drafts) != 1 || drafts[0].ID != "mine" {
		t.Errorf("跨租户串了,实际 %v", draftIDs(drafts))
	}
}

// ===== HTTP 链路 =====

func TestDraftsEndpointReturnsOwnDrafts(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet, "/api/inspection/drafts", tokens["inspector_a"])
	if got.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", got.Code, got.Body.String())
	}
	var out struct {
		Drafts []draftBrief `json:"drafts"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	// 【空列表也要是数组】返回 null 的话前端 .map 会炸,
	// 而"没有未提交的记录"恰恰是最常见的情况。
	if !strings.Contains(got.Body.String(), `"drafts":[`) {
		t.Errorf("空列表应序列化成 [],实际 %s", got.Body.String())
	}
}

func TestDraftsEndpointNeedsLogin(t *testing.T) {
	server, _ := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet, "/api/inspection/drafts", "")
	if got.Code == http.StatusOK {
		t.Errorf("未登录不该拿到草稿,实际 code=%d", got.Code)
	}
}

func draftIDs(recs []*Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return out
}
