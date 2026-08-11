package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 截断必须按【字符】,不能按字节。
//
// 线上企业微信群里出现过 "AI建议:立即补拍KT-5电梯使用登记标志特写照片,并确认
// 其是否在有效期内??.." —— 结尾那两个方块就是一个汉字被从中间劈开的残骸。
// 这类 bug 不报错、不进日志,只有人盯着群消息时才会发现,所以必须有测试守。
//
// "偶尔"出现的原因也在这里:纯中文时 90 字节正好是 30 个整字,不会劈;
// 建议里混进 "KT-5" 这类 ASCII,边界一偏移就落进汉字中间。

func TestTruncateNeverSplitsChineseChars(t *testing.T) {
	// 逐个长度扫一遍,任何一个截断点都不能产出无效 UTF-8
	src := "立即补拍KT-5电梯使用登记标志特写照片,并确认其是否在有效期内"
	for n := 1; n <= utf8.RuneCountInString(src)+5; n++ {
		got := truncate(src, n)
		if !utf8.ValidString(got) {
			t.Fatalf("truncate(src, %d) 产出了无效 UTF-8: %q", n, got)
		}
		if strings.ContainsRune(got, utf8.RuneError) {
			t.Fatalf("truncate(src, %d) 里出现了替换字符(乱码): %q", n, got)
		}
	}
}

func TestTruncateCountsRunesNotBytes(t *testing.T) {
	// 10 个汉字 = 30 字节。按字节截会砍成 3 个字,按字符截应原样保留。
	src := "一二三四五六七八九十"
	if got := truncate(src, 10); got != src {
		t.Fatalf("10 个字截到 10 应原样返回,得到 %q —— 还在按字节数", got)
	}
	if got := truncate(src, 4); got != "一二三四…" {
		t.Fatalf("截到 4 个字应为 \"一二三四…\",得到 %q", got)
	}
}

func TestTruncateLeavesShortStringsAlone(t *testing.T) {
	for _, s := range []string{"", "abc", "正常", "KT-5 电梯"} {
		if got := truncate(s, 90); got != s {
			t.Errorf("未超长的 %q 被改成了 %q", s, got)
		}
	}
}

// 混合 ASCII 与中文是线上真实形状:边界最容易落在汉字中间。
func TestTruncateMixedAsciiAndChinese(t *testing.T) {
	src := "KT-5 电梯使用登记标志已过期,请立即联系维保单位换证并补拍照片留档"
	got := truncate(src, 20)
	if !utf8.ValidString(got) || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("混合文本截断出现乱码: %q", got)
	}
	// 20 个字符 + 省略号
	if n := utf8.RuneCountInString(got); n != 21 {
		t.Fatalf("应为 20 字 + 省略号 = 21 个字符,得到 %d: %q", n, got)
	}
}
