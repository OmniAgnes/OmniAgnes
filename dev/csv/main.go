// csv MCP 服务器：CSV/TSV 读与写。
//
// 工具：
//   - read_csv：读取 CSV 为 Markdown 预览（默认前 20 行），并报告总行数、列数、
//     探测到的编码与分隔符
//   - write_csv：生成 CSV 文件（表头 + 多行数据）
//
// 编码：读时自动识别 UTF-8 BOM / UTF-8 / GB18030（Windows Excel 导出的中文 csv
// 多为 GBK，GB18030 是其超集）；写时默认带 UTF-8 BOM，Excel 直接双击不乱码。
// 依赖：标准库 + golang.org/x/text（已在依赖图中，仅用于 GB18030 解码）。
package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	defaultPreviewRows = 20       // read_csv 默认预览行数
	maxPreviewRows     = 500      // 预览行数上限（避免刷爆上下文）
	maxFileBytes       = 10 << 20 // 读取文件大小上限 10MB
)

type readArgs struct {
	Path      string `json:"path" jsonschema:"CSV 文件路径"`
	MaxRows   int    `json:"max_rows,omitempty" jsonschema:"预览行数，默认 20，上限 500"`
	Delimiter string `json:"delimiter,omitempty" jsonschema:"分隔符，默认自动探测（逗号/分号/tab/竖线）；可显式传 , ; | 或 tab"`
}

type writeArgs struct {
	Path      string     `json:"path" jsonschema:"要写入的 CSV 文件路径"`
	Headers   []string   `json:"headers,omitempty" jsonschema:"表头（可选）"`
	Rows      [][]string `json:"rows" jsonschema:"数据行，二维数组，每行元素按列顺序"`
	Delimiter string     `json:"delimiter,omitempty" jsonschema:"分隔符，默认逗号；可传 tab 或 ;"`
	BOM       *bool      `json:"bom,omitempty" jsonschema:"是否写 UTF-8 BOM，默认 true（Excel 打开中文不乱码）"`
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "omni-csv", Version: "0.0.1"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "read_csv",
		Annotations: &mcp.ToolAnnotations{Title: "读取 CSV", ReadOnlyHint: true},
		Description: "读取 CSV/TSV 并返回 Markdown 预览：附编码、分隔符、总行数与列数。列数不齐、引号异常的脏数据也能尽量读出。",
	}, readCSV)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_csv",
		Annotations: &mcp.ToolAnnotations{Title: "生成 CSV"},
		Description: "生成 CSV 文件：表头 + 多行数据。默认带 UTF-8 BOM（Windows Excel 双击不乱码），自动转义含逗号/引号/换行的字段。",
	}, writeCSV)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "omni-csv:", err)
		os.Exit(1)
	}
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

// ---------- read_csv ----------

func readCSV(_ context.Context, _ *mcp.CallToolRequest, args readArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	info, err := os.Stat(p)
	if err != nil {
		return nil, nil, err
	}
	if info.IsDir() {
		return nil, nil, fmt.Errorf("%s 是目录，不是文件", p)
	}
	if info.Size() > maxFileBytes {
		return nil, nil, fmt.Errorf("文件 %d 字节，超过 10MB 上限；请用 bash 的 head/split 分段处理", info.Size())
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, nil, err
	}

	text, encName, err := decodeText(raw)
	if err != nil {
		return nil, nil, err
	}

	limit := args.MaxRows
	if limit <= 0 {
		limit = defaultPreviewRows
	}
	if limit > maxPreviewRows {
		limit = maxPreviewRows
	}

	sep := detectSeparator(text)
	if d := strings.TrimSpace(args.Delimiter); d != "" {
		if d == "tab" || d == `\t` {
			sep = '\t'
		} else {
			sep = []rune(d)[0]
		}
	}

	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.FieldsPerRecord = -1 // 容忍列数不齐
	r.LazyQuotes = true    // 容忍引号异常
	r.TrimLeadingSpace = true

	var (
		rows   [][]string
		total  int
		broken bool
	)
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if total > 0 { // 中途出错：保留已读内容并提示
				broken = true
				break
			}
			return nil, nil, fmt.Errorf("解析失败（可能不是 CSV，或分隔符不对）：%w", err)
		}
		total++
		if len(rows) < limit+1 { // 多留一行用于表头判断
			rows = append(rows, rec)
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "文件：%s\n", p)
	fmt.Fprintf(&b, "编码：%s\n", encName)
	fmt.Fprintf(&b, "分隔符：%s\n", sepName(sep))
	if total == 0 {
		b.WriteString("内容：空（没有可解析的行）\n")
		return textResult(b.String()), nil, nil
	}
	cols := 0
	for _, rec := range rows {
		if len(rec) > cols {
			cols = len(rec)
		}
	}
	fmt.Fprintf(&b, "规模：%d 行 × 最多 %d 列\n", total, cols)
	if broken {
		b.WriteString("注意：解析中途遇到异常行已停止，结果可能不完整\n")
	}
	b.WriteString("\n")

	head := rows[0]
	fmt.Fprintf(&b, "| %s |\n", strings.Join(mdCells(head), " | "))
	b.WriteString("|" + strings.Repeat(" --- |", len(head)) + "\n")
	shown := 0
	for _, rec := range rows[1:] {
		fmt.Fprintf(&b, "| %s |\n", strings.Join(mdCells(rec), " | "))
		shown++
	}
	if total-1 > shown {
		fmt.Fprintf(&b, "\n（仅预览前 %d 行数据，共 %d 行；要更多请设 max_rows，上限 %d）\n", shown, total-1, maxPreviewRows)
	}
	return textResult(b.String()), nil, nil
}

