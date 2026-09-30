# omni_agent 项目长期笔记

## 项目概况
- Go 1.27 单体：`main.go`（TUI/Web 双入口，`--web` 进网页版工作台）+ internal/{agent,llm,store,tools,webui,tui,...}。
- 前端：`webui/frontend`（React 19 + Vite + Tailwind v4 + lucide-react，组件用 shadcn 风格 ui/ 目录），构建产物输出到 `internal/webui/dist`，经 go:embed 打进二进制。
- 桌面端：`webui/desktop`（Electron 壳，无边框自绘标题栏，拉起 `omni --web --no-open`），打包 `npm run dist`（NSIS），图标在 `build/icon.ico`，生成脚本 `make_icon.py`（Pillow+numpy，装在 `C:/Users/sun/.workbuddy/binaries/python/envs/default`）。AUMID 统一为 `com.omni.desktop`（main.js 的 `APP_AUMID` 与 electron-builder.yml 的 `appId` 必须一致）。
- 数据持久化：任务 = `~/.config/omni`（DefaultDataDir）下 sessions/*.json，项目 = projects.json。

## 构建链（改动必走）
1. 前端：`cd webui/frontend && npm run build`（tsc --noEmit + vite build，沙箱内命令前加 `NODE_OPTIONS=` 防 shim 干扰）
2. 内核：`go build -o omni.exe .`（改 main.go 后必跑，否则桌面端跑旧内核）
3. 同步：`cp omni.exe webui/desktop/bin/omni.exe`
4. 安装包：`cd webui/desktop && npm run dist`
- 沙箱启动桌面：`cd webui/desktop && env -u ELECTRON_RUN_AS_NODE -u NODE_OPTIONS npx electron . --disable-gpu --disable-gpu-sandbox`（ELECTRON_RUN_AS_NODE=1 会让 electron 退化成纯 Node；GPU 进程在沙箱内崩溃需禁 GPU）
- 发版：打 tag v* → `.github/workflows/release.yml` 双架构出 Win NSIS + mac dmg；CI 交叉编译内核用**备料名**（omni-<平台>-<架构>[.exe]）放 bin/，beforePack 钩子无条件覆盖正式名（顺序无关、幂等）
- **cp 到 `webui/desktop/bin/omni.exe` 报 `Device or resource busy`** = 旧 omni.exe 还在跑，先 `taskkill //F //IM omni.exe` 和 `taskkill //F //IM electron.exe` 再 cp（重启后后台任务显示 failed 是预期的，那是我自己杀的）。
- **改内置技能必须改三份**：仓库源 `.omni/skills/` → `go run ./build/gen-builtin-skills` → `internal/skills/embedded/`；**全局 `~/.config/omni/skills/` 要手动 cp**——`SeedBuiltIn` 见目录已存在就跳过，永不覆盖（改了源文件全局旧副本不会更新）。

## 已有约定
- 会话历史：webui `agentFor` 创建 Agent 时必须传 `History: t.Messages` 恢复持久化消息。
- 文件夹操作：右键菜单（打开/移除），移除走 AlertDialog 确认；行内 hover 只显示 PlusIcon 添加按钮；不显示任务数徽标；**文件夹行不做选中高亮**。
- 任务行：hover 右侧显示删除（用 lucide 图标，禁止 "×"/"+" 这类字符）。
- 标题栏：自绘 Titlebar 无底边框，左右两块背景分别对齐下方侧边栏（bg-sidebar w-72 border-r）与对话区（bg-background）。
- 对话头部只显示任务标题，**不显示模型名/目录路径**；模型选择只放在输入框右下角（发送按钮左边）。
- 打开文件夹 API：`POST /api/projects/{id}/open`（explorer 在 Windows 成功也返回非 0，用 Start 不等待）。
- 文件夹下任务**分页显示**：初始 5 条，底部「更多（还有 N 条）」按钮每次再加 5 条，加完按钮消失（`const PAGE = 5` + `useState` + `slice(0, limit)`）。
- 「任务」TAB 只放**无 project_id 的默认任务**；项目内任务只在对应文件夹下出现。
- TUI 列表对齐：slash 提示与 /model 选择器的描述列**按当前项最长名动态取列宽（间距≥3格）**，禁止固定列宽——长技能名（/brainstorming）会把描述贴到命令上（用户明确要求留距离）。
- `/model` 无参 = 全屏模型选择器（列 providers 全部模型，标注当前，Enter/数字键切换并持久化 cfg.Provider）；带参 = 直接切换。切换收录模型时重建 provider，ollama/localhost 免 key 校验。

## 滚动容器约定（重要，别再用 Radix ScrollArea）
- **本项目禁用 `components/ui/scroll-area.tsx`（已删除）**：Radix Viewport 是 `size-full`，在 flex 容器里撑不开 → 内容溢出但没有滚动条（已在 SettingsDialog、Chat、Sidebar、AddProjectDialog 上连续踩坑两次）。
- 统一用原生滚动：`<div className="omni-scroll min-h-0 flex-1 overflow-y-auto">`。
- **高度链必须完整**：`h-screen` → `flex flex-col` → `flex-1 min-h-0` → 子栏 `h-full min-h-0` → 滚动区 `min-h-0 flex-1`。少一个 `min-h-0` flex 子项就会被内容撑破。
- `.omni-scroll`（index.css）：细滚动条、悬停/聚焦显形、`overscroll-behavior: contain`。Chromium + Firefox 双端生效。
- 长列表自动跟随新内容：用 ref 记 `距底 < 阈值(80px)` 的 sticky 标志，**不要**用 `scrollIntoView`（会在流式输出时把用户上滚位置强行拽回底部）。

## 访问模式
- 语义命名：`build` = **完全访问**（直接执行）；`plan` = **普通访问**（写操作前逐条问用户）。
- 实现：`agent.Options.Approve func(callID, name string, args map[string]any) bool` + `Agent.SetApprove`。
  - plan 模式下若 Approve 非 nil → `toolDefs()` 下发**全量**工具，`Execute` 里对非只读工具先 emit `EventToolStart`/`EventApproval` 再阻塞等审批。
  - Approve 为 nil（纯 TUI）→ plan 退化为 `ReadOnlyDefinitions()` + `forceReadOnly`。
- webui 审批流：server 持有 `approvals map[string]*approvalReq`（含 `ch chan bool`），`POST /api/approvals/{id}` body `{"allow":bool}`；SSE 事件
  `tool_start` → `approval_pending` → `approval{id,call_id,name,args}` → （前端 POST）→ `approval_result{allowed}` → `tool_output`/`tool_end`。
  `waitApproval` 3 分钟超时视为拒绝。
- 前端确认 UI 在 `ToolCallView` 里（琥珀色卡片 + 拒绝/允许按钮），挂在最近一张 running 工具卡上。

## 工具卡片展示约定（不露 JSON、不要边框）
- 组件 `components/ToolCallView.tsx`：`META` 表把 read/write/edit/bash/grep/glob 映射成图标 + 中文标签（读取/写入/编辑/bash/搜索/查找）+ 一句话摘要。
- **折叠行不要 border、不要常驻背景色**（用户明确要求「就是普通的」），只在 hover 时给 `bg-muted/50`；展开详情用 `bg-muted/40 rounded-md` 独立块区分，不靠 `border-t`。
- 行内元素顺序：`工具图标 → 名称 → 摘要 → 状态图标(对号/叉/转圈，ml-auto) → 展开箭头`。状态图标靠右贴箭头，不要放最左。
- 展开详情按工具定制：edit → `lib/diff.ts` 的 LCS 行级 diff（+绿/-红 + 增删计数）；write → 内容代码块；bash → 命令代码块；read/grep → 路径/参数；MCP 等未知工具 → 键值列表（不吐 JSON）。
- 禁止在 UI 里直接 `JSON.stringify(args)` 给用户看。


## 内置浏览器（Electron WebContentsView，仅桌面端）
- **链路**：内核 `browser` 工具（`internal/webui/browser.go`）→ op 入队阻塞等结果 → 前端 `BrowserPane` 长轮询 `GET /api/browser/next` 领取 → preload `window.omniDesktop.browserOp` IPC → Electron 主进程在 `WebContentsView` 执行 → 前端 `POST /api/browser/result` 回填。通道设计对称于 approval/question。
- **在线检测**：长轮询挂起数 `pollers > 0 || time.Since(lastSeen) < 5s` 判定"桌面壳在线"；非 Electron 时 browser 工具**快速报错**，不等待（否则会挂 3 分钟）。
- **视图定位**：原生 `WebContentsView` 悬浮在主窗口之上，前端占位 div 用 `ResizeObserver` + `getBoundingClientRect` 实时上报矩形（`omni:browser-bounds`），显隐用 `omni:browser-visible`；**收起时占位 div 不存在 → 必须上报 0×0**。
- **op 语义**：`open/navigate/reload/back/forward/click(x,y,doubleClick)/type(text)/key/scroll(x,y,amount)/wait(ms)/extract/eval(code)/state/show/hide/close`；`extract` 返回可见交互元素（视口中心坐标）+ 标题 + 正文，模型靠它"看"页面 → **必须先 extract 拿坐标再 click**。
- **入口 UI**：输入框底部「完全访问」(ModePicker) 之后的 `GlobeIcon` 幽灵按钮（用户明确要求放这里，**不要**放右侧竖排按钮）。`BrowserPane` 是受控组件（`open`/`onOpenChange`），收起时 `return null`。
- AI 开始操作时自动展开面板（`close`/`hide` 除外），让用户看得见它在干什么。
- `.omni/skills/browser-use/SKILL.md` 已改为内置 browser 工具操作手册（原文是 browser-use.com 上游的外部 CLI 手册，本机无此 CLI，会让 AI 反复安装/等授权 → 界面卡"处理中"）。

## Windows 桌面端易踩的坑
- **任务栏图标/标题 ≠ 窗口属性**：`win.setIcon()` / `setTitle()` / 拦 `page-title-updated` 只管窗口；
  任务栏按钮（含右键菜单顶部）的名字与图标走 **AUMID 解析**——按进程 AUMID 去开始菜单找同名快捷方式。
  开发态进程是 electron.exe（文件描述="Electron"）所以永远显示 Electron；
  修法 = `app.setAppUserModelId()` + 开始菜单写带同 AUMID/图标的 `omni.lnk`（仅 dev，打包态由 electron-builder 负责）。
- **`shell.writeShortcutLink(path, "replace", opts)` 文件不存在时返回 false**（与文档相反）。
  必须 `create(opts) || replace(opts)`。`update` 对不存在的文件同样失败。
- **改桌面端只需重启 Electron，不用重编内核**：main.js/preload.js 是直接被 Electron 加载的源码，不进 bundle。
- **验证 UI/Electron 行为的两个限制**：`contextBridge` 暴露的 `window.omniDesktop` 在渲染层冻结，
  不能 monkey-patch 记录调用；preload（isolated world）的 `console.log` 不进主进程 stdout。
  记录桩调用必须 `ipcRenderer.send` 回主进程。
- **capturePage 是物理像素，getBoundingClientRect 是 CSS 像素**：差一个 DPR。
  验证 UI 时加 `app.commandLine.appendSwitch("force-device-scale-factor", "1")` 让两者 1:1，别自己换算。
