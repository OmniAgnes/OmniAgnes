package tools

import (
	"unicode/utf8"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestToUTF8(t *testing.T) {
	// 中文 Windows 上 PowerShell 的典型输出：按 ANSI 码页(GBK)编码的字节
	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("找到 21 个匹配的文本文件"))
	if err != nil {
		t.Fatalf("构造 GBK 样本失败: %v", err)
	}

	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"空输入", nil, ""},
		{"已是 UTF-8 原样保留", []byte("你好世界 🚀"), "你好世界 🚀"},
		{"GBK 字节转成中文", gbkBytes, "找到 21 个匹配的文本文件"},
		{"混入 CRLF 的 GBK", append(append([]byte{}, gbkBytes...), '\r', '\n'), "找到 21 个匹配的文本文件\r\n"},
	}
	for _, c := range cases {
		got := toUTF8(c.in)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if got != "" && !utf8.ValidString(got) {
			t.Errorf("%s: 输出不是合法 UTF-8", c.name)
		}
	}
}

// 关键回归：无论子进程吐什么编码，都不能产出非法 UTF-8，
// 否则 json.Marshal 会把它永久写成 U+FFFD 污染会话文件。
func TestToUTF8AlwaysValid(t *testing.T) {
	inputs := [][]byte{
		{0xff, 0xfe, 0x00, 0x01},
		{0x80, 0x81, 0x82},
		{0xc4, 0xe3, 0xba, 0xc3},
		{0xed, 0xa0, 0x80}, // 孤立代理项
	}
	for _, in := range inputs {
		if got := toUTF8(in); !utf8.ValidString(got) {
			t.Errorf("输入 %v 转码后仍非合法 UTF-8: %q", in, got)
		}
	}
}
