// 与后端 internal/webui 的 JSON 契约对应。

export interface TaskMeta {
  id: string
  title: string
  project_id?: string
  model?: string
  mode?: string
  created_at: string
  updated_at: string
}

export interface ToolCallMsg {
  id: string
  name: string
  arguments: Record<string, unknown>
}

export interface ToolResultMsg {
  call_id: string
  content: string
  is_error?: boolean
}

export interface ChatMessage {
  role: "user" | "assistant" | "tool" | "system"
  text?: string
  tool_calls?: ToolCallMsg[]
  tool_result?: ToolResultMsg
}

export interface TaskFull extends TaskMeta {
  messages: ChatMessage[]
}

export interface Project {
  id: string
  name: string
  path: string
  created_at: string
}

export interface MCPStatus {
  name: string
  kind: string
  target: string
  connected: boolean
  tool_count: number
  error?: string
}

export interface SkillInfo {
  name: string
  description: string
  path: string
  source: string
}

// 技能详情（含 SKILL.md 原文，供编辑）
export interface SkillDetail {
  name: string
  description: string
  content: string
  path: string
}

export interface MCPServerConfig {
  command?: string
  args?: string[]
  env?: Record<string, string>
  url?: string
  disabled?: boolean
}

export interface BrowseEntry {
  name: string
  path: string
  is_dir: boolean
}

export interface BrowseResult {
  path: string
  parent: string
  entries: BrowseEntry[]
}

export type Theme = "dark" | "light" | "system"

export type ThemeColor = "zinc" | "violet" | "blue" | "emerald" | "amber" | "rose"

export interface ProviderConfig {
  name?: string
  type: string
  model?: string
  models?: string[]
  base_url?: string
  api_key?: string
  env_key?: string
  default?: boolean
  context_window?: number
  max_output_tokens?: number
  input_types?: string[]
  reasoning?: string
}

export interface StateInfo {
  model: string
  theme: Theme
  theme_color?: ThemeColor
  configured: boolean
  version: string
  start_cwd: string
}

// SSE 聊天事件
export type ChatEvent =
  | { type: "start"; task_id: string; title: string; mode: string }
  | { type: "text"; text: string }
  | { type: "tool_start"; name: string; args: string }
  | { type: "tool_output"; name: string; output: string; is_error: boolean }
  | { type: "tool_end"; name: string; is_error: boolean }
  | { type: "approval_pending"; name: string }
  | { type: "approval"; id: string; call_id: string; name: string; args: string }
  | { type: "approval_result"; id: string; call_id: string; allowed: boolean }
  | { type: "error"; error: string }
  | { type: "done"; model?: string }
