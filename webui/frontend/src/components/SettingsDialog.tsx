import { useEffect, useRef, useState } from "react"
import {
  BlocksIcon,
  BrainIcon,
  ChartNoAxesColumnIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  CpuIcon,
  ChevronRightIcon,
  FolderOpenIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  SunMoonIcon,
  Trash2Icon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { UsagePanel } from "@/components/UsagePanel"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type {
  MCPStatus,
  MCPServerConfig,
  ModelOptions,
  ProviderConfig,
  SkillDirs,
  SkillInfo,
  StateInfo,
  Theme,
  ThemeColor,
} from "@/types"

const SECTIONS = [
  { id: "model", label: "模型", icon: CpuIcon },
  { id: "theme", label: "主题", icon: SunMoonIcon },
  { id: "memory", label: "记忆", icon: BrainIcon },
  { id: "plugins", label: "插件", icon: BlocksIcon },
  { id: "usage", label: "使用量", icon: ChartNoAxesColumnIcon },
] as const

/** 新添加模型时自动填入的默认上下文/输出上限 */
const DEFAULT_CONTEXT_WINDOW = 1_000_000
const DEFAULT_MAX_OUTPUT_TOKENS = 128_000

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
  /** 供应商配置保存成功后回调——外层据此刷新模型列表 */
  onProvidersSaved?: () => void
}

// MCP 服务器编辑草稿
interface MCPDraft {
  name: string
  kind: "stdio" | "http"
  command: string
  argsText: string
  /** 环境变量键值列表（左 KEY 右值，可增删多行） */
  envRows: { key: string; value: string }[]
  url: string
  disabled: boolean
  isNew: boolean
}

function emptyMcpDraft(): MCPDraft {
  return {
    name: "",
    kind: "stdio",
    command: "",
    argsText: "",
    envRows: [],
    url: "",
    disabled: false,
    isNew: true,
  }
}

// 内置插件需要的环境变量：编辑弹窗里按 KEY 预生成行，用户只填值
const MCP_ENV_KEYS: Record<string, string[]> = {
  agnes: ["AGNES_API_KEY", "AGNES_IMAGE_MODEL", "AGNES_VIDEO_MODEL"],
  mimo: ["MIMO_API_KEY", "MIMO_TTS_MODEL"],
}

// 已知插件环境变量的默认值（预填进输入框，用户可改）
const MCP_ENV_DEFAULTS: Record<string, string> = {
  AGNES_IMAGE_MODEL: "agnes-image-2.5-flash",
  AGNES_VIDEO_MODEL: "agnes-video-2.5-flash",
  MIMO_TTS_MODEL: "mimo-v2.5-tts",
}

// 各环境变量输入框的占位提示
const MCP_ENV_PLACEHOLDERS: Record<string, string> = {
  AGNES_API_KEY: "填入 API Key（留空则未配置）",
  AGNES_IMAGE_MODEL: "默认生图模型（可选）：agnes-image-2.5-flash / agnes-image-2.1-flash",
  AGNES_VIDEO_MODEL: "默认生视频模型（可选）：agnes-video-2.5-flash / agnes-video-2.5",
  MIMO_API_KEY: "填入 API Key（留空则未配置）",
  MIMO_TTS_MODEL: "默认 TTS 模型（可选）：mimo-v2.5-tts / -voicedesign / -voiceclone",
}

// seedEnvRows 生成某服务器的环境变量行：已知插件（agnes/mimo）按固定 KEY 预生成
// （模型类变量带默认值），配置里其余自定义变量追加在后。
function seedEnvRows(name: string, env?: Record<string, string>): { key: string; value: string }[] {
  const rows: { key: string; value: string }[] = []
  const used = new Set<string>()
  for (const k of MCP_ENV_KEYS[name] ?? []) {
    const v = env?.[k] ?? MCP_ENV_DEFAULTS[k] ?? ""
    rows.push({ key: k, value: v })
    used.add(k)
  }
  for (const [k, v] of Object.entries(env ?? {})) {
    if (!used.has(k)) rows.push({ key: k, value: v })
  }
  return rows
}

