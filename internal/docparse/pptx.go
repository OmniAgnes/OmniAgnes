package docparse

// pptx 提取：按页号顺序抽取幻灯片文本与备注（移植自 dev/pptx）。
// ppt/slides/slideN.xml 是 zip 内的 OOXML（DrawingML），标准库解析。
// 不支持老 .ppt（二进制）格式。

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

func extractPPTX(path string) (string, error) {
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
		nums = append(nums, n)
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
