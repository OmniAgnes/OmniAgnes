// 来源站点收集：从工具调用参数里提取外部 URL，供回复末尾「来源：」行展示。
// 只认 http(s)，排除本机/内网地址；去重、上限 8 个，保持出现顺序。

const MAX_SOURCES = 8

// 首个 URL 的宽松匹配；结尾再手工去掉黏连的中西文标点
const URL_RE = /https?:\/\/[^\s"'<>()\[\]{}]+/i
const TRAILING_PUNCT = /[),.;:!?'"，。；：！？、）】》]+$/

/** 从任意 JSON 值（对象/数组/字符串）里递归提取外部 URL，追加进 out 并返回。 */
export function extractExternalURLs(value: unknown, out: string[] = [], seen = new Set<string>()): string[] {
  if (out.length >= MAX_SOURCES) return out
  if (typeof value === "string") {
    const m = value.match(URL_RE)
    if (m) {
      const url = m[0].replace(TRAILING_PUNCT, "")
      if (isExternal(url) && !seen.has(url)) {
        seen.add(url)
        out.push(url)
      }
    }
    return out
  }
  if (Array.isArray(value)) {
    for (const v of value) {
      extractExternalURLs(v, out, seen)
      if (out.length >= MAX_SOURCES) return out
    }
    return out
  }
  if (value && typeof value === "object") {
    for (const v of Object.values(value)) {
      extractExternalURLs(v, out, seen)
      if (out.length >= MAX_SOURCES) return out
    }
  }
  return out
}

function isExternal(raw: string): boolean {
  try {
    const u = new URL(raw)
    if (u.protocol !== "http:" && u.protocol !== "https:") return false
    const h = u.hostname.toLowerCase()
    if (!h || h === "localhost" || h === "::1" || h.endsWith(".local")) return false
    if (/^(127\.|10\.|192\.168\.|169\.254\.)/.test(h)) return false
    if (/^172\.(1[6-9]|2\d|3[01])\./.test(h)) return false
    return true
  } catch {
    return false
  }
}

/** 来源展示名：主机名去掉 www. 前缀（如 https://www.gov.cn/x → gov.cn）。 */
export function sourceName(url: string): string {
  try {
    const h = new URL(url).hostname.toLowerCase()
    return h.startsWith("www.") ? h.slice(4) : h
  } catch {
    return url
  }
}
