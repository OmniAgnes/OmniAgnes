![omni 桌面版](screenshot/1.png)

![omni 桌面版](screenshot/2.png)

![omni 桌面版](screenshot/3.png)

桌面版 / 终端 TUI / 网页工作台，在本地与 AI 协作编程。模型与供应商在设置里配置，开箱即用。

## 功能

- **三种形态，同一内核**：桌面版（Electron）、TUI（`omni`）、网页版（`omni --web`，SSE 流式、适配移动端），共用同一份配置与数据
- **多 Provider**：OpenAI 兼容 / Anthropic / Gemini / Ollama，聊天底部随时切换模型与 build/plan 模式
- **MCP**：支持 stdio / streamable HTTP 服务器，远端工具注入为 `mcp__服务器__工具`
- **技能与记忆**：`SKILL.md` 定义技能，匹配自动调用、`/技能名` 直接触发；全局记忆跨会话沉淀
- **计划任务**：到点（每 N 天 HH:MM）自动执行，错过 24 小时内自动补跑
- **移动端远程访问**：桌面端扫码即可在手机上用同一份工作台，免登录，默认关闭
- **脚本可用**：`omni --print "问题"` 单轮问答，供脚本与 CI 使用
- **omni-server 后台（可选）**：手机号登录、会员与积分、模型与供应商管理（见 `server_cloudflare/`）

## 安装

macOS / Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.sh | sh
```

Windows（PowerShell）：

```powershell
irm https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.ps1 | iex
```

装完敲 `omni` 进 TUI，不需要桌面端、Electron 或 Go 环境。
内含内核 + 内置插件，插件需与 `omni` 主程序放同目录。其他附件与桌面安装包见 [Releases]。

从源码安装：

```sh
go build -o omni .
```

## 快速开始

桌面版双击安装包，在「设置 → 模型」添加供应商（Base URL / API Key / 模型）即可开始。
命令行版首次运行 `/config` 添加供应商；`/help` 查看全部命令，`/webui` 打开网页版。

## 说明

- 配置在 `~/.config/omni/config.json`（模板 `config.example.json`），数据在 `~/.local/share/omni/`
- 实验性产品：build 模式下 AI 可直接读写文件、执行命令，使用前注意备份

## 许可

[MIT](./LICENSE)

[Releases]: https://github.com/OmniAgnes/OmniAgnes/releases/latest
