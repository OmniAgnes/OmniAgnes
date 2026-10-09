import { useState } from "react"
import { ChevronDownIcon, Loader2Icon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useI18n } from "@/lib/i18n"
import { relTime } from "@/lib/time"
import { cn } from "@/lib/utils"
import type { TaskMeta } from "@/types"

// 默认显示 5 条；「显示更多」每次再加载 5 条
const VISIBLE = 5

interface TaskListProps {
  tasks: TaskMeta[]
  activeTaskId: string | null
  /** 正在执行的任务 ID → 状态文字（处理中/重试 n/10）：行右侧显示动画与状态 */
  running: Record<string, string>
  onOpen: (id: string) => void
  onDelete: (id: string, title: string) => void
  emptyText?: string
}

export function TaskList({ tasks, activeTaskId, running, onOpen, onDelete, emptyText }: TaskListProps) {
  const { t } = useI18n()
  // 当前展示的任务条数，点「显示更多」逐步 +VISIBLE，而不是一次全展开
  const [visible, setVisible] = useState(VISIBLE)
  if (tasks.length === 0) {
    return <div className="text-muted-foreground px-2 py-1.5 text-xs">{emptyText ?? t("s.noTasks")}</div>
  }
  const shown = tasks.slice(0, visible)
  const hidden = tasks.length - visible

  return (
    <div className="flex flex-col gap-0.5">
      {shown.map((task) => (
        <div
          key={task.id}
          className={cn(
            // 13px：比默认 text-xs(12) 明显大一号，又不跟文件夹名的 text-sm(14) 抢视觉。
            // relative：删除按钮绝对定位覆盖在最右，不占位，让时间真正贴最右。
            "group hover:bg-sidebar-accent relative flex items-center rounded-md px-2 py-1 text-[13px]",
            activeTaskId === task.id && "bg-sidebar-accent",
          )}
        >
          <button
            type="button"
            onClick={() => onOpen(task.id)}
            className="min-w-0 flex-1 truncate text-left cursor-pointer"
            title={task.title}
          >
            {task.title}
          </button>
          {/* 运行中：转圈 + 状态文字（处理中/重试 n/10），切走任务后台也在跑也看得见；
              空闲：时间常驻最右；悬停时时间淡出、删除图标在原位浮现 */}
          {running[task.id] ? (
            <span
              className="text-muted-foreground flex shrink-0 items-center gap-1 text-[11px]"
              title={running[task.id]}
            >
              <Loader2Icon className="size-3.5 shrink-0 animate-spin" />
              <span className="max-w-20 truncate">{running[task.id]}</span>
            </span>
          ) : (
            <>
              <span className="text-muted-foreground w-14 shrink-0 text-right text-[11px] tabular-nums transition-opacity group-hover:opacity-0">
                {relTime(t, task.last_reply_at ?? task.created_at)}
              </span>
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground absolute top-1/2 right-1 -translate-y-1/2 opacity-0 transition-opacity group-hover:opacity-100"
                onClick={(e) => {
                  e.stopPropagation()
                  onDelete(task.id, task.title)
                }}
                title={t("s.deleteTask")}
              >
                <XIcon />
              </Button>
            </>
          )}
        </div>
      ))}
      {hidden > 0 && (
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground justify-start text-[13px]"
          onClick={() => setVisible((v) => Math.min(v + VISIBLE, tasks.length))}
        >
          <ChevronDownIcon />
          {t("s.showMore")}
        </Button>
      )}
    </div>
  )
}
