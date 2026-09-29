import { MinusIcon, SquareIcon, XIcon } from "lucide-react"

// 自绘标题栏（仅桌面端渲染）：跟随主题，左侧应用名，右侧窗口控制按钮。
export function Titlebar() {
  const drag = { WebkitAppRegion: "drag" } as React.CSSProperties
  const noDrag = { WebkitAppRegion: "no-drag" } as React.CSSProperties

  return (
    <div
      className="bg-background flex h-9 shrink-0 items-center justify-between border-b pl-3 select-none"
      style={drag}
    >
      <div className="flex items-center gap-2">
        <span className="text-sm font-semibold">omni</span>
      </div>
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
