import { useCallback, useEffect, useRef, useState } from "react"
import { ShieldOffIcon } from "lucide-react"

import { AddProjectDialog } from "@/components/AddProjectDialog"
import { SchedulesPage } from "@/components/SchedulesPage"
import { BrowserPane } from "@/components/BrowserPane"
import { Chat, type QueuedMsg, type ViewItem } from "@/components/Chat"
import { Sidebar } from "@/components/Sidebar"
import { SettingsDialog } from "@/components/SettingsDialog"
import { Titlebar } from "@/components/Titlebar"
import { OmniToolbar } from "@/components/OmniToolbar"
import { ServerLoginDialog } from "@/components/ServerLoginDialog"
import { UpgradeDialog } from "@/components/UpgradeDialog"
import { Button } from "@/components/ui/button"
import { api, HttpError, streamChat } from "@/lib/api"
import { setHljsTheme } from "@/lib/hljs"
import { isDesktop } from "@/lib/platform"
import { useI18n } from "@/lib/i18n"
import { extractExternalURLs } from "@/lib/sources"
import { server, serverToken, setServerToken, initServerUrl, probeServer, type ServerUser } from "@/lib/serverAuth"
import type {
  AttachmentInfo,
  ChatEvent,
  Project,
  RawAttachment,
  SkillInfo,
  StateInfo,
  TaskFull,
  TaskMeta,
  Theme,
  ThemeColor,
} from "@/types"

/** 持久化的原始附件 → 可展示结构（Path→接口 URL、Data→data URL、URL 原样） */
function attachmentToInfo(a: RawAttachment): AttachmentInfo {
  const src = a.data
    ? `data:${a.media_type || "application/octet-stream"};base64,${a.data}`
    : a.path
      ? `/api/attachment?path=${encodeURIComponent(a.path)}`
      : (a.url ?? "")
  return { kind: a.kind, media_type: a.media_type, caption: a.caption, src, path: a.path }
}

/** 模型按供应商分组：一级 = 供应商名，二级 = 模型名 */
interface ModelGroup {
  provider: string
  models: string[]
}

/**
 * 每个任务的运行状态。切走任务不销毁——后台任务的 SSE 继续往这里写，
 * 切回来时看到的正是最新进度；处理中的输入进入 queue（发送队列）。
 */
interface RunState {
  title: string
  mode: "build" | "plan"
  items: ViewItem[]
  streaming: boolean
  thinking: boolean
  /** 上游瞬时故障的自动重试进度；非空时「处理中」指示器显示「重试 n/10」 */
  retry?: { attempt: number; max: number }
  /** 本任务当前工作（回复生成中）的起始时刻；按任务持有，切任务互不串扰 */
  workSince?: number
  /** 本轮从工具参数里收集到的外部 URL（来源站点），整轮结束挂到最终回复上 */
  pendingSources?: string[]
  roundBase: number
  lastRound: number
  queue: QueuedMsg[]
}

const newRun = (title: string, mode: "build" | "plan"): RunState => ({
  title,
  mode,
  items: [],
  streaming: false,
  thinking: false,
  roundBase: 0,
  lastRound: -1,
  queue: [],
})

