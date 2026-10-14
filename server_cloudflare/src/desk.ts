/**
 * 桌面端兼容层（omni 桌面端 webui/frontend 说的是 Go 版 server/ 的协议）。
 *
 * 桌面端的账号入口只在 `probeServer()` 探到 GET /api/state 后才渲染，登录走
 * 手机号+验证码，余额叫 balance、积分按笔带到期时间与剩余量（FEFO 扣减）。
 * 这套语义与青鸟本体的「邮箱账号 + users.points + point_logs」不同，因此落在
 * desk_* 独立表里（见 schema.sql），模型与倍率则复用后台已配的 ai_models：
 * 后台配好的模型直接出现在桌面端「内置模型」里，扣分口径也一致。
 *
 * 错误体是 {error} 而不是全局的 {message}——因为桌面端 lib/serverAuth.ts 读的是
 * data.error；且 401 会被前端当作「会话过期」清掉本地 token，状态码不能含糊。
 *
 * /auth/login 与 /auth/send-code 两个路径和邮箱版重合：这里先看请求体形状，
 * 带 phone 的归本层，否则 next() 交回 admin（Hono 缓存 body，二次解析安全）。
 */
import { Hono } from 'hono'
import type { Context, MiddlewareHandler } from 'hono'
import type { AppEnv } from './auth.ts'
import { ApiError, baseURL, fail, randHex, rfc, sha256Hex, tzHours } from './db.ts'
import { costOf } from './ai.ts'

type C = Context<AppEnv>

/** 流水原因标记，与 Go 版 store 的同名常量一致（前端按 daily_claim 文案化）。 */
const REASON_DAILY = 'daily_claim'
const REASON_AI = 'AI 消耗'

/** 验证码有效期与错误次数上限（Go 版 OTPStore 是 5 分钟、无次数限制，这里补上限防试码）。 */
const CODE_TTL_MS = 5 * 60_000
const CODE_MAX_ATTEMPTS = 5

/** 登录态有效期：Go 版 CreateToken(30*24h)。 */
const TOKEN_TTL_MS = 30 * 24 * 3600_000

interface DeskUser {
  id: number
  phone: string
  is_admin: number
  balance: number
  agreement: number
  created_at: string
}

/** 对外用户对象：is_admin/agreement 转 bool，与 Go 的 User JSON 一致。 */
function userJson(u: DeskUser) {
  return {
    id: u.id,
    phone: u.phone,
    is_admin: u.is_admin === 1,
    balance: u.balance,
    agreement: u.agreement === 1,
    created_at: u.created_at
  }
}

const DESK_USER_COLS = `id, phone, is_admin, balance, agreement, created_at`

async function userById(c: C, id: number): Promise<DeskUser | null> {
  return await c.env.DB.prepare(`SELECT ${DESK_USER_COLS} FROM desk_users WHERE id = ?`).bind(id).first<DeskUser>()
}

/** 库里还没有任何桌面端账号时，第一个注册的手机号就是超管（不内置种子账号）。 */
async function isFirstUser(c: C): Promise<boolean> {
  const r = await c.env.DB.prepare(`SELECT COUNT(*) AS n FROM desk_users`).first<{ n: number }>()
  return (r?.n ?? 0) === 0
}

async function userByPhone(c: C, phone: string): Promise<DeskUser | null> {
  return await c.env.DB.prepare(`SELECT ${DESK_USER_COLS} FROM desk_users WHERE phone = ?`).bind(phone).first<DeskUser>()
}

/**
 * 取当前登录的桌面端用户。401 必须原样是 401——前端据此清 token，
 * 换成别的码会让过期会话卡在「已登录但拉不到资料」。
 */
