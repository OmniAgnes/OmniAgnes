import ReactMarkdown from "react-markdown"
import rehypeHighlight from "rehype-highlight"
import remarkGfm from "remark-gfm"

// Markdown 渲染：GFM 表格/任务列表 + 代码高亮（样式见 index.css 的 .md-output）
export function Markdown({ text }: { text: string }) {
  return (
    <div className="md-output text-sm leading-relaxed">
      <ReactMarkdown remarkPlugins={[remarkGfm]} rehypePlugins={[[rehypeHighlight, { detect: false, ignoreMissing: true }]]}>
        {text}
      </ReactMarkdown>
    </div>
  )
}
