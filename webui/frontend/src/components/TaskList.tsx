import { useState } from "react"
import { ChevronDownIcon, ChevronUpIcon, Loader2Icon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { TaskMeta } from "@/types"

// 默认显示 5 条，超出折叠
const VISIBLE = 5

interface TaskListProps {
  tasks: TaskMeta[]
  activeTaskId: string | null
  /** 正在执行的任务 ID 集合：行右侧显示加载动画（后台跑也可见） */
  running: Record<string, boolean>
  onOpen: (id: string) => void
  onDelete: (id: string, title: string) => void
  emptyText?: string
}

export function TaskList({ tasks, activeTaskId, running, onOpen, onDelete, emptyText }: TaskListProps) {
  const [expanded, setExpanded] = useState(false)
  if (tasks.length === 0) {
    return <div className="text-muted-foreground px-2 py-1.5 text-xs">{emptyText ?? "暂无任务"}</div>
  }
  const shown = expanded ? tasks : tasks.slice(0, VISIBLE)
  const hidden = tasks.length - VISIBLE

  return (
    <div className="flex flex-col gap-0.5">
      {shown.map((t) => (
        <div
          key={t.id}
          className={cn(
            "group hover:bg-sidebar-accent flex items-center rounded-md px-2 py-1.5 text-sm",
            activeTaskId === t.id && "bg-sidebar-accent",
          )}
        >
          <button
            type="button"
            onClick={() => onOpen(t.id)}
            className="min-w-0 flex-1 truncate text-left cursor-pointer"
            title={t.title}
          >
            {t.title}
          </button>
          {/* 运行中：常驻转圈动画（切走任务后台也在跑）；否则悬停显示删除 */}
          {running[t.id] ? (
            <Loader2Icon className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
          ) : (
            <Button
              variant="ghost"
              size="icon-sm"
              className="text-muted-foreground opacity-0 transition-opacity group-hover:opacity-100 max-md:opacity-100"
              onClick={(e) => {
                e.stopPropagation()
                onDelete(t.id, t.title)
              }}
              title="删除任务"
            >
              <XIcon />
            </Button>
          )}
        </div>
      ))}
      {hidden > 0 && (
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground justify-start"
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? <ChevronUpIcon /> : <ChevronDownIcon />}
          {expanded ? "收起" : `更多（${hidden} 条）`}
        </Button>
      )}
    </div>
  )
}
