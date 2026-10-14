import { useEffect, useRef, useState } from "react"

import { api, getToken, setToken, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label } from "@/components/ui/controls"
import { Card, CardContent } from "@/components/ui/layout"

/**
 * 桌面端授权页（#/ide/login）：
 *   1. 用平台账号登录（邮箱 + 验证码，未配邮件时任意码通过；密码是管理员兜底）
 *   2. 换取一次性授权码（10 分钟、一次有效）
 *   3. 跳 omni://auth?code=xxx 把授权码交给本机桌面端；唤不起就复制授权码手动粘贴
 *
 * 页面本身不依赖登录态；未登录直接访问即进入登录流程。
 */
export function IdeLogin() {
  const [booting, setBooting] = useState(true)
  const [mode, setMode] = useState<"login" | "register">("login")
  const [way, setWay] = useState<"code" | "password">("code")
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [name, setName] = useState("")
  const [code, setCode] = useState("")
  const [busy, setBusy] = useState(false)
  const [pair, setPair] = useState<{ code: string; expires_in: number; app_url: string; user: User } | null>(null)
  const [left, setLeft] = useState(0)
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState("")
  const wayApplied = useRef(false)

  const deviceName = "omni 桌面端"

  // 已有登录态（同一浏览器登录过后台）→ 直接进入授权步骤
  useEffect(() => {
    const token = getToken()
    if (!token) {
      setBooting(false)
      return
    }
    api
      .me()
      .then(() => void doPair())
      .catch(() => {
        setToken("")
        setBooting(false)
      })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 邮件已配置默认验证码登录；未配置时验证码任意通过（登录页可两个方式都试）
  useEffect(() => {
    if (wayApplied.current) return
    api
      .settings()
      .then((s) => {
        wayApplied.current = true
        setWay(s.mail_configured ? "code" : "password")
      })
      .catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 授权码倒计时
  useEffect(() => {
    if (!pair) return
    setLeft(pair.expires_in)
    const t = setInterval(() => setLeft((v) => (v <= 1 ? 0 : v - 1)), 1000)
    return () => clearInterval(t)
  }, [pair])

  const doPair = async () => {
    try {
      setPair(await api.pair(deviceName))
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBooting(false)
    }
  }

  const login = async () => {
    setErr("")
    const isRegister = mode === "register"
    if (!email.trim()) return setErr("请输入邮箱")
    if (isRegister && !code.trim()) return setErr("请输入验证码")
    if (!isRegister && way === "password" && !password) return setErr("请输入密码")
    if (!isRegister && way === "code" && !code.trim()) return setErr("请输入验证码")
    setBusy(true)
    try {
      const r = isRegister
        ? await api.register(email.trim(), "", code.trim(), name.trim() || undefined)
        : await api.login(email.trim(), way === "code" ? { code: code.trim() } : { password })
      if (!r.token) return setErr("服务端未返回令牌，请重试")
      setToken(r.token)
      await doPair()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const switchAccount = async () => {
    try {
      await api.logout()
    } catch {
      /* 忽略 */
    }
    setToken("")
    setPair(null)
    setEmail("")
    setPassword("")
    setCode("")
    setErr("")
  }

  const openApp = () => {
    if (!pair) return
    window.location.href = pair.app_url
  }

  const copyCode = async () => {
    if (!pair) return
    try {
      await navigator.clipboard.writeText(pair.code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      setErr("复制失败，请手动选中授权码复制")
    }
  }

  const showCode = mode === "register" || way === "code"

  return (
    <div className="bg-muted/40 flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardContent className="p-6">
          <div className="mb-6 flex items-center gap-2">
            <div className="bg-primary text-primary-foreground grid size-8 place-items-center rounded-md text-sm font-bold">O</div>
            <div>
              <div className="font-semibold">授权登录 omni</div>
              <div className="text-muted-foreground text-xs">使用平台账号授权本机桌面端</div>
            </div>
          </div>

          {booting ? (
            <div className="text-muted-foreground flex items-center justify-center gap-2 py-10 text-sm">正在检查登录状态…</div>
          ) : pair ? (
            <div className="space-y-4">
              <div className="bg-muted/30 rounded-md border p-3 text-sm">
                已为 <b>{pair.user.name || pair.user.email}</b> 生成授权码，剩余有效时间{" "}
                {String(Math.floor(left / 60)).padStart(2, "0")}:{String(left % 60).padStart(2, "0")}
              </div>

              <Button className="w-full" size="lg" onClick={openApp} disabled={left <= 0}>
                打开 omni 桌面端
              </Button>
              <p className="text-muted-foreground text-xs">
                点上方按钮会唤起本机 omni 并自动完成登录。如果浏览器没有反应（未安装或系统未注册协议），请复制下面的授权码，
                回到桌面端点「登录」后粘贴。
              </p>

              <div className="space-y-1">
                <Label className="text-xs">授权码（10 分钟内有效，用一次即作废）</Label>
                <div className="flex gap-2">
                  <Input readOnly value={pair.code} className="font-mono text-xs" onFocus={(e) => e.currentTarget.select()} />
                  <Button variant="outline" onClick={copyCode}>{copied ? "已复制" : "复制"}</Button>
                </div>
              </div>

              <button type="button" className="text-muted-foreground text-xs underline" onClick={switchAccount}>
                换一个账号登录
              </button>
            </div>
          ) : (
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault()
                void login()
              }}
            >
              {mode === "register" && (
                <div className="space-y-1">
                  <Label>账号名（选填）</Label>
                  <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="不填则取邮箱 @ 前面的部分" />
                </div>
              )}
              <div className="space-y-1">
                <Label>邮箱</Label>
                <Input type="email" autoFocus value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@example.com" />
              </div>
              {showCode ? (
                <div className="space-y-1">
                  <Label>邮箱验证码</Label>
                  <div className="flex gap-2">
                    <Input value={code} onChange={(e) => setCode(e.target.value)} placeholder="验证码" />
                    <Button
                      type="button"
                      variant="secondary"
                      onClick={() => void api.sendCode(email.trim(), mode === "register" ? "register" : "login")}
                    >
                      发送验证码
                    </Button>
                  </div>
                  <p className="text-muted-foreground text-xs">未配置邮件服务时验证码任意通过。</p>
                </div>
              ) : (
                <div className="space-y-1">
                  <Label>密码</Label>
                  <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
                </div>
              )}
              {mode === "login" && (
                <button
                  type="button"
                  className="text-muted-foreground text-xs underline"
                  onClick={() => setWay(way === "code" ? "password" : "code")}
                >
                  {way === "code" ? "用密码登录" : "用验证码登录"}
                </button>
              )}
              {err && <p className="text-destructive text-sm">{err}</p>}
              <Button className="w-full" type="submit" disabled={busy || !email}>
                {busy ? "处理中…" : mode === "register" ? "注册并授权" : "登录并授权"}
              </Button>
              <p className="text-muted-foreground text-xs">账号与后台一致。桌面端只会拿到一份可随时吊销的长期授权，不会保存你的密码。</p>
              <button
                type="button"
                className="text-muted-foreground text-xs underline"
                onClick={() => setMode(mode === "login" ? "register" : "login")}
              >
                {mode === "login" ? "没有账号？直接注册" : "已有账号？去登录"}
              </button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
