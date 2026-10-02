import { useCallback, useEffect, useRef, useState } from "react"
import { ArrowLeftIcon, ArrowRightIcon, Loader2Icon, RotateCwIcon, XIcon } from "lucide-react"

import { BROWSER_OPEN_EVENT } from "@/lib/browser"
import { useI18n } from "@/lib/i18n"

// 内置浏览器面板（仅桌面端挂载）：
//   · 长轮询 GET /api/browser/next 领取内核 browser 工具发来的操作，
//     经 window.omniDesktop.browserOp 交给 Electron 主进程在 WebContentsView 里执行；
//   · 原生视图悬浮在占位 div 之上——占位区尺寸变化实时上报（ResizeObserver），
//     保证 AI 点击坐标与用户所见对齐；
//   · AI 开始操作而面板收起时自动展开（close/hide 除外），让用户看得见它在干什么。
// 轮询本身即“桌面壳在线”心跳：内核据此放行 browser 工具。
//
// 显隐由外部控制（受控组件）：入口在输入框底部「完全访问」后面的地球图标，
// 收起时本组件不渲染任何 UI（原生视图同时隐藏）。

interface BrowserOpMsg {
  id: string
  op: string
  args: Record<string, unknown>
}

interface BrowserPaneProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

// 领取到的操作 → IPC 执行 → 结果回填。
async function runOp(op: BrowserOpMsg): Promise<void> {
  const desktop = window.omniDesktop
  if (!desktop?.browserOp) return
  let res: { ok: boolean; output?: string; error?: string } = { ok: false, error: "桌面桥不可用" }
  try {
    res = await desktop.browserOp(op.op, op.args)
  } catch (e) {
    res = { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
  try {
    await fetch("/api/browser/result", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id: op.id, ok: res.ok, output: res.output ?? "", error: res.error ?? "" }),
    })
  } catch {
    /* 服务断开时下一轮 poll 自会感知 */
  }
}

export function BrowserPane({ open, onOpenChange }: BrowserPaneProps) {
  const { t } = useI18n()
  const [url, setUrl] = useState("")
  const [busy, setBusy] = useState(false)
  const holderRef = useRef<HTMLDivElement>(null)
  // 轮询循环只跑一次，用 ref 读取最新的显隐回调，避免闭包过期
  const openChangeRef = useRef(onOpenChange)
  openChangeRef.current = onOpenChange

  // 面板显隐 → 原生视图显隐
  useEffect(() => {
    window.omniDesktop?.browserVisible?.(open)
  }, [open])

  // 占位区矩形上报（原生视图对齐用）。收起时组件不渲染 → 上报 0×0 让视图隐藏。
  useEffect(() => {
    if (!open) {
      window.omniDesktop?.browserBounds?.({ x: 0, y: 0, width: 0, height: 0 })
      return
    }
    const el = holderRef.current
    if (!el) return
    const report = () => {
      const r = el.getBoundingClientRect()
      window.omniDesktop?.browserBounds?.({ x: r.x, y: r.y, width: r.width, height: r.height })
    }
    report()
    const ro = new ResizeObserver(report)
    ro.observe(el)
    window.addEventListener("resize", report)
    return () => {
      ro.disconnect()
      window.removeEventListener("resize", report)
    }
  }, [open])

  // 领取操作并执行；AI 页面操作前自动展开面板
  useEffect(() => {
    let stop = false
    const desktop = window.omniDesktop
    if (!desktop?.browserOp) return

    const loop = async () => {
      while (!stop) {
        let op: BrowserOpMsg | null = null
        try {
          const r = await fetch("/api/browser/next")
          if (r.ok) {
            const d = await r.json()
            op = d.op ?? null
          }
        } catch {
          /* 服务断开：下一轮重试 */
        }
        if (!op) {
          if (!stop) await new Promise((res) => setTimeout(res, 1500))
          continue
        }
        setBusy(true)
        if (op.op === "open" || op.op === "navigate") {
          const u = typeof op.args.url === "string" ? op.args.url : ""
          if (u) setUrl(u)
        }
        if (op.op !== "close" && op.op !== "hide") openChangeRef.current(true)
        await runOp(op)
        setBusy(false)
      }
    }
    void loop()
    return () => {
      stop = true
    }
  }, [])

  const toolbarOp = useCallback(async (op: string, args: Record<string, unknown> = {}) => {
    const res = await window.omniDesktop?.browserOp?.(op, args)
    if (res?.ok && op === "state") {
      const m = /^URL: (\S+)/m.exec(res.output ?? "")
      if (m) setUrl(m[1])
    }
    return res
  }, [])

  const go = async () => {
    const v = url.trim()
    if (!v) return
    setBusy(true)
    await toolbarOp("navigate", { url: v })
    setBusy(false)
  }

  // 聊天里的链接：展开面板并打开该网址（不接管主窗口导航）。
  // 放在展开态判断之前 —— 面板此刻可能还是收起的，先展开，浏览器视图随之显示，再导航。
  useEffect(() => {
    const onOpen = (e: Event) => {
      const url = (e as CustomEvent<{ url?: string }>).detail?.url
      if (!url) return
      openChangeRef.current(true)
      setUrl(url)
      setBusy(true)
      void toolbarOp("navigate", { url }).finally(() => setBusy(false))
    }
    window.addEventListener(BROWSER_OPEN_EVENT, onOpen)
    return () => window.removeEventListener(BROWSER_OPEN_EVENT, onOpen)
  }, [toolbarOp])

  if (!open) return null

  return (
    <div className="bg-background flex h-full w-[560px] shrink-0 flex-col border-l">
      {/* 工具栏 */}
      <div className="bg-muted/30 flex items-center gap-1 px-2 py-1.5">
        <button
          type="button"
          title={t("b.back")}
          onClick={() => void toolbarOp("back")}
          className="text-muted-foreground hover:bg-accent hover:text-foreground flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors"
        >
          <ArrowLeftIcon className="size-4" />
        </button>
        <button
          type="button"
          title={t("b.forward")}
          onClick={() => void toolbarOp("forward")}
          className="text-muted-foreground hover:bg-accent hover:text-foreground flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors"
        >
          <ArrowRightIcon className="size-4" />
        </button>
        <button
          type="button"
          title={t("b.reload")}
          onClick={() => void toolbarOp("reload")}
          className="text-muted-foreground hover:bg-accent hover:text-foreground flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors"
        >
          {busy ? <Loader2Icon className="size-4 animate-spin" /> : <RotateCwIcon className="size-4" />}
        </button>
        <input
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void go()
          }}
          placeholder={t("b.urlPh")}
          className="border-input bg-background text-foreground min-w-0 flex-1 rounded-md border px-2 py-1 text-xs outline-none focus:ring-1 focus:ring-ring"
        />
        <button
          type="button"
          title={t("b.hide")}
          onClick={() => {
            void window.omniDesktop?.browserOp?.("hide")
            onOpenChange(false)
          }}
          className="text-muted-foreground hover:bg-accent hover:text-foreground flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md transition-colors"
        >
          <XIcon className="size-4" />
        </button>
      </div>
      {/* AI 操作指示条 */}
      {busy && (
        <div className="bg-primary/5 text-primary flex items-center gap-2 px-3 py-1 text-xs">
          <Loader2Icon className="size-3 animate-spin" />
          {t("b.busy")}
        </div>
      )}
      {/* 原生 WebContentsView 悬浮占位区：必须 min-h-0 flex-1，矩形变化即上报 */}
      <div ref={holderRef} className="min-h-0 flex-1" />
    </div>
  )
}
