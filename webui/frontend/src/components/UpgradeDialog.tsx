import { useCallback, useEffect, useState } from "react"
import { ArrowUpCircleIcon, DownloadIcon, Loader2Icon, RocketIcon, XIcon } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useI18n } from "@/lib/i18n"

/**
 * 升级确认弹窗（由标题栏左上角的「更新」入口打开）：确认后直接下载 + 安装。
 * 状态全部来自主进程快照（父级实时下传）：
 *  · 发现新版 → 「立即升级」→ download，下载完成后自动 install（confirmed 置位才自动）；
 *  · 下载中 → 进度条 + 取消下载；
 *  · 已就绪 → 「立即安装并重启」（不自动装，等用户点）。
 */
export function UpgradeDialog({ st, onClose }: { st: OmniUpdateState; onClose: () => void }) {
  const { t } = useI18n()
  const [confirmed, setConfirmed] = useState(false)
  const [installing, setInstalling] = useState(false)
  const update = window.omniDesktop?.update

  const latest = st.latest ?? null
  const downloading = !!st.progress
  const ready = !!st.canInstall
  const pct =
    st.progress && st.progress.total > 0
      ? Math.min(100, Math.round((st.progress.received / st.progress.total) * 100))
      : null

  const doInstall = useCallback(() => {
    setInstalling(true)
    void update?.install()
  }, [update])

  useEffect(() => {
    if (confirmed && ready && !installing) doInstall()
  }, [confirmed, ready, installing, doInstall])

  const startUpgrade = () => {
    if (ready) {
      doInstall()
      return
    }
    setConfirmed(true)
    void update?.download().catch(() => {}) // 失败原因从状态快照的 error 里来，不在这里弹窗
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("ud.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3 pt-1">
          {latest && (
            <div className="flex items-center gap-2 text-sm">
              <ArrowUpCircleIcon className="text-primary size-4 shrink-0" />
              <b>{t("ud.found", { v: latest.version })}</b>
              <span className="text-muted-foreground text-xs">{t("ud.from", { v: st.current })}</span>
            </div>
          )}
          {st.error && <p className="text-destructive text-sm">{st.error}</p>}
          {downloading && (
            <div className="space-y-1.5">
              <div className="bg-muted h-2 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full transition-all"
                  style={{ width: pct === null ? "30%" : `${pct}%` }}
                />
              </div>
              <span className="text-muted-foreground text-xs">
                {pct === null ? t("ud.downloading") : `${t("ud.downloading")} ${pct}%`}
              </span>
            </div>
          )}
          {ready && !installing && (
            <p className="text-muted-foreground text-xs">{t("ud.readyHint")}</p>
          )}
          <div className="flex justify-end gap-2 pt-1">
            {installing ? (
              <Button size="sm" disabled>
                <Loader2Icon className="size-4 animate-spin" />
                {t("ud.installing")}
              </Button>
            ) : ready ? (
              <Button size="sm" onClick={doInstall}>
                <RocketIcon className="size-4" />
                {t("ud.ready")}
              </Button>
            ) : downloading ? (
              <Button size="sm" variant="ghost" onClick={() => void update?.cancel()}>
                <XIcon className="size-4" />
                {t("ud.cancel")}
              </Button>
            ) : (
              <Button size="sm" onClick={startUpgrade}>
                <DownloadIcon className="size-4" />
                {t("ud.now")}
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
