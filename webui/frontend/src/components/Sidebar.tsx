import { useEffect, useState } from "react"
import { ChevronDownIcon, ChevronRightIcon, FolderIcon, FolderOpenIcon, FolderPlusIcon, Loader2Icon, MessageSquareIcon, PlusIcon, SettingsIcon, SquarePenIcon, Trash2Icon, XIcon } from "lucide-react"

import { TaskList } from "@/components/TaskList"
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
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { Project, TaskMeta } from "@/types"

/** 文件夹下任务每页条数（点「显示更多」再加载这么多） */
const PAGE = 5

/**
 * 文件夹展开状态持久化到 localStorage：切到别的任务再切回来、刷新页面、
 * 重启应用都保持原样（用户要求）。用 Set 存展开的项目 id。
 */
const OPEN_KEY = "omni.sidebar.openProjects"

function readOpenProjects(): Set<string> {
  try {
    const raw = localStorage.getItem(OPEN_KEY)
    const arr = raw ? (JSON.parse(raw) as string[]) : []
    return new Set(Array.isArray(arr) ? arr : [])
  } catch {
    return new Set()
  }
}

function writeOpenProjects(s: Set<string>) {
  try {
    localStorage.setItem(OPEN_KEY, JSON.stringify([...s]))
  } catch {
    /* 隐私模式等场景写不进去，忽略即可 */
  }
}

function isProjectOpen(id: string): boolean {
  return readOpenProjects().has(id)
}

function setProjectOpen(id: string, open: boolean) {
  const s = readOpenProjects()
  if (open) {
    s.add(id)
  } else {
    s.delete(id)
  }
  writeOpenProjects(s)
}

/** 项目被移除时清掉其展开记录，避免 localStorage 堆积 */
export function forgetProjectOpen(id: string) {
  setProjectOpen(id, false)
}

interface SidebarProps {
  tasks: TaskMeta[]
  projects: Project[]
  tasksByProject: Record<string, TaskMeta[]>
  activeTaskId: string | null
  /** 正在执行（后台或前台）的任务 ID 集合，用于任务行右侧加载动画 */
  running: Record<string, boolean>
  open: boolean
  onOpenTask: (id: string) => void
  onDeleteTask: (id: string) => void
  onNewTask: (projectId?: string) => void
  onDeleteProject: (id: string) => void
  onOpenProject: (p: Project) => void
  onAddProject: () => void
  onOpenSettings: () => void
  /** 点「任务」TAB：有任务选中最近一条，没任务则新建 */
  onFocusTasksTab: () => void
  onClose: () => void
}

