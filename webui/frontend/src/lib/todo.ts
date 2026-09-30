/** agent todo 工具的清单项：面板（Chat）与工具卡片详情共用 */
export interface TodoItem {
  content: string
  activeForm?: string
  status: "pending" | "in_progress" | "completed"
}

const STATUS = new Set(["pending", "in_progress", "completed"])

/** 从 todo 调用参数（已解析的 args 记录）提取清单 */
export function todosFromArgs(args: Record<string, unknown>): TodoItem[] {
  const raw = args.items
  if (!Array.isArray(raw)) return []
  const out: TodoItem[] = []
  for (const it of raw) {
    const m = it as Record<string, unknown> | null
    if (!m || typeof m.content !== "string" || !m.content) continue
    const st = typeof m.status === "string" && STATUS.has(m.status) ? (m.status as TodoItem["status"]) : "pending"
    out.push({
      content: m.content,
      activeForm: typeof m.active_form === "string" ? m.active_form : undefined,
      status: st,
    })
  }
  return out
}

/** 从工具卡片存的原始 JSON 字符串提取清单；非 todo 参数返回 [] */
export function todosFromJson(raw: string): TodoItem[] {
  try {
    const v = JSON.parse(raw)
    if (v && typeof v === "object") return todosFromArgs(v as Record<string, unknown>)
  } catch {
    /* ignore */
  }
  return []
}
