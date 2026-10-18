// xlsx MCP 服务器：读取 .xlsx/.xlsm 为 markdown 表格，或从行数组生成新文件。
// 基于 excelize（纯 Go）；不支持 .xls 老格式（BIFF 二进制）。
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/xuri/excelize/v2"
)

const (
	defaultMaxRows = 500
	maxCellWidth   = 80 // 单元格文本超长截断，避免 markdown 行爆掉
)

type readXLSXArgs struct {
	Path    string `json:"path" jsonschema:"xlsx/xlsm 文件路径（相对路径基于 MCP 进程工作目录）"`
	Sheet   string `json:"sheet,omitempty" jsonschema:"工作表名；省略则列出全部工作表及行列数"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"最多输出的行数，默认 500"`
}

type writeXLSXArgs struct {
	Path      string `json:"path,omitempty" jsonschema:"目标文件路径；省略则按首个 sheet 名命名存到用户「下载」目录。已存在时报错（除非 overwrite）"`
	Sheets    []xlsxSheet `json:"sheets" jsonschema:"工作表序列，不能为空"`
	Overwrite bool `json:"overwrite,omitempty" jsonschema:"目标文件已存在时是否覆盖"`
}

// xlsxSheet 一个工作表。
type xlsxSheet struct {
	Name       string  `json:"name,omitempty" jsonschema:"工作表名（省略按 Sheet1/Sheet2… 编号）"`
	Rows       [][]any `json:"rows" jsonschema:"二维行数组，第一行通常为表头，如 [[\"名称\",\"数量\"],[\"A\",1]]"`
	ColWidths  []int   `json:"col_widths,omitempty" jsonschema:"逐列宽度（Excel 字符宽，默认约 9），未指定的列保持默认"`
	BoldHeader *bool   `json:"bold_header,omitempty" jsonschema:"表头（首行）加粗，默认 true，传 false 关闭"`
	FreezeTop  bool    `json:"freeze_top,omitempty" jsonschema:"冻结首行（滚动时表头保持可见）"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-xlsx", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_xlsx",
		Description: "读取 xlsx/xlsm：省略 sheet 时列出全部工作表与行列数；指定 sheet 时输出该表为 markdown 表格。",
		Annotations: &mcp.ToolAnnotations{
			Title:        "读取 xlsx",
			ReadOnlyHint: true,
		},
	}, readXLSX)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_xlsx",
		Description: "生成 xlsx 文件（可多工作表）：每个 sheet 传二维 rows；支持列宽、表头加粗、冻结首行。文件已存在时报错，除非 overwrite=true。",
		Annotations: &mcp.ToolAnnotations{Title: "写入 xlsx"},
	}, writeXLSX)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-xlsx:", err)
		os.Exit(1)
	}
}

func readXLSX(ctx context.Context, req *mcp.CallToolRequest, args readXLSXArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	path, err := resolvePath(args.Path)
	if err != nil {
		return nil, nil, err
	}
	if strings.EqualFold(filepath.Ext(path), ".xls") {
		return nil, nil, fmt.Errorf("read_xlsx: 仅支持 .xlsx/.xlsm，不支持 .xls 老格式")
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read_xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, nil, fmt.Errorf("read_xlsx: 文件没有工作表")
	}

	// 未指定 sheet：列清单，供模型下一步精确读取
	if strings.TrimSpace(args.Sheet) == "" {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("共 %d 个工作表（传 sheet 参数读取具体表）：\n", len(sheets)))
		for _, s := range sheets {
			n, cols, err := sheetSize(f, s)
			if err != nil {
				fmt.Fprintf(&b, "- %s（读取失败: %v）\n", s, err)
				continue
			}
			fmt.Fprintf(&b, "- %s：%d 行 × %d 列\n", s, n, cols)
		}
		return textResult(strings.TrimSpace(b.String())), nil, nil
	}

	idx, err := f.GetSheetIndex(args.Sheet)
	if err != nil || idx < 0 {
		return nil, nil, fmt.Errorf("read_xlsx: 工作表 %q 不存在（可用: %s）", args.Sheet, strings.Join(sheets, ", "))
	}
	sheet := sheets[idx]

	rows, err := f.Rows(sheet)
	if err != nil {
		return nil, nil, fmt.Errorf("read_xlsx: %w", err)
	}
	defer rows.Close()

	maxRows := defaultMaxRows
	if args.MaxRows > 0 {
		maxRows = args.MaxRows
	}

	var b strings.Builder
	printed, total, firstCols := 0, 0, 0
	for rows.Next() {
		total++
		cols, err := rows.Columns()
		if err != nil || printed == maxRows {
			continue
		}
		if printed == 0 {
			firstCols = len(cols)
		} else if printed == 1 && firstCols > 0 {
			// 首行后补 markdown 表头分隔行
			b.WriteString("|" + strings.Repeat(" --- |", firstCols) + "\n")
		}
		b.WriteString("| " + strings.Join(mapCells(cols), " | ") + " |\n")
		printed++
	}
	if printed == 0 {
		return textResult(fmt.Sprintf("工作表 %q 为空", sheet)), nil, nil
	}
	out := fmt.Sprintf("工作表 %q（markdown 表格）：\n\n%s", sheet, strings.TrimSpace(b.String()))
	if total > printed {
		out += fmt.Sprintf("\n\n…（共 %d 行，仅显示前 %d 行；用 max_rows 提高上限）", total, printed)
	}
	return textResult(out), nil, nil
}

func writeXLSX(ctx context.Context, req *mcp.CallToolRequest, args writeXLSXArgs) (*mcp.CallToolResult, any, error) {
	if len(args.Sheets) == 0 {
		return nil, nil, fmt.Errorf("sheets 不能为空")
	}
	// path 可选：省略时按首个 sheet 名生成文件名落到输出目录（默认用户的「下载」目录）
	path, err := resolveParent(args.Path, defaultName(args.Sheets, "数据", ".xlsx"))
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(path); err == nil && !args.Overwrite {
		return nil, nil, fmt.Errorf("write_xlsx: 文件已存在: %s（overwrite=true 可覆盖）", path)
	}

	f := excelize.NewFile()
	defer f.Close()
	totalRows := 0
	for i, sh := range args.Sheets {
		if len(sh.Rows) == 0 {
			return nil, nil, fmt.Errorf("write_xlsx: 第 %d 个 sheet 的 rows 不能为空", i+1)
		}
		name := strings.TrimSpace(sh.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, nil, fmt.Errorf("write_xlsx: %w", err)
			}
		} else if _, err := f.NewSheet(name); err != nil {
			return nil, nil, fmt.Errorf("write_xlsx: 新建 %q 失败: %w", name, err)
		}
		for r, row := range sh.Rows {
			cell, err := excelize.CoordinatesToCellName(1, r+1)
			if err != nil {
				return nil, nil, fmt.Errorf("write_xlsx: %q 第 %d 行: %w", name, r+1, err)
			}
			if err := f.SetSheetRow(name, cell, &row); err != nil {
				return nil, nil, fmt.Errorf("write_xlsx: %q 第 %d 行写入失败: %w", name, r+1, err)
			}
		}
		totalRows += len(sh.Rows)
		for ci, w := range sh.ColWidths {
			if w > 0 {
				_ = f.SetColWidth(name, colName(ci+1), colName(ci+1), float64(w))
			}
		}
		// 表头加粗（默认开）
		if (sh.BoldHeader == nil || *sh.BoldHeader) {
			style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
			if err == nil {
				for c := 0; c < len(sh.Rows[0]); c++ {
					cell := colName(c+1) + "1"
					_ = f.SetCellStyle(name, cell, cell, style)
				}
			}
		}
		if sh.FreezeTop && len(sh.Rows) > 1 {
			_ = f.SetPanes(name, &excelize.Panes{
				Freeze:      true,
				YSplit:      1,
				TopLeftCell: "A2",
				ActivePane:  "bottomLeft",
			})
		}
	}
	if err := f.SaveAs(path); err != nil {
		return nil, nil, fmt.Errorf("write_xlsx: %w", err)
	}
	return docResult(fmt.Sprintf("已生成 %s（%d 个工作表，共 %d 行）", path, len(args.Sheets), totalRows), path,
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"), nil, nil
}

// colName 把 1 基列号转成 Excel 列名（1→A，27→AA）。
func colName(n int) string {
	s := ""
	for n > 0 {
		n--
		s = string(rune('A'+n%26)) + s
		n /= 26
	}
	return s
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

// sheetSize 统计工作表行数与最大列数。
func sheetSize(f *excelize.File, sheet string) (int, int, error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	n, cols := 0, 0
	for rows.Next() {
		n++
		if r, err := rows.Columns(); err == nil && len(r) > cols {
			cols = len(r)
		}
	}
	return n, cols, nil
}

// mapCells 压缩超长单元格并转义竖线。
func mapCells(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		c = strings.ReplaceAll(c, "|", "\\|")
		c = strings.ReplaceAll(c, "\n", " ")
		if len(c) > maxCellWidth {
			c = c[:maxCellWidth] + "…"
		}
		out[i] = c
	}
	return out
}

// resolvePath 解析已存在的文件；相对路径基于 MCP 进程工作目录。
func resolvePath(p string) (string, error) {
	abs, err := expand(p)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("文件不存在: %s", abs)
	}
	return abs, nil
}

// resolveParent 解析写入目标路径：p 为空时用 fallback 落到输出目录
// （默认下载目录），用户显式指定的路径原样尊重。父目录不存在则创建。
func resolveParent(p, fallback string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		p = filepath.Join(outputDir(), fallback)
	}
	abs, err := expand(p)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	return abs, nil
}

// outputDir 返回生成文件的默认输出目录：优先 OMNI_OUTPUT_DIR（内核注入，
// 通常就是用户的「下载」目录），否则用户主目录下的 Downloads。
func outputDir() string {
	if d := strings.TrimSpace(os.Getenv("OMNI_OUTPUT_DIR")); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "Downloads")
	}
	return "Downloads"
}

// defaultName 由首个 sheet 名生成默认文件名；名为空时用「数据-<时间戳>」。
func defaultName(sheets []xlsxSheet, fallback, ext string) string {
	name := ""
	if len(sheets) > 0 {
		name = strings.TrimSpace(sheets[0].Name)
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '\n', '\r', '\t':
			return -1
		}
		return r
	}, name)
	name = strings.Trim(strings.TrimSpace(name), ".")
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	if name == "" {
		return fmt.Sprintf("%s-%s%s", fallback, time.Now().Format("20060102-150405"), ext)
	}
	if strings.EqualFold(filepath.Ext(name), ext) {
		return name
	}
	return name + ext
}

func expand(p string) (string, error) {
	abs := p
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			abs = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
