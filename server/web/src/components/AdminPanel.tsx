import { useEffect, useState } from "react"

import { api, type Model, type ModelProvider, type Provider, type ProviderModel, type User, type AdminSettings } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Input, Label, Select, Switch, Textarea } from "@/components/ui/controls"
import { Badge, Card, Table, Tbody, Td, Th, Thead, Tr } from "@/components/ui/layout"
import { Dialog } from "@/components/ui/layout"

export type AdminSection = "members" | "models" | "providers" | "settings"

const SECTIONS: { id: AdminSection; label: string }[] = [
  { id: "members", label: "会员" },
  { id: "models", label: "模型" },
  { id: "providers", label: "供应商" },
  { id: "settings", label: "站点设置" },
]

/** 后台面板：左侧菜单（H5 下横排）+ 右侧内容。section 由 URL 路由（#/admin/xxx）驱动。 */
export function AdminPanel({ section, onNav }: { section: AdminSection; onNav: (s: AdminSection) => void }) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:gap-6">
      <nav className="flex shrink-0 gap-1 overflow-x-auto md:w-40 md:flex-col">
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            onClick={() => onNav(s.id)}
            className={`shrink-0 cursor-pointer rounded-md px-3 py-1.5 text-left text-sm transition-colors ${
              section === s.id
                ? "bg-secondary text-secondary-foreground font-medium"
                : "text-muted-foreground hover:bg-accent"
            }`}
          >
            {s.label}
          </button>
        ))}
      </nav>
      <div className="min-w-0 flex-1">
        {section === "members" && <MembersTab />}
        {section === "providers" && <ProvidersTab />}
        {section === "models" && <ModelsTab />}
        {section === "settings" && <SettingsTab />}
      </div>
    </div>
  )
}

