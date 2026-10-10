package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

// handleSimilarAssets —— GET /api/assets/{id}/similar
// 同一项目里和这台"看着是同一台"的设备,给后台"合并到…"列候选用。规则见 asset_similar.go。
func (s *Server) handleSimilarAssets(w http.ResponseWriter, r *http.Request, id string) {
	tenant := s.tenantForRequest(r)
	asset, err := s.store.GetAsset(tenant, id)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	all, err := s.store.ListAssets(tenant)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	out := []*AssetEntry{}
	for _, a := range similarAssets(all, asset.Project, asset.AssetKey, asset.AssetName) {
		if a.ID == asset.ID {
			continue
		}
		s.enrichAssetForDisplay(a)
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

// handleMergeAsset —— POST /api/assets/{id}/merge  {"intoId": "..."}
//
// 把这台(重复登记的那条)并到 intoId 上:历史、任务、计划、修改申请全部改挂过去,再删掉这台。
// 【只许同一项目里合并】不同项目下编号重名是常态(每栋楼都有 K01),跨项目合并会把两台真设备并成一台。
func (s *Server) handleMergeAsset(w http.ResponseWriter, r *http.Request, id string) {
	if !s.requirePermission(w, r, "asset_manage") {
		return
	}
	var req struct {
		IntoID string `json:"intoId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.IntoID = strings.TrimSpace(req.IntoID)
	tenant := s.tenantForRequest(r)
	from, err := s.store.GetAsset(tenant, id)
	if err != nil || from == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "要合并的设备不存在")
		return
	}
	into, err := s.store.GetAsset(tenant, req.IntoID)
	if err != nil || into == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "要保留的那台设备不存在")
		return
	}
	if from.ID == into.ID {
		writeError(w, http.StatusBadRequest, "merge_same_asset", "不能把设备并到它自己")
		return
	}
	if strings.TrimSpace(from.Project) != strings.TrimSpace(into.Project) {
		writeError(w, http.StatusBadRequest, "merge_cross_project",
			"只能合并同一项目里的设备 —— 不同项目下编号一样的,通常是两台真设备")
		return
	}
	if !s.visibilityFor(r).allowsProject(from.Project) {
		writeError(w, http.StatusForbidden, "project_not_visible", "你没有项目「"+from.Project+"」的权限")
		return
	}
	if err := s.store.MergeAsset(tenant, from.ID, into.ID); err != nil {
		if errors.Is(err, errMergeSameAsset) {
			writeError(w, http.StatusBadRequest, "merge_same_asset", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "merge_failed", "合并失败,什么都没改:"+err.Error())
		return
	}
	// 并掉的那台的封面图没人用了;和删除设备一样清掉(两台共用同一张时不动)
	if from.CoverImagePath != "" && from.CoverImagePath != into.CoverImagePath {
		_ = os.Remove(from.CoverImagePath)
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     "merge_asset",
		TargetType: "asset",
		TargetID:   into.ID,
		Detail: map[string]any{
			"fromId": from.ID, "fromName": from.AssetName, "intoName": into.AssetName, "project": into.Project,
		},
	})
	merged, err := s.store.GetAsset(tenant, into.ID)
	if err != nil || merged == nil {
		writeJSON(w, http.StatusOK, map[string]any{"merged": true})
		return
	}
	s.enrichAssetForDisplay(merged)
	writeJSON(w, http.StatusOK, map[string]any{"merged": true, "asset": merged})
}
