package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func newID(prefix string) string {
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixNano(), hex.EncodeToString(buf))
}

func businessRecordNo(id, project, pointID, pointName string, createdAt time.Time) string {
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	return fmt.Sprintf("ZX-%s-%s-%s-%s",
		createdAt.Format("20060102"),
		businessProjectCode(project),
		businessPointCode(pointID, pointName),
		businessIDSuffix(id),
	)
}

func businessProjectCode(project string) string {
	switch strings.TrimSpace(project) {
	case "会议中心":
		return "HYZX"
	case "紫菡雅集":
		return "ZHYJ"
	default:
		return businessCode(project, "PROJ")
	}
}

func businessPointCode(pointID, pointName string) string {
	switch strings.TrimSpace(pointID) {
	case "p_elevator_no_room":
		return "WJDT"
	case "p_elevator_machine_room":
		return "YJDT"
	case "p_escalator":
		return "FT"
	case "p_power_room":
		return "BDS"
	case "p_ups":
		return "UPS"
	case "p_fire_pump":
		return "XFBF"
	case "p_water_pump":
		return "SHSB"
	case "p_hot_water":
		return "RSJF"
	case "p_zihan_energy":
		return "NHCB"
	case "p_zihan_daily":
		return "ZHXJ"
	default:
		return businessCode(firstNonEmpty(pointID, pointName), "POINT")
	}
}

func businessIDSuffix(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return "0000"
	}
	parts := strings.Split(id, "_")
	id = parts[len(parts)-1]
	id = businessCode(id, "0000")
	if len(id) > 4 {
		return id[len(id)-4:]
	}
	return fmt.Sprintf("%04s", id)
}

func businessCode(value, fallback string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		out = fallback
	}
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	replacer := strings.NewReplacer(
		" ", "_", "/", "_", "\\", "_", ":", "_",
		"?", "_", "*", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	out := replacer.Replace(name)
	if len(out) > 80 {
		ext := filepath.Ext(out)
		out = out[:80-len(ext)] + ext
	}
	return out
}

func sanitizeAssetIdent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "default"
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_", "：", "_")
	return replacer.Replace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error":   code,
		"message": message,
	})
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// fieldByCode 找一个字段
func fieldByCode(fields []FieldValue, code string) (*FieldValue, int) {
	for i := range fields {
		if fields[i].Code == code {
			return &fields[i], i
		}
	}
	return nil, -1
}

func fieldValue(fields []FieldValue, code string) string {
	if f, _ := fieldByCode(fields, code); f != nil {
		return strings.TrimSpace(f.Value)
	}
	return ""
}

func statusLevel(status string) string {
	switch strings.TrimSpace(status) {
	case "正常":
		return "normal"
	case "待复核":
		return "warning"
	case "异常":
		return "danger"
	case "待维修":
		return "repair"
	default:
		return "unknown"
	}
}

func statusOrder(status string) int {
	switch statusLevel(status) {
	case "normal":
		return 10
	case "warning":
		return 20
	case "danger":
		return 30
	case "repair":
		return 40
	default:
		return 99
	}
}

func assetIdentFromRecord(rec *Record) string {
	return firstNonEmpty(
		fieldValue(rec.Fields, "asset_no"),
		fieldValue(rec.Fields, "site"),
		rec.PointID,
		rec.PointName,
	)
}

func deriveAssetDisplayKeys(a *AssetEntry) (projectCode, templateID, assetKey string) {
	projectCode = sanitizeAssetIdent(a.Project)
	templateID = strings.TrimSpace(a.TemplateID)
	assetKey = strings.TrimSpace(a.AssetKey)
	parts := strings.Split(a.ID, "::")
	if len(parts) >= 2 && templateID == "" {
		templateID = parts[1]
	}
	if len(parts) >= 3 && assetKey == "" {
		assetKey = strings.Join(parts[2:], "::")
	}
	if assetKey == "" {
		assetKey = sanitizeAssetIdent(a.AssetName)
	}
	return projectCode, templateID, assetKey
}
