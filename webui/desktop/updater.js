// omni 桌面端更新：照搬 opencode_desk 的思路（不依赖 latest.yml，只用 GitHub Releases 元数据）。
// 链路：检查（配置的更新域名 /api/upgrade 优先，回落 api.github.com）→ 流式下载安装包（带进度、可取消）
//       → Windows 静默原地升级并重启（NSIS /S --updated --force-run），其余平台打开安装包手动替换。
//
// 与 opencode_desk 的差异：
//  · 更新域名没有默认值（留空 = 直连 GitHub），存 userData/update.json；
//  · dev（未打包）允许从设置里手动「检查更新」看清状态，但不做启动自动检查；
//  · 附件名按本项目 electron-builder.yml / release.yml 约定：omni-Setup.*（win x64 固定无架构后缀）。
const { app, shell } = require("electron")
const { spawn } = require("node:child_process")
const { createWriteStream, existsSync, mkdirSync, readFileSync, rmSync, statSync, writeFileSync } = require("node:fs")
const { join } = require("node:path")
const { once } = require("node:events")

const UPDATE_REPO = "OmniAgnes/OmniAgnes"
const UPDATE_TAG_PREFIX = "v"

// ---------- 更新域名（可配置，无默认值） ----------

function cfgFile() {
  return join(app.getPath("userData"), "update.json")
}

function getDomain() {
  try {
    const j = JSON.parse(readFileSync(cfgFile(), "utf8"))
    return typeof j.domain === "string" ? j.domain.trim() : ""
  } catch {
    return ""
  }
}