async function current(c: C): Promise<DeskUser> {
  const plain = (c.req.header('Authorization') || '').replace(/^Bearer\s+/i, '').trim()
  if (!plain) throw fail(401, '未登录或会话已过期')
  const tok = await c.env.DB.prepare(`SELECT id, user_id, expires_at FROM desk_tokens WHERE token_hash = ?`)
    .bind(await sha256Hex(plain))
    .first<{ id: number; user_id: number; expires_at: string }>()
  if (!tok) throw fail(401, '未登录或会话已过期')
  if (new Date(tok.expires_at).getTime() < Date.now()) {
    await c.env.DB.prepare(`DELETE FROM desk_tokens WHERE id = ?`).bind(tok.id).run()
    throw fail(401, '未登录或会话已过期')
  }
  const u = await userById(c, tok.user_id)
  if (!u) throw fail(401, '未登录或会话已过期')
  return u
}

/** 本层所有业务错误都从这转成 {error}；500 之外的可读中文原因直接 fail(status, msg)。 */
const failJson = (c: C, e: unknown): Response => {
  if (e instanceof ApiError) return c.json({ error: e.message }, e.status as never)
  console.error('[desk]', e)
  return c.json({ error: '服务端内部错误' }, 500)
}

const wrap = (fn: (c: C) => Promise<Response>) => async (c: C) => {
  try {
    return await fn(c)
  } catch (e) {
    return failJson(c, e)
  }
}

/**
 * 与邮箱版重合的路径按「请求体形状」分流：带 phone 的归本层，否则 next() 交回 admin。
 * 不能直接抢——后台前端（web/）的登录/发码/注销都走同一批路径。
 * Hono 缓存已解析的 body，所以这里读过之后 admin 再读一次是安全的。
 */
const byPhone = (fn: (c: C, req: Record<string, unknown>) => Promise<Response>): MiddlewareHandler<AppEnv> => {
  return async (c, next) => {
    let req: Record<string, unknown> | null = null
    try {
      req = (await c.req.json()) as Record<string, unknown>
    } catch {
      req = null
    }
    if (typeof req?.phone !== 'string' || !req.phone.trim()) return await next()
    try {
      return await fn(c, req)
    } catch (e) {
      return failJson(c, e)
    }
  }
}

async function setting(c: C, key: string, def = ''): Promise<string> {
  const row = await c.env.DB.prepare(`SELECT value FROM settings WHERE key = ?`).bind(key).first<{ value: string }>()
  return row?.value || def
}

/** 测试模式（默认开）：验证码固定 111111 并随响应回显。未接短信通道时只有这条可用。 */
const testMode = async (c: C): Promise<boolean> => (await setting(c, 'desk_test_mode', '1')) === '1'

/** 与 Go 的 validPhone 同规则：纯数字、非空、≤20 位（前端另按 4~20 位先筛一道）。 */
function validPhone(p: string): boolean {
  const v = p.trim()
  return v !== '' && v.length <= 20 && /^\d+$/.test(v)
}

/** 「今天」按 TZ_HOURS 的日界，与 Go 版 time.Now().Format(2006-01-02) 的本地日界对齐。 */
function today(c: C): string {
  return new Date(Date.now() + tzHours(c.env) * 3600_000).toISOString().slice(0, 10)
}

async function issueCode(c: C, phone: string): Promise<string> {
  const code = (await testMode(c)) ? '111111' : String(Math.floor(Math.random() * 1_000_000)).padStart(6, '0')
  const now = rfc()
  await c.env.DB.prepare(
    `INSERT INTO desk_codes (phone, code, attempts, expires_at, created_at) VALUES(?, ?, 0, ?, ?)
     ON CONFLICT(phone) DO UPDATE SET code=excluded.code, attempts=0, expires_at=excluded.expires_at, created_at=excluded.created_at`
  )
    .bind(phone, code, new Date(Date.now() + CODE_TTL_MS).toISOString(), now)
    .run()
  return code
}

