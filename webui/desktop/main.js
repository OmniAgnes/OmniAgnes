// omni 桌面版：Electron 壳。
// 无边框窗口（自绘标题栏）+ 拉起 omni --web --no-open 服务并加载。
//
// 用法：
//   开发：先编译 omni 并放到 webui/desktop/bin/（omni.exe / omni），或设置 OMNI_BIN 指向二进制
//        cd webui/desktop && npm install && npm start
//   打包：npm run dist（NSIS 安装包，内核从 bin/ 经 extraResources 打进 resources/bin）
//
// 可选环境变量：OMNI_BIN（二进制完整路径）、OMNI_CWD（omni 工作目录，默认用户主目录）。
const { app, BrowserWindow, Menu, dialog, ipcMain, shell, WebContentsView } = require("electron")
const { spawn } = require("node:child_process")
const { existsSync } = require("node:fs")
const http = require("node:http")
const path = require("node:path")

// ---------- 内置浏览器 ----------
// WebContentsView 悬浮在主窗口内容之上，位置大小由渲染层 BrowserPane 上报的矩形决定。
// 操作链：内核 browser 工具 → /api/browser/next（前端长轮询）→ preload IPC → 这里执行。
// 用户实时看得见 AI 在点什么；AI 通过 extract（元素+坐标+正文）/eval「看」页面。
let browserView = null
let browserBounds = { x: 0, y: 0, width: 0, height: 0 }
let browserVisible = false

function ensureBrowserView() {
  if (!win) return null
  if (!browserView) {
    browserView = new WebContentsView({
      webPreferences: { sandbox: true, contextIsolation: true, nodeIntegration: false },
    })
    // 内链外跳、window.open 都留在本视图里
    const wc = browserView.webContents
    wc.setWindowOpenHandler(({ url }) => {
      if (/^https?:/i.test(url)) wc.loadURL(url)
      return { action: "deny" }
    })
    win.contentView.addChildView(browserView)
  }
  applyBrowserBounds()
  return browserView
}

function applyBrowserBounds() {
  if (!browserView) return
  const b = browserBounds
  browserView.setBounds(
    browserVisible
      ? { x: Math.round(b.x), y: Math.round(b.y), width: Math.max(0, Math.round(b.width)), height: Math.max(0, Math.round(b.height)) }
      : { x: 0, y: 0, width: 0, height: 0 },
  )
}

function destroyBrowserView() {
  if (!browserView) return
  if (win) {
    try { win.contentView.removeChildView(browserView) } catch { /* 已移除 */ }
  }
  try { browserView.webContents.close() } catch { /* 已销毁 */ }
  browserView = null
  browserVisible = false
}

function waitLoad(wc) {
  return new Promise((resolve) => {
    if (!wc.isLoading()) return resolve()
    const done = () => { clearTimeout(timer); wc.removeListener("did-stop-loading", done); resolve() }
    const timer = setTimeout(done, 8000)
    wc.once("did-stop-loading", done)
  })
}

async function navigate(wc, url) {
  if (wc.getURL() === url && !url.startsWith("about:")) wc.reload()
  else await wc.loadURL(url)
  await waitLoad(wc)
  return `已打开：${wc.getURL()}（${wc.getTitle() || "无标题"}）`
}

function safeStringify(v) {
  if (typeof v === "string") return v
  if (v && v.__error) return "JS 错误: " + v.__error
  try { return JSON.stringify(v, null, 2) ?? String(v) } catch { return String(v) }
}

