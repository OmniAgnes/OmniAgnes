---
name: xlsx
description: 创建、读取、编辑 Excel 表格（.xlsx/.xlsm）：整理数据、生成报表、透视汇总、加公式图表格式。当用户提到 Excel、表格、xlsx、数据报表、台账、对账单、做表/改表时使用。老 .xls 格式与 PDF 不适用。
---

# Excel（xlsx）操作

用 Python + openpyxl 完成。执行前先确认依赖，没有再装（一次即可）：

```bash
python -c "import openpyxl" || pip install openpyxl
```

> 若当前会话已有 xlsx MCP 工具（`mcp__xlsx__read_xlsx` / `mcp__xlsx__write_xlsx`），
> 「只读」或「整表新建」优先用它，比写脚本快；改现有文件、加公式/图表/格式才用 openpyxl。

## 按任务选做法

| 任务 | 做法 |
|---|---|
| 读数据 | openpyxl 读目标表；>5万行大文件用 `read_only=True` |
| 新建整表 | 行数组 `ws.append()` 写入，首行表头 |
| 改现有文件 | `load_workbook` 加载 → 只改目标单元格 → `save` 回原路径 |
| 加公式 | 写公式字符串，别写 Python 算好的值（数据是动态的时） |
| 透视/汇总 | Python 里聚合好再写结果，不在单元格里堆一屏公式 |
| 图表 | `openpyxl.chart`（Bar/Line/Pie），基于已写好的数据区域 |

## 常用骨架

```python
from openpyxl import Workbook, load_workbook
from openpyxl.styles import Font, Alignment

wb = Workbook(); ws = wb.active; ws.title = "明细"
ws.append(["日期", "类别", "金额"])          # 表头
for r in data: ws.append(r)                  # 数据行
for i, w in enumerate((12, 16, 10), 1):      # 列宽
    ws.column_dimensions[ws.cell(1, i).column_letter].width = w
ws.freeze_panes = "A2"                        # 冻结表头
wb.save("out.xlsx")

# 读取：data_only=True 拿公式的「计算值」（文件曾在 Excel 里打开过才有）
wb = load_workbook("in.xlsx", data_only=True)
for row in wb["明细"].iter_rows(values_only=True): ...
```

## 坑（按踩中频率排序）

- **`data_only=True` 只给「读」用**：返回的是 Excel 上次保存时缓存的计算值，
  刚生成、没在 Excel 里打开过的文件里公式格全是 None。写文件用默认 load_workbook。
- **公式格存的是字符串**：`ws["A1"] = "=SUM(B1:B10)"` 合法但 openpyxl 不计算公式，
  要等 Excel/WPS 打开才算出结果。需要立刻出数就在 Python 里算好写数值。
- **CSV 来的数字是字符串**：append 前 `int()` / `float()` 转换，
  否则 Excel 里变文本、求和为 0。
- **合并单元格**：先 `ws.merge_cells("A1:C1")`，值写在左上格；
  合并区内其他格是 MergedCell，写入会报错。
- **sheet 名**：≤31 字符，不能含 `[]:*?/\`，重名报错。
- **load_workbook 往返会丢特性**：.xlsm 的宏要 `keep_vba=True`；透视表、部分图形可能丢。
  改现有文件先备份或「另存为」，别直接覆盖原件。
- **大文件**：写 >10万行用 `Workbook(write_only=True)` 流式 append；读用 `read_only=True`，
  别整簿 load。
- **样式写一次**：同一格先写值再设样式；整列套格式用循环，没有继承一说。

## 验证

生成/修改后必须回读核对，再交付：

```python
wb = load_workbook("out.xlsx")
print([ws.title for ws in wb])
print(wb["明细"].max_row, wb["明细"].max_column)
for r in wb["明细"].iter_rows(min_row=1, max_row=3, values_only=True): print(r)
```

核对三点：表数量与名称、行数与源数据一致、抽查数字格类型是 number 不是 text。
用户要看观感（列宽/图表长什么样）且机器上有 LibreOffice 时，可转 PDF 看图：
`soffice --headless --convert-to pdf out.xlsx`；没有就把核对结果（行数、抽查值）报给用户。

## 老格式 .xls

openpyxl 不读 .xls：`soffice --headless --convert-to xlsx in.xls` 先转换（有 LibreOffice 时），
或 `pip install xlrd` 只读提取。