function MembersTab() {
  const [users, setUsers] = useState<User[]>([])
  const [dialog, setDialog] = useState<User | null>(null)
  const [amount, setAmount] = useState("")
  const [reason, setReason] = useState("")
  const [msg, setMsg] = useState("")

  const reload = () => api.adminUsers().then((r) => setUsers(r.users)).catch((e) => setMsg(String(e)))
  useEffect(() => void reload(), [])

  const grant = async () => {
    if (!dialog) return
    const n = Number(amount)
    setMsg("")
    if (!n) return setMsg("填积分（正为赠送、负为回退）")
    try {
      await api.adminCredit(dialog.id, Math.trunc(n), reason || "管理调整")
      setDialog(null)
      setAmount("")
      setReason("")
      reload()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <>
      <Card>
        <Table>
          <Thead>
            <Tr>
              <Th>手机号</Th>
              <Th>积分</Th>
              <Th>管理员</Th>
              <Th>注册时间</Th>
              <Th className="text-right">操作</Th>
            </Tr>
          </Thead>
          <Tbody>
            {users.map((u) => (
              <Tr key={u.id}>
                <Td>{u.phone}</Td>
                <Td className="font-medium">{u.balance}</Td>
                <Td>{u.is_admin ? <Badge>管理员</Badge> : <span className="text-muted-foreground">—</span>}</Td>
                <Td className="text-muted-foreground text-xs">{new Date(u.created_at).toLocaleDateString()}</Td>
                <Td className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button size="sm" variant="outline" onClick={() => setDialog(u)}>
                      积分调整
                    </Button>
                  </div>
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </Card>
      {msg && <p className="text-destructive text-sm">{msg}</p>}

      <Dialog
        open={!!dialog}
        onOpenChange={(o) => !o && setDialog(null)}
        title={dialog ? `积分调整：${dialog.phone}` : ""}
      >
        <div className="flex flex-col gap-3">
          <div className="flex flex-col gap-1">
            <Label>积分变动（赠送填正数，回退填负数）</Label>
            <Input type="number" value={amount} onChange={(e) => setAmount(e.target.value)} placeholder="例：100 / -50" />
          </div>
          <div className="flex flex-col gap-1">
            <Label>说明</Label>
            <Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder="选填" />
          </div>
          <Button onClick={() => void grant()}>确定</Button>
        </div>
      </Dialog>
    </>
  )
}

/** 站点设置：标题/页脚/用户协议 + 每日积分领取开关与数量。 */
function SettingsTab() {
  const [s, setS] = useState<AdminSettings | null>(null)
  const [msg, setMsg] = useState("")
  const [failed, setFailed] = useState(false)
  const [saved, setSaved] = useState(false)

  const load = () => {
    setMsg("")
    setFailed(false)
    api.adminSettings()
      .then(setS)
      .catch((e) => {
        setFailed(true)
        setMsg(e instanceof Error ? e.message : String(e))
      })
  }
  useEffect(load, [])

  const save = async () => {
    if (!s) return
    setMsg("")
    setSaved(false)
    try {
      await api.saveAdminSettings(s)
      setSaved(true)
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }

  if (failed) {
    return (
      <div className="flex flex-col gap-2">
        <p className="text-destructive text-sm">{msg}</p>
        <Button size="sm" variant="outline" onClick={load}>
          重试
        </Button>
      </div>
    )
  }
  if (!s) return <div className="text-muted-foreground text-sm">加载中…</div>

  return (
    <div className="flex flex-col gap-3">
      <Card className="p-4">
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-medium">站点信息</h3>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>网站标题（浏览器标题 + 页头品牌）</Label>
            <Input
              value={s.site_title}
              onChange={(e) => setS({ ...s, site_title: e.target.value })}
              placeholder="如 OmniAgens"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>页脚文案（留空则不显示页脚）</Label>
            <Input
              value={s.footer}
              onChange={(e) => setS({ ...s, footer: e.target.value })}
              placeholder="如 © 2026 OmniAgens"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>用户协议（登录页「用户协议」弹窗内容）</Label>
            <Textarea
              value={s.agreement}
              onChange={(e) => setS({ ...s, agreement: e.target.value })}
              placeholder="留空使用默认文案"
            />
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <div className="flex flex-col gap-3">
          <h3 className="text-sm font-medium">每日积分领取</h3>
          <div className="flex items-center gap-2 text-sm">
            <Switch
              checked={s.daily_credit_enabled}
              onCheckedChange={(v) => setS({ ...s, daily_credit_enabled: v })}
            />
            <Label>开启后，登录用户打开客户端自动领取当日积分</Label>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>每日积分数</Label>
            <Input
              type="number"
              min={0}
              value={String(s.daily_credit_amount)}
              onChange={(e) => setS({ ...s, daily_credit_amount: Number(e.target.value) || 0 })}
              className="max-w-40"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>有效期（天）</Label>
            <Input
              type="number"
              min={0}
              value={String(s.daily_credit_days)}
              onChange={(e) => setS({ ...s, daily_credit_days: Number(e.target.value) || 0 })}
              className="max-w-40"
            />
            <p className="text-muted-foreground text-xs">
              自领取之时计算，到期自动失效，失效后才能再次领取；0 = 不限制，每天可领一次。
            </p>
          </div>
        </div>
      </Card>

      {msg && <p className="text-destructive text-sm">{msg}</p>}
      <div className="flex items-center gap-3">
        <Button onClick={() => void save()}>保存</Button>
        {saved && <span className="text-emerald-600 text-sm">已保存</span>}
      </div>
    </div>
  )
}

function ProvidersTab() {
  const [list, setList] = useState<Provider[]>([])
  const [dialog, setDialog] = useState<Provider | null>(null)
  const [msg, setMsg] = useState("")

  const reload = () => api.providers().then((r) => setList(r.providers)).catch((e) => setMsg(String(e)))
  useEffect(() => void reload(), [])

  const save = async (p: Partial<Provider>) => {
    setMsg("")
    try {
      await api.saveProvider(p)
      setDialog(null)
      reload()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <>
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium">供应商</h3>
        <Button size="sm" onClick={() => setDialog({ id: 0, name: "", type: "openai", base_url: "", api_key: "", enabled: true })}>
          添加供应商
        </Button>
      </div>
      <Card>
        <Table>
          <Thead>
            <Tr>
              <Th>名称</Th>
              <Th>类型</Th>
              <Th>BaseURL</Th>
              <Th>启用</Th>
              <Th className="text-right">操作</Th>
            </Tr>
          </Thead>
          <Tbody>
            {list.map((p) => (
              <Tr key={p.id}>
                <Td>{p.name}</Td>
                <Td>{p.type}</Td>
                <Td className="text-muted-foreground max-w-56 truncate text-xs">{p.base_url || "—"}</Td>
                <Td>
                  <Switch
                    checked={p.enabled}
                    onCheckedChange={(v) => void save({ ...p, enabled: v })}
                  />
                </Td>
                <Td className="text-right">
                  <Button size="sm" variant="outline" onClick={() => setDialog({ ...p })}>
                    编辑
                  </Button>
                </Td>
              </Tr>
            ))}
          </Tbody>
        </Table>
      </Card>
      {msg && <p className="text-destructive text-sm">{msg}</p>}

      {dialog && (
        <ProviderDialog
          p={dialog}
          onClose={() => setDialog(null)}
          onSave={(v) => void save(v)}
          onDelete={dialog.id ? () => void (async () => { await api.deleteProvider(dialog.id); setDialog(null); reload() })() : undefined}
        />
      )}
    </>
  )
}

function ProviderDialog({
  p,
  onSave,
  onDelete,
  onClose,
}: {
  p: Partial<Provider>
  onSave: (p: Partial<Provider>) => void
  onDelete?: () => void
  onClose: () => void
}) {
  const [form, setForm] = useState<Partial<Provider>>({ ...p, models: p.models ?? [] })
  const set = (k: keyof Provider, v: unknown) => setForm((f) => ({ ...f, [k]: v }))
  const setModel = (i: number, k: keyof ProviderModel, v: unknown) =>
    setForm((f) => {
      const models = (f.models ?? []).map((m, idx) => (idx === i ? { ...m, [k]: v } : m))
      return { ...f, models }
    })
  const addModel = () =>
    setForm((f) => ({ ...f, models: [...(f.models ?? []), { key: "", context_window: undefined, max_output_tokens: undefined, input_types: [], reasoning: "default" }] }))
  const delModel = (i: number) =>
    setForm((f) => ({ ...f, models: (f.models ?? []).filter((_, idx) => idx !== i) }))

  return (
    <Dialog open onOpenChange={onClose} title={p.id ? "编辑供应商" : "添加供应商"} className="max-w-2xl">
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label>名称</Label>
          <Input value={form.name ?? ""} onChange={(e) => set("name", e.target.value)} placeholder="如 openai" />
        </div>
        <div className="flex flex-col gap-1">
          <Label>类型</Label>
          <Select value={form.type ?? "openai"} onChange={(e) => set("type", e.target.value)}>
            {["openai", "anthropic", "gemini", "ollama"].map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </Select>
        </div>
        <div className="flex flex-col gap-1">
          <Label>Base URL</Label>
          <Input value={form.base_url ?? ""} onChange={(e) => set("base_url", e.target.value)} placeholder="https://api.openai.com/v1" />
        </div>
        <div className="flex flex-col gap-1">
          <Label>API Key（明文显示）</Label>
          <Input type="text" value={form.api_key ?? ""} onChange={(e) => set("api_key", e.target.value)} placeholder="sk-..." />
        </div>
        <div className="flex items-center gap-2 text-sm">
          <Switch checked={!!form.enabled} onCheckedChange={(v) => set("enabled", v)} />
          <Label>启用</Label>
        </div>

        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between">
            <Label>模型配置</Label>
            <Button size="sm" variant="outline" onClick={addModel}>
              + 添加模型
            </Button>
          </div>
          <div className="omni-scroll flex max-h-64 flex-col gap-2 overflow-y-auto">
            {(form.models ?? []).map((m, i) => (
              <div key={i} className="flex flex-col gap-1.5 rounded-lg border p-2">
                <div className="flex items-center gap-2">
                  <Input
                    className="h-7 flex-1 text-xs"
                    value={m.key ?? ""}
                    placeholder="模型名，如 gpt-4o"
                    onChange={(e) => setModel(i, "key", e.target.value)}
                  />
                  <Button size="sm" variant="ghost" className="h-7 px-2 text-[11px] text-destructive" onClick={() => delModel(i)}>
                    删
                  </Button>
                </div>
                <div className="grid grid-cols-2 gap-1.5">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-muted-foreground text-[11px]">上下文窗口</span>
                    <Input
                      type="number"
                      className="h-7 text-xs"
                      value={m.context_window ?? ""}
                      placeholder="如 128000"
                      onChange={(e) => setModel(i, "context_window", e.target.value ? Number(e.target.value) : undefined)}
                    />
                  </div>
                  <div className="flex flex-col gap-0.5">
                    <span className="text-muted-foreground text-[11px]">最大输出 TOKEN</span>
                    <Input
                      type="number"
                      className="h-7 text-xs"
                      value={m.max_output_tokens ?? ""}
                      placeholder="如 8192"
                      onChange={(e) => setModel(i, "max_output_tokens", e.target.value ? Number(e.target.value) : undefined)}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-1.5">
                  <div className="flex flex-col gap-0.5">
                    <span className="text-muted-foreground text-[11px]">输入类型（逗号分隔）</span>
                    <Input
                      className="h-7 text-xs"
                      value={(m.input_types ?? []).join(",")}
                      placeholder="text,image,pdf"
                      onChange={(e) => setModel(i, "input_types", e.target.value.split(",").map((x) => x.trim()).filter(Boolean))}
                    />
                  </div>
                  <div className="flex flex-col gap-0.5">
                    <span className="text-muted-foreground text-[11px]">推理等级（高级）</span>
                    <Select
                      className="h-7 text-xs"
                      value={m.reasoning ?? "default"}
                      onChange={(e) => setModel(i, "reasoning", e.target.value)}
                    >
                      <option value="default">默认</option>
                      <option value="off">关闭</option>
                      <option value="on">开启</option>
                    </Select>
                  </div>
                </div>
              </div>
            ))}
            {(form.models ?? []).length === 0 && (
              <p className="text-muted-foreground text-xs">还没有模型，点「+ 添加模型」配置。</p>
            )}
          </div>
        </div>

        <div className="flex justify-end gap-2">
          {onDelete && (
            <Button variant="ghost" className="text-destructive" onClick={onDelete}>
              删除
            </Button>
          )}
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button disabled={!(form.name ?? "").trim()} onClick={() => onSave(form)}>
            保存
          </Button>
        </div>
      </div>
    </Dialog>
  )
}

function ModelsTab() {
  const [models, setModels] = useState<Model[]>([])
  const [mps, setMps] = useState<ModelProvider[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [dialog, setDialog] = useState<Partial<Model> | null>(null)
  const [bindProvider, setBindProvider] = useState(0)
  const [msg, setMsg] = useState("")

  const reload = () =>
    api.models().then((r) => { setModels(r.models); setMps(r.model_providers); setProviders(r.providers) }).catch((e) => setMsg(String(e)))
  useEffect(() => void reload(), [])

  const save = async (m: Partial<Model>) => {
    setMsg("")
    try {
      await api.saveModel({ ...m, provider_id: bindProvider })
      setDialog(null)
      setBindProvider(0)
      reload()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }
  const toggleModel = async (m: Model) => {
    try {
      await api.saveModel({ ...m, enabled: !m.enabled })
      reload()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }
  const toggleMp = async (modelID: number, providerID: number, enabled: boolean) => {
    try {
      await api.toggleModelProvider(modelID, providerID, enabled)
      reload()
    } catch (e) {
      setMsg(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <>
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm font-medium">模型</h3>
        <Button size="sm" onClick={() => setDialog({ id: 0, key: "", builtin: false, enabled: true, multiplier: 1, free: false })}>
          添加模型
        </Button>
      </div>
      <p className="text-muted-foreground mb-2 text-xs">
        供应商「模型配置」里填的模型名，保存供应商时会自动同步到这里并完成绑定，无需手动添加。
      </p>
      <div className="flex flex-col gap-2">
        {models.map((m) => (
          <Card key={m.id} className="p-3">
            <div className="flex items-center gap-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{m.key}</span>
                  {m.builtin && <Badge variant="secondary">内置</Badge>}
                  {m.free ? (
                    <span className="flex items-center gap-1">
                      <Badge variant="destructive">0x 限免</Badge>
                      <span className="text-muted-foreground text-xs line-through">×{m.multiplier}</span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground text-xs">×{m.multiplier}</span>
                  )}
                </div>
                <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-2 text-xs">
                  <span>启用</span>
                  <Switch checked={m.enabled} onCheckedChange={() => void toggleModel(m)} />
                  {mps.filter((x) => x.model_id === m.id).length > 0 && (
                    <span className="text-muted-foreground/80">供应商：</span>
                  )}
                  {mps.filter((x) => x.model_id === m.id).map((mp) => {
                    const p = providers.find((x) => x.id === mp.provider_id)
                    return (
                      <span key={mp.provider_id} className="flex items-center gap-1">
                        <span className={mp.enabled ? "" : "text-muted-foreground/40 line-through"}>{p?.name}</span>
                        <Switch
                          checked={mp.enabled}
                          onCheckedChange={(v) => void toggleMp(m.id, mp.provider_id, v)}
                        />
                      </span>
                    )
                  })}
                </div>
              </div>
              <Button size="sm" variant="outline" onClick={() => setDialog({ ...m })}>
                编辑
              </Button>
            </div>
          </Card>
        ))}
      </div>
      {msg && <p className="text-destructive text-sm">{msg}</p>}

      {dialog && (
        <Dialog open onOpenChange={() => setDialog(null)} title={dialog.id ? "编辑模型" : "添加模型"}>
          <div className="flex flex-col gap-3">
            <div className="flex flex-col gap-1">
              <Label>模型名(key)</Label>
              <Input value={dialog.key ?? ""} onChange={(e) => setDialog({ ...dialog, key: e.target.value })} />
            </div>
            <div className="flex flex-col gap-1">
              <Label>倍率</Label>
              <Input type="number" step="0.1" value={dialog.multiplier} onChange={(e) => setDialog({ ...dialog, multiplier: Number(e.target.value) || 1 })} />
            </div>
            <div className="flex items-center gap-2 text-sm">
              <Switch checked={!!dialog.free} onCheckedChange={(v) => setDialog({ ...dialog, free: v })} />
              <Label>限免（不扣积分，显示 0x）</Label>
            </div>
            {!dialog.builtin && (
              <div className="flex flex-col gap-1">
                <Label>绑定供应商（此项用于首次创建）」</Label>
                <Select value={bindProvider} onChange={(e) => setBindProvider(Number(e.target.value))}>
                  <option value={0}>不绑定</option>
                  {providers.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}
                    </option>
                  ))}
                </Select>
              </div>
            )}
            <div className="flex justify-end gap-2">
              {dialog.id ? (
                <Button
                  variant="ghost"
                  className="text-destructive"
                  onClick={() => void (async () => { await api.deleteModel(dialog.id!); setDialog(null); reload() })()}
                >
                  删除
                </Button>
              ) : null}
              <Button variant="outline" onClick={() => setDialog(null)}>
                取消
              </Button>
              <Button onClick={() => void save(dialog)}>保存</Button>
            </div>
          </div>
        </Dialog>
      )}
    </>
  )
}