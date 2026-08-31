package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===== 设备静态档案 =====
//
// 【和"巡检结论"分开两个接口,不是偷懒】
// 名称/状态/摘要是巡检产生的结论 —— 改它等于改一次巡检的判定,
// 所以非管理角色要走审批流(见 handlePatchAsset 上面那行注释)。
// 而厂家、型号、投运日期、维保周期是台账管理数据,和某一次巡检无关,
// 混在一个接口里的话,补一条厂家信息也会被当成"篡改巡检结论"。

// AssetProfilePatch 静态档案的可改字段。
//
// 【全部用指针】要能区分"没传这个字段"和"传了空字符串把它清空" ——
// 不区分的话,前端只想改维保周期,提交上来的空厂家会把已有的厂家抹掉,
// 而且不报错。
type AssetProfilePatch struct {
	Manufacturer         *string `json:"manufacturer,omitempty"`
	Model                *string `json:"model,omitempty"`
	CommissionedAt       *string `json:"commissionedAt,omitempty"`
	LastMaintainedAt     *string `json:"lastMaintainedAt,omitempty"`
	MaintenanceCycleDays *int    `json:"maintenanceCycleDays,omitempty"`
	AssetNote            *string `json:"assetNote,omitempty"`
}

// validProfileDate 空串或 YYYY-MM-DD。
//
// 【格式当场校验,不要存进去再说】存了个"2011年"进去,维保超期的计算
// 会静默失败(解析不了 → 当没填 → 永远不报超期)。人以为填了,
// 系统当没填,而两边都不吭声。
func validProfileDate(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

// handleAssetProfile —— PUT /api/assets/{id}/profile
func (s *Server) handleAssetProfile(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	var p AssetProfilePatch
	if err := decodeJSON(r, &p); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if p.CommissionedAt != nil && !validProfileDate(*p.CommissionedAt) {
		writeError(w, http.StatusBadRequest, "bad_date", "投运日期要写成 YYYY-MM-DD,例如 2011-06-01")
		return
	}
	if p.LastMaintainedAt != nil && !validProfileDate(*p.LastMaintainedAt) {
		writeError(w, http.StatusBadRequest, "bad_date", "上次维保日期要写成 YYYY-MM-DD")
		return
	}
	if p.MaintenanceCycleDays != nil {
		// 上限压一道:填了 36500(一百年)这种值,超期永远不会触发,
		// 而人以为自己配好了周期。
		if *p.MaintenanceCycleDays < 0 || *p.MaintenanceCycleDays > 3650 {
			writeError(w, http.StatusBadRequest, "bad_cycle",
				"维保周期要在 0–3650 天之间(0 = 未设定)")
			return
		}
	}

	asset, err := s.store.UpdateAssetProfile(s.tenantForRequest(r), id, p)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	s.recordOperation(r, "asset_profile_update", "asset", id, map[string]any{
		"assetName": asset.AssetName,
	})
	s.enrichAssetForDisplay(asset)
	writeJSON(w, http.StatusOK, map[string]any{"asset": asset})
}

// maintenanceOverdueDays 距上次维保超了多少天。
//
// 返回 <=0 表示不超期或算不出来。
//
// 【周期为 0 时一律不判超期】0 是"还没填",不是"不用维保" ——
// 当成 0 天周期的话,每台没填周期的设备都会被报成"已超期",
// 满屏红字之后没人再看这条提示。
func maintenanceOverdueDays(a *AssetEntry, now time.Time) int {
	if a == nil || a.MaintenanceCycleDays <= 0 {
		return 0
	}
	last := strings.TrimSpace(a.LastMaintainedAt)
	if last == "" {
		return 0 // 没填上次维保时间,同样算不出来
	}
	t, err := time.Parse("2006-01-02", last)
	if err != nil {
		return 0
	}
	elapsed := int(now.Sub(t).Hours() / 24)
	over := elapsed - a.MaintenanceCycleDays
	if over <= 0 {
		return 0
	}
	return over
}

// assetAgeYears 投运至今多少年。算不出来返回 0。
func assetAgeYears(a *AssetEntry, now time.Time) int {
	if a == nil {
		return 0
	}
	t, err := time.Parse("2006-01-02", strings.TrimSpace(a.CommissionedAt))
	if err != nil {
		return 0
	}
	y := int(now.Sub(t).Hours() / 24 / 365.25)
	if y < 0 {
		return 0
	}
	return y
}

// 供 verdict 用的可读描述
func maintenanceReason(over int) string {
	return "距上次维保已超期 " + strconv.Itoa(over) + " 天"
}
