// docx MCP 服务器：提取 .docx 正文（段落 + 表格，按文档顺序），输出 markdown。
// word/document.xml 是 zip 内的 OOXML，用标准库 archive/zip + encoding/xml 解析，无第三方依赖。
// 不支持老 .doc（OLE 二进制）格式。
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxChars = 40000

type readDocxArgs struct {
	Path     string `json:"path" jsonschema:"docx 文件路径（相对路径基于 MCP 进程工作目录）"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"输出最大字符数，默认 40000，超出截断"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-docx", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_docx",
		Description: "提取 docx 正文文本：段落、标题（映射为 markdown #）、表格（markdown 表格），按文档顺序输出。不支持老 .doc 格式。",
		Annotations: &mcp.ToolAnnotations{
			Title:        "读取 docx",
			ReadOnlyHint: true,
		},
		}, readDocx)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_docx",
		Description: "生成 .docx：传入 title 与内容块（heading/段落/列表/表格），输出可在 Word 打开的文件；表格首行自动加粗为表头。",
		Annotations: &mcp.ToolAnnotations{Title: "写入 docx"},
	}, writeDocx)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-docx:", err)
		os.Exit(1)
	}
}

func readDocx(ctx context.Context, req *mcp.CallToolRequest, args readDocxArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	path, err := resolvePath(args.Path)
	if err != nil {
		return nil, nil, err
	}
	text, err := extractDocx(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read_docx: %w", err)
	}
	if text == "" {
		return nil, nil, fmt.Errorf("read_docx: 文档没有可提取的正文")
	}
	if args.MaxChars > 0 && len(text) > args.MaxChars {
		text = text[:args.MaxChars] + fmt.Sprintf("\n…（已按 max_chars=%d 截断）", args.MaxChars)
	} else if len(text) > maxChars {
		text = text[:maxChars] + "\n…（已截断）"
	}
	return textResult(text), nil, nil
}

// textResult 以纯文本内容返回（不用 structured content，避免客户端拿到 JSON 编码文本）。
func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// docResult 返回说明文本 + 产物 file:// 资源链接：内核会把链接转成本地附件，
// 前端随图片/媒体同区展示，用户可直接打开（与生成图片一致的展示位置）。
func docResult(text, path, mime string) *mcp.CallToolResult {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	name := filepath.Base(abs)
	return &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: text},
		&mcp.ResourceLink{
			URI:      (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(abs)}).String(),
			Name:     name,
			Title:    name,
			MIMEType: mime,
		},
	}}
}

// extractDocx 解包 docx 并按文档顺序抽取段落与表格。
func extractDocx(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("打开失败，可能不是 .docx（老 .doc 为二进制格式，暂不支持）: %w", err)
	}
	defer zr.Close()

	var data []byte
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			data, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			break
		}
	}
	if data == nil {
		return "", fmt.Errorf("不是有效的 docx（缺少 word/document.xml）")
	}

	w := &docxWalker{}
	if err := w.walk(data); err != nil {
		return "", err
	}
	return strings.TrimSpace(w.out.String()), nil
}

// docxWalker 用 xml token 流按文档顺序重组段落与表格。
type docxWalker struct {
	out strings.Builder

	// 表格外段落
	inPara  bool
	heading int // 0=普通段落，1-6=markdown 标题级别
	para    strings.Builder

	// 表格（只处理第一层，嵌套表格拍平为文本）
	tblDepth  int
	tableRows []string
	cells     []string
	inCell    bool
	cell      strings.Builder
}

func (w *docxWalker) walk(data []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			w.start(t)
		case xml.EndElement:
			w.end(t)
		case xml.CharData:
			w.text(string(t))
		}
	}
}

func isWord(space string) bool { return strings.Contains(space, "wordprocessingml") }

func (w *docxWalker) start(t xml.StartElement) {
	if !isWord(t.Name.Space) {
		return
	}
	switch t.Name.Local {
	case "p":
		if w.tblDepth > 0 {
			// 单元格内多段：文本用空格拼接
			if w.inCell && w.cell.Len() > 0 {
				w.cell.WriteString(" ")
			}
		} else {
			w.inPara, w.heading = true, 0
			w.para.Reset()
		}
	case "tbl":
		w.tblDepth++
		if w.tblDepth == 1 {
			w.tableRows = nil
		}
	case "tr":
		if w.tblDepth == 1 {
			w.cells = nil
		}
	case "tc":
		if w.tblDepth == 1 {
			w.inCell = true
			w.cell.Reset()
		}
	case "pStyle":
		if w.inPara {
			w.heading = headingLevel(attrVal(t, "val"))
		}
	case "tab":
		w.text("\t")
	case "br", "cr":
		w.text("\n")
	}
}

func (w *docxWalker) end(t xml.EndElement) {
	if !isWord(t.Name.Space) {
		return
	}
	switch t.Name.Local {
	case "p":
		if w.tblDepth > 0 || !w.inPara {
			return
		}
		w.inPara = false
		if line := strings.TrimSpace(w.para.String()); line != "" {
			if w.heading > 0 {
				w.out.WriteString(strings.Repeat("#", w.heading) + " " + line + "\n\n")
			} else {
				w.out.WriteString(line + "\n\n")
			}
		}
	case "tbl":
		if w.tblDepth == 1 {
			w.flushTable()
		}
		if w.tblDepth > 0 {
			w.tblDepth--
		}
	case "tr":
		if w.tblDepth == 1 && len(w.cells) > 0 {
			w.tableRows = append(w.tableRows, "| "+strings.Join(w.cells, " | ")+" |")
		}
	case "tc":
		if w.tblDepth == 1 && w.inCell {
			w.inCell = false
			text := strings.TrimSpace(strings.ReplaceAll(w.cell.String(), "\n", " "))
			w.cells = append(w.cells, strings.ReplaceAll(text, "|", "\\|"))
		}
	}
}

// text 把 w:t 等产生的文本路由到当前容器（段落或单元格）。
func (w *docxWalker) text(s string) {
	if s == "" {
		return
	}
	switch {
	case w.inCell && w.tblDepth > 0:
		w.cell.WriteString(s)
	case w.inPara && w.tblDepth == 0:
		w.para.WriteString(s)
	}
}

// flushTable 把攒好的表格行输出为 markdown 表格（首行作表头）。
func (w *docxWalker) flushTable() {
	if len(w.tableRows) == 0 {
		return
	}
	cols := strings.Count(w.tableRows[0], "|") - 1
	if cols <= 0 {
		return
	}
	var sep strings.Builder
	sep.WriteString("|")
	for i := 0; i < cols; i++ {
		sep.WriteString(" --- |")
	}
	sep.WriteString("\n")
	w.out.WriteString(w.tableRows[0] + "\n" + sep.String())
	for _, row := range w.tableRows[1:] {
		w.out.WriteString(row + "\n")
	}
	w.out.WriteString("\n")
}

func attrVal(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// headingLevel 把 Word 标题样式（Heading1 / heading 1 / Title）映射为 markdown 级别。
func headingLevel(style string) int {
	style = strings.ToLower(strings.TrimSpace(style))
	if style == "title" {
		return 1
	}
	if i := strings.Index(style, "heading"); i >= 0 {
		if n, err := strconv.Atoi(strings.TrimSpace(style[i+len("heading"):])); err == nil {
			if n < 1 {
				n = 1
			}
			if n > 6 {
				n = 6
			}
			return n
		}
	}
	return 0
}

// resolvePath 解析已存在的文件；相对路径基于 MCP 进程工作目录。
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
	return filepath.Clean(abs), nil
}
