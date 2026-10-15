// 拉取各平台的静态 ffmpeg / ffprobe，作为「备料」放进 webui/desktop/bin/ffmpeg/。
//
// 背景：media MCP 插件是 ffmpeg 的前端，需要完整版 ffmpeg（Electron 自带的
// ffmpeg.dll 是 Chromium 的裁剪版，仅解码、无编码器、也不可当命令行调用）。
//
// 与内核 / MCP 插件的备料命名保持一致（bin/omni-<平台>-<架构>、bin/mcp-<平台>-<架构>/）：
//   bin/ffmpeg/ffmpeg-<平台>-<架构>[.exe]
//   bin/ffmpeg/ffprobe-<平台>-<架构>[.exe]
// 打包时由 scripts/electron-before-pack.cjs 按目标架构换成正式名，extraResources 带进包。
//
// ⚠️ 必须放在 bin/ffmpeg/ 子目录，不能直接放 bin/ 根目录：内核启动时会扫描
//    「自身 exe 同目录」把里面的可执行文件**逐个自动注册为 MCP 插件**，
//    ffmpeg 放根目录会被当成 MCP 服务器去连接（必然失败），插件列表冒出幻影条目。
//    子目录会被扫描跳过（内核遇到目录直接 continue）。
//
// 用法：
//   node scripts/fetch-ffmpeg.mjs --platform win32 --arch x64          # CI：只备料
//   node scripts/fetch-ffmpeg.mjs --platform win32 --arch x64 --local  # 顺带装入本机 bin/ffmpeg/ 正式名
//   node scripts/fetch-ffmpeg.mjs --list                               # 打印来源表
//
// 来源（见 MATRIX；均可用环境变量覆盖）：
//   win32/*、linux/*  → BtbN/FFmpeg-Builds（master 每日构建，zip / tar.xz，单个压缩包内含 ffmpeg+ffprobe）
//   darwin/*          → ffmpeg 取 eugeneware/ffmpeg-static 的 b6.0 资源（GitHub，含原生 arm64-x64），
//                       ffprobe 取 npm 平台包 @ffprobe-installer/darwin-<arch>
//     （备选 evermeet.cx 版本更新，但 CDN 限流严重——连续两次请求即 429，不适合 CI，故不用于默认路径）
//
// 覆盖用环境变量：
//   FFMPEG_URL_<PLATFORM>_<ARCH>   如 FFMPEG_URL_WIN32_ARM64=https://…/ffmpeg.zip
//   FFPROBE_URL_<PLATFORM>_<ARCH>  同上，单独覆盖 ffprobe
//   NPM_REGISTRY                   默认 https://registry.npmjs.org（可换 https://registry.npmmirror.com）
//   FFMPEG_CACHE=0                 关闭下载缓存（默认缓存于仓库根 .ffmpeg-cache/）
//   FFMPEG_FORCE=1                 忽略缓存强制重新下载
import { createWriteStream, existsSync, mkdirSync, copyFileSync, statSync, rmSync, readdirSync, readFileSync } from "node:fs"
import { get as httpGet } from "node:https"
import { execFileSync } from "node:child_process"
import { dirname, join, basename, relative } from "node:path"
import { fileURLToPath } from "node:url"

const HERE = dirname(fileURLToPath(import.meta.url))
const DESKTOP_DIR = join(HERE, "..")
const REPO_ROOT = join(DESKTOP_DIR, "..", "..")
const BIN_DIR = join(DESKTOP_DIR, "bin")
/** ffmpeg 备料与正式名都放这个子目录（内核扫描跳过目录，不会被误注册成 MCP 插件） */
const FF_DIR = join(BIN_DIR, "ffmpeg")
const CACHE_DIR = join(REPO_ROOT, ".ffmpeg-cache")

const PKG = { win32: "windows", darwin: "darwin", linux: "linux" }

