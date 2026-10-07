// write_pptx：从幻灯片结构生成 .pptx（标题 + 要点 + 可选演讲者备注）。
// 纯 Go 手写 OOXML zip，含最小可用的 master/layout/theme，保证 PowerPoint 可打开。
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pptxSlide 一页幻灯片。
type pptxSlide struct {
	Title   string   `json:"title,omitempty" jsonschema:"标题"`
	Bullets []string `json:"bullets,omitempty" jsonschema:"要点列表，每元素一行"`
	Body    string   `json:"body,omitempty" jsonschema:"正文（bullets 为空时用；多行以 \\n 分隔）"`
	Notes   string   `json:"notes,omitempty" jsonschema:"演讲者备注"`
}

type writePPTXArgs struct {
	Path      string      `json:"path" jsonschema:"目标 .pptx 路径（相对路径基于 MCP 进程工作目录）"`
	Slides    []pptxSlide `json:"slides" jsonschema:"幻灯片序列，不能为空"`
	Overwrite bool        `json:"overwrite,omitempty" jsonschema:"目标已存在时是否覆盖"`
}

func writePPTX(ctx context.Context, req *mcp.CallToolRequest, args writePPTXArgs) (*mcp.CallToolResult, any, error) {
	if strings.TrimSpace(args.Path) == "" {
		return nil, nil, fmt.Errorf("缺少 path 参数")
	}
	if len(args.Slides) == 0 {
		return nil, nil, fmt.Errorf("slides 不能为空")
	}
	path, err := resolvePathParent(args.Path)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(path); err == nil && !args.Overwrite {
		return nil, nil, fmt.Errorf("write_pptx: 文件已存在: %s（overwrite=true 可覆盖）", path)
	}
	data, err := buildPPTX(args)
	if err != nil {
		return nil, nil, fmt.Errorf("write_pptx: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return nil, nil, err
	}
	return textResult(fmt.Sprintf("已生成 %s（%d 页）", path, len(args.Slides))), nil, nil
}

// ---------- 尺寸（EMU，16:9） ----------

const (
	slideW  = "12192000"
	slideH  = "6858000"
	titleX  = "457200"
	titleY  = "274638"
	titleW  = "11282400"
	titleH  = "1524150"
	bodyX   = "457200"
	bodyY   = "1920240"
	bodyW   = "11282400"
	bodyH   = "4876440"
	notesW  = "9144000"
	notesH  = "4800600"
	notesX  = "1524000"
	notesY  = "1524000"
)

const pptxXMLHdr = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`

const (
	relNS    = "http://schemas.openxmlformats.org/package/2006/relationships"
	ctypeNS  = "http://schemas.openxmlformats.org/package/2006/content-types"
	prNS     = "http://schemas.openxmlformats.org/presentationml/2006/main"
	dNS      = "http://schemas.openxmlformats.org/drawingml/2006/main"
	relsNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	themeCt  = "application/vnd.openxmlformats-officedocument.theme+xml"
)

func buildPPTX(args writePPTXArgs) ([]byte, error) {
	n := len(args.Slides)
	notesOn := make([]bool, n)
	for i, s := range args.Slides {
		notesOn[i] = strings.TrimSpace(s.Notes) != ""
	}

	files := map[string]string{
		"[Content_Types].xml":     contentTypes(n, notesOn),
		"_rels/.rels":             relsXML(rels("rId1", relsNS+"/officeDocument", "ppt/presentation.xml")),
		"ppt/presentation.xml":    presentationXML(n),
		"ppt/_rels/presentation.xml.rels": relsXML(
			rels("rId1", relsNS+"/slideMaster", "slideMasters/slideMaster1.xml") +
				slideRels(n) +
				rels(fmt.Sprintf("rId%d", n+2), relsNS+"/theme", "theme/theme1.xml"),
		),
		"ppt/slideMasters/slideMaster1.xml": masterXML,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": relsXML(
			rels("rId1", relsNS+"/slideLayout", "../slideLayouts/slideLayout1.xml") +
				rels("rId2", relsNS+"/theme", "../theme/theme1.xml"),
		),
		"ppt/slideLayouts/slideLayout1.xml": layoutXML,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": relsXML(rels("rId1", relsNS+"/slideMaster", "../slideMasters/slideMaster1.xml")),
		"ppt/theme/theme1.xml": themeXML,
	}
	for i := 0; i < n; i++ {
		files[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = slideXML(args.Slides[i])
		files[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1)] = slideRelXML(i, notesOn[i])
		if notesOn[i] {
			files[fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", i+1)] = notesXML(args.Slides[i])
			files[fmt.Sprintf("ppt/notesSlides/_rels/notesSlide%d.xml.rels", i+1)] = notesRelXML(i)
		}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range stableOrder(files) {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(files[name])); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------- 各 part ----------

func contentTypes(n int, notesOn []bool) string {
	var b strings.Builder
	b.WriteString(pptxXMLHdr + "\n<Types xmlns=\"" + ctypeNS + "\">")
	b.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>`)
	b.WriteString(`<Default Extension="xml" ContentType="application/xml"/>`)
	b.WriteString(`<Override PartName="/ppt/presentation.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"/>`)
	b.WriteString(`<Override PartName="/ppt/slideMasters/slideMaster1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml"/>`)
	b.WriteString(`<Override PartName="/ppt/slideLayouts/slideLayout1.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml"/>`)
	b.WriteString(`<Override PartName="/ppt/theme/theme1.xml" ContentType="` + themeCt + `"/>`)
	for i := 0; i < n; i++ {
		b.WriteString(fmt.Sprintf(`<Override PartName="/ppt/slides/slide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml"/>`, i+1))
		if notesOn[i] {
			b.WriteString(fmt.Sprintf(`<Override PartName="/ppt/notesSlides/notesSlide%d.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.notesSlide+xml"/>`, i+1))
		}
	}
	b.WriteString("</Types>")
	return b.String()
}

