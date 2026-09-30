import { useMemo } from "react"

import {
  BracesIcon,
  ChevronRightIcon,
  CircleCheckIcon,
  CircleDashedIcon,
  CircleSlashIcon,
  FilePenIcon,
  FileTextIcon,
  FolderSearchIcon,
  HelpCircleIcon,
  ListTodoIcon,
  Loader2Icon,
  PencilLineIcon,
  SearchIcon,
  TerminalIcon,
  WrenchIcon,
} from "lucide-react"

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible"
import { diffLines, diffStat } from "@/lib/diff"
import { highlightToHtml, langFromPath } from "@/lib/highlight"
import { todosFromArgs } from "@/lib/todo"
import { cn } from "@/lib/utils"

/** 工具调用的展示元数据：图标 + 人类可读名称。 */
interface ToolMeta {
  icon: React.ComponentType<{ className?: string }>
  label: string
  /** 从参数里提取一句话摘要 */
  summary: (args: Record<string, unknown>) => string
}

const str = (v: unknown): string => (typeof v === "string" ? v : v == null ? "" : String(v))

const META: Record<string, ToolMeta> = {
  read: {
    icon: FileTextIcon,
    label: "读取",
    summary: (a) => str(a.path),
  },
  write: {
    icon: FilePenIcon,
    label: "写入",
    summary: (a) => str(a.path),
  },
  edit: {
    icon: PencilLineIcon,
    label: "编辑",
    summary: (a) => str(a.path),
  },
  bash: {
    icon: TerminalIcon,
    label: "bash",
    summary: (a) => str(a.command),
  },
  todo: {
    icon: ListTodoIcon,
    label: "任务清单",
    // 折叠行不显示摘要（进度以输入框上方面板呈现）
    summary: () => "",
  },
  ask: {
    icon: HelpCircleIcon,
    label: "提问",
    summary: (a) => str(a.question),
  },
  grep: {
    icon: SearchIcon,
    label: "搜索",
    // 查找类工具不在折叠行显示 pattern 摘要（正则原文对阅读没帮助）
    summary: () => "",
  },
  glob: {
    icon: FolderSearchIcon,
    label: "查找",
    summary: () => "",
  },
  json: {
    icon: BracesIcon,
    label: "JSON",
    summary: (a) => (a.pointer ? `${str(a.op) || "query"} ${str(a.pointer)}` : str(a.path)),
  },
}

function metaOf(name: string): ToolMeta {
  return (
    META[name] ?? {
      icon: WrenchIcon,
      label: name,
      summary: () => "",
    }
  )
}

export interface ToolRun {
  id: string
  name: string
  /** 原始 JSON 字符串 */
  args: string
  output: string
  isError: boolean
  running: boolean
  /** 用户拒绝后标记 */
  denied?: boolean
  /** 普通访问模式下的确认请求 ID（等待用户选择时存在） */
  approvalId?: string
  /** 用户已作出的选择（null 表示仍未选择） */
  approvalChoice?: boolean | null
  /** ask 工具的提问请求 ID */
  questionId?: string
  /** 提问内容（有值表示这是一次待答/已答提问） */
  question?: string
  /** 提问候选项 */
  questionOptions?: string[]
  /** 用户已回答的内容（undefined 表示仍未回答） */
  questionAnswer?: string
  /** write 覆盖已有文件时后端在执行前抓的旧内容快照（渲染 old→new 差异用） */
  baseOld?: string
}

