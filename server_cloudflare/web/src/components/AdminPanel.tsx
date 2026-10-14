import { useEffect, useState } from "react"
import { ActivityIcon, CpuIcon, MailIcon, ServerIcon, UsersIcon } from "lucide-react"

import { api, type AiModel, type Member, type Upstream } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label, Switch } from "@/components/ui/controls"
import { Badge, Card, CardContent, CardHeader, CardTitle, EmptyRow, Pager, PanelNav, StatCard, Table, Tbody, Td, Th, Thead, Tr } from "@/components/ui/layout"
import { Dialog } from "@/components/ui/layout"

export type AdminSection = "members" | "usage" | "models" | "upstreams" | "mail"

const SECTIONS: { id: AdminSection; label: string; icon: React.ReactNode }[] = [
  { id: "members", label: "会员", icon: <UsersIcon /> },
  { id: "usage", label: "使用量", icon: <ActivityIcon /> },
  { id: "upstreams", label: "上游", icon: <ServerIcon /> },
  { id: "models", label: "模型", icon: <CpuIcon /> },
  { id: "mail", label: "邮件设置", icon: <MailIcon /> },
]

/** 后台面板：左侧菜单 + 右侧内容。section 由 URL 路由（#/admin/xxx）驱动。 */
export function AdminPanel({ section, onNav }: { section: AdminSection; onNav: (s: AdminSection) => void }) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:gap-6">
      <PanelNav items={SECTIONS} active={section} onNav={(id) => onNav(id as AdminSection)} />
      <div className="min-w-0 flex-1">
        {section === "members" && <MembersTab />}
        {section === "usage" && <UsageTab />}
        {section === "upstreams" && <UpstreamsTab />}
        {section === "models" && <ModelsTab />}
        {section === "mail" && <MailTab />}
      </div>
    </div>
  )
}

/** 会员：列表 + 调整积分/状态。 */
function MembersTab() {
  const [q, setQ] = useState("")
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<{ items: Member[]; total: number; page: number } | null>(null)
  const [msg, setMsg] = useState("")
  const [edit, setEdit] = useState<Member | null>(null)

  const load = () => {
    setMsg("")
    api
      .members(q, page, 20)
      .then((r) => setRows({ items: r.items, total: r.total, page: r.page }))
      .catch((e) => setMsg(e instanceof Error ? e.message : String(e)))
  }
  useEffect(load, [q, page])

  const list = rows?.items ?? []
  const total = rows?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder="按邮箱/账号名搜索" className="max-w-xs" />
        <Button variant="outline" onClick={() => { setPage(1); load() }}>搜索</Button>
      </div>
      {msg && <p className="text-destructive text-sm">{msg}</p>}
      <Card>
        <CardContent>
          <Table>
            <Thead>
              <Tr>
                <Th>账号</Th>
                <Th>邮箱</Th>
                <Th>角色</Th>
                <Th>积分</Th>
                <Th>状态</Th>
                <Th>操作</Th>
              </Tr>
            </Thead>
            <Tbody>
              {list.map((u) => (
                <Tr key={u.id}>
                  <Td className="font-medium">{u.name || "—"}</Td>
                  <Td className="text-xs">{u.email}</Td>
                  <Td>{u.role === "admin" ? <Badge>管理员</Badge> : <span className="text-muted-foreground">会员</span>}</Td>
                  <Td className="tabular-nums">{u.points}</Td>
                  <Td>{u.status === "active" ? <span className="text-emerald-600">正常</span> : <span className="text-destructive">禁用</span>}</Td>
                  <Td>
                    <Button variant="outline" size="sm" onClick={() => setEdit(u)}>调整</Button>
                  </Td>
                </Tr>
              ))}
              {list.length === 0 && <EmptyRow colSpan={6} label="暂无会员" />}
            </Tbody>
          </Table>
          {pages > 1 && <Pager page={page} pageSize={20} total={total} onChange={setPage} />}
        </CardContent>
      </Card>
      {edit && <EditMemberDialog member={edit} onClose={() => setEdit(null)} onSaved={() => { setEdit(null); load() }} />}
    </div>
  )
}

