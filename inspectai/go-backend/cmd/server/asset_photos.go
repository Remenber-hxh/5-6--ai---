package main

import (
	"net/http"
	"strconv"
	"strings"
)

// ===== 历次巡检的照片 =====
//
// 【照片是最有说服力的证据,也是最直观的"趋势"】曲线要人认得读数、
// 理解基线;照片不用 —— 锈迹、渗漏、积灰在两张图之间一眼可见。
// 对非技术的人(领导、甲方、监管)它比任何图表都好使。
//
// 【也是尽职证明的核心】出事时能拿出"我们每月都拍了,当时是这样"。
//
// 【不叫"同机位对比"】拍照的机位由现场的人决定,系统保证不了每次
// 站在同一个位置。叫它"历次巡检照片"是准确的;叫"同机位对比"
// 会让人以为系统做了对齐,那是过度承诺。

type assetPhoto struct {
	RecordID  string `json:"recordId"`
	Path      string `json:"path"`
	At        string `json:"at"`
	Status    string `json:"status,omitempty"`
	Inspector string `json:"inspector,omitempty"`
}

// handleAssetPhotos —— GET /api/assets/{id}/photos
//
// 每次巡检取【第一张】。一次巡检往往拍五张以上(细节图、铭牌、读数特写),
// 全铺出来会把时间线冲散 —— 而要对比的是"这台设备整体看上去怎么样",
// 那正是第一张(现场到位后先拍的全景)。想看全部仍然可以点进那条记录。
func (s *Server) handleAssetPhotos(w http.ResponseWriter, r *http.Request, id string) {
	tenant := s.tenantForRequest(r)
	if _, err := s.store.GetAsset(tenant, id); err != nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 40 {
		// 【上限压得住】这里每条快照要回查一次记录,没有批量接口。
		// 40 次往返已经是详情页能接受的极限;真要更多,该先加批量查询。
		limit = 12
	}

	snaps, err := s.store.ListAssetSnapshots(id, limit, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	out := make([]assetPhoto, 0, len(snaps))
	for _, sn := range snaps {
		if sn == nil || strings.TrimSpace(sn.RecordID) == "" {
			continue
		}
		rec, recErr := s.store.GetRecord(tenant, sn.RecordID)
		if recErr != nil || rec == nil || len(rec.Images) == 0 {
			// 【没有照片的那次直接跳过,不占位】时间线上留一个灰框
			// 只会让人以为图挂了,而事实是那次巡检本来就没传照片。
			continue
		}
		path := rec.Images[0].Path
		if strings.TrimSpace(path) == "" {
			continue
		}
		out = append(out, assetPhoto{
			RecordID:  sn.RecordID,
			Path:      path,
			At:        fmtStamp(sn.CreatedAt),
			Status:    sn.Status,
			Inspector: sn.Inspector,
		})
	}

	// 【按时间正序返回:老的在左,新的在右】时间线要顺着读才看得出变化方向。
	// 快照表是倒序给的(最近的在前),照抄的话最新的排在最左边,
	// 人从左往右看会得到一个反过来的印象。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}

	writeJSON(w, http.StatusOK, map[string]any{"photos": out})
}
