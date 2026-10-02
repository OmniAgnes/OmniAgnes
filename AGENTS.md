# omni Agent

Go 内核 + React 前端 + Electron 桌面端 + 独立 Go 后台的单体仓库。
本文件只记「可复用的结论与触点清单」，以代码为准，改动后同步。

## 目录

- `internal/` 后端模块：`agent`(会话编排) `llm`(多 provider) `mcp` `tools` `skills`
  `memory` `store`(持久化) `config` `webui`(HTTP/SSE) `docparse` `extras` `tui`
- `webui/frontend/` React + Vite 前端（产物由 Go 内核 `internal/webui` 内嵌分发）
- `webui/desktop/` Electron 壳；`bin/` 放内核副本与内置工具 exe（不入库）
- `server/` 独立账号/积分后台（Go + SQLite），前端产物在 `server/dist`
- `dev/` 自研 MCP 服务器源码（agnes/mimo/docx/pptx/xlsx），构建 exe 不入库

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

## 常用命令

```powershell
go build ./...                          # 编译检查
go vet ./...
cd webui\frontend; npm run typecheck    # 前端类型检查（不产产物）
cd webui\desktop; npm run check:bin     # 打包前校验内核副本存在
```

## 详细文档

历史排查结论见 git log 与本目录 `docs/`（如存在）。
