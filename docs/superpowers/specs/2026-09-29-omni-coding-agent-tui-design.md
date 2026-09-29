# omni — 类 opencode 的终端 AI 编码工作台（设计方案）

日期：2026-09-29
状态：已批准

## 1. 目标

在仓库 `https://atomgit.com/kken/demo` 中从零构建一个类 [opencode](https://github.com/sst/opencode) 的 CLI TUI 编码智能体工具，命名为 **omni**。

核心能力：
- 终端 TUI 聊天式交互（Bubble Tea / Lip Gloss）
- 多 LLM Provider 抽象（OpenAI 兼容 / Anthropic / Gemini）
- Agent 循环：LLM 产出结构化工具调用，Agent 执行并将结果回传，形成闭环
- 工具集：`read` / `write` / `edit` / `bash` / `grep` / `glob`
- Agent 模式切换：`build`（全部工具）与 `plan`（仅只读工具）
- 生成中断（Esc）、重新生成（Ctrl+r）、slash 命令（/new /model /mode /help）

## 2. 技术栈与结构

- Go 1.22+，依赖 `bubbletea`、`bubbles`、`lipgloss`
- Module 名：`omni`

```
demo/
├── main.go                 入口 / flag 解析 / TUI 或 --print 非交互模式
├── internal/
│   ├── config/             配置加载（JSON + env 探测 + 默认值）
│   ├── llm/                Provider 抽象（openai / anthropic / gemini）
│   ├── tools/              工具注册表与实现（read/write/edit/bash/grep/glob）
│   ├── agent/              Agent 循环、消息历史、模式、中断
│   └── tui/                Bubble Tea 界面（布局 / 视图 / 样式 / 事件桥接）
├── config.example.json     配置模板
└── README.md               使用说明
```

## 3. Agent 循环（数据流）

```
用户输入 → 追加 user 消息 → Agent 循环:
  ① 组装历史 + system prompt + 工具定义 → 流式调用 LLM
  ② 产出 tool_use → 执行工具（plan 模式剪裁只读）→ 结果回填消息 → 回到 ①
  ③ 产出纯文本且无工具调用 → 结束本轮
```

- Agent 循环运行在独立 goroutine，通过 channel 向 UI 发送事件：
  `text_delta` / `tool_start` / `tool_output` / `tool_end` / `error` / `done`
- UI 侧用 `tea.Cmd` 桥接 channel 到 Bubble Tea 消息流
- 中断通过 context.Context 取消整轮

## 4. Provider 抽象

```go
type Provider interface {
    Name() string
    Models() []string
    Chat(ctx context.Context, req ChatRequest, onEvent func(StreamEvent) error) (*Message, error)
}
```

实现：
- **OpenAI 兼容**（`type: openai`）：`/chat/completions` SSE 流式；Ollama、DeepSeek 等通过 baseURL 复用
- **Anthropic**（`type: anthropic`）：`/v1/messages`，tool_use / tool_result
- **Gemini**（`type: gemini`）：`:streamGenerateContent?alt=sse`，functionDeclarations

注册表按配置声明的 `providers[].type` 创建；无配置文件时自动从环境变量探测 provider。

## 5. 工具集

| 工具 | 参数 | 只读 | 说明 |
|------|------|------|------|
| read | path, offset?, limit? | ✅ | 读取文件片段 |
| write | path, content | ❌ | 整文件写入（工作目录内） |
| edit | path, old_string, new_string | ❌ | 精确字符串替换，未命中报错 |
| bash | command, timeout? | ❌ | 执行 shell 命令，输出截断 |
| grep | pattern, path?, include? | ✅ | 正则搜索 |
| glob | pattern | ✅ | 文件模式匹配 |

- 默认工作目录限定：文件工具只允许写工作目录内文件（`--unsafe` 绕过）
- bash 默认 10s 超时，输出默认 4000 字符截断

## 6. Agent 模式

- `build`：全部工具
- `plan`：write/edit/bash 不可用，执行时拒绝并提示
- 通过 system prompt 差异 + 工具定义剪裁实现

## 7. TUI 布局

```
┌─ header ─────────────────────────────┐  ▶ logo / 模型 / 模式徽标 / 会话
│ 闲聊消息流（可折叠工具调用树）...    │
│  ▸ bash "go test ./..."   [展开]     │
├─ status ────────────────────────────┤  ▶ 生成中 spinner + 当前工具
│ 输入区（多行）                       │  ▶ Enter 发送 / Alt+Enter 换行
└──────────────────────────────────────┘
```

快捷键：`Esc` 取消生成、`Ctrl+c` 退出（生成中先取消）、`Ctrl+o` 展开/折叠工具树、`Ctrl+r` 重新生成、`Ctrl+k/Ctrl+l` 滚动、`/` slash 命令。

## 8. 配置

- 默认路径 `~/.config/omni/config.json`
- 字段：`provider`、`model`、`providers[]`、`max_tool_output`、`bash_timeout_seconds`
- 环境变量：`OPENAI_API_KEY` / `ANTHROPIC_API_KEY` / `GEMINI_API_KEY`
- flags：`--config`、`--model`、`--mode`、`--cwd`、`--unsafe`、`--print`（非交互单轮）、`--version`

## 9. 错误处理

- LLM 网络错误/超时 → 界面错误气泡，允许重试
- 工具执行失败 → 结果以 is_error 回传模型，界面标记
- 工具输出超限截断
- 中断：两次 Ctrl+c 强制退出；Esc 取消当前轮

## 10. 测试

- `llm`：httptest 模拟 SSE 流式响应
- `tools`：各工具独立单测（tmpdir）
- `agent`：mock provider 驱动工具闭环 / plan 只读 / 迭代上限
- `config`：默认值与文件加载
- 不测试 TUI 渲染细节，仅测消息驱动的 model 状态转移

## 11. 里程碑

- M1 骨架 + config + 基础 TUI 布局
- M2 Provider 抽象 + 流式聊天
- M3 工具集 + UI 工具树
- M4 agent 模式 + 中断 + slash 命令
- M5 测试 + README + config.example.json