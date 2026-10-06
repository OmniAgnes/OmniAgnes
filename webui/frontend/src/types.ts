// 与后端 internal/webui 的 JSON 契约对应。

export interface TaskMeta {
  id: string
  title: string
  project_id?: string
  model?: string
  mode?: string
  created_at: string
  updated_at: string
  /** 后台无人值守执行中（计划任务轮次），任务 TAB 显示「处理中」 */
  running?: boolean
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
  /** 工具产出的多媒体（持久化历史里的原始结构，重开软件后恢复展示用） */
  attachments?: RawAttachment[]
}

/** 持久化历史里的原始附件（对应后端 llm.Attachment：Data/URL/Path 三选一） */
export interface RawAttachment {
  kind: "image" | "video" | "audio"
  media_type?: string
  caption?: string
  data?: string
  url?: string
  path?: string
}

export interface ChatMessage {
  role: "user" | "assistant" | "tool" | "system"
  text?: string
  tool_calls?: ToolCallMsg[]
  tool_result?: ToolResultMsg
  /** assistant 最终答复的工作耗时（毫秒）；持久化，重开会话后回复上仍显示「已工作」 */
  elapsed_ms?: number
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

/** 计划任务：到点（每 N 天的 HH:MM）后端自动以指定模型执行一次 prompt */
export interface ScheduledTaskInfo {
  id: string
  name: string
  prompt: string
  model?: string
  interval_days: number
  at: string
  project_id?: string
  enabled: boolean
  last_run?: string
  created_at: string
  next_run?: string
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
  disabled?: boolean
}

/** token 用量统计（后端按 日期×模型 聚合） */
export interface UsageRecord {
  date: string // YYYY-MM-DD
  model: string
  input: number
  output: number
  /** 聚合结果：合并进该 (日期, 模型) 的对话次数 */
  calls?: number
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

// 技能 / MCP 的落盘位置（供「打开目录」与粘贴导入用）
export interface SkillDirInfo {
  path?: string
  dir?: string
  error?: string
}

export interface SkillDirs {
  global?: SkillDirInfo
  project?: SkillDirInfo
  mcp?: SkillDirInfo
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

export type ThemeColor = "zinc" | "violet" | "blue" | "lime" | "emerald" | "amber" | "rose"

/** 单个模型的个性化参数；未设置项回落到供应商级同名字段 */
export interface ModelOptions {
  context_window?: number
  max_output_tokens?: number
  input_types?: string[]
  reasoning?: string
}

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
  /** 按模型名覆盖供应商级默认值 */
  model_options?: Record<string, ModelOptions>
}

export interface StateInfo {
  model: string
  theme: Theme
  theme_color?: ThemeColor
  configured: boolean
  version: string
  start_cwd: string
}

// 工具产出的多媒体附件（截图/生成图片/视频/音频），src 可为本地接口路径、data URL 或远程 URL
export interface AttachmentInfo {
  kind: "image" | "video" | "audio"
  media_type?: string
  caption?: string
  src: string
  path?: string
}

// SSE 聊天事件
// round = LLM 迭代轮次（从 0 开始）。一次提问会跑多轮，
// 每轮 = 一段文本 + 若干工具调用，UI 按 round 拆成独立消息。
export type ChatEvent =
  | { type: "start"; task_id: string; title: string; mode: string }
  | { type: "text"; text: string; round: number }
  | { type: "thinking"; text: string; round: number }
  | { type: "tool_start"; round: number; id: string; name: string; args: string }
  | { type: "write_base"; round: number; id: string; old: string }
  | { type: "tool_output"; round: number; id: string; name: string; output: string; is_error: boolean; attachments?: AttachmentInfo[] }
  | { type: "tool_end"; round: number; id: string; name: string; is_error: boolean }
  | { type: "approval_pending"; round: number; id: string }
  | { type: "approval"; id: string; call_id: string; name: string; args: string }
  | { type: "approval_result"; id: string; call_id: string; allowed: boolean }
  | { type: "question_pending"; round: number; id: string }
  | { type: "question"; id: string; call_id: string; question: string; options?: string[] }
  | { type: "question_answered"; id: string; call_id: string; answer: string }
  | { type: "error"; error: string; round: number }
  | { type: "media"; round: number; attachments: AttachmentInfo[] }
  /** 上游瞬时故障，provider 自动重试中（重试次数/总次数） */
  | { type: "retry"; attempt: number; max: number; round: number }
  | { type: "done"; model?: string; round: number; input_tokens?: number; output_tokens?: number }

/** 移动端远程访问状态：urls 为每张网卡的扫码 URL（自带设备 token）。 */
export type MobileInfo = {
  enabled: boolean
  port: number
  urls: string[]
}
