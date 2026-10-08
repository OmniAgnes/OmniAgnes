/**
 * 相对时间文案（中英/繁简体由 i18n 提供）：
 * 刚刚 / N 分钟前 / N 小时前 / N 天前；超过 30 天显示 MM-DD。
 * 项目内任务与「任务」TAB 复用它，保证两处时间显示一致。
 */
export function relTime(t: (k: string, v?: Record<string, string | number>) => string, iso: string): string {
  const ts = new Date(iso).getTime()
  if (!Number.isFinite(ts)) return ""
  const min = Math.floor(Math.max(0, Date.now() - ts) / 60000)
  if (min < 1) return t("s.agoNow")
  if (min < 60) return t("s.agoMin", { n: min })
  const hr = Math.floor(min / 60)
  if (hr < 24) return t("s.agoHour", { n: hr })
  const day = Math.floor(hr / 24)
  if (day < 30) return t("s.agoDay", { n: day })
  const d = new Date(ts)
  return `${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`
}