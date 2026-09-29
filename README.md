# omni — 终端 AI 编码智能体

类 [opencode](https://github.com/sst/opencode) 的 CLI TUI 编码智能体，在终端里与 AI 协作编程。

## 特性

- **TUI 聊天界面**：Bubble Tea 构建，消息流、流式输出、工具调用树（可折叠）
- **网页版**：`/webui` 在浏览器打开本地网页聊天（127.0.0.1，SSE 流式）
- **Slash 提示**：输入 `/` 弹出命令列表，↑↓ 选择、Tab 补全
- **多 Provider**：OpenAI 兼容 / Anthropic / Gemini / Ollama 等
- **Agent 模式**：`build`（可写文件、执行命令）与 `plan`（只读）
- **内置工具**：`read` / `write` / `edit` / `bash` / `grep` / `glob`
- **交互控制**：Esc 取消生成、Ctrl+R 重新生成、slash 命令切换模型/模式
- **非交互模式**：`--print "问题"` 供脚本与 CI 使用

## 安装

```sh
go build -o omni .
```

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

交互模式：

```
输入消息，Enter 发送，Alt+Enter 换行；/help 查看帮助
```

## 配置文件

默认读取 `~/.config/omni/config.json`，模板见 `config.example.json`。也可用 `--config` 指定路径。

```json
{
  "provider": "gpt-4o",
  "providers": [
    { "type": "openai", "model": "gpt-4o", "env_key": "OPENAI_API_KEY", "default": true },
    { "type": "ollama", "model": "qwen2.5-coder", "base_url": "http://localhost:11434/v1" }
  ],
  "max_tool_output": 4000,
  "bash_timeout_seconds": 10
}
```

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

`/help` `/new` `/config` `/model <名称>` `/mode build|plan` `/webui` `/quit`

## 工具

| 工具 | 可写 | 说明 |
|------|:----:|------|
| read | 只读 | 读取文件（支持偏移与行数限制） |
| write | 写 | 整文件写入 |
| edit | 写 | 精确字符串替换 |
| bash | 执行 | 运行 shell 命令（默认 10s 超时） |
| grep | 只读 | 正则搜索 |
| glob | 只读 | 文件名模式匹配（支持 `**`） |

## 开发

```sh
go build ./...        # 编译
go test ./...         # 单元与集成测试
go vet ./...          # 静态检查
```

## 目录结构

```
├── main.go              入口 / flag / provider 解析 / TUI / --print
├── config.example.json  配置模板
└── internal/
    ├── config/          配置加载
    ├── llm/             Provider 抽象（openai / anthropic / gemini）
    ├── tools/           工具实现与安全边界
    ├── agent/           Agent 循环 / 模式 / 历史
    └── tui/             Bubble Tea 界面
```