// 提取页面：可见交互元素（视口中心坐标，可直接喂 click）+ 标题 + 正文。
const EXTRACT_SCRIPT = `(() => {
  const vis = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width < 4 || r.height < 4) return false;
    if (r.bottom < 0 || r.top > innerHeight || r.right < 0 || r.left > innerWidth) return false;
    const st = getComputedStyle(el);
    return st.visibility !== 'hidden' && st.display !== 'none' && +st.opacity > 0.05;
  };
  const sel = 'a,button,input,select,textarea,[role=button],[role=link],[role=tab],[role=menuitem],[onclick],[tabindex]:not([tabindex="-1"]),img[alt]';
  const out = [];
  const seen = new Set();
  document.querySelectorAll(sel).forEach((el) => {
    if (!vis(el)) return;
    const r = el.getBoundingClientRect();
    const tag = el.tagName.toLowerCase();
    const type = (el.getAttribute('type') || '').toLowerCase();
    if (tag === 'input' && type === 'hidden') return;
    const label = (el.innerText || el.value || el.getAttribute('aria-label') || el.getAttribute('placeholder') || el.getAttribute('alt') || el.textContent || '').trim().replace(/\\s+/g, ' ').slice(0, 60);
    const key = tag + '|' + Math.round(r.x) + ',' + Math.round(r.y) + '|' + label;
    if (seen.has(key)) return; seen.add(key);
    const kind = tag === 'input' ? (type || 'text') : (el.getAttribute('role') || tag);
    out.push({ k: kind, t: label, x: Math.round(r.x + r.width / 2), y: Math.round(r.y + r.height / 2) });
  });
  const heads = [...document.querySelectorAll('h1,h2,h3')].filter(vis).slice(0, 12).map((h) => h.innerText.trim().replace(/\\s+/g, ' ').slice(0, 80)).filter(Boolean);
  const body = (document.body ? document.body.innerText : '').replace(/\\n{3,}/g, '\\n\\n').slice(0, 4000);
  return JSON.stringify({ url: location.href, title: document.title, heads, scrollY: Math.round(scrollY), els: out.slice(0, 120), body });
})()`

async function extractPage(wc) {
  const raw = await wc.executeJavaScript(EXTRACT_SCRIPT, true)
  let d
  try { d = JSON.parse(raw) } catch { return "（页面内容解析失败）" }
  const lines = [`URL: ${d.url}`, `标题: ${d.title}`]
  if (d.heads && d.heads.length) lines.push("页面标题栏: " + d.heads.join(" / "))
  lines.push(`滚动位置: ${d.scrollY}`)
  if (d.els && d.els.length) {
    lines.push(`\n可交互元素 ${d.els.length} 个（x,y=视口中心坐标，配合 click 使用）:`)
    for (const e of d.els) lines.push(`  [${e.k}] "${e.t}" @(${e.x},${e.y})`)
  }
  if (d.body) lines.push("\n正文:\n" + d.body)
  return lines.join("\n")
}

