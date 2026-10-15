# omni Agent

Go 内核 + React 前端 + Electron 桌面端 + Cloudflare Workers 后台的单体仓库。
本文件只记「可复用的结论与触点清单」，以代码为准，改动后同步。

> **新任务先读**：`docs/ARCHITECTURE.md`（一页代码地图）+ `.workbuddy/memory/MEMORY.md`（项目长期约定）+ 最近一日志 `YYYY-MM-DD.md`，再按需下钻，避免重扫全仓。

## 目录

- `internal/` 后端模块：`agent`(会话编排) `llm`(多 provider) `mcp` `tools` `skills`
  `memory` `store`(持久化) `config` `webui`(HTTP/SSE) `docparse` `extras` `tui`
- `webui/frontend/` React + Vite 前端（产物由 Go 内核 `internal/webui` 内嵌分发）
- `webui/desktop/` Electron 壳；`bin/` 放内核副本与内置工具 exe（不入库）
- `server_cloudflare/` 账号/积分后台（Hono + TS，worker 名 `omni-server`，D1 `omni_server`）；
  后台前端源码在它的 `web/`，产物 `dist/` 由 wrangler assets 分发。桌面端说的手机号协议在
  `src/desk.ts`，邮箱那套在 `src/admin.ts`，两套并存、路由注册顺序决定优先级（先 desk 后 admin）
- `dev/` 自研 MCP 服务器源码（pdf/xlsx/docx/pptx/csv/image/media/fetch/agnes/mimo），构建 exe 不入库。
  （media 是 ffmpeg 的纯 Go 前端，ffmpeg/ffprobe 由 `webui/desktop/scripts/fetch-ffmpeg.mjs`
  按平台拉取，随包内置在 `resources/bin/ffmpeg/`；其余插件零外部依赖）
- ⚠️ 内核会把「自身 exe 同目录」里的**每个可执行文件**自动注册为 MCP 插件（仅跳过 `omni` 与
  `omni-*` 备料）。辅助程序（ffmpeg 等）**必须放子目录**（子目录会被扫描跳过），否则插件列表
  会冒出连不上的幻影条目。桌面端 bin/ 的正确布局：`omni.exe` + 各 `<插件>.exe` 在根，
  `ffmpeg/` 子目录放 ffmpeg/ffprobe，`mcp-<平台>-<架构>/` 放插件备料
  **新增插件三件事**：dev/ 写服务器 → `builtinMCPDescs`（internal/config/config.go）加描述 →
  `.github/workflows/release.yml` 6 处 `for P in ...` 全加上（win/mac/linux 编译+校验循环）。
  产物放 `webui/desktop/bin/`（内核 exe 同目录，dev 与打包态都扫描）

## 改前端必须走完四步

只跑 `npm run build` 不生效——桌面端跑的是 `webui/desktop/bin/` 里的旧内核：

```powershell
cd webui\frontend; npm run build      # 1. 前端产物
cd ..\..; go build -o omni.exe .      # 2. 重编内核（内嵌新前端）
copy omni.exe webui\desktop\bin\      # 3. 覆盖桌面端内核副本
# 4. 重启桌面端
```

不拷第 3 步 = 改了个寂寞。`.gitignore` 忽略 `webui/desktop/bin/`，别提交。

## Windows 上的坑

- 禁用 `Set-Content`/`>` 写中文文件，必然坏 UTF-8；用编辑器或 Edit/Write 工具。
  已坏：`git show HEAD:路径 > 路径` 恢复后用 node 校验。
- 禁止创建 `nul` 文件：cmd 风格 `> nul` 在 bash 下会真建文件。
- `commit && push` 链式命令里 push 会静默不执行；push 单独跑，
  并用 `git status -sb` 确认 ahead 归零。
- 调试日志统一写 `.omni/log/`（已 gitignore）。

## 代码约定

- 后端配置示例见 `config.example.json`；`.omni/` 为本地运行时数据目录
- 桌面端界面缩放走 Electron `setZoomFactor`（IPC `omni:zoom`，钳 0.5~2），
  禁止根容器 CSS `zoom`（会撑破布局、portal 弹窗错位）
- 上游 500/429/502/503/504 走统一重试；进度用独立事件，不进思考流
- 空回复优先排查上下文长度（滑动窗口只保留最近 20 个用户轮）

## 移动端远程访问

桌面端左下角手机图标（设置图标左边）→ 悬停出二维码 → 开关「允许移动端连接此设备」。
实现全部在 `internal/webui/mobile.go`，前端 `components/MobilePanel.tsx`。

- **不用登录**：手机扫码直接是完整工作台。前端登录只管账号/积分，后台不可达时
  就是「纯本地版」（`probeServer()` 失败 → `serverOnline=false`），不构成门禁。
