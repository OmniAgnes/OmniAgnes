// 桌面端调用 omni-server（9000）进行登录/积分；token 存 localStorage。
// 服务器地址固定在内核配置文件（config.json 的 server_url）里，前端启动时经
// /api/server-url 读取，不用 localStorage。
const TOKEN_KEY = "omni.server_token"

let serverBase = "http://127.0.0.1:9000"

/** 启动时从内核读取配置的后台地址；拿不到时保留缺省值 */
export async function initServerUrl(): Promise<void> {
  try {
    const r = await fetch("/api/server-url")
    const d = await r.json()
    if (d?.url) serverBase = d.url
  } catch {
    /* 内核不可达时用缺省地址 */
  }
}

export interface ServerUser {
  id: number
  phone: string
  is_admin: boolean
  balance: number
  created_at: string
}
export interface ServerCredit {
  id: number
  user_id: number
  amount: number
  reason: string
  model?: string
  created_at: string
  /** 到期时间（RFC3339）；空=永久。仅「获得」行有 */
  expires_at?: string
  /** 该笔「获得」的剩余可用积分 */
  remaining?: number
}
/** 后台启用的模型（内置模型组）：multiplier=原倍率，free 时现倍率按 0 计 */
export interface ServerModel {
  key: string
  multiplier: number
  free: boolean
}

// 服务器地址只读：来自内核配置（initServerUrl 读取后的值）
export function serverUrl(): string {
  return serverBase
}
export function serverToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ""
}
export function setServerToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

async function jfetch(base: string, path: string, method: string, body?: unknown): Promise<any> {
  const res = await fetch(base + path, {
    method,
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${serverToken()}` },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(10000),
  })
  let data: any = {}
  try {
    data = await res.json()
  } catch {
    /* 空响应 */
  }
  if (!res.ok) {
    const e = new Error(data.error || `HTTP ${res.status}`)
    e.name = "ServerRequestError"
    ;(e as { status?: number }).status = res.status
    throw e
  }
  return data
}

export const server = {
  sendCode: async (phone: string) => jfetch(serverUrl(), "/api/auth/send-code", "POST", { phone }),
  /** 登录成功后 token 直接落 localStorage，桌面端重启可恢复登录态 */
  login: async (phone: string, code: string, agreement: boolean) => {
    const r = await jfetch(serverUrl(), "/api/auth/login", "POST", { phone, code, agreement })
    setServerToken(r.token as string)
    return r
  },
  logout: () => jfetch(serverUrl(), "/api/auth/logout", "POST"),
  me: async () => {
    const r = await jfetch(serverUrl(), "/api/me", "GET")
    return { user: r.user as ServerUser, records: (r.records ?? []) as ServerCredit[] }
  },
  /** 每日积分领取（幂等：当天已领则 claimed=false） */
  claim: async () => {
    const r = await jfetch(serverUrl(), "/api/claim", "POST")
    return { claimed: !!r.claimed, amount: (r.amount ?? 0) as number, balance: (r.balance ?? 0) as number }
  },
  /** 后台启用的模型列表（登录用户的前端内置模型组） */
  models: async (): Promise<ServerModel[]> => {
    const r = await jfetch(serverUrl(), "/api/models", "GET")
    return (r.models ?? []) as ServerModel[]
  },
}