export function Sidebar(props: SidebarProps) {
  const {
    tasks,
    projects,
    tasksByProject,
    activeTaskId,
    running,
    open,
    onOpenTask,
    onDeleteTask,
    onNewTask,
    onDeleteProject,
    onOpenProject,
    onAddProject,
    onOpenSettings,
    onFocusTasksTab,
    onClose,
  } = props

  // 默认展示「项目」tab
  const [tab, setTab] = useState<"projects" | "tasks">("projects")
  // 文件夹右键菜单与移除确认
  const [menu, setMenu] = useState<{ project: Project; x: number; y: number } | null>(null)
  const [confirmProject, setConfirmProject] = useState<Project | null>(null)

  // 右键菜单打开期间：任意点击 / Esc / 窗口变化时关闭。
  // 注意：Chromium 里右键抬起也会派发 click(button=2)，必须过滤——
  // 否则刚弹出的右键菜单会被同一次右击的 click 立刻关掉（表现为"菜单不出现"）。
  useEffect(() => {
    if (!menu) return
    const close = (e: Event) => {
      // resize/keydown 传来的不是 MouseEvent（button undefined），一律关闭；
      // 只有明确的右键 click 才忽略
      if (e instanceof MouseEvent && e.button !== 0) return
      setMenu(null)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenu(null)
    }
    window.addEventListener("click", close)
    window.addEventListener("resize", close)
    window.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("click", close)
      window.removeEventListener("resize", close)
      window.removeEventListener("keydown", onKey)
    }
  }, [menu])

  return (
    <>
      {/* 移动端遮罩 */}
      {open && (
        <div className="bg-black/50 fixed inset-0 z-30 md:hidden" onClick={onClose} />
      )}
      <aside
        className={cn(
          // 窄屏抽屉：fixed 覆盖整个视口（top-0 起、h-full 到底），顶部与标题栏窗口按钮那一行齐平。
          // md 以上切回 static，宽度固定常驻。
          "bg-sidebar text-sidebar-foreground fixed inset-y-0 left-0 z-40 flex min-h-0 w-72 shrink-0 flex-col border-r transition-transform duration-200 md:static md:translate-x-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        {/* 顶栏：桌面端由自绘标题栏承担品牌展示，不再重复 */}
        {!window.omniDesktop && (
          <div className="flex items-center justify-between px-4 py-3">
            <span className="text-base font-semibold tracking-tight">OmniAgens</span>
            <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onClose} title="收起">
              <XIcon />
            </Button>
          </div>
        )}

        {/* 桌面端没有顶栏品牌区，给「新建任务」留出顶部间距，别贴着标题栏 */}
        <div className={cn("px-3 pb-2", window.omniDesktop ? "pt-2.5" : "")}>
          <Button variant="secondary" className="w-full justify-start" onClick={() => onNewTask()} title="新建任务（Ctrl + N）">
            <SquarePenIcon /> 新建任务
            <kbd className="text-muted-foreground ml-auto text-[11px]">Ctrl + N</kbd>
          </Button>
        </div>

        {/* 任务 / 项目 TAB */}
        <div className="bg-sidebar-border/50 mx-3 mb-2 flex rounded-md p-0.5">
          {(
            [
              { id: "projects", label: "项目", count: projects.length, icon: FolderIcon },
              { id: "tasks", label: "任务", count: tasks.length, icon: MessageSquareIcon },
            ] as const
          ).map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                setTab(t.id)
                // 点「任务」TAB 时：有任务就跳到最近的一条，没有就直接新建一个
                if (t.id === "tasks") onFocusTasksTab()
              }}
              className={cn(
                "flex flex-1 items-center justify-center gap-1.5 rounded-[5px] px-2 py-1.5 text-sm cursor-pointer transition-colors",
                tab === t.id
                  ? "bg-background text-foreground shadow-xs"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              <t.icon className="size-3.5" />
              {t.label}
              <span className="text-muted-foreground text-[11px]">{t.count}</span>
            </button>
          ))}
        </div>

        {/* 列表：原生滚动容器（min-h-0 保证在 flex 里能收缩） */}
        <div className="omni-scroll min-h-0 flex-1 overflow-y-auto">
          <div className="flex flex-col gap-1 px-3 pb-6">
            {tab === "tasks" ? (
              <TaskList
                tasks={tasks}
                activeTaskId={activeTaskId}
                running={running}
                onOpen={(id) => {
                  onOpenTask(id)
                  onClose()
                }}
                onDelete={onDeleteTask}
                emptyText="还没有任务，点上方「新建任务」开始"
              />
            ) : (
              <>
                <div className="flex items-center justify-between pr-1">
                  <SectionTitle className="mb-0">本地文件夹</SectionTitle>
                  <Button variant="ghost" size="icon-sm" onClick={onAddProject} title="添加本地文件夹">
                    <FolderPlusIcon />
                  </Button>
                </div>
                {projects.length === 0 && (
                  <div className="text-muted-foreground px-2 py-1.5 text-xs">
                    添加本地文件夹作为项目，集中管理它的任务
                  </div>
                )}
                {projects.map((p) => (
                  <ProjectGroup
                    key={p.id}
                    project={p}
                    tasks={tasksByProject[p.id] ?? []}
                    activeTaskId={activeTaskId}
                    running={running}
                    onOpenTask={(id) => {
                      onOpenTask(id)
                      onClose()
                    }}
                    onNewTask={() => onNewTask(p.id)}
                    onDeleteTask={onDeleteTask}
                    onContextMenu={(e) => {
                      e.preventDefault()
                      // 视口边缘时向内收，避免菜单被截断
                      const x = Math.min(e.clientX, window.innerWidth - 170)
                      const y = Math.min(e.clientY, window.innerHeight - 110)
                      setMenu({ project: p, x, y })
                    }}
                  />
                ))}
              </>
            )}
          </div>
        </div>

        {/* 底部：设置入口（不要分隔线，与自绘标题栏一致保持无边框） */}
        <div className="flex items-center justify-end px-3 py-1.5">
          <Button variant="ghost" size="icon-sm" onClick={onOpenSettings} title="设置">
            <SettingsIcon />
          </Button>
        </div>
      </aside>

      {/* 文件夹右键菜单 */}
      {menu && (
        <div
          role="menu"
          className="bg-popover text-popover-foreground fixed z-50 min-w-40 rounded-md border p-1 shadow-md"
          style={{ left: menu.x, top: menu.y }}
          onContextMenu={(e) => e.preventDefault()}
        >
          <button
            type="button"
            className="hover:bg-accent flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm cursor-pointer"
            onClick={() => {
              onOpenProject(menu.project)
              setMenu(null)
            }}
          >
            <FolderOpenIcon className="text-muted-foreground size-4" />
            打开文件夹
          </button>
          <button
            type="button"
            className="text-destructive hover:bg-accent flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm cursor-pointer"
            onClick={() => {
              setConfirmProject(menu.project)
              setMenu(null)
            }}
          >
            <Trash2Icon className="size-4" />
            移除文件夹
          </button>
        </div>
      )}

      {/* 移除文件夹确认弹窗 */}
      <AlertDialog
        open={confirmProject !== null}
        onOpenChange={(o) => {
          if (!o) setConfirmProject(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除文件夹？</AlertDialogTitle>
            <AlertDialogDescription>
              将「{confirmProject?.name}」从列表中移除。不会删除磁盘上的文件，其中的任务也会保留。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (confirmProject) {
                  // 清掉展开记录，否则重新添加同一文件夹时会被误判为"已展开"
                  forgetProjectOpen(confirmProject.id)
                  onDeleteProject(confirmProject.id)
                }
                setConfirmProject(null)
              }}
            >
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function SectionTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      className={cn("text-muted-foreground mb-1 px-2 text-xs font-medium tracking-wide", className)}
      {...props}
    />
  )
}

