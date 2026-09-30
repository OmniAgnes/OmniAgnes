import { useEffect, useState } from "react"
import {
  BlocksIcon,
  BrainIcon,
  ChartNoAxesColumnIcon,
  CheckCircle2Icon,
  CircleAlertIcon,
  ClipboardPasteIcon,
  CpuIcon,
  ChevronRightIcon,
  FolderOpenIcon,
  PencilIcon,
  PlusIcon,
  RefreshCwIcon,
  SunMoonIcon,
  Trash2Icon,
  XIcon,
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

// 技能编辑草稿
interface SkillDraft {
  name: string
  description: string
  content: string
  isNew: boolean
}

// MCP 服务器编辑草稿
interface MCPDraft {
  name: string
  kind: "stdio" | "http"
  command: string
  argsText: string
  envText: string
  url: string
  disabled: boolean
  isNew: boolean
}

// 技能粘贴导入草稿：直接把 SKILL.md 全文贴进来
interface SkillPaste {
  content: string
}

function emptyMcpDraft(): MCPDraft {
  return {
    name: "",
    kind: "stdio",
    command: "",
    argsText: "",
    envText: "",
    url: "",
    disabled: false,
    isNew: true,
  }
}

// envText（KEY=VALUE 每行一条）→ env 对象
function parseEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split("\n")) {
    const s = line.trim()
    if (!s) continue
    const i = s.indexOf("=")
    if (i <= 0) continue
    out[s.slice(0, i).trim()] = s.slice(i + 1).trim()
  }
  return out
}

