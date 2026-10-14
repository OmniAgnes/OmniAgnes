import { useEffect, useState } from "react"

import { api, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label } from "@/components/ui/controls"
import { Dialog } from "@/components/ui/layout"

/** 修改自己的手机号：新号先发验证码再确认（测试模式回显验证码）。 */
export function ChangePhoneDialog({
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
  const [phone, setPhone] = useState("")
  const [code, setCode] = useState("")
  const [hint, setHint] = useState("")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open) {
      setPhone("")
      setCode("")
      setHint("")
      setErr("")
    }
  }, [open])

  const send = async () => {
    setErr("")
    if (!/^\d{4,20}$/.test(phone.trim())) return setErr("请输入正确的手机号")
    try {
      const r = await api.sendCode(phone.trim())
      if (r.hint) setHint(r.hint)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const submit = async () => {
    setErr("")
    if (!/^\d{4,20}$/.test(phone.trim()) || !code.trim()) return setErr("请输入新手机号与验证码")
    setBusy(true)
    try {
      await api.mePhone(phone.trim(), code.trim())
      onSaved()
      onOpenChange(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title={`修改手机号（当前 ${user.phone}）`}>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label>新手机号</Label>
          <div className="flex gap-2">
            <Input value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="请输入新手机号" />
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