// 执行一次浏览器操作；op 语义与 internal/webui/browser.go 的工具描述一一对应。
async function runBrowserOp(op, args) {
  if (op === "close") { destroyBrowserView(); return "已关闭内置浏览器" }
  const v = ensureBrowserView()
  if (!v) throw new Error("主窗口不存在")
  const wc = v.webContents
  const num = (k) => (typeof args?.[k] === "number" ? args[k] : 0)

  switch (op) {
    case "open":
    case "navigate": {
      let url = String(args?.url || "").trim()
      if (!url) throw new Error("缺少 url")
      if (!/^[a-z][a-z0-9+.-]*:/i.test(url)) url = "https://" + url
      return navigate(wc, url)
    }
    case "reload":
      wc.reload(); await waitLoad(wc); return `已重新加载：${wc.getURL()}`
    case "back":
      if (wc.navigationHistory.canGoBack()) { wc.navigationHistory.goBack(); await waitLoad(wc); return `已后退：${wc.getURL()}` }
      return "没有可后退的历史"
    case "forward":
      if (wc.navigationHistory.canGoForward()) { wc.navigationHistory.goForward(); await waitLoad(wc); return `已前进：${wc.getURL()}` }
      return "没有可前进的历史"
    case "click": {
      const x = num("x"), y = num("y")
      wc.focus()
      const opts = { x, y, clickCount: args?.doubleClick ? 2 : 1 }
      await wc.sendInputEvent({ type: "mouseDown", ...opts, button: "left" })
      await wc.sendInputEvent({ type: "mouseUp", ...opts, button: "left" })
      await new Promise((r) => setTimeout(r, 300))
      return `点击 (${Math.round(x)}, ${Math.round(y)})`
    }
    case "type": {
      const text = String(args?.text ?? "")
      for (const ch of text) await wc.sendInputEvent({ type: "char", keyCode: ch })
      return `输入 ${text.length} 个字符`
    }
    case "key": {
      const key = String(args?.key ?? "")
      const map = { Enter: "Return", Escape: "Escape", " ": "Space", Delete: "Delete" }
      const keyCode = map[key] || key
      await wc.sendInputEvent({ type: "keyDown", keyCode })
      await wc.sendInputEvent({ type: "keyUp", keyCode })
      return `按键 ${key}`
    }
    case "scroll": {
      const amount = num("amount") || 300
      await wc.sendInputEvent({ type: "mouseWheel", x: num("x") || browserBounds.width / 2, y: num("y") || browserBounds.height / 2, deltaX: 0, deltaY: amount })
      await new Promise((r) => setTimeout(r, 250))
      return `滚动 ${amount}px`
    }
    case "wait": {
      const ms = Math.min(10000, Math.max(0, num("ms") || 1000))
      await new Promise((r) => setTimeout(r, ms))
      return `等待 ${ms}ms`
    }
    case "eval": {
      const code = String(args?.code ?? "")
      if (!code) throw new Error("缺少 code")
      const result = await wc.executeJavaScript(
        `(async () => { try { return await (${code}) } catch (e) { return { __error: String((e && e.message) || e) } } })()`,
        true,
      )
      return safeStringify(result)
    }
    case "extract":
      return extractPage(wc)
    case "state": {
      const st = await wc.executeJavaScript(`JSON.stringify({title: document.title, ready: document.readyState})`, true)
      let o = {}
      try { o = JSON.parse(st || "{}") } catch { /* 忽略 */ }
      return `URL: ${wc.getURL()}\n标题: ${o.title || ""}\n状态: ${wc.isLoading() ? "加载中" : o.ready || "idle"}\n面板: ${browserVisible ? "显示中" : "隐藏"}`
    }
    case "show":
      browserVisible = true; applyBrowserBounds(); return "浏览器面板已显示"
    case "hide":
      browserVisible = false; applyBrowserBounds(); return "浏览器面板已隐藏（视图保留，可继续操作）"
    default:
      throw new Error("未知操作: " + op)
  }
}

// 渲染兜底（本机黑屏问题 2026-09-30 结论，全部叠加才生效）：
// 1) 本机 GPU 进程反复崩溃（0xC0000005 /「GPU process isn't usable. Goodbye.」）。
//    仅 app.disableHardwareAcceleration()（≈ --disable-gpu）不够——Chromium 仍会 fork
//    GPU 进程跑软件合成并崩。必须再加 --disable-gpu-sandbox 才能止崩。
//    想开回硬件渲染：设环境变量 OMNI_GPU=1。
// 2) CalculateNativeWinOcclusion 在「部分 Windows 驱动 + frame:false 无边框窗口」组合下
//    误判窗口被遮挡 → 停止绘制 → 整窗纯黑（页面其实已加载，浏览器打开同一地址正常）。
//    这是 Electron 黑窗经典 bug（electron#39798），禁用该特性即可。
// 3) 系统代理可能干扰 localhost → 强制直连。
if (process.env.OMNI_GPU !== "1") {
  app.disableHardwareAcceleration()
  app.commandLine.appendSwitch("disable-gpu")
  app.commandLine.appendSwitch("disable-gpu-sandbox")
}
// 渲染进程崩溃 0x80000003（STATUS_BREAKPOINT）：Windows 上 Chromium 渲染器沙箱
// 与软件渲染组合易触发（尤其受限环境），关掉整体沙箱最稳。
app.commandLine.appendSwitch("no-sandbox")
// GPU 进程崩时改用「进程内软件合成」，避免渲染器反复重建
app.commandLine.appendSwitch("disable-gpu-compositing")
app.commandLine.appendSwitch("disable-features", "CalculateNativeWinOcclusion")
app.commandLine.appendSwitch("no-proxy-server")

