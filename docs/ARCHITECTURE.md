# OmniAgnes 代码结构地图（ARCHITECTURE.md）

> 给 AI / 开发者的一页"代码地图"：先读本文件 + AGENTS.md 建立全局上下文，
> 再按需下钻到具体文件，**避免每次从头扫全仓**。结构与触点以代码为准，改动后同步。
> 配套：项目长期记忆在 `.workbuddy/memory/MEMORY.md`（稳定约定）+ `YYYY-MM-DD.md`（每日进展）。

## 一、总体形态

```
OmniAgnes（单体仓库，单二进制交付）
├─ main.go                程序入口：flag 解析、启动 webui/TUI，含版本号读取
├─ cmd/                   （少量命令/测试，如 shot_test）
├─ internal/              Go 内核（全部业务）
│  ├─ agent/              会话编排（工具调用循环、检查点、并发护栏）
│  ├─ llm/                多 provider 封装（OpenAI/兼容接口、重试 500/429/502/503/504）
│  ├─ mcp/                MCP 客户端集成
│  ├─ tools/              工具执行（bash.go 有 Windows 子进程输出 GB18030→UTF-8 转码）
│  ├─ skills/             技能系统
│  ├─ memory/             记忆模块（区别于 .workbuddy/memory 的 AI 记忆，这是运行时记忆）
│  ├─ store/              持久化（会话等落盘，~/.local/share/omni/…）
│  ├─ config/             配置（config.example.json）
│  ├─ webui/              HTTP/SSE 后台 + 前端静态资源
│  │  ├─ webui.go         /go:embed all:dist 内嵌前端，起 HTTP 服
│  │  ├─ api.go / events.go   API 与 SSE 事件流
│  │  ├─ schedule.go / usage.go  定时任务 / 使用量
│  │  ├─ mobile.go        移动端远程访问（8792 端口、设备 token、See"移动端"）
│  │  └─ browser.go       内置浏览器桥（WebContentsView 的前端长轮询对接）
│  ├─ extras/             杂项扩展
│  ├─ docparse/           文档解析（docx/pptx/xlsx/pdf）
│  └─ tui/                bubbletea 终端程序（无 HTTP，手机用不上）
├─ webui/
│  ├─ frontend/           React + Vite 前端（产物 → internal/webui/dist 被内核内嵌）
│  └─ desktop/            Electron 壳（main.js/preload.js/updater.js）+ bin/ 内核副本
├─ server_cloudflare/     账号/积分后台（Hono + TS，worker 名 omni-server，D1 omni_server）
├─ dev/                   自研 MCP 服务器源码（agnes/mimo/docx/pptx/xlsx，构建 exe 不入库）
└─ .workbuddy/memory/     AI 长期记忆（MEMORY.md + 每日日志）—— 非运行时数据
```

## 二、关键流程

### 1. 桌面端访问题目栏"更新"链路
```
updater.js(状态全在主进程) --emit--> 主进程 win.webContents.send("omni:update")
   → frontend Titlebar.tsx（左上角 hasUpdate 入口）
   → SettingsDialog.tsx「关于」页签（内联横幅 + 立即升级）
   → UpgradeDialog.tsx（下载/进度/取消/安装，三者复用同一套 updater API）
```
更新域名：无默认（留空直连 GitHub Releases）；填了优先 `{域名}/api/upgrade`（服务端代拉，国内可达）。

### 2. 模型选择器两级联动
```
frontend/src/components/ModelPicker.tsx
  一级 = 供应商列表（renderRow）：当前选中组 → 行尾对号（no 箭头）；未选中 → 箭头作展开提示
  二级 = Flyout：选中模型行尾对号（text-primary）+ 行高亮
  内置模型组签 key 前缀 "server:"，与本地同名模型区分
```
`selectedOne` 推导：`model.startsWith("server:")` → 内置组；否则 → 该模型所在的 provider。

### 3. 前端产物 → 内核 → 桌面端（改前端必走完）
```
cd frontend; npm run build     # 1. 产物写 internal/webui/dist
cd ../..; go build -o omni.exe .  # 2. 重编内核（/go:embed all:dist 内嵌新前端）
cp omni.exe webui/desktop/bin/ # 3. 桌面端跑的是 bin/ 里的旧内核，必须覆盖
# 4. 杀 electron/omni 后重启桌面端
```

