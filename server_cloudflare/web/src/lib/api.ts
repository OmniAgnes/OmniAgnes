// 后台前端 API 客户端。请求同源（前端与 Worker 同域），token 存 localStorage。
//
// ⚠️ 口径必须与 src/admin.ts 严格对齐：后端已从「手机号」协议整体迁到「邮箱」协议，
//    路由也从 /admin/users、/admin/providers 换成了 /admin/members、/admin/ai-upstreams。
//    改这里之前先看 admin.ts 的路由表，别按旧名字想当然。
const TOKEN_KEY = "omni_server_token"

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ""
}
export function setToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

/** 当前登录者（后端 auth.userPayload：id/name/email/role） */
export interface User {
  id: number
  name: string
  email: string
  /** admin | member；判管理员用这个 */
  role: string
  points: number
  status: string
  created_at: string
}
/** 是否管理员 */
export const isAdmin = (u: User | null): boolean => !!u && u.role === "admin"

/** 站点公开信息（desk.ts /api/site，登录页读 title/agreement/test_mode） */
export interface Site {
  title: string
  footer: string
  agreement: string
  test_mode: boolean
}

/** 公开设置（admin.ts /api/settings） */
export interface PublicSettings {
  mall_name: string
  logo_url: string
  mail_configured: boolean
}

/** 后端分页信封（db.ts pageEnvelope） */
export interface Page<T> {
  items: T[]
  total: number
  page: number
  per_page: number
  pages: number
}

/** 会员行（admin.ts memberCols） */
export interface Member {
  id: number
  name: string
  email: string
  role: string
  points: number
  status: string
  created_at: string
}

/** 上游（admin.ts upstreamPayload） */
export interface Upstream {
  id: number
  name: string
  driver: string
  base_url: string
  enabled: boolean
  sort: number
  remark: string
  models_synced_at: string
  keys: {
    id: number
    name: string
    enabled: boolean
    status: string
    latency_ms: number | null
    last_error: string
    last_checked_at: string
    last_used_at: string
  }[]
  models: {
    id: number
    model_id: number
    model: string
    enabled: boolean
    status: string
    latency_ms: number | null
    last_error: string
    last_checked_at: string
  }[]
}

/** AI 模型（admin.ts modelAdminPayload） */
export interface AiModel {
  id: number
  name: string
  builtin: boolean
  ratio: number
  enabled: boolean
  remark: string
  sort: number
  free: boolean
  discount: number | null
  discount_label: string
  effective_ratio: number
  /** ok | unknown | error | unmatched */
  health: string
  upstreams: { name: string; model: string; status: string }[]
}

/** 管理端总览（admin.ts /admin/dashboard） */
export interface Dashboard {
  users: number
  devices: number
  online: number
  upstreams: number
  models: number
  mail_configured: boolean
  calls_today: number
  tokens_today: number
}

