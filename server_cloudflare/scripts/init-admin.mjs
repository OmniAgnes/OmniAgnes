/**
 * 初始化超管账号（全新部署后跑一次）。
 *
 * 注册规则：users 表为空 = 第一个注册的账号自动成为 admin。
 * 没配邮件服务时验证码任意通过（免验证模式），脚本因此不需要真收码；
 * 配好 SMTP 后验证码是真校验，届时请直接在网页上注册（或先把邮箱换成能收码的）。
 * 超管的密码是「管理员兜底登录」用的（邮件还没配置时，密码是进后台配 SMTP 的唯一方式）；
 * 配好邮件、在后台右上角把邮箱换成真实邮箱后，日常登录请改用邮箱验证码。
 *
 * 用法：
 *   node scripts/init-admin.mjs <baseURL> [email] [password]
 *
 *   baseURL  必填，如 https://omni-server.xxx.workers.dev
 *   email    缺省 admin@omni.local（假域名收不到验证码，配好邮件后记得在后台右上角换成真实邮箱）
 *   password 缺省 Omni@2026（固定初始密码，上线后务必改成只有你知道的）
 *
 * 注意：后台当前没有「改密码」功能，密码丢了需要重建（先删 users 里那行再跑本脚本）。
 */
const [base = '', email = 'admin@omni.local', password = 'Omni@2026'] = process.argv.slice(2)
if (!base) {
  console.error('用法: node scripts/init-admin.mjs <baseURL> [email] [password]')
  process.exit(1)
}
const pwd = password || 'Omni@2026'

const res = await fetch(base.replace(/\/+$/, '') + '/api/auth/register', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ email, password: pwd, name: '超级管理员' })
})
const data = await res.json()

if (res.status === 409) {
  console.error(`该邮箱已注册（${data.message}）。超管已初始化过，无需再跑；忘了密码只能重建账号。`)
  process.exit(1)
}
if (!res.ok || !data.user) {
  console.error(`注册失败: ${JSON.stringify(data)}`)
  process.exit(1)
}

const me = await fetch(base.replace(/\/+$/, '') + '/api/auth/me', {
  headers: { Authorization: 'Bearer ' + data.token }
}).then((r) => r.json())

console.log('超管初始化成功')
console.log('登录页: ' + base.replace(/\/+$/, '') + '/#/ide/login')
console.log('邮箱:   ' + email)
console.log('密码:   ' + pwd)
console.log('角色:   ' + me.user.role + (me.user.role === 'admin' ? ' ✓' : ' ⚠ 不是管理员，检查是否为首个注册账号'))
console.log('下一步: 密码登录后台 → 邮件设置里配 SMTP → 右上角把邮箱换成真实邮箱 → 之后用邮箱验证码登录')
