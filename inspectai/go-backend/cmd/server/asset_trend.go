package main

import (
	"math"
	"net/http"
	"sort"
	"strconv"
)

// ===== 单台设备的读数趋势 =====
//
// 【这是 AI 巡检相对人工的真正增量】慢性劣化单次巡检永远看不出来:
// 水箱水位每周降一点、电池电压每月低一点、控制柜温度悄悄爬上去 ——
// 每一次巡检看都"正常",连起来看才是问题。
//
// 数据底座早就在了(field_observations,数值字段单独存 value_number),
// 但一直没有接口读它 —— 攒了几千条,没人看得到。这个文件就是把它读出来。

// trendPoint 一次巡检读到的一个数。
type trendPoint struct {
	At       string  `json:"at"`
	Value    float64 `json:"value"`
	RecordID string  `json:"recordId,omitempty"`
	// Outlier 这个点明显偏离历史。
	//
	// 【标出来才有意义】一条曲线上二十个点,人不会逐个比;
	// 而"这次比平时高很多"正是要人看见的那件事。
	Outlier bool `json:"outlier,omitempty"`
}

// trendSeries 一个数值字段的时间序列。
type trendSeries struct {
	FieldKey   string       `json:"fieldKey"`
	FieldLabel string       `json:"fieldLabel"`
	Points     []trendPoint `json:"points"`
	Latest     float64      `json:"latest"`
	// Baseline 平时大约是多少 —— 取中位数,不取均值。
	//
	// 【均值会被离群点拖走】一台常年 0.60 的表突然读到 0.20,均值立刻掉到 0.52,
	// 于是"这次偏离平时多少"被算小了一半。中位数几乎不动,才是"平时"。
	Baseline float64 `json:"baseline"`
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	// Deviation 最新一次相对基线的偏离(百分比)。基线为 0 时不给。
	Deviation *float64 `json:"deviation,omitempty"`
	// Drifting 最新一次落在异常区间外 —— 需要人看一眼
	Drifting bool `json:"drifting,omitempty"`
}

// assetTrendResp 一台设备的全部可画曲线的字段。
type assetTrendResp struct {
	AssetID   string        `json:"assetId"`
	AssetName string        `json:"assetName"`
	Series    []trendSeries `json:"series"`
	// SingleReading 只有一次读数的字段名 —— 有数据但画不出趋势。
	//
	// 【和"没有数值字段"分开说】前者再巡几次就有了,后者是模板压根没配数值字段,
	// 得去改模板。两种情况下人要做的事完全不同,合成一句"暂无趋势"等于什么都没说。
	SingleReading []string `json:"singleReading,omitempty"`
	// HasNumericField 这台设备的模板里到底有没有数值字段
	HasNumericField bool `json:"hasNumericField"`
}

// median 中位数。入参会被排序的副本占用,不改调用方的切片。
func median(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	v := append([]float64(nil), in...)
	sort.Float64s(v)
	n := len(v)
	if n%2 == 1 {
		return v[n/2]
	}
	return (v[n/2-1] + v[n/2]) / 2
}

// trendMinPoints 少于这个点数不算趋势。
//
// 两个点连成一条直线,看着像"在上升",其实只是两次读数不同 ——
// 那不是趋势,是差值。三个点起才谈得上方向。
const trendMinPoints = 3