// 常见插件需要的环境变量（配置里没填时在列表里提示，避免调用时才报错）
const MCP_ENV_HINTS: Record<string, string> = {
  agnes: "AGNES_API_KEY（Agnes AI 生图/生视频，点编辑填写）",
  mimo: "MIMO_API_KEY（MiMo 语音合成，点编辑填写）",
}

export function SettingsDialog({ open, onOpenChange, state, onStateChange, onProvidersSaved }: SettingsDialogProps) {
  const [section, setSection] = useState<SectionId>("model")
  const [providers, setProviders] = useState<ProviderConfig[] | null>(null)
  const [theme, setTheme] = useState<Theme>(state?.theme ?? "dark")
  const [themeColor, setThemeColor] = useState<ThemeColor>(state?.theme_color ?? "zinc")
  const [memory, setMemory] = useState("")
  const [memorySaved, setMemorySaved] = useState(false)
  const [mcpStatus, setMcpStatus] = useState<MCPStatus[]>([])
  const [mcpServers, setMcpServers] = useState<Record<string, MCPServerConfig>>({})
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [pluginTab, setPluginTab] = useState<"skills" | "mcp">("skills")
  const [mcpDraft, setMcpDraft] = useState<MCPDraft | null>(null)
  const [dirs, setDirs] = useState<SkillDirs>({})
  const [toast, setToast] = useState("")
  const [confirm, setConfirm] = useState<{ name: string } | null>(null)
  const [err, setErr] = useState("")
  const [saving, setSaving] = useState(false)
  /** 供应商配置保存成功提示（区别于技能/MCP 的 toast） */
  const [providersSaved, setProvidersSaved] = useState(false)
  /** 最近一次「添加」的供应商下标：新卡片据此自动展开并聚焦名称 */
  const [newProviderIndex, setNewProviderIndex] = useState(-1)

  const refreshSkills = async () => {
    try {
      const r = await api.skills()
      setSkills(r.skills ?? [])
    } catch {
      /* 忽略 */
    }
  }

  // 启用/禁用技能：后端写进配置 disabled_skills，完成后刷新列表
  const toggleSkill = async (name: string, disabled: boolean) => {
    try {
      await api.skillToggle(name, disabled)
      await refreshSkills()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const refreshMcp = async (delayMs = 0) => {    const load = async () => {
      try {
        const [cfg, st] = await Promise.all([api.mcpConfig(), api.mcp()])
        setMcpServers(cfg.servers ?? {})
        setMcpStatus(st.servers ?? [])
      } catch {
        /* 忽略 */
      }
    }
    if (delayMs > 0) setTimeout(() => void load(), delayMs)
    else await load()
  }

  useEffect(() => {
    if (!open) return
    api.providers().then((r) => setProviders(r.providers)).catch(() => setProviders([]))
    api.settings().then((r) => {
      setTheme(r.theme)
      if (r.theme_color) setThemeColor(r.theme_color)
    }).catch(() => {})
    api.memory().then((r) => setMemory(r.content)).catch(() => {})
    void refreshMcp()
    void refreshSkills()
    void loadDirs()
    setErr("")
    setToast("")
    setMcpDraft(null)
  }, [open])

  const saveProviders = async () => {
    if (!providers) return
    setSaving(true)
    setErr("")
    setProvidersSaved(false)
    try {
      const r = await api.saveProviders(providers)
      setProviders(r.providers)
      if (state) onStateChange({ ...state, model: r.current, configured: r.providers.length > 0 })
      // 保存成功后通知外层刷新模型列表——否则「选择模型」二级菜单仍是旧数据
      onProvidersSaved?.()
      setProvidersSaved(true)
      setTimeout(() => setProvidersSaved(false), 2500)
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
    // 新卡片挂载时自动展开并聚焦名称输入框，方便直接填写
    setNewProviderIndex((providers ?? []).length)
    setProviders((prev) => [...(prev ?? []), { type: "openai", models: [] }])
  }

  const removeProvider = (i: number) => {
    setProviders((prev) => prev?.filter((_, j) => j !== i) ?? null)
  }

  // ---------- MCP 增删改 ----------

  const startEditMcp = (name: string) => {
    setErr("")
    const cfg = mcpServers[name] ?? {}
    setMcpDraft({
      name,
      kind: cfg.url ? "http" : "stdio",
      command: cfg.command ?? "",
      argsText: (cfg.args ?? []).join(" "),
      envRows: seedEnvRows(name, cfg.env),
      url: cfg.url ?? "",
      disabled: Boolean(cfg.disabled),
      isNew: false,
    })
  }

  const saveMcp = async () => {
    if (!mcpDraft) return
    const name = mcpDraft.name.trim()
    if (!name) {
      setErr("请填写服务器名称")
      return
    }
    const next = { ...mcpServers }
    delete next[name]
    // 环境变量：KEY 非空的行才保留；已知 KEY 值留空视为清除
    const env: Record<string, string> = {}
    for (const row of mcpDraft.envRows) {
      const k = row.key.trim()
      if (k) env[k] = row.value.trim()
    }
    next[name] =
      mcpDraft.kind === "http"
        ? { url: mcpDraft.url.trim(), disabled: mcpDraft.disabled }
        : {
            command: mcpDraft.command.trim(),
            args: mcpDraft.argsText.split(/\s+/).filter(Boolean),
            env,
            disabled: mcpDraft.disabled,
          }
    setSaving(true)
    setErr("")
    try {
      const r = await api.saveMcpConfig(next)
      setMcpServers(r.servers ?? {})
      setMcpDraft(null)
      void refreshMcp(1200) // 等后台重连完成再看状态
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  // 删除 MCP 服务器
  const removeMcp = async (name: string) => {
    setErr("")
    try {
      const next = { ...mcpServers }
      delete next[name]
      const r = await api.saveMcpConfig(next)
      setMcpServers(r.servers ?? {})
      void refreshMcp(1200)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  // ---------- 打开技能 / MCP 目录 ----------

  const loadDirs = async () => {
    try {
      const r = await api.skillDirs()
      setDirs(r.dirs ?? {})
    } catch {
      /* 忽略 */
    }
  }

  // 打开后延迟自动刷新：用户把 SKILL.md 放进目录，回到界面即可看到新技能
  const openSkillFolder = async (source: "global" | "project", name?: string) => {
    setErr("")
    try {
      await api.openSkillDir(source, name)
      setToast("已打开目录，放入 SKILL.md 后点「刷新」即可加载")
      setTimeout(() => void refreshSkills(), 1500)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const openMcpConfig = async () => {
    setErr("")
    try {
      const r = await api.openMcpFile()
      setToast(r.exists ? "已打开配置文件，改完点「刷新」即可重连" : "配置文件还不存在，已打开所在目录")
      setTimeout(() => void refreshMcp(1200), 1500)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
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

        <div className="flex h-full min-h-0 flex-col md:flex-row">
          <nav className="shrink-0 border-b px-2 py-2 md:w-42 md:border-r md:border-b-0 md:px-2 md:py-3">
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

          <div className="min-h-0 flex-1 overflow-y-auto">
            <div className="flex flex-col gap-4 p-5">
              {section === "model" && (
                <div className="flex flex-col gap-4">
                  <div className="flex items-center justify-between">
                    <Label>供应商（点击展开配置）</Label>
                    <div className="flex gap-2">
                      <Button variant="outline" size="sm" onClick={addProvider}>
                        <PlusIcon /> 添加
                      </Button>
                      <Button size="sm" disabled={saving || !providers} onClick={() => void saveProviders()}>
                        {saving ? "保存中…" : "保存"}
                      </Button>
                    </div>
                  </div>

                  {err && <div className="text-destructive text-sm">{err}</div>}

                  {providersSaved && (
                    <div className="text-muted-foreground flex items-center gap-1.5 text-sm">
                      <CheckCircle2Icon className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                      已保存，模型列表已更新
                    </div>
                  )}

                  {toast && (
                    <div className="text-muted-foreground flex items-center gap-2 text-sm">
                      <span>{toast}</span>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          setToast("")
                          void refreshSkills()
                          void refreshMcp(1200)
                        }}
                      >
                        <RefreshCwIcon /> 刷新
                      </Button>
                    </div>
                  )}

                  {(providers ?? []).length === 0 && (
                    <div className="text-muted-foreground text-sm">暂无供应商</div>
                  )}


                  {(providers ?? []).map((p, i) => (
                    <ProviderCard
                      key={i}
                      provider={p}
                      defaultOpen={i === newProviderIndex}
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
                        { id: "mcp", label: `MCP${mcpStatus.length ? ` ${mcpStatus.length}` : ""}` },
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
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <Label>技能（对话中用 /名称 触发）</Label>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => void openSkillFolder("global")}
                            title={dirs.global?.path ?? "打开全局技能目录"}
                          >
                            <FolderOpenIcon /> 打开目录
                          </Button>
                        </div>
                      </div>

                      {skills.length === 0 && (
                        <div className="text-muted-foreground text-sm">未安装技能</div>
                      )}

                      <div className="flex flex-col gap-1.5">
                        {skills.map((s) => (
                          <div
                            key={`${s.source}-${s.name}`}
                            className={cn(
                              "flex items-center gap-2 rounded-md border px-3 py-2 text-sm",
                              s.disabled && "opacity-60",
                            )}
                          >
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="font-medium">/{s.name}</span>
                              </div>
                              <div className="text-muted-foreground text-xs [overflow-wrap:anywhere] line-clamp-1">
                                {s.description}
                              </div>
                            </div>
                            <Button
                              variant={s.disabled ? "outline" : "secondary"}
                              size="sm"
                              className="shrink-0"
                              onClick={() => void toggleSkill(s.name, !s.disabled)}
                            >
                              {s.disabled ? "禁用中" : "启用中"}
                            </Button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {pluginTab === "mcp" && (
                    <div className="flex flex-col gap-2">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <Label>MCP 服务器</Label>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => void openMcpConfig()}
                            title={dirs.mcp?.path ?? "打开 MCP 配置文件"}
                          >
                            <FolderOpenIcon /> 打开配置
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setErr("")
                              setMcpDraft(emptyMcpDraft())
                            }}
                          >
                            <PlusIcon /> 添加服务器
                          </Button>
                        </div>
                      </div>

                      {/* MCP 编辑弹窗：挂在列表上方不显眼，改成居中弹窗 */}
                      <Dialog open={mcpDraft !== null} onOpenChange={(o) => { if (!o) setMcpDraft(null) }}>
                        <DialogContent className="max-h-[85dvh] overflow-y-auto p-5 sm:max-w-2xl" showCloseButton>
                          <DialogHeader>
                            <DialogTitle>
                              {mcpDraft?.isNew ? "添加 MCP 服务器" : `编辑 ${mcpDraft?.name}`}
                            </DialogTitle>
                          </DialogHeader>

                          {mcpDraft && (
                            <div className="flex flex-col gap-2.5">
                              <div className="grid gap-2 sm:grid-cols-2">
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">名称</Label>
                                  <Input
                                    value={mcpDraft.name}
                                    disabled={!mcpDraft.isNew}
                                    onChange={(e) => {
                                      const name = e.target.value
                                      // 新建时输入已知插件名（agnes/mimo）→ 自动预生成环境变量行（模型带默认值）
                                      const rows =
                                        mcpDraft.isNew && mcpDraft.envRows.length === 0 && (MCP_ENV_KEYS[name] ?? []).length > 0
                                          ? seedEnvRows(name, undefined)
                                          : mcpDraft.envRows
                                      setMcpDraft({ ...mcpDraft, name, envRows: rows })
                                    }}
                                    placeholder="如 filesystem"
                                  />
                                </div>
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">连接方式</Label>
                                  <Select
                                    value={mcpDraft.kind}
                                    onValueChange={(v) =>
                                      setMcpDraft({ ...mcpDraft, kind: v as "stdio" | "http" })
                                    }
                                  >
                                    <SelectTrigger size="sm" className="w-full">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      <SelectItem value="stdio">本地命令（stdio）</SelectItem>
                                      <SelectItem value="http">远程 URL（HTTP）</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                              </div>

                              {mcpDraft.kind === "stdio" ? (
                                <>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">启动命令</Label>
                                    <Input
                                      value={mcpDraft.command}
                                      onChange={(e) => setMcpDraft({ ...mcpDraft, command: e.target.value })}
                                      placeholder="npx"
                                    />
                                  </div>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">参数（空格分隔）</Label>
                                    <Input
                                      value={mcpDraft.argsText}
                                      onChange={(e) => setMcpDraft({ ...mcpDraft, argsText: e.target.value })}
                                      placeholder="-y @modelcontextprotocol/server-filesystem C:\work"
                                    />
                                  </div>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">环境变量</Label>
                                    {mcpDraft.envRows.map((row, i) => (
                                      <div key={i} className="flex items-center gap-2">
                                        <Input
                                          value={row.key}
                                          onChange={(e) =>
                                            setMcpDraft({
                                              ...mcpDraft,
                                              envRows: mcpDraft.envRows.map((r, j) =>
                                                j === i ? { ...r, key: e.target.value } : r,
                                              ),
                                            })
                                          }
                                          placeholder="KEY"
                                          className="w-44 shrink-0 font-mono text-xs"
                                        />
                                        <Input
                                          value={row.value}
                                          onChange={(e) =>
                                            setMcpDraft({
                                              ...mcpDraft,
                                              envRows: mcpDraft.envRows.map((r, j) =>
                                                j === i ? { ...r, value: e.target.value } : r,
                                              ),
                                            })
                                          }
                                          placeholder={MCP_ENV_PLACEHOLDERS[row.key] ?? "值"}
                                        />
                                        <Button
                                          variant="ghost"
                                          size="icon-sm"
                                          className="text-muted-foreground hover:text-destructive shrink-0"
                                          onClick={() =>
                                            setMcpDraft({
                                              ...mcpDraft,
                                              envRows: mcpDraft.envRows.filter((_, j) => j !== i),
                                            })
                                          }
                                          title="删除此变量"
                                        >
                                          <Trash2Icon />
                                        </Button>
                                      </div>
                                    ))}
                                    <div>
                                      <Button
                                        variant="outline"
                                        size="sm"
                                        onClick={() =>
                                          setMcpDraft({ ...mcpDraft, envRows: [...mcpDraft.envRows, { key: "", value: "" }] })
                                        }
                                      >
                                        <PlusIcon /> 添加变量
                                      </Button>
                                    </div>
                                  </div>
                                </>
                              ) : (
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">服务器 URL</Label>
                                  <Input
                                    value={mcpDraft.url}
                                    onChange={(e) => setMcpDraft({ ...mcpDraft, url: e.target.value })}
                                    placeholder="https://example.com/mcp"
                                  />
                                </div>
                              )}

                              <div className="flex items-center justify-between">
                                <Label className="text-muted-foreground text-xs">启用</Label>
                                <Button
                                  variant={mcpDraft.disabled ? "outline" : "secondary"}
                                  size="sm"
                                  onClick={() => setMcpDraft({ ...mcpDraft, disabled: !mcpDraft.disabled })}
                                >
                                  {mcpDraft.disabled ? "已禁用" : "启用中"}
                                </Button>
                              </div>

                              {err && <div className="text-destructive text-sm">{err}</div>}
                              <div className="flex justify-end gap-2">
                                <Button variant="outline" size="sm" onClick={() => setMcpDraft(null)}>
                                  取消
                                </Button>
                                <Button size="sm" disabled={saving} onClick={() => void saveMcp()}>
                                  保存并连接
                                </Button>
                              </div>
                            </div>
                          )}
                        </DialogContent>
                      </Dialog>

                      {Object.keys(mcpServers).length === 0 && !mcpDraft && (
                        <div className="text-muted-foreground text-sm">未配置 MCP 服务器</div>
                      )}

                      <div className="flex flex-col gap-1.5">
                        {Object.entries(mcpServers).map(([name, cfg]) => {
                          const st = mcpStatus.find((x) => x.name === name)
                          const envHint = MCP_ENV_HINTS[name]
                          const envMissing = envHint && !Object.values(cfg.env ?? {}).some((v) => v.trim() !== "")
                          return (
                            <div
                              key={name}
                              className="group flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 text-sm"
                            >
                              {st?.connected ? (
                                <CheckCircle2Icon className="size-4 shrink-0 text-green-600" />
                              ) : (
                                <CircleAlertIcon className="text-destructive size-4 shrink-0" />
                              )}
                              <span className="font-medium">{name}</span>
                              {cfg.disabled && <Badge variant="outline">已禁用</Badge>}
                              <span className="text-muted-foreground w-full text-xs sm:w-auto sm:flex-1">
                                {st?.connected ? `${st.tool_count} 个工具` : st?.error ? st.error : ""}
                              </span>                              <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  onClick={() => startEditMcp(name)}
                                  title="编辑服务器"
                                >
                                  <PencilIcon />
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  className="text-muted-foreground hover:text-destructive"
                                  onClick={() => setConfirm({ name })}
                                  title="删除服务器"
                                >
                                  <Trash2Icon />
                                </Button>
                              </div>
                              {envMissing && (
                                <div className="text-amber-600 dark:text-amber-400 w-full text-xs">
                                  还需要环境变量：{envHint}
                                </div>
                              )}
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {section === "usage" && (
                <div className="flex flex-col gap-3">
                  <UsagePanel />
                </div>
              )}
            </div>
          </div>
        </div>

        {/* 删除 MCP 服务器确认 */}
        <AlertDialog
          open={confirm !== null}
          onOpenChange={(o) => {
            if (!o) setConfirm(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>删除 MCP 服务器？</AlertDialogTitle>
              <AlertDialogDescription>
                将从配置中移除「{confirm?.name}」并断开连接，不影响已保存的配置文件。
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  if (confirm) void removeMcp(confirm.name)
                  setConfirm(null)
                }}
              >
                删除
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </DialogContent>
    </Dialog>
  )
}

function ProviderCard({
  provider,
  onChange,
  onRemove,
  defaultOpen = false,
}: {
  provider: ProviderConfig
  onChange: (patch: Partial<ProviderConfig>) => void
  onRemove: () => void
  /** 挂载时是否展开（新添加的供应商为 true），展开后自动聚焦名称输入框 */
  defaultOpen?: boolean
}) {
  const [open, setOpen] = useState(defaultOpen)
  const [modelInput, setModelInput] = useState("")
  const nameRef = useRef<HTMLInputElement>(null)

  // 新添加的供应商：展开后直接聚焦名称，省一次点击
  useEffect(() => {
    if (defaultOpen) nameRef.current?.focus()
  }, [defaultOpen])

  /** 批量添加：支持一次粘贴多个（逗号/空格分隔），自动去重 */
  const addModels = () => {
    const parts = modelInput
      .split(/[,，\s]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    if (parts.length === 0) return
    const cur = provider.models ?? []
    const models = [...cur]
    for (const p of parts) if (!models.includes(p)) models.push(p)
    if (models.length === cur.length) {
      setModelInput("")
      return
    }
    // 新增模型默认带上下文/输出参数（用户既有值不覆盖）
    const opts = { ...provider.model_options }
    for (const p of models) {
      if (opts[p]) continue
      const prev = opts[cur[cur.length - 1]]
      opts[p] = {
        context_window: prev?.context_window ?? DEFAULT_CONTEXT_WINDOW,
        max_output_tokens: prev?.max_output_tokens ?? DEFAULT_MAX_OUTPUT_TOKENS,
      }
    }
    onChange({ models, model: models[0], model_options: opts })
    setModelInput("")
  }

  const removeModel = (m: string) => {
    const models = (provider.models ?? []).filter((x) => x !== m)
    onChange({ models, model: models[0] })
    // 模型被删时一并清掉它的参数，避免配置文件留下死条目
    if (provider.model_options?.[m]) {
      const next = { ...provider.model_options }
      delete next[m]
      onChange({ model_options: next })
    }
  }

  /** 重命名模型：模型名是 model_options 的键，改名时要把参数一并迁过去 */
  const renameModel = (from: string, to: string) => {
    const name = to.trim()
    if (!from || name === from) return
    if (!name) {
      removeModel(from)
      return
    }
    if ((provider.models ?? []).includes(name)) return // 重名则忽略，避免键冲突
    const models = (provider.models ?? []).map((x) => (x === from ? name : x))
    const patch: Partial<ProviderConfig> = { models }
    // 主模型指向被改名的那一个时同步更新
    if (provider.model === from) patch.model = name
    const opts = { ...provider.model_options }
    if (opts[from]) {
      const { [from]: moved, ...rest } = opts
      patch.model_options = { ...rest, [name]: moved }
    }
    onChange(patch)
  }

  /** 写入单个模型的参数；全部为空时删掉该条目，回落到供应商默认值 */
  const patchModelOptions = (m: string, patch: Partial<ModelOptions>) => {
    const cur = provider.model_options?.[m] ?? {}
    const next = { ...cur, ...patch }
    const empty =
      !next.context_window &&
      !next.max_output_tokens &&
      !next.input_types?.length &&
      !next.reasoning
    const all = { ...provider.model_options }
    if (empty) {
      delete all[m]
    } else {
      all[m] = next
    }
    onChange({ model_options: Object.keys(all).length ? all : undefined })
  }

  return (
    <div className="rounded-lg border">
      {/* 折叠行：点击展开全部配置 */}
      <div className="flex items-center gap-1 px-2 py-1.5">
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="group flex min-w-0 flex-1 items-center gap-2 px-1 text-left cursor-pointer"
        >
          <ChevronRightIcon
            className={cn(
              "text-muted-foreground size-4 shrink-0 transition-transform",
              open && "rotate-90",
            )}
          />
          <span className="truncate text-sm font-medium">{provider.name || "未命名供应商"}</span>
          <Badge variant="secondary">{PROVIDER_TYPES.find((t) => t.value === provider.type)?.label ?? provider.type}</Badge>
          <span className="text-muted-foreground truncate text-xs">
            {(provider.models ?? []).length} 个模型
          </span>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground hover:text-destructive"
          onClick={onRemove}
          title="删除供应商"
        >
          <Trash2Icon />
        </Button>
      </div>

      {open && (
        <div className="border-t p-4">
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between">
              <Label className="text-muted-foreground text-xs">名称</Label>
            </div>
            <Input
              ref={nameRef}
              value={provider.name ?? ""}
              onChange={(e) => onChange({ name: e.target.value })}
              placeholder="供应商名称"
              className="max-w-56"
            />

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
              {/* 添加模型：只负责新增；改名/删除在下方「模型参数」列表里做 */}
              <div className="flex flex-col gap-1.5 sm:col-span-2">
                <Label className="text-muted-foreground text-xs">添加模型（Enter 添加，可逗号分隔多个）</Label>
                <Input
                  value={modelInput}
                  onChange={(e) => setModelInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === ",") {
                      e.preventDefault()
                      addModels()
                    }
                  }}
                  onBlur={addModels}
                  placeholder={provider.models?.length ? "" : "如 gpt-4o、claude-sonnet-4-20250514"}
                />
              </div>

              {/* 各模型独立参数 */}
              <div className="flex flex-col gap-2 sm:col-span-2">
                {(provider.models ?? []).length === 0 ? (
                  <div className="text-muted-foreground text-xs">还没有模型，先在上面添加</div>
                ) : (
                  (provider.models ?? []).map((m) => (
                    <ModelOptionsRow
                      key={m}
                      name={m}
                      options={provider.model_options?.[m] ?? {}}
                      onChange={(opt) => patchModelOptions(m, opt)}
                      onRename={renameModel}
                      onRemove={removeModel}
                    />
                  ))
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

/** 输入类型多选：text / image / video / pdf / audio */
function InputTypePicker({
  value,
  onChange,
}: {
  value?: string[]
  onChange: (v: string[]) => void
}) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {INPUT_TYPES.map((t) => {
        const active = (value ?? []).includes(t.value)
        return (
          <button
            key={t.value}
            type="button"
            onClick={() =>
              onChange(
                active ? (value ?? []).filter((x) => x !== t.value) : [...(value ?? []), t.value],
              )
            }
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
  )
}

/** 推理等级下拉 */
function ReasoningPicker({
  value,
  onChange,
}: {
  value?: string
  onChange: (v: string) => void
}) {
  return (
    <Select value={value || "default"} onValueChange={onChange}>
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
  )
}

/**
 * 单个模型的参数行：折叠时只显示模型名 + 已配置项摘要，
 * 展开后可单独设置上下文窗口 / 最大输出 / 输入类型 / 推理等级，
 * 并可就地重命名模型（改名时参数跟着迁移）。
 */
function ModelOptionsRow({
  name,
  options,
  onChange,
  onRename,
  onRemove,
}: {
  name: string
  options: ModelOptions
  onChange: (patch: Partial<ModelOptions>) => void
  onRename: (from: string, to: string) => void
  onRemove: (name: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(name)

  // 已配置项摘要，让用户不展开也能看到差异
  const summary: string[] = []
  if (options.context_window) summary.push(`上下文 ${options.context_window}`)
  if (options.max_output_tokens) summary.push(`输出 ${options.max_output_tokens}`)
  if (options.input_types?.length) summary.push(options.input_types.join("/"))
  if (options.reasoning) summary.push(`推理 ${options.reasoning}`)

  const commitRename = () => {
    setEditing(false)
    if (draft.trim() && draft.trim() !== name) onRename(name, draft.trim())
    else setDraft(name)
  }

  return (
    <div className="rounded-md border">
      <div className="flex items-center">
        <button
          type="button"
          onClick={() => setOpen(!open)}
          className="hover:bg-accent/40 flex min-w-0 flex-1 cursor-pointer items-center gap-2 px-2.5 py-2 text-left transition-colors"
        >
          <ChevronRightIcon
            className={cn(
              "text-muted-foreground size-3.5 shrink-0 transition-transform",
              open && "rotate-90",
            )}
          />
          {editing ? (
            <Input
              autoFocus
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              onBlur={commitRename}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  commitRename()
                } else if (e.key === "Escape") {
                  e.preventDefault()
                  setDraft(name)
                  setEditing(false)
                }
                e.stopPropagation()
              }}
              onClick={(e) => e.stopPropagation()}
              className="h-6 font-mono text-xs"
            />
          ) : (
            <span className="truncate font-mono text-xs">{name}</span>
          )}
          {!editing && summary.length > 0 && (
            <span className="text-muted-foreground ml-auto truncate text-[11px]">{summary.join(" · ")}</span>
          )}
        </button>
        {!editing && (
          <div className="flex shrink-0 items-center pr-1.5">
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground"
              onClick={() => {
                setDraft(name)
                setEditing(true)
              }}
              title="重命名模型"
            >
              <PencilIcon />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground hover:text-destructive"
              onClick={() => onRemove(name)}
              title="删除模型"
            >
              <Trash2Icon />
            </Button>
          </div>
        )}
      </div>

      {open && (
        <div className="flex flex-col gap-3 border-t p-3">
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label className="text-muted-foreground text-xs">上下文窗口（token）</Label>
              <Input
                type="number"
                min={0}
                value={options.context_window ?? ""}
                onChange={(e) => onChange({ context_window: Number(e.target.value) || 0 })}
                placeholder="留空则用默认"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label className="text-muted-foreground text-xs">最大输出（token）</Label>
              <Input
                type="number"
                min={0}
                value={options.max_output_tokens ?? ""}
                onChange={(e) => onChange({ max_output_tokens: Number(e.target.value) || 0 })}
                placeholder="留空则用默认"
              />
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">输入类型</Label>
            <InputTypePicker
              value={options.input_types}
              onChange={(v) => onChange({ input_types: v })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">推理等级</Label>
            <ReasoningPicker
              value={options.reasoning}
              onChange={(v) => onChange({ reasoning: v })}
            />
          </div>
        </div>
      )}
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
