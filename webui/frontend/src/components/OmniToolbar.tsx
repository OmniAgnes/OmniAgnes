import { useEffect, useRef, useState } from "react"
import {
  CalendarClockIcon,
  ChevronRightIcon,
  CoinsIcon,
  LanguagesIcon,
  LogInIcon,
  MaximizeIcon,
  PaletteIcon,
  ZoomInIcon,
  ZoomOutIcon,
} from "lucide-react"

import { cn } from "@/lib/utils"
import { useI18n, type Lang } from "@/lib/i18n"
import type { ServerUser } from "@/lib/serverAuth"

// 8 套主题（与设置里一致），名称走 i18n
const THEMES = [
  "dark",
  "light",
  "violet",
  "blue",
  "sky",
  "emerald",
  "amber",
  "rose",
] as const

const LANGS: { id: Lang; label: string }[] = [
  { id: "zh-Hans", label: "简体中文" },
  { id: "zh-Hant", label: "繁體中文" },
  { id: "en", label: "English" },
]

type Sub = "none" | "lang" | "theme" | "zoom"

export function OmniToolbar({
  zoom,
  onZoom,
  currentTheme,
  onTheme,
  onOpenUsage,
  onOpenAccount,
  onRefreshUser,
  user,
  onOpenLogin,
}: {
  zoom: number
  onZoom: (z: number) => void
  currentTheme: string
  onTheme: (id: string) => void
  onOpenUsage: () => void
  /** 点积分行：打开设置「账号」页签看余额与流水 */
  onOpenAccount: () => void
  /** 点左下角头像：已登录则重新拉一次资料，积分余额及时更新 */
  onRefreshUser: () => void
  user: ServerUser | null
  onOpenLogin: () => void
}) {
  const { t, lang, setLang } = useI18n()
  const [open, setOpen] = useState(false)
  const [sub, setSub] = useState<Sub>("none")
  const wrapRef = useRef<HTMLDivElement>(null)

  // 点其他地方关闭弹出菜单
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) {
        setOpen(false)
        setSub("none")
      }
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [open])

  const items: { key: Sub; icon: React.ReactNode; label: string }[] = [
    { key: "lang", icon: <LanguagesIcon className="size-4" />, label: t("menu.language") },
    { key: "theme", icon: <PaletteIcon className="size-4" />, label: t("menu.theme") },
    // 缩放走桌面端原生页面缩放，网页端不提供（浏览器自带缩放）
    ...(window.omniDesktop
      ? [{ key: "zoom" as Sub, icon: <ZoomInIcon className="size-4" />, label: `${t("menu.zoom")} · ${Math.round(zoom * 100)}%` }]
      : []),
  ]

  return (
    <div ref={wrapRef} className="fixed bottom-3 left-3 z-50">
      <div className="relative">
        <button
          type="button"
          onClick={() => {
            setOpen((o) => !o)
            onRefreshUser()
          }}
          title={t("toolbar.title")}
          className="bg-foreground text-background flex size-8 items-center justify-center rounded-full text-sm font-bold shadow-md cursor-pointer"
        >
          o
        </button>

        {/* 菜单向上弹出 */}
        {open && (
          <div className="bg-background text-foreground absolute bottom-full left-0 mb-2 w-52 rounded-lg border p-1 shadow-xl">
            {/* 子菜单右飞 */}
            {sub !== "none" && (
              <div className="bg-background text-foreground absolute left-full top-0 z-10 ml-1 w-40 rounded-lg border p-1 shadow-xl">
                {sub === "lang" &&
                  LANGS.map((l) => (
                    <button
                      key={l.id}
                      type="button"
                      // 切语言不关菜单：用户要看切换后的效果
                      onClick={() => setLang(l.id)}
                      className={cn(
                        "flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent cursor-pointer",
                        lang === l.id && "font-medium",
                      )}
                    >
                      <span className={cn(lang === l.id ? "text-primary" : "text-muted-foreground")}>
                        {l.id === lang ? "●" : "○"}
                      </span>
                      {t(l.id)}
                    </button>
                  ))}
                {sub === "theme" &&
                  THEMES.map((tb) => (
                    <button
                      key={tb}
                      type="button"
                      // 切主题不关菜单：用户要看切换后的效果
                      onClick={() => onTheme(tb)}
                      className={cn(
                        "flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent cursor-pointer",
                        currentTheme === tb && "font-medium",
                      )}
                    >
                      <span className={cn("size-2 shrink-0 rounded-full", currentTheme === tb ? "bg-primary" : "bg-border")} />
                      {t(`theme.${tb}`)}
                    </button>
                  ))}
                {sub === "zoom" && (
                  <>
                    <ZoomItem onClick={() => { onZoom(Math.min(2, +(zoom + 0.1).toFixed(1))); setSub("none") }} icon={<ZoomInIcon className="size-3.5" />} label={t("zoom.in")} hint={t("zoom.hint.in")} />
                    <ZoomItem onClick={() => { onZoom(Math.max(0.5, +(zoom - 0.1).toFixed(1))); setSub("none") }} icon={<ZoomOutIcon className="size-3.5" />} label={t("zoom.out")} hint={t("zoom.hint.out")} />
                    <ZoomItem onClick={() => { onZoom(1); setSub("none") }} icon={<MaximizeIcon className="size-3.5" />} label={t("zoom.reset")} hint={t("zoom.hint.reset")} />
                  </>
                )}
              </div>
            )}

            <div className="flex flex-col gap-0.5">
              {items.map((m) => (
                <button
                  key={m.key}
                  type="button"
                  onClick={() => setSub(sub === m.key ? "none" : m.key)}
                  className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm hover:bg-accent cursor-pointer"
                >
                  {m.icon}
                  <span className="flex-1">{m.label}</span>
                  <ChevronRightIcon className="text-muted-foreground size-3.5" />
                </button>
              ))}
            </div>
            <SeparatorLight />
            <button
              type="button"
              onClick={() => { onOpenUsage(); setOpen(false) }}
              className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm hover:bg-accent cursor-pointer"
            >
              <CalendarClockIcon className="size-4" />
              <span className="flex-1">{t("menu.usage")}</span>
            </button>
            <SeparatorLight />
            {user ? (
              /* 积分行：与「使用统计」同款左对齐样式；余额为 0 时不显示数字；点击进设置「账号」页签。
                 退出登录也在「账号」页签里（帐号显示右侧），这里不再放 */
              <button
                type="button"
                onClick={() => { onOpenAccount(); setOpen(false) }}
                title={t("se.account")}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm hover:bg-accent cursor-pointer"
              >
                <CoinsIcon className="size-4" />
                <span className="flex-1">{t("me.credits")}</span>
                <span className="text-muted-foreground tabular-nums">
                  {user.balance > 0 ? user.balance : null}
                </span>
              </button>
            ) : (
              <button
                type="button"
                onClick={() => { onOpenLogin(); setOpen(false) }}
                className="flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-sm hover:bg-accent cursor-pointer"
              >
                <LogInIcon className="size-4" />
                {t("menu.login")}
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

function ZoomItem({
  onClick,
  icon,
  label,
  hint,
}: {
  onClick: () => void
  icon: React.ReactNode
  label: string
  hint: string
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-1.5 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent cursor-pointer"
    >
      {icon}
      <span className="flex-1">{label}</span>
      <span className="text-muted-foreground shrink-0 text-[10px]">{hint}</span>
    </button>
  )
}

function SeparatorLight() {
  return <div className="bg-border my-1 h-px" />
}
