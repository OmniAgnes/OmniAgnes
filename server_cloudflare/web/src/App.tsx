import { useEffect, useRef, useState } from "react"
import { BotIcon, ChevronDownIcon } from "lucide-react"

import { AdminPanel } from "@/components/AdminPanel"
import { ChangeEmailDialog } from "@/components/ChangeEmailDialog"
import { IdeLogin } from "@/components/IdeLogin"
import { Landing } from "@/components/Landing"
import { LoginPanel } from "@/components/LoginPanel"
import { MemberPanel, type MemberSection } from "@/components/MemberPanel"
import { Button } from "@/components/ui/button"
import { api, getToken, isAdmin, setToken, type Site, type User } from "@/lib/api"

/** hash 路由：#/ 推广首页、#/me、#/me/usage、#/admin、#/admin/members|usage|models|providers|settings。不同界面 URL 不同，可分享、刷新不 404。 */
function useHashRoute(): string {
  const [route, setRoute] = useState(() => location.hash.replace(/^#/, "") || "/")
  useEffect(() => {
    const fn = () => setRoute(location.hash.replace(/^#/, "") || "/")
    window.addEventListener("hashchange", fn)
    return () => window.removeEventListener("hashchange", fn)
  }, [])
  return route
}

const nav = (h: string) => {
  location.hash = h
}

type AdminSection = "members" | "usage" | "models" | "upstreams" | "mail"
function sectionOf(route: string): AdminSection | null {
  if (!route.startsWith("/admin")) return null
  const seg = route.split("/")[2]
  return seg === "usage" || seg === "models" || seg === "upstreams" || seg === "mail" ? (seg as AdminSection) : "members"
}

function memberSectionOf(route: string): MemberSection | null {
  if (route === "/me/usage") return "usage"
  if (route === "/me") return "account"
  return null
}

export function App() {
  const route = useHashRoute()
  const [user, setUser] = useState<User | null>(null)
  const [site, setSite] = useState<Site>({ title: "", footer: "", agreement: "", test_mode: false })
  const [booting, setBooting] = useState(true)

  const loadMe = async () => {
    if (!getToken()) {
      setUser(null)
      return
    }
    try {
      setUser(await api.me())
    } catch {
      // token 失效/过期：登出态
      setToken("")
      setUser(null)
    }
  }
  useEffect(() => {
    api.site().then(setSite).catch(() => {})
    void loadMe().finally(() => setBooting(false))
  }, [])

  useEffect(() => {
    document.title = site.title || "omni server"
  }, [site.title])

  const refreshMe = () => void loadMe()

  const logout = async () => {
    try {
      await api.logout()
    } catch {
      /* 忽略 */
    }
    setToken("")
    setUser(null)
    nav("/")
  }

  const adminSection = sectionOf(route)
  const memberSection = memberSectionOf(route)
  const isHome = route === "/" || route === ""

  // 路由守卫：非管理员访问后台回我的页；未知路由回我的页（首页与授权页放行）
  useEffect(() => {
    if (booting || !user || isHome || route === "/ide/login") return
    if (adminSection && !isAdmin(user)) nav("/me")
    else if (!adminSection && !memberSection) nav("/me")
  }, [user, route, adminSection, memberSection, booting, isHome])

  if (booting) {
    return <div className="text-muted-foreground flex min-h-screen items-center justify-center text-sm">加载中…</div>
  }

  // 桌面端授权页：独立于登录态（未登录也能进，页面内完成登录 + 授权码生成）
  if (route === "/ide/login") {
    return <IdeLogin />
  }

  // 推广首页：未登录展示「登录 / 注册」，已登录展示「进入控制台」
  if (isHome) {
    return <Landing site={site} loggedIn={!!user} />
  }

  // 未登录：登录页（除首页外任何 hash 都显示登录）
  if (!user) {
    return (
      <>
        <LoginPanel site={site} onLogin={(u) => { setUser(u); nav("/me") }} />
        {site.footer && <Footer text={site.footer} />}
      </>
    )
  }

  const showAdmin = adminSection !== null && isAdmin(user)

  return (
    <div className="flex min-h-screen flex-col">
      <Header
        site={site}
        user={user}
        activeAdmin={showAdmin}
        onLogout={() => void logout()}
        onUserChanged={refreshMe}
      />
      <main className="mx-auto w-full max-w-5xl flex-1 px-3 py-4 md:px-6 md:py-6">
        {showAdmin ? (
          <AdminPanel section={adminSection!} onNav={(s) => nav(`/admin/${s}`)} />
        ) : (
          <MemberPanel
            section={memberSection ?? "account"}
            user={user}
            onNav={(s) => nav(s === "usage" ? "/me/usage" : "/me")}
          />
        )}
      </main>
      {site.footer && <Footer text={site.footer} />}
    </div>
  )
}

function Footer({ text }: { text: string }) {
  return <div className="text-muted-foreground border-t px-4 py-3 text-center text-xs">{text}</div>
}

/** 顶栏：左侧站点标题（跳我的页）；右侧管理员「后台管理」按钮 + 账号菜单（改邮箱/退出）。 */
function Header({
  site,
  user,
  activeAdmin,
  onLogout,
  onUserChanged,
}: {
  site: Site
  user: User
  activeAdmin: boolean
  onLogout: () => void
  onUserChanged: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [emailOpen, setEmailOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // 点菜单外区域关闭
  useEffect(() => {
    if (!menuOpen) return
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setMenuOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [menuOpen])

  return (
    <header className="border-border/70 sticky top-0 z-40 border-b bg-card">
      {/* 内容宽度与下方 main 一致（max-w-5xl 居中）；
          注意：这里不能用 backdrop-blur——它会成为 fixed 元素的包含块，
          导致页面内的弹窗（如修改邮箱）fixed 定位参照 header 而跑到界面上方 */}
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-2 px-3 py-2.5 md:px-6 md:py-3">
      <button type="button" className="flex cursor-pointer items-center gap-2" onClick={() => nav("/")}>
        <span className="bg-primary flex size-7 items-center justify-center rounded-lg text-white [&_svg]:size-4">
          <BotIcon />
        </span>
        <span className="text-base font-semibold tracking-tight">{site.title || "omni server"}</span>
      </button>
      <div className="flex items-center gap-2">
        {isAdmin(user) && (
          <Button
            variant={activeAdmin ? "secondary" : "outline"}
            size="sm"
            onClick={() => nav("/admin/members")}
            className="hidden md:inline-flex"
          >
            后台管理
          </Button>
        )}
        {/* 账号菜单：邮箱 + 积分（0 不显示数字） */}
        <div ref={ref} className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            className="flex cursor-pointer items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm hover:bg-accent"
          >
            <span className="font-medium">{user.email}</span>
            {user.points > 0 && (
              <>
                <span className="text-muted-foreground">·</span>
                <span>积分 {user.points}</span>
              </>
            )}
            <ChevronDownIcon className="text-muted-foreground size-3.5" />
          </button>
          {menuOpen && (
            <div className="bg-card text-card-foreground absolute right-0 top-full z-50 mt-1 w-40 rounded-lg border p-1 shadow-lg">
              {isAdmin(user) && <MenuItem label="后台管理" onClick={() => { setMenuOpen(false); nav("/admin/members") }} />}
              <MenuItem label="修改邮箱" onClick={() => { setMenuOpen(false); setEmailOpen(true) }} />
              <MenuItem label="退出登录" onClick={() => { setMenuOpen(false); onLogout() }} />
            </div>
          )}
          <ChangeEmailDialog open={emailOpen} onOpenChange={setEmailOpen} user={user} onSaved={onUserChanged} />
        </div>
      </div>
      </div>
    </header>
  )
}

function MenuItem({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-accent block w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left text-sm"
    >
      {label}
    </button>
  )
}