// buildAssetTrend 把观测明细整理成按字段分组的时间序列。
//
// 【纯函数,不碰数据库】喂进去观测列表就能测 —— 而"什么算漂移"这种判断
// 一旦只能靠真实数据验证,就等于没法验证。
func buildAssetTrend(obs []*FieldObservation, numericFields map[string]string) []trendSeries {
	byField := map[string][]trendPoint{}
	labels := map[string]string{}
	for _, o := range obs {
		if o == nil || o.ValueNumber == nil {
			continue
		}
		// 【只画模板当前声明为数值的字段】模板改过之后,历史里可能留着
		// 已经不是数值的字段。照旧画出来的话,图上会出现一条界面上
		// 根本找不到对应字段的曲线,没人说得清它是什么。
		if _, ok := numericFields[o.FieldKey]; !ok {
			continue
		}
		labels[o.FieldKey] = firstNonEmpty(o.FieldLabel, numericFields[o.FieldKey], o.FieldKey)
		byField[o.FieldKey] = append(byField[o.FieldKey], trendPoint{
			At:       fmtStamp(o.CreatedAt),
			Value:    *o.ValueNumber,
			RecordID: o.RecordID,
		})
	}

	out := make([]trendSeries, 0, len(byField))
	for key, pts := range byField {
		// 【按真实时间排,不按写入顺序】巡检不是等间隔发生的:
		// 同一天三次和三个月三次,照序号画出来一模一样,坡度完全失真。
		sort.Slice(pts, func(i, j int) bool { return pts[i].At < pts[j].At })

		s := trendSeries{FieldKey: key, FieldLabel: labels[key], Points: pts}
		vals := make([]float64, len(pts))
		mn, mx := pts[0].Value, pts[0].Value
		for i, p := range pts {
			vals[i] = p.Value
			mn = math.Min(mn, p.Value)
			mx = math.Max(mx, p.Value)
		}
		s.Min, s.Max = mn, mx
		s.Latest = pts[len(pts)-1].Value
		s.Baseline = median(vals)

		if s.Baseline != 0 {
			d := (s.Latest - s.Baseline) / math.Abs(s.Baseline) * 100
			s.Deviation = &d
		}

		// 标异常点:偏离基线超过带宽。
		//
		// 【必须用中位数 + MAD,不能用均值 + 标准差】离群点会把均值和 σ 一起
		// 撑大,于是带宽也跟着变宽,那个点自己就漏掉了 —— 越极端的读数越容易
		// 被漏掉,正好和需求相反。这不是理论问题:测试里 0.6/0.61/0.6/0.59/0.2
		// 这组数据,均值法算出的带宽是 0.3202,而偏差正好 0.32,差一点点没报出来。
		// 中位数和 MAD 几乎不受那一个点影响。
		//
		// 【MAD 为 0 时要有下限】读数常年一模一样时 MAD = 0,不兜底的话
		// 任何一点点变化都会被标成异常 —— 满屏红点等于没标。用基线的 8% 兜。
		if len(pts) >= trendMinPoints {
			devs := make([]float64, len(vals))
			for i, v := range vals {
				devs[i] = math.Abs(v - s.Baseline)
			}
			// 1.4826:让 MAD 在正态分布下与标准差可比,这是通行系数
			scale := median(devs) * 1.4826
			band := math.Max(3*scale, math.Abs(s.Baseline)*0.08)
			// 基线和波动都是 0(读数恒定为 0)时无法区分信号与噪声,一个都不标 ——
			// 那种情况下标出来的"异常"没有任何依据。
			if band > 0 {
				for i := range s.Points {
					if math.Abs(s.Points[i].Value-s.Baseline) > band {
						s.Points[i].Outlier = true
					}
				}
				s.Drifting = s.Points[len(s.Points)-1].Outlier
			}
		}
		out = append(out, s)
	}
	// 漂移的排前面 —— 这一屏是给人"哪个指标不对劲"用的
	sort.Slice(out, func(i, j int) bool {
		if out[i].Drifting != out[j].Drifting {
			return out[i].Drifting
		}
		return out[i].FieldLabel < out[j].FieldLabel
	})
	return out
}

// handleAssetTrend —— GET /api/assets/{id}/trend
func (s *Server) handleAssetTrend(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	resp, err := s.assetTrendFor(asset, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// assetTrendFor 算出这台设备的读数趋势。
//
// 【抽出来是为了让"结论"和"曲线"用同一份计算】顶部那句结论要说
// "水箱水位较平时低 33%",下面的图要画出那条线 —— 各算一次的话,
// 迟早出现结论和图对不上,而看的人不知道该信哪个。
func (s *Server) assetTrendFor(asset *AssetEntry, limit int) (assetTrendResp, error) {
	// 这台设备的模板声明了哪些数值字段
	numeric := map[string]string{}
	if tpl, ok := templateByID(asset.TemplateID); ok {
		for _, f := range tpl.Fields {
			if f.Kind == "number" {
				numeric[f.Code] = f.Label
			}
		}
	}

	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	obs, err := s.store.ListFieldObservations(asset.ID, "", limit)
	if err != nil {
		return assetTrendResp{}, err
	}

	series := buildAssetTrend(obs, numeric)
	resp := assetTrendResp{
		AssetID:         asset.ID,
		AssetName:       firstNonEmpty(asset.AssetName, asset.AssetKey, asset.ID),
		HasNumericField: len(numeric) > 0,
		Series:          make([]trendSeries, 0, len(series)),
	}
	for _, sr := range series {
		if len(sr.Points) < trendMinPoints {
			// 点太少画不出趋势,但要说出来是哪个字段 ——
			// "再巡两次就有了"和"这个模板没数值字段"是两回事
			resp.SingleReading = append(resp.SingleReading, sr.FieldLabel)
			continue
		}
		resp.Series = append(resp.Series, sr)
	}
	return resp, nil
}
