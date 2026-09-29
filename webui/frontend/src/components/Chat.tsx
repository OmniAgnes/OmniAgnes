import { useEffect, useRef } from "react"
import {
  AlertCircleIcon,
  ChevronRightIcon,
  CircleCheckIcon,
  CircleXIcon,
  Loader2Icon,
  MenuIcon,
  SendHorizontalIcon,
  SquareIcon,
  WrenchIcon,
} from "lucide-react"

import { Markdown } from "@/components/Markdown"
import { Button } from "@/components/ui/button"
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

export interface ToolRun {
  id: string
  name: string
  args: string
  output: string
  isError: boolean
  running: boolean
}

export interface ViewItem {
  id: number
  role: "user" | "assistant"
  text: string
  tools: ToolRun[]
  error?: string
}

interface ChatProps {
  title: string
  subtitle: string
  mode: "build" | "plan"
  items: ViewItem[]
  streaming: boolean
  emptyHint?: React.ReactNode
  onSend: (text: string) => void
  onStop: () => void
  onModeChange: (mode: "build" | "plan") => void
  onOpenSidebar: () => void
}

export function Chat({
  title,
  subtitle,
  mode,
  items,
  streaming,
  emptyHint,
  onSend,
  onStop,
  onModeChange,
  onOpenSidebar,
}: ChatProps) {
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [items])

  const submit = () => {
    const text = inputRef.current?.value.trim()
    if (!text || streaming) return
    if (inputRef.current) inputRef.current.value = ""
    onSend(text)
  }

  return (
    <div className="flex h-full min-w-0 flex-1 flex-col">
      {/* 头部 */}
      <div className="flex items-center gap-2 border-b px-3 py-3 md:px-6">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onOpenSidebar} title="菜单">
          <MenuIcon />
        </Button>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{title}</div>
          <div className="text-muted-foreground truncate text-xs">{subtitle}</div>
        </div>
        <div className="flex items-center gap-1 rounded-md border p-0.5">
          {(["build", "plan"] as const).map((m) => (
            <button
              key={m}
              type="button"
              disabled={streaming}
              onClick={() => onModeChange(m)}
              className={cn(
                "rounded px-2 py-1 text-xs cursor-pointer disabled:opacity-50 md:px-2.5",
                mode === m ? "bg-primary text-primary-foreground" : "hover:bg-accent text-muted-foreground",
              )}
            >
              {m === "build" ? "build" : "plan"}
            </button>
          ))}
        </div>
      </div>

      {/* 消息流 */}
      <ScrollArea className="flex-1">
        <div className="mx-auto flex max-w-3xl flex-col gap-6 px-3 py-6 md:px-6">
          {items.length === 0 && emptyHint}
          {items.map((item) =>
            item.role === "user" ? (
              <div key={item.id} className="flex justify-end">
                <div className="bg-primary text-primary-foreground max-w-[85%] rounded-2xl px-4 py-2.5 text-sm whitespace-pre-wrap">
                  {item.text}
                </div>
              </div>
            ) : (
              <div key={item.id} className="flex flex-col gap-2">
                {item.text && <Markdown text={item.text} />}
                {item.tools.map((t) => (
                  <ToolCallCard key={t.id} run={t} />
                ))}
                {item.error && (
                  <div className="text-destructive flex items-center gap-2 text-sm">
                    <AlertCircleIcon className="size-4" /> {item.error}
                  </div>
                )}
              </div>
            ),
          )}
          <div ref={bottomRef} />
        </div>
      </ScrollArea>

      {/* 输入区 */}
      <div className="border-t px-3 py-3 md:px-6 md:py-4">
        <div className="mx-auto flex max-w-3xl items-end gap-2">
          <Textarea
            ref={inputRef}
            placeholder="输入任务…（/技能名 可直接触发技能）"
            className="max-h-40 min-h-11"
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                submit()
              }
            }}
          />
          {streaming ? (
            <Button variant="secondary" onClick={onStop} title="中断生成">
              <SquareIcon /> 中断
            </Button>
          ) : (
            <Button onClick={submit} title="发送">
              <SendHorizontalIcon /> 发送
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function ToolCallCard({ run }: { run: ToolRun }) {
  return (
    <Collapsible className="border-bg-border rounded-lg border bg-muted/40">
      <CollapsibleTrigger className="group flex w-full items-center gap-2 px-3 py-2 text-left text-xs cursor-pointer">
        {run.running ? (
          <Loader2Icon className="text-muted-foreground size-3.5 animate-spin" />
        ) : run.isError ? (
          <CircleXIcon className="text-destructive size-3.5" />
        ) : (
          <CircleCheckIcon className="text-muted-foreground size-3.5" />
        )}
        <WrenchIcon className="text-muted-foreground size-3.5" />
        <span className="truncate font-medium">{run.name}</span>
        <ChevronRightIcon className="text-muted-foreground ml-auto size-3.5 shrink-0 transition-transform group-data-[state=open]:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="flex flex-col gap-2 border-t px-3 py-2 font-mono text-xs">
          {run.args && run.args !== "{}" && (
            <pre className="text-muted-foreground overflow-x-auto whitespace-pre-wrap">{run.args}</pre>
          )}
          {run.output && <pre className="overflow-x-auto whitespace-pre-wrap">{run.output}</pre>}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}