/** 校验并消费验证码（一次性：成功后删除该行）。 */
async function verifyCode(c: C, phone: string, code: string): Promise<void> {
  const row = await c.env.DB.prepare(`SELECT code, attempts, expires_at FROM desk_codes WHERE phone = ?`)
    .bind(phone)
    .first<{ code: string; attempts: number; expires_at: string }>()
  const bad = fail(400, '验证码错误或已过期')
  if (!row) throw bad
  if (new Date(row.expires_at).getTime() < Date.now()) {
    await c.env.DB.prepare(`DELETE FROM desk_codes WHERE phone = ?`).bind(phone).run()
    throw bad
  }
  if (row.code !== code) {
    if (row.attempts + 1 >= CODE_MAX_ATTEMPTS) {
      await c.env.DB.prepare(`DELETE FROM desk_codes WHERE phone = ?`).bind(phone).run()
    } else {
      await c.env.DB.prepare(`UPDATE desk_codes SET attempts = attempts + 1 WHERE phone = ?`).bind(phone).run()
    }
    throw bad
  }
  await c.env.DB.prepare(`DELETE FROM desk_codes WHERE phone = ?`).bind(phone).run()
}

/**
 * 到期清扫：把过期获得行的 remaining 归零并从余额里扣掉。
 * 余额恒等于「未过期获得行 remaining 之和」，Go 版每次读路径都先跑它。
 */
async function sweepExpired(c: C, userID: number): Promise<void> {
  const now = rfc()
  const agg = await c.env.DB.prepare(
    `SELECT COALESCE(SUM(remaining), 0) AS lost FROM desk_credit_records
     WHERE user_id = ? AND expires_at != '' AND expires_at < ? AND remaining > 0`
  )
    .bind(userID, now)
    .first<{ lost: number }>()
  if (!agg || agg.lost <= 0) return
  await c.env.DB.batch([
    c.env.DB.prepare(
      `UPDATE desk_credit_records SET remaining = 0
       WHERE user_id = ? AND expires_at != '' AND expires_at < ? AND remaining > 0`
    ).bind(userID, now),
    c.env.DB.prepare(`UPDATE desk_users SET balance = balance - ? WHERE id = ?`).bind(agg.lost, userID)
  ])
}

/**
 * 增减积分并写流水，返回新余额。扣减按 FEFO：最快到期的先用，永久积分（expires_at 空）最后用。
 * 余额不足抛 400「积分不足」，由调用方决定映射成什么状态码（上报用量那条要 402）。
 */
async function addCredit(c: C, userID: number, delta: number, reason: string, model: string, expiresAt: string): Promise<number> {
  await sweepExpired(c, userID)
  const now = rfc()
  if (delta >= 0) {
    await c.env.DB.batch([
      c.env.DB.prepare(`UPDATE desk_users SET balance = balance + ? WHERE id = ?`).bind(delta, userID),
      c.env.DB.prepare(
        `INSERT INTO desk_credit_records (user_id, amount, reason, model, created_at, expires_at, remaining)
         VALUES(?, ?, ?, ?, ?, ?, ?)`
      ).bind(userID, delta, reason, model, now, expiresAt, delta)
    ])
    const u = await userById(c, userID)
    return u?.balance ?? 0
  }
  let need = -delta
  const { results: grants } = await c.env.DB.prepare(
    `SELECT id, remaining FROM desk_credit_records
     WHERE user_id = ? AND remaining > 0 AND (expires_at = '' OR expires_at >= ?)
     ORDER BY (expires_at = ''), expires_at ASC`
  )
    .bind(userID, now)
    .all<{ id: number; remaining: number }>()
  const takes: { id: number; use: number }[] = []
  for (const g of grants) {
    if (need <= 0) break
    const use = Math.min(g.remaining, need)
    takes.push({ id: g.id, use })
    need -= use
  }
  if (need > 0) throw fail(400, '积分不足')
  const stmts = takes.map((t) => c.env.DB.prepare(`UPDATE desk_credit_records SET remaining = remaining - ? WHERE id = ?`).bind(t.use, t.id))
  stmts.push(
    c.env.DB.prepare(`UPDATE desk_users SET balance = balance - ? WHERE id = ?`).bind(-delta, userID),
    c.env.DB.prepare(
      `INSERT INTO desk_credit_records (user_id, amount, reason, model, created_at, expires_at, remaining)
       VALUES(?, ?, ?, ?, ?, '', 0)`
    ).bind(userID, delta, reason, model, now)
  )
  await c.env.DB.batch(stmts)
  const u = await userById(c, userID)
  return u?.balance ?? 0
}