/** 调整会员：积分 + 状态。 */
function EditMemberDialog({ member, onClose, onSaved }: { member: Member; onClose: () => void; onSaved: () => void }) {
  const [points, setPoints] = useState("")
  const [remark, setRemark] = useState("")
  const [status, setStatus] = useState(member.status)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState("")

  const submit = async () => {
    setErr("")
    const delta = Number(points) || 0
    if (delta === 0 && status === member.status) return setErr("没有需要保存的改动")
    setBusy(true)
    try {
      await api.updateMember(member.id, { points: delta, remark: remark.trim() || undefined, status })
      onSaved()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`调整会员：${member.email}`}>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label>积分变动（正=发放，负=扣除，0=不变）</Label>
          <Input type="number" value={points} onChange={(e) => setPoints(e.target.value)} placeholder="如 100 或 -50" />
        </div>
        <div className="flex flex-col gap-1">
          <Label>备注</Label>
          <Input value={remark} onChange={(e) => setRemark(e.target.value)} placeholder="调整原因（选填）" />
        </div>
        <div className="flex items-center gap-2">
          <Label>账号状态</Label>
          <Switch checked={status === "active"} onCheckedChange={(v) => setStatus(v ? "active" : "disabled")} />
          <span className="text-muted-foreground text-xs">{status === "active" ? "正常" : "禁用"}</span>
        </div>
        {err && <p className="text-destructive text-sm">{err}</p>}
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button disabled={busy} onClick={() => void submit()}>{busy ? "提交中…" : "保存"}</Button>
        </div>
      </div>
    </Dialog>
  )
}

