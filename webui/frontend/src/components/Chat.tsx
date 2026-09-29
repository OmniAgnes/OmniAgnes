import { useEffect, useMemo, useRef } from "react"
import {
  AlertCircleIcon,
  Loader2Icon,
  MenuIcon,
  SendHorizontalIcon,
  SquareIcon,
} from "lucide-react"

import { Markdown } from "@/components/Markdown"
import { ToolCallGroup } from "@/components/ToolCallGroup"
import type { ToolRun } from "@/components/ToolCallView"
import { Button } from "@/components/ui/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { cn } from "@/lib/utils"

export type { ToolRun } from "@/components/ToolCallView"

/** 一次待确认的写操作 */
export interface PendingApproval {
  id: string
  callId: string
  name: string
  args: string
}

export interface ViewItem {
  id: number
  role: "user" | "assistant"
  text: string
  tools: ToolRun[]
  error?: string
  /** LLM 迭代轮次：一次提问会跑多轮，每轮 = 一段文本 + 若干工具调用 */
  round?: number
}

interface ChatProps {
  title: string
  mode: "build" | "plan"
  items: ViewItem[]
  streaming: boolean
  /** 收到用户消息后、模型首个 token 前的「思考中」状态 */
  thinking: boolean
  models: string[]
  model: string
  emptyHint?: React.ReactNode
  onSend: (text: string) => void
  onStop: () => void
  onApprove: (id: string, allow: boolean) => void
  onModeChange: (mode: "build" | "plan") => void
  onModelChange: (model: string) => void
  onOpenSidebar: () => void
}

export function Chat({
  title,
  mode,
  items,
  streaming,
  thinking,
  models,
  model,
  emptyHint,
  onSend,
  onStop,
  onApprove,
  onModeChange,
  onModelChange,
  onOpenSidebar,
}: ChatProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  // 用户手动上滚后不再强行拉回底部，直到重新滚到底
  const stickRef = useRef(true)

  // 记录是否贴近底部：贴底才自动跟随新内容
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80
  }

  // 滚到底（切任务、首次渲染）时立刻归位
  const jumpToBottom = () => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }

  useEffect(() => {
    jumpToBottom()
  }, [])

  useEffect(() => {
    if (!stickRef.current) return
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [items, thinking])

  const submit = () => {
    const text = inputRef.current?.value.trim()
    if (!text || streaming) return
    if (inputRef.current) inputRef.current.value = ""
    onSend(text)
  }

  // 处理中指示：整个生成期间都常驻消息流末尾。
  // ① thinking：等首个 token、或一轮工具跑完等下一轮推理的间隙；
  // ② streaming：模型还在输出（哪怕正在流式吐字），末尾始终有个"还在忙"的提示。
  const showThinking = streaming || thinking

  // 所有轮次的工具调用汇总成一组，统一折叠在消息流最上方——用户不关心内部细节。
  const allTools = useMemo(() => items.flatMap((it) => it.tools), [items])

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
      {/* 头部：只显示任务标题 */}
      <div className="flex items-center gap-2 px-3 py-3 md:px-6">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onOpenSidebar} title="菜单">
          <MenuIcon />
        </Button>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium">{title}</div>
        </div>
      </div>

      {/* 消息流：原生滚动容器（min-h-0 保证在 flex 里能收缩） */}
      <div ref={scrollRef} onScroll={onScroll} className="omni-scroll min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-3 py-6 md:px-6">
          {/* 顶部固定区：工具调用折叠组，默认收起 */}
          {allTools.length > 0 && <ToolCallGroup tools={allTools} onApprove={onApprove} />}

          {items.length === 0 && emptyHint}
          {items.map((item) =>
            item.role === "user" ? (
              <div key={item.id} className="flex justify-end">
                <div className="bg-primary text-primary-foreground max-w-[85%] rounded-2xl px-4 py-2.5 text-sm whitespace-pre-wrap">
                  {item.text}
                </div>
              </div>
            ) : (
              <div key={item.id} className="flex flex-col gap-1.5">
                {item.text && <Markdown text={item.text} />}
                {item.error && (
                  <div className="text-destructive flex items-center gap-2 text-sm">
                    <AlertCircleIcon className="size-4" /> {item.error}
                  </div>
                )}
              </div>
            ),
          )}

          {/* 底部固定区：处理中动画，永远在消息流最后 */}
          {showThinking && <ProcessingIndicator />}
        </div>
      </div>

      {/* 输入区：左=访问模式，右=模型选择 + 发送 */}
      <div className="px-3 pb-3 md:px-6 md:pb-4">
        <div className="bg-card mx-auto max-w-3xl rounded-2xl border p-2 shadow-xs">
          <Textarea
            ref={inputRef}
            placeholder="输入任务…（/技能名 可直接触发技能）"
            className="max-h-48 min-h-24 resize-none border-0 bg-transparent px-2 shadow-none focus-visible:ring-0"
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                submit()
              }
            }}
          />
          <div className="flex items-center justify-between gap-2 px-1 pt-1">
            {/* 左：访问模式 */}
            <div className="bg-muted/60 flex items-center gap-0.5 rounded-md p-0.5">
              {(
                [
                  { id: "build", label: "完全访问", tip: "完全访问：可直接修改文件、执行命令" },
                  { id: "plan", label: "普通访问", tip: "普通访问：写文件、执行命令前会先询问你" },
                ] as const
              ).map((m) => (
                <button
                  key={m.id}
                  type="button"
                  disabled={streaming}
                  title={m.tip}
                  onClick={() => onModeChange(m.id)}
                  className={cn(
                    "rounded-[5px] px-2.5 py-1 text-xs cursor-pointer disabled:opacity-50",
                    mode === m.id
                      ? "bg-background text-foreground shadow-xs"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {m.label}
                </button>
              ))}
            </div>

            {/* 右：模型选择 + 发送/中断 */}
            <div className="flex shrink-0 items-center gap-1.5">
              {models.length > 0 && (
                <Select value={model || undefined} onValueChange={onModelChange}>
                  <SelectTrigger
                    size="sm"
                    title="切换模型"
                    className="bg-muted/60 max-w-28 shrink-0 gap-1 border-0 shadow-none sm:max-w-44"
                  >
                    <SelectValue placeholder="模型" />
                  </SelectTrigger>
                  <SelectContent>
                    {models.map((m) => (
                      <SelectItem key={m} value={m}>
                        {m}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              {streaming ? (
                <Button variant="secondary" size="icon" className="rounded-full" onClick={onStop} title="中断生成">
                  <SquareIcon />
                </Button>
              ) : (
                <Button size="icon" className="rounded-full" onClick={submit} title="发送">
                  <SendHorizontalIcon />
                </Button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

/** 处理中动画：固定在消息流最末尾，跟着工具调用组下方 */
function ProcessingIndicator() {
  return (
    <div className="text-muted-foreground flex items-center gap-2 px-2 py-1 text-sm">
      <Loader2Icon className="size-3.5 shrink-0 animate-spin" />
      <span>处理中…</span>
    </div>
  )
}
