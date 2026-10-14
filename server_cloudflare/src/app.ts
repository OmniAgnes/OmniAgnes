/**
 * 青鸟平台服务端 · Hono 应用本体（与运行时无关）。
 * 两个入口共用它：index.ts（Cloudflare Workers）与 local/main.ts（本地 Node 部署）。
 * 协议口径见 docs/服务端API契约.md，改这里必须同步改契约与桌面端调用方。
 */
import { Hono } from 'hono'
import type { AppEnv } from './auth.ts'
import { ApiError } from './db.ts'
import { desk } from './desk.ts'
import { ide } from './ide.ts'
import { gw } from './gateway.ts'
import { admin } from './admin.ts'

export const app = new Hono<AppEnv>()

app.get('/up', (c) => c.json({ ok: true }))

/**
 * 桌面端（Electron / 本机 8791）跨源调用云端接口，带 Authorization，浏览器会先发 OPTIONS 预检。
 * 不加这层 CORS，连 probeServer() 的 GET /api/state 都会被拦下——前端读不到响应就判后台不可达，
 * 于是整个账号/积分入口不渲染（表现是「配了域名也没有账号页」）。口径与 Go 版 cors() 一致。
 */
const CORS_HEADERS: Record<string, string> = {
  'Access-Control-Allow-Origin': '*',
  'Access-Control-Allow-Methods': 'GET,POST,PUT,DELETE,OPTIONS',
  'Access-Control-Allow-Headers': 'Content-Type,Authorization',
  'Access-Control-Max-Age': '600'
}
app.use('/api/*', async (c, next) => {
  if (c.req.method === 'OPTIONS') return new Response(null, { status: 204, headers: CORS_HEADERS })
  await next()
  for (const [k, v] of Object.entries(CORS_HEADERS)) c.res.headers.set(k, v)
})

/** 桌面端启动时探测这个：通了就说明服务端活着（与 Go 版同款响应）。 */
app.get('/api/health', async (c) => {
  await c.env.DB.prepare(`SELECT 1`).first()
  return c.json({ ok: true, service: 'qingniao-server', version: '1.0.0', port: 0, db: 'cloudflare-d1' })
})

// 先 desk 后 admin：/api/auth/login|send-code 两条路径重合，由 desk 按请求体是不是 phone 分流，
// 非手机号请求 next() 落回 admin 的邮箱版。注册顺序决定谁先拿到请求，别调换。
app.route('/api', desk)
app.route('/api/ide', ide)
app.route('/api/ide/v1', gw)
app.route('/api', admin)

// 未匹配的 /api/* 路径：明确 404，不能落进 SPA 兜底（否则接口错误被吞成 HTML）
app.notFound((c) => c.json({ message: '接口不存在' }, 404))

// 统一错误出口：业务错误回 {message}，其余按 500
app.onError((err, c) => {
  if (err instanceof ApiError) return c.json({ message: err.message }, err.status as never)
  console.error('[server]', err)
  return c.json({ message: '服务端内部错误' }, 500)
})
