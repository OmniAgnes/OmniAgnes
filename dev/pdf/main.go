// read_pdf MCP 服务器：提取 PDF 文本，按页返回。
// 纯 Go 实现（ledongthuc/pdf），无外部依赖；不支持扫描版 OCR 与加密 PDF。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxChars = 40000

type readPDFArgs struct {
	Path     string `json:"path" jsonschema:"PDF 文件路径（相对路径基于 MCP 进程工作目录）"`
	Page     int    `json:"page,omitempty" jsonschema:"只读这一页（1 起），省略则读全部"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"输出最大字符数，默认 40000，超出截断"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-pdf", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_pdf",
		Description: "提取 PDF 文本，按页返回。仅支持文本型 PDF；扫描件与加密文件会明确报错。",
		Annotations: &mcp.ToolAnnotations{
			Title:        "读取 PDF",
			ReadOnlyHint: true,
		},
		}, readPDF)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_pdf",
		Description: "生成 .pdf：传入 title 与纯文本 content（\\n 分行，-/* 行变项目符号），A4 竖版、支持中文（STSong-Light）。仅文本版式，无表格/图片/样式。path 可省略，省略时按 title 自动命名并存到用户「下载」目录；生成结果会作为附件直接展示给用户。",
		Annotations: &mcp.ToolAnnotations{Title: "写入 PDF"},
	}, writePDF)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-pdf:", err)
		os.Exit(1)
	}
}

func readPDF(ctx context.Context, req *mcp.CallToolRequest, args readPDFArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	path, err := resolvePath(args.Path)
	if err != nil {
		return nil, nil, err
	}

	text, err := extractPDF(path, args.Page)
	if err != nil {
		return nil, nil, fmt.Errorf("read_pdf: %w", err)
	}
	if args.MaxChars > 0 && args.MaxChars < maxChars && len(text) > args.MaxChars {
		text = text[:args.MaxChars] + fmt.Sprintf("\n…（已按 max_chars=%d 截断）", args.MaxChars)
	} else if len(text) > maxChars {
		text = text[:maxChars] + "\n…（已截断，可传 page 分页读取）"
	}
	return textResult(text), nil, nil
}

// textResult 以纯文本内容返回（不用 structured content，避免客户端拿到 JSON 编码文本）。
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

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
		if b.Len() > maxChars*2 {
			b.WriteString("…（长文档，仅展示部分；可传 page=N 逐页读取）\n")
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

// resolvePath 展开并清洗路径；相对路径基于 MCP 进程工作目录。
func resolvePath(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("文件不存在: %s", abs)
	}
	return abs, nil
}
