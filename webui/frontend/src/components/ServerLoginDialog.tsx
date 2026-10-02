import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { useI18n } from "@/lib/i18n"
import { server, type ServerUser } from "@/lib/serverAuth"

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
  const [phone, setPhone] = useState("")
  const [code, setCode] = useState("")
  const [agreed, setAgreed] = useState(false)
  const [hint, setHint] = useState("")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)

  const send = async () => {
    setErr("")
    if (!/^\d{4,20}$/.test(phone.trim())) return setErr(t("login.phone"))
    try {
      const r = await server.sendCode(phone.trim())
      if (r.hint) setHint(r.hint)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const login = async () => {
    setErr("")
    if (!/^\d{4,20}$/.test(phone.trim()) || !code.trim()) return setErr(t("login.phone") + " / " + t("login.code"))
    if (!agreed) return setErr(t("login.agreement"))
    setBusy(true)
    try {
      // token 由 server.login 直接写入 localStorage
      const r = await server.login(phone.trim(), code.trim(), agreed)
      onLoggedIn(r.user)
      onOpenChange(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
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
          <div className="flex flex-col gap-1">
            <Label>{t("login.phone")}</Label>
            <Input value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="13800000000" />
          </div>
          <div className="flex flex-col gap-1">
            <Label>{t("login.code")}</Label>
            <div className="flex gap-2">
              <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="111111" />
              <Button type="button" variant="secondary" onClick={() => void send()}>
                {t("login.send")}
              </Button>
            </div>
            {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
          </div>
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-0.5" checked={agreed} onChange={(e) => setAgreed(e.target.checked)} />
            <span>{t("login.agreement")}</span>
          </label>
          {err && <p className="text-destructive text-sm">{err}</p>}
          <Button onClick={() => void login()} disabled={busy}>
            {busy ? "…" : t("login.submit")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}