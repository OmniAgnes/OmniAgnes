// API 客户端：请求同源（server 前端与后端同 9000）。token 存 localStorage。
const TOKEN_KEY = "omni_server_token"

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ""
}
export function setToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export interface User {
  id: number
  phone: string
  is_admin: boolean
  balance: number
  agreement: boolean
  created_at: string
}
export interface CreditRecord {
  id: number
  user_id: number
  amount: number
  reason: string
  model?: string
  created_at: string
  /** 到期时间（RFC3339）；空=永久。仅「获得」行有 */
  expires_at?: string
  remaining?: number
}
/** AI 请求记录（管理端返回额外带 phone） */
export interface UsageLog {
  id: number
  user_id: number
  model: string
  input_tokens: number
  output_tokens: number
  credits: number
  /** ok=已计费 / insufficient=积分不足未扣 / error=请求失败 */
  status: string
  error?: string
  created_at: string
  phone?: string
}
/** 会员使用量页数据：summary/credit 为汇总，items 按 list 分页 */
export interface MemberUsage {
  summary: { requests: number; input_tokens: number; output_tokens: number; credits: number }
  credit: { got: number; spent: number }
  /** ai=AI 请求记录 / spent=消耗记录 / got=获取记录 */
  list: "ai" | "spent" | "got"
  total: number
  page: number
  page_size: number
  items: (UsageLog | CreditRecord)[]
}
/** 管理端使用量数据：overview 为总览，items 按 list 分页 */
export interface AdminUsage {
  overview: { users: number; granted: number; spent: number; today_requests: number; today_credits: number }
  total: number
  page: number
  page_size: number
  items: (UsageLog | (CreditRecord & { phone: string }))[]
}
export interface ProviderModel {
  key: string
  context_window?: number
  max_output_tokens?: number
  input_types?: string[]
  reasoning?: string
}
export interface Provider {
  id: number
  name: string
  type: string
  model?: string
  models?: ProviderModel[]
  base_url?: string
  api_key?: string
  enabled: boolean
}
export interface Model {
  id: number
  key: string
  builtin: boolean
  enabled: boolean
  multiplier: number
  free: boolean
  created_at: string
}
export interface ModelProvider {
  model_id: number
  provider_id: number
  enabled: boolean
  priority: number
}

/** 站点公开信息（管理端在后台设置） */
export interface Site {
  title: string
  footer: string
  agreement: string
  test_mode: boolean
}

/** 管理端系统设置 */
export interface AdminSettings {
  site_title: string
  footer: string
  agreement: string
  daily_credit_enabled: boolean
  daily_credit_amount: number
  daily_credit_days: number
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
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data as T
}

export const api = {
  state: () => req<any>("GET", "/api/state"),
  site: () => req<Site>("GET", "/api/site"),
  sendCode: (phone: string) => req<any>("POST", "/api/auth/send-code", { phone }),
  login: (phone: string, code: string, agreement: boolean) =>
    req<{ token: string; user: User; is_new: boolean }>("POST", "/api/auth/login", { phone, code, agreement }),
  logout: () => req<any>("POST", "/api/auth/logout"),
  me: async () => {
    const r = await req<{ user: User }>("GET", "/api/me")
    return r.user
  },
  /** 用户修改自己的手机号（新号需验证码） */
  mePhone: (phone: string, code: string) =>
    req<{ user: User }>("POST", "/api/me/phone", { phone, code }),

  /** 会员使用量：汇总 + 按 list 分页的记录（ai=请求记录/spent=消耗/got=获取） */
  usage: (list: "ai" | "spent" | "got", page = 1, pageSize = 20) =>
    req<MemberUsage>("GET", `/api/usage?list=${list}&page=${page}&page_size=${pageSize}`),
  /** 桌面端上报一次 AI 请求（按模型倍率扣积分；积分不足抛错） */
  reportUsage: (v: { model: string; input_tokens: number; output_tokens: number; status?: string; error?: string }) =>
    req<{ credits: number; balance: number }>("POST", "/api/usage", v),

  adminUsers: async (page = 1, pageSize = 20) => {
    const r = await req<{ users: User[]; total: number }>("GET", `/api/admin/users?page=${page}&page_size=${pageSize}`)
    return { users: r.users ?? [], total: r.total ?? 0 }
  },
  adminCredit: (id: number, amount: number, reason: string) =>
    req<{ balance: number }>("POST", `/api/admin/users/${id}/credit`, { amount, reason }),
  /** 管理端使用量：总览 + 按 list 分页的全体记录（ai=请求记录/credit=积分流水，phone 非空按手机号过滤） */
  adminUsage: (v: { list?: "ai" | "credit"; page?: number; pageSize?: number; phone?: string } = {}) => {
    const q = new URLSearchParams({
      list: v.list ?? "ai",
      page: String(v.page ?? 1),
      page_size: String(v.pageSize ?? 20),
    })
    if (v.phone) q.set("phone", v.phone)
    return req<AdminUsage>("GET", "/api/admin/usage?" + q.toString())
  },
  adminSettings: () => req<AdminSettings>("GET", "/api/admin/settings"),
  saveAdminSettings: (v: Partial<AdminSettings>) =>
    req<AdminSettings>("POST", "/api/admin/settings", v),

  providers: async () => {
    const r = await req<{ providers: Provider[] }>("GET", "/api/admin/providers")
    return { providers: r.providers ?? [] }
  },
  saveProvider: (p: Partial<Provider>) =>
    req<{ provider: Provider }>(p.id ? "PUT" : "POST", p.id ? `/api/admin/providers/${p.id}` : "/api/admin/providers", p),
  deleteProvider: (id: number) => req("DELETE", `/api/admin/providers/${id}`),

  models: async () => {
    const r = await req<{ models: Model[]; model_providers: ModelProvider[]; providers: Provider[] }>(
      "GET",
      "/api/admin/models",
    )
    return { models: r.models ?? [], model_providers: r.model_providers ?? [], providers: r.providers ?? [] }
  },
  saveModel: (m: Partial<Model> & { provider_id?: number }) =>
    req<{ model: Model }>(m.id ? "PUT" : "POST", m.id ? `/api/admin/models/${m.id}` : "/api/admin/models", m),
  deleteModel: (id: number) => req("DELETE", `/api/admin/models/${id}`),
  toggleModelProvider: (model_id: number, provider_id: number, enabled: boolean) =>
    req("POST", "/api/admin/model-providers/toggle", { model_id, provider_id, enabled }),
}