/** 该模型应扣多少积分：限免与后台未配置的模型不计费（Go 的 costOf 同规则）。 */
async function creditsOf(c: C, model: string, input: number, output: number): Promise<number> {
  const m = await c.env.DB.prepare(`SELECT ratio, free FROM ai_models WHERE name = ?`)
    .bind(model)
    .first<{ ratio: number; free: number }>()
  if (!m || m.free === 1) return 0
  return costOf(m.ratio, input + output)
}

/** 分页参数：page 从 1 起，page_size 默认 20、上限 100（Go 的 pageReq）。 */
function pageReq(c: C): { page: number; size: number } {
  let page = Number(c.req.query('page'))
  if (!Number.isFinite(page) || page < 1) page = 1
  let size = Number(c.req.query('page_size'))
  if (!Number.isFinite(size) || size <= 0) size = 20
  if (size > 100) size = 100
  return { page, size }
}

interface CreditRow {
  id: number
  user_id: number
  amount: number
  reason: string
  model: string
  created_at: string
  expires_at: string
  remaining: number
}

/** Go 的 CreditRecord 带 omitempty：model/expires_at 为空时整个键不出现。 */
function creditJson(r: CreditRow): Record<string, unknown> {
  return {
    id: r.id,
    user_id: r.user_id,
    amount: r.amount,
    reason: r.reason,
    created_at: r.created_at,
    remaining: r.remaining,
    ...(r.model ? { model: r.model } : {}),
    ...(r.expires_at ? { expires_at: r.expires_at } : {})
  }
}

async function creditPage(c: C, userID: number, kind: string, page: number, size: number) {
  let where = 'user_id = ?'
  if (kind === 'spent') where += ' AND amount < 0'
  if (kind === 'got') where += ' AND amount > 0'
  const total = (
    await c.env.DB.prepare(`SELECT COUNT(*) AS n FROM desk_credit_records WHERE ${where}`).bind(userID).first<{ n: number }>()
  )?.n ?? 0
  const { results } = await c.env.DB.prepare(
    `SELECT id, user_id, amount, reason, model, created_at, expires_at, remaining FROM desk_credit_records
     WHERE ${where} ORDER BY id DESC LIMIT ? OFFSET ?`
  )
    .bind(userID, size, (page - 1) * size)
    .all<CreditRow>()
  return { rows: results.map(creditJson), total }
}

interface UsageRow {
  id: number
  user_id: number
  model: string
  input_tokens: number
  output_tokens: number
  credits: number
  status: string
  error: string
  created_at: string
}

function usageJson(r: UsageRow): Record<string, unknown> {
  return {
    id: r.id,
    user_id: r.user_id,
    model: r.model,
    input_tokens: r.input_tokens,
    output_tokens: r.output_tokens,
    credits: r.credits,
    status: r.status,
    created_at: r.created_at,
    ...(r.error ? { error: r.error } : {})
  }
}

async function logUsage(c: C, userID: number, model: string, input: number, output: number, credits: number, status: string, error: string) {
  await c.env.DB.prepare(
    `INSERT INTO desk_usage_logs (user_id, model, input_tokens, output_tokens, credits, status, error, created_at)
     VALUES(?, ?, ?, ?, ?, ?, ?, ?)`
  )
    .bind(userID, model, input, output, credits, status, error, rfc())
    .run()
}

export const desk = new Hono<AppEnv>()

/** GET /api/state — 桌面端探活就是打这个；Go 版返回 base_url/test_mode/now。 */
desk.get('/state', wrap(async (c) => c.json({ base_url: baseURL(c, c.env), test_mode: await testMode(c), now: rfc() })))

