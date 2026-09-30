package docparse

// docx 提取：word/document.xml 是 zip 内的 OOXML，用标准库 archive/zip + encoding/xml
// 按文档顺序重组段落与表格（移植自 dev/docx，原为 MCP 服务器）。
// 不支持老 .doc（OLE 二进制）格式。

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

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
