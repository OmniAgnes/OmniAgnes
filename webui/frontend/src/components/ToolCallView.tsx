import {
  ChevronRightIcon,
  CircleCheckIcon,
  CircleSlashIcon,
  CircleXIcon,
  FilePenIcon,
  FileTextIcon,
  FolderSearchIcon,
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
import { Button } from "@/components/ui/button"
import { diffLines, diffStat } from "@/lib/diff"
import { cn } from "@/lib/utils"

/** 工具调用的展示元数据：图标 + 人类可读名称。 */
interface ToolMeta {
  icon: React.ComponentType<{ className?: string }>
  label: string
  /** 从参数里提取一句话摘要 */
  summary: (args: Record<string, unknown>) => string
}

const str = (v: unknown): string => (typeof v === "string" ? v : v == null ? "" : String(v))

function fileName(p: string): string {
  if (!p) return ""
  const parts = p.split(/[\\/]/)
  return parts[parts.length - 1] || p
}

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
  grep: {
    icon: SearchIcon,
    label: "搜索",
    summary: (a) => str(a.pattern),
  },
  glob: {
    icon: FolderSearchIcon,
    label: "查找",
    summary: (a) => str(a.pattern),
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
}

function parseArgs(raw: string): Record<string, unknown> {
  try {
    const v = JSON.parse(raw)
    return v && typeof v === "object" ? (v as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

/** 工具调用卡片：图标 + 名称 + 摘要，点击展开详情。 */
export function ToolCallView({
  run,
  onApprove,
}: {
  run: ToolRun
  onApprove?: (id: string, allow: boolean) => void
}) {
  const meta = metaOf(run.name)
  const Icon = meta.icon
  const args = parseArgs(run.args)
  const summary = meta.summary(args)
  const waiting = run.approvalId != null && run.approvalChoice == null

  return (
    <Collapsible
      className={cn(
        "overflow-hidden rounded-lg",
        waiting && "bg-amber-500/5 ring-1 ring-amber-500/50",
      )}
    >
      <CollapsibleTrigger className="group hover:bg-muted/50 flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors cursor-pointer">
        <Icon className="text-muted-foreground size-3.5 shrink-0" />
        <span className="font-medium">{meta.label}</span>
        {summary && (
          <span className="text-muted-foreground min-w-0 truncate font-mono text-[11px]">
            {run.name === "read" || run.name === "edit" || run.name === "write" ? fileName(summary) : summary}
          </span>
        )}
        {/* 状态图标靠右，展开箭头在最右 */}
        {run.running ? (
          <Loader2Icon className="text-muted-foreground ml-auto size-3.5 shrink-0 animate-spin" />
        ) : run.denied ? (
          <CircleSlashIcon className="text-muted-foreground ml-auto size-3.5 shrink-0" />
        ) : run.isError ? (
          <CircleXIcon className="text-destructive ml-auto size-3.5 shrink-0" />
        ) : (
          <CircleCheckIcon className="text-muted-foreground ml-auto size-3.5 shrink-0" />
        )}
        <ChevronRightIcon className="text-muted-foreground size-3.5 shrink-0 transition-transform group-data-[state=open]:rotate-90" />
      </CollapsibleTrigger>

      {/* 普通访问模式：确认条 */}
      {run.approvalId != null &&
        (waiting ? (
          <div className="flex items-center gap-2 px-2.5 pb-1.5">
            <span className="text-amber-700 dark:text-amber-400 text-xs">需要你的确认</span>
            <div className="ml-auto flex gap-1.5">
              <Button
                size="sm"
                variant="outline"
                className="h-7 px-2.5 text-xs"
                onClick={() => onApprove?.(run.approvalId!, false)}
              >
                拒绝
              </Button>
              <Button
                size="sm"
                className="h-7 px-2.5 text-xs"
                onClick={() => onApprove?.(run.approvalId!, true)}
              >
                允许
              </Button>
            </div>
          </div>
        ) : (
          <div className="text-muted-foreground px-2.5 pb-1.5 text-[11px]">
            {run.approvalChoice ? "你允许了该操作" : "你拒绝了这个操作"}
          </div>
        ))}

      <CollapsibleContent>
        <ToolDetail name={run.name} args={args} output={run.output} isError={run.isError} denied={run.denied} />
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
}: {
  name: string
  args: Record<string, unknown>
  output: string
  isError: boolean
  denied?: boolean
}) {
  return (
    <div className="bg-muted/40 mx-1.5 mb-1.5 flex flex-col gap-2 rounded-md p-2 text-xs">
      {name === "edit" && <EditDiff args={args} />}

      {name === "write" && str(args.content) !== "" && (
        <Field label={`内容（${countLines(str(args.content))} 行）`}>
          <CodeBlock code={str(args.content)} />
        </Field>
      )}

      {name === "bash" && str(args.command) !== "" && (
        <Field label="命令">
          <CodeBlock code={str(args.command)} tone="command" />
        </Field>
      )}

      {name === "read" && str(args.path) !== "" && (
        <Field label="文件">
          <div className="text-muted-foreground font-mono break-all">{str(args.path)}</div>
        </Field>
      )}

      {(name === "grep" || name === "glob") && <SearchArgs args={args} />}

      {denied ? (
        <div className="text-muted-foreground">用户拒绝了该操作，未执行。</div>
      ) : output ? (
        <Field label={isError ? "错误" : "输出"}>
          <CodeBlock code={output} tone={isError ? "error" : "output"} />
        </Field>
      ) : null}

      {/* 未识别的工具/MCP 工具：按键值展示，不给用户原始 JSON */}
      {!META[name] && <GenericArgs args={args} />}
    </div>
  )
}

function EditDiff({ args }: { args: Record<string, unknown> }) {
  const oldStr = str(args.old_string)
  const newStr = str(args.new_string)
  if (oldStr === "" && newStr === "") return null
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
}: {
  code: string
  tone?: "command" | "output" | "error"
}) {
  return (
    <pre
      className={cn(
        "max-h-96 overflow-auto rounded-md border p-2 font-mono text-[11px] leading-relaxed whitespace-pre-wrap",
        tone === "error" && "border-destructive/40 bg-destructive/8 text-destructive",
        tone === "command" && "bg-muted/60",
        tone === "output" && "bg-muted/40",
      )}
    >
      {code}
    </pre>
  )
}

function countLines(s: string): number {
  if (s === "") return 0
  return s.replace(/\r\n/g, "\n").split("\n").length
}
