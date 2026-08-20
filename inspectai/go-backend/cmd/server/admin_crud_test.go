package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// 删除是不可逆的,所以守卫失效的代价比别处都大。
// 这里全部走真库(SQLite),不用内存实现 —— 守卫里有事务和 COUNT 子查询。

func newCrudStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "crud.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mkUser(t *testing.T, store *SQLiteStore, username, role string) *User {
	t.Helper()
	u := &User{Username: username, DisplayName: username, RoleCode: role, TenantID: defaultTenantID}
	if err := store.CreateUser(u, "pw-for-test-only"); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

// 【提交过巡检记录的人不能删】记录里存着提交人,人删了记录就成了无主的。
// 台账是给客户看的证据,不能出现查不到人的记录 —— 这种情况该用"停用"。
func TestDeleteUserRefusedWhenHasRecords(t *testing.T) {
	store := newCrudStore(t)
	worker := mkUser(t, store, "worker", roleInspector)
	admin := mkUser(t, store, "boss", roleAdmin)

	// 没有记录时可以删
	spare := mkUser(t, store, "spare", roleInspector)
	if err := store.DeleteUser(spare.ID, admin.ID); err != nil {
		t.Fatalf("没有任何记录的用户应该能删:%v", err)
	}

	if err := store.CreateRecord(&Record{
		ID: "rec_1", TenantID: defaultTenantID, Project: "会议中心",
		Inspector: "worker", InspectorUserID: worker.ID,
		TemplateID: "zihan_energy", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUser(worker.ID, admin.ID); !errors.Is(err, errInUse) {
		t.Fatalf("提交过记录的人被删掉了(err=%v)—— 那条记录会变成无主的", err)
	}
	// 记录还在,人也还在
	if u, err := store.GetUser(worker.ID); err != nil || u == nil {
		t.Fatal("拒绝删除之后用户不该消失")
	}
}

// 【不能把自己删了】
func TestDeleteUserRefusesSelf(t *testing.T) {
	store := newCrudStore(t)
	me := mkUser(t, store, "me", roleAdmin)
	if err := store.DeleteUser(me.ID, me.ID); !errors.Is(err, errDeleteSelf) {
		t.Fatalf("允许删自己了:%v", err)
	}
}

// 【不能删掉最后一个管理员】删完没人进得了后台,而且没有任何提示,
// 只能改库救回来。这种"把自己锁在门外"的操作必须挡住。
//
// 注意:新建的测试库里【没有】种子管理员(种子是服务启动时才建的),
// 所以这里必须自己建够两个 —— 第一版没建,结果第一次删就被拦,
// 我差点以为守卫写错了。
func TestDeleteUserRefusesLastAdmin(t *testing.T) {
	store := newCrudStore(t)
	adminA := mkUser(t, store, "admin_a", roleAdmin)
	adminB := mkUser(t, store, "admin_b", roleAdmin)
	operator := mkUser(t, store, "op", roleInspector)

	// 有两个管理员,删掉一个应该允许
	if err := store.DeleteUser(adminA.ID, operator.ID); err != nil {
		t.Fatalf("还有别的管理员时应该能删:%v", err)
	}
	// 只剩一个了,再删必须挡住
	if err := store.DeleteUser(adminB.ID, operator.ID); !errors.Is(err, errLastAdmin) {
		t.Fatalf("最后一个管理员被删掉了(err=%v)—— 没人能进后台了", err)
	}
	if u, _ := store.GetUser(adminB.ID); u == nil {
		t.Fatal("被拒绝之后管理员不该消失")
	}

	// 【停用的管理员不算数】只剩一个"停用中的管理员"时,
	// 把在用的那个删掉照样会锁死后台。
	stopped := mkUser(t, store, "admin_off", roleAdmin)
	if err := store.SetUserStatus(stopped.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteUser(adminB.ID, operator.ID); !errors.Is(err, errLastAdmin) {
		t.Fatalf("停用的管理员被算成了有效管理员(err=%v)", err)
	}
}

// 【项目下还有设备就不能删】删了项目那些设备就成了指向不存在项目的孤儿。
func TestDeleteProjectRefusedWhenHasAssets(t *testing.T) {
	store := newCrudStore(t)
	p := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	if err := store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAsset(&AssetEntry{
		ID: "会议中心::elevator_no_room::KT-7", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "无机房电梯", AssetKey: "KT-7",
		AssetName: "KT-7", LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProject(defaultTenantID, p.ID); !errors.Is(err, errInUse) {
		t.Fatalf("有设备的项目被删掉了(err=%v)", err)
	}

	// 空项目可以删,而且成员关系要跟着清掉 ——
	// 留着的话那个人就成了"分了项目但查不到",表现是一片空白页
	empty := &Project{TenantID: defaultTenantID, Name: "已交付现场"}
	if err := store.CreateProject(empty); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserProjects(defaultTenantID, "user_x", []string{empty.ID}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProject(defaultTenantID, empty.ID); err != nil {
		t.Fatalf("空项目应该能删:%v", err)
	}
	if ids, _ := store.ListUserProjectIDs(defaultTenantID, "user_x"); len(ids) != 0 {
		t.Fatalf("项目删了但成员关系还在:%v —— 那个人会看到空白页且查不出原因", ids)
	}
}

// 【部门下还有人就不能删】不连带把人的部门置空:那会让一批用户悄悄失去归属。
func TestDeleteDepartmentRefusedWhenHasUsers(t *testing.T) {
	store := newCrudStore(t)
	d, err := store.CreateDepartment("运维部", "")
	if err != nil {
		t.Fatal(err)
	}
	u := &User{Username: "d1", DisplayName: "d1", RoleCode: roleInspector,
		TenantID: defaultTenantID, DepartmentID: d.ID}
	if err := store.CreateUser(u, "pw-for-test-only"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDepartment(d.ID); !errors.Is(err, errInUse) {
		t.Fatalf("还有人的部门被删掉了(err=%v)", err)
	}

	// 默认部门永远不能删:它是新建用户的兜底归属
	if err := store.DeleteDepartment("dept_default"); !errors.Is(err, errInUse) {
		t.Fatalf("默认部门被删掉了(err=%v)—— 之后建的用户会没有部门", err)
	}

	// 空部门可以删
	empty, err := store.CreateDepartment("空部门", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDepartment(empty.ID); err != nil {
		t.Fatalf("空部门应该能删:%v", err)
	}
}

// 部门重名要拒绝:两个同名部门在下拉里根本分不出来。
func TestDepartmentNameUnique(t *testing.T) {
	store := newCrudStore(t)
	if _, err := store.CreateDepartment("运维部", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDepartment("运维部", ""); !errors.Is(err, errDeptNameTaken) {
		t.Fatalf("重名部门被建出来了:%v", err)
	}
	// 改名也不能改成已有的
	other, err := store.CreateDepartment("工程部", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateDepartment(other.ID, "运维部"); !errors.Is(err, errDeptNameTaken) {
		t.Fatalf("改成重名了:%v", err)
	}
	if err := store.UpdateDepartment(other.ID, "工程二部"); err != nil {
		t.Fatalf("正常改名失败:%v", err)
	}
}
