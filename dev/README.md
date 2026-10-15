# dev — 本地 MCP 服务器

实验性 MCP 服务器集合，独立于 omni 主程序。构建后在 设置 → 插件 中自行导入，不导入不影响任何功能。

## 服务器

| 目录 | 工具 | 说明 |
|---|---|---|
| `pdf` | `read_pdf`、`write_pdf` | 提取 PDF 文本（仅文本型，不支持扫描件/加密）；从纯文本生成 PDF（A4、中文按 STSong-Light 渲染，仅文本版式无表格/图片） |
| `xlsx` | `read_xlsx`、`write_xlsx` | 读取 .xlsx/.xlsm（markdown 表格）；生成多工作表（列宽/表头加粗/冻结首行）。不支持 .xls 老格式 |
| `docx` | `read_docx`、`write_docx` | 提取 .docx 正文；从内容块（标题/段落/列表/表格）生成 .docx。不支持 .doc 老格式 |
| `pptx` | `read_pptx`、`write_pptx` | 按页提取 .pptx 文本；从 slides（标题/要点/备注）生成 16:9 .pptx。不支持 .ppt 老格式 |
| `csv` | `read_csv`、`write_csv` | 读取 CSV/TSV 为 Markdown 预览（附行数/列数/编码/分隔符，自动识别 UTF-8 BOM 与 GB18030，容忍列数不齐的脏数据）；生成 CSV（默认带 UTF-8 BOM，Excel 双击不乱码） |
| `image` | `image_info`、`image_resize`、`image_convert`、`image_compress` | 图片处理：元信息、缩放（等比装箱/严格拉伸/按比例，双线性插值）、格式转换（png/jpg/gif）、压缩（按质量重编 jpg，可选限最大宽/最大边长）。重编码会剥离 EXIF/GPS；不支持 webp |
| `media` | `media_setup`、`media_info`、`media_convert`、`media_extract_audio`、`media_thumbnail` | 音视频：读取信息（时长/分辨率/码率/编码）、格式转换（容器与编码按后缀推断，可只取音频或仅重封装）、提取音轨、视频截图。**基于 ffmpeg，但安装包不内置**（两个静态 exe 约 180MB，是安装包最大的一块）——首次用音视频功能前先调 `media_setup`（它会先探测本机已有 ffmpeg 并复用，只补缺失的），详见下文「ffmpeg / ffprobe：按需安装」 |
| `fetch` | `fetch_html`、`fetch_text`、`fetch_markdown`、`fetch_json`、`fetch_readable` | 抓取网页：HTML/纯文本/Markdown/JSON/正文提取。含 robots.txt 遵循与 SSRF 防护（拒绝私网/内网），响应体上限 5MB |
| `agnes` | `generate_image`、`generate_video` | Agnes AI 生图（agnes-image-2.5-flash / 2.1-flash，文生图与图生图）与生视频（agnes-video-2.5 / 2.5-flash，异步轮询）。需环境变量 `AGNES_API_KEY` |
| `mimo` | `tts` | MiMo 语音合成（mimo-v2.5-tts / voicedesign / voiceclone）。需环境变量 `MIMO_API_KEY` |

均为 stdio 服务器、纯 Go 实现、无外部进程依赖。agnes / mimo 产出图片、视频、音频附件，导入后可直接在对话里显示/播放（图片点击可放大）。

## 构建

构建产物直接输出到 `.omni/mcp/`——主程序启动时会自动扫描该目录（以及数据目录 `~/.local/share/omni/mcp`），把里面的可执行文件注册为内置 MCP 插件，无需手动导入：

```bash
go build -o .omni/mcp/pdf.exe ./dev/pdf
go build -o .omni/mcp/xlsx.exe ./dev/xlsx
go build -o .omni/mcp/docx.exe ./dev/docx
go build -o .omni/mcp/pptx.exe ./dev/pptx
go build -o .omni/mcp/csv.exe ./dev/csv
go build -o .omni/mcp/image.exe ./dev/image
go build -o .omni/mcp/media.exe ./dev/media
go build -o .omni/mcp/fetch.exe ./dev/fetch
go build -o .omni/mcp/agnes.exe ./dev/agnes
go build -o .omni/mcp/mimo.exe ./dev/mimo
```

