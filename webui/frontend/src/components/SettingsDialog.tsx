import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import {
  BlocksIcon,
  BrainIcon,
  ChartNoAxesColumnIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  CoinsIcon,
  CpuIcon,
  ChevronRightIcon,
  FolderOpenIcon,
  InfoIcon,
  Loader2Icon,
  LogInIcon,
  LogOutIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  SunMoonIcon,
  Trash2Icon,
  UserIcon,
  WifiIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { UsagePanel } from "@/components/UsagePanel"
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
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { server, type ServerCredit, type ServerUser } from "@/lib/serverAuth"
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
  { id: "account", key: "se.account", icon: UserIcon, loginOnly: true },
  { id: "model", key: "se.model", icon: CpuIcon },
  { id: "theme", key: "se.theme", icon: SunMoonIcon },
  { id: "memory", key: "se.memory", icon: BrainIcon },
  { id: "plugins", key: "se.plugins", icon: BlocksIcon },
  { id: "usage", key: "se.usage", icon: ChartNoAxesColumnIcon },
  { id: "version", key: "se.version", icon: InfoIcon },
] as const

/** 应用版本号：发版（CHANGELOG/tag）时同步更新 */
const APP_VERSION = "v1.0.13"

/** 新添加模型时自动填入的默认上下文/输出上限 */
const DEFAULT_CONTEXT_WINDOW = 1_000_000
const DEFAULT_MAX_OUTPUT_TOKENS = 128_000

type SectionId = (typeof SECTIONS)[number]["id"]

const PROVIDER_TYPES = [
  { value: "openai", key: "se.openai" },
  { value: "anthropic", key: "se.anthropic" },
  { value: "gemini", key: "se.gemini" },
  { value: "ollama", key: "se.ollama" },
] as const

const THEME_COLORS = [
  { id: "zinc", label: "石墨", light: "#18181b", dark: "#3f3f46", tint: "#ececee", tintSoft: "#f7f7f8" },
  { id: "violet", label: "紫罗兰", light: "#7c5cff", dark: "#a78bfa", tint: "#e7e1f5", tintSoft: "#f6f4fb" },
  { id: "blue", label: "海洋", light: "#2f7cd6", dark: "#60a5fa", tint: "#dee9f4", tintSoft: "#f4f8fb" },
  { id: "lime", label: "青柠", light: "#65a30d", dark: "#a3e635", tint: "#ebf1d8", tintSoft: "#f8fbee" },
  { id: "emerald", label: "翡翠", light: "#0e9f6e", dark: "#34d399", tint: "#e3ece4", tintSoft: "#f6f8f5" },
  { id: "amber", label: "琥珀", light: "#d97706", dark: "#fbbf24", tint: "#f2ebdd", tintSoft: "#faf8f1" },
  { id: "rose", label: "玫瑰", light: "#e11d48", dark: "#fb7185", tint: "#f6e3e8", tintSoft: "#fbf3f5" },
] as const satisfies ReadonlyArray<{ id: ThemeColor; label: string; light: string; dark: string; tint: string; tintSoft: string }>

/** 统一主题选择：深色 + 浅色系六个配色（彩色主题均为浅色底）。内部映射到 theme/theme_color 两个字段。 */
const THEME_PRESETS = [
  "dark",
  "light",
  "violet",
  "blue",
  "lime",
  "emerald",
  "amber",
  "rose",
] as const

type ThemePreset = (typeof THEME_PRESETS)[number]

/** 由存储的 theme/theme_color 推导当前选中的主题项（旧的跟随系统配置归入浅色显示） */
function deriveThemePreset(t?: Theme, c?: ThemeColor): ThemePreset {
  if (t === "dark") return "dark"
  return c && c !== "zinc" ? c : "light"
}

const INPUT_TYPES = [
  { value: "text", key: "se.it.text" },
  { value: "image", key: "se.it.image" },
  { value: "video", key: "se.it.video" },
  { value: "pdf", key: "se.it.pdf" },
] as const

// 只有开/关两档：未配置（空）视同开启
const REASONING_LEVELS = [
  { value: "on", key: "se.rl.on" },
  { value: "off", key: "se.rl.off" },
] as const

// 推理强度仅部分模型支持（low/medium/high）；none 哨兵 = 不发送该参数
const REASONING_EFFORTS = ["low", "medium", "high"] as const
const EFFORT_NONE = "none"

interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  state: StateInfo | null
  onStateChange: (s: StateInfo) => void
  /** 打开时直达的 section（如使用统计、账号） */
  initialSection?: string
  /** 供应商配置保存成功后回调——外层据此刷新模型列表 */
  onProvidersSaved?: () => void
  /** 登录后台用户：有值则「账号」页签显示账号信息 */
  serverUser?: ServerUser | null
  /** 后台可达性：未登录但可达时「账号」页签也要出来（里面给登录入口） */
  serverOnline?: boolean
  /** 退出登录（「账号」页签里用） */
  onLogout?: () => void
  /** 「账号」页签未登录时打开登录弹窗 */
  onOpenLogin?: () => void
}

