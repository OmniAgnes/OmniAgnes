import { useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
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
  ClockIcon,
  CopyIcon,
  FolderIcon,
  GlobeIcon,
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
import { openInBrowser } from "@/lib/browser"
import { sourceName } from "@/lib/sources"
import { useI18n } from "@/lib/i18n"
import type { ServerUser } from "@/lib/serverAuth"
import { cn } from "@/lib/utils"
import { isDesktop } from "@/lib/platform"
import type { AttachmentInfo, SkillInfo } from "@/types"
import { MediaList } from "./ToolCallView"

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
  /** 本轮工具调用触达过的外部 URL（来源站点），回复末尾「来源：」行展示，点击进内置浏览器 */
  sources?: string[]
  /** 模型思考/推理内容（本轮；不持久化，刷新后消失） */
  thinking?: string
  /** 本轮还在流式输出思考内容 */
  thinkingActive?: boolean
  /** LLM 迭代轮次：一次提问会跑多轮，每轮 = 一段文本 + 若干工具调用 */
  round?: number
  /** 本轮从用户发出到回复完成的工作耗时（毫秒）；仅当轮会话内存，不持久化 */
  elapsedMs?: number
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
  /** 当前正在回复（工作中）的起始时刻；回复生成中，末条回复上方显示动态「工作中 N 秒」 */
  workSince?: number
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
  /** 输入框历史回退/前进（Ctrl+Z 后退 / Ctrl+Y 前进） */
  onInputUndo?: () => void
  onInputRedo?: () => void
  onOpenSidebar: () => void
  /** 内置浏览器面板是否展开（仅桌面端有该入口） */
  browserOpen?: boolean
  onToggleBrowser?: () => void
  /** 登录后台用户：有值时模型选择器顶部显示「内置模型」组 */
  serverUser?: ServerUser | null
  /** 模型选择器底部「配置模型」：打开设置并直达「模型」页签 */
  onConfigureModels?: () => void
}

