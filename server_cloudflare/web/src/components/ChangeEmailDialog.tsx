import { useEffect, useState } from "react"

import { api, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label } from "@/components/ui/controls"
import { Dialog } from "@/components/ui/layout"

/** 修改自己的邮箱：新邮箱先收验证码再确认（后端 /admin/profile/email/code + /admin/profile/email）。
 *  走「新邮箱收码」而不是旧邮箱——初始化用的超管邮箱往往是填出来的假地址，收不到信。 */
export function ChangeEmailDialog({
  open,
  onOpenChange,
  user,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: User
  /** 保存成功后回调（父级刷新 me） */
  onSaved: () => void
}) {
  const [email, setEmail] = useState("")
  const [code, setCode] = useState("")
  const [hint, setHint] = useState("")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open) {
      setEmail("")
      setCode("")
      setHint("")
      setErr("")
    }
  }, [open])

  const send = async () => {
    setErr("")
    const v = email.trim()
    // 粗判邮箱形状：后端也会校验（validEmail），这里先挡一次少一次往返
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)) return setErr("请输入正确的邮箱")
    try {
      await api.sendProfileEmailCode(v)
      setHint("验证码已发送，请查收新邮箱")
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const submit = async () => {
    setErr("")
    if (!email.trim() || !code.trim()) return setErr("请输入新邮箱与验证码")
    setBusy(true)
    try {
      await api.saveProfileEmail(email.trim(), code.trim())
      onSaved()
      onOpenChange(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={`修改邮箱（当前 ${user.email}）`}>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label>新邮箱</Label>
          <div className="flex gap-2">
            <Input value={email} onChange={(e) => setEmail(e.target.value)} placeholder="请输入新邮箱" />
            <Button type="button" variant="secondary" onClick={() => void send()}>
              发送验证码
            </Button>
          </div>
          {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
        </div>
        <div className="flex flex-col gap-1">
          <Label>验证码</Label>
          <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="请输入验证码" />
        </div>
        {err && <p className="text-destructive text-sm">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button disabled={busy} onClick={() => void submit()}>
            {busy ? "提交中…" : "确认修改"}
          </Button>
        </div>
      </div>
    </Dialog>
  )
}
