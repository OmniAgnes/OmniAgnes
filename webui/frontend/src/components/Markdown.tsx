import ReactMarkdown from "react-markdown"
import rehypeHighlight from "rehype-highlight"
import remarkGfm from "remark-gfm"

import { openInBrowser } from "@/lib/browser"

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
        components={{
          a: ({ href, children, ...rest }) => (
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
          ),
          // 行内代码遇到长串（如 URL、路径、字段名）要能断行，
          // 否则长单词会把整行撑出边界、把右侧留白顶没。
          code: ({ className, children, ...rest }) => (
            <code className={className} {...rest}>
              {children}
            </code>
          ),
        }}
      >
        {text}
      </ReactMarkdown>
    </div>
  )
}