export function Chat({
  title,
  source,
  mode,
  items,
  streaming,
  thinking,
  retry,
  workSince,
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
  onInputUndo,
  onInputRedo,
  onOpenSidebar,
  browserOpen,
  onToggleBrowser,
  serverUser,
  onConfigureModels,
}: ChatProps) {
  const { t } = useI18n()
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  // 用户手动上滚后不再强行拉回底部，直到重新滚到底
  const stickRef = useRef(true)
  // 是否离开底部：离开后显示「回到底部」悬浮按钮
  const [notBottom, setNotBottom] = useState(false)
  // 「工作中 N 秒」动态跳表：回复生成期间每秒刷新一次
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (workSince == null) return
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [workSince])
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

  // 用户消息导航条（仅大屏）：每道横线对应一条用户提问；悬停时浮出该条内容预览卡
  const [msgPreview, setMsgPreview] = useState<{ mid: number; text: string; top: number; left: number } | null>(null)
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)
  // 预览卡收起定时器：离开导航条/卡片后只留这片刻过桥时间（够鼠标从线上挪到卡片上），
  // 不是奔着卡片去就立刻收，不挂着碍眼
  const PREVIEW_HIDE_MS = 150
  const previewTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(() => () => {
    if (previewTimer.current) clearTimeout(previewTimer.current)
  }, [])
  const userMsgs = useMemo(() => items.filter((it) => it.role === "user" && it.text), [items])

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
    // 发送即归底：哪怕之前手动上滚看过历史，新提问发出后也要跟着最新回复走
    jumpToBottom()
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

  // 提问横幅被临时收起（X）：收起后主输入卡恢复显示，
  // 消息流里的「提问」卡片上有「去回答」可重新打开；来新问题默认展开
  const [qClosed, setQClosed] = useState(false)
  useEffect(() => {
    setQClosed(false)
  }, [pendingQ?.id])
  const showQ = !!pendingQ && !qClosed

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

    return blocks.map((b, bi) => {
      // 本次提问的工作耗时：显示中的那段（该任务仍在本团流式生成）顶部固定显示动态「工作中 N 秒」，
      // 其余已结束的段在其顶部固定显示定格的「已工作 N 秒」。
      // 必须同时满足 streaming——切到已完成/不活动的任务时不会误显示「工作中」。
      const live = streaming && workSince != null && bi === blocks.length - 1
      let elapsedMs: number | undefined
      if (!live) {
        for (let i = b.replies.length - 1; i >= 0; i--) {
          if (b.replies[i].elapsedMs != null) {
            elapsedMs = b.replies[i].elapsedMs
            break
          }
        }
      }
      return (
        <div key={b.user?.id ?? `blk-${bi}`} data-mid={b.user?.id} className="flex flex-col gap-4">
          {b.user && <UserBubble text={b.user.text} attachments={b.user.attachments} onEdit={onEditMessage} />}

          {/* 本次提问的产出：工具折叠组 + 各轮回复。两者同属一组内容，贴紧排（gap-1.5），
              不吃段内的 gap-4 —— 否则工具行和它下面的回复之间会留出很大空白。
              注意：只渲染有文本的回复——中间轮次（纯工具调用）的空 assistant 消息
              若也渲染成 div，每个都会吃一个 gap，多轮时空白越叠越大。 */}
          {(b.tools.length > 0 || b.replies.some((r) => r.text || r.thinking || r.thinkingActive)) && (
            <div className="flex flex-col gap-1.5">
              {/* 耗时标签固定在本段 AI 回复的最上方：下方内容流式更新/追加时它始终在最顶 */}
              {(live || elapsedMs != null) && (
                <ElapsedLabel live={live} liveSince={workSince} now={now} elapsedMs={elapsedMs} />
              )}
              {b.tools.length > 0 && (
                <ToolCallGroup tools={b.tools} onOpenQuestion={() => setQClosed(false)} />
              )}
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
                    {/* 来源站点：本轮工具调用触达过的外部 URL，点击进内置浏览器（网页端开新标签页） */}
                    {item.sources && item.sources.length > 0 && (
                      <div className="text-muted-foreground flex flex-wrap items-center gap-x-1 gap-y-0.5 text-xs">
                        <span>{t("c.sources")}</span>
                        {item.sources.map((u, i) => (
                          <span key={u} className="flex items-center gap-1">
                            {i > 0 && <span className="text-muted-foreground/50">|</span>}
                            <a
                              href={u}
                              title={u}
                              onClick={(e) => {
                                e.preventDefault()
                                openInBrowser(u)
                              }}
                              className="hover:text-foreground cursor-pointer underline-offset-2 hover:underline"
                            >
                              {sourceName(u)}
                            </a>
                          </span>
                        ))}
                      </div>
                    )}
                    {/* 错误浮在输入框上方横幅显示，消息流内不再重复 */}
                  </div>
                ))}
            </div>
          )}
        </div>
      )
    })
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
      {/* 头部：来源标识（文件夹名 / 任务）+ 任务标题。
          桌面端这行挪进了自绘标题栏（窗口按钮左边），此处仅保留移动端菜单按钮；webui 不变。 */}
      <div className="flex items-center gap-2 px-3 py-3 md:px-6">
        <Button variant="ghost" size="icon-sm" className="md:hidden" onClick={onOpenSidebar} title={t("c.menu")}>
          <MenuIcon />
        </Button>
        {!isDesktop && (
          <div className="flex min-w-0 items-center gap-2">
            <span className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs" title={source}>
              {source !== t("a.taskSource") ? <FolderIcon className="size-3.5" /> : <MessageSquareIcon className="size-3.5" />}
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
          <div className="mx-auto flex max-w-3xl lg:max-w-4xl xl:max-w-5xl flex-col gap-4 px-3 py-6 md:px-6">
            {items.length === 0 && emptyHint}
            {renderItems()}

            {/* 底部固定区：处理中动画，永远在消息流最后 */}
            {showThinking && <ProcessingIndicator retry={retry} />}
          </div>
        </div>
        {/* 用户消息导航条（仅 2xl 及以上大屏）：一道横线对应一条用户提问，上下扫过时按距离
            依次变长（Dock 放大效果），悬停浮出该条提问的内容预览卡，点击平滑跳到那一轮对话 */}
        {userMsgs.length > 1 && (
          <div
            className="absolute left-1.5 top-1/2 hidden max-h-[70vh] -translate-y-1/2 flex-col items-start overflow-hidden 2xl:flex"
            onMouseLeave={() => {
              // 离开导航条：鼠标不是奔着预览卡去就立刻收（留 PREVIEW_HIDE_MS 够它过桥上卡）
              setHoverIdx(null)
              if (previewTimer.current) clearTimeout(previewTimer.current)
              previewTimer.current = setTimeout(() => setMsgPreview(null), PREVIEW_HIDE_MS)
            }}
          >
            {userMsgs.map((u, i) => {
              // 只有悬停项上下各 2 道参与变长（0→24、1→20、2→16），更远的保持基准 12 不动
              const dist = hoverIdx == null ? 3 : Math.min(3, Math.abs(i - hoverIdx))
              return (
                // 命中区撑满整条槽位（12px 高 × 36px 宽）：可见横线仍是中间那道 2px 细线，
                // 鼠标沿导航条竖直长度随便放都能悬停/点击，不必精准压在细线上
                <button
                  key={u.id}
                  type="button"
                  onMouseEnter={(e) => {
                    if (previewTimer.current) {
                      clearTimeout(previewTimer.current)
                      previewTimer.current = null
                    }
                    setHoverIdx(i)
                    const r = e.currentTarget.getBoundingClientRect()
                    setMsgPreview({
                      mid: u.id,
                      text: u.text,
                      top: Math.min(Math.max(8, r.top + r.height / 2 - 40), window.innerHeight - 190),
                      // 预览卡贴着实线最长处（24px）右侧 10px，不随透明命中区变宽而远离
                      left: r.left + 34,
                    })
                  }}
                  onClick={() => {
                    scrollRef.current
                      ?.querySelector(`[data-mid="${u.id}"]`)
                      ?.scrollIntoView({ behavior: "smooth", block: "start" })
                  }}
                  className="group/line flex h-3 w-9 shrink-0 cursor-pointer items-center"
                >
                  <span
                    style={{ width: [24, 20, 16, 12][dist] }}
                    className={cn(
                      "h-0.5 rounded-full transition-[width] duration-200 ease-out",
                      dist === 0 ? "bg-muted-foreground/60" : "bg-muted-foreground/25",
                    )}
                  />
                </button>
              )
            })}
          </div>
        )}
        {notBottom && (
          <Button
            variant="ghost"
            size="icon"
            onClick={jumpToBottom}
            title={t("c.backToBottom")}
            className="bg-card text-muted-foreground hover:text-foreground absolute left-1/2 -translate-x-1/2 bottom-3 z-20 rounded-full border shadow-md"
          >
            <ChevronDownIcon />
          </Button>
        )}
      </div>

      {/* 输入区：提问/审批/任务清单/错误浮条固定在输入框上方；左=访问模式，右=模型选择 + 发送。
          提问未答时主输入卡被提问横幅顶替（避免往队列里误发新消息），X 收起后恢复 */}
      <div className="px-3 pb-3 md:px-6 md:pb-4">
        {showQ && pendingQ && (
          <QuestionBanner
            run={pendingQ}
            onAnswer={onAnswer}
            onClose={() => setQClosed(true)}
            streaming={streaming}
            onStop={onStop}
          />
        )}
        {pending && !pendingQ && <ApprovalBanner run={pending} onApprove={onApprove} />}
        {todosOpen && !pendingQ && <TodoPanel items={todos} active={streaming} />}
        {queue.length > 0 && (
          <QueuePanel queue={queue} onDispatch={onQueueDispatch} onEdit={onQueueEdit} onRemove={onQueueRemove} />
        )}
        {lastError && !pending && !pendingQ && (
          <div className="border-destructive/40 bg-destructive/8 text-destructive mx-auto mb-2 flex max-w-3xl lg:max-w-4xl xl:max-w-5xl items-start gap-2 rounded-xl border px-3.5 py-2.5 text-sm">
            <AlertCircleIcon className="mt-0.5 size-4 shrink-0" />
            <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">{lastError}</span>
          </div>
        )}
        {!showQ && (
        <div className="bg-card relative mx-auto max-w-3xl lg:max-w-4xl xl:max-w-5xl rounded-2xl border p-2 shadow-xs">
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
                      {s.source === "project" ? t("c.skillProject") : t("c.skillGlobal")}
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
                    title={t("c.remove")}
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
            placeholder={t("c.inputPh")}
            className="max-h-48 min-h-24 resize-none border-0 bg-transparent px-2 shadow-none focus-visible:ring-0"
            onKeyDown={(e) => {
              // Ctrl+Z 回退到之前发送过的消息、Ctrl(+Shift)+Z / Ctrl+Y 前进
              if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
                e.preventDefault()
                if (e.shiftKey) onInputRedo?.()
                else onInputUndo?.()
                return
              }
              if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "y") {
                e.preventDefault()
                onInputRedo?.()
                return
              }
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
          {/* 工具行：附件/模式/模型放进可横向滚动区，窄屏（H5）滑动查看而不是把模型名挤成省略号；
              发送/中断按钮固定在右侧，不随滚动跑掉，随时可点。 */}
          <div className="flex items-center gap-2 px-1 pt-1">
            <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
              <Button
                variant="ghost"
                size="icon-sm"
                className="text-muted-foreground hover:text-foreground shrink-0"
                onClick={() => fileRef.current?.click()}
                title={t("c.attach")}
              >
                <PaperclipIcon />
              </Button>
              <div className="shrink-0">
                <ModePicker mode={mode} disabled={streaming} onChange={onModeChange} />
              </div>
              {/* 内置浏览器：地球图标紧跟访问模式，展开态高亮（仅桌面端） */}
              {isDesktop && onToggleBrowser && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={onToggleBrowser}
                  title={browserOpen ? t("c.browserHide") : t("c.browserOpen")}
                  className={cn("shrink-0", browserOpen ? "text-foreground" : "text-muted-foreground hover:text-foreground")}
                >
                  <GlobeIcon />
                </Button>
              )}
              {(models.length > 0 || !!serverUser) && (
                <div className="shrink-0">
                  <ModelPicker groups={models} model={model} onChange={onModelChange} serverUser={serverUser} onConfigure={onConfigureModels} />
                </div>
              )}
            </div>

            <div className="flex shrink-0 items-center gap-1.5">
              {streaming ? (
                <Button variant="secondary" size="icon" className="rounded-full" onClick={onStop} title={t("c.stop")}>
                  {/* 实心方块：更像"停止"语义（lucide 默认是描边空心） */}
                  <SquareIcon fill="currentColor" />
                </Button>
              ) : (
                <Button size="icon" className="rounded-full" onClick={submit} title={t("c.send")}>
                  <SendHorizontalIcon />
                </Button>
              )}
            </div>
          </div>
        </div>
        )}
      </div>

      {/* 用户提问预览卡：导航条悬停时浮出（portal 到 body 避开滚动容器裁切）。
          卡片可悬停可点击：移上去取消收起计时，点卡片同样跳到那一轮对话 */}
      {msgPreview && createPortal(
        <div
          onMouseEnter={() => {
            if (previewTimer.current) clearTimeout(previewTimer.current)
            previewTimer.current = null
          }}
          onMouseLeave={() => {
            // 离开卡片：不是回导航条就立刻收（同样只留过桥时间）
            if (previewTimer.current) clearTimeout(previewTimer.current)
            previewTimer.current = setTimeout(() => setMsgPreview(null), PREVIEW_HIDE_MS)
          }}
          onClick={() => {
            scrollRef.current
              ?.querySelector(`[data-mid="${msgPreview.mid}"]`)
              ?.scrollIntoView({ behavior: "smooth", block: "start" })
            setMsgPreview(null)
          }}
          className="bg-popover text-popover-foreground fixed z-50 hidden w-80 cursor-pointer rounded-md border p-3 text-xs leading-relaxed break-words shadow-md md:block"
          style={{ top: msgPreview.top, left: msgPreview.left }}
        >
          <div className="line-clamp-6">{msgPreview.text}</div>
        </div>,
        document.body
      )}
    </div>
  )
}