- **TUI 用不上**：`internal/tui` 是 bubbletea 终端程序，无 HTTP 监听，手机渲染不了。
  移动端复用桌面端同一个 `webui` handler（同一份任务/会话数据），另开 LAN 监听器。
- **门禁 = 设备 token**，落盘 `~/.local/share/omni/mobile.json`（256 位随机，
  base64url）。二维码 URL 形如 `http://<内网IP>:8792/device/<token>`；
  首次访问校验后换 HttpOnly Cookie 并 302 到 `/`，token 不留在地址栏。
  走 Cookie 而非 Authorization 头，因为 `/api/events` 是 EventSource，
  浏览器不允许给 SSE 带自定义头。
- **开关状态持久化**：重启后自动恢复监听，token 不变、二维码继续有效。
  副作用是重启不会自动收回端口——要断开就手动关开关。
- **token 不过期，但可手动换发**：`POST /api/mobile/rotate` 重新生成并落盘。
  旧二维码、旧链接、按旧 token 换到的 Cookie 全部立刻失效——不需要维护黑名单，
  `guard` 每个请求都拿当前 token 比对，已连着的手机下一次请求就被拒。
  不可逆，所以 `MobilePanel` 里必须先弹 `AlertDialog` 二次确认。
- **内网 IP / 线上域名**：二维码基址取 `mobile.json` 的 `public_url`，
  为空则回退到 `lanIPs()` 列出的本机 IPv4（当前形态）。部署到线上时把
  `public_url` 填成 `https://<域名>`，扫码 URL 自动变成 `https://<域名>/device/<token>`，
  不必改代码。开关往返不换 token，只有换发接口换。
- 端口 8792（与桌面端 8791 分开，避免动 localStorage 的 origin）；
  未开启时 guard 直接 404，不暴露端口存在。
- **多端实时同步**：交互式聊天过程中必须 `hub.broadcast()`（原先只有计划任务广播，
  导致另一端全程收不到任何信号）。做法是在 `handleChat` 里把「检查点落盘 + 广播」
  绑成节流的 `checkpoint()`（3s，EventText 与 EventToolEnd 都触发——纯文本长回复不经过
  工具调用，否则要等整轮结束才可见），整轮结束再广播一次。
  另一端收到信号后走既有逻辑：`GET /api/tasks` 刷「处理中」+ `GET /api/tasks/{id}`
  按快照重建视图；发起方自己因 `streaming` 标记跳过重建，不会闪烁。
- **同任务并发护栏**：`handleChat` 抢在**任何响应写入与 `a.Prepare`/`a.SetApprove` 之前**
  用 `runningTasks.LoadOrStore` 占位，否则第二个请求会先改坏共享 Agent 状态。
  放在 `send()` 之后无效——响应体一旦写出状态码就锁死 200，再 `WriteHeader(409)` 是 no-op。

## 子进程输出的编码（Windows 乱码）

`internal/tools/bash.go` 捕获子进程输出**必须按字节**接 `bytes.Buffer`，
再用 `toUTF8` 转码后再进 `json.Marshal`：

- 已是合法 UTF-8 → 原样返回（Git Bash 的工具基本都是）
- 否则按 GB18030 解码（GBK/GB2312 超集），覆盖中文 Windows 上 PowerShell/cmd
  及大量 exe 按 ANSI 码页(936)吐出的字节
- 都失败才退化成 `�`

**为什么必须转码**：坏字节进 `json.Marshal` 会被替换成 U+FFFD **永久写进会话文件**，
之后所有客户端（桌面/手机）从 store 读到的都是 `�`，模型也读不懂——
排查这类「乱码」先去 `~/.local/share/omni/sessions/*.json` 里搜 U+FFFD，
看它落在 `tool_result.content` 还是用户消息里，两者根因完全不同。
（用 node 读文件查，别用 PowerShell，Windows 控制台自身就是 GBK，会误导判断。）

顺带给子进程注入 `LANG=C.UTF-8` / `PYTHONIOENCODING=utf-8`，少走转码路径。

## 常用命令

```powershell
go build ./...                          # 编译检查
go vet ./...
cd webui\frontend; npm run typecheck    # 前端类型检查（不产产物）
cd webui\desktop; npm run check:bin     # 打包前校验内核副本存在
```

## 详细文档

- **代码结构地图**：`docs/ARCHITECTURE.md`（一页建立全局上下文：模块树、关键流程、组件触点、桌面壳触点、启动命令、版本号来源）。新任务先读它 + 本文件，按需下钻，别重扫全仓。
- **AI 长期记忆**：`.workbuddy/memory/MEMORY.md`（稳定约定，跨对话复用）+ `.workbuddy/memory/YYYY-MM-DD.md`（每日进展追加）。
- 历史排查结论见 git log。