function ProjectGroup(props: {
  project: Project
  tasks: TaskMeta[]
  activeTaskId: string | null
  running: Record<string, boolean>
  onOpenTask: (id: string) => void
  onNewTask: () => void
  onDeleteTask: (id: string) => void
  onContextMenu: (e: React.MouseEvent) => void
}) {
  const { project, tasks, activeTaskId, running, onOpenTask, onNewTask, onDeleteTask, onContextMenu } = props
  // 任务多时先显示 5 条，点「显示更多」每次再加载 5 条
  const [limit, setLimit] = useState(PAGE)
  // 任务列表默认收起；展开状态按项目 id 持久化——切任务、刷新、重启后都保持
  const [open, setOpen] = useState(() => isProjectOpen(project.id))
  const toggle = () => {
    setOpen((o) => {
      setProjectOpen(project.id, !o)
      return !o
    })
  }
  const shown = tasks.slice(0, limit)
  const rest = tasks.length - shown.length

  return (
    <div className="flex flex-col gap-0.5">
      {/* 文件夹行：悬停只浮现「新建任务」图标，整行不加背景色（用户要求）；打开/移除走右键菜单 */}
      <div className="group flex items-center rounded-md px-2 py-1.5" onContextMenu={onContextMenu}>
        <button
          type="button"
          onClick={toggle}
          className="flex min-w-0 flex-1 items-center gap-2 text-left text-sm cursor-pointer"
          title={project.path}
        >
          {open ? (
            <ChevronDownIcon className="text-muted-foreground size-3.5 shrink-0" />
          ) : (
            <ChevronRightIcon className="text-muted-foreground size-3.5 shrink-0" />
          )}
          <FolderIcon className="text-muted-foreground size-4 shrink-0" />
          <span className="truncate">{project.name}</span>
        </button>
        {/* 悬停只显示「新建任务」添加图标；打开/移除走右键菜单 */}
        <div className="flex items-center opacity-0 transition-opacity group-hover:opacity-100 max-md:opacity-100">
          <Button variant="ghost" size="icon-sm" onClick={onNewTask} title="在这个项目里新建任务">
            <PlusIcon />
          </Button>
        </div>
      </div>
      {open && (
      <div className="border-sidebar-border ml-6 flex flex-col gap-0.5 border-l pl-2">
        {shown.map((t) => (
          <div
            key={t.id}
            className={cn(
              // 同 TaskList：13px，比 text-xs 大一号，与文件夹行的 text-sm 拉开层次
              "group hover:bg-sidebar-accent flex items-center rounded-md px-2 py-1 text-[13px]",
              activeTaskId === t.id && "bg-sidebar-accent",
            )}
          >
            <button
              type="button"
              onClick={() => onOpenTask(t.id)}
              className="min-w-0 flex-1 truncate text-left cursor-pointer"
              title={t.title}
            >
              {t.title}
            </button>
            {/* 运行中：常驻转圈动画（切走也在后台跑，让用户知道处理中）；否则悬停显示删除 */}
            {running[t.id] ? (
              <Loader2Icon className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
            ) : (
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 max-md:opacity-100"
                onClick={(e) => {
                  e.stopPropagation()
                  onDeleteTask(t.id)
                }}
                title="删除任务"
              >
                <Trash2Icon />
              </Button>
            )}
          </div>
        ))}
        {tasks.length === 0 && <div className="text-muted-foreground px-2 py-1 text-xs">暂无任务</div>}
        {rest > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground hover:text-foreground h-6 justify-start px-2 text-[13px]"
            onClick={() => setLimit((n) => n + PAGE)}
          >
            显示更多
          </Button>
        )}
      </div>
      )}
    </div>
  )
}
