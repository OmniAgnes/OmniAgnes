# dev — 本地 MCP 服务器

实验性 MCP 服务器集合，独立于 omni 主程序。构建后在 设置 → 插件 中自行导入，不导入不影响任何功能。

## 服务器

| 目录 | 工具 | 说明 |
|---|---|---|
| `pdf` | `read_pdf` | 提取 PDF 文本，按页返回。仅文本型 PDF，不支持扫描件/加密文件 |
| `xlsx` | `read_xlsx`、`write_xlsx` | 读取/生成 .xlsx/.xlsm（markdown 表格 / 二维行数组）。不支持 .xls 老格式 |
| `docx` | `read_docx` | 提取 .docx 正文（标题映射 markdown #、表格映射 markdown 表）。不支持 .doc 老格式 |
| `pptx` | `read_pptx` | 按页提取 .pptx 文本，含演讲者备注。不支持 .ppt 老格式 |

均为 stdio 服务器、纯 Go 实现、无外部进程依赖。

## 构建

```bash
go build -o dev/bin/pdf.exe ./dev/pdf
go build -o dev/bin/xlsx.exe ./dev/xlsx
go build -o dev/bin/docx.exe ./dev/docx
go build -o dev/bin/pptx.exe ./dev/pptx
```

## 导入

设置 → 插件 → 添加服务器，类型选 stdio，命令填构建产物的绝对路径，例如：

```json
{
  "mcp_servers": {
    "pdf":  { "command": "C:\\newcode\\TEST\\demo\\omni_agent\\dev\\bin\\pdf.exe" },
    "xlsx": { "command": "C:\\newcode\\TEST\\demo\\omni_agent\\dev\\bin\\xlsx.exe" }
  }
}
```

导入后工具名为 `mcp__pdf__read_pdf` 这类格式。文档路径参数用相对路径时，基于 MCP 进程的工作目录（即 omni 的当前工作目录）。

## 验证

`dev/testdata/` 内有四种格式的样例文件（`go run ./dev/gen` 可重新生成），导入后让模型读取试试。
