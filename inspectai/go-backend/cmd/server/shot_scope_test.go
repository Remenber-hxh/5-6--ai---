package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 照片能看谁的 / 能拿谁的。
//
// 【这块判错不会报错,只会静默越权】看到同事的照片不报错;把同事的照片
// 认领进自己的记录也不报错 —— 原主的「待处理」里少一张,现场证据记到了
// 别人名下,两边都没有任何提示。所以每一条都要钉死。

// ===== 归属名单的语义 =====

// 【最容易搞反的一条】空名单 = 不限,不是"谁都看不到"。
// 在"该收紧"的分支上返回空,后果正好相反 —— 全租户照片一次交出去。
func TestEmptyOwnerListMeansUnrestricted(t *testing.T) {
	if !ownerAllowed(nil, "anyone") {
		t.Error("nil 名单应表示不限")
	}
	if !ownerAllowed([]string{}, "anyone") {
		t.Error("空切片应表示不限")
	}
}

func TestOwnerListFiltersByUploader(t *testing.T) {
	owners := []string{"u1", "u2"}
	if !ownerAllowed(owners, "u1") || !ownerAllowed(owners, "u2") {
		t.Error("名单里的人应放行")
	}
	if ownerAllowed(owners, "u3") {
		t.Error("名单外的人不该放行")
	}
	// 空 user_id(老数据)不能被空字符串意外匹配上
	if ownerAllowed([]string{""}, "") {
		t.Error("空 ID 不该匹配空 ID —— 那会让一条脏数据放开所有老照片")
	}
}

// ===== fail-open =====

// 【原来的 bug】不能看全部、又取不到会话用户时(拿静态令牌调接口),
// 归属条件被短路 → 看到全租户所有照片。而代码意图写的是"巡检员只看自己的"。
func TestPhotosRefuseWhenIdentityUnknown(t *testing.T) {
	server, _ := newRecordAccessTestServer(t)
	// 静态巡检员令牌:能过鉴权,但背后没有会话用户
	server.authToken = "static-inspector-token"

	req := httptest.NewRequest(http.MethodGet, "/api/inspection/offline-shots?pending=1", nil)
	req.Header.Set("X-InspectAI-Token", "static-inspector-token")
	rec := httptest.NewRecorder()
	server.router(rec, req)

	if rec.Code == http.StatusOK {
		t.Errorf("认不出是谁又不能看全部时,必须拒绝而不是把全租户照片交出去(实际 code=%d body=%s)",
			rec.Code, rec.Body.String())
	}
}

// 正常登录的巡检员照常能看(别把口子堵死了)
func TestPhotosStillWorkForLoggedInInspector(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet,
		"/api/inspection/offline-shots?pending=1", tokens["inspector_a"])
	if got.Code != http.StatusOK {
		t.Fatalf("登录的巡检员应能看自己的照片,实际 code=%d body=%s", got.Code, got.Body.String())
	}
}

// ===== 认领归属 =====

func shotOf(id, tenant, owner string) *OfflineShot {
	return &OfflineShot{
		ID: id, TenantID: tenant, UserID: owner,
		// 【幂等键必须唯一】留空的话第二张之后会被当成重放合并掉,
		// 于是测试里"造 5 张"实际只有 1 张 —— 断言就测了个寂寞。
		IdempotencyKey: "idem_" + tenant + "_" + id,
		FileName:       "p.jpg", ImagePath: "", Status: "uploaded",
	}
}