export default function App() {
  const [state, setState] = useState<StateInfo | null>(null)
  const [projects, setProjects] = useState<Project[]>([])
  const [tasks, setTasks] = useState<TaskMeta[]>([])
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null)
  const activeTaskIdRef = useRef<string | null>(null)
  activeTaskIdRef.current = activeTaskId
  const [settingsOpen, setSettingsOpen] = useState(false)
  // 打开设置时直达的 section（使用统计等）；为空默认「模型」
  const [settingsSection, setSettingsSection] = useState<string | undefined>(undefined)
  const [addProjectOpen, setAddProjectOpen] = useState(false)
  // 右侧主区视图：聊天（默认）或计划任务页
  const [view, setView] = useState<"chat" | "schedules">("chat")
  const [sidebarOpen, setSidebarOpen] = useState(false)
  // 侧边栏是否真的可见。不能只看 sidebarOpen：Sidebar 的 CSS 是 md 以上常驻
  // （md:static md:translate-x-0），窄屏才看 sidebarOpen 决定抽屉开合。
  // 悬浮球的显示条件必须镜像这条规则，否则宽屏浏览器里侧边栏明明在、球却没了。
  // 断点用 48rem 而非 768px：Tailwind v4 产出的是 @media (width>=48rem)，
  // 这里跟着用 rem 基准，根字号变了也不会和 CSS 分叉。
  const WIDE = "(width >= 48rem)"
  const [wide, setWide] = useState(
    () => typeof window !== "undefined" && window.matchMedia(WIDE).matches,
  )
  useEffect(() => {
    const mq = window.matchMedia(WIDE)
    const on = (e: MediaQueryListEvent) => setWide(e.matches)
    mq.addEventListener("change", on)
    return () => mq.removeEventListener("change", on)
  }, [])
  const sidebarVisible = isDesktop || wide || sidebarOpen
  // 内置浏览器面板显隐（入口在输入框底部「完全访问」后的地球图标）
  const [browserOpen, setBrowserOpen] = useState(false)
  const [loadErr, setLoadErr] = useState("")
  // 桌面更新：状态快照全在主进程（updater.js），界面只读 + 订阅，左上角「更新」入口靠它点亮
  const [updateSt, setUpdateSt] = useState<OmniUpdateState | null>(null)
  const [upgradeOpen, setUpgradeOpen] = useState(false)
  useEffect(() => {
    const upd = window.omniDesktop?.update
    if (!upd) return
    let alive = true
    void upd.get().then((s) => alive && setUpdateSt(s)).catch(() => {})
    const off = upd.onChanged((s) => alive && setUpdateSt(s))
    return () => {
      alive = false
      off()
    }
  }, [])  // 移动端换发 token 后，旧 Cookie 立刻失效。此时页面外壳还活着（HTML/JS 已在内存里），
  // 但每个接口都 401，结果就是「列表全空 + 一条红条」，用户完全不知道发生了什么。
  // 401 单独拎出来，整屏提示重新扫码。
  const [unauth, setUnauth] = useState(false)
  const fail = (e: unknown) => {
    if (e instanceof HttpError && e.status === 401) {
      setUnauth(true)
      return
    }
    setLoadErr(e instanceof Error ? e.message : String(e))
  }
  // 递增即让右侧输入框聚焦（新建任务、打开任务、点「任务」TAB）
  const [focusSignal, setFocusSignal] = useState(0)
  // 递增即让聊天消息列表滚到底部（侧栏点击任务、重开当前任务）
  const [taskOpenSignal, setTaskOpenSignal] = useState(0)
  // 当前项目可用技能（输入 / 时提示补全）
  const [skills, setSkills] = useState<SkillInfo[]>([])
  // 无任务时（首次发送会即时建任务）新任务默认用的访问模式；有任务时以 run.mode 为准
  const [defaultMode, setDefaultMode] = useState<"build" | "plan">("build")
  // 队列「编辑」：把文本回填输入框（seq 递增触发）
  const [draftSeed, setDraftSeed] = useState<{ text: string; seq: number }>({ text: "", seq: 0 })

  // ---- 桌面工具条：界面缩放 / 登录(omni-server) ----
  const { t } = useI18n()
  // 缩放倍数持久化（0.5~2）；桌面端经 IPC 走 Chromium 原生页面缩放
  const [zoom, setZoom] = useState(() => {
    const v = Number(localStorage.getItem("omni.zoom"))
    return v >= 0.5 && v <= 2 ? v : 1
  })
  const [serverUser, setServerUser] = useState<ServerUser | null>(null)
  // 后台（omni-server）可达性：启动探活一次。不可达时当纯本地版，登录/帐号/内置模型等入口都不渲染
  const [serverOnline, setServerOnline] = useState(false)
  const [loginOpen, setLoginOpen] = useState(false)
  // 全局 toast（每日积分领取成功等）
  const [toast, setToast] = useState("")
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const showToast = (msg: string) => {
    setToast(msg)
    if (toastTimer.current) clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(""), 3000)
  }
  // 后台（omni-server）探活 + 登录态恢复。桌面端常在后台还没起来时就打开，
  // 只在启动探一次会永远卡在「纯本地版」，所以点左下角 LOGO 球、点设置时都要现探；
  // 已有 token 则顺手刷新资料。
  // 地址来源见 lib/serverAuth：用户在设置里填的（localStorage）优先于内核配置。
  const checkServer = async () => {
    // 先定下后台地址，再探活；后台没开时不发任何业务请求
    await initServerUrl()
    // 注意这里不能「探不通就 return」了事：用户可能刚把地址改成错的/换了个域，
    // 必须把 serverOnline 打回 false，否则界面还按旧后台在线显示，登录必然失败。
    const online = await probeServer()
    setServerOnline(online)
    if (!online) return
    if (!serverToken()) return
    try {
      const r = await server.me()
      setServerUser(r.user)
    } catch (e) {
      // 401 = 会话过期：清 token 回未登录；服务器暂不可达则保留，恢复后可直接续用
      if (e instanceof Error && (e as { status?: number }).status === 401) {
        setServerToken("")
        setServerUser(null)
      }
    }
  }
  useEffect(() => {
    void checkServer()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  // 缩放落库 + 同步到原生页面缩放（网页端没有这个 IPC，浏览器自带缩放，
  // 页内 CSS zoom 会把 h-screen 撑破、弹窗大小不一致，故桌面端专用）
  useEffect(() => {
    localStorage.setItem("omni.zoom", String(zoom))
    if (isDesktop) window.omniDesktop?.setZoom(zoom)
  }, [zoom])
  // Ctrl++ / Ctrl+- / Ctrl+0 缩放（桌面全局快捷键）
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.ctrlKey || e.metaKey) || e.altKey) return
      if (e.key === "=" || e.key === "+") {
        e.preventDefault()
        setZoom((z) => Math.min(2, +(z + 0.1).toFixed(1)))
      } else if (e.key === "-") {
        e.preventDefault()
        setZoom((z) => Math.max(0.5, +(z - 0.1).toFixed(1)))
      } else if (e.key === "0") {
        e.preventDefault()
        setZoom(1)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  // 主题快速切换（复用设置保存逻辑）
  const themePreset = (): string => {
    const t = state?.theme ?? "light"
    const c = state?.theme_color ?? "zinc"
    if (t === "dark") return "dark"
    return c && c !== "zinc" ? c : "light"
  }
  const changeThemePreset = async (id: string) => {
    if (!state) return
    try {
      const r =
        id === "dark"
          ? await api.saveSettings({ theme: "dark" as Theme })
          : await api.saveSettings({ theme: "light" as Theme, theme_color: (id === "light" ? "zinc" : id) as ThemeColor })
      setState({ ...state, theme: r.theme, theme_color: r.theme_color })
    } catch {
      /* 忽略 */
    }
  }

  const openUsage = () => {
    setSettingsSection("usage")
    setSettingsOpen(true)
  }
  // 点工具条「积分」行：进设置「账号」页签看余额与流水
  const openAccount = () => {
    setSettingsSection("account")
    setSettingsOpen(true)
  }
  // 模型选择器底部「配置模型」：直达设置「模型」页签
  const openModels = () => {
    setSettingsSection("model")
    setSettingsOpen(true)
  }

  // 输入框历史：Ctrl+Z 回退到之前发送过的消息、Ctrl(+Shift)+Z / Ctrl+Y 前进（用户要求）
  const sentHist = useRef<string[]>([])
  const [histBack, setHistBack] = useState(0) // 回退步数；0 = 停在最新
  const onInputUndo = () => {
    if (histBack >= sentHist.current.length) return
    const next = histBack + 1
    setHistBack(next)
    setDraftSeed((s) => ({ text: sentHist.current[sentHist.current.length - next], seq: s.seq + 1 }))
  }
  const onInputRedo = () => {
    if (histBack === 0) return
    const next = histBack - 1
    setHistBack(next)
    const text = next === 0 ? "" : sentHist.current[sentHist.current.length - next]
    setDraftSeed((s) => ({ text, seq: s.seq + 1 }))
  }
  const openSettings = () => {
    setSettingsSection(undefined)
    setSettingsOpen(true)
    // 后台可能刚启动：现探一次，通了设置里才出「账号」页签
    void checkServer()
  }
  // 上报一次 AI 用量到 omni-server：按模型倍率扣积分并留请求记录。
  // 成功即把最新余额写回工具条；402=积分不足时Toast 提醒，其余错误静默（不影响桌面端）
  const reportServerUsage = (model: string, input: number, output: number, status?: string, error?: string) => {
    server
      .reportUsage({ model, input_tokens: input, output_tokens: output, status, error })
      .then((r) => setServerUser((u) => (u ? { ...u, points: r.balance } : u)))
      .catch((e) => {
        if (e instanceof Error && (e as { status?: number }).status === 402) {
          showToast(t("toast.noCredit"))
        }
      })
  }
  const logout = async () => {
    try {
      await server.logout()
    } catch {
      /* 忽略 */
    }
    setServerToken("")
    setServerUser(null)
    // 「账号」页签登录后才存在，登出后直接关掉设置窗
    setSettingsOpen(false)
  }

  const focusInput = useCallback(() => setFocusSignal((n) => n + 1), [])

  // 运行状态表：taskId → RunState。可变 ref + 版本号触发渲染，
  // 后台事件随时 patch 后 bump()。
  const runsRef = useRef<Record<string, RunState>>({})
  const abortRefs = useRef<Record<string, AbortController>>({})
  const runPromises = useRef<Record<string, Promise<unknown>>>({})
  const [, setRunsV] = useState(0)
  const bump = useCallback(() => setRunsV((v) => v + 1), [])

  const idRef = useRef(0)
  const queueSeq = useRef(0)
  const nextId = () => ++idRef.current

  const refreshTasks = useCallback(async () => {
    try {
      const r = await api.tasks("all")
      setTasks(r.tasks ?? [])
    } catch (e) {
      fail(e)
    }
  }, [])

  const refreshProjects = useCallback(async () => {
    try {
      const r = await api.projects()
      setProjects(r.projects ?? [])
    } catch (e) {
      fail(e)
    }
  }, [])

  useEffect(() => {
    api.state().then((s) => setState(s)).catch(fail)
    void refreshTasks()
    void refreshProjects()
  }, [refreshTasks, refreshProjects])

  // 服务端事件推送：计划任务在后台新建/更新任务时实时刷新侧栏，无需轮询；
  // EventSource 断线会自动重连
  useEffect(() => {
    const es = new EventSource("/api/events")
    es.onmessage = (e) => {
      try {
        if (JSON.parse(e.data)?.type !== "tasks_changed") return
        void refreshTasks()
        // 当前打开的任务只要本地没有实时流（后台执行中的计划任务），就用服务端快照重建——
        // 后台每 3 秒落一次检查点并广播，工具卡/回复会逐段补进来；本地正在流式的任务绝不覆盖
        const id = activeTaskIdRef.current
        const r = id ? runsRef.current[id] : null
        if (!id || r?.streaming) return
        void api
          .task(id)
          .then((resp) => {
            const msgs = resp.task.messages ?? []
            if (msgs.length === 0 || activeTaskIdRef.current !== id) return
            const cur = runsRef.current[id]
            // 拉快照的往返里本地可能又开了一轮（上一轮结束、队列刚出队）：
            // 重建会把正在流式的实时进度整段换成旧快照，必须让位
            if (cur?.streaming) return
            // 快照落后于本地视图（末条还是未获回复的提问，本地挂着错误横幅）：
            // 错误不落服务端快照，重建会把横幅抹掉、只剩光秃秃的提问（表现为错误一闪而过）。
            // 保留本地视图，直到快照真正前进（其他客户端续跑）或下次发送。
            const lastLocal = cur?.items.at(-1)
            if (msgs[msgs.length - 1].role === "user" && lastLocal?.role === "assistant" && lastLocal.error) return
            const next = buildRun(resp.task)
            // 排队消息是纯客户端状态、不在服务端快照里：重建 RunState 时必须原样搬过来。
            // 轮次一结束 streaming 先置 false、要等 refreshTasks 回来才出队，这个空窗里落进的
            // 广播（整轮结束那一次正踩着点来）会把队列整段抹掉——排队消息就「突然没了」。
            next.queue = cur?.queue ?? []
            runsRef.current[id] = next
            bump()
          })
          .catch(() => {})
      } catch {
        /* 忽略坏帧 */
      }
    }
    return () => es.close()
  }, [refreshTasks, bump])

  // 主题跟随设置（外观模式）
  useEffect(() => {
    const theme: Theme = state?.theme ?? "light"
    const root = document.documentElement
    const apply = (dark: boolean) => {
      root.classList.toggle("dark", dark)
      setHljsTheme(dark)
    }
    if (theme === "system") {
      const mq = window.matchMedia("(prefers-color-scheme: dark)")
      apply(mq.matches)
      const fn = (e: MediaQueryListEvent) => apply(e.matches)
      mq.addEventListener("change", fn)
      return () => mq.removeEventListener("change", fn)
    }
    apply(theme === "dark")
    return undefined
  }, [state?.theme])

  useEffect(() => {
    const color = state?.theme_color ?? "zinc"
    const root = document.documentElement
    for (const c of ["zinc", "violet", "blue", "lime", "emerald", "amber", "rose"]) {
      root.classList.toggle(`theme-${c}`, c === color)
    }
  }, [state?.theme_color])

  // 模型列表：按供应商分组（一级 = 供应商名，二级 = 模型名），聊天底部二级联动选择。
  const [modelGroups, setModelGroups] = useState<ModelGroup[]>([])
  const refreshModels = useCallback(async () => {
    try {
      const r = await api.models()
      const groups: ModelGroup[] = []
      const byName = new Map<string, ModelGroup>()
      for (const m of r.models) {
        // 空供应商名保持空串，渲染侧统一回落到 i18n「其他」
        const provider = m.provider || ""
        let g = byName.get(provider)
        if (!g) {
          g = { provider, models: [] }
          byName.set(provider, g)
          groups.push(g)
        }
        g.models.push(m.model)
      }
      setModelGroups(groups)
    } catch {
      setModelGroups([])
    }
  }, [])
  useEffect(() => {
    void refreshModels()
  }, [refreshModels, state?.model])

  const changeModel = async (m: string) => {
    try {
      const r = await api.saveSettings({ model: m })
      if (state) setState({ ...state, model: r.model })
    } catch (e) {
      fail(e)
    }
  }

  const activeMeta = tasks.find((t) => t.id === activeTaskId)
  const activeProject = projects.find((p) => p.id === activeMeta?.project_id)

  // 技能列表按当前任务所属项目目录加载（全局技能始终包含在后端结果里）
  const activeProjectPath = activeProject?.path ?? ""
  useEffect(() => {
    let alive = true
    api
      .skills(activeProjectPath || undefined)
      .then((r) => {
        if (alive) setSkills(r.skills ?? [])
      })
      .catch(() => {
        if (alive) setSkills([])
      })
    return () => {
      alive = false
    }
  }, [activeProjectPath])

  // 从持久化消息构建运行状态。
  // 持久化结构本身就是「每条 assistant 消息 = 一轮」，天然按轮次拆开；
  // roundBase 记录最大轮次，后续实时事件的 round 从它之后接着数，避免与历史撞号。
  const buildRun = (t: TaskFull): RunState => {
    const r = newRun(t.title, t.mode === "plan" ? "plan" : "build")
    const out: ViewItem[] = []
    let cur: ViewItem | null = null
    let round = 0
    let runMedia: AttachmentInfo[] = []
    // 本轮（两条用户消息之间）的来源站点：从工具调用参数里推导，挂到该轮最终回复上
    let turnSources: string[] = []
    const seenSources = new Set<string>()
    const flushSources = () => {
      if (turnSources.length === 0) return
      for (let i = out.length - 1; i >= 0; i--) {
        if (out[i].role === "assistant") {
          out[i].sources = [...turnSources]
          break
        }
      }
      turnSources = []
      seenSources.clear()
    }
    for (const m of t.messages ?? []) {
      if (m.role === "user") {
        flushSources()
        // 历史里附件以「[附件] + 路径列表」附在正文后：展示时拆出来，只显示文件名
        let text = m.text ?? ""
        let atts: string[] | undefined
        const marker = "\n\n[附件]"
        const at = text.indexOf(marker)
        if (at >= 0) {
          atts = text
            .slice(at + marker.length)
            .split("\n")
            .map((l) => l.replace(/^\d+\.\s*/, "").trim().split(/[\\/]/).pop() ?? "")
            .filter(Boolean)
          text = text.slice(0, at)
        }
        cur = { id: nextId(), role: "user", text, attachments: atts, tools: [] }
        out.push(cur)
      } else if (m.role === "assistant") {
        cur = { id: nextId(), role: "assistant", text: m.text ?? "", tools: [], round: round++ }
        // 历史里 persisted 的整轮工作耗时/模型/输出 token：重开软件后回复末行仍显示统计
        if (m.elapsed_ms != null) cur.elapsedMs = m.elapsed_ms
        if (m.reply_model) cur.replyModel = m.reply_model
        if (m.output_tokens) cur.outTokens = m.output_tokens
        for (const tc of m.tool_calls ?? []) {
          cur.tools.push({
            id: tc.id,
            name: tc.name,
            args: JSON.stringify(tc.arguments ?? {}, null, 2),
            output: "",
            isError: false,
            running: false,
          })
          extractExternalURLs(tc.arguments, turnSources, seenSources)
        }
        out.push(cur)
      } else if (m.role === "tool" && m.tool_result && cur) {
        const run = cur.tools.find((x) => x.id === m.tool_result!.call_id)
        if (run) {
          run.output = m.tool_result.content
          run.isError = m.tool_result.is_error ?? false
        }
        // 工具产出的多媒体随历史恢复：先收集，最后统一挂到最后一条回答（与实时展示一致）
        if (m.tool_result.attachments?.length) {
          runMedia = [...runMedia, ...m.tool_result.attachments.map(attachmentToInfo)]
        }
      }
    }
    flushSources()
    if (runMedia.length > 0) {
      const lastAssistant = [...out].reverse().find((x) => x.role === "assistant")
      if (lastAssistant) {
        lastAssistant.media = [...(lastAssistant.media ?? []), ...runMedia]
      } else {
        out.push({ id: nextId(), role: "assistant", text: "", tools: [], media: runMedia })
      }
    }
    r.items = out
    r.roundBase = round
    return r
  }

  const openTask = async (id: string) => {
    setActiveTaskId(id)
    setView("chat")
    focusInput()
    // 点开任务即归底：含重开当前任务（标题不变时 [title] 效果不会触发）
    setTaskOpenSignal((n) => n + 1)
    // 已有运行状态（可能正在后台执行）：直接复用，绝不用服务端快照覆盖实时进度
    if (runsRef.current[id]) {
      bump()
      return
    }
    try {
      const r = await api.task(id)
      runsRef.current[id] = buildRun(r.task)
      bump()
    } catch (e) {
      fail(e)
    }
  }

  const newTask = async (projectId?: string) => {
    try {
      const r = await api.createTask(projectId)
      await refreshTasks()
      runsRef.current[r.task.id] = newRun(r.task.title, defaultMode)
      setActiveTaskId(r.task.id)
      setView("chat")
      focusInput()
      bump()
    } catch (e) {
      fail(e)
    }
  }

  // 全局快捷键：Ctrl+N（macOS 为 Cmd+N）新建任务；用 ref 持有最新的 newTask，避免每次渲染重绑监听
  const newTaskRef = useRef(newTask)
  newTaskRef.current = newTask
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "n") {
        e.preventDefault()
        void newTaskRef.current()
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  // 点「任务」TAB：已有任务就选中最近的一条，没有则直接新建
  // tasks 已由后端按「最近回复完成时间（SortKey）」降序返回，第一条即最近
  const focusTasksTab = async () => {
    const list = tasks.filter((t) => !t.project_id)
    if (list.length > 0) await openTask(list[0].id)
    else await newTask()
  }

  const deleteTask = async (id: string) => {
    // 正在跑的任务：先掐掉 SSE 再删
    abortRefs.current[id]?.abort()
    delete runsRef.current[id]
    if (activeTaskId === id) setActiveTaskId(null)
    try {
      await api.deleteTask(id)
    } catch (e) {
      fail(e)
    }
    await refreshTasks()
  }

  const deleteProject = async (id: string) => {
    await api.deleteProject(id)
    await refreshProjects()
    await refreshTasks()
  }

  // 在系统文件管理器中打开项目文件夹
  const openProject = async (p: Project) => {
    try {
      await api.openProject(p.id)
    } catch (e) {
      fail(e)
    }
  }

  // 桌面端：直接弹原生文件夹选择
  const addProjectNative = async () => {
    const path = await window.omniDesktop?.pickFolder()
    if (!path) return
    try {
      await api.addProject(path)
      await refreshProjects()
    } catch (e) {
      fail(e)
    }
  }

  // 取（或创建）某一轮的 assistant 消息项。
  // 后端 round 从 0 开始，加上该任务的 roundBase 偏移量，保证与已加载的消息不冲突。
  const slotOf = (items: ViewItem[], r: RunState, round: number): ViewItem => {
    const target = r.roundBase + round
    let cur = items[items.length - 1]
    if (!cur || cur.role !== "assistant" || cur.round !== target) {
      cur = { id: nextId(), role: "assistant", text: "", tools: [], round: target }
      items.push(cur)
    }
    return cur
  }

  // 在指定轮次里找工具卡片：优先按 call_id 精确匹配，否则退回该轮最后一张
  const findRun = (items: ViewItem[], r: RunState, round: number, callId: string) => {
    const target = r.roundBase + round
    const cur = items.find((it) => it.role === "assistant" && it.round === target)
    if (!cur) return undefined
    return cur.tools.find((t) => t.id === callId) ?? cur.tools.at(-1)
  }

  // 生成某任务的 SSE 事件处理器：所有更新写进该任务自己的 RunState，
  // 与当前显示哪个任务无关——后台任务照常在各自状态里推进。
  // model = 本轮发送时选中的模型（携带来源：内置模型带 server: 前缀）。
  // 用户中途切换模型不改它——上报只认实际发起请求的那一个。
  const makeHandler = (tid: string, model: string) => (ev: ChatEvent) => {
    // 仅内置模型上报后台计积分；用户自配模型不记录、不计费
    const builtin = model.startsWith("server:") ? model.slice(7) : ""
    const r = runsRef.current[tid]
    if (!r) return
    if ("round" in ev) {
      r.lastRound = Math.max(r.lastRound, ev.round)
    }
    // 重试指示：模型恢复活动（任何非 retry 事件）即清掉，指示器回到「处理中…」
    if (ev.type !== "retry") r.retry = undefined
    switch (ev.type) {
      case "start":
        r.title = ev.title
        r.mode = ev.mode === "plan" ? "plan" : "build"
        // 任务名立即跟随（后端按首条消息生成标题），不等本轮跑完才刷新；
        // 首条消息直接发的任务此时才首次进入侧栏列表（之前是「未命名任务」不上列表）
        if (ev.title) {
          setTasks((ts) => {
            const i = ts.findIndex((t) => t.id === tid)
            if (i < 0) {
              const now = new Date().toISOString()
              return [{ id: tid, title: ev.title, created_at: now, updated_at: now }, ...ts]
            }
            const copy = [...ts]
            copy[i] = { ...copy[i], title: ev.title }
            return copy
          })
        }
        break
      case "text":
        r.thinking = false
        {
          const items = [...r.items]
          const cur = slotOf(items, r, ev.round)
          cur.text += ev.text
          // 正文开始输出：本轮思考结束
          if (cur.thinkingActive) cur.thinkingActive = false
          r.items = items
        }
        break
      case "retry":
        // 上游瞬时故障、provider 自动重试中：挂在运行状态上，由「处理中」指示器显示
        r.retry = { attempt: ev.attempt, max: ev.max }
        break
      case "thinking":
        {
          // 思考/推理增量：挂到本轮 assistant 项上，供「思考」折叠行展示
          const items = [...r.items]
          const cur = slotOf(items, r, ev.round)
          cur.thinking = (cur.thinking ?? "") + ev.text
          cur.thinkingActive = true
          r.items = items
        }
        break
      case "tool_start":
        r.thinking = false
        {
          const items = [...r.items]
          const cur = slotOf(items, r, ev.round)
          // 工具开始执行：本轮思考结束
          if (cur.thinkingActive) cur.thinkingActive = false
          // 同一 id 重复到达时不要重复插入
          if (!cur.tools.some((t) => t.id === ev.id)) {
            cur.tools.push({
              id: ev.id,
              name: ev.name,
              args: ev.args,
              output: "",
              isError: false,
              running: true,
            })
          }
          // 从工具参数里收集外部 URL，整轮结束后作为「来源」挂在最终回复上
          try {
            const urls = extractExternalURLs(JSON.parse(ev.args))
            for (const u of urls) {
              if (!r.pendingSources) r.pendingSources = []
              if (!r.pendingSources.includes(u)) r.pendingSources.push(u)
            }
          } catch {
            // args 不是合法 JSON（理论不会），跳过来源收集
          }
          r.items = items
        }
        break
      case "write_base":
        // write 覆盖已有文件：后端在写入前抓的旧内容，挂到卡片渲染差异
        {
          const items = [...r.items]
          const run = findRun(items, r, ev.round, ev.id)
          if (run) run.baseOld = ev.old
          r.items = items
        }
        break
      case "tool_output":
        {
          const items = [...r.items]
          const run = findRun(items, r, ev.round, ev.id)
          if (run) {
            run.output = ev.output
            run.isError = ev.is_error
          }
          r.items = items
        }
        break
      case "media":
        // 本轮产出的多媒体：挂到当前助手消息，在最终回答里集中展示
        {
          const items = [...r.items]
          const cur = slotOf(items, r, ev.round)
          cur.media = [...(cur.media ?? []), ...ev.attachments]
          r.items = items
        }
        break
      case "tool_end":
        // 一轮工具跑完，模型要重新推理下一轮：重新显示思考动画，避免空窗期看着像卡住
        r.thinking = true
        {
          const items = [...r.items]
          const run = findRun(items, r, ev.round, ev.id)
          if (run) {
            run.running = false
            run.isError = ev.is_error
          }
          r.items = items
        }
        break
      case "approval_pending":
        // 工具卡片已出现，这里只清掉思考动画
        r.thinking = false
        break
      case "approval":
        // 普通访问模式：把待确认请求挂到对应工具卡片上
        {
          const items = [...r.items]
          for (let i = items.length - 1; i >= 0; i--) {
            const cur = items[i]
            if (cur.role !== "assistant") continue
            const run = cur.tools.find((t) => t.id === ev.call_id) ?? cur.tools.at(-1)
            if (run) {
              run.approvalId = ev.id
              run.approvalChoice = null
            }
            break
          }
          r.items = items
        }
        break
      case "approval_result":
        {
          const items = [...r.items]
          for (const it of items) {
            for (const run of it.tools) {
              if (run.approvalId === ev.id) {
                run.approvalChoice = ev.allowed
                run.denied = !ev.allowed
              }
            }
          }
          r.items = items
        }
        break
      case "question_pending":
        // ask 工具卡片已出现，问题事件紧随其后；清掉思考动画
        r.thinking = false
        break
      case "question":
        // 把提问挂到对应工具卡，横幅显示在输入框上方
        {
          const items = [...r.items]
          for (let i = items.length - 1; i >= 0; i--) {
            const cur = items[i]
            if (cur.role !== "assistant") continue
            const run = cur.tools.find((t) => t.id === ev.call_id) ?? cur.tools.at(-1)
            if (run) {
              run.questionId = ev.id
              run.question = ev.question
              run.questionOptions = ev.options
              run.questionAnswer = undefined
            }
            break
          }
          r.items = items
        }
        break
      case "question_answered":
        {
          const items = [...r.items]
          for (const it of items) {
            for (const run of it.tools) {
              if (run.questionId === ev.id) run.questionAnswer = ev.answer
            }
          }
          r.items = items
        }
        break
      case "error":
        {
          const items = [...r.items]
          slotOf(items, r, ev.round).error = ev.error
          r.items = items
          // 上游失败也上报后台（不计费），管理端能看到失败请求；仅内置模型
          if (serverToken() && builtin) {
            reportServerUsage(builtin, 0, 0, "error", ev.error)
          }
        }
        break
      case "done":
        // 登录了 omni-server：本次问答的 token 用量上报后台，按模型倍率扣积分并留痕；仅内置模型
        if (serverToken() && builtin && (ev.input_tokens || ev.output_tokens)) {
          reportServerUsage(ev.model || builtin, ev.input_tokens ?? 0, ev.output_tokens ?? 0)
        }
        // 把产出模型与输出 token 挂到本轮最后一条回复：回复末行「模型名 · Token速度 · 耗时」
        {
          const items = [...r.items]
          for (let i = items.length - 1; i >= 0; i--) {
            if (items[i].role === "assistant") {
              items[i] = { ...items[i], replyModel: ev.model, outTokens: ev.output_tokens ?? 0 }
              break
            }
          }
          r.items = items
        }
        break
    }
    bump()
  }

  // 真正发起一轮问答。同一任务串行（streaming 期间不并发），不同任务并行。
  // attachments = 已上传附件的绝对路径（随文本一起发给模型），attNames = 展示用文件名。
  // replaceLast：AI 还没回复上一条时，重发直接替换最后一对 user+assistant（在当前行上改），不追加新行
  // 返回值：本轮是否真正开跑（"ok" 开跑 / "busy" 另一端正跑 409 未开跑 / "skip" 未满足发送条件），
  // flushQueue 据此决定排队消息放回重试还是丢掉。
  const doSend = async (
    tid: string,
    text: string,
    attachments: string[] = [],
    attNames?: string[],
    replaceLast = false,
  ): Promise<"ok" | "busy" | "skip"> => {
    const r = runsRef.current[tid]
    if (!r || r.streaming) return "skip"
    let res: "ok" | "busy" = "ok"
    // 输入框历史：每次实际发送都记账，供 Ctrl+Z 回退
    if (text) sentHist.current.push(text)
    setHistBack(0)
    const ac = new AbortController()
    abortRefs.current[tid] = ac
    const baseAtSend = r.roundBase
    // 本轮开始工作的时刻：记录到本任务上，结束时据此算出「已工作」耗时；
    // 由 Chat 在本任务仍在流式生成时渲染动态的「工作中 N 秒」
    r.workSince = Date.now()
    // 本轮来源站点从空开始收集
    r.pendingSources = []
    r.streaming = true
    r.thinking = true
    r.lastRound = -1
    if (replaceLast) {
      const items = [...r.items]
      const userItem = items[items.length - 2]
      const asstItem = items[items.length - 1]
      if (userItem && userItem.role === "user" && asstItem && asstItem.role === "assistant") {
        userItem.text = text
        userItem.attachments = attNames && attNames.length > 0 ? attNames : undefined
        asstItem.text = ""
        asstItem.error = undefined
        asstItem.tools = []
        asstItem.media = undefined
        asstItem.thinking = undefined
        asstItem.thinkingActive = false
        asstItem.sources = undefined
        asstItem.round = r.roundBase
        r.items = items
        bump()
      }
    } else {
      const userItem: ViewItem = {
        id: nextId(),
        role: "user",
        text,
        attachments: attNames && attNames.length > 0 ? attNames : undefined,
        tools: [],
      }
      // 预置第 0 轮的占位项：后端 round 从 0 起算，正好接上
      const asstItem: ViewItem = { id: nextId(), role: "assistant", text: "", tools: [], round: r.roundBase }
      r.items = [...r.items, userItem, asstItem]
      bump()
    }
    try {
      const model = state?.model ?? ""
      await streamChat(tid, text, r.mode, model, makeHandler(tid, model), ac.signal, attachments)
    } catch (e) {
      if (!ac.signal.aborted) {
        const msg = e instanceof Error ? e.message : String(e)
        // 另一端正跑（409）：本轮没真正开起来，不报错也不进历史——由 flushQueue 保留队列等空闲重试
        if (msg.includes("另一端运行中")) {
          res = "busy"
          sentHist.current = sentHist.current.slice(0, -1)
          setHistBack(0)
        } else {
          const items = [...r.items]
          const cur = items[items.length - 1]
          if (cur && cur.role === "assistant") cur.error = msg
          r.items = items
        }
      }
    } finally {
      delete abortRefs.current[tid]
      const rr = runsRef.current[tid]
      if (rr) {
        // 本轮结束（完成/出错/手动中断）：把还没等到 tool_end 的工具卡一并停掉，
        // 否则出错后卡片和「执行中」分组会一直转圈；思考中状态同样收尾
        for (const it of rr.items) {
          for (const t of it.tools) {
            if (t.running) t.running = false
          }
          if (it.thinkingActive) it.thinkingActive = false
        }
        // 上游正常结束却什么都没回（文本/工具/错误全空）：给个兜底提示，避免「加载一下就消失」
        const last = rr.items.at(-1)
        if (!ac.signal.aborted && last?.role === "assistant" && !last.text && !last.error && last.tools.length === 0 && !last.media?.length) {
          const items = [...rr.items]
          items[items.length - 1] = { ...last, error: t("a.emptyReply") }
          rr.items = items
        }
        // 本轮用掉多少轮就往后推多少，保证下次提问的轮次不与本次重叠
        rr.roundBase = Math.max(rr.roundBase, baseAtSend + rr.lastRound + 1)
        // 记录本轮「已工作」耗时与来源站点：写到本轮最后一条 assistant 回复上
        for (let i = rr.items.length - 1; i >= 0; i--) {
          if (rr.items[i].role === "assistant") {
            rr.items[i].elapsedMs = rr.workSince != null ? Date.now() - rr.workSince : undefined
            if (rr.pendingSources?.length) rr.items[i].sources = [...rr.pendingSources]
            break
          }
        }
        rr.pendingSources = undefined
        rr.streaming = false
        rr.thinking = false
        rr.retry = undefined
        rr.workSince = undefined
        bump()
      }
      await refreshTasks()
      // 侧栏项目按"最后对话时间"排序，对话后也要重排
      void refreshProjects()
      // 正常跑完（非中断）→ 自动发下一条排队消息；手动停止/中断不发；busy 未开跑由 flushQueue 兜底
      if (!ac.signal.aborted && res === "ok") void flushQueue(tid)
    }
    return res
  }

  // 发送入口：有附件先上传（拿到落盘路径），再发起一轮问答。
  const launch = (tid: string, text: string, files?: File[], replaceLast = false): Promise<"ok" | "busy" | "skip"> => {
    const p = (async () => {
      let paths: string[] = []
      let names: string[] = []
      if (files && files.length > 0) {
        try {
          const up = await api.upload(tid, files)
          paths = up.files.map((f) => f.path)
          names = up.files.map((f) => f.name)
        } catch (e) {
          fail(e)
          return "skip"
        }
      }
      return doSend(tid, text, paths, names, replaceLast)
    })()
    runPromises.current[tid] = p
    void p.finally(() => {
      if (runPromises.current[tid] === p) delete runPromises.current[tid]
    })
    return p
  }

  // 队列头自动出队（当前轮正常结束后调用 / 兜底扫描触发）。
  // 同一任务同一时刻只允许一条在队列上工作（互斥锁），杜绝并发双发；
  // 本轮没真正开跑（另一端正跑 409 等）时放回队首，由兜底扫描在任务空闲后自动补发——
  // 排队消息绝不弄丢、不静默消失，也绝不紧循环。
  const flushing = useRef<Set<string>>(new Set())
  const flushQueue = async (tid: string) => {
    if (flushing.current.has(tid)) return
    const r = runsRef.current[tid]
    if (!r || r.streaming || r.queue.length === 0) return
    flushing.current.add(tid)
    try {
      const [head, ...rest] = r.queue
      r.queue = rest
      bump()
      const res = await launch(tid, head.text, head.files)
      if (res !== "ok") {
        const rr = runsRef.current[tid]
        if (rr) {
          rr.queue = [head, ...rr.queue]
          bump()
        }
      }
    } finally {
      flushing.current.delete(tid)
    }
  }

  // 队列兜底扫描：无论发起的 flush 链是否被瞬时异常打乱，这里有排队的任务且空闲时
  // 就尝试出队发送（唯一的重试时钟，避免多个定时器紧循环）。保证排队消息最终一定发出
  useEffect(() => {
    const t = setInterval(() => {
      for (const tid of Object.keys(runsRef.current)) {
        const r = runsRef.current[tid]
        if (r && !r.streaming && r.queue.length > 0) void flushQueue(tid)
      }
    }, 2000)
    return () => clearInterval(t)
    // flushQueue 用 ref 读实时状态且不在闭包里变，只挂一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 输入框「发送」入口：空闲直接发；处理中（或已有排队）进队列。附件随消息一起发。
  const submitText = (text: string, files?: File[]) => {
    const t = text.trim()
    if (!t && (!files || files.length === 0)) return
    if (!activeTaskId) {
      // 空状态第一条消息：先建任务再发（标题等首条消息发出后由后端生成，列表届时才出现）
      void (async () => {
        try {
          const c = await api.createTask("")
          await refreshTasks()
          runsRef.current[c.task.id] = newRun(c.task.title, defaultMode)
          setActiveTaskId(c.task.id)
          await launch(c.task.id, t, files)
        } catch (e) {
          fail(e)
        }
      })()
      return
    }
    const r = runsRef.current[activeTaskId]
    if (!r) return
    if (r.streaming || r.queue.length > 0) {
      r.queue = [...r.queue, { id: ++queueSeq.current, text: t, files }]
      bump()
      if (!r.streaming) void flushQueue(activeTaskId)
    } else {
      // AI 还没回复上一条（末条 user 后面跟的是空占位）：重发 = 替换当前行重跑一遍，不追加新消息
      const last = r.items.at(-1)
      const prev = r.items[r.items.length - 2]
      const unanswered =
        last?.role === "assistant" &&
        prev?.role === "user" &&
        !last.text &&
        last.tools.length === 0 &&
        !last.media?.length
      void launch(activeTaskId, t, files, unanswered)
    }
  }

  // 停止只停当前这一轮对话；排队消息仍在任务空闲后继续逐个自动处理
  const stop = () => {
    if (activeTaskId) abortRefs.current[activeTaskId]?.abort()
  }

  // 队列「发送」：中断当前轮，立即发这条。
  // 插回队首而不是自己 launch：当前轮 abort 后要等它收尾 streaming 才置 false，那段里
  // 兜底扫描可能已把队首发出去，这边直接 launch 只会拿到 "skip" 把本条悄悄丢掉；
  // 统一走 flushQueue 出队，无论谁抢到头部，发的都是同一条。
  const dispatchQueued = async (qid: number) => {
    const tid = activeTaskId
    if (!tid) return
    const r = runsRef.current[tid]
    if (!r) return
    const item = r.queue.find((q) => q.id === qid)
    if (!item) return
    r.queue = [item, ...r.queue.filter((q) => q.id !== qid)]
    bump()
    abortRefs.current[tid]?.abort()
    const p = runPromises.current[tid]
    if (p) {
      try {
        await p
      } catch {
        /* 中断的错误忽略 */
      }
    }
    void flushQueue(tid)
  }

  const editQueued = (qid: number) => {
    const tid = activeTaskId
    const r = tid ? runsRef.current[tid] : null
    if (!r) return
    const item = r.queue.find((q) => q.id === qid)
    if (!item) return
    r.queue = r.queue.filter((q) => q.id !== qid)
    bump()
    setDraftSeed((s) => ({ text: item.text, seq: s.seq + 1 }))
  }

  const removeQueued = (qid: number) => {
    const tid = activeTaskId
    const r = tid ? runsRef.current[tid] : null
    if (!r) return
    r.queue = r.queue.filter((q) => q.id !== qid)
    bump()
  }

  // 答复一次写操作确认（普通访问模式）
  const approve = async (id: string, allow: boolean) => {
    const r = activeTaskId ? runsRef.current[activeTaskId] : null
    if (r) {
      const items = [...r.items]
      for (const it of items) {
        for (const run of it.tools) {
          if (run.approvalId === id) run.approvalChoice = allow
        }
      }
      r.items = items
      bump()
    }
    try {
      await api.approval(id, allow)
    } catch (e) {
      fail(e)
    }
  }

  // 回答一次 ask 工具的提问
  const answer = async (id: string, text: string) => {
    const r = activeTaskId ? runsRef.current[activeTaskId] : null
    if (r) {
      const items = [...r.items]
      for (const it of items) {
        for (const run of it.tools) {
          if (run.questionId === id) run.questionAnswer = text
        }
      }
      r.items = items
      bump()
    }
    try {
      await api.answer(id, text)
    } catch (e) {
      fail(e)
    }
  }

  const changeMode = (m: "build" | "plan") => {
    setDefaultMode(m)
    const r = activeTaskId ? runsRef.current[activeTaskId] : null
    if (r) {
      r.mode = m
      bump()
    }
  }

  // 还没发过首条消息的任务（标题仍是「未命名任务」）不上侧栏列表，
  // 等首条对话发出、后端按消息生成标题后才出现
  const listedTasks = tasks.filter((t) => t.title !== "未命名任务")
  // 项目内任务归到项目下；无项目归属的任务才进「任务」TAB
  const tasksByProject: Record<string, TaskMeta[]> = {}
  for (const t of listedTasks) {
    if (!t.project_id) continue
    ;(tasksByProject[t.project_id] ??= []).push(t)
  }
  const standaloneTasks = listedTasks.filter((t) => !t.project_id)

  // 侧栏运行指示：任务 → 状态文字（处理中/重试 n/10）。
  // 服务端标记（计划任务后台执行）+ 本地实时状态（交互对话、重试进度）合并；
  // 后台执行的任务在任务 TAB 里也能一眼看出状况
  const running: Record<string, string> = {}
  for (const t2 of tasks) {
    if (t2.running) running[t2.id] = t("a.processing")
  }
  for (const [id, r] of Object.entries(runsRef.current)) {
    if (r.streaming) running[id] = r.retry ? t("a.retry", { n: r.retry.attempt, max: r.retry.max }) : t("a.processing")
  }

  const run = activeTaskId ? runsRef.current[activeTaskId] : undefined
  // 后台无人值守执行中（计划任务）：本地没有实时流，也让聊天区显示「处理中」指示，
  // 否则执行期间点开任务只会看到一条孤零零的提问
  const backgroundRunning = !!activeMeta?.running && !run?.streaming
  const chatTitle = run?.title || activeMeta?.title || t("a.newTask")
  const chatSource = activeProject?.name ?? t("a.taskSource")

  const emptyHint = (
    <div className="text-muted-foreground flex flex-col items-center gap-3 py-24 text-center">
      <div className="text-lg font-medium">{t("a.workbench")}</div>
      <div className="max-w-md text-sm">
        {t("a.empty1")}
        <br />
        {t("a.empty2")}
      </div>
    </div>
  )

  return (
    // h-dvh 而非 h-screen：手机浏览器地址栏收起时 100vh 大于可见区，整页能滚、
    // 输入框底部被顶出屏幕；dvh 跟随实际可见高度（桌面端两者相等，无差别）。
    <div className="relative flex h-dvh flex-col overflow-hidden pr-[env(safe-area-inset-right)]">
      {/* 凭证失效整屏接管：换发 token 后旧设备的所有请求都是 401，
          与其让用户对着空列表猜，不如明确让他回电脑端重新扫码。 */}
      {unauth && (
        <div className="bg-background absolute inset-0 z-[100] flex flex-col items-center justify-center gap-3 px-8 text-center">
          <ShieldOffIcon className="text-muted-foreground size-8" />
          <div className="text-base font-semibold">{t("a.unauthTitle")}</div>
          <p className="text-muted-foreground max-w-xs text-sm leading-relaxed">
            {t("a.unauthDesc")}
          </p>
          <Button className="mt-2" onClick={() => window.location.reload()}>
            {t("a.unauthRetry")}
          </Button>
        </div>
      )}
      {isDesktop && (
        <Titlebar
          source={chatSource}
          title={chatTitle}
          browserOpen={browserOpen}
          hasUpdate={!!updateSt && (!!updateSt.latest || !!updateSt.progress || updateSt.canInstall)}
          updateStateText={
            updateSt?.canInstall
              ? t("ud.ready")
              : updateSt?.progress
                ? t("ud.downloading")
                : updateSt?.latest
                  ? t("ud.found", { v: updateSt.latest.version })
                  : undefined
          }
          onUpdateClick={() => setUpgradeOpen(true)}
        />
      )}
      <div className="flex min-h-0 flex-1">
        <Sidebar
          tasks={standaloneTasks}
          projects={projects}
          tasksByProject={tasksByProject}
          activeTaskId={activeTaskId ?? null}
          running={running}
          serverOnline={serverOnline}
          user={serverUser}
          onOpenAccount={openAccount}
          open={sidebarOpen}
          onOpenTask={(id) => void openTask(id)}
          onDeleteTask={(id) => void deleteTask(id)}
          onNewTask={(projectId) => void newTask(projectId)}
          onDeleteProject={(id) => void deleteProject(id)}
          onOpenProject={(p) => void openProject(p)}
          onAddProject={() => (isDesktop ? void addProjectNative() : setAddProjectOpen(true))}
          onOpenSettings={() => void openSettings()}
          onOpenSchedules={() => setView("schedules")}
          onFocusTasksTab={() => void focusTasksTab()}
          onClose={() => setSidebarOpen(false)}
        />
        {view === "schedules" ? (
          <SchedulesPage
            models={modelGroups}
            projects={projects}
            onRan={() => {
              void refreshTasks()
              setTimeout(() => void refreshTasks(), 2000)
            }}
            onBack={() => setView("chat")}
          />
        ) : (
        <Chat
          title={chatTitle}
          source={chatSource}
          mode={run?.mode ?? defaultMode}
          items={run?.items ?? []}
          streaming={(run?.streaming ?? false) || backgroundRunning}
          thinking={run?.thinking ?? false}
          retry={run?.retry}
          workSince={run?.workSince}
          queue={run?.queue ?? []}
          draftSeed={draftSeed}
          models={modelGroups}
          model={state?.model ?? ""}
          skills={skills}
          focusSignal={focusSignal}
          taskOpenSignal={taskOpenSignal}
          emptyHint={emptyHint}
          onSend={submitText}
          onStop={stop}
          onQueueDispatch={(qid) => void dispatchQueued(qid)}
          onQueueEdit={editQueued}
          onQueueRemove={removeQueued}
          onEditMessage={(t) => setDraftSeed((s) => ({ text: t, seq: s.seq + 1 }))}
          onApprove={(id, allow) => void approve(id, allow)}
          onAnswer={(id, text) => void answer(id, text)}
          onModeChange={changeMode}
          onModelChange={(m) => void changeModel(m)}
          onOpenSidebar={() => {
            setSidebarOpen(true)
            focusInput()
          }}
          browserOpen={browserOpen}
          onToggleBrowser={() => setBrowserOpen((v) => !v)}
          serverUser={serverUser}
          onConfigureModels={openModels}
          onInputUndo={onInputUndo}
          onInputRedo={onInputRedo}
        />
        )}
        {/* 内置浏览器面板：仅桌面端（Electron 壳有 WebContentsView 与操作桥） */}
        {isDesktop && <BrowserPane open={browserOpen} onOpenChange={setBrowserOpen} />}
      </div>
      <SettingsDialog
        open={settingsOpen}
        initialSection={settingsSection}
        onOpenChange={(o) => {
          setSettingsOpen(o)
          // 关闭时再刷一次模型列表：兜底覆盖"改了配置但没点保存"等路径，
          // 保证聊天底部的选择器与设置里看到的一致
          if (!o) void refreshModels()
        }}
        state={state}
        onStateChange={setState}
        onProvidersSaved={() => void refreshModels()}
        serverUser={serverUser}
        serverOnline={serverOnline}
        onDomainSaved={() => void checkServer()}
        onLogout={() => void logout()}
        onOpenLogin={() => setLoginOpen(true)}
      />
      <AddProjectDialog
        open={addProjectOpen}
        onOpenChange={setAddProjectOpen}
        onAdded={() => void refreshProjects()}
      />
      <ServerLoginDialog open={loginOpen} onOpenChange={setLoginOpen} onLoggedIn={setServerUser} />
      {upgradeOpen && updateSt && updateSt.latest && (
        <UpgradeDialog st={updateSt} onClose={() => setUpgradeOpen(false)} />
      )}
      {/* 快捷工具悬浮球：侧边栏可见时显示（桌面端与宽屏浏览器常驻，H5 仅抽屉展开时）；
          侧边栏收起时它正好压住输入框左下角的附件按钮，所以不渲染。 */}
      {sidebarVisible && (
        <OmniToolbar
          zoom={zoom}
          onZoom={setZoom}
          currentTheme={themePreset()}
          onTheme={(id) => void changeThemePreset(id)}
          onOpenUsage={openUsage}
          onOpenAccount={openAccount}
          onCheckServer={() => void checkServer()}
          user={serverUser}
          serverOnline={serverOnline}
          onOpenLogin={() => setLoginOpen(true)}
        />
      )}
      {toast && (
        <div className="border-ring/30 bg-card text-foreground fixed bottom-4 left-1/2 z-50 -translate-x-1/2 rounded-md border px-4 py-2 text-sm shadow-lg">
          {toast}
        </div>
      )}
      {loadErr && (
        <div className="bg-destructive text-destructive-foreground fixed bottom-4 left-1/2 z-50 -translate-x-1/2 rounded-md px-4 py-2 text-sm shadow-lg">
          {loadErr}
          <button className="ml-3 underline cursor-pointer" onClick={() => setLoadErr("")}>
            {t("a.close")}
          </button>
        </div>
      )}
    </div>
  )
}
