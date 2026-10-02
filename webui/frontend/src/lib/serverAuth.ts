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
/** 后台可达性：启动探活一次。不可达时桌面端按纯本地版运行，不渲染任何 server 相关入口 */
export async function probeServer(): Promise<boolean> {
  try {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), 3000)
    const res = await fetch(serverUrl() + "/api/state", { signal: ctrl.signal })
    clearTimeout(timer)
    return res.ok
  } catch {
    return false
  }
}
export function serverToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ""
}
export function setServerToken(t: string) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

async function jfetch(base: string, path: string, method: string, body?: unknown): Promise<any> {
  let res: Response
  try {
    res = await fetch(base + path, {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${serverToken()}` },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(10000),
    })
  } catch {
    // 连接失败/超时（后台没开或不可达）：中文提示，别把 fetch failed 抛给用户
    throw new Error("无法连接 omni-server，请确认后台服务已启动")
  }
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
  /** 账号信息 + 积分流水分页（kind=gain 只要获取 / spend 只要消耗，空=全部） */
  me: async (kind = "", page = 1, pageSize = 20) => {
    const q = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (kind) q.set("kind", kind)
    const r = await jfetch(serverUrl(), "/api/me?" + q.toString(), "GET")
    return {
      user: r.user as ServerUser,
      records: (r.records ?? []) as ServerCredit[],
      total: (r.total ?? 0) as number,
      page: (r.page ?? page) as number,
      page_size: (r.page_size ?? pageSize) as number,
    }
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
  /** 上报一次 AI 请求：按模型倍率扣积分并留痕（积分不足时 402 抛出） */
  reportUsage: async (u: { model: string; input_tokens: number; output_tokens: number; status?: string; error?: string }) => {
    const r = await jfetch(serverUrl(), "/api/usage", "POST", u)
    return { credits: (r.credits ?? 0) as number, balance: (r.balance ?? 0) as number }
  },
}