/** GET /api/site — 登录页的站点标题/页脚/协议文案。 */
desk.get('/site', wrap(async (c) =>
  c.json({
    title: await setting(c, 'site_title', ''),
    footer: await setting(c, 'site_footer', ''),
    agreement: await setting(c, 'site_agreement', ''),
    test_mode: await testMode(c)
  })
))

/** POST /api/auth/send-code — 体带 phone 归本层（手机号验证码），否则交回邮箱版。 */
desk.post('/auth/send-code', byPhone(async (c, req) => {
  const phone = String(req.phone).trim()
  if (!validPhone(phone)) throw fail(400, '请输入正确手机号')
  const code = await issueCode(c, phone)
  const t = await testMode(c)
  return c.json({
    ok: true,
    test_mode: t,
    ...(t ? { code, hint: `测试模式：验证码 ${code}（正式开通短信后不会回显）` } : {})
  })
}))

/** POST /api/auth/login — 手机号+验证码，未注册自动注册；发 30 天 token。 */
desk.post('/auth/login', byPhone(async (c, req) => {
  const phone = String(req.phone).trim()
  const code = typeof req.code === 'string' ? req.code.trim() : ''
  if (!validPhone(phone) || !code) throw fail(400, '请输入手机号与验证码')
  if (req.agreement !== true) throw fail(400, '请先勾选并同意用户协议')
  await verifyCode(c, phone, code)
  let u = await userByPhone(c, phone)
  let isNew = false
  if (!u) {
    // 不内置管理员账号：库里第一个注册的手机号就是超管，后续注册一律 member。
    const first = await isFirstUser(c)
    await c.env.DB.prepare(
      `INSERT INTO desk_users (phone, is_admin, balance, agreement, created_at) VALUES(?, ?, 0, 1, ?)`
    )
      .bind(phone, first ? 1 : 0, rfc())
      .run()
    const fresh = await userByPhone(c, phone)
    if (!fresh) throw fail(500, '账号创建失败')
    u = fresh
    isNew = true
  } else {
    await c.env.DB.prepare(`UPDATE desk_users SET agreement = 1 WHERE id = ?`).bind(u.id).run()
    u = { ...u, agreement: 1 }
  }
  const plain = randHex(24)
  await c.env.DB.prepare(`INSERT INTO desk_tokens (user_id, token_hash, expires_at, created_at) VALUES(?, ?, ?, ?)`)
    .bind(u.id, await sha256Hex(plain), new Date(Date.now() + TOKEN_TTL_MS).toISOString(), rfc())
    .run()
  return c.json({ token: plain, user: userJson(u), is_new: isNew })
}))

/**
 * POST /api/auth/logout — 只注销桌面端 token；不是本层的 token 就 next() 交回去，
 * 后台前端（web/）也在调这条路径，不能把它按桌面端的 401 语义怼回去。
 */
desk.post('/auth/logout', async (c, next) => {
  const plain = (c.req.header('Authorization') || '').replace(/^Bearer\s+/i, '').trim()
  if (!plain) return await next()
  const tok = await c.env.DB.prepare(`SELECT id FROM desk_tokens WHERE token_hash = ?`)
    .bind(await sha256Hex(plain))
    .first<{ id: number }>()
  if (!tok) return await next()
  await c.env.DB.prepare(`DELETE FROM desk_tokens WHERE id = ?`).bind(tok.id).run()
  return c.json({ ok: true })
})

/** GET /api/me — 账号信息 + 积分流水分页（kind=gain|spend，空=全部）。 */
desk.get('/me', wrap(async (c) => {
  let u = await current(c)
  await sweepExpired(c, u.id)
  const fresh = (await userById(c, u.id)) ?? u
  u = fresh
  const kindRaw = c.req.query('kind') || ''
  const kind = kindRaw === 'gain' ? 'got' : kindRaw === 'spend' ? 'spent' : kindRaw
  const { page, size } = pageReq(c)
  const { rows, total } = await creditPage(c, u.id, kind, page, size)
  return c.json({ user: userJson(u), records: rows, total, page, page_size: size })
}))

