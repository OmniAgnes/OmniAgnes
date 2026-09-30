import { FolderIcon, MessageSquareIcon, MinusIcon, SquareIcon, XIcon } from "lucide-react"

import { cn } from "@/lib/utils"

// 自绘标题栏（仅桌面端渲染）：跟随主题。
// 平时不画底边框，整体分左右两块，背景色分别与下方的侧边栏 / 对话区对齐。
// 右块左侧显示当前「来源（文件夹/任务）/ 标题」——对话区顶部的这行在桌面端挪到这里。
//
// browserOpen：内置浏览器展开时给右块补一条底边框。浏览器面板左侧有 border-l，
// 若顶头没有横向分隔，那条竖线会悬空断在标题栏下沿，很不协调；补上后两者交成 T 形接点。
export function Titlebar({ source, title, browserOpen }: { source: string; title: string; browserOpen?: boolean }) {
  const drag = { WebkitAppRegion: "drag" } as React.CSSProperties
  const noDrag = { WebkitAppRegion: "no-drag" } as React.CSSProperties

  return (
    <div className="flex h-9 shrink-0 select-none" style={drag}>
      {/* 左块：对应下方侧边栏（宽度与 border-r 与侧边栏一致） */}
      <div className="bg-sidebar hidden w-72 shrink-0 items-center border-r pl-3 md:flex">
        <span className="text-base font-semibold tracking-tight">OmniAgens</span>
      </div>
      {/* 右块：对应下方对话区。左边是当前任务来源/标题（字号加大，过长自动截断，悬停看全称），右边是窗口按钮 */}
      <div className={cn("bg-background flex min-w-0 flex-1 items-center", browserOpen && "border-b")}>
        <div className="text-muted-foreground flex min-w-0 flex-1 items-center gap-2 px-3 pt-1.5 text-sm">
          <span className="flex shrink-0 items-center gap-1.5" title={source}>
            {source !== "任务" ? <FolderIcon className="size-4" /> : <MessageSquareIcon className="size-4" />}
            <span className="max-w-48 truncate">{source}</span>
          </span>
          <span className="text-muted-foreground/60 shrink-0">/</span>
          <span className="text-foreground min-w-0 truncate text-[15px] font-medium" title={title}>
            {title}
          </span>
        </div>
        <div className="flex h-full shrink-0" style={noDrag}>
          <TitlebarButton title="最小化" onClick={() => window.omniDesktop?.win("minimize")}>
            <MinusIcon className="size-4" />
          </TitlebarButton>
          <TitlebarButton title="最大化 / 还原" onClick={() => window.omniDesktop?.win("maximize")}>
            <SquareIcon className="size-3" />
          </TitlebarButton>
          <TitlebarButton title="关闭" onClick={() => window.omniDesktop?.win("close")}>
            <XIcon className="size-4" />
          </TitlebarButton>
        </div>
      </div>
    </div>
  )
}

function TitlebarButton({
  title,
  onClick,
  children,
}: {
  title: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className="hover:bg-accent flex h-full w-11 items-center justify-center cursor-pointer transition-colors"
    >
      {children}
    </button>
  )
}
