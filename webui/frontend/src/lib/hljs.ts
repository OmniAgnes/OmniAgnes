// 直接挂载 highlight.js 官方 GitHub 主题（插件自带 github.css / github-dark.css），
// 不再手写配色。本应用主题用 .dark class 切换而非 prefers-color-scheme，
// 因此同一 <style> 里按当前主题装载其一。
import light from "highlight.js/styles/github.css?inline"
import dark from "highlight.js/styles/github-dark.css?inline"

let el: HTMLStyleElement | null = null

export function setHljsTheme(isDark: boolean) {
  if (!el) {
    el = document.createElement("style")
    el.id = "hljs-theme"
    document.head.appendChild(el)
  }
  el.textContent = isDark ? dark : light
}