/** 邮件配置（admin.ts /admin/mail-config） */
export interface MailConfig {
  host: string
  port: number
  user: string
  from: string
  /** ssl | starttls */
  secure: string
  /** 已设置密码（后端不回显密码本身） */
  has_pass: boolean
  /** 写接口专用：新密码（留空=保持原密码） */
  pass?: string
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${getToken()}`,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  let data: any = {}
  try {
    data = await res.json()
  } catch {
    /* 空响应 */
  }
  // 后端错误体统一是 {message}（db.ts 的 fail → app.onError）。
  // 只读 data.error 会把「未登录或授权已失效」这类中文提示丢掉、只剩裸 HTTP 401。
  if (!res.ok) throw new Error(data.message || data.error || `HTTP ${res.status}`)
  return data as T
}

/** 拉分页信封；后端部分列表接口直接返回数组（非分页），用 listOf 兜底 */
function pageOf<T>(r: Page<T> | T[]): Page<T> {
  if (Array.isArray(r)) return { items: r, total: r.length, page: 1, per_page: r.length, pages: 1 }
  return r
}

export const api = {
  // ---- 公开 ----
  site: () => req<Site>("GET", "/api/site"),
  settings: () => req<PublicSettings>("GET", "/api/settings"),
  /** 发邮箱验证码。purpose：login 验证码登录 / register 注册 */
  sendCode: (email: string, purpose: "register" | "login" = "login") =>
    req<{ hint?: string; cooldown?: number }>("POST", "/api/auth/send-code", { email, purpose }),
  /**
   * 登录（admin.ts /auth/login）。后端按请求体分流：
   *   传 code     → 验证码登录（需先配好邮件服务）
   *   传 password → 账号密码登录（管理员兜底）
   * 两个都不传回 422「请输入密码或邮箱验证码」。
   */
  login: (email: string, v: { code?: string; password?: string }) =>
    req<{ token: string; user: User; is_new?: boolean }>("POST", "/api/auth/login", {
      email,
      code: v.code,
      password: v.password,
    }),
  register: (email: string, password: string, code: string, name?: string) =>
    req<{ token: string; user: User }>("POST", "/api/auth/register", { email, password, code, name }),

  // ---- 登录态 ----
  logout: () => req<any>("POST", "/api/auth/logout"),
  /** 当前登录者。注意是 /auth/me 不是 /me —— 统一走邮箱协议（admin.ts 提供 /me 给桌面端）。 */
  me: async () => (await req<{ user: User }>("GET", "/api/auth/me")).user,
  /** 生成一次性授权码（桌面端「打开授权」登录链路：登录后调它拿 code，再跳 omni://auth?code=xxx）。 */
  pair: (device?: string) =>
    req<{ code: string; expires_in: number; app_url: string; user: User }>("POST", "/api/ide/pair", { device }),

  // ---- 会员侧（登录用户自助看自己的积分与用量）----
  /** 自己的积分流水（point_logs），type 可选 consume/grant/adjust */
  myPoints: async (type = "", page = 1, perPage = 20) => {
    const qs = new URLSearchParams({ page: String(page), per_page: String(perPage) })
    if (type) qs.set("type", type)
    return pageOf(await req<Page<any>>("GET", `/api/me/points?${qs}`))
  },
  /** 自己的 AI 用量（ai_usages） */
  myUsages: async (page = 1, perPage = 20) =>
    pageOf(await req<Page<any>>("GET", `/api/me/usages?page=${page}&per_page=${perPage}`)),

  // ---- 管理端：总览 ----
  dashboard: () => req<Dashboard>("GET", "/api/admin/dashboard"),

  // ---- 管理端：会员 ----
  members: async (q = "", page = 1, perPage = 20) => {
    const qs = new URLSearchParams({ page: String(page), per_page: String(perPage) })
    if (q) qs.set("q", q)
    return pageOf<Member>(await req<Page<Member>>("GET", `/api/admin/members?${qs}`))
  },
  member: (id: number) => req<Member>("GET", `/api/admin/members/${id}`),
  updateMember: (id: number, v: Partial<Pick<Member, "name" | "status" | "points">> & { remark?: string }) =>
    req<Member>("PUT", `/api/admin/members/${id}`, v),
  memberLogs: async (id: number, page = 1, perPage = 20) =>
    pageOf(await req<Page<any>>("GET", `/api/admin/members/${id}/logs?page=${page}&per_page=${perPage}`)),
  memberUsages: async (id: number, page = 1, perPage = 20) =>
    pageOf(await req<Page<any>>("GET", `/api/admin/members/${id}/usages?page=${page}&per_page=${perPage}`)),

  // ---- 管理端：上游 ----
  /** 上游列表直接返回数组（非分页），pageOf 归一化成信封 */
  upstreams: async () => pageOf<Upstream>(await req<Upstream[]>("GET", "/api/admin/ai-upstreams")),
  upstream: (id: number) => req<Upstream>("GET", `/api/admin/ai-upstreams/${id}`),
  createUpstream: (v: Partial<Upstream>) => req<Upstream>("POST", "/api/admin/ai-upstreams", v),
  updateUpstream: (id: number, v: Partial<Upstream>) => req<Upstream>("PUT", `/api/admin/ai-upstreams/${id}`, v),
  deleteUpstream: (id: number) => req<any>("DELETE", `/api/admin/ai-upstreams/${id}`),
  /** 手动新增密钥到上游 */
  addUpstreamKey: (id: number, v: { key?: string; name?: string }) =>
    req<any>("POST", `/api/admin/ai-upstreams/${id}/keys`, v),
  updateKey: (key: string, v: { enabled?: boolean; name?: string }) =>
    req<any>("PUT", `/api/admin/ai-upstreams/keys/${encodeURIComponent(key)}`, v),
  deleteKey: (key: string) => req<any>("DELETE", `/api/admin/ai-upstreams/keys/${encodeURIComponent(key)}`),
  testKey: (key: string) => req<any>("POST", `/api/admin/ai-upstreams/keys/${encodeURIComponent(key)}/test`),
  probeKey: (key: string) => req<any>("POST", `/api/admin/ai-upstreams/keys/${encodeURIComponent(key)}/probe`),
  testUpstream: (id: number) => req<any>("POST", `/api/admin/ai-upstreams/${id}/test`),
  syncUpstream: (id: number) => req<any>("POST", `/api/admin/ai-upstreams/${id}/sync`),
  importUpstreamModels: (id: number) => req<any>("POST", `/api/admin/ai-upstreams/${id}/import-models`),
  addUpstreamModel: (id: number, v: { model?: string; name?: string }) =>
    req<any>("POST", `/api/admin/ai-upstreams/${id}/models`, v),
  updateUpstreamModel: (model: string, v: Record<string, unknown>) =>
    req<any>("PUT", `/api/admin/ai-upstreams/models/${encodeURIComponent(model)}`, v),
  deleteUpstreamModel: (model: string) =>
    req<any>("DELETE", `/api/admin/ai-upstreams/models/${encodeURIComponent(model)}`),

  // ---- 管理端：AI 模型 ----
  models: async () => pageOf<AiModel>(await req<AiModel[]>("GET", "/api/admin/ai-models/models")),
  createModel: (v: Partial<AiModel>) => req<AiModel>("POST", "/api/admin/ai-models/models", v),
  updateModel: (name: string, v: Partial<AiModel>) =>
    req<AiModel>("PUT", `/api/admin/ai-models/models/${encodeURIComponent(name)}`, v),
  deleteModel: (name: string) => req<any>("DELETE", `/api/admin/ai-models/models/${encodeURIComponent(name)}`),
  testModel: (name: string) => req<any>("POST", `/api/admin/ai-models/models/${encodeURIComponent(name)}/test`),
  rematchModels: () => req<any>("POST", "/api/admin/ai-models/re-match"),
  modelHealth: () => req<Record<string, any>>("GET", "/api/admin/ai-models/health"),
  modelHealthSeries: () => req<Record<string, any>>("GET", "/api/admin/ai-models/health/series"),

  // ---- 管理端：用量 ----
  usageOverview: () => req<Record<string, any>>("GET", "/api/admin/usage/overview"),
  usageStats: () => req<Record<string, any>>("GET", "/api/admin/usage/stats"),

  // ---- 管理端：邮件配置 ----
  mailConfig: () => req<MailConfig>("GET", "/api/admin/mail-config"),
  saveMailConfig: (v: Partial<MailConfig>) => req<MailConfig>("PUT", "/api/admin/mail-config", v),
  testMail: () => req<any>("POST", "/api/admin/mail-config/test"),

  // ---- 个人资料 ----
  sendProfileEmailCode: (email: string) => req<any>("POST", "/api/admin/profile/email/code", { email }),
  saveProfileEmail: (email: string, code: string) =>
    req<any>("PUT", "/api/admin/profile/email", { email, code }),
}
