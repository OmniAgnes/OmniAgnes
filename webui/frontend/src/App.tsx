import { useCallback, useEffect, useRef, useState } from "react"

import { AddProjectDialog } from "@/components/AddProjectDialog"
import { Chat, type ViewItem } from "@/components/Chat"
import { Sidebar } from "@/components/Sidebar"
import { SettingsDialog } from "@/components/SettingsDialog"
import { Titlebar } from "@/components/Titlebar"
import { Button } from "@/components/ui/button"
import { api, streamChat } from "@/lib/api"
import type { ChatEvent, Project, StateInfo, TaskFull, TaskMeta, Theme } from "@/types"

const isDesktop = typeof window !== "undefined" && Boolean(window.omniDesktop)

export default function App() {
  const [state, setState] = useState<StateInfo | null>(null)
  const [projects, setProjects] = useState<Project[]>([])
  const [tasks, setTasks] = useState<TaskMeta[]>([])
  const [activeTask, setActiveTask] = useState<TaskFull | null>(null)
  const [items, setItems] = useState<ViewItem[]>([])
  const [mode, setMode] = useState<"build" | "plan">("build")
  const [streaming, setStreaming] = useState(false)
  // 已发出请求但模型还没吐出首个 token：显示「思考中」动画
  const [thinking, setThinking] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [addProjectOpen, setAddProjectOpen] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [loadErr, setLoadErr] = useState("")

  const abortRef = useRef<AbortController | null>(null)
  const idRef = useRef(0)
  const activeProjectId = activeTask?.project_id ?? null

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
    const theme: Theme = state?.theme ?? "dark"
    const root = document.documentElement
    const apply = (dark: boolean) => root.classList.toggle("dark", dark)
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

  // 模型列表（聊天底部快速切换用）
  const [models, setModels] = useState<string[]>([])
  const refreshModels = useCallback(async () => {
    try {
      const r = await api.models()
      setModels(r.models.map((m) => m.model))
    } catch {
      setModels([])
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

  const nextId = () => ++idRef.current

  // 从持久化消息构建视图
  const buildItems = (t: TaskFull): ViewItem[] => {
    const out: ViewItem[] = []
    let cur: ViewItem | null = null
    for (const m of t.messages ?? []) {
      if (m.role === "user") {
        cur = { id: nextId(), role: "user", text: m.text ?? "", tools: [] }
        out.push(cur)
      } else if (m.role === "assistant") {
        cur = { id: nextId(), role: "assistant", text: m.text ?? "", tools: [] }
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
    return out
  }

  const openTask = async (id: string) => {
    if (streaming) abortRef.current?.abort()
    try {
      const r = await api.task(id)
      setActiveTask(r.task)
      setMode(r.task.mode === "plan" ? "plan" : "build")
      setItems(buildItems(r.task))
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }

  const newTask = async (projectId?: string) => {
    if (streaming) abortRef.current?.abort()
    try {
      const r = await api.createTask(projectId)
      await refreshTasks()
      setActiveTask({ ...r.task, messages: [] })
      setItems([])
      setMode("build")
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }

  const deleteTask = async (id: string) => {
    await api.deleteTask(id)
    if (activeTask?.id === id) {
      setActiveTask(null)
      setItems([])
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

  const handleEvent = (ev: ChatEvent, taskId: string) => {
    switch (ev.type) {
      case "start":
        setActiveTask((prev) => (prev && prev.id === taskId ? { ...prev, title: ev.title, mode: ev.mode } : prev))
        setMode(ev.mode === "plan" ? "plan" : "build")
        break
      case "text":
        setThinking(false)
        setItems((prev) => {
          const next = [...prev]
          const cur = next[next.length - 1]
          if (cur && cur.role === "assistant") cur.text += ev.text
          return next
        })
        break
      case "tool_start":
        setThinking(false)
        setItems((prev) => {
          const next = [...prev]
          let cur = next[next.length - 1]
          if (!cur || cur.role !== "assistant") {
            cur = { id: nextId(), role: "assistant", text: "", tools: [] }
            next.push(cur)
          }
          cur.tools.push({
            id: `${Date.now()}-${cur.tools.length}`,
            name: ev.name,
            args: ev.args,
            output: "",
            isError: false,
            running: true,
          })
          return next
        })
        break
      case "tool_output":
        setItems((prev) => {
          const next = [...prev]
          const cur = next[next.length - 1]
          const run = cur?.tools.filter((t) => t.running).at(-1) ?? cur?.tools.at(-1)
          if (run) {
            run.output = ev.output
            run.isError = ev.is_error
          }
          return next
        })
        break
      case "tool_end":
        setItems((prev) => {
          const next = [...prev]
          const cur = next[next.length - 1]
          const run = cur?.tools.filter((t) => t.running).at(-1)
          if (run) {
            run.running = false
            run.isError = ev.is_error
          }
          return next
        })
        break
      case "approval_pending":
        // 工具卡片已出现，这里只清掉思考动画
        setThinking(false)
        break
      case "approval":
        // 普通访问模式：把待确认请求挂到最近一张运行中的工具卡片上
        setItems((prev) => {
          const next = [...prev]
          const cur = next[next.length - 1]
          const run = cur?.tools.filter((t) => t.running).at(-1)
          if (run) {
            run.approvalId = ev.id
            run.approvalChoice = null
          }
          return next
        })
        break
      case "approval_result":
        setItems((prev) => {
          const next = [...prev]
          for (const it of next) {
            for (const run of it.tools) {
              if (run.approvalId === ev.id) {
                run.approvalChoice = ev.allowed
                run.denied = !ev.allowed
              }
            }
          }
          return next
        })
        break
      case "error":
        setItems((prev) => {
          const next = [...prev]
          const cur = next[next.length - 1]
          if (cur && cur.role === "assistant") {
            cur.error = ev.error
          } else {
            next.push({ id: nextId(), role: "assistant", text: "", tools: [], error: ev.error })
          }
          return next
        })
        break
      case "done":
        break
    }
  }

  const send = async (text: string) => {
    let task = activeTask
    try {
      if (!task) {
        const r = await api.createTask(activeProjectId ?? "")
        task = { ...r.task, messages: [] }
        setActiveTask(task)
        await refreshTasks()
      }
      const userItem: ViewItem = { id: nextId(), role: "user", text, tools: [] }
      const asstItem: ViewItem = { id: nextId(), role: "assistant", text: "", tools: [] }
      setItems((prev) => [...prev, userItem, asstItem])
      setStreaming(true)
      setThinking(true)
      const ac = new AbortController()
      abortRef.current = ac
      try {
        await streamChat(task.id, text, mode, state?.model ?? "", (ev) => handleEvent(ev, task!.id), ac.signal)
      } catch (e) {
        if (!ac.signal.aborted) {
          const msg = e instanceof Error ? e.message : String(e)
          setItems((prev) => {
            const next = [...prev]
            const cur = next[next.length - 1]
            if (cur && cur.role === "assistant") cur.error = msg
            return next
          })
        }
      } finally {
        setStreaming(false)
        setThinking(false)
        abortRef.current = null
        await refreshTasks()
      }
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
      setStreaming(false)
      setThinking(false)
    }
  }

  const stop = () => {
    abortRef.current?.abort()
    setStreaming(false)
    setThinking(false)
  }

  // 答复一次写操作确认（普通访问模式）
  const approve = async (id: string, allow: boolean) => {
    setItems((prev) => {
      const next = [...prev]
      for (const it of next) {
        for (const run of it.tools) {
          if (run.approvalId === id) run.approvalChoice = allow
        }
      }
      return next
    })
    try {
      await api.approval(id, allow)
    } catch (e) {
      setLoadErr(e instanceof Error ? e.message : String(e))
    }
  }

  // 项目内任务归到项目下；无项目归属的任务才进「任务」TAB
  const tasksByProject: Record<string, TaskMeta[]> = {}
  for (const t of tasks) {
    if (!t.project_id) continue
    ;(tasksByProject[t.project_id] ??= []).push(t)
  }
  const standaloneTasks = tasks.filter((t) => !t.project_id)

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
      {isDesktop && <Titlebar />}
      <div className="flex min-h-0 flex-1">
        <Sidebar
          state={state}
          tasks={standaloneTasks}
          projects={projects}
          tasksByProject={tasksByProject}
          activeTaskId={activeTask?.id ?? null}
          open={sidebarOpen}
          onOpenTask={(id) => void openTask(id)}
          onDeleteTask={(id) => void deleteTask(id)}
          onNewTask={(projectId) => void newTask(projectId)}
          onDeleteProject={(id) => void deleteProject(id)}
          onOpenProject={(p) => void openProject(p)}
          onAddProject={() => (isDesktop ? void addProjectNative() : setAddProjectOpen(true))}
          onOpenSettings={() => setSettingsOpen(true)}
          onClose={() => setSidebarOpen(false)}
        />
        <Chat
          title={activeTask?.title || "新任务"}
          mode={mode}
          items={items}
          streaming={streaming}
          thinking={thinking}
          models={models}
          model={state?.model ?? ""}
          emptyHint={emptyHint}
          onSend={(text) => void send(text)}
          onStop={stop}
          onApprove={(id, allow) => void approve(id, allow)}
          onModeChange={setMode}
          onModelChange={(m) => void changeModel(m)}
          onOpenSidebar={() => setSidebarOpen(true)}
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
