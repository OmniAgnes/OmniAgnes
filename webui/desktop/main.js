// omni 桌面版：Electron 壳。
// 启动时拉起 omni 二进制的 --web --no-open 服务，等待端口就绪后用 BrowserWindow 加载。
//
// 用法：
//   1) 先编译 omni 并放到 webui/desktop/bin/（omni.exe / omni），或设置 OMNI_BIN 指向二进制
//   2) cd webui/desktop && npm install && npm start
//
// 可选环境变量：OMNI_BIN（二进制完整路径）、OMNI_CWD（omni 工作目录，默认用户主目录）。
const { app, BrowserWindow, Menu } = require("electron")
const { spawn } = require("node:child_process")
const { existsSync } = require("node:fs")
const http = require("node:http")
const path = require("node:path")

let win = null
let omni = null

function resolveOmniBin() {
  if (process.env.OMNI_BIN) return process.env.OMNI_BIN
  const exe = process.platform === "win32" ? "omni.exe" : "omni"
  const candidates = [
    path.join(__dirname, "bin", exe),
    path.join(__dirname, "..", "..", exe),
  ]
  for (const p of candidates) {
    if (existsSync(p)) return p
  }
  return null
}

// 等待 omni 的 HTTP 服务可访问
function waitFor(url, timeoutMs) {
  const started = Date.now()
  return new Promise((resolve, reject) => {
    const probe = () => {
      const req = http.get(url, (res) => {
        res.resume()
        if (res.statusCode && res.statusCode < 500) return resolve(url)
        retry()
      })
      req.on("error", retry)
      req.setTimeout(1500, () => {
        req.destroy()
        retry()
      })
    }
    const retry = () => {
      if (Date.now() - started > timeoutMs) return reject(new Error("omni 服务启动超时"))
      setTimeout(probe, 300)
    }
    probe()
  })
}

function startOmni() {
  const bin = resolveOmniBin()
  if (!bin) {
    fail("找不到 omni 二进制。请先 go build -o omni.exe . 并复制到 webui/desktop/bin/，或设置 OMNI_BIN。")
    return null
  }
  const cwd = process.env.OMNI_CWD || app.getPath("home")
  const child = spawn(bin, ["--web", "--no-open", "--cwd", cwd], {
    stdio: ["ignore", "pipe", "pipe"],
  })
  child.stderr.on("data", (d) => process.stderr.write(`[omni] ${d}`))
  child.on("exit", (code) => {
    if (!app.isQuittingByUser && code !== 0 && !win) {
      fail(`omni 退出（code=${code}）`)
    }
  })
  return child
}

function fail(msg) {
  const { dialog } = require("electron")
  dialog.showErrorBox("omni 桌面版", msg)
  app.quit()
}

async function createWindow() {
  omni = startOmni()
  if (!omni) return

  // 从 stdout 抓取服务地址：omni --web 会打印 "omni 工作台已启动: http://127.0.0.1:PORT"
  const url = await new Promise((resolve, reject) => {
    let buf = ""
    const timer = setTimeout(() => reject(new Error("读取服务地址超时")), 20000)
    omni.stdout.on("data", (d) => {
      buf += d.toString()
      const m = buf.match(/http:\/\/127\.0\.0\.1:\d+/)
      if (m) {
        clearTimeout(timer)
        resolve(m[0])
      }
    })
    omni.on("exit", () => {
      clearTimeout(timer)
      reject(new Error("omni 提前退出"))
    })
  })
  await waitFor(url, 10000)

  win = new BrowserWindow({
    width: 1280,
    height: 820,
    minWidth: 420,
    minHeight: 480,
    title: "omni",
    autoHideMenuBar: true,
    backgroundColor: "#0a0a0a",
    webPreferences: { contextIsolation: true, nodeIntegration: false },
  })
  Menu.setApplicationMenu(null)
  win.loadURL(url)
  win.on("closed", () => {
    win = null
    if (omni) omni.kill()
  })
}

app.whenReady().then(() => {
  createWindow().catch((e) => fail(e.message))
  app.on("activate", () => {
    if (!win) createWindow().catch((e) => fail(e.message))
  })
})

app.on("window-all-closed", () => {
  app.isQuittingByUser = true
  if (omni) omni.kill()
  app.quit()
})