// decodeText 识别 BOM / UTF-8 / GB18030，返回文本与编码名。
func decodeText(raw []byte) (string, string, error) {
	switch {
	case len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF:
		return string(raw[3:]), "UTF-8（含 BOM）", nil
	case len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE:
		return "", "", fmt.Errorf("检测到 UTF-16 LE 编码，暂不支持；请先转 UTF-8（bash: iconv -f UTF-16 -t UTF-8）")
	case len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF:
		return "", "", fmt.Errorf("检测到 UTF-16 BE 编码，暂不支持；请先转 UTF-8（bash: iconv -f UTF-16 -t UTF-8）")
	}
	if validUTF8(raw) {
		return string(raw), "UTF-8", nil
	}
	out, _, err := transform.String(simplifiedchinese.GB18030.NewDecoder(), string(raw))
	if err != nil {
		return "", "", fmt.Errorf("无法识别编码（既不是 UTF-8 也不是 GB18030）")
	}
	return out, "GB18030（GBK/GB2312 兼容）", nil
}

func validUTF8(b []byte) bool {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			i++
		case c&0xE0 == 0xC0:
			if i+1 >= len(b) || b[i+1]&0xC0 != 0x80 {
				return false
			}
			i += 2
		case c&0xF0 == 0xE0:
			if i+2 >= len(b) || b[i+1]&0xC0 != 0x80 || b[i+2]&0xC0 != 0x80 {
				return false
			}
			i += 3
		case c&0xF8 == 0xF0:
			if i+3 >= len(b) || b[i+1]&0xC0 != 0x80 || b[i+2]&0xC0 != 0x80 || b[i+3]&0xC0 != 0x80 {
				return false
			}
			i += 4
		default:
			return false
		}
	}
	return true
}

// detectSeparator 在前 20 行里挑「出现行数多且总量大」的分隔符。
func detectSeparator(text string) rune {
	lines := strings.Split(text, "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	best, bestScore := ',', -1
	for _, c := range []rune{',', ';', '\t', '|'} {
		hit, sum := 0, 0
		for _, ln := range lines {
			if n := strings.Count(ln, string(c)); n > 0 {
				hit++
				sum += n
			}
		}
		if hit == 0 {
			continue
		}
		// 出现行数权重更高（避免某行偶然含大量分隔符就取胜）
		if score := hit*100 + sum; score > bestScore {
			best, bestScore = c, score
		}
	}
	return best
}

func sepName(r rune) string {
	switch r {
	case '\t':
		return "tab（制表符）"
	case ';':
		return "分号 ;"
	case '|':
		return "竖线 |"
	default:
		return "逗号 ,"
	}
}

// mdCells 转义 Markdown 表格单元格里的竖线与换行。
func mdCells(rec []string) []string {
	out := make([]string, 0, len(rec))
	for _, f := range rec {
		f = strings.ReplaceAll(f, "\r", "")
		f = strings.ReplaceAll(f, "\n", " ")
		f = strings.ReplaceAll(f, "|", "\\|")
		if strings.TrimSpace(f) == "" {
			f = " "
		}
		out = append(out, f)
	}
	return out
}

// ---------- write_csv ----------

func writeCSV(_ context.Context, _ *mcp.CallToolRequest, args writeArgs) (*mcp.CallToolResult, any, error) {
	p := strings.TrimSpace(args.Path)
	if p == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if len(args.Headers) == 0 && len(args.Rows) == 0 {
		return nil, nil, fmt.Errorf("headers 与 rows 都为空，没有内容可写")
	}
	sep := ','
	switch {
	case args.Delimiter == "tab" || args.Delimiter == `\t`:
		sep = '\t'
	case strings.TrimSpace(args.Delimiter) != "":
		sep = []rune(args.Delimiter)[0]
	}
	withBOM := true
	if args.BOM != nil {
		withBOM = *args.BOM
	}

	if dir := filepath.Dir(p); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	f, err := os.Create(p)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	if withBOM {
		if _, err := f.WriteString("\ufeff"); err != nil {
			return nil, nil, err
		}
	}
	w := csv.NewWriter(f)
	w.Comma = sep
	if len(args.Headers) > 0 {
		if err := w.Write(args.Headers); err != nil {
			return nil, nil, err
		}
	}
	for _, row := range args.Rows {
		if err := w.Write(row); err != nil {
			return nil, nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, nil, err
	}
	if err := f.Sync(); err != nil {
		return nil, nil, err
	}

	totalRows := len(args.Rows)
	if len(args.Headers) > 0 {
		totalRows++
	}
	cols := len(args.Headers)
	for _, r := range args.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	bomNote := "不带 BOM"
	if withBOM {
		bomNote = "带 UTF-8 BOM"
	}
	return textResult(fmt.Sprintf("已写入 %s\n行数：%d（其中表头 %d 行）\n列数：%d\n分隔符：%s\n编码：UTF-8（%s）",
		p, totalRows, boolToInt(len(args.Headers) > 0), cols, sepName(sep), bomNote)), nil, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
