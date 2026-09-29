import { useState } from "react"
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { TaskMeta } from "@/types"

// 默认显示 5 条，超出折叠
const VISIBLE = 5

interface TaskListProps {
  tasks: TaskMeta[]
  activeTaskId: string | null
  onOpen: (id: string) => void
  onDelete: (id: string, title: string) => void
  emptyText?: string
}

export function TaskList({ tasks, activeTaskId, onOpen, onDelete, emptyText }: TaskListProps) {
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
            className="flex min-w-0 flex-1 flex-col items-start gap-0.5 text-left cursor-pointer"
            title={t.title}
          >
            <span className="truncate w-full">{t.title}</span>
            <span className="text-muted-foreground text-[11px]">
              {formatTime(t.updated_at)}
              {t.mode === "plan" ? " · plan" : ""}
            </span>
          </button>
          <Button
            variant="ghost"
            size="icon-sm"
            className="opacity-0 group-hover:opacity-100 text-muted-foreground hover:text-destructive"
            onClick={(e) => {
              e.stopPropagation()
              onDelete(t.id, t.title)
            }}
            title="删除任务"
          >
            ×
          </Button>
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

function formatTime(iso: string): string {
  try {
    const d = new Date(iso)
    const now = new Date()
    if (d.toDateString() === now.toDateString()) {
      return d.toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })
    }
    return d.toLocaleDateString("zh-CN", { month: "numeric", day: "numeric" })
  } catch {
    return ""
  }
}
