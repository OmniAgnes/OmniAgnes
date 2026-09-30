// Package docparse 从常见办公文档（docx/pdf/pptx/xlsx）里提取文本，
// 供 read 工具按扩展名自动调用——用户无需配置任何 MCP 或外部依赖。
package docparse

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Options 是可选的提取参数（零值 = 全部页/表，默认截断）。
type Options struct {
	Page     int    // PDF：只读这一页（从 1 起）；0 = 全部
	Sheet    string // xlsx：工作表名；空 = 列出全部工作表
	MaxChars int    // 输出最大字符数；0 = 默认 40000
}

const defaultMaxChars = 40000

// Supported 报告该扩展名是否支持提取。
func Supported(ext string) bool {
	switch strings.ToLower(ext) {
	case ".docx", ".pdf", ".pptx", ".xlsx", ".xlsm":
		return true
	}
	return false
}

// Extract 按扩展名提取文档文本。
func Extract(path string, opt Options) (string, error) {
	if opt.MaxChars <= 0 {
		opt.MaxChars = defaultMaxChars
	}
	var (
		text string
		err  error
	)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		text, err = extractDocx(path)
	case ".pdf":
		text, err = extractPDF(path, opt.Page)
	case ".pptx":
		text, err = extractPPTX(path)
	case ".xlsx", ".xlsm":
		text, err = extractXLSX(path, opt.Sheet)
	default:
		return "", fmt.Errorf("docparse: 不支持的格式 %s", filepath.Ext(path))
	}
	if err != nil {
		return "", err
	}
	if len(text) > opt.MaxChars {
		text = text[:opt.MaxChars] + "\n…（内容过长已截断）"
	}
	return text, nil
}
