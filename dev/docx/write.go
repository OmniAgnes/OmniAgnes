// write_docx：从结构化内容生成 .docx（标题/段落/项目符号/表格），纯 Go 手写 OOXML zip。
// 结构与 dev/gen 一致但参数化；不依赖第三方库。
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// docxBlock 一个内容块，按字段互斥：heading / text / list / table 四选一。
type docxBlock struct {
	Heading string   `json:"heading,omitempty" jsonschema:"标题文本；填了即视为标题段"`
	Level   int      `json:"level,omitempty" jsonschema:"标题级别 1-4，缺省 1"`
	Text    string   `json:"text,omitempty" jsonschema:"普通段落文本"`
	List    []string `json:"list,omitempty" jsonschema:"项目符号列表，每元素一条"`
	Table   [][]any  `json:"table,omitempty" jsonschema:"表格：二维行数组，首行作为加粗表头"`
}

type writeDocxArgs struct {
	Path      string    `json:"path" jsonschema:"目标 .docx 路径（相对路径基于 MCP 进程工作目录）"`
	Title     string    `json:"title,omitempty" jsonschema:"文档大标题"`
	Blocks    []docxBlock `json:"blocks" jsonschema:"内容块序列，按顺序渲染"`
	Overwrite bool      `json:"overwrite,omitempty" jsonschema:"目标已存在时是否覆盖"`
}

func writeDocx(ctx context.Context, req *mcp.CallToolRequest, args writeDocxArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if len(args.Blocks) == 0 && strings.TrimSpace(args.Title) == "" {
		return nil, nil, fmt.Errorf("需要 title 或 blocks")
	}
	path, err := resolvePathParent(args.Path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(path); err == nil && !args.Overwrite {
		return nil, nil, fmt.Errorf("write_docx: 文件已存在: %s（overwrite=true 可覆盖）", path)
	}

	data, err := buildDocx(args)
	if err != nil {
		return nil, nil, fmt.Errorf("write_docx: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, nil, err
	}
	return textResult(fmt.Sprintf("已生成 %s（%d 个内容块）", path, len(args.Blocks))), nil, nil
}

// buildDocx 组装 docx zip 包。
func buildDocx(args writeDocxArgs) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct {
		name string
		body string
	}{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", docxRootRels},
		{"word/_rels/document.xml.rels", docxDocRels},
		{"word/styles.xml", docxStyles},
		{"word/numbering.xml", docxNumbering},
		{"word/document.xml", docxDocumentXML(args)},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

const xmlHdr = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`
const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

const docxContentTypes = xmlHdr + `
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
</Types>`

const docxRootRels = xmlHdr + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docxDocRels = xmlHdr + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
</Relationships>`

const docxStyles = xmlHdr + `
<w:styles ` + wNS + `>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/><w:rPr><w:sz w:val="22"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="240" w:line="240" w:lineRule="auto"/></w:pPr><w:rPr><w:b/><w:sz w:val="56"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="280" w:after="120"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="100"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="200" w:after="80"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:spacing w:before="160" w:after="60"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
</w:styles>`

const docxNumbering = xmlHdr + `
<w:numbering ` + wNS + `>
<w:abstractNum w:abstractNumId="0">
<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr><w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/></w:rPr></w:lvl>
</w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`

// docxDocumentXML 把内容块渲染为 word/document.xml。
func docxDocumentXML(args writeDocxArgs) string {
	var b strings.Builder
	b.WriteString(xmlHdr + "\n")
	b.WriteString(`<w:document ` + wNS + `><w:body>`)
	if strings.TrimSpace(args.Title) != "" {
		b.WriteString(stylePara("Title", args.Title, false))
	}
	for _, blk := range args.Blocks {
		switch {
		case blk.Heading != "":
			lvl := blk.Level
			if lvl < 1 {
				lvl = 1
			}
			if lvl > 4 {
				lvl = 4
			}
			b.WriteString(stylePara("Heading"+itoa(lvl), blk.Heading, false))
		case blk.Text != "":
			b.WriteString(stylePara("", blk.Text, false))
		case len(blk.List) > 0:
			for _, item := range blk.List {
				b.WriteString(listPara(item))
			}
		case len(blk.Table) > 0:
			b.WriteString(tableXML(blk.Table))
		}
	}
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="851" w:footer="992" w:gutter="0"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

// stylePara 生成带（可选）样式与（可选）加粗的段落。
func stylePara(styleID, text string, bold bool) string {
	p := "<w:p>"
	if styleID != "" {
		p += `<w:pPr><w:pStyle w:val="` + styleID + `"/></w:pPr>`
	}
	rpr := ""
	if bold {
		rpr = "<w:rPr><w:b/></w:rPr>"
	}
	p += `<w:r>` + rpr + `<w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
	return p
}

func listPara(text string) string {
	return `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
}

// tableXML 生成带边框表格，首行加粗。
func tableXML(rows [][]any) string {
	var b strings.Builder
	b.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="9360" w:type="dxa"/><w:tblBorders>`)
	for _, edge := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:` + edge + ` w:val="single" w:sz="4" w:space="0" w:color="auto"/>`)
	}
	b.WriteString(`</w:tblBorders><w:tblLayout w:type="fixed"/></w:tblPr>`)
	for i, row := range rows {
		b.WriteString(`<w:tr>`)
		for _, cell := range row {
			bold := ""
			if i == 0 {
				bold = "<w:rPr><w:b/></w:rPr>"
			}
			b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr><w:p><w:r>` + bold +
				`<w:t xml:space="preserve">` + xmlEscape(fmt.Sprintf("%v", cell)) + `</w:t></w:r></w:p></w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
	return b.String()
}

// xmlEscape 转义 OOXML 文本。
func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;")
	return r.Replace(s)
}

// itoa 避免引 strconv（本包未导入）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [8]byte
	i := 8
	for n > 0 {
		d[i-1] = byte('0' + n%10)
		n /= 10
		i--
	}
	return string(d[i:])
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