function envToText(env?: Record<string, string>): string {
  return Object.entries(env ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join("\n")
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
  const [skillDraft, setSkillDraft] = useState<SkillDraft | null>(null)
  const [mcpDraft, setMcpDraft] = useState<MCPDraft | null>(null)
  const [skillPaste, setSkillPaste] = useState<SkillPaste | null>(null)
  const [dirs, setDirs] = useState<SkillDirs>({})
  const [toast, setToast] = useState("")
  const [confirm, setConfirm] = useState<{ kind: "skill" | "mcp"; name: string } | null>(null)
  const [err, setErr] = useState("")
  const [saving, setSaving] = useState(false)
  /** 供应商配置保存成功提示（区别于技能/MCP 的 toast） */
  const [providersSaved, setProvidersSaved] = useState(false)

  const refreshSkills = async () => {
    try {
      const r = await api.skills()
      setSkills(r.skills ?? [])
    } catch {
      /* 忽略 */
    }
  }

  const refreshMcp = async (delayMs = 0) => {
    const load = async () => {
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
    setSkillDraft(null)
    setMcpDraft(null)
    setSkillPaste(null)
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
    setProviders((prev) => [...(prev ?? []), { type: "openai", models: [] }])
  }

  const removeProvider = (i: number) => {
    setProviders((prev) => prev?.filter((_, j) => j !== i) ?? null)
  }

  // ---------- 技能增删改 ----------

  const startEditSkill = async (s: SkillInfo) => {
    setErr("")
    try {
      const r = await api.skill(s.name, s.source)
      // 编辑时保留原文（含 frontmatter），避免二次保存丢字段
      setSkillDraft({
        name: s.name,
        description: r.skill.description,
        content: r.skill.content,
        isNew: false,
      })
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const saveSkill = async () => {
    if (!skillDraft) return
    setSaving(true)
    setErr("")
    try {
      if (skillDraft.isNew) {
        await api.createSkill({
          name: skillDraft.name.trim(),
          description: skillDraft.description.trim(),
          content: skillDraft.content,
        })
      } else {
        await api.updateSkill(skillDraft.name, {
          description: skillDraft.description.trim(),
          content: skillDraft.content,
        })
      }
      setSkillDraft(null)
      await refreshSkills()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
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
      envText: envToText(cfg.env),
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
    next[name] =
      mcpDraft.kind === "http"
        ? { url: mcpDraft.url.trim(), disabled: mcpDraft.disabled }
        : {
            command: mcpDraft.command.trim(),
            args: mcpDraft.argsText.split(/\s+/).filter(Boolean),
            env: parseEnv(mcpDraft.envText),
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

  // 删除技能 / MCP 服务器
  const removePlugin = async (kind: "skill" | "mcp", name: string) => {
    setErr("")
    try {
      if (kind === "skill") {
        await api.deleteSkill(name)
        await refreshSkills()
      } else {
        const next = { ...mcpServers }
        delete next[name]
        const r = await api.saveMcpConfig(next)
        setMcpServers(r.servers ?? {})
        void refreshMcp(1200)
      }
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  // ---------- 打开技能 / MCP 目录，粘贴即自动加载 ----------

  const loadDirs = async () => {
    try {
      const r = await api.skillDirs()
      setDirs(r.dirs ?? {})
    } catch {
      /* 忽略 */
    }
  }

  // 打开后延迟自动刷新：用户粘完文件回到界面即可看到新技能
  const openSkillFolder = async (source: "global" | "project", name?: string) => {
    setErr("")
    try {
      await api.openSkillDir(source, name)
      setToast("已打开目录，粘贴 SKILL.md 后点「刷新」即可加载")
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

  const savePastedSkill = async () => {
    if (!skillPaste || !skillPaste.content.trim()) return
    setErr("")
    setSaving(true)
    try {
      const r = await api.importSkill({ content: skillPaste.content, source: "global" })
      setSkillPaste(null)
      await refreshSkills()
      setToast(`已导入技能 /${r.name ?? ""}`)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
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
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => {
                              setErr("")
                              setSkillDraft(null)
                              setSkillPaste({ content: "" })
                            }}
                            title="把 SKILL.md 全文粘进来，自动解析名称和描述"
                          >
                            <ClipboardPasteIcon /> 粘贴导入
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setErr("")
                              setSkillPaste(null)
                              setSkillDraft({ name: "", description: "", content: "", isNew: true })
                            }}
                          >
                            <PlusIcon /> 添加技能
                          </Button>
                        </div>
                      </div>

                      {dirs.global?.path && (
                        <div className="text-muted-foreground text-xs [overflow-wrap:anywhere]">
                          全局技能目录：<code className="font-mono">{dirs.global.path}</code>
                          <span className="ml-1.5">
                            每个技能一个子目录，内含 SKILL.md
                          </span>
                        </div>
                      )}

                      {skillPaste && (
                        <div className="flex flex-col gap-2.5 rounded-lg border p-3">
                          <div className="flex items-center justify-between">
                            <span className="text-sm font-medium">粘贴 SKILL.md</span>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => setSkillPaste(null)}
                              title="取消"
                            >
                              <XIcon />
                            </Button>
                          </div>
                          <Textarea
                            value={skillPaste.content}
                            onChange={(e) => setSkillPaste({ content: e.target.value })}
                            className="min-h-40 font-mono text-xs"
                            placeholder={
                              "---\nname: pdf-report\ndescription: 把 PDF 整理成结构化报告\n---\n\n# 执行步骤\n1. …"
                            }
                          />
                          <p className="text-muted-foreground text-xs">
                            带 frontmatter 就原样保存；没有的话自动从正文推断名称和描述。
                          </p>
                          <div className="flex justify-end gap-2">
                            <Button variant="outline" size="sm" onClick={() => setSkillPaste(null)}>
                              取消
                            </Button>
                            <Button
                              size="sm"
                              disabled={saving || !skillPaste.content.trim()}
                              onClick={() => void savePastedSkill()}
                            >
                              导入
                            </Button>
                          </div>
                        </div>
                      )}

                      {skillDraft && (
                        <div className="flex flex-col gap-2.5 rounded-lg border p-3">
                          <div className="flex items-center justify-between">
                            <span className="text-sm font-medium">
                              {skillDraft.isNew ? "新建技能" : `编辑 /${skillDraft.name}`}
                            </span>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => setSkillDraft(null)}
                              title="取消"
                            >
                              <XIcon />
                            </Button>
                          </div>
                          <div className="grid gap-2 sm:grid-cols-2">
                            <div className="flex flex-col gap-1.5">
                              <Label className="text-muted-foreground text-xs">名称</Label>
                              <Input
                                value={skillDraft.name}
                                disabled={!skillDraft.isNew}
                                onChange={(e) => setSkillDraft({ ...skillDraft, name: e.target.value })}
                                placeholder="如 pdf-report"
                              />
                            </div>
                            <div className="flex flex-col gap-1.5">
                              <Label className="text-muted-foreground text-xs">描述（模型据此判断何时使用）</Label>
                              <Input
                                value={skillDraft.description}
                                onChange={(e) => setSkillDraft({ ...skillDraft, description: e.target.value })}
                                placeholder="把 PDF 整理成结构化报告"
                              />
                            </div>
                          </div>
                          <div className="flex flex-col gap-1.5">
                            <Label className="text-muted-foreground text-xs">指令内容（Markdown）</Label>
                            <Textarea
                              value={skillDraft.content}
                              onChange={(e) => setSkillDraft({ ...skillDraft, content: e.target.value })}
                              className="min-h-40 font-mono text-xs"
                              placeholder={"# 执行步骤\n1. …"}
                            />
                          </div>
                          <div className="flex justify-end gap-2">
                            <Button variant="outline" size="sm" onClick={() => setSkillDraft(null)}>
                              取消
                            </Button>
                            <Button size="sm" disabled={saving} onClick={() => void saveSkill()}>
                              {skillDraft.isNew ? "创建" : "保存"}
                            </Button>
                          </div>
                        </div>
                      )}

                      {skills.length === 0 && !skillDraft && (
                        <div className="text-muted-foreground text-sm">未安装技能</div>
                      )}

                      <div className="flex flex-col gap-1.5">
                        {skills.map((s) => (
                          <div
                            key={`${s.source}-${s.name}`}
                            className="group flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
                          >
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="font-medium">/{s.name}</span>
                                <Badge variant="outline">{s.source === "project" ? "项目" : "全局"}</Badge>
                              </div>
                              <div className="text-muted-foreground text-xs [overflow-wrap:anywhere]">{s.description}</div>
                            </div>
                            <div className="flex shrink-0 items-center gap-1">
                              {s.source === "project" ? (
                                <span className="text-muted-foreground text-xs">项目内只读</span>
                              ) : (
                                <div className="flex items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                                  <Button
                                    variant="ghost"
                                    size="icon-sm"
                                    onClick={() => void openSkillFolder("global", s.name)}
                                    title="在文件管理器中打开"
                                  >
                                    <FolderOpenIcon />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-sm"
                                    onClick={() => void startEditSkill(s)}
                                    title="编辑技能"
                                  >
                                    <PencilIcon />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-sm"
                                    className="text-muted-foreground hover:text-destructive"
                                    onClick={() => setConfirm({ kind: "skill", name: s.name })}
                                    title="删除技能"
                                  >
                                    <Trash2Icon />
                                  </Button>
                                </div>
                              )}
                            </div>
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

                      {dirs.mcp?.path && (
                        <div className="text-muted-foreground text-xs [overflow-wrap:anywhere]">
                          配置文件：<code className="font-mono">{dirs.mcp.path}</code>
                          <span className="ml-1.5">按 JSON 结构加 servers，改完点刷新重连</span>
                        </div>
                      )}

                      {mcpDraft && (
                        <div className="flex flex-col gap-2.5 rounded-lg border p-3">
                          <div className="flex items-center justify-between">
                            <span className="text-sm font-medium">
                              {mcpDraft.isNew ? "添加 MCP 服务器" : `编辑 ${mcpDraft.name}`}
                            </span>
                            <Button variant="ghost" size="icon-sm" onClick={() => setMcpDraft(null)} title="取消">
                              <XIcon />
                            </Button>
                          </div>
                          <div className="grid gap-2 sm:grid-cols-2">
                            <div className="flex flex-col gap-1.5">
                              <Label className="text-muted-foreground text-xs">名称</Label>
                              <Input
                                value={mcpDraft.name}
                                disabled={!mcpDraft.isNew}
                                onChange={(e) => setMcpDraft({ ...mcpDraft, name: e.target.value })}
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
                                <Label className="text-muted-foreground text-xs">环境变量（每行 KEY=VALUE）</Label>
                                <Textarea
                                  value={mcpDraft.envText}
                                  onChange={(e) => setMcpDraft({ ...mcpDraft, envText: e.target.value })}
                                  className="min-h-20 font-mono text-xs"
                                  placeholder={"API_KEY=xxx"}
                                />
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

                      {Object.keys(mcpServers).length === 0 && !mcpDraft && (
                        <div className="text-muted-foreground text-sm">未配置 MCP 服务器</div>
                      )}

                      <div className="flex flex-col gap-1.5">
                        {Object.entries(mcpServers).map(([name, cfg]) => {
                          const st = mcpStatus.find((x) => x.name === name)
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
                              <Badge variant="secondary">{cfg.url ? "http" : "stdio"}</Badge>
                              {cfg.disabled && <Badge variant="outline">已禁用</Badge>}
                              <span className="text-muted-foreground w-full font-mono text-xs [overflow-wrap:anywhere] sm:w-auto sm:flex-1">
                                {cfg.url || [cfg.command, ...(cfg.args ?? [])].filter(Boolean).join(" ")}
                                {st?.connected ? ` · ${st.tool_count} 个工具` : st?.error ? ` · ${st.error}` : ""}
                              </span>
                              <div className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
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
                                  onClick={() => setConfirm({ kind: "mcp", name })}
                                  title="删除服务器"
                                >
                                  <Trash2Icon />
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

              {section === "usage" && (
                <div className="flex flex-col gap-3">
                  <UsagePanel />
                </div>
              )}
            </div>
          </div>
        </div>

        {/* 删除技能 / MCP 服务器确认 */}
        <AlertDialog
          open={confirm !== null}
          onOpenChange={(o) => {
            if (!o) setConfirm(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{confirm?.kind === "skill" ? "删除技能？" : "删除 MCP 服务器？"}</AlertDialogTitle>
              <AlertDialogDescription>
                {confirm?.kind === "skill"
                  ? `「${confirm.name}」的 SKILL.md 及其目录将被删除，无法恢复。`
                  : `将从配置中移除「${confirm?.name}」并断开连接，不影响已保存的配置文件。`}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>取消</AlertDialogCancel>
              <AlertDialogAction
                onClick={() => {
                  if (confirm) void removePlugin(confirm.kind, confirm.name)
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
}: {
  provider: ProviderConfig
  onChange: (patch: Partial<ProviderConfig>) => void
  onRemove: () => void
}) {
  const [open, setOpen] = useState(false)
  const [modelInput, setModelInput] = useState("")

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
    onChange({ models, model: models[0] })
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
