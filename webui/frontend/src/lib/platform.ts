// 运行形态判断：Electron 桌面壳注入 window.omniDesktop，普通浏览器（H5 / 手机扫码）没有。
// H5 侧据此隐藏只在桌面端成立的功能：自绘标题栏、内置浏览器、移动端二维码入口。
export const isDesktop = typeof window !== "undefined" && Boolean(window.omniDesktop)
