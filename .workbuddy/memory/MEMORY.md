# omni_agent 项目长期笔记

## 项目概况
- Go 1.27 单体：`main.go`（TUI/Web 双入口，`--web` 进网页版工作台）+ internal/{agent,llm,store,tools,webui,tui,...}。
- 前端：`webui/frontend`（React 19 + Vite + Tailwind v4 + lucide-react，组件用 shadcn 风格 ui/ 目录），构建产物输出到 `internal/webui/dist`，经 go:embed 打进二进制。
- 桌面端：`webui/desktop`（Electron 壳，无边框自绘标题栏，拉起 `omni --web --no-open`），打包 `npm run dist`（NSIS），图标在 `build/icon.ico`，生成脚本 `make_icon.py`（Pillow+numpy，装在 `C:/Users/sun/.workbuddy/binaries/python/envs/default`）。
- 数据持久化：任务 = `~/.config/omni`（DefaultDataDir）下 sessions/*.json，项目 = projects.json。

## 构建链（改动必走）
1. 前端：`cd webui/frontend && npm run build`（tsc --noEmit + vite build，沙箱内命令前加 `NODE_OPTIONS=` 防 shim 干扰）
2. 内核：`go build -o omni.exe .`
3. 同步：`cp omni.exe webui/desktop/bin/omni.exe`
4. 安装包：`cd webui/desktop && npm run dist`

## 已有约定
- 会话历史：webui `agentFor` 创建 Agent 时必须传 `History: t.Messages` 恢复持久化消息。
- 文件夹操作：右键菜单（打开/移除），移除走 AlertDialog 确认；行内 hover 只显示 PlusIcon 添加按钮；不显示任务数徽标。
- 对话头部只显示模型名，不显示目录路径。
- 打开文件夹 API：`POST /api/projects/{id}/open`（explorer 在 Windows 成功也返回非 0，用 Start 不等待）。
