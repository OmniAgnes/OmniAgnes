# omni_agent 项目长期笔记

## 项目概况
- Go 1.27 单体：`main.go`（TUI/Web 双入口，`--web` 进网页版工作台）+ internal/{agent,llm,store,tools,webui,tui,...}。
- 前端：`webui/frontend`（React 19 + Vite + Tailwind v4 + lucide-react，组件用 shadcn 风格 ui/ 目录），构建产物输出到 `internal/webui/dist`，经 go:embed 打进二进制。
- 桌面端：`webui/desktop`（Electron 壳，无边框自绘标题栏，拉起 `omni --web --no-open`），打包 `npm run dist`（NSIS），图标在 `build/icon.ico`，生成脚本 `make_icon.py`（Pillow+numpy，装在 `C:/Users/sun/.workbuddy/binaries/python/envs/default`）。
- 数据持久化：任务 = `~/.config/omni`（DefaultDataDir）下 sessions/*.json，项目 = projects.json。

## 构建链（改动必走）
1. 前端：`cd webui/frontend && npm run build`（tsc --noEmit + vite build，沙箱内命令前加 `NODE_OPTIONS=` 防 shim 干扰）
2. 内核：`go build -o omni.exe .`（改 main.go 后必跑，否则桌面端跑旧内核）
3. 同步：`cp omni.exe webui/desktop/bin/omni.exe`
4. 安装包：`cd webui/desktop && npm run dist`
- 沙箱启动桌面：`cd webui/desktop && env -u ELECTRON_RUN_AS_NODE -u NODE_OPTIONS npx electron . --disable-gpu --disable-gpu-sandbox`（ELECTRON_RUN_AS_NODE=1 会让 electron 退化成纯 Node；GPU 进程在沙箱内崩溃需禁 GPU）
- 发版：打 tag v* → `.github/workflows/release.yml` 双架构出 Win NSIS + mac dmg；CI 交叉编译内核用**备料名**（omni-<平台>-<架构>[.exe]）放 bin/，beforePack 钩子无条件覆盖正式名（顺序无关、幂等）

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

