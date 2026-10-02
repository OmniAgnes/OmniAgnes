import { useEffect, useRef, useState } from "react"
import { ChevronDownIcon } from "lucide-react"

import { AdminPanel } from "@/components/AdminPanel"
import { ChangePhoneDialog } from "@/components/ChangePhoneDialog"
import { LoginPanel } from "@/components/LoginPanel"
import { MePanel } from "@/components/MePanel"
import { Button } from "@/components/ui/button"
import { api, getToken, setToken, type Site, type User } from "@/lib/api"

/** hash 路由：#/me、#/admin、#/admin/members|models|providers|settings。不同界面 URL 不同，可分享、刷新不 404。 */
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

type AdminSection = "members" | "models" | "providers" | "settings"
function sectionOf(route: string): AdminSection | null {
  if (!route.startsWith("/admin")) return null
  const seg = route.split("/")[2]
  return seg === "models" || seg === "providers" || seg === "settings" ? (seg as AdminSection) : "members"
}

export function App() {
  const route = useHashRoute()
  const [user, setUser] = useState<User | null>(null)
  const [records, setRecords] = useState<any[]>([])
  const [site, setSite] = useState<Site>({ title: "", footer: "", agreement: "", test_mode: false })
  const [booting, setBooting] = useState(true)

  const loadMe = async () => {
    if (!getToken()) {
      setUser(null)
      return
    }
    try {
      const r = await api.me()
      setUser(r.user)
      setRecords(r.records)
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
    nav("/me")
  }

  const adminSection = sectionOf(route)

  // 路由守卫：非管理员访问后台回我的页；未知路由回我的页
  useEffect(() => {
    if (booting || !user) return
    if (adminSection && !user.is_admin) nav("/me")
    else if (!adminSection && route !== "/me") nav("/me")
  }, [user, route, adminSection, booting])

  if (booting) {
    return <div className="text-muted-foreground flex min-h-screen items-center justify-center text-sm">加载中…</div>
  }

  // 未登录：登录页（任何 hash 都显示登录）
  if (!user) {
    return (
      <>
        <LoginPanel site={site} onLogin={(u) => { setUser(u); nav("/me") }} />
        {site.footer && <Footer text={site.footer} />}
      </>
    )
  }

  const showAdmin = adminSection !== null && user.is_admin

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
          <MePanel user={user} records={records} onLogout={() => void logout()} />
        )}
      </main>
      {site.footer && <Footer text={site.footer} />}
    </div>
  )
}

function Footer({ text }: { text: string }) {
  return <div className="text-muted-foreground border-t px-4 py-3 text-center text-xs">{text}</div>
}

/** 顶栏：左侧站点标题（跳我的页）；右侧管理员「后台管理」按钮 + 账号菜单（改手机号/退出）。 */
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
  const [phoneOpen, setPhoneOpen] = useState(false)
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
    <header className="border-background/80 sticky top-0 z-40 border-b bg-background">
      {/* 内容宽度与下方 main 一致（max-w-5xl 居中）；
          注意：这里不能用 backdrop-blur——它会成为 fixed 元素的包含块，
          导致页面内的弹窗（如修改手机号）fixed 定位参照 header 而跑到界面上方 */}
      <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-2 px-3 py-2.5 md:px-6 md:py-3">
      <button type="button" className="cursor-pointer text-base font-semibold tracking-tight" onClick={() => nav("/me")}>
        {site.title || "omni server"}
      </button>
      <div className="flex items-center gap-2">
        {user.is_admin && (
          <Button
            variant={activeAdmin ? "secondary" : "outline"}
            size="sm"
            onClick={() => nav("/admin/members")}
            className="hidden md:inline-flex"
          >
            后台管理
          </Button>
        )}
        {/* 账号菜单：手机号 + 积分（0 不显示数字） */}
        <div ref={ref} className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            className="flex cursor-pointer items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm hover:bg-accent"
          >
            <span className="font-medium">{user.phone}</span>
            {user.balance > 0 && (
              <>
                <span className="text-muted-foreground">·</span>
                <span>积分 {user.balance}</span>
              </>
            )}
            <ChevronDownIcon className="text-muted-foreground size-3.5" />
          </button>
          {menuOpen && (
            <div className="bg-card text-card-foreground absolute right-0 top-full z-50 mt-1 w-40 rounded-lg border p-1 shadow-lg">
              {user.is_admin && <MenuItem label="后台管理" onClick={() => { setMenuOpen(false); nav("/admin/members") }} />}
              <MenuItem label="修改手机号" onClick={() => { setMenuOpen(false); setPhoneOpen(true) }} />
              <MenuItem label="退出登录" onClick={() => { setMenuOpen(false); onLogout() }} />
            </div>
          )}
          <ChangePhoneDialog open={phoneOpen} onOpenChange={setPhoneOpen} user={user} onSaved={onUserChanged} />
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
