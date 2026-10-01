import { useEffect, useMemo, useRef, useState } from "react"
import {
  AlertCircleIcon,
  BrainIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ChevronUpIcon,
  CircleAlertIcon,
  CircleCheckIcon,
  CircleDashedIcon,
  CopyIcon,
  FolderIcon,
  GlobeIcon,
  HelpCircleIcon,
  ListChecksIcon,
  ListTodoIcon,
  Loader2Icon,
  MenuIcon,
  MessageSquareIcon,
  PaperclipIcon,
  PencilLineIcon,
  SendHorizontalIcon,
  SlashIcon,
  SquareIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import { Markdown } from "@/components/Markdown"
import { ModelPicker } from "@/components/ModelPicker"
import { ToolCallGroup } from "@/components/ToolCallGroup"
import { toolApprovalLabel, type ToolRun } from "@/components/ToolCallView"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Textarea } from "@/components/ui/textarea"
import { todosFromJson, type TodoItem } from "@/lib/todo"
import { cn } from "@/lib/utils"
import type { AttachmentInfo, SkillInfo } from "@/types"
import { MediaList } from "./ToolCallView"

const isDesktop = typeof window !== "undefined" && Boolean(window.omniDesktop)

export type { ToolRun } from "@/components/ToolCallView"

/** 模型按供应商分组：一级 = 供应商名，二级 = 模型名 */
export interface ModelGroup {
  provider: string
  models: string[]
}

/** 一次待确认的写操作 */
export interface PendingApproval {
  id: string
  callId: string
  name: string
  args: string
}

/** 处理中排队的用户消息 */
export interface QueuedMsg {
  id: number
  text: string
  /** 随消息一起发送的附件 */
  files?: File[]
}

export interface ViewItem {
  id: number
  role: "user" | "assistant"
  text: string
  tools: ToolRun[]
  error?: string
  /** 该消息随带的附件文件名（仅用户消息，展示用） */
  attachments?: string[]
  /** 本轮产出的多媒体（生成图片/视频/音频），在回答里集中展示 */
  media?: AttachmentInfo[]
  /** 模型思考/推理内容（本轮；不持久化，刷新后消失） */
  thinking?: string
  /** 本轮还在流式输出思考内容 */
  thinkingActive?: boolean
  /** LLM 迭代轮次：一次提问会跑多轮，每轮 = 一段文本 + 若干工具调用 */
  round?: number
}

interface ChatProps {
  title: string
  /** 当前任务归属：文件夹名（项目内任务）或 "任务"（默认任务），显示在头部 */
  source: string
  mode: "build" | "plan"
  items: ViewItem[]
  streaming: boolean
  /** 收到用户消息后、模型首个 token 前的「思考中」状态 */
  thinking: boolean
  /** 上游瞬时故障的自动重试进度；非空时「处理中」指示器显示「重试 n/10」 */
  retry?: { attempt: number; max: number }
  /** 处理中排队的消息（显示在输入框上方，可发送/编辑/删除） */
  queue: QueuedMsg[]
  /** seq 递增即把 text 回填输入框（队列「编辑」） */
  draftSeed: { text: string; seq: number }
  models: ModelGroup[]
  model: string
  /** 项目可用技能（供输入 / 时提示补全） */
  skills: SkillInfo[]
  /** 递增即触发输入框聚焦（新建任务、打开任务、点 TAB 后自动对焦） */
  focusSignal?: number
  /** 递增即把消息列表滚到底部（侧栏点击任务、重开当前任务） */
  taskOpenSignal?: number
  emptyHint?: React.ReactNode
  onSend: (text: string, files?: File[]) => void
  onStop: () => void
  /** 队列项：中断当前轮并立即发送这条 */
  onQueueDispatch: (id: number) => void
  /** 队列项：回填输入框编辑 */
  onQueueEdit: (id: number) => void
  /** 队列项：删除 */
  onQueueRemove: (id: number) => void
  /** 历史用户消息：回填输入框，编辑（或原样）后重发 */
  onEditMessage: (text: string) => void
  onApprove: (id: string, allow: boolean) => void
  /** 回答一次 ask 工具的提问 */
  onAnswer: (id: string, answer: string) => void
  onModeChange: (mode: "build" | "plan") => void
  onModelChange: (model: string) => void
  onOpenSidebar: () => void
  /** 内置浏览器面板是否展开（仅桌面端有该入口） */
  browserOpen?: boolean
  onToggleBrowser?: () => void
}

