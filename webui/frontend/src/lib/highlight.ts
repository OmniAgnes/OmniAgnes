import hljs from "highlight.js/lib/common"

/** 文件扩展名 → highlight.js 语言名（找不到返回 ""，交由 highlightAuto 猜测） */
const EXT_LANG: Record<string, string> = {
  go: "go",
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  css: "css",
  scss: "scss",
  html: "xml",
  htm: "xml",
  json: "json",
  py: "python",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  swift: "swift",
  c: "c",
  h: "c",
  cpp: "cpp",
  hpp: "cpp",
  cs: "csharp",
  php: "php",
  rb: "ruby",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  yml: "yaml",
  yaml: "yaml",
  toml: "ini",
  md: "markdown",
  sql: "sql",
  xml: "xml",
  vue: "xml",
  dockerfile: "docker",
  tf: "hcl",
  ini: "ini",
  conf: "ini",
  proto: "protobuf",
  graphql: "graphql",
}

export function langFromPath(path: string): string {
  const base = path.split(/[\\/]/).pop() || ""
  if (/^dockerfile/i.test(base)) return "docker"
  const ext = base.includes(".") ? base.split(".").pop()!.toLowerCase() : ""
  return EXT_LANG[ext] ?? ""
}

/** 用 highlight.js 生成 GitHub 风格的着色 HTML；输入由 hljs 内部转义，可安全注入。 */
export function highlightToHtml(code: string, lang: string): string {
  if (!code) return ""
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
    }
    return hljs.highlightAuto(code).value
  } catch {
    return escapeHtml(code)
  }
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
}
