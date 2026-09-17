package main

import "strings"

// ===== 计划的多位负责人 =====
//
// 【为什么要多位】每日提醒按负责人点名(daily_push.go 里「负责人:设备…」那一行)。
// 一条计划只能写一个人,可现场是几个人轮着巡同一批设备 —— 提醒只点了一个人的名,
// 其他人看到消息会以为"不关我事"。
//
// 【负责人不决定谁能巡】这一点不变:谁能在手机上看到计划按项目权限,
// 算不算完成按设备有没有被巡过。负责人只回答"没巡完找谁"。
//
// 【Owners 是唯一事实来源,OwnerName / OwnerID 是跟着它算出来的】
// 那两个老字段不删:群提醒、计划列表、手机端、派任务时的默认执行人都在读
// OwnerName。让它变成「周新宇、苑文涛」这种合并写法,那些地方一行不改
// 就能显示多个人 —— 比逐处改成读列表少漏很多地方。
//
// 【同步只在一个地方做】syncPlanOwners 在两种存储写入时(normalizeEngineeringPlan)
// 和读出时(scanEngineeringPlan)各跑一次。散在各处自己拼名字的话,
// 迟早有一处拼法不一样,表现是列表里和提醒里写的负责人对不上。

// PlanOwner 一位负责人。
type PlanOwner struct {
	// ID 账号 ID。外委人员没有账号,就空着 —— 名字照常显示和点名。
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

// planOwnerSep 合并写法里的分隔符。和界面上列人名的习惯一致。
const planOwnerSep = "、"

// syncPlanOwners 让负责人列表和两个老字段说同一句话。
//
//  1. 列表是空的、老字段有名字 → 这是老数据或老客户端,从老字段生成一人列表。
//     【不按「、」拆开】历史上手打的「张三、李四」可能就是一个外委班组的叫法,
//     拆开会凭空多出两个"人"。
//  2. 去空、去重:有账号的按账号去重,没账号的按名字去重 ——
//     同一个人选两次,提醒里就会出现两遍他的名字。
//  3. 老字段跟着列表重算:OwnerName = 名字用「、」连起来,
//     OwnerID = 第一位有账号的人(「负责人绑定」工具靠它判断这条是否已绑)。
func syncPlanOwners(item *EngineeringPlanItem) {
	if item == nil {
		return
	}
	if len(item.Owners) == 0 {
		// 【名字或账号有一个就算有负责人】只看名字的话,只传了 ownerId 的
		// 老客户端会被静默丢掉 ID —— 于是不存在的账号、看不到项目的人都能
		// 存进去,因为后面的校验根本没轮到它。第一版就这么写错了,
		// TestSavePlanRejectsUnknownOwnerID 抓出来的。
		name := strings.TrimSpace(item.OwnerName)
		id := strings.TrimSpace(item.OwnerID)
		if name != "" || id != "" {
			item.Owners = []PlanOwner{{ID: id, Name: name}}
		}
	}

	clean := make([]PlanOwner, 0, len(item.Owners))
	seenID := map[string]bool{}
	seenName := map[string]bool{}
	for _, o := range item.Owners {
		o.ID = strings.TrimSpace(o.ID)
		o.Name = strings.TrimSpace(o.Name)
		if o.ID == "" && o.Name == "" {
			continue
		}
		if o.ID != "" {
			if seenID[o.ID] {
				continue
			}
			seenID[o.ID] = true
		} else {
			key := ownerNameKey(o.Name)
			if seenName[key] {
				continue
			}
			seenName[key] = true
		}
		clean = append(clean, o)
	}
	item.Owners = clean

	names := make([]string, 0, len(clean))
	firstID := ""
	for _, o := range clean {
		if o.Name != "" {
			names = append(names, o.Name)
		}
		if firstID == "" && o.ID != "" {
			firstID = o.ID
		}
	}
	item.OwnerName = strings.Join(names, planOwnerSep)
	item.OwnerID = firstID
}

// planHasOwner 这条计划的负责人里有没有这个名字(按人比,不是按合并后的整串比)。
//
// 【为什么不能再用 OwnerName == filter】有了多位负责人之后 OwnerName 是
// 「周新宇、苑文涛」,按「周新宇」筛就筛不出来 —— 表现是"我明明是负责人,
// 按我的名字筛却没有这条"。
func planHasOwner(item *EngineeringPlanItem, name string) bool {
	key := ownerNameKey(name)
	if key == "" {
		return false
	}
	for _, o := range item.Owners {
		if ownerNameKey(o.Name) == key {
			return true
		}
	}
	return false
}