### 4. 移动端远程访问
- 门禁 = 设备 token（落盘 `~/.local/share/omni/mobile.json`），扫码 `http://内网IP:8792/device/<token>`。
- 走 Cookie 而非 Authorization（SSE EventSource 不能带自定义头）。
- token 不过期、可 `POST /api/mobile/rotate` 换发（旧 token 立即失效）。
- 端口 8792 与桌面端 8791 分开（避免 pollute localStorage origin）。

## 三、前端组件触点索引（业务/设置/ui）

| 关注点 | 文件 | 备注 |
|---|---|---|
| 主页骨架/状态提升 | `frontend/src/App.tsx` | 主题、更新快照上浮、SSE、会话 |
| 对话 | `components/Chat.tsx` / `TaskList.tsx` | SSE 流式渲染、任务快照重建 |
| 侧栏 | `components/Sidebar.tsx` / `AddProjectDialog.tsx` | 项目/会话树 |
| 标题栏 | `components/Titlebar.tsx` | 应用名硬编码"OmniAgens"，左上角"更新"入口 |
| 模型选择器 | `components/ModelPicker.tsx` | 见流程 2 |
| 设置弹窗 | `components/SettingsDialog.tsx` | 页签：account/model/theme/memory/plugins/usage/**version(=关于)** |
| 升级弹窗 | `components/UpgradeDialog.tsx` | 与标题栏共用更新逻辑 |
| 使用量/积分 | `components/UsagePanel.tsx` | |
| 移动面板 | `components/MobilePanel.tsx` | 见流程 4 |
| 内置浏览器面板 | `components/BrowserPane.tsx` | |
| 登录 | `components/ServerLoginDialog.tsx` | 网页授权码 omni:// 协议回调 |
| 工具调用视图 | `components/ToolCallGroup.tsx` / `ToolCallView.tsx` | |
| 定时任务 | `components/SchedulesPage.tsx` | |
| 通用 UI | `components/ui/` | dialog/button/input/label/select/switch? 等 |
| API 客户端 | `lib/api.ts` / `lib/serverAuth.ts` | HTTP + SSE、后台认证 |
| 浏览器树桥 | `lib/browser.ts` / `lib/platform.ts` | 桌面壳与网页差异判断 |
| 国际化 | `lib/i18n.tsx` | 简/繁/英三语字典 |

## 四、桌面壳（Electron）触点

| 文件 | 职责 |
|---|---|
| `webui/desktop/main.js` | 窗口、IPC（win/zoom/autostart❌/auth/browser/update）、拉起内核、无边框 + 软件渲染（Windows 黑屏修复） |
| `webui/desktop/preload.js` | contextBridge 暴露 `window.omniDesktop`（win/pickFolder/setZoom/browserOp/update/auth 等） |
| `webui/desktop/updater.js` | 检查/下载/安装/取消更新的全部状态机（状态只存主进程） |
| `webui/desktop/electron-builder.yml` | 打包配置；`bin/` 经 extraResources 打进 resources/bin |

> ⚠️ 自启动（autostart）已按用户要求砍掉（2026-10-15），main/preload/类型/前端均无。
> ⚠️ 桌面端必须用 `electron/dist/electron.exe` 直接启动，且 `unset ELECTRON_RUN_AS_NODE`（见 MEMORY.md）。

## 五、启动桌面端（兑现流程）
```bash
unset http_proxy https_proxy HTTP_PROXY HTTPS_PROXY ELECTRON_RUN_AS_NODE
OMNI_BIN="C:/newcode/TEST/demo/OmniAgnes/webui/desktop/bin/omni.exe" \
  ./node_modules/electron/dist/electron.exe .
```

## 六、版本号来源
- 版本唯一来源 = `webui/desktop/package.json`（`main.go` 用 `//go:embed webui/desktop/package.json` 读取；electron-builder 按它命名安装包）。
- 构建可 `-ldflags "-X main.version=..."` 覆盖。