// rels 拼单个 Relationship 元素。
func rels(id, typ, target string) string {
	return fmt.Sprintf(`<Relationship Id="%s" Type="%s" Target="%s"/>`, id, typ, target)
}

// relsXML 包上 Relationships 根元素。
func relsXML(inner string) string {
	return pptxXMLHdr + "\n<Relationships xmlns=\"" + relNS + "\">" + inner + "</Relationships>"
}

func presentationXML(n int) string {
	var b strings.Builder
	b.WriteString(pptxXMLHdr + "\n")
	b.WriteString(`<p:presentation xmlns:p="` + prNS + `" xmlns:a="` + dNS + `" xmlns:r="` + relsNS + `">`)
	b.WriteString(`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst><p:sldIdLst>`)
	for i := 0; i < n; i++ {
		b.WriteString(fmt.Sprintf(`<p:sldId id="%d" r:id="rId%d"/>`, 256+i, i+2))
	}
	b.WriteString(`</p:sldIdLst>`)
	b.WriteString(fmt.Sprintf(`<p:sldSz cx="%s" cy="%s"/><p:notesSz cx="%s" cy="%s"/>`, slideW, slideH, slideW, slideH))
	b.WriteString("</p:presentation>")
	return b.String()
}

// slideRels presentation.xml.rels 里每页 slide 的 Relationship（rId2..rId(n+1)）。
func slideRels(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(rels(fmt.Sprintf("rId%d", i+2), relsNS+"/slide", fmt.Sprintf("slides/slide%d.xml", i+1)))
	}
	return b.String()
}

func slideRelXML(i int, hasNotes bool) string {
	inner := rels("rId1", relsNS+"/slideLayout", "../slideLayouts/slideLayout1.xml")
	if hasNotes {
		inner += rels("rId2", relsNS+"/notesSlide", fmt.Sprintf("../notesSlides/notesSlide%d.xml", i+1))
	}
	return relsXML(inner)
}

func notesRelXML(i int) string {
	// notesSlide 指回宿主 slide（rId1）
	return relsXML(rels("rId1", relsNS+"/slide", fmt.Sprintf("../slides/slide%d.xml", i+1)))
}

