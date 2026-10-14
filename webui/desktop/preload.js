// 预加载脚本：以最小面暴露桌面能力给渲染层（网页端不存在 window.omniDesktop）。
const { contextBridge, ipcRenderer } = require("electron")

contextBridge.exposeInMainWorld("omniDesktop", {
  platform: process.platform,
  // 原生文件夹选择；取消返回 null
  pickFolder: () => ipcRenderer.invoke("omni:pick-folder"),
  // 窗口控制：minimize | maximize | close
  win: (action) => ipcRenderer.send("omni:win", action),
  // 界面缩放（Chromium 原生页面缩放，0.5~2）
  setZoom: (z) => ipcRenderer.send("omni:zoom", z),
  // 内置浏览器：执行一次操作（返回 {ok, output, error}）
  browserOp: (op, args) => ipcRenderer.invoke("omni:browser-op", { op, args }),
  // 上报浏览器占位区矩形（视口 CSS 像素）
  browserBounds: (b) => ipcRenderer.send("omni:browser-bounds", b),
  // 显示/隐藏原生视图（面板折叠时置 false，避免操作打到看不见的区域）
  browserVisible: (v) => ipcRenderer.send("omni:browser-visible", Boolean(v)),
  // 应用更新：状态快照全在主进程，界面读快照 + 订阅变化
  update: {
    get: () => ipcRenderer.invoke("omni:update:get"),
    check: () => ipcRenderer.invoke("omni:update:check"),
    download: () => ipcRenderer.invoke("omni:update:download"),
    cancel: () => ipcRenderer.invoke("omni:update:cancel"),
    install: () => ipcRenderer.invoke("omni:update:install"),
    openRelease: (tag) => ipcRenderer.invoke("omni:update:openRelease", tag),
    getDomain: () => ipcRenderer.invoke("omni:update:getDomain"),
    setDomain: (d) => ipcRenderer.invoke("omni:update:setDomain", d),
    onChanged: (cb) => {
      const handler = (_e, s) => cb(s)
      ipcRenderer.on("omni:update", handler)
      return () => ipcRenderer.removeListener("omni:update", handler)
    },
  },
})
