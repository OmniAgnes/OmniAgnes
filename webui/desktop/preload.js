// 预加载脚本：以最小面暴露桌面能力给渲染层（网页端不存在 window.omniDesktop）。
const { contextBridge, ipcRenderer } = require("electron")

contextBridge.exposeInMainWorld("omniDesktop", {
  platform: process.platform,
  // 原生文件夹选择；取消返回 null
  pickFolder: () => ipcRenderer.invoke("omni:pick-folder"),
  // 窗口控制：minimize | maximize | close
  win: (action) => ipcRenderer.send("omni:win", action),
  // 内置浏览器：执行一次操作（返回 {ok, output, error}）
  browserOp: (op, args) => ipcRenderer.invoke("omni:browser-op", { op, args }),
  // 上报浏览器占位区矩形（视口 CSS 像素）
  browserBounds: (b) => ipcRenderer.send("omni:browser-bounds", b),
  // 显示/隐藏原生视图（面板折叠时置 false，避免操作打到看不见的区域）
  browserVisible: (v) => ipcRenderer.send("omni:browser-visible", Boolean(v)),
})