// slideXML 一页：标题 + 要点正文 + 页码占位。
func slideXML(s pptxSlide) string {
	lines := s.Bullets
	if len(lines) == 0 && strings.TrimSpace(s.Body) != "" {
		lines = strings.Split(s.Body, "\n")
	}
	paras := ""
	if len(lines) > 0 {
		var b strings.Builder
		for _, ln := range lines {
			b.WriteString(`<a:p><a:pPr><a:buChar char="•"/></a:pPr><a:r><a:rPr lang="zh-CN" sz="2400" dirty="0"/><a:t>` + xmlEscape(ln) + `</a:t></a:r></a:p>`)
		}
		paras = b.String()
	}
	var out strings.Builder
	out.WriteString(pptxXMLHdr + "\n")
	out.WriteString(`<p:sld xmlns:p="` + prNS + `" xmlns:a="` + dNS + `" xmlns:r="` + relsNS + `"><p:cSld><p:spTree>`)
	out.WriteString(`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`)
	out.WriteString(shape("2", "Title", `type="title"`, titleX, titleY, titleW, titleH,
		`<a:p><a:r><a:rPr lang="zh-CN" sz="4000" b="1" dirty="0"/><a:t>` + xmlEscape(s.Title) + `</a:t></a:r></a:p>`))
	if len(lines) > 0 {
		out.WriteString(shape("3", "Content", `type="body" idx="1"`, bodyX, bodyY, bodyW, bodyH, paras))
	}
	// 页码占位（与 layout 的 sldNum ph 对应）
	out.WriteString(shape("4", "Slide Number", `type="sldNum" sz="quarter" idx="2"`, "9144000", "6356350", "2286000", "458788",
		`<a:fld id="{3F59D995-4391-4E43-9D9B-120098B4E1F5}" type="slidenum"><a:r><a:t>1</a:t></a:r></a:fld>`))
	out.WriteString("</p:spTree></p:cSld></p:sld>")
	return out.String()
}

