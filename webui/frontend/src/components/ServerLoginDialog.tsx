import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { useI18n } from "@/lib/i18n"
import { server, authLoginUrl, type ServerUser } from "@/lib/serverAuth"
import { isDesktop } from "@/lib/platform"

/**
 * 登录弹窗：打开网页授权。
 *
 * 链路：
 *   1. 点「打开授权页」→ 主进程用系统浏览器打开后台 /#/ide/login
 *   2. 网页用平台账号登录 → 生成一次性授权码 → 跳 omni://auth?code=xxx
 *   3. 主进程收到协议回调 → 经 IPC 把 code 推给本组件 → 调 /api/ide/token 换长期 token
 *
 * 桌面端（Electron）走协议回调自动完成；浏览器端（无 omni:// 协议）退回手动粘贴授权码。
 */
export function ServerLoginDialog({
  open,
  onOpenChange,
  onLoggedIn,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onLoggedIn: (u: ServerUser) => void
}) {
  const { t } = useI18n()
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)
  // 手动粘贴授权码（浏览器端没有 omni:// 协议回调时用）
  const [manualCode, setManualCode] = useState("")

  // 监听主进程推来的授权码（omni://auth?code=xxx 回调）
  useEffect(() => {
    if (!isDesktop || !window.omniDesktop?.auth) return
    const off = window.omniDesktop.auth.onCode((code) => {
      if (typeof code === "string" && code) void exchange(code)
    })
    return off
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const exchange = async (code: string) => {
    setErr("")
    setBusy(true)
    try {
      // 设备名：区分是桌面端还是网页端
      const device = isDesktop ? "omni 桌面端" : "omni 网页端"
      const r = await server.exchangeCode(code.trim(), device)
      onLoggedIn(r.user)
      onOpenChange(false)
      setManualCode("")
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const openAuth = () => {
    setErr("")
    const url = authLoginUrl()
    if (isDesktop && window.omniDesktop?.auth) {
      window.omniDesktop.auth.open(url)
    } else {
      // 浏览器端：直接跳转授权页（同源，登录后手动复制授权码回来粘贴）
      window.open(url, "_blank")
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("login.title")}</DialogTitle>
          <DialogDescription>{t("login.subtitle")}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <Button onClick={openAuth} disabled={busy} className="w-full">
            {busy ? "…" : t("login.openAuth")}
          </Button>
          <p className="text-muted-foreground text-xs">{t("login.authHint")}</p>

          {/* 浏览器端兜底：手动粘贴授权码 */}
          {!isDesktop && (
            <div className="flex flex-col gap-2">
              <div className="flex gap-2">
                <Input
                  value={manualCode}
                  onChange={(e) => setManualCode(e.target.value)}
                  placeholder={t("login.pasteCode")}
                  spellCheck={false}
                  className="font-mono text-xs"
                />
                <Button variant="outline" size="sm" disabled={busy || !manualCode.trim()} onClick={() => void exchange(manualCode)}>
                  {t("login.exchange")}
                </Button>
              </div>
            </div>
          )}

          {err && <p className="text-destructive text-sm">{err}</p>}
        </div>
      </DialogContent>
    </Dialog>
  )
}
