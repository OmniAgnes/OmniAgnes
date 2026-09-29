// omni 桌面版：Electron 壳。
// 无边框窗口（自绘标题栏）+ 拉起 omni --web --no-open 服务并加载。
//
// 用法：
//   开发：先编译 omni 并放到 webui/desktop/bin/（omni.exe / omni），或设置 OMNI_BIN 指向二进制
//        cd webui/desktop && npm install && npm start
//   打包：npm run dist（NSIS 安装包，内核从 bin/ 经 extraResources 打进 resources/bin）
//
// 可选环境变量：OMNI_BIN（二进制完整路径）、OMNI_CWD（omni 工作目录，默认用户主目录）。
const { app, BrowserWindow, Menu, dialog, ipcMain } = require("electron")
const { spawn } = require("node:child_process")
const { existsSync } = require("node:fs")
const http = require("node:http")
const path = require("node:path")

let win = null
let omni = null

function resolveOmniBin() {
  if (process.env.OMNI_BIN) return process.env.OMNI_BIN
  const exe = process.platform === "win32" ? "omni.exe" : "omni"
  if (app.isPackaged) {
    // electron-builder extraResources：resources/bin/omni(.exe)
    return path.join(process.resourcesPath, "bin", exe)
  }
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
    fail("找不到 omni 内核。请先 go build -o webui/desktop/bin/omni.exe . （或设置 OMNI_BIN）。")
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

// ---------- IPC：窗口控制与原生文件夹选择 ----------
ipcMain.on("omni:win", (_e, action) => {
  if (!win) return
  if (action === "minimize") win.minimize()
  else if (action === "maximize") {
    if (win.isMaximized()) win.unmaximize()
    else win.maximize()
  } else if (action === "close") win.close()
})

ipcMain.handle("omni:pick-folder", async () => {
  const opts = { properties: ["openDirectory", "createDirectory"] }
  const r = win ? await dialog.showOpenDialog(win, opts) : await dialog.showOpenDialog(opts)
  return r.canceled || r.filePaths.length === 0 ? null : r.filePaths[0]
})

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
    frame: false, // 去原生标题栏，用自绘 Titlebar（跟随主题）
    backgroundColor: "#0a0a0a",
    // 开发态窗口/任务栏图标（打包态直接用 exe 内置的 build/icon.ico）
    icon: existsSync(path.join(__dirname, "build", "icon.png"))
      ? path.join(__dirname, "build", "icon.png")
      : undefined,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
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