/** 格式化工作耗时：<1 分钟只显示秒，>=1 分钟按「分+秒」，>=1 小时按「小时+分+秒」。 */
function fmtElapsed(ms: number, t: (k: string, v?: Record<string, string | number>) => string): string {
  const s = Math.max(1, Math.round(ms / 1000))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return t("d.hms", { h, m, s: sec })
  if (m > 0) return t("d.ms", { m, s: sec })
  return t("d.s", { s: sec })
}

/** 回复上方的耗时标签：live 时为生成中的动态「工作中 N 秒」（每秒跳变），否则显示定格的「已工作 N 秒」。 */
function ElapsedLabel({
  live,
  liveSince,
  now,
  elapsedMs,
}: {
  live: boolean
  liveSince?: number
  now: number
  elapsedMs?: number
}) {
  const { t } = useI18n()
  if (live && liveSince != null) {
    return (
      <div className="text-muted-foreground flex items-center gap-1 pt-1 text-xs">
        <ClockIcon className="size-3.5 shrink-0" />
        <span>{t("c.working", { d: fmtElapsed(Math.max(0, now - liveSince), t) })}</span>
      </div>
    )
  }
  if (elapsedMs != null) {
    return (
      <div className="text-muted-foreground flex items-center gap-1 pt-1 text-xs">
        <ClockIcon className="size-3.5 shrink-0" />
        <span>{t("c.worked", { d: fmtElapsed(elapsedMs, t) })}</span>
      </div>
    )
  }
  return null
}

