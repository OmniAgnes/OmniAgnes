// 内置浏览器的统一入口：谁想打开网页（Markdown 链接、后续的搜索结果等）都调 openInBrowser。
//
// 桌面端：派发 window 事件给已挂载的 BrowserPane，由它展开面板 + 走长轮询执行导航。
//         不直接调 IPC —— 面板可能还没打开，主进程那侧 view 未显示、尺寸为 0，
//         直接 navigate 用户看不到；交给 BrowserPane 能保证「先展开、再导航」。
// 网页端：没有 Electron 壳，直接新标签页打开，绝不顶掉工作台本身。

export const BROWSER_OPEN_EVENT = "omni:browser-open"

/** 目标是否就是本应用自身（同源）——工作台/内核接口都不是「网页」 */
function isSelfURL(url: string): boolean {
  if (typeof window === "undefined") return false
  try {
    return new URL(url, window.location.href).origin === window.location.origin
  } catch {
    return false
  }
}

export function openInBrowser(url: string) {
  if (typeof window === "undefined" || !url) return
  // 绝不在内置浏览器里打开本应用自己：那等于把工作台嵌进工作台（嵌套一份 UI，
  // 又占满右栏、还可能递归触发导航）。同源地址一律拦掉——内核的 /api/* 也一样，
  // 它们不是给人浏览的页面。
  if (isSelfURL(url)) return
  if (window.omniDesktop?.browserOp) {
    window.dispatchEvent(new CustomEvent(BROWSER_OPEN_EVENT, { detail: { url } }))
    return
  }
  window.open(url, "_blank", "noopener,noreferrer")
}