function parseArgs(raw: string): Record<string, unknown> {
  try {
    const v = JSON.parse(raw)
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

/** 供输入框上方的审批浮条复用：把一次工具调用压成「标签 + 摘要（文件类带完整路径）」。 */
export function toolApprovalLabel(run: ToolRun): { label: string; summary: string } {
  const meta = metaOf(run.name)
  const args = parseArgs(run.args)
  return { label: meta.label, summary: meta.summary(args) }
}

/** 工具调用卡片：图标 + 名称 + 摘要，点击展开详情。待确认浮条在输入框上方（见 Chat）。 */
export function ToolCallView({ run }: { run: ToolRun }) {
  const meta = metaOf(run.name)
  const Icon = meta.icon
  const args = parseArgs(run.args)
  const summary = meta.summary(args)

  return (
    <Collapsible className="overflow-hidden rounded-lg">
      <CollapsibleTrigger className="group hover:bg-muted/50 flex w-full items-center gap-2 rounded-lg px-2 py-1 text-left text-xs transition-colors cursor-pointer">
        <Icon className="text-muted-foreground size-3.5 shrink-0" />
        <span className="font-medium">{meta.label}</span>
        {summary && (
          <span className="text-muted-foreground min-w-0 truncate font-mono text-[11px]" title={summary}>
            {summary}
          </span>
        )}
        {/* 状态图标靠右，展开箭头在最右。待确认由输入框上方浮条提示，这里只显示转圈。
            出错不标红叉（用户要求），错误详情看展开内容。 */}
        {run.running ? (
          <Loader2Icon className="text-muted-foreground ml-auto size-3.5 shrink-0 animate-spin" />
        ) : run.denied ? (
          <CircleSlashIcon className="text-muted-foreground ml-auto size-3.5 shrink-0" />
        ) : (
          <CircleCheckIcon className="text-muted-foreground ml-auto size-3.5 shrink-0" />
        )}
        <ChevronRightIcon className="text-muted-foreground size-3.5 shrink-0 transition-transform group-data-[state=open]:rotate-90" />
      </CollapsibleTrigger>

      {/* 已作出选择的留下记录；待确认的按钮在输入框上方浮条中 */}
      {run.approvalId != null && run.approvalChoice != null && (
        <div className="text-muted-foreground px-2.5 pb-1.5 text-[11px]">
          {run.approvalChoice ? "你允许了该操作" : "你拒绝了这个操作"}
        </div>
      )}

      <CollapsibleContent>
        <ToolDetail
          name={run.name}
          args={args}
          output={run.output}
          isError={run.isError}
          denied={run.denied}
          baseOld={run.baseOld}
        />
      </CollapsibleContent>
    </Collapsible>
  )
}

function ToolDetail({
  name,
  args,
  output,
  isError,
  denied,
  baseOld,
}: {
  name: string
  args: Record<string, unknown>
  output: string
  isError: boolean
  denied?: boolean
  /** write 覆盖已有文件时的旧内容（由后端在执行前抓取），有值则渲染差异 */
  baseOld?: string
}) {
  const path = str(args.path)
  return (
    // ml-3：展开详情相对折叠行明显左缩进，层级一眼可辨
    <div className="bg-muted/40 ml-3 mb-1 mr-1.5 flex flex-col gap-2 rounded-md p-2 text-xs">
      {name === "edit" && <EditDiff args={args} />}

      {name === "ask" && str(args.question) !== "" && (
        <div className="flex flex-col gap-1.5">
          <Field label="问题">
            <div className="break-words">{str(args.question)}</div>
          </Field>
          {Array.isArray(args.options) && (args.options as unknown[]).length > 0 && (
            <div className="text-muted-foreground flex flex-wrap gap-1.5">
              {(args.options as unknown[]).map((o, i) => (
                <span key={i} className="bg-background rounded-full border px-2 py-0.5 text-[11px]">
                  {str(o)}
                </span>
              ))}
            </div>
          )}
          {output && (
            <Field label="用户回答">
              <div className="break-words">{output}</div>
            </Field>
          )}
        </div>
      )}

      {name === "todo" && <TodoDetail args={args} />}

      {name === "write" && str(args.content) !== "" && (
        baseOld != null ? (
          // 覆盖已有文件：写入前旧内容 vs 新内容 的行级差异
          <DiffLines oldStr={baseOld} newStr={str(args.content)} />
        ) : (
          <Field label={`内容（${countLines(str(args.content))} 行）`}>
            <CodeBlock code={str(args.content)} path={path} />
          </Field>
        )
      )}

      {name === "bash" && str(args.command) !== "" && (
        <Field label="命令">
          <CodeBlock code={str(args.command)} tone="command" lang="bash" />
        </Field>
      )}

      {/* read：直接显示文件内容（代码插件高亮），不再显示"文件: 路径"行 */}
      {name === "read" && output && !denied ? (
        <CodeBlock code={output} path={path} tone={isError ? "error" : "code"} />
      ) : null}

      {(name === "grep" || name === "glob") && !output && <SearchArgs args={args} />}

      {denied ? (
        <div className="text-muted-foreground">用户拒绝了该操作，未执行。</div>
      ) : output && name !== "read" ? (
        <Field label={isError ? "错误" : "输出"}>
          <CodeBlock code={output} tone={isError ? "error" : "output"} />
        </Field>
      ) : null}

      {/* 未识别的工具/MCP 工具：按键值展示，不给用户原始 JSON */}
      {!META[name] && <GenericArgs args={args} />}
    </div>
  )
}

/** todo 工具展开详情：静态清单（进度面板在输入框上方另有展示） */
function TodoDetail({ args }: { args: Record<string, unknown> }) {
  const items = todosFromArgs(args)
  if (items.length === 0) return null
  return (
    <div className="flex flex-col gap-1">
      {items.map((t, i) => (
        <div key={i} className="flex items-center gap-2">
          {t.status === "completed" ? (
            <CircleCheckIcon className="text-emerald-600 dark:text-emerald-400 size-3.5 shrink-0" />
          ) : t.status === "in_progress" ? (
            <Loader2Icon className="text-muted-foreground size-3.5 shrink-0 animate-spin" />
          ) : (
            <CircleDashedIcon className="text-muted-foreground size-3.5 shrink-0" />
          )}
          <span className={cn("min-w-0 break-words", t.status === "completed" && "text-muted-foreground line-through")}>
            {t.status === "in_progress" && t.activeForm ? t.activeForm : t.content}
          </span>
        </div>
      ))}
    </div>
  )
}

function EditDiff({ args }: { args: Record<string, unknown> }) {
  const oldStr = str(args.old_string)
  const newStr = str(args.new_string)
  if (oldStr === "" && newStr === "") return null
  return <DiffLines oldStr={oldStr} newStr={newStr} />
}

/** 行级差异渲染（edit 与 write 覆盖共用） */
function DiffLines({ oldStr, newStr }: { oldStr: string; newStr: string }) {
  const lines = diffLines(oldStr, newStr)
  const { added, removed } = diffStat(lines)
  return (
    <Field
      label={
        <span className="flex items-center gap-2">
          差异
          <span className="font-mono text-[11px]">
            <span className="text-emerald-600 dark:text-emerald-400">+{added}</span>
            <span className="text-muted-foreground mx-1">/</span>
            <span className="text-rose-600 dark:text-rose-400">-{removed}</span>
          </span>
        </span>
      }
    >
      <div className="overflow-x-auto rounded-md border">
        {lines.map((l, i) => (
          <div
            key={i}
            className={cn(
              "flex font-mono text-[11px] leading-relaxed",
              l.op === "add" && "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
              l.op === "del" && "bg-rose-500/12 text-rose-700 dark:text-rose-300",
              l.op === "keep" && "text-muted-foreground",
            )}
          >
            <span className="text-muted-foreground/70 w-8 shrink-0 select-none pr-1 text-right">
              {l.op === "add" ? l.newNo : l.oldNo}
            </span>
            <span className="w-4 shrink-0 select-none text-center">
              {l.op === "add" ? "+" : l.op === "del" ? "-" : " "}
            </span>
            <span className="min-w-0 flex-1 pr-2 whitespace-pre-wrap">{l.text || " "}</span>
          </div>
        ))}
      </div>
    </Field>
  )
}

function SearchArgs({ args }: { args: Record<string, unknown> }) {
  return (
    <div className="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 font-mono text-[11px]">
      <span>pattern: {str(args.pattern) || "-"}</span>
      {str(args.path) !== "" && <span>path: {str(args.path)}</span>}
      {str(args.include) !== "" && <span>include: {str(args.include)}</span>}
    </div>
  )
}

const HIDDEN_KEYS = new Set(["path", "content", "command", "old_string", "new_string", "pattern"])

function GenericArgs({ args }: { args: Record<string, unknown> }) {
  const entries = Object.entries(args).filter(([k, v]) => !HIDDEN_KEYS.has(k) && v != null && v !== "")
  if (entries.length === 0) return null
  return (
    <div className="text-muted-foreground flex flex-col gap-1 font-mono text-[11px]">
      {entries.map(([k, v]) => (
        <div key={k} className="flex gap-2">
          <span className="shrink-0">{k}:</span>
          <span className="min-w-0 break-all">{typeof v === "object" ? JSON.stringify(v) : String(v)}</span>
        </div>
      ))}
    </div>
  )
}

function Field({ label, children }: { label: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <div className="text-muted-foreground text-[11px] font-medium">{label}</div>
      {children}
    </div>
  )
}

function CodeBlock({
  code,
  tone = "output",
  path,
  lang,
}: {
  code: string
  tone?: "command" | "output" | "error" | "code"
  /** 文件路径：按扩展名推断高亮语言 */
  path?: string
  /** 显式指定语言（优先于 path 推断） */
  lang?: string
}) {
  const html = useMemo(
    () => highlightToHtml(code, lang || (path ? langFromPath(path) : "")),
    [code, lang, path],
  )
  return (
    <pre
      className={cn(
        "max-h-96 overflow-auto rounded-md border p-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap",
        tone === "error" && "border-destructive/40 bg-destructive/8 text-destructive",
        tone === "command" && "bg-muted/60",
        tone === "output" && "bg-muted/40",
        // 代码块：GitHub 风底（浅色 #f6f8fa / 深色 #0d1117，见 index.css）
        tone === "code" && "gh-code",
      )}
    >
      {html ? <code className="hljs" dangerouslySetInnerHTML={{ __html: html }} /> : code}
    </pre>
  )
}

function countLines(s: string): number {
  if (s === "") return 0
  return s.replace(/\r\n/g, "\n").split("\n").length
}