const BTBN = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest"
const EUGENE = "https://github.com/eugeneware/ffmpeg-static/releases/download/b6.0"
const NPM_REGISTRY = (process.env.NPM_REGISTRY || "https://registry.npmjs.org").replace(/\/+$/, "")

/** BtbN：单个压缩包内含 ffmpeg + ffprobe */
const btbn = (variant, ext) => ({ kind: "btbn", variant, ext })

/**
 * 组合 → 各工具来源。spec 三种：
 *   { kind: "btbn", variant, ext }              同一个包内含 ffmpeg+ffprobe
 *   { kind: "file", url }                       直接是可执行文件
 *   { kind: "npm", pkg, entry }                 npm 平台包 tgz，entry 为包内可执行文件路径
 */
const MATRIX = {
  "win32/x64": { ffmpeg: btbn("win64", "zip"), ffprobe: btbn("win64", "zip") },
  "win32/arm64": { ffmpeg: btbn("winarm64", "zip"), ffprobe: btbn("winarm64", "zip") },
  "linux/x64": { ffmpeg: btbn("linux64", "tarxz"), ffprobe: btbn("linux64", "tarxz") },
  "linux/arm64": { ffmpeg: btbn("linuxarm64", "tarxz"), ffprobe: btbn("linuxarm64", "tarxz") },
  "darwin/x64": {
    ffmpeg: { kind: "file", url: `${EUGENE}/ffmpeg-darwin-x64` },
    ffprobe: { kind: "npm", pkg: "@ffprobe-installer/darwin-x64", entry: "package/ffprobe" },
  },
  "darwin/arm64": {
    ffmpeg: { kind: "file", url: `${EUGENE}/ffmpeg-darwin-arm64` },
    ffprobe: { kind: "npm", pkg: "@ffprobe-installer/darwin-arm64", entry: "package/ffprobe" },
  },
}

function parseArgs(argv) {
  const out = { platform: "", arch: "", local: false, list: false }
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (a === "--platform") out.platform = argv[++i]
    else if (a === "--arch") out.arch = argv[++i]
    else if (a === "--local") out.local = true
    else if (a === "--list") out.list = true
  }
  return out
}

const args = parseArgs(process.argv.slice(2))

if (args.list) {
  console.log("来源表（--platform / --arch 可用组合）：\n")
  for (const [k, v] of Object.entries(MATRIX)) {
    const d = (s) => (s.kind === "btbn" ? `BtbN ${s.variant}.${s.ext}` : s.kind === "file" ? s.url : `npm ${s.pkg} → ${s.entry}`)
    console.log(`  ${k.padEnd(14)} ffmpeg: ${d(v.ffmpeg)}`)
    console.log(`  ${"".padEnd(14)} ffprobe: ${d(v.ffprobe)}\n`)
  }
  console.log("覆盖：FFMPEG_URL_<PLATFORM>_<ARCH> / FFPROBE_URL_<PLATFORM>_<ARCH> / NPM_REGISTRY / FFMPEG_CACHE=0 / FFMPEG_FORCE=1")
  process.exit(0)
}

if (!args.platform || !args.arch) {
  console.error("用法：node scripts/fetch-ffmpeg.mjs --platform <win32|darwin|linux> --arch <x64|arm64> [--local] | --list")
  process.exit(1)
}

const key = `${args.platform}/${args.arch}`
const spec = MATRIX[key]
if (!spec) {
  console.error(`[ffmpeg] 不支持的组合：${key}（可用：${Object.keys(MATRIX).join(", ")}）`)
  process.exit(1)
}
const pkg = PKG[args.platform]
if (!pkg) {
  console.error(`[ffmpeg] 未知平台：${args.platform}`)
  process.exit(1)
}

const exe = args.platform === "win32" ? ".exe" : ""
const envKey = (tool) => `${tool.toUpperCase()}_URL_${args.platform.toUpperCase()}_${args.arch.toUpperCase()}`
const toolExe = (tool) => `${tool}${exe}`

// ---------- 下载 ----------

