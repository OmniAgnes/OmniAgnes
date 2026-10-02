import type {
  BrowseResult,
  ChatEvent,
  MCPStatus,
  MCPServerConfig,
  Project,
  ProviderConfig,
  ScheduledTaskInfo,
  SkillDetail,
  SkillDirs,
  SkillInfo,
  StateInfo,
  UsageRecord,
  TaskFull,
  TaskMeta,
  Theme,
  ThemeColor,
} from "@/types"

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      /* 忽略解析失败 */
    }
    throw new Error(msg)
  }
  return res.json() as Promise<T>
}

export const api = {
  state: () => fetch("/api/state").then((r) => json<StateInfo>(r)),

  models: () =>
    fetch("/api/models").then((r) =>
      json<{
        models: { model: string; type: string; provider?: string; default: boolean }[]
        current: string
      }>(r),
    ),

  providers: () => fetch("/api/providers").then((r) => json<{ providers: ProviderConfig[] }>(r)),

  saveProviders: (providers: ProviderConfig[]) =>
    fetch("/api/providers", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ providers }),
    }).then((r) => json<{ providers: ProviderConfig[]; current: string }>(r)),

  projects: () => fetch("/api/projects").then((r) => json<{ projects: Project[] }>(r)),

  addProject: (path: string) =>
    fetch("/api/projects", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path }),
    }).then((r) => json<{ project: Project }>(r)),

  deleteProject: (id: string) => fetch(`/api/projects/${id}`, { method: "DELETE" }),

  openProject: (id: string) =>
    fetch(`/api/projects/${id}/open`, { method: "POST" }).then((r) => json<{ ok: boolean }>(r)),

  browse: (path: string) =>
    fetch(`/api/browse?path=${encodeURIComponent(path)}`).then((r) => json<BrowseResult>(r)),

  tasks: (project: string) =>
    fetch(`/api/tasks?project=${encodeURIComponent(project)}`).then((r) =>
      json<{ tasks: TaskMeta[] }>(r),
    ),

  createTask: (projectId?: string) =>
    fetch("/api/tasks", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ project_id: projectId ?? "" }),
    }).then((r) => json<{ task: TaskMeta }>(r)),

  task: (id: string) => fetch(`/api/tasks/${id}`).then((r) => json<{ task: TaskFull }>(r)),

  deleteTask: (id: string) => fetch(`/api/tasks/${id}`, { method: "DELETE" }),

  schedules: () =>
    fetch("/api/schedules").then((r) => json<{ schedules: ScheduledTaskInfo[] }>(r)),

  createSchedule: (p: { name?: string; prompt: string; model?: string; interval_days: number; at: string; project_id?: string }) =>
    fetch("/api/schedules", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(p),
    }).then((r) => json<{ schedule: ScheduledTaskInfo }>(r)),

  updateSchedule: (id: string, p: { name?: string; enabled?: boolean; prompt?: string; model?: string; interval_days?: number; at?: string; project_id?: string }) =>
    fetch(`/api/schedules/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(p),
    }).then((r) => json<{ schedule: ScheduledTaskInfo }>(r)),

  deleteSchedule: (id: string) => fetch(`/api/schedules/${encodeURIComponent(id)}`, { method: "DELETE" }),

  /** 手动触发一次计划任务（立即执行，不影响原有周期） */
  runSchedule: (id: string) =>
    fetch(`/api/schedules/${encodeURIComponent(id)}/run`, { method: "POST" }).then((r) =>
      json<{ ok: boolean }>(r),
    ),

  settings: () =>
    fetch("/api/settings").then((r) => json<{ model: string; theme: Theme; theme_color: ThemeColor }>(r)),

  saveSettings: (payload: { model?: string; theme?: Theme; theme_color?: ThemeColor }) =>
    fetch("/api/settings", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }).then((r) => json<{ model: string; theme: Theme; theme_color: ThemeColor }>(r)),

  memory: () =>
    fetch("/api/memory").then((r) => json<{ path: string; content: string }>(r)),

  saveMemory: (content: string) =>
    fetch("/api/memory", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ content }),
    }),

  mcp: () => fetch("/api/mcp").then((r) => json<{ servers: MCPStatus[] }>(r)),

  mcpConfig: () =>
    fetch("/api/mcp/config").then((r) =>
      json<{ servers: Record<string, MCPServerConfig> }>(r),
    ),

  saveMcpConfig: (servers: Record<string, MCPServerConfig>) =>
    fetch("/api/mcp/config", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ servers }),
    }).then((r) => json<{ servers: Record<string, MCPServerConfig> }>(r)),

  skills: (projectDir?: string) =>
    fetch(`/api/skills${projectDir ? `?project_dir=${encodeURIComponent(projectDir)}` : ""}`).then((r) =>
      json<{ skills: SkillInfo[] }>(r),
    ),

  skill: (name: string, source = "global") =>
    fetch(`/api/skills/${encodeURIComponent(name)}?source=${source}`).then((r) =>
      json<{ skill: SkillDetail }>(r),
    ),

  createSkill: (payload: { name: string; description: string; content: string }) =>
    fetch("/api/skills", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }).then((r) => json<{ skill: SkillInfo }>(r)),

  updateSkill: (name: string, payload: { description: string; content: string }) =>
    fetch(`/api/skills/${encodeURIComponent(name)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    }).then((r) => json<{ ok: boolean }>(r)),

  deleteSkill: (name: string, source = "global") =>
    fetch(`/api/skills/${encodeURIComponent(name)}?source=${source}`, { method: "DELETE" }),

  /** 技能目录 / MCP 配置文件位置（后端会确保目录已创建） */
  skillDirs: (projectDir?: string) =>
    fetch(`/api/skills/dirs?project_dir=${encodeURIComponent(projectDir ?? "")}`).then((r) =>
      json<{ dirs: SkillDirs }>(r),
    ),

  /** 启用/禁用技能 */
  skillToggle: (name: string, disabled: boolean) =>
    fetch("/api/skills/toggle", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name, disabled }),
    }).then((r) => json<{ ok: boolean }>(r)),

  /** 在系统文件管理器中打开技能目录 */
  openSkillDir: (source: string, name?: string) =>
    fetch("/api/skills/open", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source, name: name ?? "" }),
    }).then((r) => json<{ ok: boolean; path: string }>(r)),

  /** 在文件管理器中定位 MCP 配置文件 */
  openMcpFile: () =>
    fetch("/api/mcp/open", { method: "POST" }).then((r) =>
      json<{ ok: boolean; path: string; exists: boolean }>(r),
    ),

  /** 粘贴 SKILL.md 全文导入（自动解析 frontmatter / 推断名称） */
  importSkill: (payload: { content: string; name?: string; description?: string; source?: string }) =>
    fetch("/api/skills/import", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ source: "global", ...payload }),
    }).then((r) => json<{ ok: boolean; name?: string; description?: string; skill?: SkillInfo }>(r)),

  /** 上传随消息发送的附件，返回落盘后的绝对路径（模型据此用工具/MCP 读取） */
  upload: (taskId: string, files: File[]) => {
    const fd = new FormData()
    for (const f of files) fd.append("files", f)
    return fetch(`/api/upload?task_id=${encodeURIComponent(taskId)}`, {
      method: "POST",
      body: fd,
    }).then((r) => json<{ files: { path: string; name: string; size: number }[] }>(r))
  },

  /** 答复一次写操作确认（普通访问模式） */
  approval: (id: string, allow: boolean) =>
    fetch(`/api/approvals/${encodeURIComponent(id)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allow }),
    }).then((r) => json<{ ok: boolean }>(r)),

  /** 回答一次 ask 工具的提问 */
  answer: (id: string, answer: string) =>
    fetch(`/api/questions/${encodeURIComponent(id)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ answer }),
    }).then((r) => json<{ ok: boolean }>(r)),

  /** token 用量统计（按天×模型聚合，日期升序） */
  usage: () => fetch("/api/usage").then((r) => json<{ records: UsageRecord[] }>(r)),
}

// streamChat 发起 SSE 聊天；onEvent 收到每条事件，返回时流已结束。abort 信号可中断。
// attachments 为已上传附件的绝对路径，后端会拼进消息文本。
export async function streamChat(
  taskId: string,
  text: string,
  mode: string,
  model: string,
  onEvent: (ev: ChatEvent) => void,
  signal?: AbortSignal,
  attachments: string[] = [],
): Promise<void> {
  const res = await fetch("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task_id: taskId, text, mode, model, attachments }),
    signal,
  })
  if (!res.ok || !res.body) {
    let msg = `HTTP ${res.status}`
    try {
      const body = await res.json()
      if (body?.error) msg = body.error
    } catch {
      /* 忽略 */
    }
    throw new Error(msg)
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ""
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    let idx: number
    while ((idx = buf.indexOf("\n\n")) >= 0) {
      const frame = buf.slice(0, idx)
      buf = buf.slice(idx + 2)
      const line = frame.split("\n").find((l) => l.startsWith("data: "))
      if (!line) continue
      try {
        onEvent(JSON.parse(line.slice(6)) as ChatEvent)
      } catch {
        /* 跳过坏帧 */
      }
    }
  }
}
