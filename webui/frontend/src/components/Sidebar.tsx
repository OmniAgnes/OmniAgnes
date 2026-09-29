import { useState } from "react"
import { FolderIcon, FolderOpenIcon, MessageSquareIcon, PlusIcon, SettingsIcon, SquarePenIcon, XIcon } from "lucide-react"

import { TaskList } from "@/components/TaskList"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { cn } from "@/lib/utils"
import type { Project, StateInfo, TaskMeta } from "@/types"

const VISIBLE = 5

interface SidebarProps {
  state: StateInfo | null
  tasks: TaskMeta[]
  projects: Project[]
  tasksByProject: Record<string, TaskMeta[]>
  activeTaskId: string | null
  activeProjectId: string | null
  open: boolean
  onOpenTask: (id: string) => void
  onDeleteTask: (id: string) => void
  onNewTask: (projectId?: string) => void
  onDeleteProject: (id: string) => void
  onAddProject: () => void
  onOpenSettings: () => void
  onClose: () => void
}

export function Sidebar(props: SidebarProps) {
  const {
    state,
    tasks,
    projects,
    tasksByProject,
    activeTaskId,
    activeProjectId,
    open,
    onOpenTask,
    onDeleteTask,
    onNewTask,
    onDeleteProject,
    onAddProject,
    onOpenSettings,
    onClose,
  } = props

  // 默认展示「项目」tab
  const [tab, setTab] = useState<"projects" | "tasks">("projects")

  return (
    <>
      {/* 移动端遮罩 */}
      {open && (
        <div className="bg-black/50 fixed inset-0 z-30 md:hidden" onClick={onClose} />
      )}
      <aside
        className={cn(
          "bg-sidebar text-sidebar-foreground fixed left-0 bottom-0 z-40 flex h-full w-72 shrink-0 flex-col border-r transition-transform duration-200 md:static md:translate-x-0",
          // 桌面端顶部让出自绘标题栏的高度
          window.omniDesktop ? "top-9" : "top-0",
          open ? "translate-x-0" : "-translate-x-full",
        )}
      >
        {/* 顶栏 */}
        <div className="flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-2">
            <span className="text-base font-semibold">omni</span>
            <span className="text-muted-foreground text-xs">{state?.version}</span>
          </div>
          <div className="flex items-center gap-0.5">
            <Button variant="ghost" size="icon-sm" onClick={onOpenSettings} title="设置">
              <SettingsIcon />
            </Button>
            <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onClose} title="收起">
              <XIcon />
            </Button>
          </div>
        </div>

        <div className="px-3 pb-2">
          <Button className="w-full" onClick={() => onNewTask()}>
            <SquarePenIcon /> 新建任务
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
              onClick={() => setTab(t.id)}
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

        <ScrollArea className="flex-1">
          <div className="flex flex-col gap-1 px-3 pb-6">
            {tab === "tasks" ? (
              <>
                <SectionTitle>全部任务（最近优先）</SectionTitle>
                <TaskList
                  tasks={tasks}
                  activeTaskId={activeTaskId}
                  onOpen={(id) => {
                    onOpenTask(id)
                    onClose()
                  }}
                  onDelete={onDeleteTask}
                  emptyText="还没有任务，点上方「新建任务」开始"
                />
              </>
            ) : (
              <>
                <div className="flex items-center justify-between pr-1">
                  <SectionTitle className="mb-0">本地文件夹</SectionTitle>
                  <Button variant="ghost" size="icon-sm" onClick={onAddProject} title="添加本地文件夹">
                    <PlusIcon />
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
                    active={activeProjectId === p.id}
                    onOpenTask={(id) => {
                      onOpenTask(id)
                      onClose()
                    }}
                    onNewTask={() => onNewTask(p.id)}
                    onDelete={() => onDeleteProject(p.id)}
                  />
                ))}
              </>
            )}
          </div>
        </ScrollArea>

        {/* 底部：当前模型 */}
        <div className="text-muted-foreground border-t px-4 py-2 text-xs">
          模型：{state?.model || "未配置"} · {state?.configured ? "已就绪" : "未配置"}
        </div>
      </aside>
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
  active: boolean
  onOpenTask: (id: string) => void
  onNewTask: () => void
  onDelete: () => void
}) {
  const { project, tasks, activeTaskId, active, onOpenTask, onNewTask, onDelete } = props
  const [expanded, setExpanded] = useState(false)
  const shown = expanded ? tasks : tasks.slice(0, VISIBLE)
  const hidden = tasks.length - VISIBLE

  return (
    <div className="flex flex-col gap-0.5">
      <div
        className={cn(
          "group hover:bg-sidebar-accent flex items-center rounded-md px-2 py-1.5",
          active && "bg-sidebar-accent",
        )}
      >
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-2 text-left text-sm cursor-pointer"
          title={project.path}
        >
          {active ? (
            <FolderOpenIcon className="text-muted-foreground size-4 shrink-0" />
          ) : (
            <FolderIcon className="text-muted-foreground size-4 shrink-0" />
          )}
          <span className="truncate">{project.name}</span>
          <span className="text-muted-foreground text-[11px]">{tasks.length}</span>
        </button>
        <div className="flex items-center opacity-0 transition-opacity group-hover:opacity-100 max-md:opacity-100">
          <Button variant="ghost" size="icon-sm" onClick={onNewTask} title="在这个项目里新建任务">
            <PlusIcon />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            className="text-muted-foreground hover:text-destructive"
            onClick={() => {
              if (window.confirm(`移除项目「${project.name}」？（任务保留）`)) onDelete()
            }}
            title="移除项目"
          >
            ×
          </Button>
        </div>
      </div>
      <div className="border-sidebar-border ml-6 flex flex-col gap-0.5 border-l pl-2">
        {shown.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => onOpenTask(t.id)}
            className={cn(
              "hover:bg-sidebar-accent flex items-center rounded-md px-2 py-1 text-left text-xs cursor-pointer",
              activeTaskId === t.id && "bg-sidebar-accent",
            )}
          >
            <span className="truncate">{t.title}</span>
          </button>
        ))}
        {tasks.length === 0 && <div className="text-muted-foreground px-2 py-1 text-xs">暂无任务</div>}
        {hidden > 0 && (
          <Button
            variant="ghost"
            size="sm"
            className="text-muted-foreground h-6 justify-start px-2 text-xs"
            onClick={() => setExpanded(!expanded)}
          >
            {expanded ? "收起" : `更多（${hidden} 条）`}
          </Button>
        )}
      </div>
    </div>
  )
}
