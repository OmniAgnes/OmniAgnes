![omni 桌面版](screenshot/1.png)

![omni 桌面版](screenshot/2.png)

![omni 桌面版](screenshot/3.png)

![omni 桌面版](screenshot/4.png)

![omni 桌面版](screenshot/5.png)

# omni — 终端 AI 编码智能体

CLI TUI 编码智能体，在终端里与 AI 协作编程，并附带网页版工作台。

## 特性

- **TUI 聊天界面**：Bubble Tea 构建，消息流、流式输出、工具调用树（可折叠）
- **网页版工作台**：`--web` 或 TUI 内 `/webui` 打开（127.0.0.1，SSE 流式）
  - 左侧「任务 / 项目」：任务即发出去的一条条对话；项目是本地文件夹，各自管理任务
  - 任务列表默认显示 5 条，更多折叠展开
  - 设置：供应商与模型管理（URL / API Key / 多模型 / 上下文窗口 / 最大输出 / 输入类型 / 推理等级）、全局记忆、主题模式与 6 套主题风格、技能与 MCP 状态
  - 聊天底部可随时切换模型与 build/plan 模式，选择自动记住
  - 界面基于 [shadcn/ui](https://ui.shadcn.com/)，适配移动端（H5）
  - 任务与会话历史持久化在 `~/.local/share/omni/`
  - 桌面版：Electron 无边框窗口（自绘标题栏、原生文件夹选择），见 `webui/desktop`
- **MCP 支持**：`mcp_servers` 配置 stdio / streamable HTTP 服务器，远端工具以 `mcp__服务器__工具` 注入
- **技能（Skills）**：`SKILL.md` 定义技能，agent 自动调用，也可 `/技能名` 直接触发
- **全局记忆**：跨会话长期记忆，agent 可通过 `memory` 工具自动沉淀，设置面板可编辑
- **多 Provider**：OpenAI 兼容 / Anthropic / Gemini / Ollama 等
- **Agent 模式**：`build`（可写文件、执行命令）与 `plan`（只读）
- **非交互模式**：`--print "问题"` 供脚本与 CI 使用

## 安装

```sh
go build -o omni .
```

需要 Node.js 的场景只有修改前端（`webui/frontend`）；仓库已提交构建产物，日常编译无需 Node。

## 快速开始

配置至少一个 LLM Provider（`~/.config/omni/config.json`），或无配置文件时直接使用环境变量：

```sh
export OPENAI_API_KEY=sk-...
./omni
```

**无任何配置也能启动**：TUI 会自动打开配置表单（或随时输入 `/config`），
填写类型 / 模型 / Base URL / API Key 并保存后立即生效，同时写入配置文件供下次启动使用。

本地 Ollama：

```sh
export OLLAMA_BASE_URL=http://localhost:11434/v1
export OLLAMA_MODEL=qwen2.5-coder
./omni
```

网页版工作台：

```sh
./omni --web      # 直接启动（或 TUI 内输入 /webui）
```

## 配置文件

默认读取 `~/.config/omni/config.json`，模板见 `config.example.json`。也可用 `--config` 指定路径。

```json
{
  "provider": "agnes-3.0-flash",
  "providers": [
    {
      "name": "agnes",
      "type": "openai",
      "models": ["agnes-3.0-flash", "agnes-2.5-flash"],
      "base_url": "https://api.agnes-ai.cn/v1",
      "api_key": "sk-…",
      "context_window": 128000,
      "max_output_tokens": 8192,
      "input_types": ["text", "image", "pdf"],
      "reasoning": "default",
      "default": true
    }
  ],
  "max_tool_output": 4000,
  "bash_timeout_seconds": 10,
  "theme": "dark",
  "theme_color": "zinc",
  "mcp_servers": {
    "filesystem": { "command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "."] },
    "fetch": { "url": "http://127.0.0.1:8931/mcp" }
  }
}
```

- 供应商也可以直接在网页版「设置 → 模型」中维护（名称 / 类型 / Base URL / API Key / 多模型 / 上下文窗口 / 最大输出 / 输入类型 / 推理等级）；`max_output_tokens` 会作为该供应商模型的实际输出上限。
- 聊天输入框右下角可随时切换模型，选择会写回配置文件。

### 主题风格

`theme_color` 提供 6 套配色：`zinc`（石墨）/ `violet`（紫罗兰）/ `blue`（海洋）/ `emerald`（翡翠）/ `amber`（琥珀）/ `rose`（玫瑰），配合 `theme`（dark/light/system）组合使用。

### MCP 服务器

- stdio：`{"command": "...", "args": [...], "env": {...}}`，本地拉起子进程
- streamable HTTP：`{"url": "http://.../mcp"}`
- `"disabled": true` 可临时停用某个服务器

工具注入后命名为 `mcp__<服务器>__<工具>`；服务器发出工具变更通知时自动刷新（TUI 内 `/mcp` 查看状态）。

网页版「设置 → 插件 → MCP」里点「打开配置」会直接在文件管理器中打开 `config.json`，
按 `mcp_servers` 结构加服务器，改完点「刷新」触发重连。

### 技能（Skills）

目录约定（均为 `名称/SKILL.md`）：

- 全局：`~/.config/omni/skills/<名称>/SKILL.md`
- 项目：`<项目>/.omni/skills/<名称>/SKILL.md`（覆盖同名全局技能）

```markdown
---
name: review
description: 审查当前代码改动并给出风险与改进建议
---

请先用 git diff 查看当前改动，然后……
```

agent 在任务匹配技能描述时会自动调用 `skill` 工具；也可以直接输入 `/review` 触发。

网页版「设置 → 插件 → 技能」提供三种装法：

- **打开目录**：直接拉起系统文件管理器，把 `SKILL.md`（或整个技能文件夹）粘进去保存，回到界面点「刷新」即加载
- **粘贴导入**：把 SKILL.md 全文贴进文本框，导入时自动解析 frontmatter；没有 frontmatter 就从正文推断名称和描述
- **添加技能**：表单式新建，适合手写简单技能

### 全局记忆

文件：`~/.config/omni/memory.md`。内容注入每轮系统提示词，agent 可用 `memory` 工具追加/替换；
网页版设置面板可直接编辑。

## 命令行参数

| 参数 | 说明 |
|------|------|
| `--config <path>` | 配置文件路径 |
| `--model <name>` | 覆盖模型名 |
| `--mode <build\|plan>` | Agent 模式 |
| `--cwd <dir>` | 工作目录（默认当前目录） |
| `--unsafe` | 允许工具访问工作目录之外 |
| `--print "<query>"` | 非交互模式，单轮问答后输出并退出 |
| `--timeout 5m` | 非交互模式整体超时 |
| `--web` | 不进 TUI，直接启动网页版工作台 |
| `--no-open` | 配合 `--web`，不自动打开系统浏览器（桌面版/调试用） |
| `--version` | 打印版本 |

## 快捷键

| 按键 | 功能 |
|------|------|
| Enter | 发送消息 |
| Alt+Enter / Ctrl+J | 换行 |
| Esc | 取消当前生成 |
| Ctrl+C | 退出（生成中再次按键强制退出） |
| Ctrl+O | 展开 / 折叠工具调用 |
| Ctrl+R | 重新生成最后一条回复 |
| Ctrl+L | 滚动到底部 |

## Slash 命令

`/help` `/new` `/config` `/model <名称>` `/mode build|plan` `/skills` `/memory` `/mcp` `/webui` `/quit`
（外加已安装技能的 `/技能名`）

## 工具

| 工具 | 可写 | 说明 |
|------|:----:|------|
| read | 只读 | 读取文件（支持偏移与行数限制） |
| write | 写 | 整文件写入 |
| edit | 写 | 精确字符串替换 |
| bash | 执行 | 运行 shell 命令（默认 10s 超时） |
| grep | 只读 | 正则搜索 |
| glob | 只读 | 文件名模式匹配（支持 `**`） |
| skill | 只读 | 读取技能指令 |
| memory | 写 | 读写全局记忆 |
| `mcp__*` | 视服务器 | MCP 远端工具（plan 模式默认不可用） |

## 开发

```sh
go build ./...        # 编译
go test ./...         # 单元与集成测试
go vet ./...          # 静态检查
```

前端（webui/frontend，React + Vite + Tailwind + shadcn/ui）：

```sh
cd webui/frontend
npm install
npm run build         # 产物输出到 internal/webui/dist，由 go:embed 打进二进制
OMNI_URL=http://127.0.0.1:PORT npm run dev   # 调试：代理到正在运行的 omni
```

桌面版打包（webui/desktop）：

```sh
go build -o webui/desktop/bin/omni.exe .   # 先编译内核
cd webui/desktop
npm install
npm run dist        # 产出 NSIS 安装包（release/omni-Setup-x64.exe）
```

## 目录结构

```
├── main.go              入口 / flag / 扩展装配 / TUI / --print / --web
├── config.example.json  配置模板
├── internal/
│   ├── config/          配置加载（providers / theme / mcp_servers）
│   ├── llm/             Provider 抽象（openai / anthropic / gemini）+ 解析
│   ├── tools/           工具实现与安全边界（支持动态注册）
│   ├── agent/           Agent 循环 / 模式 / 历史
│   ├── mcp/             MCP 客户端管理（stdio / streamable HTTP）
│   ├── skills/          技能（SKILL.md）加载
│   ├── memory/          全局记忆
│   ├── extras/          技能/记忆/MCP 聚合装配
│   ├── store/           任务与项目持久化
│   ├── tui/             Bubble Tea 界面
│   ├── webui/           网页版服务 + 内嵌前端 dist
│   └── ...
├── webui/frontend/      前端源码（React + shadcn/ui）
└── webui/desktop/       桌面版 Electron 壳
```
