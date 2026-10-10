package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestAssetSimilarKey(t *testing.T) {
	same := [][2]string{
		{"K07", "K7"}, {"k07", "K7"}, {"K-07", "K7"}, {"K 07", "K7"}, {"KT-01", "KT1"},
		{"Z1能耗表", "Z1"}, {"Z1 能耗表读数", "Z1"}, {"1#", "1"}, {"007", "7"},
	}
	for _, p := range same {
		if assetSimilarKey(p[0]) != assetSimilarKey(p[1]) {
			t.Errorf("「%s」和「%s」应算同一台:%q vs %q", p[0], p[1], assetSimilarKey(p[0]), assetSimilarKey(p[1]))
		}
	}
	diff := [][2]string{{"K10", "K1"}, {"K100", "K1"}, {"Z1", "Z2"}, {"生活水表", "消防水表"}, {"K0", "K"}}
	for _, p := range diff {
		if assetSimilarKey(p[0]) == assetSimilarKey(p[1]) {
			t.Errorf("「%s」和「%s」不该算同一台:都压成了 %q", p[0], p[1], assetSimilarKey(p[0]))
		}
	}
}

// K07 已在台账里,同一项目再建一台 K7 要先提醒;人确认是另一台后照常建。不相干的编号不受影响。
func TestCreateAssetWarnsOnLookalike(t *testing.T) {
	s, tok := newAssetGuardServer(t)
	if got := postAsset(t, s, tok, `{"project":"紫菡雅集","assetKey":"K07","assetName":"K07"}`); got.Code != http.StatusCreated {
		t.Fatalf("建第一台失败:%d %s", got.Code, got.Body.String())
	}
	got := postAsset(t, s, tok, `{"project":"紫菡雅集","assetKey":"K7","assetName":"K7"}`)
	if got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "asset_similar") ||
		!strings.Contains(got.Body.String(), "K07") {
		t.Fatalf("K7 和已有的 K07 看着是同一台,应先提醒:%d %s", got.Code, got.Body.String())
	}
	if got := postAsset(t, s, tok, `{"project":"紫菡雅集","assetKey":"K7","assetName":"K7","confirmSimilar":true}`); got.Code != http.StatusCreated {
		t.Fatalf("人确认是另一台后应照常建:%d %s", got.Code, got.Body.String())
	}
	if got := postAsset(t, s, tok, `{"project":"紫菡雅集","assetKey":"K08","assetName":"K08"}`); got.Code != http.StatusCreated {
		t.Fatalf("不相干的编号不该被拦:%d %s", got.Code, got.Body.String())
	}
}