/** 使用量：总览统计。 */
function UsageTab() {
  const [ov, setOv] = useState<Record<string, any> | null>(null)
  const [msg, setMsg] = useState("")

  useEffect(() => {
    api
      .usageOverview()
      .then(setOv)
      .catch((e) => setMsg(e instanceof Error ? e.message : String(e)))
  }, [])

  if (msg) return <p className="text-destructive text-sm">{msg}</p>
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <StatCard label="调用次数" value={ov?.calls ?? 0} />
        <StatCard label="Token" value={ov?.tokens ?? 0} />
        <StatCard label="消耗积分" value={ov?.points ?? 0} />
        <StatCard label="活跃用户" value={ov?.users ?? 0} />
        <StatCard label="设备" value={ov?.devices ?? 0} />
      </div>
      {ov?.models && ov.models.length > 0 && (
        <Card>
          <CardHeader><CardTitle>按模型</CardTitle></CardHeader>
          <CardContent>
            <Table>
              <Thead>
                <Tr><Th>模型</Th><Th>调用</Th><Th>Token</Th><Th>积分</Th></Tr>
              </Thead>
              <Tbody>
                {ov.models.map((m: any) => (
                  <Tr key={m.label}>
                    <Td className="text-xs">{m.label}</Td>
                    <Td className="tabular-nums">{m.calls}</Td>
                    <Td className="tabular-nums">{m.tokens}</Td>
                    <Td className="tabular-nums">{m.points}</Td>
                  </Tr>
                ))}
              </Tbody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

/** 上游：列表 + 启用/禁用 + 删除。 */
function UpstreamsTab() {
  const [rows, setRows] = useState<Upstream[] | null>(null)
  const [msg, setMsg] = useState("")

  const load = () => {
    setMsg("")
    api
      .upstreams()
      .then((r) => setRows(r.items))
      .catch((e) => setMsg(e instanceof Error ? e.message : String(e)))
  }
  useEffect(load, [])

  return (
    <div className="flex flex-col gap-4">
      {msg && <p className="text-destructive text-sm">{msg}</p>}
      <Card>
        <CardContent>
          <Table>
            <Thead>
              <Tr><Th>名称</Th><Th>驱动</Th><Th>地址</Th><Th>状态</Th><Th>模型数</Th><Th>操作</Th></Tr>
            </Thead>
            <Tbody>
              {(rows ?? []).map((u) => (
                <Tr key={u.id}>
                  <Td className="font-medium">{u.name}</Td>
                  <Td className="text-xs">{u.driver}</Td>
                  <Td className="text-muted-foreground text-xs">{u.base_url}</Td>
                  <Td>{u.enabled ? <span className="text-emerald-600">启用</span> : <span className="text-muted-foreground">停用</span>}</Td>
                  <Td className="tabular-nums">{u.models.length}</Td>
                  <Td>
                    <Button variant="outline" size="sm" onClick={() => api.updateUpstream(u.id, { enabled: !u.enabled }).then(load).catch((e) => setMsg(e.message))}>
                      {u.enabled ? "停用" : "启用"}
                    </Button>
                  </Td>
                </Tr>
              ))}
              {(rows ?? []).length === 0 && <EmptyRow colSpan={6} label="暂无上游" />}
            </Tbody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}

/** 模型：列表 + 启用/禁用。 */
function ModelsTab() {
  const [rows, setRows] = useState<AiModel[] | null>(null)
  const [msg, setMsg] = useState("")

  const load = () => {
    setMsg("")
    api
      .models()
      .then((r) => setRows(r.items))
      .catch((e) => setMsg(e instanceof Error ? e.message : String(e)))
  }
  useEffect(load, [])

  return (
    <div className="flex flex-col gap-4">
      {msg && <p className="text-destructive text-sm">{msg}</p>}
      <Card>
        <CardContent>
          <Table>
            <Thead>
              <Tr><Th>模型</Th><Th>内置</Th><Th>倍率</Th><Th>限免</Th><Th>健康</Th><Th>状态</Th><Th>操作</Th></Tr>
            </Thead>
            <Tbody>
              {(rows ?? []).map((m) => (
                <Tr key={m.id}>
                  <Td className="font-medium">{m.name}</Td>
                  <Td>{m.builtin ? <Badge variant="outline">内置</Badge> : "—"}</Td>
                  <Td className="tabular-nums">{m.effective_ratio}</Td>
                  <Td>{m.free ? <span className="text-emerald-600">限免</span> : "—"}</Td>
                  <Td>
                    <span className={m.health === "ok" ? "text-emerald-600" : m.health === "error" ? "text-destructive" : "text-muted-foreground"}>
                      {m.health === "ok" ? "正常" : m.health === "error" ? "异常" : m.health === "unknown" ? "未知" : "未匹配"}
                    </span>
                  </Td>
                  <Td>{m.enabled ? <span className="text-emerald-600">启用</span> : <span className="text-muted-foreground">停用</span>}</Td>
                  <Td>
                    <Button variant="outline" size="sm" onClick={() => api.updateModel(m.name, { enabled: !m.enabled }).then(load).catch((e) => setMsg(e.message))}>
                      {m.enabled ? "停用" : "启用"}
                    </Button>
                  </Td>
                </Tr>
              ))}
              {(rows ?? []).length === 0 && <EmptyRow colSpan={7} label="暂无模型" />}
            </Tbody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}

/** 邮件设置：SMTP 配置 + 测试。 */
function MailTab() {
  const [cfg, setCfg] = useState<{ host: string; port: string; user: string; from: string; secure: string; has_pass: boolean }>({ host: "", port: "465", user: "", from: "", secure: "ssl", has_pass: false })
  const [pass, setPass] = useState("")
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState("")

  useEffect(() => {
    api
      .mailConfig()
      .then((c) => setCfg({ host: c.host, port: String(c.port), user: c.user, from: c.from, secure: c.secure ?? "ssl", has_pass: c.has_pass }))
      .catch((e) => setMsg(e instanceof Error ? e.message : String(e)))
  }, [])

  const save = async () => {
    setMsg("")
    setBusy(true)
    try {
      await api.saveMailConfig({
        host: cfg.host,
        port: Number(cfg.port) || 465,
        user: cfg.user,
        from: cfg.from,
        secure: cfg.secure as "ssl" | "starttls",
        pass: pass || undefined,
      })
      setPass("")
      setMsg("已保存")
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader><CardTitle>SMTP 配置</CardTitle></CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label>SMTP 服务器</Label>
            <Input value={cfg.host} onChange={(e) => setCfg({ ...cfg, host: e.target.value })} placeholder="smtp.example.com" />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1">
              <Label>端口</Label>
              <Input value={cfg.port} onChange={(e) => setCfg({ ...cfg, port: e.target.value })} placeholder="465" />
            </div>
            <div className="flex flex-col gap-1">
              <Label>加密方式</Label>
              <select className="border-input bg-background h-9 rounded-md border px-3 text-sm" value={cfg.secure} onChange={(e) => setCfg({ ...cfg, secure: e.target.value })}>
                <option value="ssl">SSL</option>
                <option value="starttls">STARTTLS</option>
              </select>
            </div>
          </div>
          <div className="flex flex-col gap-1">
            <Label>发件邮箱（SMTP 账号）</Label>
            <Input value={cfg.user} onChange={(e) => setCfg({ ...cfg, user: e.target.value })} placeholder="noreply@example.com" />
          </div>
          <div className="flex flex-col gap-1">
            <Label>发件人显示</Label>
            <Input value={cfg.from} onChange={(e) => setCfg({ ...cfg, from: e.target.value })} placeholder="OmniAgnes <noreply@example.com>" />
          </div>
          <div className="flex flex-col gap-1">
            <Label>SMTP 密码{cfg.has_pass ? "（已设置，留空保持不变）" : ""}</Label>
            <Input type="password" value={pass} onChange={(e) => setPass(e.target.value)} placeholder="留空则不修改" />
          </div>
          {msg && <p className={msg === "已保存" ? "text-emerald-600 text-sm" : "text-destructive text-sm"}>{msg}</p>}
          <div className="flex justify-end">
            <Button disabled={busy} onClick={() => void save()}>{busy ? "保存中…" : "保存"}</Button>
          </div>
        </CardContent>
      </Card>
      <p className="text-muted-foreground text-xs">
        配置好 SMTP 后，登录验证码将真实发送到邮箱；未配置时验证码任意通过（便于首次部署引导）。
      </p>
    </div>
  )
}