// shape 一个占位符文本框；ph 是 p:ph 属性（如 type="body" idx="1"，空串表示普通形状），inner 是 a:txBody 内部的 a:p/a:fld 序列。
func shape(id, name, ph, x, y, cx, cy, inner string) string {
	phElem := ""
	if ph != "" {
		phElem = `<p:ph ` + ph + `/>`
	}
	return `<p:sp><p:nvSpPr><p:cNvPr id="` + id + `" name="` + name + `"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr>` + phElem + `</p:nvPr></p:nvSpPr>` +
		`<p:spPr><a:xfrm><a:off x="` + x + `" y="` + y + `"/><a:ext cx="` + cx + `" cy="` + cy + `"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
		`<p:txBody><a:bodyPr/><a:lstStyle/>` + inner + `</p:txBody></p:sp>`
}

// notesXML 演讲者备注页。
func notesXML(s pptxSlide) string {
	var b strings.Builder
	b.WriteString(pptxXMLHdr + "\n")
	b.WriteString(`<p:notes xmlns:p="` + prNS + `" xmlns:a="` + dNS + `" xmlns:r="` + relsNS + `"><p:cSld><p:spTree>`)
	b.WriteString(`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>`)
	// 幻灯片缩略占位
	b.WriteString(shape("2", "Slide Image", "type=\"sldImg\"", "0", "0", slideW, "3691880", ""))
	// 备注正文
	var para string
	for _, ln := range strings.Split(s.Notes, "\n") {
		para += `<a:p><a:r><a:rPr lang="zh-CN" sz="1200"/><a:t>` + xmlEscape(ln) + `</a:t></a:r></a:p>`
	}
	b.WriteString(shape("3", "Notes", "type=\"body\"", notesX, notesY, notesW, notesH, para))
	b.WriteString("</p:spTree></p:cSld></p:notes>")
	return b.String()
}

// ---------- 固定模板 part ----------

const masterXML = pptxXMLHdr + `
<p:sldMaster xmlns:p="` + prNS + `" xmlns:a="` + dNS + `" xmlns:r="` + relsNS + `"><p:cSld><p:bg><a:bgPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:effectLst/></a:bgPr></p:bg><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/><p:sp><p:nvSpPr><p:cNvPr id="2" name="Title Placeholder 1"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="3" name="Body Placeholder 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="4" name="Slide Number Placeholder 3"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldNum" sz="quarter" idx="2"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="5" name="Date Placeholder 4"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="dt" sz="quarter" idx="3"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="6" name="Footer Placeholder 5"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="ftr" sz="quarter" idx="4"/></p:nvPr></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp></p:spTree></p:cSld><p:clrMap og1="lt1" og2="dk1" lt1="lt1" lt2="lt2" dk1="dk1" dk2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/><p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`

const layoutXML = pptxXMLHdr + `
<p:sldLayout xmlns:p="` + prNS + `" xmlns:a="` + dNS + `" xmlns:r="` + relsNS + `" type="title" preserve="1"><p:cSld name="Title and Content"><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/><p:sp><p:nvSpPr><p:cNvPr id="2" name="Title 1"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="457200" y="274638"/><a:ext cx="11282400" cy="1524150"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="3" name="Content 2"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph idx="1" type="body"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="457200" y="1920240"/><a:ext cx="11282400" cy="4876440"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="4" name="Slide Number Placeholder 3"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="sldNum" sz="quarter" idx="2"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="9144000" y="6356350"/><a:ext cx="2286000" cy="458788"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="5" name="Date Placeholder 4"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="dt" sz="quarter" idx="3"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="457200" y="6356350"/><a:ext cx="2286000" cy="458788"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp><p:sp><p:nvSpPr><p:cNvPr id="6" name="Footer Placeholder 5"/><p:cNvSpPr><a:spLocks noGrp="1"/></p:cNvSpPr><p:nvPr><p:ph type="ftr" sz="quarter" idx="4"/></p:nvPr></p:nvSpPr><p:spPr><a:xfrm><a:off x="3021000" y="6356350"/><a:ext cx="6144000" cy="458788"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr><p:txBody><a:bodyPr/><a:lstStyle/></p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOv><a:overrideClrMapping/></p:clrMapOv></p:sldLayout>`

// themeXML 最小主题：Office 默认色板 + 通用字体（含 CJK 字体回退）。
const themeXML = pptxXMLHdr + `
<a:theme xmlns:a="` + dNS + `" name="Office Theme"><a:themeElements><a:clrScheme name="Office">` +
	`<a:dk1><a:sysClr val="window1" lastClr="FFFFFF"/></a:dk1>` +
	`<a:lt1><a:sysClr val="window2" lastClr="000000"/></a:lt1>` +
	`<a:dk2><a:srgbClr val="44546A"/></a:dk2>` +
	`<a:lt2><a:srgbClr val="E7E6E6"/></a:lt2>` +
	`<a:accent1><a:srgbClr val="5B9BD5"/></a:accent1>` +
	`<a:accent2><a:srgbClr val="ED7D31"/></a:accent2>` +
	`<a:accent3><a:srgbClr val="A5A5A5"/></a:accent3>` +
	`<a:accent4><a:srgbClr val="FFC000"/></a:accent4>` +
	`<a:accent5><a:srgbClr val="4472C4"/></a:accent5>` +
	`<a:accent6><a:srgbClr val="70AD47"/></a:accent6>` +
	`<a:hlink><a:srgbClr val="0563C1"/></a:hlink><a:folHlink><a:srgbClr val="954F72"/></a:folHlink>` +
	`</a:clrScheme>` +
	`<a:fontScheme name="Office"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/></a:majorFont><a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/></a:minorFont></a:fontScheme>` +
	`<a:fmtScheme name="Office"><a:fillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:fillStyleLst><a:lnStyleLst><a:ln w="6350"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="12700"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln><a:ln w="19050"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln></a:lnStyleLst><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst><a:bgFillStyleLst><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:bgFillStyleLst></a:fmtScheme>` +
	`</a:themeElements></a:theme>`

// stableOrder 固定 zip 条目顺序（可读性/确定性）。
func stableOrder(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	priority := map[string]int{
		"[Content_Types].xml": 0, "_rels/.rels": 1,
		"ppt/presentation.xml": 2, "ppt/_rels/presentation.xml.rels": 3,
		"ppt/slideMasters/slideMaster1.xml": 4,
		"ppt/slideMasters/_rels/slideMaster1.xml.rels": 5,
		"ppt/slideLayouts/slideLayout1.xml": 6,
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels": 7,
		"ppt/theme/theme1.xml": 8,
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		pi, ok1 := priority[a]
		pj, ok2 := priority[b]
		if ok1 && ok2 {
			return pi < pj
		}
		if ok1 {
			return true
		}
		if ok2 {
			return false
		}
		// slides / notesSlides：按名字数值序
		return a < b
	})
	return names
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

// xmlEscape 转义 DrawingML 文本。
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(s)
}