let win = null
let omni = null

// 开发态（npx electron .）的窗口/任务栏图标：Electron 默认用自己的图标，
// 任务栏显示成 Electron。显式指向 build/ 下的 omni 图标。
// Windows 任务栏需要 .ico（PNG 在部分 Windows 版本不生效），macOS 用 PNG。
function appIcon(wantIco = false) {
  const dir = path.join(__dirname, "build")
  const file = wantIco && process.platform === "win32" ? "icon.ico" : "icon.png"
  const p = path.join(dir, file)
  return existsSync(p) ? p : undefined
}

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

// ---------- IPC：内置浏览器 ----------
ipcMain.handle("omni:browser-op", async (_e, { op, args }) => {
  try {
    return { ok: true, output: await runBrowserOp(op, args) }
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : String(err) }
  }
})

// 渲染层上报浏览器占位区（视口坐标）。WebContentsView 是原生悬浮层，必须跟面板对齐。
ipcMain.on("omni:browser-bounds", (_e, b) => {
  if (!b || typeof b.width !== "number") return
  browserBounds = b
  applyBrowserBounds()
})

ipcMain.on("omni:browser-visible", (_e, visible) => {
  browserVisible = Boolean(visible)
  applyBrowserBounds()
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
    backgroundColor: "#ffffff", // 主题默认 light，黑底会在加载瞬间表现为黑窗
    // 开发态窗口/任务栏图标（打包态直接用 exe 内置的 build/icon.ico）
    icon: appIcon(),
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })
  Menu.setApplicationMenu(null)

  // 任务栏图标与标题：开发态（npx electron .）默认是 Electron 的图标 + "Electron" 标题，
  // 因为进程名/默认窗口标题都来自 electron 本身。显式覆盖成 omni 的图标和标题。
  if (process.platform === "win32" && win.setIcon && !app.isPackaged) {
    const ico = appIcon(true)
    if (ico) win.setIcon(ico)
  }
  win.on("page-title-updated", (e) => {
    // 屏蔽网页 <title> 覆盖窗口标题（否则任务栏文字会变成 "omni 工作台"甚至站点名）
    e.preventDefault()
    if (win && win.getTitle() !== "omni") win.setTitle("omni")
  })
  win.setTitle("omni")

  // 主窗口导航防护：工作台是单页应用，任何指向外部站点的跳转/新窗口都不该把整个界面顶掉。
  // 聊天里的链接由前端拦截后交给内置浏览器打开（见 lib/browser.ts）。

  // 空白排障：把渲染层失败原因打进主进程 stdout（日志见 /tmp/omni-electron.log）
  const wc = win.webContents

  wc.setWindowOpenHandler(({ url: target }) => {
    if (/^https?:/i.test(target)) shell.openExternal(target)
    return { action: "deny" }
  })
  wc.on("will-navigate", (event, navUrl) => {
    if (navUrl === wc.getURL() || navUrl.startsWith(url)) return // 同源刷新/前进后退，放行
    event.preventDefault()
    if (/^https?:/i.test(navUrl)) shell.openExternal(navUrl)
  })

  wc.on("did-start-loading", () => console.log("[diag] did-start-loading"))
  wc.on("did-finish-load", () => console.log("[diag] did-finish-load"))
  wc.on("did-fail-load", (_e, code, desc, validatedURL) =>
    console.log(`[diag] did-fail-load code=${code} desc=${desc} url=${validatedURL}`))
  wc.on("render-process-gone", (_e, details) =>
    console.log(`[diag] render-process-gone ${JSON.stringify(details)}`))
  wc.on("console-message", (_e, level, message, line, sourceId) => {
    if (level >= 2) console.log(`[diag] renderer console: ${message} (${sourceId}:${line})`)
  })

  win.loadURL(url)
  win.on("closed", () => {
    win = null
    browserView = null // 随窗口销毁，不做 close（webContents 已失效）
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
