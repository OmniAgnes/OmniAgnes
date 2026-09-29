# omni 桌面版

Electron 壳：拉起 `omni --web --no-open` 服务并用原生窗口加载网页版工作台。

## 使用

1. 编译 omni 并放入 `bin/`（或设置 `OMNI_BIN` 指向二进制）：

   ```sh
   # 仓库根目录
   go build -o webui/desktop/bin/omni.exe .
   ```

2. 安装依赖并启动：

   ```sh
   cd webui/desktop
   npm install
   npm start
   ```

## 环境变量

| 变量 | 说明 |
|------|------|
| `OMNI_BIN` | omni 二进制完整路径（优先于 `bin/`） |
| `OMNI_CWD` | omni 工作目录（默认用户主目录） |
