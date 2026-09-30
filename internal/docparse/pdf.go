package docparse

// pdf 提取：纯 Go（ledongthuc/pdf），按页返回文本（移植自 dev/pdf）。
// 不支持扫描版 OCR 与加密 PDF。

import (
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

func extractPDF(path string, onlyPage int) (out string, err error) {
	// 库解析损坏文件时可能 panic，统一转成 error
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("文件解析失败，可能不是有效的 PDF: %v", r)
		}
	}()

	f, r, err := pdf.Open(path)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "encrypt") {
			return "", fmt.Errorf("PDF 已加密，暂不支持: %w", err)
		}
		return "", err
	}
	defer f.Close()

	total := r.NumPage()
	if total <= 0 {
		return "", fmt.Errorf("PDF 无页面")
	}
	if onlyPage > 0 && onlyPage > total {
		return "", fmt.Errorf("page=%d 超出范围（共 %d 页）", onlyPage, total)
	}
	start, end := 1, total
	if onlyPage > 0 {
		start, end = onlyPage, onlyPage
	}

	var b strings.Builder
	for i := start; i <= end; i++ {
		if b.Len() > defaultMaxChars*2 {
			b.WriteString("…（长文档，仅展示部分）\n")
			break
		}
		text, err := r.Page(i).GetPlainText(nil)
		if err != nil {
			fmt.Fprintf(&b, "--- 第 %d 页（提取失败: %v）---\n", i, err)
			continue
		}
		text = strings.TrimSpace(text)
		fmt.Fprintf(&b, "--- 第 %d 页 ---\n", i)
		if text == "" {
			b.WriteString("（本页无可提取文本，可能是扫描图片页）\n")
			continue
		}
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}
