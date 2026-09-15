package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// ===== 项目名必须是真实存在的项目 =====
//
// 【这条规则守的是什么】assets.project 存的是项目名字符串,没有外键。
// 后台新增设备时手打一个错别字("紫涵"vs"紫菡"),建出来的设备会落进一个
// 不存在的项目,然后谁都看不见它 —— 项目页按 projects 表列,列不出;
// 台账按人的项目范围裁,裁掉;项目的设备数按名字聚合,数不到。
// 全程不报错,只表现成"我建的设备不见了"。线上真发生过。

func newGuardServer(t *testing.T) *Server {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "guard.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Server{store: store, storeKind: "sqlite"}
}

func seedProject(t *testing.T, s *Server, name string, disabled bool) {
	t.Helper()
	p := &Project{ID: "proj_" + name, TenantID: defaultTenantID, Name: name, Disabled: disabled}
	if err := s.store.CreateProject(p); err != nil {
		t.Fatalf("CreateProject %s: %v", name, err)
	}
	if disabled {
		if err := s.store.UpdateProjectMeta(defaultTenantID, p.ID, "", true); err != nil {
			t.Fatalf("停用 %s: %v", name, err)
		}
	}
}

func TestRegisteredProjectPasses(t *testing.T) {
	s := newGuardServer(t)
	seedProject(t, s, "紫菡雅集", false)
	if err := s.checkProjectRegistered(defaultTenantID, "紫菡雅集"); err != nil {
		t.Errorf("建好的项目却被拦下:%v", err)
	}
	// 前后空格不该算成另一个项目 —— 复制粘贴项目名时很容易带上
	if err := s.checkProjectRegistered(defaultTenantID, "  紫菡雅集 "); err != nil {
		t.Errorf("带空格的同一个项目被拦下:%v", err)
	}
}

// 【最要紧的一条】错别字必须当场拦住,不能建完了才发现设备不见了。
func TestTypoProjectIsRejected(t *testing.T) {
	s := newGuardServer(t)
	seedProject(t, s, "紫菡雅集", false)
	err := s.checkProjectRegistered(defaultTenantID, "紫涵雅集") // 涵 ≠ 菡
	if !errors.Is(err, errUnknownProject) {
		t.Fatalf("错别字项目没被拦住(err=%v)—— 这台设备会建完就谁都看不见", err)
	}
}

// 停用的项目也要拦:它不参与可见范围计算,往里建设备是同一种"建完就看不见"。
func TestDisabledProjectIsRejected(t *testing.T) {
	s := newGuardServer(t)
	seedProject(t, s, "老项目", true)
	if err := s.checkProjectRegistered(defaultTenantID, "老项目"); !errors.Is(err, errProjectDisabled) {
		t.Fatalf("停用项目没被拦住(err=%v)", err)
	}
}

// 一个项目都没有时给的是"这个项目不存在",不是放行 ——
// 放行的话全新部署的第一台设备又会造出幽灵项目。
func TestNoProjectsMeansReject(t *testing.T) {
	s := newGuardServer(t)
	if err := s.checkProjectRegistered(defaultTenantID, "随便什么"); !errors.Is(err, errUnknownProject) {
		t.Fatalf("空项目表却放行了(err=%v)", err)
	}
}
