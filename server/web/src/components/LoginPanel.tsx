import { useState } from "react"

import { api, setToken, type Site, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label } from "@/components/ui/controls"
import { Card, CardContent, CardHeader, CardTitle, Dialog } from "@/components/ui/layout"

/** 默认用户协议文案（后台未配置协议时使用） */
const DEFAULT_AGREEMENT =
  "登录即代表您已阅读并同意平台服务条款与隐私政策。平台可能调整服务功能与计费规则，调整前将通过站内通知告知。"

export function LoginPanel({ site, onLogin }: { site: Site; onLogin: (u: User) => void }) {
  const [phone, setPhone] = useState("")
  const [code, setCode] = useState("")
  const [agreed, setAgreed] = useState(false)
  const [agreementOpen, setAgreementOpen] = useState(false)
  const [testHint, setTestHint] = useState("")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)

  const send = async () => {
    setErr("")
    try {
      const r = await api.sendCode(phone)
      if (r.hint) setTestHint(r.hint)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const login = async () => {
    setErr("")
    if (!phone || !code) return setErr("请输入手机号与验证码")
    if (!agreed) return setErr("请先勾选并同意《用户协议》")
    setBusy(true)
    try {
      const r = await api.login(phone, code, agreed)
      setToken(r.token)
      onLogin(r.user)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const agreementText = site.agreement || DEFAULT_AGREEMENT

  return (
    <div className="flex min-h-screen flex-col items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{site.title || "omni server"}</CardTitle>
          <p className="text-muted-foreground text-sm">
            手机号一键登录；未注册的手机号将自动注册。
          </p>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label>手机号</Label>
            <Input value={phone} onChange={(e) => setPhone(e.target.value)} placeholder="请输入手机号" />
          </div>
          <div className="flex flex-col gap-1">
            <Label>验证码</Label>
            <div className="flex gap-2">
              <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="验证码" />
              <Button type="button" variant="secondary" onClick={() => void send()}>
                发送验证码
              </Button>
            </div>
            {testHint && <p className="text-muted-foreground mt-1 text-xs">{testHint}</p>}
          </div>
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-0.5"
              checked={agreed}
              onChange={(e) => setAgreed(e.target.checked)}
            />
            <span>
              我已阅读并同意{" "}
              <button
                type="button"
                onClick={() => setAgreementOpen(true)}
                className="text-primary cursor-pointer underline underline-offset-2"
              >
                《用户协议》
              </button>
            </span>
          </label>
          {err && <p className="text-destructive text-sm">{err}</p>}
          <Button onClick={() => void login()} disabled={busy} className="mt-1">
            {busy ? "登录中…" : "登录"}
          </Button>
        </CardContent>
      </Card>

      {/* 用户协议弹窗：内容由后台「站点设置」管理 */}
      <Dialog open={agreementOpen} onOpenChange={setAgreementOpen} title="用户协议" className="max-w-lg">
        <div className="text-muted-foreground max-h-72 overflow-y-auto whitespace-pre-wrap text-sm">{agreementText}</div>
        <div className="mt-4 flex justify-end">
          <Button variant="outline" onClick={() => setAgreementOpen(false)}>
            我知道了
          </Button>
        </div>
      </Dialog>
    </div>
  )
}
