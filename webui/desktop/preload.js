// 预加载脚本：以最小面暴露桌面能力给渲染层（网页端不存在 window.omniDesktop）。
const { contextBridge, ipcRenderer } = require("electron")

contextBridge.exposeInMainWorld("omniDesktop", {
  platform: process.platform,
  // 原生文件夹选择；取消返回 null
  pickFolder: () => ipcRenderer.invoke("omni:pick-folder"),
  // 窗口控制：minimize | maximize | close
  win: (action) => ipcRenderer.send("omni:win", action),
})
