import { useCallback, useEffect, useRef, useState } from "react"
import QRCode from "qrcode"
import { Loader2Icon, RefreshCwIcon, SmartphoneIcon } from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { MobileInfo } from "@/types"

/**
 * 左下角「移动端」入口：悬停弹出面板，上方是扫码 URL 的二维码，
 * 下方是「允许移动端连接此设备」开关。
 *
 * 二维码内容就是 urls[0]（形如 http://<内网IP>:8792/device/<设备token>，
 * 部署到线上后是 https://<域名>/device/<设备token>）：
 * 手机扫码直接进入工作台，不需要登录。token 首次校验后由内核换成
 * HttpOnly Cookie 并 302 到干净的 /，所以它不会长期留在手机地址栏里。
 *
 * token 本身不过期，但可以手动换发：面板右上角的刷新按钮会先弹确认，
 * 因为换发后旧二维码和已扫码的设备会立刻失效，必须重新扫。
 */
export function MobilePanel() {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [info, setInfo] = useState<MobileInfo | null>(null)
  const [qr, setQr] = useState("")
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")
  const [rotating, setRotating] = useState(false)
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
  //
  // rotating 期间不关：换发确认框是 portal 到 body 的，鼠标点过去时本面板
  // 必然已经触发 mouseleave，不拦住的话确认框一开面板就缩回去，
  // 用户确认完只剩一个空图标，得重新悬停才知道换没换成。
  const scheduleClose = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    if (rotating) return
    closeTimer.current = setTimeout(() => setOpen(false), 250)
  }
  const cancelClose = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
  }

  // 面板打开时按 Esc 关闭
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !rotating) setOpen(false)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, rotating])

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

  // 换发 token：旧二维码与已扫码的设备立刻失效，所以必须先在对话框里确认
  const rotate = async () => {
    setBusy(true)
    try {
      setInfo(await api.mobileRotate())
      setErr("")
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

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
          <div className="mb-2 flex items-center justify-between gap-2">
            <span className="text-sm font-medium">{t("mobile.title")}</span>
            {/* 换发 token：图标常驻但置灰，鼠标移上去才显形，避免抢二维码的注意力 */}
            <button
              type="button"
              title={t("mobile.refresh")}
              aria-label={t("mobile.refresh")}
              disabled={!enabled || busy}
              onClick={() => {
                cancelClose()
                setOpen(true)
                setRotating(true)
              }}
              className={cn(
                "text-muted-foreground hover:text-foreground hover:bg-accent cursor-pointer rounded-sm p-1 transition-colors",
                "disabled:pointer-events-none disabled:opacity-30",
                "focus-visible:ring-ring/50 focus-visible:ring-[3px] outline-none",
              )}
            >
              <RefreshCwIcon className={cn("size-3.5", busy && "animate-spin")} />
            </button>
          </div>

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

      {/* 换发确认框。独立于面板之外（portal），所以面板因鼠标移开而收起也不影响它 */}
      <AlertDialog open={rotating} onOpenChange={setRotating}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("mobile.refreshTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("mobile.refreshDesc")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={busy}>{t("mobile.refreshCancel")}</AlertDialogCancel>
            <AlertDialogAction disabled={busy} onClick={() => void rotate()}>
              {t("mobile.refreshOk")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
