import type {
  BrowseResult,
  ChatEvent,
  MCPStatus,
  MCPServerConfig,
  Project,
  ProviderConfig,
  SkillDetail,
  SkillInfo,
  StateInfo,
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

  skills: () => fetch("/api/skills").then((r) => json<{ skills: SkillInfo[] }>(r)),

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

  /** 答复一次写操作确认（普通访问模式） */
  approval: (id: string, allow: boolean) =>
    fetch(`/api/approvals/${encodeURIComponent(id)}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ allow }),
    }).then((r) => json<{ ok: boolean }>(r)),
}

// streamChat 发起 SSE 聊天；onEvent 收到每条事件，返回时流已结束。abort 信号可中断。
export async function streamChat(
  taskId: string,
  text: string,
  mode: string,
  model: string,
  onEvent: (ev: ChatEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/chat", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task_id: taskId, text, mode, model }),
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
