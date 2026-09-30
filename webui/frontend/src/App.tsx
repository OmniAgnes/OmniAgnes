import { useCallback, useEffect, useRef, useState } from "react"

import { AddProjectDialog } from "@/components/AddProjectDialog"
import { Chat, type QueuedMsg, type ViewItem } from "@/components/Chat"
import { Sidebar } from "@/components/Sidebar"
import { SettingsDialog } from "@/components/SettingsDialog"
import { Titlebar } from "@/components/Titlebar"
import { Button } from "@/components/ui/button"
import { api, streamChat } from "@/lib/api"
import { setHljsTheme } from "@/lib/hljs"
import type { ChatEvent, Project, SkillInfo, StateInfo, TaskFull, TaskMeta, Theme } from "@/types"

const isDesktop = typeof window !== "undefined" && Boolean(window.omniDesktop)

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
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [addProjectOpen, setAddProjectOpen] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [loadErr, setLoadErr] = useState("")
  // 递增即让右侧输入框聚焦（新建任务、打开任务、点「任务」TAB）
  const [focusSignal, setFocusSignal] = useState(0)
  // 当前项目可用技能（输入 / 时提示补全）
  const [skills, setSkills] = useState<SkillInfo[]>([])
  // 无任务时（首次发送会即时建任务）新任务默认用的访问模式；有任务时以 run.mode 为准
  const [defaultMode, setDefaultMode] = useState<"build" | "plan">("build")
  // 队列「编辑」：把文本回填输入框（seq 递增触发）
  const [draftSeed, setDraftSeed] = useState<{ text: string; seq: number }>({ text: "", seq: 0 })

  const focusInput = useCallback(() => setFocusSignal((n) => n + 1), [])

  // 运行状态表：taskId → RunState。可变 ref + 版本号触发渲染，
  // 后台事件随时 patch 后 bump()。
  const runsRef = useRef<Record<string, RunState>>({})
  const abortRefs = useRef<Record<string, AbortController>>({})
  const runPromises = useRef<Record<string, Promise<void>>>({})
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
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }, [])

  const refreshProjects = useCallback(async () => {
    try {
      const r = await api.projects()
      setProjects(r.projects ?? [])
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    api.state().then((s) => setState(s)).catch((e) => setLoadErr(String(e)))
    void refreshTasks()
    void refreshProjects()
  }, [refreshTasks, refreshProjects])

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
    for (const c of ["zinc", "violet", "blue", "emerald", "amber", "rose"]) {
      root.classList.toggle(`theme-${c}`, c === color)
    }
  }, [state?.theme_color])

  // 模型列表：按供应商分组（一级 = 供应商名，二级 = 模型名），聊天底部二级联动选择
  const [modelGroups, setModelGroups] = useState<ModelGroup[]>([])
  const refreshModels = useCallback(async () => {
    try {
      const r = await api.models()
      const groups: ModelGroup[] = []
      const byName = new Map<string, ModelGroup>()
      for (const m of r.models) {
        const provider = m.provider || "其他"
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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
    for (const m of t.messages ?? []) {
      if (m.role === "user") {
        cur = { id: nextId(), role: "user", text: m.text ?? "", tools: [] }
        out.push(cur)
      } else if (m.role === "assistant") {
        cur = { id: nextId(), role: "assistant", text: m.text ?? "", tools: [], round: round++ }
        for (const tc of m.tool_calls ?? []) {
          cur.tools.push({
            id: tc.id,
            name: tc.name,
            args: JSON.stringify(tc.arguments ?? {}, null, 2),
            output: "",
            isError: false,
            running: false,
          })
        }
        out.push(cur)
      } else if (m.role === "tool" && m.tool_result && cur) {
        const run = cur.tools.find((x) => x.id === m.tool_result!.call_id)
        if (run) {
          run.output = m.tool_result.content
          run.isError = m.tool_result.is_error ?? false
        }
      }
    }
    r.items = out
    r.roundBase = round
    return r
  }

  const openTask = async (id: string) => {
    setActiveTaskId(id)
    focusInput()
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
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }

  const newTask = async (projectId?: string) => {
    try {
      const r = await api.createTask(projectId)
      await refreshTasks()
      runsRef.current[r.task.id] = newRun(r.task.title, defaultMode)
      setActiveTaskId(r.task.id)
      focusInput()
      bump()
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }

  // 点「任务」TAB：已有任务就选中最近的一条，没有则直接新建
  // tasks 已由后端按 UpdatedAt 降序返回，第一条即最近
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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
  const makeHandler = (tid: string) => (ev: ChatEvent) => {
    const r = runsRef.current[tid]
    if (!r) return
    if ("round" in ev) {
      r.lastRound = Math.max(r.lastRound, ev.round)
    }
    switch (ev.type) {
      case "start":
        r.title = ev.title
        r.mode = ev.mode === "plan" ? "plan" : "build"
        break
      case "text":
        r.thinking = false
        {
          const items = [...r.items]
          slotOf(items, r, ev.round).text += ev.text
          r.items = items
        }
        break
      case "tool_start":
        r.thinking = false
        {
          const items = [...r.items]
          const cur = slotOf(items, r, ev.round)
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
        }
        break
      case "done":
        break
    }
    bump()
  }

  // 真正发起一轮问答。同一任务串行（streaming 期间不并发），不同任务并行。
  const doSend = async (tid: string, text: string) => {
    const r = runsRef.current[tid]
    if (!r || r.streaming) return
    const ac = new AbortController()
    abortRefs.current[tid] = ac
    const baseAtSend = r.roundBase
    r.streaming = true
    r.thinking = true
    r.lastRound = -1
    const userItem: ViewItem = { id: nextId(), role: "user", text, tools: [] }
    // 预置第 0 轮的占位项：后端 round 从 0 起算，正好接上
    const asstItem: ViewItem = { id: nextId(), role: "assistant", text: "", tools: [], round: r.roundBase }
    r.items = [...r.items, userItem, asstItem]
    bump()
    try {
      await streamChat(tid, text, r.mode, state?.model ?? "", makeHandler(tid), ac.signal)
    } catch (e) {
      if (!ac.signal.aborted) {
        const msg = e instanceof Error ? e.message : String(e)
        const items = [...r.items]
        const cur = items[items.length - 1]
        if (cur && cur.role === "assistant") cur.error = msg
        r.items = items
      }
    } finally {
      delete abortRefs.current[tid]
      const rr = runsRef.current[tid]
      if (rr) {
        // 本轮用掉多少轮就往后推多少，保证下次提问的轮次不与本次重叠
        rr.roundBase = Math.max(rr.roundBase, baseAtSend + rr.lastRound + 1)
        rr.streaming = false
        rr.thinking = false
        bump()
      }
      await refreshTasks()
      // 侧栏项目按"最后对话时间"排序，对话后也要重排
      void refreshProjects()
      // 正常跑完（非中断）→ 自动发下一条排队消息；手动停止/中断不发
      if (!ac.signal.aborted) void flushQueue(tid)
    }
  }

  const launch = (tid: string, text: string): Promise<void> => {
    const p = doSend(tid, text)
    runPromises.current[tid] = p
    void p.finally(() => {
      if (runPromises.current[tid] === p) delete runPromises.current[tid]
    })
    return p
  }

  // 队列头自动出队（当前轮正常结束后调用）
  const flushQueue = async (tid: string) => {
    const r = runsRef.current[tid]
    if (!r || r.streaming || r.queue.length === 0) return
    const [head, ...rest] = r.queue
    r.queue = rest
    bump()
    await launch(tid, head.text)
  }

  // 输入框「发送」入口：空闲直接发；处理中（或已有排队）进队列。
  const submitText = (text: string) => {
    const t = text.trim()
    if (!t) return
    if (!activeTaskId) {
      // 空状态第一条消息：先建任务再发
      void (async () => {
        try {
          const c = await api.createTask("")
          await refreshTasks()
          runsRef.current[c.task.id] = newRun(c.task.title, defaultMode)
          setActiveTaskId(c.task.id)
          await launch(c.task.id, t)
        } catch (e) {
          setLoadErr(e instanceof Error ? e.message : String(e))
        }
      })()
      return
    }
    const r = runsRef.current[activeTaskId]
    if (!r) return
    if (r.streaming || r.queue.length > 0) {
      r.queue = [...r.queue, { id: ++queueSeq.current, text: t }]
      bump()
      if (!r.streaming) void flushQueue(activeTaskId)
    } else {
      void launch(activeTaskId, t)
    }
  }

  const stop = () => {
    if (activeTaskId) abortRefs.current[activeTaskId]?.abort()
  }

  // 队列「发送」：中断当前轮，立即发这条
  const dispatchQueued = async (qid: number) => {
    const tid = activeTaskId
    if (!tid) return
    const r = runsRef.current[tid]
    if (!r) return
    const item = r.queue.find((q) => q.id === qid)
    if (!item) return
    r.queue = r.queue.filter((q) => q.id !== qid)
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
    await launch(tid, item.text)
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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
      setLoadErr(e instanceof Error ? e.message : String(e))
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

  // 项目内任务归到项目下；无项目归属的任务才进「任务」TAB
  const tasksByProject: Record<string, TaskMeta[]> = {}
  for (const t of tasks) {
    if (!t.project_id) continue
    ;(tasksByProject[t.project_id] ??= []).push(t)
  }
  const standaloneTasks = tasks.filter((t) => !t.project_id)

  // 侧栏运行指示：哪些任务正在后台/前台执行
  const running: Record<string, boolean> = {}
  for (const [id, r] of Object.entries(runsRef.current)) {
    if (r.streaming) running[id] = true
  }

  const run = activeTaskId ? runsRef.current[activeTaskId] : undefined
  const chatTitle = run?.title || activeMeta?.title || "新任务"
  const chatSource = activeProject?.name ?? "任务"

  const emptyHint = (
    <div className="text-muted-foreground flex flex-col items-center gap-3 py-24 text-center">
      <div className="text-lg font-medium">omni 工作台</div>
      <div className="max-w-md text-sm">
        左侧「新建任务」开始对话，或选择一个项目在其中创建任务。
        <br />
        输入 / 可触发技能；MCP 与技能可在设置中查看。
      </div>
      {!state?.configured && (
        <Button variant="outline" onClick={() => setSettingsOpen(true)}>
          先去配置模型
        </Button>
      )}
    </div>
  )

  return (
    <div className="flex h-screen flex-col overflow-hidden">
      {isDesktop && <Titlebar source={chatSource} title={chatTitle} />}
      <div className="flex min-h-0 flex-1">
        <Sidebar
          tasks={standaloneTasks}
          projects={projects}
          tasksByProject={tasksByProject}
          activeTaskId={activeTaskId ?? null}
          running={running}
          open={sidebarOpen}
          onOpenTask={(id) => void openTask(id)}
          onDeleteTask={(id) => void deleteTask(id)}
          onNewTask={(projectId) => void newTask(projectId)}
          onDeleteProject={(id) => void deleteProject(id)}
          onOpenProject={(p) => void openProject(p)}
          onAddProject={() => (isDesktop ? void addProjectNative() : setAddProjectOpen(true))}
          onOpenSettings={() => setSettingsOpen(true)}
          onFocusTasksTab={() => void focusTasksTab()}
          onClose={() => setSidebarOpen(false)}
        />
        <Chat
          title={chatTitle}
          source={chatSource}
          mode={run?.mode ?? defaultMode}
          items={run?.items ?? []}
          streaming={run?.streaming ?? false}
          thinking={run?.thinking ?? false}
          queue={run?.queue ?? []}
          draftSeed={draftSeed}
          models={modelGroups}
          model={state?.model ?? ""}
          skills={skills}
          focusSignal={focusSignal}
          emptyHint={emptyHint}
          onSend={submitText}
          onStop={stop}
          onQueueDispatch={(qid) => void dispatchQueued(qid)}
          onQueueEdit={editQueued}
          onQueueRemove={removeQueued}
          onApprove={(id, allow) => void approve(id, allow)}
          onAnswer={(id, text) => void answer(id, text)}
          onModeChange={changeMode}
          onModelChange={(m) => void changeModel(m)}
          onOpenSidebar={() => {
            setSidebarOpen(true)
            focusInput()
          }}
        />
      </div>
      <SettingsDialog
        open={settingsOpen}
        onOpenChange={setSettingsOpen}
        state={state}
        onStateChange={setState}
      />
      <AddProjectDialog
        open={addProjectOpen}
        onOpenChange={setAddProjectOpen}
        onAdded={() => void refreshProjects()}
      />
      {loadErr && (
        <div className="bg-destructive text-destructive-foreground fixed bottom-4 left-1/2 z-50 -translate-x-1/2 rounded-md px-4 py-2 text-sm shadow-lg">
          {loadErr}
          <button className="ml-3 underline cursor-pointer" onClick={() => setLoadErr("")}>
            关闭
          </button>
        </div>
      )}
    </div>
  )
}
