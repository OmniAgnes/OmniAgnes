// 桌面端调用 omni-server 进行登录/积分。
//
// 后台地址的取值优先级（不再单独配置，复用「设置 → 版本号」里那个更新域名）：
//   1. 桌面壳 userData/update.json 的 domain（IPC omni:update:getDomain）
//   2. 内核配置文件 config.json 的 server_url（经 /api/server-url 读）
//   3. 缺省 http://127.0.0.1:9000
// token 存 localStorage。
const TOKEN_KEY = "omni.server_token"

let serverBase = "http://127.0.0.1:9000"

/** 启动时确定后台地址：版本号界面配的域名优先，其次读内核配置。拿不到时保留缺省值 */
export async function initServerUrl(): Promise<void> {
  try {
    const d = await window.omniDesktop?.update.getDomain()
    if (d) {
      serverBase = d
      return
    }
  } catch {
    /* 网页端 / IPC 不可用时走下面的内核配置 */
  }
  try {
    const r = await fetch("/api/server-url")
    const j = await r.json()
    if (j?.url) serverBase = j.url
  } catch {
    /* 内核不可达时用缺省地址 */
  }
}

export interface ServerUser {
  id: number
  name: string
  email: string
  /** admin | member */
  role: string
  /** 积分余额 */
  points: number
}
export interface ServerCredit {
  id: number
  change: number
  after: number
  type: string
  type_label: string
  remark: string
  created_at: string
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

/** 网页授权页地址：桌面端「登录」→ 系统浏览器打开这个，登录成功跳 omni://auth?code=xxx。 */
export function authLoginUrl(): string {
  return serverUrl() + "/#/ide/login"
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
  /** 打开授权：用一次性授权码换长期 token（登录链路最后一步）。
   *  授权码来自网页授权页（用户点登录后浏览器打开后台登录页，登录成功跳 omni://auth?code=xxx）。 */
  exchangeCode: async (code: string, device: string) => {
    const r = await jfetch(serverUrl(), "/api/ide/token", "POST", { code, device })
    setServerToken(r.token as string)
    return { user: r.user as ServerUser, token: r.token as string }
  },
  logout: () => jfetch(serverUrl(), "/api/ide/logout", "POST"),
  /** 当前登录者（邮箱协议）。 */
  me: async () => {
    const r = await jfetch(serverUrl(), "/api/me", "GET")
    return { user: r.user as ServerUser }
  },
  /** 积分流水（point_logs），type 可选 consume/grant/adjust */
  points: async (type = "", page = 1, pageSize = 20) => {
    const q = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
    if (type) q.set("type", type)
    const r = await jfetch(serverUrl(), "/api/me/points?" + q.toString(), "GET")
    return {
      records: (r.items ?? []) as ServerCredit[],
      total: (r.total ?? 0) as number,
      page: (r.page ?? page) as number,
      page_size: (r.page_size ?? pageSize) as number,
    }
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