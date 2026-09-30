package docparse

// xlsx/xlsm 提取：基于 excelize（纯 Go）。省略 sheet 时列出全部工作表与行列数，
// 指定 sheet 时输出该表为 markdown 表格（移植自 dev/xlsx 的读取部分）。
// 不支持 .xls 老格式（BIFF 二进制）。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	xlsxDefaultMaxRows = 500
	xlsxMaxCellWidth   = 80 // 单元格文本超长截断，避免 markdown 行爆掉
)

func extractXLSX(path, sheet string) (string, error) {
	if strings.EqualFold(filepath.Ext(path), ".xls") {
		return "", fmt.Errorf("仅支持 .xlsx/.xlsm，不支持 .xls 老格式")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("文件不存在: %s", path)
	}

	f, err := excelize.OpenFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return "", fmt.Errorf("文件没有工作表")
	}

	// 未指定 sheet：列清单，供模型下一步精确读取
	if strings.TrimSpace(sheet) == "" {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("共 %d 个工作表（读取具体表时指定 sheet 参数）：\n", len(sheets)))
		for _, s := range sheets {
			n, cols, err := sheetSize(f, s)
			if err != nil {
				fmt.Fprintf(&b, "- %s（读取失败: %v）\n", s, err)
				continue
			}
			fmt.Fprintf(&b, "- %s：%d 行 × %d 列\n", s, n, cols)
		}
		return strings.TrimSpace(b.String()), nil
	}

	idx, err := f.GetSheetIndex(sheet)
	if err != nil || idx < 0 {
		return "", fmt.Errorf("工作表 %q 不存在（可用: %s）", sheet, strings.Join(sheets, ", "))
	}
	name := sheets[idx]

	rows, err := f.Rows(name)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var b strings.Builder
	printed, total, firstCols := 0, 0, 0
	for rows.Next() {
		total++
		cols, err := rows.Columns()
		if err != nil || printed == xlsxDefaultMaxRows {
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
		return fmt.Sprintf("工作表 %q 为空", name), nil
	}
	out := fmt.Sprintf("工作表 %q（markdown 表格）：\n\n%s", name, strings.TrimSpace(b.String()))
	if total > printed {
		out += fmt.Sprintf("\n\n…（共 %d 行，仅显示前 %d 行）", total, printed)
	}
	return out, nil
}

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

func mapCells(cols []string) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		c = strings.ReplaceAll(c, "|", "\\|")
		c = strings.ReplaceAll(c, "\n", " ")
		if len(c) > xlsxMaxCellWidth {
			c = c[:xlsxMaxCellWidth] + "…"
		}
		out[i] = c
	}
	return out
}
