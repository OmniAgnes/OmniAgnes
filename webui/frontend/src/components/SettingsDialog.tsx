import { useEffect, useState } from "react"
import {
  BlocksIcon,
  BrainIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  CpuIcon,
  ChevronRightIcon,
  PlusIcon,
  SunMoonIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { MCPStatus, ProviderConfig, SkillInfo, StateInfo, Theme, ThemeColor } from "@/types"

const SECTIONS = [
  { id: "model", label: "模型", icon: CpuIcon },
  { id: "theme", label: "主题", icon: SunMoonIcon },
  { id: "memory", label: "记忆", icon: BrainIcon },
  { id: "plugins", label: "插件", icon: BlocksIcon },
] as const

type SectionId = (typeof SECTIONS)[number]["id"]

const PROVIDER_TYPES = [
  { value: "openai", label: "OpenAI 兼容" },
  { value: "anthropic", label: "Anthropic" },
  { value: "gemini", label: "Gemini" },
  { value: "ollama", label: "Ollama" },
] as const

const THEME_COLORS = [
  { id: "zinc", label: "石墨", light: "#18181b", dark: "#3f3f46" },
  { id: "violet", label: "紫罗兰", light: "#7c5cff", dark: "#a78bfa" },
  { id: "blue", label: "海洋", light: "#2f7cd6", dark: "#60a5fa" },
  { id: "emerald", label: "翡翠", light: "#0e9f6e", dark: "#34d399" },
  { id: "amber", label: "琥珀", light: "#d97706", dark: "#fbbf24" },
  { id: "rose", label: "玫瑰", light: "#e11d48", dark: "#fb7185" },
] as const satisfies ReadonlyArray<{ id: ThemeColor; label: string; light: string; dark: string }>

const INPUT_TYPES = [
  { value: "text", label: "文字" },
  { value: "image", label: "图片" },
  { value: "video", label: "视频" },
  { value: "pdf", label: "PDF" },
] as const

const REASONING_LEVELS = [
  { value: "default", label: "默认" },
  { value: "off", label: "关闭" },
  { value: "on", label: "开启" },
] as const

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  state: StateInfo | null
  onStateChange: (s: StateInfo) => void
}

