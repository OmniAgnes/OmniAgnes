// 临时联调 mock：vite dev 代理 /api 到这里，仅供浏览器实测使用量面板
import http from "node:http"

function pad(n) {
  return String(n).padStart(2, "0")
}
function dateStr(offset) {
  const d = new Date()
  d.setDate(d.getDate() - offset)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

const models = ["GLM-5.3", "deepseek-v4"]
const records = []
for (let i = 9; i >= 0; i--) {
  const date = dateStr(i)
  for (const [mi, model] of models.entries()) {
    // 后天留一天 0 数据，验证 tooltip 过滤
    const base = i === 4 ? 0 : 12000 + ((i * 7919 + mi * 104729) % 90000)
    records.push({ date, model, input: base, output: Math.round(base * 0.4) })
  }
}

http
  .createServer((req, res) => {
    res.setHeader("Content-Type", "application/json")
    const chunks = []
    req.on("data", (c) => chunks.push(c))
    req.on("end", () => {
      const body = req.url === "/api/usage" ? JSON.stringify({ records }) : "{}"
      res.end(body)
    })
  })
  .listen(18742, "127.0.0.1", () => console.log("mock on 18742"))