/** POST /api/me/phone — 换绑手机号：新号先验码，且不能被他人占用。 */
desk.post('/me/phone', wrap(async (c) => {
  const u = await current(c)
  const req = (await c.req.json().catch(() => null)) as { phone?: string; code?: string } | null
  const phone = (req?.phone ?? '').trim()
  const code = (req?.code ?? '').trim()
  if (!validPhone(phone) || !code) throw fail(400, '请输入新手机号与验证码')
  await verifyCode(c, phone, code)
  const taken = await userByPhone(c, phone)
  if (taken && taken.id !== u.id) throw fail(400, '该手机号已被使用')
  await c.env.DB.prepare(`UPDATE desk_users SET phone = ? WHERE id = ?`).bind(phone, u.id).run()
  const fresh = await userById(c, u.id)
  if (!fresh) throw fail(500, '账号不存在')
  return c.json({ user: userJson(fresh) })
}))

/**
 * POST /api/claim — 每日积分领取（桌面端登录后自动调，同一天幂等）。
 * 开关与额度走 settings：daily_credit_enabled='1' / daily_credit_amount / daily_credit_days（0=不限）。
 */
desk.post('/claim', wrap(async (c) => {
  const u = await current(c)
  await sweepExpired(c, u.id)
  const latest = (await userById(c, u.id)) ?? u
  const enabled = (await setting(c, 'daily_credit_enabled')) === '1'
  const amount = Number(await setting(c, 'daily_credit_amount')) || 0
  const days = Number(await setting(c, 'daily_credit_days')) || 0
  if (!enabled || amount <= 0) return c.json({ claimed: false, amount: 0, balance: latest.balance })

  const date = today(c)
  if (days > 0) {
    const last = await c.env.DB.prepare(`SELECT claim_date FROM desk_daily_claims WHERE user_id = ? ORDER BY claim_date DESC LIMIT 1`)
      .bind(u.id)
      .first<{ claim_date: string }>()
    if (last && new Date(new Date(last.claim_date).getTime() + days * 86400_000) > new Date()) {
      return c.json({ claimed: false, amount: 0, balance: latest.balance })
    }
  }
  const hit = await c.env.DB.prepare(`SELECT 1 AS x FROM desk_daily_claims WHERE user_id = ? AND claim_date = ?`)
    .bind(u.id, date)
    .first<{ x: number }>()
  if (hit) return c.json({ claimed: false, amount: 0, balance: latest.balance })

  const expiresAt = days > 0 ? new Date(Date.now() + days * 86400_000).toISOString() : ''
  await c.env.DB.batch([
    c.env.DB.prepare(`INSERT INTO desk_daily_claims (user_id, claim_date) VALUES(?, ?)`).bind(u.id, date),
    c.env.DB.prepare(`UPDATE desk_users SET balance = balance + ? WHERE id = ?`).bind(amount, u.id),
    c.env.DB.prepare(
      `INSERT INTO desk_credit_records (user_id, amount, reason, model, created_at, expires_at, remaining) VALUES(?, ?, ?, '', ?, ?, ?)`
    ).bind(u.id, amount, REASON_DAILY, rfc(), expiresAt, amount)
  ])
  const after = (await userById(c, u.id)) ?? latest
  return c.json({ claimed: true, amount, balance: after.balance })
}))

/** GET /api/models — 后台启用中的模型，喂给桌面端「内置模型」组（name→key、ratio→multiplier）。 */
desk.get('/models', wrap(async (c) => {
  await current(c)
  const { results } = await c.env.DB.prepare(
    `SELECT name AS key, builtin, ratio AS multiplier, free FROM ai_models WHERE enabled = 1 ORDER BY sort, id`
  ).all<{ key: string; builtin: number; multiplier: number; free: number }>()
  return c.json({ models: results.map((m) => ({ ...m, builtin: m.builtin === 1, free: m.free === 1 })) })
}))

/**
 * GET /api/usage — 使用量页：请求汇总 + 积分累计 + 分页明细。
 * list=ai(默认) 请求记录 | spent 消耗流水 | got 获取流水。
 */
