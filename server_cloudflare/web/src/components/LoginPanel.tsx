import { useState } from "react"
import { BotIcon } from "lucide-react"

import { api, setToken, type Site, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label } from "@/components/ui/controls"
import { Card, CardContent, CardHeader, CardTitle, Dialog } from "@/components/ui/layout"

/** 默认用户协议文案（后台未配置协议时使用） */
const DEFAULT_AGREEMENT =
  "登录即代表您已阅读并同意平台服务条款与隐私政策。平台可能调整服务功能与计费规则，调整前将通过站内通知告知。"

/**
 * 登录面板：邮箱 + 密码 或 邮箱 + 验证码。
 * 口径对齐后端 admin.post('/auth/login')——传 code 走验证码、传 password 走密码，
 * 后端按请求体形状分流，两种方式共用这一个接口。
 */
export function LoginPanel({ site, onLogin }: { site: Site; onLogin: (u: User) => void }) {
  const [email, setEmail] = useState("")
  const [code, setCode] = useState("")
  const [password, setPassword] = useState("")
  const [mode, setMode] = useState<"password" | "code">("password")
  const [agreed, setAgreed] = useState(false)
  const [agreementOpen, setAgreementOpen] = useState(false)
  const [testHint, setTestHint] = useState("")
  const [err, setErr] = useState("")
  const [busy, setBusy] = useState(false)

  const send = async () => {
    setErr("")
    if (!email) return setErr("请输入邮箱")
    try {
      const r = await api.sendCode(email, "login")
      // 未配置邮件服务时后端会回显验证码（测试模式），原样提示出来便于自测
      if (r.hint) setTestHint(r.hint)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const login = async () => {
    setErr("")
    if (!email) return setErr("请输入邮箱")
    if (mode === "password" && !password) return setErr("请输入密码")
    if (mode === "code" && !code) return setErr("请输入验证码")
    if (!agreed) return setErr("请先勾选并同意《用户协议》")
    setBusy(true)
    try {
      // 只传当前模式的那个字段：两个都传时后端优先按验证码分支走
      const r = await api.login(email, mode === "password" ? { password } : { code })
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
    <div className="from-primary/8 via-background to-background flex min-h-screen flex-col items-center justify-center bg-gradient-to-b p-4">
      <Card className="w-full max-w-sm shadow-md">
        <CardHeader className="items-center text-center">
          <span className="bg-primary mx-auto mb-2 flex size-12 items-center justify-center rounded-2xl text-white shadow-md [&_svg]:size-6">
            <BotIcon />
          </span>
          <CardTitle>{site.title || "omni server"}</CardTitle>
          <p className="text-muted-foreground text-sm">
            {mode === "password" ? "使用邮箱与密码登录" : "使用邮箱验证码登录"}
          </p>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label>邮箱</Label>
            <Input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="请输入邮箱"
              autoComplete="username"
            />
          </div>

          {mode === "password" ? (
            <div className="flex flex-col gap-1">
              <Label>密码</Label>
              <Input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="请输入密码"
                autoComplete="current-password"
              />
            </div>
          ) : (
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
          )}

          <button
            type="button"
            className="text-primary self-start text-xs underline underline-offset-2"
            onClick={() => {
              setErr("")
              setMode(mode === "password" ? "code" : "password")
            }}
          >
            {mode === "password" ? "改用验证码登录" : "改用密码登录"}
          </button>

          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="accent-primary mt-0.5 size-4 cursor-pointer"
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