export function Chat({
  title,
  source,
  mode,
  items,
  streaming,
  thinking,
  retry,
  queue,
  draftSeed,
  models,
  model,
  skills,
  focusSignal,
  taskOpenSignal,
  emptyHint,
  onSend,
  onStop,
  onQueueDispatch,
  onQueueEdit,
  onQueueRemove,
  onEditMessage,
  onApprove,
  onAnswer,
  onModeChange,
  onModelChange,
  onOpenSidebar,
  browserOpen,
  onToggleBrowser,
}: ChatProps) {
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  // 用户手动上滚后不再强行拉回底部，直到重新滚到底
  const stickRef = useRef(true)
  // 是否离开底部：离开后显示「回到底部」悬浮按钮
  const [notBottom, setNotBottom] = useState(false)
  // 待发送的附件：随下一条消息一起上传发出
  const [pendingFiles, setPendingFiles] = useState<File[]>([])

  // 受控输入：斜杠提示需要读取当前文本
  const [draft, setDraft] = useState("")
  // 斜杠补全：仅当整段输入是 "/xxx"（无空格/换行）时激活
  const slashOpen = /^\/\S*$/.test(draft) && !streaming
  const slashMatch = useMemo(() => {
    if (!slashOpen) return []
    const q = draft.slice(1).toLowerCase()
    return skills.filter((s) => s.name.toLowerCase().startsWith(q)).slice(0, 12)
  }, [slashOpen, draft, skills])
  const [slashIdx, setSlashIdx] = useState(0)
  useEffect(() => setSlashIdx(0), [draft])

  // 记录是否贴近底部：贴底才自动跟随新内容；离底显示「回到底部」悬浮按钮
  const onScroll = () => {
    const el = scrollRef.current
    if (!el) return
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 80
    stickRef.current = near
    setNotBottom((prev) => (prev === !near ? prev : !near))
  }

  // 滚到底（切任务、首次渲染、点「回到底部」按钮）时立刻归位
  const jumpToBottom = () => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
    stickRef.current = true
    setNotBottom(false)
  }

  useEffect(() => {
    jumpToBottom()
  }, [])

  // 侧栏点击任务（含重开当前任务）：立即归底并恢复贴底跟随
  useEffect(() => {
    if (taskOpenSignal == null) return
    jumpToBottom()
  }, [taskOpenSignal])

  // 新建任务 / 打开任务 / 点「任务」TAB 后，右侧输入框自动对焦
  useEffect(() => {
    if (focusSignal == null) return
    // 等消息列表渲染完再对焦，否则可能被后续渲染抢走
    const t = setTimeout(() => inputRef.current?.focus(), 0)
    return () => clearTimeout(t)
  }, [focusSignal])

  // 队列「编辑」：seq 变化时把内容回填输入框
  useEffect(() => {
    if (draftSeed.seq === 0) return
    setDraft(draftSeed.text)
    inputRef.current?.focus()
  }, [draftSeed])

  // 切换任务时列表整体替换：归位到底部（jumpToBottom 会恢复贴底跟随）
  useEffect(() => {
    jumpToBottom()
  }, [title])

  useEffect(() => {
    if (!stickRef.current) return
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [items, thinking])

  const submit = () => {
    const text = draft.trim()
    if (!text && pendingFiles.length === 0) return
    setDraft("")
    const files = pendingFiles
    setPendingFiles([])
    // 处理中发送不再阻止：App 会把它放进队列（输入框上方可见）
    onSend(text, files.length > 0 ? files : undefined)
  }

  /** 附件选择：支持图片 / PDF / Office 文档 / 音视频等，交给模型用工具/MCP 处理 */
  const ATTACH_ACCEPT =
    "image/*,.pdf,.ppt,.pptx,.doc,.docx,.xls,.xlsx,.csv,.txt,.md,.mp3,.wav,.m4a,.mp4,.mov,.zip"

  /** 斜杠补全：把选中的技能名写进输入框（保留光标后的内容一般为空） */
  const acceptSlash = (name: string) => {
    setDraft("/" + name + " ")
    requestAnimationFrame(() => inputRef.current?.focus())
  }

  // 处理中指示：整个生成期间都常驻消息流末尾。
  // ① thinking：等首个 token、或一轮工具跑完等下一轮推理的间隙；
  // ② streaming：模型还在输出（哪怕正在流式吐字），末尾始终有个"还在忙"的提示。
  const showThinking = streaming || thinking

  // 待确认的写操作：普通访问模式下后端阻塞等答复，浮条固定在输入框上方，
  // 保证用户不看消息流也能第一时间发现要确认。
  const pending = useMemo(() => {
    for (let i = items.length - 1; i >= 0; i--) {
      for (const t of items[i].tools) {
        if (t.approvalId != null && t.approvalChoice == null) return t
      }
    }
    return null
  }, [items])

  // 最近一条错误（429 限流等）：只看末条消息，新提问后自动消失；
  // 浮在输入框上方保证一眼可见，消息流内不再重复显示。
  const lastError = items.at(-1)?.error ?? ""

  // 待回答的提问（ask 工具）：挂在工具卡上，横幅固定在输入框上方
  const pendingQ = useMemo(() => {
    for (let i = items.length - 1; i >= 0; i--) {
      for (const t of items[i].tools) {
        if (t.questionId != null && t.questionAnswer === undefined) return t
      }
    }
    return null
  }, [items])

  // 任务清单：取最近一次 todo 工具调用的参数（全量覆盖语义，最后一次即最新）
  const todos = useMemo<TodoItem[]>(() => {
    for (let i = items.length - 1; i >= 0; i--) {
      for (let j = items[i].tools.length - 1; j >= 0; j--) {
        const t = items[i].tools[j]
        if (t.name === "todo") return todosFromJson(t.args)
      }
    }
    return []
  }, [items])
  // 显隐规则：清单只作为「当前轮次的实时进度」——处理中（streaming/thinking）且有未完成项才显示；
  // 本轮结束立即收起（哪怕是中途停下），避免旧清单常驻输入框上方；
  // 下一轮再次出现 todo 时自动弹出。完整的清单记录保留在消息流的 todo 工具卡里可回看。
  const todosOpen =
    showThinking && todos.length > 0 && todos.some((t) => t.status !== "completed")

  /**
   * 渲染消息流。
   *
   * 工具调用放在**对应那次提问的 AI 回复上方**（不是界面最上方）——
   * 用户可能连着问好几轮，每轮的调用得跟着自己那轮走，不能全堆在顶部。
   * 做法：遇到 user 消息就开一个「段」，把该段之后、下一个 user 之前的所有
   * assistant 消息里的工具收集起来，插在这个 user 气泡下面。
   */
  function renderItems() {
    const blocks: { user: ViewItem | null; tools: ToolRun[]; replies: ViewItem[] }[] = []
    for (const item of items) {
      if (item.role === "user") {
        blocks.push({ user: item, tools: [], replies: [] })
      } else {
        const b = blocks.at(-1)
        if (b) {
          b.tools.push(...item.tools)
          b.replies.push(item)
        } else {
          // 没有前导 user 的孤立 assistant（例如历史只存了回复）
          blocks.push({ user: null, tools: item.tools, replies: [item] })
        }
      }
    }

    return blocks.map((b, bi) => (
      <div key={b.user?.id ?? `blk-${bi}`} className="flex flex-col gap-4">
        {b.user && <UserBubble text={b.user.text} attachments={b.user.attachments} onEdit={onEditMessage} />}

        {/* 本次提问的产出：工具折叠组 + 各轮回复。两者同属一组内容，贴紧排（gap-1.5），
            不吃段内的 gap-4 —— 否则工具行和它下面的回复之间会留出很大空白。
            注意：只渲染有文本的回复——中间轮次（纯工具调用）的空 assistant 消息
            若也渲染成 div，每个都会吃一个 gap，多轮时空白越叠越大。 */}
        {(b.tools.length > 0 || b.replies.some((r) => r.text || r.thinking || r.thinkingActive)) && (
          <div className="flex flex-col gap-1.5">
            {b.tools.length > 0 && <ToolCallGroup tools={b.tools} />}
            {b.replies
              .filter((r) => r.text || r.thinking || r.thinkingActive || r.media?.length)
              .map((item) => (
                <div key={item.id} className="flex flex-col gap-1.5">
                  {(item.thinking || item.thinkingActive) && (
                    <ThinkingBlock text={item.thinking} active={item.thinkingActive} />
                  )}
                  {item.text && <Markdown text={item.text} />}
                  {/* 本轮产出的多媒体（生成图片/视频/音频）：在最终回答里集中展示 */}
                  {item.media && item.media.length > 0 && <MediaList atts={item.media} />}
                  {/* 错误浮在输入框上方横幅显示，消息流内不再重复 */}
                </div>
              ))}
          </div>
        )}
      </div>
    ))
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
      {/* 头部：来源标识（文件夹名 / 任务）+ 任务标题。
          桌面端这行挪进了自绘标题栏（窗口按钮左边），此处仅保留移动端菜单按钮；webui 不变。 */}
      <div className="flex items-center gap-2 px-3 py-3 md:px-6">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onOpenSidebar} title="菜单">
          <MenuIcon />
        </Button>
        {!isDesktop && (
          <div className="flex min-w-0 items-center gap-2">
            <span className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs" title={source}>
              {source !== "任务" ? <FolderIcon className="size-3.5" /> : <MessageSquareIcon className="size-3.5" />}
              <span className="max-w-40 truncate">{source}</span>
            </span>
            <span className="text-muted-foreground/60 shrink-0">/</span>
            <div className="truncate text-sm font-medium">{title}</div>
          </div>
        )}
      </div>

      {/* 消息流：原生滚动容器（min-h-0 保证在 flex 里能收缩）；
          上滚离开底部时右下角浮现「回到底部」按钮 */}
      <div className="relative min-h-0 flex-1">
        <div ref={scrollRef} onScroll={onScroll} className="omni-scroll h-full overflow-y-auto">
          <div className="mx-auto flex max-w-3xl flex-col gap-4 px-3 py-6 md:px-6">
            {items.length === 0 && emptyHint}
            {renderItems()}

            {/* 底部固定区：处理中动画，永远在消息流最后 */}
            {showThinking && <ProcessingIndicator retry={retry} />}
          </div>
        </div>
        {notBottom && (
          <Button
            variant="ghost"
            size="icon"
            onClick={jumpToBottom}
            title="回到底部"
            className="bg-card text-muted-foreground hover:text-foreground absolute left-1/2 -translate-x-1/2 bottom-3 z-20 rounded-full border shadow-md"
          >
            <ChevronDownIcon />
          </Button>
        )}
      </div>

      {/* 输入区：提问/审批/任务清单/错误浮条固定在输入框上方；左=访问模式，右=模型选择 + 发送 */}
      <div className="px-3 pb-3 md:px-6 md:pb-4">
        {pendingQ && <QuestionBanner run={pendingQ} onAnswer={onAnswer} />}
        {pending && !pendingQ && <ApprovalBanner run={pending} onApprove={onApprove} />}
        {todosOpen && !pendingQ && <TodoPanel items={todos} active={streaming} />}
        {queue.length > 0 && (
          <QueuePanel queue={queue} onDispatch={onQueueDispatch} onEdit={onQueueEdit} onRemove={onQueueRemove} />
        )}
        {lastError && !pending && !pendingQ && (
          <div className="border-destructive/40 bg-destructive/8 text-destructive mx-auto mb-2 flex max-w-3xl items-start gap-2 rounded-xl border px-3.5 py-2.5 text-sm">
            <AlertCircleIcon className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{lastError}</span>
          </div>
        )}
        <div className="bg-card relative mx-auto max-w-3xl rounded-2xl border p-2 shadow-xs">
          <input
            ref={fileRef}
            type="file"
            multiple
            accept={ATTACH_ACCEPT}
            className="hidden"
            onChange={(e) => {
              const picked = [...(e.target.files ?? [])]
              if (picked.length > 0) setPendingFiles((fs) => [...fs, ...picked])
              e.target.value = ""
            }}
          />
          {slashOpen && slashMatch.length > 0 && (
            <div className="bg-popover text-popover-foreground absolute bottom-full left-2 mb-2 max-h-64 w-72 overflow-auto rounded-lg border p-1 shadow-lg">
              {slashMatch.map((s, i) => (
                <button
                  key={s.name}
                  type="button"
                  onMouseDown={(e) => {
                    e.preventDefault()
                    acceptSlash(s.name)
                  }}
                  onMouseEnter={() => setSlashIdx(i)}
                  className={cn(
                    "flex w-full flex-col items-start gap-0.5 rounded-md px-2 py-1.5 text-left cursor-pointer",
                    i === slashIdx && "bg-accent text-accent-foreground",
                  )}
                >
                  <span className="flex items-center gap-1.5 text-sm">
                    <SlashIcon className="text-muted-foreground size-3.5" />
                    <span className="font-medium">{s.name}</span>
                    <span className="text-muted-foreground text-[10px]">
                      {s.source === "project" ? "项目" : "全局"}
                    </span>
                  </span>
                  {s.description && (
                    <span className="text-muted-foreground line-clamp-1 pl-5 text-xs">{s.description}</span>
                  )}
                </button>
              ))}
            </div>
          )}
          {pendingFiles.length > 0 && (
            <div className="flex flex-wrap gap-1.5 px-1 pb-1 pt-0.5">
              {pendingFiles.map((f, i) => (
                <span
                  key={`${f.name}-${i}`}
                  className="bg-muted flex max-w-56 items-center gap-1 rounded-md border px-1.5 py-0.5 text-[11px]"
                  title={f.name}
                >
                  <PaperclipIcon className="text-muted-foreground size-3 shrink-0" />
                  <span className="min-w-0 truncate">{f.name}</span>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground cursor-pointer"
                    onClick={() => setPendingFiles((fs) => fs.filter((_, j) => j !== i))}
                    title="移除"
                  >
                    <XIcon className="size-3" />
                  </button>
                </span>
              ))}
            </div>
          )}
          <Textarea
            ref={inputRef}
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="输入任务…（输入 / 触发技能）"
            className="max-h-48 min-h-24 resize-none border-0 bg-transparent px-2 shadow-none focus-visible:ring-0"
            onKeyDown={(e) => {
              if (slashOpen && slashMatch.length > 0) {
                if (e.key === "ArrowDown") {
                  e.preventDefault()
                  setSlashIdx((n) => (n + 1) % slashMatch.length)
                  return
                }
                if (e.key === "ArrowUp") {
                  e.preventDefault()
                  setSlashIdx((n) => (n - 1 + slashMatch.length) % slashMatch.length)
                  return
                }
                if (e.key === "Tab" || (e.key === "Enter" && !e.nativeEvent.isComposing)) {
                  e.preventDefault()
                  acceptSlash(slashMatch[slashIdx].name)
                  return
                }
                if (e.key === "Escape") {
                  e.preventDefault()
                  // 临时收起：置一个空格打断 "/" 前缀匹配
                  setDraft(" ")
                  return
                }
              }
              if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
                e.preventDefault()
                submit()
              }
            }}
          />
          <div className="flex items-center justify-between gap-2 px-1 pt-1">
            {/* 左：添加附件 + 访问模式（下拉，样式对齐模型选择） */}
            <div className="flex min-w-0 items-center gap-1">
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground hover:text-foreground shrink-0"
                onClick={() => fileRef.current?.click()}
                title="添加图片或文件"
              >
                <PaperclipIcon />
              </Button>
              <ModePicker mode={mode} disabled={streaming} onChange={onModeChange} />
              {/* 内置浏览器：地球图标紧跟访问模式，展开态高亮（仅桌面端） */}
              {isDesktop && onToggleBrowser && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={onToggleBrowser}
                  title={browserOpen ? "收起内置浏览器" : "打开内置浏览器"}
                  className={cn("shrink-0", browserOpen ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  <GlobeIcon />
                </Button>
              )}
            </div>

            {/* 右：模型选择（供应商 → 模型 二级）+ 发送/中断 */}
            <div className="flex shrink-0 items-center gap-1.5">
              {models.length > 0 && (
                <ModelPicker groups={models} model={model} onChange={onModelChange} />
              )}
              {streaming ? (
                <Button variant="secondary" size="icon" className="rounded-full" onClick={onStop} title="中断生成">
                  {/* 实心方块：更像"停止"语义（lucide 默认是描边空心） */}
                  <SquareIcon fill="currentColor" />
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

/** 处理中动画：固定在消息流最末尾，跟着工具调用组下方。
 * 上游自动重试中显示「重试 n/10」（重试结束、模型恢复输出后自动回到「处理中…」）。 */
function ProcessingIndicator({ retry }: { retry?: { attempt: number; max: number } }) {
  return (
    <div className="text-muted-foreground flex items-center gap-2 py-1 pr-2 text-sm">
      <Loader2Icon className="size-3.5 shrink-0 animate-spin" />
      <span>{retry ? `重试 ${retry.attempt}/${retry.max}` : "处理中…"}</span>
    </div>
  )
}

/** 思考折叠行：模型推理内容收在这里（同工具调用组，默认收起）。
 * 推理进行中显示「思考中…」，结束后固定为「思考」。 */
function ThinkingBlock({ text, active }: { text?: string; active?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="w-full">
      <CollapsibleTrigger className="group text-muted-foreground hover:bg-muted/50 flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-xs transition-colors cursor-pointer">
        <BrainIcon className="size-3.5 shrink-0" />
        <span className={cn(active && "opacity-80")}>{active ? "思考中…" : "思考"}</span>
        <ChevronRightIcon className="ml-auto size-3.5 shrink-0 transition-transform group-data-[state=open]:rotate-90" />
      </CollapsibleTrigger>
      <CollapsibleContent>
        {text && (
          <div className="bg-muted/40 text-muted-foreground mx-3 mb-1 max-h-64 overflow-y-auto rounded-md p-2 text-xs whitespace-pre-wrap break-words">
            {text}
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

/** 超过这个字符数或行数的用户消息默认收起，点「展开」看全部 */
const COLLAPSE_CHARS = 300
const COLLAPSE_LINES = 8

/** 用户气泡：内容多时默认收起（点击气泡或「展开」看全部）；悬停浮动显示 编辑/复制 按钮，不占布局空间；附件以标签展示。 */
function UserBubble({ text, attachments, onEdit }: { text: string; attachments?: string[]; onEdit: (text: string) => void }) {
  const [open, setOpen] = useState(false)
  const [copied, setCopied] = useState(false)
  const long = text.length > COLLAPSE_CHARS || text.split("\n").length > COLLAPSE_LINES
  const toggle = () => setOpen((o) => !o)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // 桌面端 file:// 等非安全上下文剪贴板 API 不可用时降级
      const ta = document.createElement("textarea")
      ta.value = text
      ta.style.position = "fixed"
      ta.style.opacity = "0"
      document.body.appendChild(ta)
      ta.select()
      document.execCommand("copy")
      ta.remove()
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  // 复制按钮绝对定位悬浮在气泡外侧（不占布局），悬停气泡才浮现。
  // 关键：按钮在气泡 group 之外，只靠 group-hover 的话鼠标移向按钮时会先离开
  // group 而按钮瞬间消失，人根本点不到。所以外层容器常驻 pointer-events
  // （opacity:0 仍能接收指针），自身 hover / focus 时保持可见，并用透明热区
  // （伪元素）向气泡方向延伸 4px，桥接气泡与按钮之间的空隙。
  const copyBtn = (
    <span
      className={cn(
        "group/copy absolute z-10 flex items-center gap-1.5 opacity-0 transition-opacity",
        "hover:opacity-100 focus-within:opacity-100 group-hover:opacity-100 max-md:opacity-100",
        long ? "top-1/2 right-full -translate-y-1/2 mr-0.5" : "top-full right-0 mt-0.5",
      )}
    >
      {/* 热区：盖住气泡与按钮之间的缝隙，鼠标经过时不会脱离 hover */}
      <span
        aria-hidden
        className={cn("absolute", long ? "-right-1 inset-y-0 w-1" : "-top-1 inset-x-0 h-1")}
      />
      <Button
        variant="ghost"
        size="icon-sm"
        className="bg-background text-muted-foreground hover:text-foreground pointer-events-auto relative shadow-sm"
        onClick={() => onEdit(text)}
        title="编辑后重发（原样发送也可以）"
      >
        <PencilLineIcon />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        className="bg-background text-muted-foreground hover:text-foreground pointer-events-auto relative shadow-sm"
        onClick={copy}
        title="复制"
      >
        {copied ? <CheckIcon className="text-emerald-600 dark:text-emerald-400" /> : <CopyIcon />}
      </Button>
    </span>
  )
  return (
    <div className="flex justify-end">
      <div className="group relative flex max-w-[85%] flex-col items-end gap-0.5">
        <div
          className={cn(
            "bg-primary text-primary-foreground min-w-0 rounded-2xl px-4 py-2.5 text-sm whitespace-pre-wrap",
            long && !open && "line-clamp-5 cursor-pointer",
          )}
          onClick={long && !open ? toggle : undefined}
        >
          {text}
          {attachments && attachments.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {attachments.map((a, i) => (
                <span
                  key={`${a}-${i}`}
                  className="flex max-w-52 items-center gap-1 rounded-md bg-white/15 px-1.5 py-0.5 text-[11px]"
                  title={a}
                >
                  <PaperclipIcon className="size-3 shrink-0 opacity-80" />
                  <span className="min-w-0 truncate">{a}</span>
                </span>
              ))}
            </div>
          )}
        </div>
        {long && (
          <div className="relative pr-1">
            <button
              type="button"
              onClick={toggle}
              className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-[11px] cursor-pointer"
            >
              {open ? "收起" : "展开"}
              {open ? <ChevronUpIcon className="size-3" /> : <ChevronDownIcon className="size-3" />}
            </button>
            {copyBtn}
          </div>
        )}
        {!long && copyBtn}
      </div>
    </div>
  )
}

/** 发送队列面板：处理中排队的消息，悬停显示 发送/编辑/删除。 */
function QueuePanel({
  queue,
  onDispatch,
  onEdit,
  onRemove,
}: {
  queue: QueuedMsg[]
  onDispatch: (id: number) => void
  onEdit: (id: number) => void
  onRemove: (id: number) => void
}) {
  return (
    <div className="mx-auto mb-2 flex max-w-3xl flex-col gap-1">
      <div className="text-muted-foreground px-1 text-[11px]">
        发送队列（{queue.length}）· 当前轮结束后自动依次发送
      </div>
      {queue.map((q) => (
        <div
          key={q.id}
          className="group bg-card flex items-center gap-2 rounded-lg border px-2.5 py-1.5"
        >
          <ListChecksIcon className="text-muted-foreground size-3.5 shrink-0" />
          <span
            className="text-muted-foreground min-w-0 flex-1 truncate text-xs"
            title={q.text || undefined}
          >
            {q.text || (
              <span className="flex items-center gap-1">
                <PaperclipIcon className="size-3" />
                {q.files?.length ?? 0} 个附件
              </span>
            )}
          </span>
          {/* 悬停浮现操作：发送=中断当前轮立即发这条 */}
          <div className="flex shrink-0 gap-1 opacity-0 transition-opacity group-hover:opacity-100">
            <Button
              size="sm"
              variant="ghost"
              className="h-6 gap-1 px-1.5 text-[11px]"
              onClick={() => onDispatch(q.id)}
              title="中断当前执行，立即发送这条"
            >
              <SendHorizontalIcon className="size-3" />
              发送
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-6 gap-1 px-1.5 text-[11px]"
              onClick={() => onEdit(q.id)}
              title="移回输入框编辑"
            >
              <PencilLineIcon className="size-3" />
              编辑
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="text-muted-foreground hover:text-destructive h-6 gap-1 px-1.5 text-[11px]"
              onClick={() => onRemove(q.id)}
              title="删除"
            >
              <Trash2Icon className="size-3" />
              删除
            </Button>
          </div>
        </div>
      ))}
    </div>
  )
}

/** 访问模式下拉：与模型选择同款交互（向上弹出、点外/Esc 关闭）。完全访问排下面。 */
const MODES = [
  { id: "plan", label: "主动询问", desc: "写文件、执行命令前会先询问你" },
  { id: "build", label: "完全访问", desc: "可直接修改文件、执行命令" },
] as const

function ModePicker({
  mode,
  disabled,
  onChange,
}: {
  mode: "build" | "plan"
  disabled: boolean
  onChange: (m: "build" | "plan") => void
}) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])
  // 找不到时回落到「完全访问」（默认权限）
  const cur = MODES.find((m) => m.id === mode) ?? MODES[MODES.length - 1]
  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        type="button"
        title="切换访问模式"
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className="bg-muted/60 hover:bg-muted flex cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors disabled:opacity-50"
      >
        <span className="font-medium">{cur.label}</span>
        <ChevronUpIcon className={cn("size-3.5 shrink-0 opacity-60 transition-transform", open && "rotate-180")} />
      </button>
      {open && (
        <div className="bg-popover text-popover-foreground absolute bottom-full left-0 z-50 mb-1.5 w-52 overflow-hidden rounded-md border p-1 shadow-md">
          {MODES.map((m) => (
            <button
              key={m.id}
              type="button"
              onClick={() => {
                onChange(m.id)
                setOpen(false)
              }}
              className={cn(
                "flex w-full cursor-pointer items-start gap-2 rounded-[5px] px-2 py-1.5 text-left transition-colors",
                m.id === mode ? "bg-accent text-accent-foreground" : "hover:bg-accent/50",
              )}
            >
              <CheckIcon className={cn("mt-0.5 size-3.5 shrink-0", m.id === mode ? "opacity-100" : "opacity-0")} />
              <span className="flex min-w-0 flex-col">
                <span className={cn("text-xs", m.id === mode ? "font-medium" : "")}>{m.label}</span>
                <span className="text-muted-foreground text-[11px]">{m.desc}</span>
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

/** 待确认横幅：普通访问模式下固定在输入框上方，必须答复才能继续。 */
function ApprovalBanner({  run,
  onApprove,
}: {
  run: ToolRun
  onApprove: (id: string, allow: boolean) => void
}) {
  const { label, summary } = toolApprovalLabel(run)
  return (
    <div className="border-amber-500/50 bg-amber-500/10 mx-auto mb-2 flex max-w-3xl flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border px-3.5 py-2.5">
      <CircleAlertIcon className="text-amber-600 dark:text-amber-400 size-4 shrink-0" />
      <div className="min-w-0 flex-1 text-sm">
        <span className="text-amber-700 dark:text-amber-400 font-medium">需要你的确认：</span>
        <span className="font-medium">{label}</span>
        {summary && (
          <span className="text-muted-foreground ml-1.5 font-mono text-xs break-all">{summary}</span>
        )}
        <div className="text-muted-foreground mt-0.5 text-xs">普通访问模式下，该操作在你允许前不会执行。</div>
      </div>
      <div className="flex shrink-0 gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-3 text-xs"
          onClick={() => onApprove(run.approvalId!, false)}
        >
          拒绝
        </Button>
        <Button size="sm" className="h-7 px-3 text-xs" onClick={() => onApprove(run.approvalId!, true)}>
          允许
        </Button>
      </div>
    </div>
  )
}

/** 提问横幅：ask 工具向用户要答案，候选项点击即答，也可自由输入。 */
function QuestionBanner({
  run,
  onAnswer,
}: {
  run: ToolRun
  onAnswer: (id: string, answer: string) => void
}) {
  const [free, setFree] = useState("")
  const send = (text: string) => {
    const t = text.trim()
    if (t) onAnswer(run.questionId!, t)
  }
  return (
    <div className="border-sky-500/50 bg-sky-500/8 mx-auto mb-2 flex max-w-3xl flex-col gap-2 rounded-xl border px-3.5 py-2.5">
      <div className="flex items-start gap-2 text-sm">
        <HelpCircleIcon className="text-sky-600 dark:text-sky-400 mt-0.5 size-4 shrink-0" />
        <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">
          <span className="text-sky-700 dark:text-sky-400 font-medium">AI 想问你：</span>
          {run.question}
        </span>
      </div>
      {run.questionOptions && run.questionOptions.length > 0 && (
        <div className="flex flex-wrap gap-1.5 pl-6">
          {run.questionOptions.map((o) => (
            <button
              key={o}
              type="button"
              onClick={() => send(o)}
              className="bg-background hover:bg-accent rounded-full border px-3 py-1 text-xs cursor-pointer transition-colors"
            >
              {o}
            </button>
          ))}
        </div>
      )}
      <div className="flex gap-2 pl-6">
        <input
          value={free}
          onChange={(e) => setFree(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault()
              send(free)
            }
          }}
          placeholder={run.questionOptions?.length ? "或输入其他答案…" : "输入你的回答…"}
          className="bg-background border-input focus-visible:ring-ring min-w-0 flex-1 rounded-md border px-2.5 py-1.5 text-sm outline-none focus-visible:ring-2"
          autoFocus
        />
        <Button size="sm" className="h-8 shrink-0 px-3 text-xs" onClick={() => send(free)} disabled={!free.trim()}>
          回答
        </Button>
      </div>
    </div>
  )
}

/** 任务清单面板：agent 的 todo 工具进度，固定在输入框上方，可折叠。
 * 新出现的清单默认收起，避免遮挡消息流；active = 当前轮还在运行；
 * 出错/中断后 in_progress 条目不再转圈，下轮开始才恢复。 */
function TodoPanel({ items, active }: { items: TodoItem[]; active: boolean }) {
  const [open, setOpen] = useState(false)
  const done = items.filter((t) => t.status === "completed").length
  return (
    <div className="bg-card mx-auto mb-2 max-w-3xl rounded-xl border px-3 py-2 shadow-xs">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 text-left text-xs cursor-pointer"
      >
        <ListTodoIcon className="text-muted-foreground size-3.5 shrink-0" />
        <span className="font-medium">任务清单</span>
        <span className="text-muted-foreground tabular-nums">
          {done}/{items.length}
        </span>
        {/* 进度条 */}
        <span className="bg-muted mx-1 h-1 flex-1 overflow-hidden rounded-full">
          <span
            className="bg-primary block h-full rounded-full transition-all"
            style={{ width: `${items.length ? (done / items.length) * 100 : 0}%` }}
          />
        </span>
        {open ? (
          <ChevronDownIcon className="text-muted-foreground size-3.5 shrink-0" />
        ) : (
          <ChevronRightIcon className="text-muted-foreground size-3.5 shrink-0" />
        )}
      </button>
      {open && (
        <div className="mt-1.5 flex flex-col gap-1 pl-1">
          {items.map((t, i) => (
            <div key={i} className="flex items-center gap-2 text-xs">
              {t.status === "completed" ? (
                <CircleCheckIcon className="text-emerald-600 dark:text-emerald-400 size-3.5 shrink-0" />
              ) : t.status === "in_progress" ? (
                <Loader2Icon
                  className={cn("text-muted-foreground size-3.5 shrink-0", active && "animate-spin")}
                />
              ) : (
                <CircleDashedIcon className="text-muted-foreground size-3.5 shrink-0" />
              )}
              <span
                className={cn(
                  "min-w-0 break-words",
                  t.status === "completed" && "text-muted-foreground line-through",
                  t.status === "in_progress" && "font-medium",
                )}
              >
                {t.status === "in_progress" && t.activeForm ? t.activeForm : t.content}
              </span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
