package docparse

import (
	"strings"
	"testing"
)

// 样例由 dev/gen 生成（dev/testdata），这里验证四种格式都能提取出正文。
func TestExtractSamples(t *testing.T) {
	const dir = "../../dev/testdata/"
	cases := []struct {
		file string
		want []string // 结果必须包含的片段（任一）
	}{
		{"sample.docx", []string{"#", "正文", "标题", "|"}},
		{"sample.pdf", []string{"第 1 页", "PDF", "页"}},
		{"sample.pptx", []string{"第 1 页"}},
		{"sample.xlsx", []string{"工作表"}},
	}
	for _, c := range cases {
		out, err := Extract(dir+c.file, Options{})
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("%s: 提取结果为空", c.file)
			continue
		}
		matched := false
		for _, w := range c.want {
			if strings.Contains(out, w) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("%s: 提取结果不含预期片段 %v（前 120 字符: %.120q）", c.file, c.want, out)
		}
	}
}

func TestExtractXLSXSheet(t *testing.T) {
	out, err := Extract("../../dev/testdata/sample.xlsx", Options{Sheet: "Sheet1"})
	if err != nil {
		t.Fatalf("Sheet1: %v", err)
	}
	if !strings.Contains(out, "|") {
		t.Fatalf("Sheet1 应输出 markdown 表格: %.200q", out)
	}
	// 不存在的工作表要报错并列出可用项
	_, err = Extract("../../dev/testdata/sample.xlsx", Options{Sheet: "不存在"})
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("期望工作表不存在错误: %v", err)
	}
}

func TestSupported(t *testing.T) {
	for _, ext := range []string{".docx", ".pdf", ".pptx", ".xlsx", ".xlsm", ".DOCX"} {
		if !Supported(ext) {
			t.Errorf("%s 应被支持", ext)
		}
	}
	for _, ext := range []string{".doc", ".ppt", ".xls", ".txt"} {
		if Supported(ext) {
			t.Errorf("%s 不应被支持", ext)
		}
	}
}