一次全编（Windows，其余平台把 `.exe` 去掉）：

```bash
for P in pdf xlsx docx pptx csv image media fetch agnes mimo; do go build -o ".omni/mcp/$P.exe" "./dev/$P"; done
```

> 桌面版请把产物放到 `webui/desktop/bin/`（内核 exe 同目录，dev 态与打包态都会扫描）；
> 该目录已 gitignore，不会入库。

## ffmpeg / ffprobe：按需安装（不再随包内置）

`dev/media` 是 ffmpeg 的前端，需要完整版 ffmpeg（Electron 自带的 `ffmpeg.dll` /
mac `libffmpeg.dylib` 是 Chromium 裁剪版：只解码、无编码器、不可当命令行调用，**不能用于转码**）。

**安装包不再内置 ffmpeg**（2026-10-16 起）。两个静态 exe 合计约 180MB，是安装包里
最大的一块，且绝大多数用户从不碰音视频功能；NSIS 也压不动（exe 本身已压缩）。
改为 AI 在对话中按需调用 `media_setup` 工具准备环境：

- **先探测再下载**：很多用户自己装过 ffmpeg（brew / winget / scoop / apt，或手动解压
  加进 PATH）。`media_setup` 先跑一遍 `findTool` 的 4 级查找，命中就**直接复用、
  一个字节都不下载**，只补真正缺失的那个（例如系统有 ffmpeg 没 ffprobe，就只装
  ffprobe）。报告会写明用的是哪一份（环境变量 / 插件同目录 / omni 数据目录 / 系统 PATH）
  以及版本号，便于判断系统那份是不是太老或是缺编码器的裁剪版；
  真需要自己那份时传 `force: true`（无视现有环境，全部装到数据目录）；
- **装到** `~/.local/share/omni/bin/`（即 `findTool` 的第 3 级查找路径）——
  用户数据目录，安装包升级不覆盖，装一次永久可用；
- **来源**：主源 npm 平台包 `@ffmpeg-installer/*` / `@ffprobe-installer/*`
  （国内优先走 `registry.npmmirror.com`，tar.gz 约 20-40MB）；
  回退 `eugeneware/ffmpeg-static` b6.0 的 `.gz` 资源（单文件 18-30MB，兼作
  `windows/arm64` 的唯一来源——npm 两个包都没有 arm64 的 Windows）；
- **实现**：`dev/media/setup.go`。本地想预先装好（开发/测试）可直接：

```bash
cd dev/media && go test -run TestFromNpmReal -v   # 顺带把 ffmpeg 装到数据目录
```

或手动放置（优先级高于数据目录）：把 ffmpeg/ffprobe 放到插件 exe 同目录或其
`ffmpeg/` 子目录，或用 `OMNI_FFMPEG` / `OMNI_FFPROBE` 环境变量指定完整路径。

> ⚠️ `webui/desktop/scripts/fetch-ffmpeg.mjs` 已**退出打包链路**（CI 不再调用，
> `beforePack` 钩子里的换名逻辑也已删除，`extraResources` filter 排除 `ffmpeg*`）。
> 脚本本体保留作本地取静态构建的备用手段，但它与 `setup.go` 是**两套独立来源**，
> 改下载源时别只改一处。三个平台的 CI 产物校验已反向断言「包内不得含 ffmpeg」。

> ⚠️ **若手工放置 ffmpeg，注意别放 `bin/` 根目录**：内核启动时会扫描自身 exe 同目录，
> 把里面的**每个可执行文件**自动注册为 MCP 插件；ffmpeg 放根目录会被当成 MCP 服务器去连接
> （必然失败），插件列表冒出连不上的幻影条目。子目录会被扫描跳过。同理，内核也会跳过
> `omni*` 开头的文件（主程序与按架构的备料 `omni-windows-x64.exe` 等）。

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