/** 处理中动画：固定在消息流最末尾，跟着工具调用组下方。
 * 上游自动重试中显示「重试 n/10」（重试结束、模型恢复输出后自动回到「处理中…」）。 */
function ProcessingIndicator({ retry }: { retry?: { attempt: number; max: number } }) {
  const { t } = useI18n()
  return (
    <div className="text-muted-foreground flex items-center gap-2 py-1 pr-2 text-sm">
      <Loader2Icon className="size-3.5 shrink-0 animate-spin" />
      <span>{retry ? t("a.retry", { n: retry.attempt, max: retry.max }) : t("c.processing")}</span>
    </div>
  )
}

/** 思考折叠行：模型推理内容收在这里（同工具调用组，默认收起）。
 * 推理进行中显示「思考中…」，结束后固定为「思考」。 */
function ThinkingBlock({ text, active }: { text?: string; active?: boolean }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className="w-full">
      <CollapsibleTrigger className="group text-muted-foreground hover:bg-muted/50 flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-xs transition-colors cursor-pointer">
        <BrainIcon className="size-3.5 shrink-0" />
        <span className={cn(active && "opacity-80")}>{active ? t("c.thinking") : t("c.thought")}</span>
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
  const { t } = useI18n()
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
        title={t("c.editResend")}
      >
        <PencilLineIcon />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        className="bg-background text-muted-foreground hover:text-foreground pointer-events-auto relative shadow-sm"
        onClick={copy}
        title={t("c.copy")}
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
              {open ? t("c.collapse") : t("c.expand")}
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
  const { t } = useI18n()
  // 队列可点击头部展开/收起（默认展开）
  const [collapsed, setCollapsed] = useState(false)
  return (
    <div className="mx-auto mb-2 flex max-w-3xl lg:max-w-4xl xl:max-w-5xl flex-col gap-1">
      <button
        type="button"
        onClick={() => setCollapsed((v) => !v)}
        className="text-muted-foreground hover:text-foreground flex cursor-pointer items-center gap-1 self-start px-1 text-[11px]"
      >
        {t("c.queue", { n: queue.length })}
        <ChevronDownIcon
          className={cn("size-3 transition-transform", collapsed && "-rotate-90")}
        />
      </button>
      {!collapsed &&
      queue.map((q) => (
        <div
          key={q.id}
          className="group flex items-center gap-2 rounded-md px-2 py-1"
        >
          <ListChecksIcon className="text-muted-foreground size-3.5 shrink-0" />
          <span
            className="text-muted-foreground min-w-0 flex-1 truncate text-xs"
            title={q.text || undefined}
          >
            {q.text || (
              <span className="flex items-center gap-1">
                <PaperclipIcon className="size-3" />
                {t("c.attachments", { n: q.files?.length ?? 0 })}
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
              title={t("c.sendNow")}
            >
              <SendHorizontalIcon className="size-3" />
              {t("c.send")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="h-6 gap-1 px-1.5 text-[11px]"
              onClick={() => onEdit(q.id)}
              title={t("c.editHint")}
            >
              <PencilLineIcon className="size-3" />
              {t("c.edit")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              className="text-muted-foreground hover:text-destructive h-6 gap-1 px-1.5 text-[11px]"
              onClick={() => onRemove(q.id)}
              title={t("c.delete")}
            >
              <Trash2Icon className="size-3" />
              {t("c.delete")}
            </Button>
          </div>
        </div>
      ))}
    </div>
  )
}

/** 访问模式下拉：与模型选择同款交互（向上弹出、点外/Esc 关闭）。完全访问排下面。 */
const MODES = [
  { id: "plan", key: "c.modePlan", descKey: "c.modePlanDesc" },
  { id: "build", key: "c.modeBuild", descKey: "c.modeBuildDesc" },
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
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  // 打开瞬间记录触发按钮位置：面板 portal 到 body 用 fixed 定位，
  // 不被工具行 overflow-x-auto 滚动容器裁切（面板伸出行外会被整个裁掉，点了看不见）。
  const [anchor, setAnchor] = useState<{ left: number; top: number } | null>(null)
  const wrapRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const tgt = e.target as Node
      if (wrapRef.current?.contains(tgt) || panelRef.current?.contains(tgt)) return
      setOpen(false)
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
        title={t("c.switchMode")}
        disabled={disabled}
        onClick={() => {
          if (open) {
            setOpen(false)
            return
          }
          const r = wrapRef.current?.getBoundingClientRect()
          if (!r) return
          setAnchor({ left: r.left, top: r.top })
          setOpen(true)
        }}
        className="bg-muted/60 hover:bg-muted flex cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors disabled:opacity-50"
      >
        <span className="font-medium">{t(cur.key)}</span>
        <ChevronUpIcon className={cn("size-3.5 shrink-0 opacity-60 transition-transform", open && "rotate-180")} />
      </button>
      {open && anchor && createPortal(
        <div
          ref={panelRef}
          style={{
            position: "fixed",
            zIndex: 50,
            left: anchor.left,
            bottom: window.innerHeight - anchor.top + 6,
          }}
          className="bg-popover text-popover-foreground w-52 overflow-hidden rounded-md border p-1 shadow-md"
        >
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
                <span className={cn("text-xs", m.id === mode ? "font-medium" : "")}>{t(m.key)}</span>
                <span className="text-muted-foreground text-[11px]">{t(m.descKey)}</span>
              </span>
            </button>
          ))}
        </div>,
        document.body
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
  const { t } = useI18n()
  const { label, summary } = toolApprovalLabel(run, t)
  return (
    <div className="border-amber-500/50 bg-amber-500/10 mx-auto mb-2 flex max-w-3xl lg:max-w-4xl xl:max-w-5xl flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border px-3.5 py-2.5">
      <CircleAlertIcon className="text-amber-600 dark:text-amber-400 size-4 shrink-0" />
      <div className="min-w-0 flex-1 text-sm">
        <span className="text-amber-700 dark:text-amber-400 font-medium">{t("c.confirmNeed")}</span>
        <span className="font-medium">{label}</span>
        {summary && (
          <span className="text-muted-foreground ml-1.5 font-mono text-xs break-all">{summary}</span>
        )}
        <div className="text-muted-foreground mt-0.5 text-xs">{t("c.confirmHint")}</div>
      </div>
      <div className="flex shrink-0 gap-2">
        <Button
          size="sm"
          variant="outline"
          className="h-7 px-3 text-xs"
          onClick={() => onApprove(run.approvalId!, false)}
        >
          {t("c.deny")}
        </Button>
        <Button size="sm" className="h-7 px-3 text-xs" onClick={() => onApprove(run.approvalId!, true)}>
          {t("c.allow")}
        </Button>
      </div>
    </div>
  )
}

/** 提问横幅：ask 工具向用户要答案，候选项点击即答，也可自由输入。
 * 样式跟随主题卡片（与主输入框同款），提问未答时顶替主输入框（避免误发进队列）；
 * 右上角 X 可收起，收起后点消息流里「提问」卡片的「去回答」重新打开。 */
function QuestionBanner({
  run,
  onAnswer,
  onClose,
  streaming,
  onStop,
}: {
  run: ToolRun
  onAnswer: (id: string, answer: string) => void
  onClose: () => void
  streaming: boolean
  onStop: () => void
}) {
  const { t } = useI18n()
  const [free, setFree] = useState("")
  const send = (text: string) => {
    const v = text.trim()
    if (v) onAnswer(run.questionId!, v)
  }
  return (
    <div className="bg-card relative mx-auto mb-2 max-w-3xl lg:max-w-4xl xl:max-w-5xl rounded-2xl border p-2 shadow-xs">
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-muted-foreground hover:text-foreground absolute top-1 right-1"
        onClick={onClose}
        title={t("c.questionHint")}
      >
        <XIcon className="size-4" />
      </Button>
      <div className="px-1 pt-0.5 text-sm">
        <span className="font-medium">{t("c.aiAsk")}</span>
        <span className="whitespace-pre-wrap break-words">{run.question}</span>
      </div>
      {run.questionOptions && run.questionOptions.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1.5 px-1">
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
      <div className="mt-1.5 flex items-center gap-2 px-1 pb-0.5">
        <input
          value={free}
          onChange={(e) => setFree(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.nativeEvent.isComposing) {
              e.preventDefault()
              send(free)
            }
          }}
          placeholder={run.questionOptions?.length ? t("c.answerPh2") : t("c.answerPh")}
          className="text-muted-foreground min-w-0 flex-1 rounded-md bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground/70"
          autoFocus
        />
        {streaming && (
          <Button
            variant="secondary"
            size="icon"
            className="shrink-0 rounded-full"
            onClick={onStop}
            title={t("c.stop")}
          >
            <SquareIcon fill="currentColor" />
          </Button>
        )}
        <Button size="sm" className="h-8 shrink-0 px-3 text-xs" onClick={() => send(free)} disabled={!free.trim()}>
          {t("c.answer")}
        </Button>
      </div>
    </div>
  )
}

/** 任务清单面板：agent 的 todo 工具进度，固定在输入框上方，可折叠。
 * 新出现的清单默认收起，避免遮挡消息流；active = 当前轮还在运行；
 * 出错/中断后 in_progress 条目不再转圈，下轮开始才恢复。 */
function TodoPanel({ items, active }: { items: TodoItem[]; active: boolean }) {
  const { t } = useI18n()
  const [open, setOpen] = useState(false)
  const done = items.filter((tt) => tt.status === "completed").length
  return (
    <div className="bg-card mx-auto mb-2 max-w-3xl lg:max-w-4xl xl:max-w-5xl rounded-xl border px-3 py-2 shadow-xs">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center gap-2 text-left text-xs cursor-pointer"
      >
        <ListTodoIcon className="text-muted-foreground size-3.5 shrink-0" />
        <span className="font-medium">{t("c.todo")}</span>
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
