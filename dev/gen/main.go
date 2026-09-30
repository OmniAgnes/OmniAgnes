// gen 生成 dev/testdata 下的样例文件，供导入 MCP 后手动验证：
//
//	go run ./dev/gen
package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/xuri/excelize/v2"
)

const outDir = "dev/testdata"

func main() {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		panic(err)
	}
	writeXLSX(filepath.Join(outDir, "sample.xlsx"))
	writeDOCX(filepath.Join(outDir, "sample.docx"))
	writePPTX(filepath.Join(outDir, "sample.pptx"))
	writePDF(filepath.Join(outDir, "sample.pdf"))
	fmt.Println("样例已生成到", outDir)
}

func writeXLSX(path string) {
	f := excelize.NewFile()
	defer f.Close()
	rows := [][]any{
		{"产品", "销售额（万元）", "同比增长"},
		{"A 产品", 120, "31%"},
		{"B 产品", 85, "12%"},
		{"C 产品", 43, "-5%"},
	}
	for i, row := range rows {
		if err := f.SetSheetRow("Sheet1", "A"+strconv.Itoa(i+1), &row); err != nil {
			panic(err)
		}
	}
	if _, err := f.NewSheet("季度目标"); err != nil {
		panic(err)
	}
	goals := [][]any{
		{"季度", "目标（万元）"},
		{"Q1", 200},
		{"Q2", 260},
	}
	for i, row := range goals {
		if err := f.SetSheetRow("季度目标", "A"+strconv.Itoa(i+1), &row); err != nil {
			panic(err)
		}
	}
	if err := f.SaveAs(path); err != nil {
		panic(err)
	}
	fmt.Println("- sample.xlsx")
}

const docxDocument = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>项目季度报告</w:t></w:r></w:p>
<w:p><w:r><w:t>本季度销售额同比增长 23%	主要由新客户贡献。</w:t></w:r></w:p>
<w:p><w:r><w:t>明细如下：</w:t></w:r></w:p>
<w:tbl>
<w:tr><w:tc><w:p><w:r><w:t>产品</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>销售额</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>A 产品</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>120 万</w:t></w:r></w:p></w:tc></w:tr>
<w:tr><w:tc><w:p><w:r><w:t>B 产品</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>85 万</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>
<w:p><w:r><w:t>以上就是全部内容。</w:t></w:r></w:p>
<w:sectPr/>
</w:body></w:document>`

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const ooxmlRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

func writeDOCX(path string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, body string }{
		{"[Content_Types].xml", docxContentTypes},
		{"_rels/.rels", ooxmlRels},
		{"word/document.xml", docxDocument},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("- sample.docx")
}

const pptxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/ppt/slides/slide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>
<Override PartName="/ppt/slides/slide2.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>
<Override PartName="/ppt/notesSlides/notesSlide1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"/>
</Types>`

const pptxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/>
</Relationships>`

const pptxPresentation = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
<p:sldIdLst><p:sldId id="1" r:id="rId1"/></p:sldIdLst>
</p:presentation>`

func slideXML(texts ...string) string {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree>`)
	for _, t := range texts {
		b.WriteString(`<p:sp><p:txBody><a:bodyPr/><a:p><a:r><a:t>` + t + `</a:t></a:r></a:p></p:txBody></p:sp>`)
	}
	b.WriteString(`</p:spTree></p:cSld></p:sld>`)
	return b.String()
}

func writePPTX(path string) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range []struct{ name, body string }{
		{"[Content_Types].xml", pptxContentTypes},
		{"_rels/.rels", pptxRels},
		{"ppt/presentation.xml", pptxPresentation},
		{"ppt/slides/slide1.xml", slideXML("2026 产品规划", "要点一：扩展 MCP 能力", "要点二：桌面端打磨")},
		{"ppt/slides/slide2.xml", slideXML("下一页：路线图")},
		{"ppt/notesSlides/notesSlide1.xml", slideXML("开场提醒：先讲背景再讲计划。")},
	} {
		w, err := zw.Create(e.name)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("- sample.pptx")
}

// writePDF 手工组装一个两页的最小 PDF（Helvetica，ASCII 文本），xref 偏移精确计算。
func writePDF(path string) {
	stream1 := "BT /F1 24 Tf 72 720 Td (Hello from page one) Tj ET\nBT /F1 14 Tf 72 690 Td (Second line on page one) Tj ET"
	stream2 := "BT /F1 24 Tf 72 720 Td (Page two text here) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",               // 1
		"<< /Type /Pages /Kids [3 0 R 5 0 R] /Count 2 >>", // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 7 0 R >> >> >>", // 3
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream1), stream1),                                    // 4
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /Resources << /Font << /F1 7 0 R >> >> >>", // 5
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream2), stream2),                                    // 6
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",                                                         // 7
	}

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
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Println("- sample.pdf")
}