const REDIRECT_BODY = /^\s*Redirecting to (\S+?)\s*$/i

function download(url, dest, redirects = 0) {
  return new Promise((resolve, reject) => {
    if (redirects > 8) return reject(new Error(`重定向过多：${url}`))
    const req = httpGet(url, { headers: { "user-agent": "omni-fetch-ffmpeg/1.0" } }, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
        res.resume()
        return download(new URL(res.headers.location, url).toString(), dest, redirects + 1).then(resolve, reject)
      }
      if (res.statusCode !== 200) {
        res.resume()
        return reject(new Error(`HTTP ${res.statusCode} ${url}`))
      }
      const total = Number(res.headers["content-length"] || 0)
      let got = 0
      let lastTick = 0
      const ws = createWriteStream(dest)
      res.on("data", (c) => {
        got += c.length
        if (total && got - lastTick > 20 * 1024 * 1024) {
          lastTick = got
          console.log(`[ffmpeg]   下载中 ${(got / 1048576).toFixed(0)}/${(total / 1048576).toFixed(0)} MB`)
        }
      })
      res.pipe(ws)
      ws.on("finish", () => resolve(dest))
      ws.on("error", reject)
      res.on("error", reject)
    })
    req.setTimeout(120000, () => req.destroy(new Error("下载超时")))
    req.on("error", reject)
  })
}

/**
 * 下载到缓存目录并返回本地路径。
 * 处理两类坑：
 *  1. 有的镜像（npmmirror 的 tarball 地址）返回的「重定向」是响应体里的一行文本
 *     `Redirecting to <url>.`，而不是 3xx + Location —— 这里识别并跟进；
 *  2. 429/5xx/超时统一退避重试（evermeet 之类 CDN 会限流）。
 */
async function fetchFile(url, name) {
  mkdirSync(CACHE_DIR, { recursive: true })
  const cached = join(CACHE_DIR, basename(new URL(url).pathname) || name)
  const fresh = process.env.FFMPEG_CACHE !== "0" && !process.env.FFMPEG_FORCE
  if (fresh && existsSync(cached) && statSync(cached).size > 64 * 1024) {
    console.log(`[ffmpeg]   命中缓存 ${basename(cached)}`)
    return cached
  }
  let target = url
  for (let attempt = 1; attempt <= 4; attempt++) {
    console.log(`[ffmpeg]   下载 ${target}`)
    try {
      await download(target, cached)
      let size = statSync(cached).size
      // 文本重定向页：跳过它去真正的地址
      if (size < 4096) {
        const m = REDIRECT_BODY.exec(readFileSync(cached, "utf8"))
        if (m) {
          target = m[1].startsWith("http") ? m[1] : new URL(m[1], target).toString()
          console.log(`[ffmpeg]   镜像返回文本重定向 → ${target}`)
          if (attempt < 4) continue
        }
      }
      if (size < 4096) throw new Error(`内容过小（${size} 字节），疑似错误页：${target}`)
      console.log(`[ffmpeg]   完成 ${(size / 1048576).toFixed(1)} MB`)
      return cached
    } catch (e) {
      rmSync(cached, { force: true })
      const retriable = /HTTP (429|5\d\d)|超时|socket|ECONNRESET|ETIMEDOUT|EAI_AGAIN/i.test(String(e.message))
      if (attempt === 4 || !retriable) throw e
      const wait = attempt * 8000
      console.log(`[ffmpeg]   ${e.message} → ${wait / 1000}s 后重试（第 ${attempt + 1} 次）`)
      await new Promise((r) => setTimeout(r, wait))
    }
  }
  throw new Error(`下载失败：${url}`)
}

// ---------- 解包 ----------

/**
 * 多策略解包：逐个尝试，任一条全部命令成功即完成。
 *
 * ⚠️ 调用 tar 时**必须传相对路径**并把 cwd 设在目标目录：Windows 上的 GNU tar 会把
 *    `C:\a\b.zip` 里的冒号当成远程主机分隔符（报 `Cannot connect to C: resolve failed`，
 *    实测踩到）。`--force-local` 只在 GNU tar 有效、bsdtar 不认，所以统一走相对路径。
 */
