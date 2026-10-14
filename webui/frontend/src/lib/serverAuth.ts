// 桌面端调用 omni-server 进行登录/积分。
//
// 后台地址的取值优先级（用户自己填的优先，不要他去改配置文件）：
//   1. localStorage 里用户填的地址（设置 → 账号 → 后台地址）
//   2. 内核配置文件 config.json 的 server_url（经 /api/server-url 读，给运维/预置用）
//   3. 缺省 http://127.0.0.1:9000
// token 也存 localStorage。
const TOKEN_KEY = "omni.server_token"
const URL_KEY = "omni.server_url"

let serverBase = "http://127.0.0.1:9000"

/** 归一化用户填的地址：去空白、补协议头、去掉尾部斜杠。
 *  空串表示「不覆盖」，交回内核配置那条路。 */
export function normalizeServerUrl(raw: string): string {
  let u = raw.trim()
  if (!u) return ""
  if (!/^https?:\/\//i.test(u)) u = "https://" + u
  return u.replace(/\/+$/, "")
}

/** 用户填的后台地址（localStorage）；空串=没填 */
export function savedServerUrl(): string {
  return localStorage.getItem(URL_KEY) ?? ""
}

/** 保存用户填的后台地址；空串=清除，回退到内核配置。
 *  返回归一化后的最终地址，供调用方直接拿去探活。 */
export function saveServerUrl(raw: string): string {
  const u = normalizeServerUrl(raw)
  if (u) {
    localStorage.setItem(URL_KEY, u)
    serverBase = u
  } else {
    localStorage.removeItem(URL_KEY)
  }
  return u
}

/** 启动时确定后台地址：用户填的优先，其次读内核配置。拿不到时保留缺省值 */
export async function initServerUrl(): Promise<void> {
  const mine = savedServerUrl()
  if (mine) {
    serverBase = mine
    return
  }
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

/** 当前生效的后台地址：用户填的（saveServerUrl 写入过）优先，否则是内核配置那条 */
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