desk.get('/usage', wrap(async (c) => {
  const u = await current(c)
  await sweepExpired(c, u.id)
  const sum = await c.env.DB.prepare(
    `SELECT COUNT(*) AS requests, COALESCE(SUM(input_tokens),0) AS input_tokens,
            COALESCE(SUM(output_tokens),0) AS output_tokens,
            COALESCE(SUM(CASE WHEN status = 'ok' THEN credits ELSE 0 END),0) AS credits
     FROM desk_usage_logs WHERE user_id = ?`
  )
    .bind(u.id)
    .first<{ requests: number; input_tokens: number; output_tokens: number; credits: number }>()
  const cred = await c.env.DB.prepare(
    `SELECT COALESCE(SUM(CASE WHEN amount > 0 THEN amount ELSE 0 END),0) AS got,
            COALESCE(SUM(CASE WHEN amount < 0 THEN -amount ELSE 0 END),0) AS spent
     FROM desk_credit_records WHERE user_id = ?`
  )
    .bind(u.id)
    .first<{ got: number; spent: number }>()
  const list = c.req.query('list') || 'ai'
  const { page, size } = pageReq(c)
  let items: unknown[] = []
  let total = 0
  if (list === 'spent' || list === 'got') {
    const r = await creditPage(c, u.id, list, page, size)
    items = r.rows
    total = r.total
  } else {
    total =
      (await c.env.DB.prepare(`SELECT COUNT(*) AS n FROM desk_usage_logs WHERE user_id = ?`).bind(u.id).first<{ n: number }>())?.n ?? 0
    const { results } = await c.env.DB.prepare(
      `SELECT id, user_id, model, input_tokens, output_tokens, credits, status, error, created_at
       FROM desk_usage_logs WHERE user_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`
    )
      .bind(u.id, size, (page - 1) * size)
      .all<UsageRow>()
    items = results.map(usageJson)
  }
  return c.json({
    summary: {
      requests: sum?.requests ?? 0,
      input_tokens: sum?.input_tokens ?? 0,
      output_tokens: sum?.output_tokens ?? 0,
      credits: sum?.credits ?? 0
    },
    credit: { got: cred?.got ?? 0, spent: cred?.spent ?? 0 },
    list,
    page,
    page_size: size,
    items,
    total
  })
}))

/**
 * POST /api/usage — 桌面端每轮问答后上报用量：按模型倍率扣分并留痕。
 * 上游失败（status=error）与限免/未配置模型都不计费；积分不足时记录以 insufficient 落库并回 402。
 */
desk.post('/usage', wrap(async (c) => {
  const u = await current(c)
  const req = (await c.req.json().catch(() => null)) as {
    model?: string
    input_tokens?: number
    output_tokens?: number
    status?: string
    error?: string
  } | null
  if (!req) throw fail(400, '参数错误')
  const model = (req.model ?? '').trim()
  const input = Math.max(0, Number(req.input_tokens) || 0)
  const output = Math.max(0, Number(req.output_tokens) || 0)
  const status = req.status || 'ok'
  const error = req.error ?? ''

  if (status === 'error') {
    await logUsage(c, u.id, model, input, output, 0, 'error', error)
    return c.json({ credits: 0, balance: u.balance })
  }
  const credits = await creditsOf(c, model, input, output)
  if (credits <= 0) {
    await logUsage(c, u.id, model, input, output, 0, 'ok', '')
    return c.json({ credits: 0, balance: u.balance })
  }
  try {
    const balance = await addCredit(c, u.id, -credits, REASON_AI, model, '')
    await logUsage(c, u.id, model, input, output, credits, 'ok', '')
    return c.json({ credits, balance })
  } catch (e) {
    if (e instanceof ApiError && e.message === '积分不足') {
      // 请求已经发生：留痕但不扣减，让会员和管理端都能看到这笔欠账（前端按 402 提示充值）
      await logUsage(c, u.id, model, input, output, credits, 'insufficient', '')
      throw fail(402, '积分不足')
    }
    throw e
  }
}))
