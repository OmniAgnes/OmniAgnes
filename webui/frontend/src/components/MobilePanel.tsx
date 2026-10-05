import { useCallback, useEffect, useRef, useState } from "react"
import QRCode from "qrcode"
import { Loader2Icon, SmartphoneIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { MobileInfo } from "@/types"

/**
 * 左下角「移动端」入口：悬停弹出面板，上方是扫码 URL 的二维码，
 * 下方是「允许移动端连接此设备」开关。
 *
 * 二维码内容就是 urls[0]（形如 http://<内网IP>:8792/device/<设备token>）：
 * 手机扫码直接进入工作台，不需要登录。token 首次校验后由内核换成
 * HttpOnly Cookie 并 302 到干净的 /，所以它不会长期留在手机地址栏里。
 */
export function MobilePanel() {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [info, setInfo] = useState<MobileInfo | null>(null)
  const [qr, setQr] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const wrapRef = useRef<HTMLDivElement>(null)

  const load = useCallback(() => {
    api
      .mobile()
      .then((r) => {
        setInfo(r)
        setErr("")
      })
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)))
  }, [])

  // 首次挂载拉一次状态，之后只在开关时刷新（面板是常驻的，不必轮询）
  useEffect(() => {
    load()
  }, [load])

  // 二维码跟着 URL 走；关停时清掉，避免扫一个已经失效的码
  const url = info?.enabled && info.urls.length > 0 ? info.urls[0] : ""
  useEffect(() => {
    if (!url) {
      setQr("")
      return
    }
    let alive = true
    QRCode.toDataURL(url, {
      width: 200,
      margin: 1,
      // 深色模块用前景色、浅色用卡片底色，保证在暗色主题下也扫得出来
      color: { dark: "#000000ff", light: "#ffffffff" },
      errorCorrectionLevel: "M",
    })
      .then((d) => {
        if (alive) setQr(d)
      })
      .catch(() => {
        if (alive) setQr("")
      })
    return () => {
      alive = false
    }
  }, [url])

  // 悬停延迟关闭：鼠标要从图标移到面板里，250ms 足够又不会粘手
  const scheduleClose = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    closeTimer.current = setTimeout(() => setOpen(false), 250)
  }
  const cancelClose = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
  }

  // 面板打开时按 Esc 关闭
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open])

  useEffect(() => () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
  }, [])

  const toggle = async (next: boolean) => {
    setBusy(true)
    try {
      setInfo(await api.mobileToggle(next))
      setErr("")
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const enabled = Boolean(info?.enabled)

  return (
    <div
      ref={wrapRef}
      className="relative"
      onMouseEnter={() => {
        cancelClose()
        setOpen(true)
      }}
      onMouseLeave={scheduleClose}
    >
      <Button
        variant="ghost"
        size="icon-sm"
        title={t("mobile.title")}
        aria-label={t("mobile.title")}
        aria-expanded={open}
        className={cn(enabled && "text-primary")}
      >
        <SmartphoneIcon />
      </Button>

      {open && (
        <div
          // 与图标同处一个 relative 容器：left-0 让面板从图标左边缘向右展开，
          // 避免向左溢出侧边栏被裁掉
          className="bg-popover text-popover-foreground absolute bottom-full left-0 z-50 mb-2 w-64 rounded-md border p-3 shadow-lg"
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        >
          <div className="mb-2 text-sm font-medium">{t("mobile.title")}</div>

          {err ? (
            <p className="text-destructive py-6 text-center text-xs">{err}</p>
          ) : enabled && qr ? (
            <>
              <div className="flex justify-center">
                <img src={qr} alt={t("mobile.qrAlt")} className="size-48 rounded-sm" />
              </div>
            </>
          ) : (
            <div className="text-muted-foreground flex h-48 items-center justify-center px-2 text-center text-xs">
              {busy ? <Loader2Icon className="size-4 animate-spin" /> : t("mobile.off")}
            </div>
          )}

          {/* 开关行：文案 + 右侧 role=switch 按钮 */}
          <div className="mt-3 flex items-center justify-between gap-2 border-t pt-3">
            <div className="min-w-0">
              <div className="text-xs font-medium">{t("mobile.switch")}</div>
              <div className="text-muted-foreground text-[11px]">{t("mobile.hint")}</div>
            </div>
            <button
              type="button"
              role="switch"
              aria-checked={enabled}
              aria-label={t("mobile.switch")}
              disabled={busy}
              onClick={() => void toggle(!enabled)}
              className={cn(
                "relative inline-flex h-5 w-9 shrink-0 cursor-pointer items-center rounded-full transition-colors outline-none",
                "focus-visible:ring-ring/50 focus-visible:ring-[3px] disabled:opacity-50",
                enabled ? "bg-primary" : "bg-input",
              )}
            >
              <span
                className={cn(
                  "bg-background pointer-events-none block size-4 rounded-full shadow-sm transition-transform",
                  enabled ? "translate-x-4.5" : "translate-x-0.5",
                )}
              />
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
