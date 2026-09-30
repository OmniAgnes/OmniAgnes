import { useState } from "react"
import {
  CheckIcon,
  ChevronDownIcon,
  CircleAlertIcon,
  Loader2Icon,
  WrenchIcon,
} from "lucide-react"

import { ToolCallView, type ToolRun } from "@/components/ToolCallView"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { cn } from "@/lib/utils"

interface ToolCallGroupProps {
  tools: ToolRun[]
}

/**
 * 工具调用分组：整组折叠成一行「工具调用 ×N」，
 * 用户不关心内部细节时只占一行；展开后逐条查看。
 * 待确认操作不在这里答复——统一由输入框上方的审批浮条处理（见 Chat）。
 */
export function ToolCallGroup({ tools }: ToolCallGroupProps) {
  // 始终默认收起：用户不关心工具调用细节，展开箭头足以表明有点可看。
  const [open, setOpen] = useState(false)

  const running = tools.filter((t) => t.running).length
  const waiting = tools.filter((t) => t.approvalId != null && t.approvalChoice == null).length

  // 失败不在这里标红/计数（用户要求）：整体状态只看是否还在跑，详情用户展开自己看
  const status = (() => {
    if (waiting > 0) return { icon: CircleAlertIcon, cls: "text-amber-600 dark:text-amber-400", text: "等待确认" }
    if (running > 0) return { icon: Loader2Icon, cls: "text-muted-foreground", text: "执行中" }
    return { icon: CheckIcon, cls: "text-muted-foreground", text: "已完成" }
  })()
  const StatusIcon = status.icon

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <div className="flex items-center gap-1.5">
        <CollapsibleTrigger className="group text-muted-foreground hover:text-foreground hover:bg-muted/50 flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-2 py-1 text-xs transition-colors cursor-pointer">
          <StatusIcon className={cn("size-3.5 shrink-0", status.cls, running > 0 && "animate-spin")} />
          <WrenchIcon className="size-3.5 shrink-0" />
          <span className="shrink-0">工具调用</span>
          <span className="text-muted-foreground/80 tabular-nums">×{tools.length}</span>
          <span className={cn("truncate text-[11px]", status.cls)}>{status.text}</span>
          <ChevronDownIcon className="ml-auto size-3.5 shrink-0 transition-transform group-data-[state=open]:rotate-180" />
        </CollapsibleTrigger>
      </div>
      <CollapsibleContent>
        <div className="flex flex-col gap-0.5 pt-0.5">
          {tools.map((t) => (
            <ToolCallView key={t.id} run={t} />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