// 【核心】不能把同事的照片认领进自己的记录。
//
// 可见性挡不住这件事:能不能看见是界面问题,能不能拿走是数据问题。
func TestCannotAdoptSomeoneElsesShot(t *testing.T) {
	server, _ := newRecordAccessTestServer(t)
	store := server.store.(*MemStore)
	if _, _, err := store.CreateOfflineShot(shotOf("s_other", defaultTenantID, "user_b")); err != nil {
		t.Fatal(err)
	}
	// user_a 想认领 user_b 的照片
	got, err := server.adoptOfflineShots("rec_x", defaultTenantID, "user_a", []string{"s_other"})
	if err != nil {
		t.Fatalf("不该报错,只该认领不到: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("认领到了别人的照片 %d 张 —— 现场证据会被记到别人名下", len(got))
	}
	// 原主的照片必须原样留着,没被标记消费
	left, _ := store.OfflineShotsByIDs(defaultTenantID, []string{"s_other"})
	if len(left) != 1 || left[0].RecordID != "" {
		t.Errorf("别人的照片被动过了: %+v", left)
	}
}

// 认领归属这条规则本身。直接测,不绕磁盘 ——
// 绕磁盘的版本会因为"文件不存在"提前 continue,于是永远测不到这条判断。
func TestCanAdoptShotRule(t *testing.T) {
	cases := []struct {
		name      string
		owner     string
		shotOwner string
		want      bool
	}{
		{"自己的照片", "user_a", "user_a", true},
		{"同事的照片", "user_a", "user_b", false},
		// 加 user_id 之前上传的老照片:没有归属信息,不能因此让它永远成不了单
		{"老照片没有归属", "user_a", "", true},
		// 本地免鉴权开发模式:认不出是谁,不校验
		{"开发模式不校验", "", "user_b", true},
		{"两边都空", "", "", true},
	}
	for _, c := range cases {
		if got := canAdoptShot(c.owner, c.shotOwner); got != c.want {
			t.Errorf("%s: canAdoptShot(%q,%q)=%v, 期望 %v",
				c.name, c.owner, c.shotOwner, got, c.want)
		}
	}
}

// ===== 项目 → 成员 反查 =====

// 「本项目」那一档要看的是同项目【所有人】的照片。
func TestProjectMembersLookup(t *testing.T) {
	store := NewMemStore()
	if err := store.CreateProject(&Project{
		ID: "p1", TenantID: defaultTenantID, Name: "会议中心",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(&Project{
		ID: "p2", TenantID: defaultTenantID, Name: "紫菡雅集",
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.SetUserProjects(defaultTenantID, "u_hui1", []string{"p1"})
	_ = store.SetUserProjects(defaultTenantID, "u_hui2", []string{"p1"})
	_ = store.SetUserProjects(defaultTenantID, "u_zi", []string{"p2"})

	ids, err := store.ListUserIDsInProjects(defaultTenantID, []string{"会议中心"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatalf("会议中心应有 2 个成员,实际 %v", ids)
	}
	for _, id := range ids {
		if id == "u_zi" {
			t.Error("串到别的项目的人了")
		}
	}

	// 空项目名单返回空,而不是"全部" —— 这一条搞反就是全放开
	none, _ := store.ListUserIDsInProjects(defaultTenantID, nil)
	if len(none) != 0 {
		t.Errorf("没给项目时应返回空名单,实际 %v", none)
	}
}

// 停用的项目不算 —— 和 ListUserProjectNames 的口径保持一致。
func TestProjectMembersSkipDisabledProject(t *testing.T) {
	store := NewMemStore()
	_ = store.CreateProject(&Project{
		ID: "p1", TenantID: defaultTenantID, Name: "已停用项目", Disabled: true,
	})
	_ = store.SetUserProjects(defaultTenantID, "u1", []string{"p1"})
	ids, _ := store.ListUserIDsInProjects(defaultTenantID, []string{"已停用项目"})
	if len(ids) != 0 {
		t.Errorf("停用项目不该带出成员,实际 %v", ids)
	}
}

// 角标和列表必须同一套口径。
//
// 【这两处曾经各写一遍可见性判断】于是改了一边、忘了另一边,
// 角标报 5、点进去只有 2 张 —— 用户看到的是"照片丢了"。
// 现在两边都走 shotOwnersFor,这条测试盯住它别再分叉。
func TestBadgeAndListUseSameScope(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	store := server.store.(*MemStore)
	// user_a 两张、user_b 三张,都未成单
	for _, id := range []string{"a1", "a2"} {
		_, _, _ = store.CreateOfflineShot(shotOf(id, defaultTenantID, "user_a"))
	}
	for _, id := range []string{"b1", "b2", "b3"} {
		_, _, _ = store.CreateOfflineShot(shotOf(id, defaultTenantID, "user_b"))
	}

	// 列表
	got := requestWithToken(server, http.MethodGet,
		"/api/inspection/offline-shots?pending=1", tokens["inspector_a"])
	var listed struct {
		Shots []*OfflineShot `json:"shots"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &listed); err != nil {
		t.Fatalf("解析列表失败: %v body=%s", err, got.Body.String())
	}
	// 角标
	badge := requestWithToken(server, http.MethodGet, "/api/inspection/badge-counts", tokens["inspector_a"])
	var counts struct {
		Shots int `json:"shots"`
	}
	if err := json.Unmarshal(badge.Body.Bytes(), &counts); err != nil {
		t.Fatalf("解析角标失败: %v body=%s", err, badge.Body.String())
	}

	if len(listed.Shots) != 2 {
		t.Errorf("巡检员应只看到自己的 2 张,实际 %d 张 —— 同事的照片漏出去了",
			len(listed.Shots))
	}
	if counts.Shots != len(listed.Shots) {
		t.Errorf("角标 %d 与列表 %d 对不上 —— 用户会以为照片丢了",
			counts.Shots, len(listed.Shots))
	}
}
