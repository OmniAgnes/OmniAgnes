// pptx MCP 服务器：按页提取 .pptx 幻灯片文本（含备注），输出分节文本。
// ppt/slides/slideN.xml 是 zip 内的 OOXML，用标准库 archive/zip + encoding/xml 解析，无第三方依赖。
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
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxChars = 40000

type readPPTXArgs struct {
	Path     string `json:"path" jsonschema:"pptx 文件路径（相对路径基于 MCP 进程工作目录）"`
	Slide    int    `json:"slide,omitempty" jsonschema:"只读这一页（1 起），省略则读全部"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"输出最大字符数，默认 40000，超出截断"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-pptx", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_pptx",
		Description: "按页提取 pptx 文本（含演讲者备注），格式为「=== 第 N 页 ===」。不支持老 .ppt 格式。",
		Annotations: &mcp.ToolAnnotations{
			Title:        "读取 pptx",
			ReadOnlyHint: true,
		},
		}, readPPTX)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_pptx",
		Description: "生成 .pptx：传入 slides（每页 title/bullets/notes），输出 16:9 演示文稿；bullets 为项目符号要点，notes 进演讲者备注。",
		Annotations: &mcp.ToolAnnotations{Title: "写入 pptx"},
	}, writePPTX)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-pptx:", err)
		os.Exit(1)
	}
}

func readPPTX(ctx context.Context, req *mcp.CallToolRequest, args readPPTXArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	path, err := resolvePath(args.Path)
	if err != nil {
		return nil, nil, err
	}
	text, err := extractPPTX(path, args.Slide)
	if err != nil {
		return nil, nil, fmt.Errorf("read_pptx: %w", err)
	}
	if text == "" {
		return nil, nil, fmt.Errorf("read_pptx: 没有可提取的文本")
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

// extractPPTX 按页号顺序提取幻灯片与备注文本。
func extractPPTX(path string, onlySlide int) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", fmt.Errorf("打开失败，可能不是 .pptx（老 .ppt 为二进制格式，暂不支持）: %w", err)
	}
	defer zr.Close()

	slides := map[int]string{} // 页号 -> ppt/slides/slideN.xml
	notes := map[int]string{}  // 页号 -> notesSlideN.xml
	for _, f := range zr.File {
		if n, ok := slideNumber(f.Name, "ppt/slides/slide"); ok {
			slides[n] = f.Name
			continue
		}
		if n, ok := slideNumber(f.Name, "ppt/notesSlides/notesSlide"); ok {
			notes[n] = f.Name
		}
	}
	if len(slides) == 0 {
		return "", fmt.Errorf("不是有效的 pptx（没有幻灯片）")
	}

	nums := make([]int, 0, len(slides))
	for n := range slides {
		if onlySlide <= 0 || n == onlySlide {
			nums = append(nums, n)
		}
	}
	if len(nums) == 0 {
		return "", fmt.Errorf("slide=%d 超出范围（共 %d 页）", onlySlide, len(slides))
	}
	sort.Ints(nums)

	read := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}

	var b strings.Builder
	for _, n := range nums {
		data, err := read(slides[n])
		if err != nil {
			fmt.Fprintf(&b, "=== 第 %d 页（读取失败: %v）===\n\n", n, err)
			continue
		}
		text := slideText(data)
		fmt.Fprintf(&b, "=== 第 %d 页 ===\n", n)
		if strings.TrimSpace(text) == "" {
			b.WriteString("（本页无文本，可能是纯图片页）\n")
		} else {
			b.WriteString(strings.TrimSpace(text) + "\n")
		}
		if notesFile, ok := notes[n]; ok {
			if noteData, err := read(notesFile); err == nil {
				if note := slideText(noteData); strings.TrimSpace(note) != "" {
					fmt.Fprintf(&b, "备注：%s\n", strings.TrimSpace(note))
				}
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// slideNumber 从 "ppt/slides/slide3.xml" 形式的名字解析页号。
func slideNumber(name, prefix string) (int, bool) {
	if !strings.HasPrefix(name, prefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(name, prefix)
	rest = strings.TrimSuffix(rest, ".xml")
	if rest == "" || strings.ContainsAny(rest, "/_") {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// slideText 提取一页 XML 里全部 a:t 文本，按 a:p 段落换行、a:br 断行。
func slideText(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false

	var b strings.Builder
	paraDepth := 0 // 在 a:p 内的层级
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if !isDrawingML(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "p":
				paraDepth++
			case "br":
				if paraDepth > 0 {
					b.WriteString("\n")
				}
			case "t":
				inText = true
			}
		case xml.EndElement:
			if !isDrawingML(t.Name.Space) {
				continue
			}
			switch t.Name.Local {
			case "p":
				if paraDepth > 0 {
					b.WriteString("\n")
					paraDepth--
				}
			case "t":
				inText = false
			}
		case xml.CharData:
			if inText && paraDepth > 0 {
				b.Write(t)
			}
		}
	}
	return b.String()
}

func isDrawingML(space string) bool {
	return strings.Contains(space, "drawingml")
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