// MCP 服务器编辑草稿
interface MCPDraft {
  name: string
  kind: "stdio" | "http"
  command: string
  /** 内置插件的真实 exe 路径（command 显示为屏蔽文案）；非内置为空 */
  builtinCmd: string
  argsText: string
  /** 环境变量键值列表（左 KEY 右值，可增删多行） */
  envRows: { key: string; value: string }[]
  url: string
  disabled: boolean
  isNew: boolean
  /** 用途描述（列表展示 + 注入系统提示词） */
  description: string
}

function emptyMcpDraft(): MCPDraft {
  return {
    name: "",
    kind: "stdio",
    command: "",
    builtinCmd: "",
    argsText: "",
    envRows: [],
    url: "",
    disabled: false,
    isNew: true,
    description: "",
  }
}

// 内置插件需要的环境变量：编辑弹窗里按 KEY 预生成行，用户只填值
const MCP_ENV_KEYS: Record<string, string[]> = {
  agnes: ["AGNES_API_KEY", "AGNES_IMAGE_MODEL", "AGNES_VIDEO_MODEL"],
  mimo: ["MIMO_API_KEY", "MIMO_TTS_MODEL"],
}

// 随桌面端分发的内置 MCP 插件名（dev/ 构建、打进 resources/bin）。
// 编辑弹窗里把它们的完整 exe 路径屏蔽为「内置 <名> 插件」，保存时原样回填，
// 避免把本机绝对路径摊在界面与配置里。
const BUILTIN_MCP_NAMES = new Set(["pdf", "xlsx", "docx", "pptx", "agnes", "mimo"])

function builtinMcpMask(cmd: string): string | null {
  const base = cmd.replace(/\\/g, "/").split("/").pop()?.toLowerCase()
  if (!base) return null
  const name = base.replace(/\.(exe|bin)$/, "")
  return BUILTIN_MCP_NAMES.has(name) ? name : null
}

// 已知插件环境变量的默认值（预填进输入框，用户可改）
const MCP_ENV_DEFAULTS: Record<string, string> = {
  AGNES_IMAGE_MODEL: "agnes-image-2.5-flash",
  AGNES_VIDEO_MODEL: "agnes-video-2.5-flash",
  MIMO_TTS_MODEL: "mimo-v2.5-tts",
}

