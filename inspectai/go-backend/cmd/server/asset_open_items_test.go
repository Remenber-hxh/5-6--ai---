package main

import "testing"

// 「这台设备身上还有没有没了结的事」。
//
// 这块的价值全在:一台挂着未销账异常的设备,不能在它自己的档案页上看着像没事。

func TestTaskClosedOnlyCountsDoneAndCancelled(t *testing.T) {
	for _, s := range []string{"已完成", "已取消"} {
		if !taskClosed(s) {
			t.Errorf("%q 应算已了结", s)
		}
	}
	// 【待整改必须算"未了结"】它是"查出问题、还没修好"——
	// 算成了结的话,一台设备明明有待整改任务,档案页上却干干净净。
	for _, s := range []string{"待执行", "进行中", "待整改", "逾期", ""} {
		if taskClosed(s) {
			t.Errorf("%q 不该算已了结", s)
		}
	}
}

// 【最危险也最不容易发现的组合】最近一次判为异常,却没有任何在办任务:
// 出了问题、没人接手。而两边各自看都很正常 ——
// 台账里它只是一个红标签,任务列表里它根本不存在。
func TestAbnormalStatusNeedsFollowUp(t *testing.T) {
	cases := []struct {
		status, level string
		want          bool
	}{
		{"异常", "", true},
		{"待整改", "", true},
		{"待维修", "", true},
		{"", "danger", true},
		{"", "repair", true},
		{"正常", "", false},
		{"", "normal", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := assetStatusNeedsFollowUp(c.status, c.level); got != c.want {
			t.Errorf("status=%q level=%q → %v,应为 %v", c.status, c.level, got, c.want)
		}
	}
}