export function SettingsDialog({ open, onOpenChange, state, onStateChange }: SettingsDialogProps) {
  const [section, setSection] = useState<SectionId>("model")
  const [providers, setProviders] = useState<ProviderConfig[] | null>(null)
  const [current, setCurrent] = useState(state?.model ?? "")
  const [theme, setTheme] = useState<Theme>(state?.theme ?? "dark")
  const [themeColor, setThemeColor] = useState<ThemeColor>(state?.theme_color ?? "zinc")
  const [memory, setMemory] = useState("")
  const [memorySaved, setMemorySaved] = useState(false)
  const [mcp, setMcp] = useState<MCPStatus[]>([])
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [pluginTab, setPluginTab] = useState<"skills" | "mcp">("skills")
  const [err, setErr] = useState("")
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    api.providers().then((r) => setProviders(r.providers)).catch(() => setProviders([]))
    api.settings().then((r) => {
      setTheme(r.theme)
      setCurrent(r.model)
      if (r.theme_color) setThemeColor(r.theme_color)
    }).catch(() => {})
    api.memory().then((r) => setMemory(r.content)).catch(() => {})
    api.mcp().then((r) => setMcp(r.servers ?? [])).catch(() => {})
    api.skills().then((r) => setSkills(r.skills ?? [])).catch(() => {})
  }, [open])

  const allModels = (providers ?? []).flatMap((p) => (p.models ?? []).map((m) => ({ model: m, type: p.type, provider: p.name })))

  const saveProviders = async () => {
    if (!providers) return
    setSaving(true)
    setErr("")
    try {
      const r = await api.saveProviders(providers)
      setProviders(r.providers)
      setCurrent(r.current)
      if (state) onStateChange({ ...state, model: r.current, configured: r.providers.length > 0 })
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const saveTheme = async (t: Theme) => {
    setTheme(t)
    const r = await api.saveSettings({ theme: t })
    if (state) onStateChange({ ...state, theme: r.theme })
  }

  const saveThemeColor = async (c: ThemeColor) => {
    setThemeColor(c)
    const r = await api.saveSettings({ theme_color: c })
    if (state) onStateChange({ ...state, theme_color: r.theme_color })
  }

  const saveMemory = async () => {
    await api.saveMemory(memory)
    setMemorySaved(true)
    setTimeout(() => setMemorySaved(false), 2000)
  }

  const patchProvider = (i: number, patch: Partial<ProviderConfig>) => {
    setProviders((prev) => prev?.map((p, j) => (j === i ? { ...p, ...patch } : p)) ?? null)
  }

  const addProvider = () => {
    setProviders((prev) => [...(prev ?? []), { type: "openai", models: [] }])
  }

  const removeProvider = (i: number) => {
    setProviders((prev) => prev?.filter((_, j) => j !== i) ?? null)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className="grid-rows-[auto_minmax(0,1fr)] h-[85dvh] max-h-[760px] w-full gap-0 overflow-hidden p-0 sm:max-w-4xl md:h-[82vh]"
        showCloseButton
      >
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle>设置</DialogTitle>
        </DialogHeader>

        <div className="grid min-h-0 h-full grid-cols-1 md:grid-cols-[168px_1fr]">
          <nav className="border-b px-2 py-2 md:border-r md:px-2 md:py-3">
            <div className="flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
              {SECTIONS.map((s) => (
                <button
                  key={s.id}
                  type="button"
                  onClick={() => setSection(s.id)}
                  className={cn(
                    "flex shrink-0 items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm cursor-pointer",
                    section === s.id
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                  )}
                >
                  <s.icon className="size-4 shrink-0" />
                  <span className="whitespace-nowrap">{s.label}</span>
                </button>
              ))}
            </div>
          </nav>

          <ScrollArea className="h-full">
            <div className="flex flex-col gap-4 p-5">
              {section === "model" && (
                <div className="flex flex-col gap-4">
                  <div className="flex items-center justify-between">
                    <Label>供应商</Label>
                    <div className="flex gap-2">
                      <Button variant="outline" size="sm" onClick={addProvider}>
                        <PlusIcon /> 添加
                      </Button>
                      <Button size="sm" disabled={saving || !providers} onClick={() => void saveProviders()}>
                        保存
                      </Button>
                    </div>
                  </div>

                  {err && <div className="text-destructive text-sm">{err}</div>}

                  {allModels.length > 0 && (
                    <div className="flex flex-col gap-2">
                      <Label className="text-muted-foreground text-xs">当前模型</Label>
                      <Select
                        value={current || undefined}
                        onValueChange={(v) => {
                          setCurrent(v)
                          void api.saveSettings({ model: v }).then(() => {
                            if (state) onStateChange({ ...state, model: v })
                          })
                        }}
                      >
                        <SelectTrigger className="w-full md:w-80">
                          <SelectValue placeholder="选择模型" />
                        </SelectTrigger>
                        <SelectContent>
                          {allModels.map((m) => (
                            <SelectItem key={m.model} value={m.model}>
                              {m.model}
                              {m.provider ? ` · ${m.provider}` : ""}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  <Separator />

                  {(providers ?? []).length === 0 && (
                    <div className="text-muted-foreground text-sm">暂无供应商</div>
                  )}

                  {(providers ?? []).map((p, i) => (
                    <ProviderCard
                      key={i}
                      provider={p}
                      onChange={(patch) => patchProvider(i, patch)}
                      onRemove={() => removeProvider(i)}
                    />
                  ))}
                </div>
              )}

              {section === "theme" && (
                <div className="flex flex-col gap-4">
                  <div className="flex flex-col gap-2">
                    <Label>外观模式</Label>
                    <div className="grid grid-cols-3 gap-2">
                      {(
                        [
                          { id: "dark", label: "深色" },
                          { id: "light", label: "浅色" },
                          { id: "system", label: "跟随系统" },
                        ] as const
                      ).map((t) => (
                        <button
                          key={t.id}
                          type="button"
                          onClick={() => void saveTheme(t.id)}
                          className={cn(
                            "flex flex-col items-center gap-2 rounded-lg border p-3 text-sm cursor-pointer transition-colors",
                            theme === t.id ? "border-ring ring-ring/30 ring-2" : "hover:bg-accent",
                          )}
                        >
                          <ThemePreview id={t.id} />
                          {t.label}
                        </button>
                      ))}
                    </div>
                  </div>

                  <div className="flex flex-col gap-2">
                    <Label>主题风格</Label>
                    <div className="grid grid-cols-3 gap-2 sm:grid-cols-6">
                      {THEME_COLORS.map((c) => (
                        <button
                          key={c.id}
                          type="button"
                          onClick={() => void saveThemeColor(c.id)}
                          className={cn(
                            "flex flex-col items-center gap-1.5 rounded-lg border p-2.5 text-xs cursor-pointer transition-colors",
                            themeColor === c.id ? "border-ring ring-ring/30 ring-2" : "hover:bg-accent",
                          )}
                        >
                          <span
                            className="size-6 rounded-full border border-black/10 shadow-sm"
                            style={{ background: `linear-gradient(135deg, ${c.light} 55%, ${c.dark} 55%)` }}
                          />
                          {c.label}
                        </button>
                      ))}
                    </div>
                  </div>
                </div>
              )}

              {section === "memory" && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between">
                    <Label>全局记忆</Label>
                    {memorySaved && <span className="text-muted-foreground text-xs">已保存</span>}
                  </div>
                  <Textarea
                    value={memory}
                    onChange={(e) => setMemory(e.target.value)}
                    className="min-h-44 font-mono text-xs"
                  />
                  <div className="flex justify-end">
                    <Button size="sm" variant="secondary" onClick={() => void saveMemory()}>
                      保存
                    </Button>
                  </div>
                </div>
              )}

              {section === "plugins" && (
                <div className="flex flex-col gap-3">
                  <div className="bg-muted/50 flex w-fit rounded-md p-0.5">
                    {(
                      [
                        { id: "skills", label: `技能${skills.length ? ` ${skills.length}` : ""}` },
                        { id: "mcp", label: `MCP${mcp.length ? ` ${mcp.length}` : ""}` },
                      ] as const
                    ).map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        onClick={() => setPluginTab(t.id)}
                        className={cn(
                          "rounded-[5px] px-3 py-1.5 text-sm cursor-pointer transition-colors",
                          pluginTab === t.id
                            ? "bg-background text-foreground shadow-xs"
                            : "text-muted-foreground hover:text-foreground",
                        )}
                      >
                        {t.label}
                      </button>
                    ))}
                  </div>

                  {pluginTab === "skills" && (
                    <div className="flex flex-col gap-2">
                      {skills.length === 0 ? (
                        <div className="text-muted-foreground text-sm">未安装技能</div>
                      ) : (
                        skills.map((s) => (
                          <div key={s.name} className="rounded-md border px-3 py-2 text-sm">
                            <div className="flex items-center gap-2">
                              <span className="font-medium">/{s.name}</span>
                              <Badge variant="outline">{s.source === "project" ? "项目" : "全局"}</Badge>
                            </div>
                            <div className="text-muted-foreground text-xs">{s.description}</div>
                          </div>
                        ))
                      )}
                    </div>
                  )}

                  {pluginTab === "mcp" && (
                    <div className="flex flex-col gap-2">
                      {mcp.length === 0 ? (
                        <div className="text-muted-foreground text-sm">未配置 MCP 服务器</div>
                      ) : (
                        mcp.map((s) => (
                          <div
                            key={s.name}
                            className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm"
                          >
                            {s.connected ? (
                              <CheckCircle2Icon className="size-4 shrink-0 text-green-600" />
                            ) : (
                              <CircleAlertIcon className="text-destructive size-4 shrink-0" />
                            )}
                            <span className="font-medium">{s.name}</span>
                            <Badge variant="secondary">{s.kind}</Badge>
                            <span className="text-muted-foreground w-full truncate font-mono text-xs sm:w-auto sm:flex-1">
                              {s.target}
                              {s.connected ? ` · ${s.tool_count}` : s.error ? ` · ${s.error}` : ""}
                            </span>
                          </div>
                        ))
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          </ScrollArea>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ProviderCard({
  provider,
  onChange,
  onRemove,
}: {
  provider: ProviderConfig
  onChange: (patch: Partial<ProviderConfig>) => void
  onRemove: () => void
}) {
  const [modelInput, setModelInput] = useState("")

  const addModel = () => {
    const m = modelInput.trim()
    if (!m) return
    const models = [...(provider.models ?? []), m]
    onChange({ models, model: models[0] })
    setModelInput("")
  }

  const removeModel = (m: string) => {
    const models = (provider.models ?? []).filter((x) => x !== m)
    onChange({ models, model: models[0] })
  }

  return (
    <div className="flex flex-col gap-3 rounded-lg border p-4">
      <div className="flex items-center justify-between">
        <Input
          value={provider.name ?? ""}
          onChange={(e) => onChange({ name: e.target.value })}
          placeholder="供应商名称"
          className="max-w-56 border-transparent bg-transparent px-1 font-medium shadow-none focus-visible:border-input"
        />
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground hover:text-destructive"
          onClick={onRemove}
        >
          <Trash2Icon />
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <div className="flex flex-col gap-1.5">
          <Label className="text-muted-foreground text-xs">类型</Label>
          <Select value={provider.type} onValueChange={(v) => onChange({ type: v })}>
            <SelectTrigger size="sm" className="w-full">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PROVIDER_TYPES.map((t) => (
                <SelectItem key={t.value} value={t.value}>
                  {t.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label className="text-muted-foreground text-xs">API Key</Label>
          <Input
            type="password"
            value={provider.api_key ?? ""}
            onChange={(e) => onChange({ api_key: e.target.value })}
            placeholder="sk-…"
          />
        </div>
        <div className="flex flex-col gap-1.5 sm:col-span-2">
          <Label className="text-muted-foreground text-xs">Base URL</Label>
          <Input
            value={provider.base_url ?? ""}
            onChange={(e) => onChange({ base_url: e.target.value })}
            placeholder="https://api.example.com/v1"
          />
        </div>
        <div className="flex flex-col gap-1.5 sm:col-span-2">
          <Label className="text-muted-foreground text-xs">模型</Label>
          <div className="border-input focus-within:border-ring focus-within:ring-ring/50 flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border bg-transparent px-2 py-1.5 focus-within:ring-[3px]">
            {(provider.models ?? []).map((m) => (
              <span
                key={m}
                className="bg-secondary text-secondary-foreground flex items-center gap-1 rounded px-1.5 py-0.5 text-xs"
              >
                {m}
                <button
                  type="button"
                  className="text-muted-foreground hover:text-foreground cursor-pointer"
                  onClick={() => removeModel(m)}
                >
                  <XIcon className="size-3" />
                </button>
              </span>
            ))}
            <input
              value={modelInput}
              onChange={(e) => setModelInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === ",") {
                  e.preventDefault()
                  addModel()
                }
              }}
              onBlur={addModel}
              placeholder={provider.models?.length ? "" : "输入模型名，Enter 添加"}
              className="min-w-32 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-3 sm:col-span-2">
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">上下文窗口（token）</Label>
            <Input
              type="number"
              min={0}
              value={provider.context_window ?? ""}
              onChange={(e) => onChange({ context_window: Number(e.target.value) || 0 })}
              placeholder="如 128000"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">最大输出（token）</Label>
            <Input
              type="number"
              min={0}
              value={provider.max_output_tokens ?? ""}
              onChange={(e) => onChange({ max_output_tokens: Number(e.target.value) || 0 })}
              placeholder="如 8192"
            />
          </div>
        </div>

        <div className="sm:col-span-2">
          <Collapsible>
            <CollapsibleTrigger className="text-muted-foreground group flex items-center gap-1 text-xs cursor-pointer hover:text-foreground">
              <ChevronRightIcon className="size-3.5 transition-transform group-data-[state=open]:rotate-90" />
              高级
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="mt-3 flex flex-col gap-3 rounded-md bg-muted/40 p-3">
                <div className="flex flex-col gap-1.5">
                  <Label className="text-muted-foreground text-xs">输入类型</Label>
                  <div className="flex flex-wrap gap-1.5">
                    {INPUT_TYPES.map((t) => {
                      const active = (provider.input_types ?? []).includes(t.value)
                      return (
                        <button
                          key={t.value}
                          type="button"
                          onClick={() => {
                            const cur = provider.input_types ?? []
                            onChange({
                              input_types: active
                                ? cur.filter((x) => x !== t.value)
                                : [...cur, t.value],
                            })
                          }}
                          className={cn(
                            "rounded-md border px-2.5 py-1 text-xs cursor-pointer transition-colors",
                            active
                              ? "bg-primary text-primary-foreground border-transparent"
                              : "text-muted-foreground hover:bg-accent",
                          )}
                        >
                          {t.label}
                        </button>
                      )
                    })}
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <Label className="text-muted-foreground text-xs">推理等级</Label>
                  <Select
                    value={provider.reasoning ?? "default"}
                    onValueChange={(v) => onChange({ reasoning: v })}
                  >
                    <SelectTrigger size="sm" className="w-36">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {REASONING_LEVELS.map((r) => (
                        <SelectItem key={r.value} value={r.value}>
                          {r.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </CollapsibleContent>
          </Collapsible>
        </div>
      </div>
    </div>
  )
}

function ThemePreview({ id }: { id: Theme }) {
  return (
    <div
      className={cn(
        "flex h-10 w-16 overflow-hidden rounded-md border",
        id === "light" ? "bg-white" : id === "dark" ? "bg-neutral-900" : "bg-gradient-to-r from-white to-neutral-900",
      )}
    >
      <div
        className={cn(
          "h-full w-1/3 border-r",
          id === "light"
            ? "border-neutral-200 bg-neutral-100"
            : id === "dark"
              ? "border-neutral-800 bg-neutral-800"
              : "border-transparent bg-white/60",
        )}
      />
    </div>
  )
}
