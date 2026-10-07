// write_pdf：从纯文本生成 PDF（A4 竖版，CJK 支持）。
// 用 Adobe 预定义 CID 字体 STSong-Light（UniGB-UCS2-H），无需嵌入字体文件；
// 附 ToUnicode CMap，文本可被 PDF 阅读器与本仓 read_pdf 提取。
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	pdfPageW   = 595 // A4 竖版（pt）
	pdfPageH   = 842
	pdfMargin  = 72
	baseSize   = 12.0
	lineGap    = 4.0
	titleSize  = 22.0
	bulletLead = "•  " // 项目符号前缀（约 2 全角宽）
)

type writePDFArgs struct {
	Path      string `json:"path" jsonschema:"目标 .pdf 路径（相对路径基于 MCP 进程工作目录）"`
	Title     string `json:"title,omitempty" jsonschema:"文档标题（第一页大字）"`
	Content   string `json:"content" jsonschema:"正文，\\n 分行；以 - 或 * 开头的行渲染为项目符号；空行留白"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema:"目标已存在时是否覆盖"`
}

func writePDF(ctx context.Context, req *mcp.CallToolRequest, args writePDFArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if strings.TrimSpace(args.Content) == "" && strings.TrimSpace(args.Title) == "" {
		return nil, nil, fmt.Errorf("title 与 content 不能同时为空")
	}
	path, err := resolvePathParent(args.Path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(path); err == nil && !args.Overwrite {
		return nil, nil, fmt.Errorf("write_pdf: 文件已存在: %s（overwrite=true 可覆盖）", path)
	}
	pages, err := buildPDF(args)
	if err != nil {
		return nil, nil, fmt.Errorf("write_pdf: %w", err)
	}
	if err := os.WriteFile(path, pages.doc, 0o644); err != nil {
		return nil, nil, err
	}
	return textResult(fmt.Sprintf("已生成 %s（%d 页；中文按 STSong-Light 渲染）", path, pages.count)), nil, nil
}

type pdfLine struct {
	text string
	size float64
}

func buildLines(args writePDFArgs) []pdfLine {
	maxW := float64(pdfPageW - 2*pdfMargin)
	var lines []pdfLine
	if t := strings.TrimSpace(args.Title); t != "" {
		lines = append(lines, pdfLine{text: t, size: titleSize})
		lines = append(lines, pdfLine{size: baseSize}) // 标题后留白
	}
	for _, raw := range strings.Split(args.Content, "\n") {
		trimmed := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(trimmed) == "" {
			lines = append(lines, pdfLine{size: baseSize})
			continue
		}
		lead := ""
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			lead = bulletLead
			trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "- "), "* ")
		}
		for _, l := range wrapText(lead+trimmed, baseSize, maxW-float64(len([]rune(lead)))*baseSize) {
			lines = append(lines, pdfLine{text: l, size: baseSize})
		}
	}
	if len(lines) == 0 {
		lines = []pdfLine{{}}
	}
	return lines
}

// wrapText 按估算宽度折行：全角字符 ≈ size，半角 ≈ 0.55*size。
func wrapText(s string, size, maxW float64) []string {
	full, half := size, size*0.55
	var lines []string
	cur, curW := "", 0.0
	for _, r := range s {
		w := half
		if r >= 0x1100 {
			w = full
		}
		if curW+w > maxW && cur != "" {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		cur += string(r)
		curW += w
	}
	lines = append(lines, cur)
	return lines
}

type pdfDoc struct {
	doc   []byte
	count int
}

func buildPDF(args writePDFArgs) (pdfDoc, error) {
	lines := buildLines(args)

	// 逐行填页：行高按 size+lineGap，整页放不下就换页
	var paged [][]pdfLine
	used := 0.0
	cur := []pdfLine{}
	for _, ln := range lines {
		h := ln.size + lineGap
		if len(cur) > 0 && used+h > (pdfPageH-2*pdfMargin) {
			paged = append(paged, cur)
			used, cur = 0.0, nil
		}
		used += h
		cur = append(cur, ln)
	}
	paged = append(paged, cur)

	// 收集用到的码位（ToUnicode 映射用）
	usedCP := map[rune]bool{}
	for _, ln := range lines {
		for _, r := range ln.text {
			usedCP[r] = true
		}
	}

	// 每页内容流
	streams := make([]string, len(paged))
	for i, page := range paged {
		var b strings.Builder
		y := pdfPageH - pdfMargin
		for _, ln := range page {
			y -= int(ln.size + lineGap)
			if ln.text == "" {
				continue
			}
			var hxs []string
			for _, r := range ln.text {
				hxs = append(hxs, fmt.Sprintf("%04X", cp(r)))
			}
			b.WriteString(fmt.Sprintf("BT /F1 %.0f Tf %d %d Td <%s> Tj ET\n",
				ln.size, pdfMargin, y, strings.Join(hxs, "")))
		}
		streams[i] = b.String()
	}

	// ToUnicode CMap（bfchar：CID=Unicode 值，identity）
	var cmap strings.Builder
	cmap.WriteString("/CIDInit /CMap def resource begin\n")
	cmap.WriteString(fmt.Sprintf("%d beginbfchar\n", len(usedCP)))
	for r := range usedCP {
		cmap.WriteString(fmt.Sprintf("<%04X> <%04X> \n", cp(r), cp(r)))
	}
	cmap.WriteString("endbfchar\nendcmap\n")

	// 对象编号：1 Catalog，2 Pages，页 i → 3+2i / 4+2i，字体 3+2P / 4+2P / 5+2P
	numPages := len(streams)
	type0, cid, uni := 3+2*numPages, 4+2*numPages, 5+2*numPages
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
	}
	var kids []string
	for i := 0; i < numPages; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+2*i))
	}
	objs = append(objs, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), numPages))
	for i, s := range streams {
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d %d] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>",
				pdfPageW, pdfPageH, 4+2*i, type0),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len([]byte(s)), s),
		)
	}
	objs = append(objs,
		fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /STSong-Light /Encoding /UniGB-UCS2-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", cid, uni),
		"<< /Type /Font /Subtype /CIDFontType0 /BaseFont /STSong-Light /CIDSystemInfo << /Registry (Adobe) /Ordering (GB1) /Supplement 2 >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len([]byte(cmap.String())), cmap.String()),
	)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objs))
	for i, body := range objs {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return pdfDoc{doc: buf.Bytes(), count: len(streams)}, nil
}

// cp 码位：非 BMP 字符（emoji 等）超出 GB1 CID 空间，用 U+FFFD 占位。
func cp(r rune) int {
	if r >= 0x10000 {
		return 0xFFFD
	}
	return int(r)
}

// resolvePathParent 解析写入目标路径，仅要求父目录存在（不存在则创建）。
func resolvePathParent(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	return abs, nil
}