// 各环境变量输入框的占位提示（i18n key）
const MCP_ENV_PLACEHOLDERS: Record<string, string> = {
  AGNES_API_KEY: "se.envPhKey",
  AGNES_IMAGE_MODEL: "se.envPhImage",
  AGNES_VIDEO_MODEL: "se.envPhVideo",
  MIMO_API_KEY: "se.envPhKey",
  MIMO_TTS_MODEL: "se.envPhTts",
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

// 常见插件需要的环境变量（配置里没填时在列表里提示，避免调用时才报错；i18n key）
const MCP_ENV_HINTS: Record<string, string> = {
  agnes: "se.envHintAgnes",
  mimo: "se.envHintMimo",
}

export function SettingsDialog({
  open,
  onOpenChange,
  initialSection,
  state,
  onStateChange,
  onProvidersSaved,
  serverUser,
  serverOnline,
  onLogout,
  onOpenLogin,
}: SettingsDialogProps) {
  const { t } = useI18n()
  const [section, setSection] = useState<SectionId>("model")
  // 每次打开弹窗递增：强制使用量面板重挂载重新拉取（对话期间数据会变化）
  const [usageSeq, setUsageSeq] = useState(0)
  useEffect(() => {
    if (open) setUsageSeq((s) => s + 1)
  }, [open])
  // 外层指定直达 section（使用统计、账号等）；后台不可达时「账号」页签不存在，回落到默认页签
  useEffect(() => {
    if (!open || !initialSection) return
    if (initialSection === "account" && !serverUser && !serverOnline) return
    setSection(initialSection as SectionId)
  }, [open, initialSection, serverUser, serverOnline])
  const [providers, setProviders] = useState<ProviderConfig[] | null>(null)
  const [preset, setPreset] = useState<ThemePreset>(deriveThemePreset(state?.theme, state?.theme_color ?? "zinc"))
  const [memory, setMemory] = useState("")
  const [memorySaved, setMemorySaved] = useState(false)
  // 账号：登录用户打开时拉最新余额（领取后余额会变化，每次打开重取）；积分流水由账号页自分页拉取
  const [cred, setCred] = useState<ServerUser | null>(null)
  const [credLoading, setCredLoading] = useState(false)
  const [mcpStatus, setMcpStatus] = useState<MCPStatus[]>([])
  const [mcpServers, setMcpServers] = useState<Record<string, MCPServerConfig>>({})
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [pluginTab, setPluginTab] = useState<"skills" | "mcp">("skills")
  const [mcpDraft, setMcpDraft] = useState<MCPDraft | null>(null)
  const [dirs, setDirs] = useState<SkillDirs>({})
  const [toast, setToast] = useState("")
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
      setPreset(deriveThemePreset(r.theme, r.theme_color))
    }).catch(() => {})
    api.memory().then((r) => setMemory(r.content)).catch(() => {})
    void refreshMcp()
    void refreshSkills()
    void loadDirs()
    // 账号：有登录态才拉；未登录不显示该页签。
    // 依赖带上 serverUser：在设置里当场登录成功时也要重拉，否则面板停在「后台暂不可达」
    if (serverUser) {
      setCredLoading(true)
      server
        .me()
        .then((r) => setCred(r.user))
        .catch(() => setCred(null))
        .finally(() => setCredLoading(false))
    } else {
      setCred(null)
    }
    setErr("")
    setToast("")
    setMcpDraft(null)
  }, [open, serverUser])

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

  // 统一主题：深色只切外观（保留原配色供日后切回彩色）；
  // 其余都是浅色系配色，浅色=石墨基调，彩色五项连主区背景一起着色
  const saveThemePreset = async (id: ThemePreset) => {
    setPreset(id)
    const r =
      id === "dark"
        ? await api.saveSettings({ theme: "dark" })
        : await api.saveSettings({ theme: "light", theme_color: id === "light" ? "zinc" : (id as ThemeColor) })
    if (state) onStateChange({ ...state, theme: r.theme, theme_color: r.theme_color })
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
    // 内置插件：完整 exe 路径是机器相关的，界面只展示标签，保存时回填
    const mask = builtinMcpMask(cfg.command ?? "") || (cfg.builtin ? name : null)
    setMcpDraft({
      name,
      kind: cfg.url ? "http" : "stdio",
      command: mask ? t("se.mcpBuiltinCmd", { name: mask }) : (cfg.command ?? ""),
      builtinCmd: mask ? (cfg.command ?? "") : "",
      argsText: (cfg.args ?? []).join(" "),
      envRows: seedEnvRows(name, cfg.env),
      url: cfg.url ?? "",
      disabled: Boolean(cfg.disabled),
      isNew: false,
      description: cfg.description ?? "",
    })
  }

  const saveMcp = async () => {
    if (!mcpDraft) return
    const name = mcpDraft.name.trim()
    if (!name) {
      setErr(t("se.mcpNameReq"))
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
        ? { url: mcpDraft.url.trim(), disabled: mcpDraft.disabled, description: mcpDraft.description.trim() }
        : {
            command: mcpDraft.builtinCmd || mcpDraft.command.trim(),
            args: mcpDraft.argsText.split(/\s+/).filter(Boolean),
            env,
            disabled: mcpDraft.disabled,
            description: mcpDraft.description.trim(),
            builtin: mcpDraft.builtinCmd !== "",
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

  // 启用/禁用 MCP 服务器（列表直接切换，等同编辑弹窗里的启用开关）
  const toggleMcp = async (name: string) => {
    setErr("")
    try {
      const cur = mcpServers[name]
      if (!cur) return
      const next = { ...mcpServers, [name]: { ...cur, disabled: !cur.disabled } }
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
      setToast(t("se.openedDir"))
      setTimeout(() => void refreshSkills(), 1500)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const openMcpConfig = async () => {
    setErr("")
    try {
      const r = await api.openMcpFile()
      setToast(r.exists ? t("se.openedMcp") : t("se.noMcpFile"))
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
          <DialogTitle>{t("se.title")}</DialogTitle>
        </DialogHeader>

        {/* min-w-0 必须有：这是 grid item，min-width:auto 会解析成 min-content，
            被页签那排 420px 顶开后整块内容比弹窗还宽，被 DialogContent 的
            overflow-hidden 裁掉（窄屏表现为「保存」按钮和页签直接消失）。 */}
        <div className="flex h-full min-h-0 min-w-0 flex-col md:flex-row">
          <nav className="shrink-0 border-b px-2 py-2 md:w-42 md:border-r md:border-b-0 md:px-2 md:py-3">
            {/* 窄屏保持单行 + 横向滚动，绝不换行：五个页签一排约 420px，放不进 390px
                视口。换成 flex-wrap 会把页签摊成两行，把内容整体往下顶、选中态落到
                屏幕外；而 overflow-x-auto 顺带解决了宽度问题——overflow 非 visible 时
                该 grid item 的 min-width:auto 会被压成 0，不会再被 min-content 顶宽。
                md 以上恢复竖排。 */}
            <div className="flex gap-1 overflow-x-auto md:flex-col md:overflow-visible">
              {/* 后台可达就显示「账号」页签：已登录看信息，未登录给登录入口 */}
              {SECTIONS.filter((s) => s.id !== "account" || !!serverUser || serverOnline).map((s) => (
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
                  <span className="whitespace-nowrap">{t(s.key)}</span>
                </button>
              ))}
            </div>
          </nav>

          <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
            <div className="flex min-w-0 flex-col gap-4 p-5">
              {section === "model" && (
                <div className="flex flex-col gap-4">
                  {/* 窄屏：标题可换行、按钮组不压缩，按钮不会被挤出弹窗右边 */}
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <Label className="min-w-0">{t("se.providers")}</Label>
                    <div className="flex shrink-0 gap-2">
                      <Button variant="outline" size="sm" onClick={addProvider}>
                        <PlusIcon /> {t("se.add")}
                      </Button>
                      <Button size="sm" disabled={saving || !providers} onClick={() => void saveProviders()}>
                        {saving ? t("se.saving") : t("se.save")}
                      </Button>
                    </div>
                  </div>

                  {err && <div className="text-destructive text-sm">{err}</div>}

                  {providersSaved && (
                    <div className="text-muted-foreground flex items-center gap-1.5 text-sm">
                      <CheckCircle2Icon className="size-3.5 text-emerald-600 dark:text-emerald-400" />
                      {t("se.savedModels")}
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
                        <RefreshCwIcon /> {t("se.refresh")}
                      </Button>
                    </div>
                  )}

                  {(providers ?? []).length === 0 && (
                    <div className="text-muted-foreground text-sm">{t("se.noProviders")}</div>
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
                <div className="flex flex-col gap-2">
                  <Label>{t("se.theme")}</Label>
                  <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
                    {THEME_PRESETS.map((p) => {
                      const c = THEME_COLORS.find((x) => x.id === p)
                      return (
                        <button
                          key={p}
                          type="button"
                          onClick={() => void saveThemePreset(p)}
                          className={cn(
                            "flex flex-col items-center gap-2 rounded-lg border p-3 text-sm cursor-pointer transition-colors",
                            preset === p ? "border-ring ring-ring/30 ring-2" : "hover:bg-accent",
                          )}
                        >
                          {c ? (
                            <span
                              className="size-8 rounded-full border border-black/10 shadow-sm"
                              style={{
                                background: `linear-gradient(135deg, ${c.tint} 55%, ${c.tintSoft} 55%)`,
                              }}
                            />
                          ) : (
                            <ThemePreview id={p as Theme} />
                          )}
                          {t(`theme.${p}`)}
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}

              {section === "memory" && (
                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between">
                    <Label>{t("se.globalMemory")}</Label>
                    {memorySaved && <span className="text-muted-foreground text-xs">{t("se.saved")}</span>}
                  </div>
                  <Textarea
                    value={memory}
                    onChange={(e) => setMemory(e.target.value)}
                    className="min-h-44 font-mono text-xs"
                  />
                  <div className="flex justify-end">
                    <Button size="sm" variant="secondary" onClick={() => void saveMemory()}>
                      {t("se.save")}
                    </Button>
                  </div>
                </div>
              )}

              {section === "plugins" && (
                <div className="flex flex-col gap-3">
                  <div className="bg-muted/50 flex w-fit rounded-md p-0.5">
                    {(
                      [
                        { id: "skills", label: `${t("se.skills")}${skills.length ? ` ${skills.length}` : ""}` },
                        { id: "mcp", label: `MCP${mcpStatus.length ? ` ${mcpStatus.length}` : ""}` },
                      ] as const
                    ).map((tt) => (
                      <button
                        key={tt.id}
                        type="button"
                        onClick={() => setPluginTab(tt.id)}
                        className={cn(
                          "rounded-[5px] px-3 py-1.5 text-sm cursor-pointer transition-colors",
                          pluginTab === tt.id
                            ? "bg-background text-foreground shadow-xs"
                            : "text-muted-foreground hover:text-foreground",
                        )}
                      >
                        {tt.label}
                      </button>
                    ))}
                  </div>

                  {pluginTab === "skills" && (
                    <div className="flex flex-col gap-2">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <Label>{t("se.skillsHint")}</Label>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => void openSkillFolder("global")}
                            title={dirs.global?.path ?? t("se.openDirHint")}
                          >
                            <FolderOpenIcon /> {t("se.openDir")}
                          </Button>
                        </div>
                      </div>

                      {skills.length === 0 && (
                        <div className="text-muted-foreground text-sm">{t("se.noSkills")}</div>
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
                              {s.disabled ? t("se.enable") : t("se.disable")}
                            </Button>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {pluginTab === "mcp" && (
                    <div className="flex flex-col gap-2">
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <Label>{t("se.mcpServers")}</Label>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => void openMcpConfig()}
                            title={dirs.mcp?.path ?? t("se.openMcpConfig")}
                          >
                            <FolderOpenIcon /> {t("se.openConfig")}
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setErr("")
                              setMcpDraft(emptyMcpDraft())
                            }}
                          >
                            <PlusIcon /> {t("se.addServer")}
                          </Button>
                        </div>
                      </div>

                      {/* MCP 编辑弹窗：挂在列表上方不显眼，改成居中弹窗 */}
                      <Dialog open={mcpDraft !== null} onOpenChange={(o) => { if (!o) setMcpDraft(null) }}>
                        <DialogContent className="max-h-[85dvh] overflow-y-auto p-5 sm:max-w-2xl" showCloseButton>
                          <DialogHeader>
                            <DialogTitle>
                              {mcpDraft?.isNew ? t("se.addMcp") : t("se.editMcp", { name: mcpDraft?.name ?? "" })}
                            </DialogTitle>
                          </DialogHeader>

                          {mcpDraft && (
                            <div className="flex flex-col gap-2.5">
                              <div className="grid gap-2 sm:grid-cols-2">
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">{t("se.mcpName")}</Label>
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
                                    placeholder={t("se.mcpExample")}
                                  />
                                </div>
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">{t("se.mcpConn")}</Label>
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
                                      <SelectItem value="stdio">{t("se.mcpStdio")}</SelectItem>
                                      <SelectItem value="http">{t("se.mcpHttp")}</SelectItem>
                                    </SelectContent>
                                  </Select>
                                </div>
                              </div>

                              <div className="flex flex-col gap-1.5">
                                <Label className="text-muted-foreground text-xs">{t("se.mcpDesc")}</Label>
                                <Input
                                  value={mcpDraft.description}
                                  onChange={(e) => setMcpDraft({ ...mcpDraft, description: e.target.value })}
                                  placeholder={t("se.mcpDescPh")}
                                />
                              </div>

                              {mcpDraft.kind === "stdio" ? (
                                <>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">{t("se.mcpCommand")}</Label>
                                    <Input
                                      value={mcpDraft.command}
                                      disabled={mcpDraft.builtinCmd !== ""}
                                      onChange={(e) => setMcpDraft({ ...mcpDraft, command: e.target.value })}
                                      placeholder="npx"
                                    />
                                  </div>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">{t("se.mcpArgs")}</Label>
                                    <Input
                                      value={mcpDraft.argsText}
                                      onChange={(e) => setMcpDraft({ ...mcpDraft, argsText: e.target.value })}
                                      placeholder="-y @modelcontextprotocol/server-filesystem C:\work"
                                    />
                                  </div>
                                  <div className="flex flex-col gap-1.5">
                                    <Label className="text-muted-foreground text-xs">{t("se.mcpEnv")}</Label>
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
                                          placeholder={t("se.mcpKey")}
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
                                          placeholder={t(MCP_ENV_PLACEHOLDERS[row.key] ?? "se.mcpValue")}
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
                                          title={t("se.mcpRemoveVar")}
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
                                        <PlusIcon /> {t("se.mcpAddVar")}
                                      </Button>
                                    </div>
                                  </div>
                                </>
                              ) : (
                                <div className="flex flex-col gap-1.5">
                                  <Label className="text-muted-foreground text-xs">{t("se.mcpUrl")}</Label>
                                  <Input
                                    value={mcpDraft.url}
                                    onChange={(e) => setMcpDraft({ ...mcpDraft, url: e.target.value })}
                                    placeholder="https://example.com/mcp"
                                  />
                                </div>
                              )}

                              <div className="flex items-center justify-between">
                                <Label className="text-muted-foreground text-xs">{t("se.mcpEnable")}</Label>
                                <Button
                                  variant={mcpDraft.disabled ? "outline" : "secondary"}
                                  size="sm"
                                  onClick={() => setMcpDraft({ ...mcpDraft, disabled: !mcpDraft.disabled })}
                                >
                                  {mcpDraft.disabled ? t("se.enable") : t("se.disable")}
                                </Button>
                              </div>

                              {err && <div className="text-destructive text-sm">{err}</div>}
                              <div className="flex justify-end gap-2">
                                <Button variant="outline" size="sm" onClick={() => setMcpDraft(null)}>
                                  {t("s.cancel")}
                                </Button>
                                <Button size="sm" disabled={saving} onClick={() => void saveMcp()}>
                                  {t("se.mcpSave")}
                                </Button>
                              </div>
                            </div>
                          )}
                        </DialogContent>
                      </Dialog>

                      {Object.keys(mcpServers).length === 0 && !mcpDraft && (
                        <div className="text-muted-foreground text-sm">{t("se.noMcp")}</div>
                      )}

                      <div className="flex flex-col gap-1.5">
                        {Object.entries(mcpServers).map(([name, cfg]) => {
                          const st = mcpStatus.find((x) => x.name === name)
                          const envHintKey = MCP_ENV_HINTS[name]
                          const envMissing =
                            envHintKey && !Object.values(cfg.env ?? {}).some((v) => v.trim() !== "")
                          return (
                            <div
                              key={name}
                              className="group flex items-start gap-2 rounded-md border px-3 py-2 text-sm"
                            >
                              {st?.connected ? (
                                <CheckCircle2Icon className="size-4 shrink-0 text-green-600 mt-0.5" />
                              ) : (
                                <CircleAlertIcon className="text-destructive size-4 shrink-0 mt-0.5" />
                              )}
                              <div className="min-w-0 flex-1">
                                <div className="flex items-center gap-2">
                                  <span className="font-medium">{name}</span>
                                  {cfg.disabled && <Badge variant="outline">{t("se.disable")}</Badge>}
                                  <span className="text-muted-foreground truncate text-xs">
                                    {st?.connected ? t("se.mcpTools", { n: st.tool_count }) : st?.error ? st.error : ""}
                                  </span>
                                </div>
                                {cfg.description && (
                                  <div className="text-muted-foreground text-xs [overflow-wrap:anywhere] line-clamp-1">
                                    {cfg.description}
                                  </div>
                                )}
                                {envMissing && (
                                  <div className="text-amber-600 dark:text-amber-400 text-xs">
                                    {t("se.mcpEnvMissing", { hint: t(envHintKey) })}
                                  </div>
                                )}
                              </div>
                              <div className="flex shrink-0 items-center gap-1">
                                <div className="opacity-0 transition-opacity group-hover:opacity-100">
                                  <Button
                                    variant="ghost"
                                    size="icon-sm"
                                    onClick={() => startEditMcp(name)}
                                    title={t("se.mcpEdit")}
                                  >
                                    <PencilIcon />
                                  </Button>
                                </div>
                                <Button
                                  variant={cfg.disabled ? "outline" : "secondary"}
                                  size="sm"
                                  onClick={() => void toggleMcp(name)}
                                >
                                  {cfg.disabled ? t("se.enable") : t("se.disable")}
                                </Button>
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {section === "usage" && <UsagePanel key={usageSeq} />}

              {section === "version" && (
                <div className="flex items-center justify-between">
                  <Label>{t("se.currentVersion")}</Label>
                  <span className="text-muted-foreground text-sm tabular-nums">{APP_VERSION}</span>
                </div>
              )}

              {section === "account" &&
                (serverUser ? (
                  <AccountPanel cred={cred} loading={credLoading} onLogout={onLogout} />
                ) : (
                  <LoginPrompt onLogin={onOpenLogin} />
                ))}
            </div>
          </div>
        </div>

      </DialogContent>
    </Dialog>
  )
}

/**
 * 后台可达但未登录：账号页签只给一个登录入口。
 * 登录成功后 App 的 serverUser 有了值，这里会换成 AccountPanel。
 */
function LoginPrompt({ onLogin }: { onLogin?: () => void }) {
  const { t } = useI18n()
  return (
    <div className="flex flex-col items-start gap-3 py-2">
      <p className="text-muted-foreground text-sm">{t("account.notLoggedIn")}</p>
      <Button size="sm" onClick={onLogin}>
        <LogInIcon className="size-4" /> {t("menu.login")}
      </Button>
    </div>
  )
}

/**
 * 账号页签（登录用户可见）：账号信息（手机号/管理员/注册时间）+ 积分余额 + 流水。
 * 未登录时的登录入口在 LoginPrompt（后台可达但没登录时渲染）。
 */
function AccountPanel({
  cred,
  loading,
  onLogout,
}: {
  cred: ServerUser | null
  loading: boolean
  onLogout?: () => void
}) {
  const { t } = useI18n()
  const [tab, setTab] = useState<"gain" | "spend">("gain")
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<{ records: ServerCredit[]; total: number; page: number } | null>(null)
  const kind = tab === "gain" ? "gain" : "spend"

  // 页签/翻页变化重新拉取该页流水（切页签回到第一页）
  useEffect(() => {
    let alive = true
    server
      .me(kind, page, 20)
      .then((r) => alive && setRows({ records: r.records, total: r.total, page: r.page }))
      .catch(() => alive && setRows(null))
    return () => {
      alive = false
    }
  }, [kind, page])

  if (loading) {
    return <div className="text-muted-foreground text-sm">{t("se.saving")}</div>
  }
  if (!cred) {
    return <div className="text-muted-foreground text-sm">{t("se.accountOffline")}</div>
  }
  const reasonLabel = (r: ServerCredit) =>
    r.reason === "daily_claim" ? t("se.credDaily") : r.reason || t("se.credDaily")
  const list = rows?.records ?? []
  const total = rows?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / 20))
  return (
    <div className="flex flex-col gap-4">
      <div className="rounded-lg border p-4">
        <div className="flex items-center gap-2">
          <UserIcon className="size-4 shrink-0" />
          <span className="font-medium">{cred.phone}</span>
          {cred.is_admin && <Badge variant="outline">{t("se.accountAdmin")}</Badge>}
          {onLogout && (
            <Button variant="outline" size="sm" className="ml-auto" onClick={onLogout}>
              <LogOutIcon /> {t("menu.logout")}
            </Button>
          )}
        </div>
        <div className="text-muted-foreground mt-1 text-xs">
          {t("se.accountSince", { d: fmtDateTime(cred.created_at) })}
        </div>
      </div>

      <div className="flex flex-col gap-3">
        <div className="flex items-center gap-3">
          <CoinsIcon className="size-5 text-amber-500" />
          <div>
            <div className="text-muted-foreground text-xs">{t("se.creditsBalance")}</div>
            <div className="text-2xl font-semibold tabular-nums">{cred.balance}</div>
          </div>
        </div>

        {/* 消耗 / 获得 两个页签 */}
        <div className="flex gap-1 border-b">
          {(["gain", "spend"] as const).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => {
                setTab(k)
                setPage(1)
              }}
              className={cn(
                "-mb-px cursor-pointer border-b-2 px-3 py-1.5 text-sm",
                k === tab ? "border-primary text-foreground font-medium" : "text-muted-foreground hover:text-foreground border-transparent",
              )}
            >
              {t(k === "gain" ? "se.credGain" : "se.credSpend")}
            </button>
          ))}
        </div>

        {list.length === 0 ? (
          <div className="text-muted-foreground rounded-md border px-3 py-4 text-center text-sm">
            {t("se.credEmpty")}
          </div>
        ) : (
          <div className="overflow-x-auto rounded-md border">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-muted-foreground border-b text-left">
                  <th className="px-3 py-1.5 font-medium">{t("se.credReason")}</th>
                  <th className="px-3 py-1.5 text-right font-medium">{t("se.credAmount")}</th>
                  <th className="px-3 py-1.5 text-right font-medium">{t("se.credTime")}</th>
                </tr>
              </thead>
              <tbody>
                {list.map((r) => {
                  // 已到期（到期时间已过）：整行置灰
                  const expired = !!r.expires_at && new Date(r.expires_at).getTime() < Date.now()
                  return (
                    <tr key={r.id} className={cn("border-b last:border-b-0", expired && "text-muted-foreground/60")}>
                      <td className="px-3 py-1.5">
                        <span
                          className="break-all"
                          title={r.model ? `${reasonLabel(r)} · ${r.model}` : undefined}
                        >
                          {reasonLabel(r)}
                        </span>
                        {r.model ? <span className="text-muted-foreground ml-2 font-mono text-xs">{r.model}</span> : null}
                        {r.expires_at ? (
                          <span className={cn("ml-2 text-xs", expired ? "" : "text-muted-foreground")}>
                            {t("se.credExpire")} {fmtDateTime(r.expires_at)}
                          </span>
                        ) : null}
                      </td>
                      <td
                        className={cn(
                          "px-3 py-1.5 text-right font-medium tabular-nums",
                          !expired && (r.amount >= 0 ? "text-emerald-600 dark:text-emerald-400" : "text-destructive"),
                        )}
                      >
                        {r.amount >= 0 ? `+${r.amount}` : r.amount}
                      </td>
                      <td className="text-muted-foreground px-3 py-1.5 text-right text-xs whitespace-nowrap tabular-nums">
                        {fmtDateTime(r.created_at)}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
        {/* 只有一页时不渲染分页：翻页控件没有意义，白白占一行 */}
        {pages > 1 && (
          <div className="flex items-center justify-end gap-2 text-xs">
            <span className="text-muted-foreground">{t("se.credTotal", { n: total })}</span>
            <Button variant="outline" size="sm" className="h-7 px-2" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              {t("se.credPrev")}
            </Button>
            <span className="text-muted-foreground tabular-nums">
              {page} / {pages}
            </span>
            <Button variant="outline" size="sm" className="h-7 px-2" disabled={page >= pages} onClick={() => setPage(page + 1)}>
              {t("se.credNext")}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

/** 积分流水时间：Y-m-d H:i:s（本地时区） */
function fmtDateTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
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
  const { t } = useI18n()
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
      !next.reasoning &&
      !next.reasoning_effort
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
          <span className="truncate text-sm font-medium">{provider.name || t("se.unnamedProvider")}</span>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          className="text-muted-foreground hover:text-destructive"
          onClick={onRemove}
          title={t("se.removeProvider")}
        >
          <Trash2Icon />
        </Button>
      </div>

      {open && (
        <div className="border-t p-4">
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between">
              <Label className="text-muted-foreground text-xs">{t("se.name")}</Label>
            </div>
            <Input
              ref={nameRef}
              value={provider.name ?? ""}
              onChange={(e) => onChange({ name: e.target.value })}
              placeholder={t("se.name")}
              className="max-w-56"
            />

            <div className="grid gap-3 sm:grid-cols-2">
              <div className="flex flex-col gap-1.5">
                <Label className="text-muted-foreground text-xs">{t("se.type")}</Label>
                <Select value={provider.type} onValueChange={(v) => onChange({ type: v })}>
                  <SelectTrigger size="sm" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {PROVIDER_TYPES.map((pt) => (
                      <SelectItem key={pt.value} value={pt.value}>
                        {t(pt.key)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label className="text-muted-foreground text-xs">{t("se.apiKey")}</Label>
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
                <Label className="text-muted-foreground text-xs">{t("se.addModels")}</Label>
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
                  placeholder={provider.models?.length ? "" : t("se.modelPh")}
                />
              </div>

              {/* 各模型独立参数 */}
              <div className="flex flex-col gap-2 sm:col-span-2">
                {(provider.models ?? []).length === 0 ? (
                  <div className="text-muted-foreground text-xs">{t("se.noModels")}</div>
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
  const { t } = useI18n()
  return (
    <div className="flex flex-wrap gap-1.5">
      {INPUT_TYPES.map((it) => {
        const active = (value ?? []).includes(it.value)
        return (
          <button
            key={it.value}
            type="button"
            onClick={() =>
              onChange(
                active ? (value ?? []).filter((x) => x !== it.value) : [...(value ?? []), it.value],
              )
            }
            className={cn(
              "rounded-md border px-2.5 py-1 text-xs cursor-pointer transition-colors",
              active
                ? "bg-primary text-primary-foreground border-transparent"
                : "text-muted-foreground hover:bg-accent",
            )}
          >
            {t(it.key)}
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
  const { t } = useI18n()
  return (
    <Select value={value === "off" ? "off" : "on"} onValueChange={onChange}>
      <SelectTrigger size="sm" className="w-36">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {REASONING_LEVELS.map((r) => (
          <SelectItem key={r.value} value={r.value}>
            {t(r.key)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** 推理强度下拉：none 哨兵对应"不发送"（存空值） */
function ReasoningEffortPicker({
  value,
  onChange,
}: {
  value?: string
  onChange: (v: string) => void
}) {
  const { t } = useI18n()
  return (
    <Select
      value={value || EFFORT_NONE}
      onValueChange={(v) => onChange(v === EFFORT_NONE ? "" : v)}
    >
      <SelectTrigger size="sm" className="w-36">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={EFFORT_NONE}>{t("se.effortNone")}</SelectItem>
        {REASONING_EFFORTS.map((e) => (
          <SelectItem key={e} value={e}>
            {e}
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
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(name)

  // 连通性测试：点图标发一次极短探测，结果只提示连接成功 / 连接失败
  const [testing, setTesting] = useState(false)
  const [testToast, setTestToast] = useState<{ ok: boolean } | null>(null)
  const runTest = async () => {
    if (testing) return
    setTesting(true)
    let ok = false
    try {
      const r = await api.testModel(name)
      ok = r.ok
    } catch {
      ok = false
    } finally {
      setTesting(false)
      setTestToast({ ok })
      setTimeout(() => setTestToast(null), 3500)
    }
  }

  // 已配置项摘要，让用户不展开也能看到差异
  const summary: string[] = []
  if (options.context_window) summary.push(t("se.sumContext", { n: options.context_window }))
  if (options.max_output_tokens) summary.push(t("se.sumOutput", { n: options.max_output_tokens }))
  if (options.input_types?.length) summary.push(options.input_types.join("/"))
  if (options.reasoning)
    summary.push(t("se.sumReasoning", { level: t(options.reasoning === "off" ? "se.rl.off" : "se.rl.on") }))
  if (options.reasoning_effort) summary.push(t("se.sumEffort", { level: options.reasoning_effort }))

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
              onClick={runTest}
              title="测试该模型连通性"
            >
              {testing ? <Loader2Icon className="size-4 animate-spin" /> : <WifiIcon className="size-4" />}
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground"
              onClick={() => {
                setDraft(name)
                setEditing(true)
              }}
              title={t("se.renameModel")}
            >
              <PencilIcon />
            </Button>
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground hover:text-destructive"
              onClick={() => onRemove(name)}
              title={t("se.deleteModel")}
            >
              <Trash2Icon />
            </Button>
            {/* 连通性结果 toast：portal 到 body 覆盖在弹窗之上 */}
            {testToast && createPortal(
              <div
                className={cn(
                  "text-foreground fixed bottom-6 left-1/2 z-[200] -translate-x-1/2 max-w-[80vw] rounded-md border px-3.5 py-2.5 text-sm shadow-md",
                  testToast.ok ? "border-emerald-500/40 bg-emerald-50 dark:bg-emerald-950" : "border-destructive/40 bg-destructive/10",
                )}
              >
                {testToast.ok ? "连接成功" : "连接失败"}
              </div>,
              document.body,
            )}
          </div>
        )}
      </div>

      {open && (
        <div className="flex flex-col gap-3 border-t p-3">
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label className="text-muted-foreground text-xs">{t("se.contextWin")}</Label>
              <Input
                type="number"
                min={0}
                value={options.context_window ?? ""}
                onChange={(e) => onChange({ context_window: Number(e.target.value) || 0 })}
                placeholder={t("se.emptyDefault")}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label className="text-muted-foreground text-xs">{t("se.maxOut")}</Label>
              <Input
                type="number"
                min={0}
                value={options.max_output_tokens ?? ""}
                onChange={(e) => onChange({ max_output_tokens: Number(e.target.value) || 0 })}
                placeholder={t("se.emptyDefault")}
              />
            </div>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">{t("se.inTypes")}</Label>
            <InputTypePicker
              value={options.input_types}
              onChange={(v) => onChange({ input_types: v })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">{t("se.reasoning")}</Label>
            <ReasoningPicker
              value={options.reasoning}
              onChange={(v) => onChange({ reasoning: v })}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label className="text-muted-foreground text-xs">{t("se.effort")}</Label>
            <ReasoningEffortPicker
              value={options.reasoning_effort}
              onChange={(v) => onChange({ reasoning_effort: v || undefined })}
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
