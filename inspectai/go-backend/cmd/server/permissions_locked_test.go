package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 锁定能力:操作日志、提示词模板只给系统管理员。
//
// 【最要紧的一条是"存量库里的旧值必须被覆盖"】
// 加锁只改了默认值和保存路径的话,老库里早就存着
// 「audit_view → 经理、主管」这几行,启动时原样读进来,加锁等于没加 ——
// 代码看着对、线上照旧,而且全程不报错。这类"改了不生效"最难发现。

func TestLockedPermsOverrideStoredMatrix(t *testing.T) {
	store := NewMemStore()
	// 模拟升级前的库:两项都发给了经理和主管
	if err := store.ReplaceRolePermissions(map[string][]string{
		"audit_view":      {"manager", "supervisor"},
		"prompt_manage":   {"manager", "supervisor"},
		"approval_review": {"manager", "supervisor"},
	}); err != nil {
		t.Fatal(err)
	}
	srv := &Server{store: store}
	if err := srv.loadPermissions(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"audit_view", "prompt_manage"} {
		for _, role := range []string{"manager", "supervisor", "inspector"} {
			if srv.permCache.allowed(key, role) {
				t.Errorf("%s 仍然开给了 %s —— 存量库里的旧值没被覆盖,加锁形同虚设", key, role)
			}
		}
	}
	// 没锁定的能力不受影响,别一刀切
	if !srv.permCache.allowed("approval_review", "supervisor") {
		t.Error("未锁定的能力被误伤了")
	}
}

// 保存矩阵时不能把锁定项发出去(包括直接构造请求绕过前端)。
func TestSavePermissionsIgnoresLockedKeys(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	if err := srv.loadPermissions(); err != nil {
		t.Fatal(err)
	}
	// 同一个请求里放一个【没锁定】的能力,用来证明这条路本身是通的 ——
	// 否则万一保存整体失败了,上面的断言会"通过",但什么都没证明。
	body := `{"matrix":{"audit_view":["manager"],"prompt_manage":["inspector"],"approval_review":["supervisor"]}}`
	r := httptest.NewRequest(http.MethodPut, "/api/permissions", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSavePermissions(w, r)

	for _, key := range []string{"audit_view", "prompt_manage"} {
		for _, role := range []string{"manager", "supervisor", "inspector"} {
			if srv.permCache.allowed(key, role) {
				t.Errorf("通过接口把锁定项 %s 发给了 %s", key, role)
			}
		}
	}
	if !srv.permCache.allowed("approval_review", "supervisor") {
		t.Fatal("未锁定的能力也没保存成功 —— 上面的断言证明不了任何事")
	}
}

// admin 永远通过 —— 锁定项对他不设限,否则系统管理员自己也看不了审计。
func TestAdminStillPassesLockedPerms(t *testing.T) {
	srv := &Server{store: NewMemStore()}
	if err := srv.loadPermissions(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"audit_view", "prompt_manage"} {
		if !srv.permCache.allowed(key, roleAdmin) {
			t.Errorf("%s 连 admin 都不通过 —— 系统管理员自己也看不了了", key)
		}
	}
}