function extract(archive, destDir) {
  rmSync(destDir, { recursive: true, force: true })
  mkdirSync(destDir, { recursive: true })
  const lower = archive.toLowerCase()
  const rel = relative(destDir, archive) || basename(archive)
  const raw = lower.endsWith(".xz") ? archive.slice(0, -3) : ""
  const py = "import sys,tarfile;tarfile.open(sys.argv[1]).extractall(sys.argv[2])"

  /** 每项：{ cmds: [[cmd, args]…]（顺序执行）, cwd? } */
  const strategies = []
  if (lower.endsWith(".zip") || lower.endsWith(".tgz") || lower.endsWith(".tar.gz")) {
    strategies.push({ cmds: [["tar", ["-xf", rel]]], cwd: destDir })
    strategies.push({ cmds: [["unzip", ["-oq", archive, "-d", destDir]]] })
    strategies.push({ cmds: [["bsdtar", ["-xf", archive, "-C", destDir]]] })
    if (process.platform === "win32") {
      strategies.push({
        cmds: [["powershell", ["-NoProfile", "-Command", `Expand-Archive -LiteralPath '${archive}' -DestinationPath '${destDir}' -Force`]]],
      })
    }
  } else {
    // tar.xz：优先 tar 自带 xz，再退到单独解压，最后用 Python 的 tarfile（各平台通常都有）
    strategies.push({ cmds: [["tar", ["-xf", rel]]], cwd: destDir })
    strategies.push({ cmds: [["tar", ["-xJf", rel]]], cwd: destDir })
    if (raw) {
      strategies.push({ cmds: [["xz", ["-dkf", archive]], ["tar", ["-xf", relative(destDir, raw)]]], cwd: destDir })
    }
    strategies.push({ cmds: [["python3", ["-c", py, archive, destDir]]] })
    strategies.push({ cmds: [["python", ["-c", py, archive, destDir]]] })
  }
  strategies.push({ cmds: [["7z", ["x", "-y", `-o${destDir}`, archive]]] })

  const errors = []
  for (const { cmds, cwd } of strategies) {
    try {
      for (const [cmd, cmdArgs] of cmds) execFileSync(cmd, cmdArgs, { stdio: "pipe", cwd })
      return
    } catch (e) {
      errors.push(String(e.message).split("\n")[0])
    }
  }
  throw new Error(`解包失败（tar/unzip/python/7z 都不可用）：${archive}\n  ${errors.join("\n  ")}`)
}

/** 在解包目录里按文件名递归查找 */
function findBinary(root, name) {
  const stack = [root]
  while (stack.length) {
    const dir = stack.pop()
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, entry.name)
      if (entry.isDirectory()) stack.push(p)
      else if (entry.name.toLowerCase() === name.toLowerCase()) return p
    }
  }
  return ""
}

async function fetchJSON(url) {
  return new Promise((resolve, reject) => {
    let body = ""
    httpGet(url, { headers: { "user-agent": "omni-fetch-ffmpeg/1.0" } }, (res) => {
      if (res.statusCode !== 200) {
        res.resume()
        return reject(new Error(`HTTP ${res.statusCode} ${url}`))
      }
      res.setEncoding("utf8")
      res.on("data", (c) => (body += c))
      res.on("end", () => {
        try {
          resolve(JSON.parse(body))
        } catch {
          reject(new Error(`返回非 JSON：${body.slice(0, 120)}`))
        }
      })
    })
      .on("error", reject)
      .setTimeout(30000, function () {
        this.destroy(new Error("请求超时"))
      })
  })
}

/**
 * 确保压缩包已解开到 tmp：已解过就直接复用。
 * 不只是省时间——重复解包要先删旧目录，而删除大目录会被沙箱/杀软的文件回收钩子拦截
 * （实测报 genie-trash ETIMEDOUT 把整轮拉取打断）。复用则完全绕开该路径。
 */
