import { useState, type ReactNode } from "react"
import ReactMarkdown, { defaultUrlTransform } from "react-markdown"
import rehypeHighlight from "rehype-highlight"
import remarkGfm from "remark-gfm"

import { openInBrowser } from "@/lib/browser"

import { InlineMedia, mediaKind } from "./ui/inline-media"
import { ZoomImage } from "./ui/zoom-image"

// Markdown 渲染：GFM 表格/任务列表 + 代码高亮（样式见 index.css 的 .md-output）
//
// 链接一律不让主窗口导航：点 http(s) 链接 → 交给内置浏览器（右侧面板）打开，
// 而不是把整个工作台替换成网页。网页端没有内置浏览器，退回新标签页。
export function Markdown({ text }: { text: string }) {
  return (
    // text-[0.9375rem]：比 text-sm(14px) 大一号。助手长回复（需求文档、方案）
    // 用 14px 偏小，连续读几屏眼睛很快就累。
    <div className="md-output text-[0.9375rem] leading-relaxed">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}
        // ⚠️ 必须放行 file:// 与本地绝对路径：react-markdown 默认的 URL 消毒只允许
        // http(s)/irc/mailto/xmpp，会把 `file:///C:/x.png` 这种 **清成空字符串**，
        // 于是图片根本加载不出来（实测踩到）。这里先放行，再统一由 toDisplaySrc
        // 转成内核的附件接口地址——消毒的活由我们自己接管。
        urlTransform={(url) =>
          /^(file:|[a-zA-Z]:[\\/])/i.test(url.trim()) ? url : defaultUrlTransform(url)
        }
        components={{
          a: ({ href, children, ...rest }) => {
            const url = toDisplaySrc(href ?? "")
            // 指向本地/远程图片、视频、音频的链接直接内联渲染，省得用户再去点开
            const kind = mediaKind(url)
            if (kind) return <InlineMedia url={url} kind={kind} label={flatten(children)} />
            return (
              <a
                {...rest}
                href={href}
                onClick={(e) => {
                  if (!/^https?:\/\//i.test(href ?? "")) return
                  e.preventDefault()
                  openInBrowser(href as string)
                }}
              >
                {children}
              </a>
            )
          },
          // 行内代码遇到长串（如 URL、路径、字段名）要能断行，
          // 否则长单词会把整行撑出边界、把右侧留白顶没。
          code: ({ className, children, ...rest }) => (
            <code className={className} {...rest}>
              {children}
            </code>
          ),
          img: ({ src, alt }) => (
            <MdImage src={typeof src === "string" ? src : ""} alt={alt ?? ""} />
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}

/** 把 children 拍平成一行文本（媒体链接的说明文字） */
function flatten(children: ReactNode): string {
  if (children == null || typeof children === "boolean") return ""
  if (typeof children === "string" || typeof children === "number") return String(children)
  if (Array.isArray(children)) return children.map(flatten).join("")
  if (typeof children === "object" && "props" in (children as never)) {
    return flatten((children as { props?: { children?: ReactNode } }).props?.children)
  }
  return ""
}

/**
 * 本地绝对路径（含 `file://` URL）转成 `/api/attachment` 接口地址。
 *
 * 模型经常直接吐 `![x](file:///C:/Users/…/a.png)` 或 `![x](C:\…\a.png)`，
 * 而页面跑在 `http://127.0.0.1:8791` 上——Chromium 禁止 http 页面加载 `file://`
 * 子资源（"Not allowed to load local resource"），照原样渲染必然是破图。
 * 走内核的附件接口就能正常显示（后端只放行数据目录/工作目录/Temp 下的路径）。
 */
export function toDisplaySrc(src: string): string {
  const s = (src ?? "").trim()
  if (!s) return s
  // 网络地址、data/blob、已经是接口地址的，原样返回
  if (/^(https?:|data:|blob:|\/\/|\/api\/)/i.test(s)) return s

  let p = s
  if (/^file:\/\//i.test(p)) {
    p = p.replace(/^file:\/\/(localhost)?/i, "")
    try {
      p = decodeURIComponent(p)
    } catch {
      /* 非法转义：原样用 */
    }
    // file:///C:/x → /C:/x：去掉盘符前多出的那个斜杠
    if (/^\/[a-zA-Z]:[\\/]/.test(p)) p = p.slice(1)
  } else if (!/^[a-zA-Z]:[\\/]/.test(p) && !p.startsWith("/")) {
    // 相对路径等其它形式交给浏览器自己解析
    return s
  }
  return `/api/attachment?path=${encodeURIComponent(p)}`
}

/** Markdown 里的图片：加载不出来就只显示路径文本（可选中复制），不做任何跳转——
 * 之前这里点了会打开内置浏览器，若 URL 为空还会打开工作台自身的 `#`，纯属添乱。 */
function MdImage({ src, alt }: { src: string; alt: string }) {
  const [failed, setFailed] = useState(false)
  const url = toDisplaySrc(src)
  if (!url || failed) {
    return (
      <span className="text-muted-foreground font-mono text-xs break-all select-all" title={src}>
        {alt || src}
      </span>
    )
  }
  return <ZoomImage src={url} alt={alt} onError={() => setFailed(true)} />
}