/** 保存域名；裸 host 自动补 https://，去尾部斜杠；返回归一化后的值 */
function setDomain(d) {
  let s = String(d ?? "").trim()
  if (s && !/^https?:\/\//i.test(s)) s = "https://" + s
  s = s.replace(/\/+$/, "")
  try {
    mkdirSync(join(app.getPath("userData")), { recursive: true })
    writeFileSync(cfgFile(), JSON.stringify({ domain: s }, null, 2), "utf8")
  } catch {
    /* 写不进去也要把归一化值还给界面 */
  }
  return s
}

// ---------- 安装包附件名（与 electron-builder.yml artifactName + release.yml 重命名一致） ----------

function linuxArtifactArch() {
  // electron-builder 对 AppImage 的 ${arch} 用 Linux 惯例名：x64 → x86_64
  return process.arch === "x64" ? "x86_64" : process.arch
}

function fallbackFileName() {
  if (process.platform === "win32") return process.arch === "arm64" ? "omni-Setup-arm64.exe" : "omni-Setup.exe"
  if (process.platform === "darwin") return `omni-Setup-${process.arch}.dmg`
  return `omni-Setup-${linuxArtifactArch()}.AppImage`
}

// ---------- 版本比较 ----------

function parseVer(v) {
  const s = String(v ?? "").trim().replace(/^v/i, "")
  const [core, ...rest] = s.split("-")
  const nums = core
    .split(".")
    .map((x) => Number.parseInt(x, 10))
    .map((n) => (Number.isFinite(n) && n >= 0 ? n : 0))
  while (nums.length < 3) nums.push(0)
  return { nums: nums.slice(0, 3), pre: rest.join("-") }
}

/** a 是否比 b 新；同号时正式版 > 预发布，不给用户推降级 */
function isNewer(a, b) {
  const x = parseVer(a)
  const y = parseVer(b)
  for (let i = 0; i < 3; i++) {
    if (x.nums[i] !== y.nums[i]) return x.nums[i] > y.nums[i]
  }
  if (x.pre === y.pre) return false
  if (!x.pre && y.pre) return true
  if (x.pre && !y.pre) return false
  return x.pre > y.pre
}

// ---------- 主进程内存状态（渲染层通过事件/快照读它，界面不自己算） ----------

const st = { checking: false, checkedAt: 0, error: undefined, latest: undefined, progress: undefined, file: undefined, abort: undefined }

function snapshot() {
  return {
    dev: !app.isPackaged,
    current: app.getVersion(),
    checking: st.checking,
    checkedAt: st.checkedAt,
    error: st.error,
    latest: st.latest ? { ...st.latest } : undefined,
    progress: st.progress ? { ...st.progress } : undefined,
    file: st.file,
    canInstall: !!st.file && existsSync(st.file),
    domain: getDomain(),
  }
}

let sink = null
function setUpdateSink(fn) {
  sink = fn
}
function emit() {
  if (sink) sink(snapshot())
}

function state() {
  return snapshot()
}

// ---------- 检查更新 ----------

function ghHeaders() {
  return { "user-agent": "omni-desktop-updater", accept: "application/vnd.github+json", "x-github-api-version": "2022-11-28" }
}

/** 从 Release 附件里挑当前平台+架构的安装包；挑不到给源码包地址兜底（至少不是死路） */
function pickAsset(assets, tag, repo) {
  const list = Array.isArray(assets) ? assets.filter((a) => a.browser_download_url) : []
  const want = process.platform === "win32" ? /\.exe$/i : process.platform === "darwin" ? /\.(dmg|zip)$/i : /\.(appimage|deb)$/i
  let archHit
  if (process.platform === "win32") {
    const expected = process.arch === "arm64" ? "omni-Setup-arm64.exe" : "omni-Setup.exe"
    archHit = list.find((a) => (a.name ?? "").toLowerCase() === expected.toLowerCase())
  } else if (process.platform === "darwin") {
    archHit = list.find((a) => new RegExp(`-${process.arch}\\.dmg$`, "i").test(a.name ?? ""))
  } else {
    archHit = list.find((a) => new RegExp(`-${linuxArtifactArch()}\\.appimage$`, "i").test(a.name ?? ""))
  }
  const hit =
    archHit ??
    list.find((a) => want.test(a.name ?? "")) ??
    list.find((a) => /\.(exe|dmg|zip|appimage|deb)$/i.test(a.name ?? "")) ??
    list[0]
  if (hit && hit.browser_download_url) {
    return { url: hit.browser_download_url, name: hit.name ?? "", size: Number(hit.size) || 0 }
  }
  return { url: `https://github.com/${repo}/archive/refs/tags/${tag}.zip`, name: "", size: 0 }
}

/** 平台服务端代拉（国内 api.github.com 常不通时的主路径）；没配域名或拉不到返回 null */
async function fetchPlatformRelease() {
  const base = getDomain()
  if (!base) return null
  try {
    const r = await fetch(`${base}/api/upgrade`, { headers: { Accept: "application/json" }, signal: AbortSignal.timeout(6000) })
    if (!r.ok) return null
    const data = await r.json()
    if (!data || data.ok !== true || !data.tag_name) return null
    return { ...data, draft: false, prerelease: false }
  } catch {
    return null
  }
}

async function checkUpdate() {
  st.checking = true
  st.error = undefined
  emit()

  const base = `https://api.github.com/repos/${UPDATE_REPO}`
  let rel = null
  let err = ""
  rel = await fetchPlatformRelease()
  if (!rel) {
    try {
      const r = await fetch(`${base}/releases/latest`, { headers: ghHeaders(), signal: AbortSignal.timeout(8000) })
      if (r.ok) {
        rel = await r.json()
      } else if (r.status === 404) {
        err = "读不到发布信息：仓库未公开（或地址不对），也可能还没建过任何 Release。"
      } else if (r.status === 403 || r.status === 429) {
        err = `GitHub 接口限流（HTTP ${r.status}），稍后再试即可`
      } else {
        err = `GitHub 返回 HTTP ${r.status}`
      }
    } catch (e) {
      err = e instanceof Error ? e.message : String(e)
    }
    if (!rel && !err) {
      try {
        const r = await fetch(`${base}/releases?per_page=10`, { headers: ghHeaders(), signal: AbortSignal.timeout(8000) })
        if (r.ok) {
          const arr = await r.json()
          rel = arr.find((x) => !x.draft && !x.prerelease) ?? null
        }
      } catch {
        /* 保留原始失败原因 */
      }
    }
  }

  st.checking = false
  st.checkedAt = Date.now()

  if (!rel || !rel.tag_name) {
    // 已下载/下载中的版本不因一次检查失败被清掉
    st.error = err || "没能读到发布信息（可能是网络不通或被限流）"
    emit()
    return snapshot()
  }

  const version = rel.tag_name.replace(new RegExp("^" + UPDATE_TAG_PREFIX.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), "")
  const asset = pickAsset(rel.assets, rel.tag_name, UPDATE_REPO)

  if (!isNewer(version, app.getVersion())) {
    const hadFile = !!st.file
    st.latest = undefined
    st.progress = undefined
    if (hadFile) {
      st.file = undefined
      cleanupDownloaded()
    }
    emit()
    return snapshot()
  }

  const changed = !st.latest || st.latest.version !== version
  st.latest = {
    version,
    tag: rel.tag_name,
    notes: typeof rel.body === "string" ? rel.body : "",
    pageUrl: rel.html_url ?? `https://github.com/${UPDATE_REPO}/releases/tag/${rel.tag_name}`,
    publishedAt: rel.published_at ?? "",
    downloadUrl: asset.url,
    downloadName: asset.name || fallbackFileName(),
    size: asset.size,
  }
  if (changed) {
    st.file = undefined
    st.progress = undefined
    cleanupDownloaded()
  }
  emit()
  return snapshot()
}

// ---------- 下载 ----------

function updateDir() {
  return join(app.getPath("userData"), "updates")
}

function targetFile(name) {
  // 附件名来自网络，不可信：只取最后一段并去掉控制字符
  const safe = String(name || "").split(/[\\/]/).pop().replace(/[\u0000-\u001f]/g, "")
  return join(updateDir(), safe && !/^\.+$/.test(safe) ? safe : fallbackFileName())
}

function cleanupDownloaded() {
  try {
    rmSync(updateDir(), { recursive: true, force: true })
  } catch {
    /* 删不掉就算了：下次下载会覆盖 */
  }
}

let lastPush = 0
function pushProgress(received, total, force = false) {
  st.progress = { received, total }
  const now = Date.now()
  if (!force && now - lastPush < 300) return
  lastPush = now
  emit()
}

async function downloadUpdate() {
  if (st.abort) return snapshot() // 已在下载：忽略重复点击
  const latest = st.latest
  if (!latest || !latest.downloadUrl) {
    st.error = "还没有可下载的版本，请先检查更新"
    emit()
    return snapshot()
  }
  const file = targetFile(latest.downloadName || "")
  mkdirSync(updateDir(), { recursive: true })
  try {
    rmSync(file, { force: true })
  } catch {
    /* 不存在就算了 */
  }

  const ac = new AbortController()
  st.abort = ac
  st.error = undefined
  pushProgress(0, latest.size || 0, true)

  let ws = null
  try {
    const r = await fetch(latest.downloadUrl, { signal: ac.signal, redirect: "follow" })
    if (!r.ok || !r.body) throw new Error(`下载失败 HTTP ${r.status}`)
    const total = Number(r.headers.get("content-length")) || latest.size || 0
    let received = 0
    ws = createWriteStream(file)
    for await (const chunk of r.body) {
      received += chunk.byteLength
      pushProgress(received, total)
      if (!ws.write(chunk)) await once(ws, "drain")
    }
    ws.end()
    await once(ws, "close")
    ws = null

    const got = statSync(file).size
    if (total > 0 && got !== total) throw new Error(`下载不完整（${got} / ${total} 字节），请重试`)

    st.file = file
    st.progress = undefined
    st.abort = undefined
    emit() // 直接 emit：不能走 pushProgress，会把已清空的进度又填回去
    return snapshot()
  } catch (e) {
    if (ws) {
      try { ws.destroy() } catch { /* 已关闭 */ }
    }
    st.abort = undefined
    st.progress = undefined
    const aborted = ac.signal.aborted
    st.error = aborted ? "已取消下载" : e instanceof Error ? e.message : String(e)
    try {
      rmSync(file, { force: true }) // 半截包一定删掉：会被误解为可安装
    } catch {
      /* 删不掉就留着，下次覆盖 */
    }
    emit()
    if (aborted) throw new Error("已取消下载")
    throw e
  }
}

function cancelDownload() {
  if (st.abort) st.abort.abort()
  st.abort = undefined
  return snapshot()
}

// ---------- 安装 ----------

function installUpdate() {
  const file = st.file
  if (!file || !existsSync(file)) return { ok: false, error: "安装包不存在或已被删除，请重新下载" }
  if (process.platform !== "win32") {
    // mac 的 dmg / linux 的 AppImage 只能打开让用户自己完成替换
    try {
      void shell.openPath(file)
    } catch (e) {
      return { ok: false, error: e instanceof Error ? e.message : String(e) }
    }
    return { ok: true, manual: true }
  }
  try {
    // NSIS 静默原地升级：/S 无界面、--updated 沿用安装目录并保留快捷方式、--force-run 装完自动拉起
    spawn(file, ["/S", "--updated", "--force-run"], { detached: true, stdio: "ignore" })
      .on("error", () => void shell.openPath(file))
      .unref()
  } catch {
    void shell.openPath(file)
  }
  app.isQuittingByUser = true
  setTimeout(() => app.quit(), 300)
  return { ok: true, manual: false }
}

async function openReleasePage(tag) {
  const url = tag
    ? `https://github.com/${UPDATE_REPO}/releases/tag/${encodeURIComponent(tag)}`
    : `https://github.com/${UPDATE_REPO}/releases`
  await shell.openExternal(url)
}

module.exports = { setUpdateSink, state, checkUpdate, downloadUpdate, cancelDownload, installUpdate, openReleasePage, getDomain, setDomain }