async function ensureExtracted(archive, tmp, tool) {
  if (existsSync(tmp) && findBinary(tmp, toolExe(tool))) {
    console.log(`[ffmpeg]   复用已解包目录 ${basename(tmp)}`)
    return tmp
  }
  extract(archive, tmp)
  return tmp
}

// ---------- 按来源取出某个工具 ----------

/** @returns {Promise<{path: string, license?: string}>} */
async function materialize(tool, s) {
  const override = process.env[envKey(tool)]

  if (s.kind === "file" || (override && (s.kind === "file" || s.kind === "btbn"))) {
    const url = override || s.url
    const f = await fetchFile(url, `${tool}-${args.platform}-${args.arch}`)
    return { path: f }
  }

  if (s.kind === "btbn") {
    const ext = s.ext === "zip" ? "zip" : "tar.xz"
    const url = `${BTBN}/ffmpeg-master-latest-${s.variant}-gpl.${ext}`
    const archive = await fetchFile(url, `${s.variant}.${ext}`)
    const tmp = await ensureExtracted(archive, join(CACHE_DIR, `x-${s.variant}`), tool)
    const bin = findBinary(tmp, toolExe(tool))
    if (!bin) throw new Error(`压缩包内未找到 ${toolExe(tool)}：${url}`)
    return { path: bin, license: findBinary(tmp, "LICENSE.txt") }
  }

  // npm 平台包：解析最新版 tarball 地址（不硬编码版本）
  const doc = await fetchJSON(`${NPM_REGISTRY}/${s.pkg}`)
  const ver = doc?.["dist-tags"]?.latest
  const tarball = doc?.versions?.[ver]?.dist?.tarball
  if (!tarball) throw new Error(`无法解析 ${s.pkg} 的 tarball 地址`)
  console.log(`[ffmpeg]   npm ${s.pkg}@${ver}`)
  const archive = await fetchFile(tarball, `${s.pkg.replace(/[@/]/g, "_")}-${ver}.tgz`)
  const tmp = await ensureExtracted(archive, join(CACHE_DIR, `x-${s.pkg.replace(/[@/]/g, "_")}`), tool)
  const bin = join(tmp, s.entry)
  if (!existsSync(bin)) {
    const alt = findBinary(tmp, toolExe(tool))
    if (!alt) throw new Error(`tgz 内未找到 ${s.entry}：${tarball}`)
    return { path: alt }
  }
  return { path: bin }
}

// ---------- 主流程 ----------

async function main() {
  mkdirSync(FF_DIR, { recursive: true })
  const staged = []

  for (const tool of ["ffmpeg", "ffprobe"]) {
    const got = await materialize(tool, spec[tool])
    const name = `${tool}-${pkg}-${args.arch}${exe}`
    const dst = join(FF_DIR, name)
    copyFileSync(got.path, dst)
    const size = statSync(dst).size
    if (size < 1 << 20) throw new Error(`${name} 体积异常（${size} 字节）`)
    console.log(`[ffmpeg]   备料 ffmpeg/${name}  ${(size / 1048576).toFixed(1)} MB`)
    staged.push(dst)
    if (got.license) copyFileSync(got.license, join(FF_DIR, `LICENSE-${pkg}-${args.arch}.txt`))
  }

  if (args.local) {
    for (const p of staged) {
      const name = basename(p).replace(new RegExp(`-${pkg}-${args.arch}`), "")
      copyFileSync(p, join(FF_DIR, name))
      console.log(`[ffmpeg]   已装入本机 bin/ffmpeg/${name}`)
    }
  }

  console.log(`[ffmpeg] 完成：${key} → ${staged.map((p) => basename(p)).join(", ")}`)
}

main().catch((e) => {
  console.error(`[ffmpeg] 失败：${e.message}`)
  process.exit(1)
})
