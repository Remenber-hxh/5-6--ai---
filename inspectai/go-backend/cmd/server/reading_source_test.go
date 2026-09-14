package main

import "testing"

func imgs(ids ...string) []ImageInfo {
	out := make([]ImageInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, ImageInfo{ID: id})
	}
	return out
}

// 模型给的是「第几张」,存下来必须换成图片 ID。
//
// 【为什么不能存下标】照片能补拍、能删、能重排。存 2 的话,删掉第 1 张之后
// 它就指向了另一张图 —— 确认页会把另一台设备的特写摆在这一行旁边,
// 而且看不出来是错的。摆一张错的图比不摆更糟:人会照着它确认。
func TestReadingSourceStoresImageIDNotIndex(t *testing.T) {
	f := &FieldValue{Code: "z1_reading"}
	got := RecognizedField{ImageIndex: 3, Bbox: []float64{0.1, 0.2, 0.3, 0.4}}
	setReadingSource(f, got, imgs("img_a", "img_b", "img_c"))

	if f.SourceImageID != "img_c" {
		t.Errorf("第 3 张应该是 img_c,得到 %q", f.SourceImageID)
	}
	if len(f.Bbox) != 4 || f.Bbox[0] != 0.1 || f.Bbox[3] != 0.4 {
		t.Errorf("bbox 没存对:%v", f.Bbox)
	}
}

// bbox 和图片下标缺一不可 —— 缺哪个都裁不出那一小块。
// 留半截数据的话,下游得处处判空,总有一处会漏。
func TestReadingSourceClearsBothWhenIncomplete(t *testing.T) {
	cases := []struct {
		name string
		got  RecognizedField
		imgs []ImageInfo
	}{
		{"没有 bbox", RecognizedField{ImageIndex: 1}, imgs("a")},
		{"bbox 不是四个数", RecognizedField{ImageIndex: 1, Bbox: []float64{0.1, 0.2}}, imgs("a")},
		{"下标是 0", RecognizedField{ImageIndex: 0, Bbox: []float64{0, 0, 1, 1}}, imgs("a")},
		{"下标越界", RecognizedField{ImageIndex: 5, Bbox: []float64{0, 0, 1, 1}}, imgs("a")},
		{"一张图都没有", RecognizedField{ImageIndex: 1, Bbox: []float64{0, 0, 1, 1}}, nil},
		{"框越界", RecognizedField{ImageIndex: 1, Bbox: []float64{-0.5, 0, 1.9, 1}}, imgs("a")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &FieldValue{SourceImageID: "脏数据", Bbox: []float64{9, 9, 9, 9}}
			setReadingSource(f, c.got, c.imgs)
			if f.SourceImageID != "" || f.Bbox != nil {
				t.Errorf("应该整个清掉,得到 id=%q bbox=%v", f.SourceImageID, f.Bbox)
			}
		})
	}
}

// 不能把模型返回的切片直接挂上去 —— 那是别处还在用的同一块内存,
// 后面任何一处改动都会顺着指针改到记录里,而且查起来完全没有线索。
func TestReadingSourceCopiesBbox(t *testing.T) {
	src := []float64{0.1, 0.2, 0.3, 0.4}
	f := &FieldValue{}
	setReadingSource(f, RecognizedField{ImageIndex: 1, Bbox: src}, imgs("a"))
	src[0] = 0.99
	if f.Bbox[0] != 0.1 {
		t.Errorf("bbox 跟着原切片一起变了:%v", f.Bbox)
	}
}

// 换一张照片重新识别时,旧的来源必须被覆盖掉,不能留着指向已经不在的图。
func TestReadingSourceOverwritesStale(t *testing.T) {
	f := &FieldValue{SourceImageID: "img_old", Bbox: []float64{0.9, 0.9, 0.95, 0.95}}
	setReadingSource(f, RecognizedField{ImageIndex: 2, Bbox: []float64{0, 0, 0.5, 0.5}}, imgs("img_x", "img_y"))
	if f.SourceImageID != "img_y" {
		t.Errorf("旧来源没被覆盖:%q", f.SourceImageID)
	}
	if f.Bbox[2] != 0.5 {
		t.Errorf("旧 bbox 没被覆盖:%v", f.Bbox)
	}
}
