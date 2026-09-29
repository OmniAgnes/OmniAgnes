import { MinusIcon, SquareIcon, XIcon } from "lucide-react"

// 自绘标题栏（仅桌面端渲染）：跟随主题。
// 不画底边框，整体分左右两块，背景色分别与下方的侧边栏 / 对话区对齐。
export function Titlebar() {
  const drag = { WebkitAppRegion: "drag" } as React.CSSProperties
  const noDrag = { WebkitAppRegion: "no-drag" } as React.CSSProperties

  return (
    <div className="flex h-9 shrink-0 select-none" style={drag}>
      {/* 左块：对应下方侧边栏（宽度与 border-r 与侧边栏一致） */}
      <div className="bg-sidebar hidden w-72 shrink-0 items-center border-r pl-3 md:flex">
        <span className="text-base font-semibold tracking-tight">OmniAgens</span>
      </div>
      {/* 右块：对应下方对话区 */}
      <div className="bg-background flex min-w-0 flex-1 items-center justify-end">
        <div className="flex h-full" style={noDrag}>
          <TitlebarButton title="最小化" onClick={() => window.omniDesktop?.win("minimize")}>
            <MinusIcon className="size-4" />
          </TitlebarButton>
          <TitlebarButton title="最大化 / 还原" onClick={() => window.omniDesktop?.win("maximize")}>
            <SquareIcon className="size-3" />
          </TitlebarButton>
          <TitlebarButton title="关闭" danger onClick={() => window.omniDesktop?.win("close")}>
            <XIcon className="size-4" />
          </TitlebarButton>
        </div>
      </div>
    </div>
  )
}

function TitlebarButton({
  title,
  danger,
  onClick,
  children,
}: {
  title: string
  danger?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      title={title}
      onClick={onClick}
      className={
        danger
          ? "hover:bg-destructive hover:text-destructive-foreground flex h-full w-11 items-center justify-center cursor-pointer transition-colors"
          : "hover:bg-accent flex h-full w-11 items-center justify-center cursor-pointer transition-colors"
      }
    >
      {children}
    </button>
  )
}
