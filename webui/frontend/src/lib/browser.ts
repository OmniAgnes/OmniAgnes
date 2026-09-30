// 内置浏览器的统一入口：谁想打开网页（Markdown 链接、后续的搜索结果等）都调 openInBrowser。
//
// 桌面端：派发 window 事件给已挂载的 BrowserPane，由它展开面板 + 走长轮询执行导航。
//         不直接调 IPC —— 面板可能还没打开，主进程那侧 view 未显示、尺寸为 0，
//         直接 navigate 用户看不到；交给 BrowserPane 能保证「先展开、再导航」。
// 网页端：没有 Electron 壳，直接新标签页打开，绝不顶掉工作台本身。

export const BROWSER_OPEN_EVENT = "omni:browser-open"

export function openInBrowser(url: string) {
  if (typeof window === "undefined") return
  if (window.omniDesktop?.browserOp) {
    window.dispatchEvent(new CustomEvent(BROWSER_OPEN_EVENT, { detail: { url } }))
    return
  }
  window.open(url, "_blank", "noopener,noreferrer")
}
