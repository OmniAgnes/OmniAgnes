import { useEffect, useMemo, useState } from "react"
import { BarChart3Icon } from "lucide-react"
import { CartesianGrid, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"

import { api } from "@/lib/api"
import type { UsageRecord } from "@/types"

/** 快捷区间（今天/昨天固定显示在卡片里，不参与筛选） */
const RANGES = [
  { id: "30d", label: "最近30天" },
  { id: "all", label: "总量" },
  { id: "custom", label: "自定义" },
] as const

type RangeId = (typeof RANGES)[number]["id"]

const MODEL_COLORS = [
  "#2f7cd6",
  "#d97706",
  "#0e9f6e",
  "#7c5cff",
  "#e11d48",
  "#0891b2",
  "#65a30d",
  "#c026d3",
]

function localDate(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, "0")
  const day = String(d.getDate()).padStart(2, "0")
  return `${y}-${m}-${day}`
}

function fmtK(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(n >= 10_000_000 ? 0 : 1) + "M"
  if (n >= 1_000) return (n / 1_000).toFixed(n >= 10_000 ? 0 : 1) + "k"
  return String(n)
}

/**
 * 使用量统计：每次对话的 token 消耗按 天×模型 聚合展示。
 * 筛选区间（最近30天/总量/自定义）；今天/昨天两张固定卡片不受筛选影响；
 * 下方折线图按模型分色显示每日 token 总量。
 */
export function UsagePanel() {
  const [records, setRecords] = useState<UsageRecord[] | null>(null)
  const [range, setRange] = useState<RangeId>("30d")
  const [from, setFrom] = useState("")
  const [to, setTo] = useState("")

  useEffect(() => {
    let alive = true
    api
      .usage()
      .then((r) => {
        if (alive) setRecords(r.records ?? [])
      })
      .catch(() => {
        if (alive) setRecords([])
      })
    return () => {
      alive = false
    }
  }, [])

  const today = useMemo(() => localDate(new Date()), [])
  const yesterday = useMemo(() => {
    const d = new Date()
    d.setDate(d.getDate() - 1)
    return localDate(d)
  }, [])

  const { lo, hi } = useMemo(() => {
    if (range === "30d") {
      const d = new Date()
      d.setDate(d.getDate() - 29)
      return { lo: localDate(d), hi: today }
    }
    if (range === "custom") {
      return { lo: from || "0000-01-01", hi: to || "9999-12-31" }
    }
    return { lo: "0000-01-01", hi: "9999-12-31" }
  }, [range, from, to, today])

  // 区间过滤 → 按模型汇总 + 每日总量
  const filtered = useMemo(
    () => (records ?? []).filter((r) => r.date >= lo && r.date <= hi),
    [records, lo, hi],
  )

  const totals = useMemo(() => {
    let input = 0
    let output = 0
    let calls = 0
    for (const r of filtered) {
      input += r.input
      output += r.output
      calls += r.input + r.output > 0 ? 1 : 0
    }
    return { input, output, all: input + output, calls }
  }, [filtered])

  // 今天/昨天的全天统计，不受上方筛选影响
  const dayStats = useMemo(() => {
    const stat = (day: string) => {
      let input = 0
      let output = 0
      let calls = 0
      for (const r of records ?? []) {
        if (r.date !== day) continue
        input += r.input
        output += r.output
        if (r.input + r.output > 0) calls += 1
      }
      return { input, output, all: input + output, calls }
    }
    return { today: stat(today), yesterday: stat(yesterday) }
  }, [records, today, yesterday])

  const byModel = useMemo(() => {
    const m = new Map<string, number>()
    for (const r of filtered) m.set(r.model || "(未知)", (m.get(r.model || "(未知)") ?? 0) + r.input + r.output)
    return [...m.entries()].sort((a, b) => b[1] - a[1])
  }, [filtered])

  const chart = useMemo(() => {
    // Recharts 数据：每行 = 一天，列为各模型 token；模型顺序与图例一致（按总量降序）
    const days = [...new Set(filtered.map((r) => r.date))].sort()
    const idx = new Map(days.map((d, i) => [d, i]))
    const rows = days.map((d) => {
      const row: Record<string, string | number> = { date: d }
      for (const [m] of byModel) row[m] = 0
      return row
    })
    for (const r of filtered) {
      const row = rows[idx.get(r.date)!]
      const key = r.model || "(未知)"
      row[key] = ((row[key] as number) ?? 0) + r.input + r.output
    }
    return { days, rows, hasData: filtered.some((r) => r.input + r.output > 0) }
  }, [filtered, byModel])

  const dateText =
    range === "all"
      ? "全部时间"
      : range === "custom"
        ? from && to
          ? `${from} ~ ${to}`
          : "选择起止日期"
        : lo === hi
          ? lo
          : `${lo} ~ ${hi}`

  if (records === null) {
    return <div className="text-muted-foreground py-6 text-center text-sm">加载用量数据…</div>
  }

  return (
    <div className="flex flex-col gap-3">
      {/* 区间选择 */}
      <div className="flex flex-wrap items-center gap-2">
        <select
          value={range}
          onChange={(e) => setRange(e.target.value as RangeId)}
          className="bg-background border-input rounded-md border px-2 py-1 text-xs"
        >
          {RANGES.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
        </select>
        <span className="text-muted-foreground text-xs">{dateText}</span>
        {range === "custom" && (
          <div className="flex items-center gap-1.5 text-xs">
            <input
              type="date"
              value={from}
              onChange={(e) => setFrom(e.target.value)}
              className="bg-background border-input rounded-md border px-2 py-1"
            />
            <span className="text-muted-foreground">至</span>
            <input
              type="date"
              value={to}
              onChange={(e) => setTo(e.target.value)}
              className="bg-background border-input rounded-md border px-2 py-1"
            />
          </div>
        )}
      </div>

      {/* 汇总卡片：今天/昨天固定显示，不随筛选变化 */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
        <StatCard label="总 token" value={fmtK(totals.all)} sub={`${totals.calls} 次对话`} />
        <StatCard label="今天" value={fmtK(dayStats.today.all)} sub={`${dayStats.today.calls} 次对话`} />
        <StatCard label="昨天" value={fmtK(dayStats.yesterday.all)} sub={`${dayStats.yesterday.calls} 次对话`} />
        <StatCard label="输入" value={fmtK(totals.input)} />
        <StatCard label="输出" value={fmtK(totals.output)} />
      </div>

      {/* 折线图 */}
      <div className="bg-muted/30 rounded-lg border p-3">
        <div className="text-muted-foreground mb-2 flex items-center gap-1.5 text-xs">
          <BarChart3Icon className="size-3.5" /> 每日 token 用量（按模型）
        </div>
        {chart.days.length === 0 || !chart.hasData ? (
          <div className="text-muted-foreground py-8 text-center text-xs">该区间没有用量数据</div>
        ) : (
          <ResponsiveContainer width="100%" height={200}>
            <LineChart data={chart.rows} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
              <CartesianGrid vertical={false} stroke="var(--border)" strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                tickFormatter={(d) => String(d).slice(5)}
                tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                tickLine={false}
                axisLine={false}
                minTickGap={24}
              />
              <YAxis
                tickFormatter={fmtK}
                tick={{ fontSize: 10, fill: "var(--muted-foreground)" }}
                tickLine={false}
                axisLine={false}
                width={38}
              />
              <Tooltip content={<UsageTooltip />} cursor={{ stroke: "var(--muted-foreground)" }} />
              {byModel.map(([m], i) => (
                <Line
                  key={m}
                  type="monotone"
                  dataKey={m}
                  stroke={MODEL_COLORS[i % MODEL_COLORS.length]}
                  strokeWidth={1.6}
                  dot={chart.days.length <= 31 ? { r: 2 } : false}
                  activeDot={{ r: 3 }}
                />
              ))}
            </LineChart>
          </ResponsiveContainer>
        )}
        {/* 图例 = 各模型总量 */}
        {byModel.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
            {byModel.map(([m, v], i) => (
              <span key={m} className="flex items-center gap-1.5 text-[11px]">
                <span
                  className="size-2 shrink-0 rounded-full"
                  style={{ background: MODEL_COLORS[i % MODEL_COLORS.length] }}
                />
                <span className="text-muted-foreground truncate max-w-40" title={m}>
                  {m}
                </span>
                <span className="font-mono">{fmtK(v)}</span>
              </span>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="bg-background rounded-lg border px-3 py-2">
      <div className="text-muted-foreground text-[11px]">{label}</div>
      <div className="truncate text-lg font-semibold tabular-nums">{value}</div>
      {sub && <div className="text-muted-foreground text-[11px]">{sub}</div>}
    </div>
  )
}

/** 图表 hover 提示：日期 + 各模型当日 token，多个模型时附合计 */
function UsageTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  label?: string | number
  payload?: Array<{ dataKey?: string | number; value?: number | string; color?: string; stroke?: string }>
}) {
  if (!active || !payload?.length) return null
  const items = payload.filter((p) => Number(p.value ?? 0) > 0)
  if (items.length === 0) return null
  const total = items.reduce((s, p) => s + Number(p.value ?? 0), 0)
  return (
    <div className="bg-popover text-popover-foreground rounded-md border px-2.5 py-1.5 text-xs shadow-md">
      <div className="text-muted-foreground mb-1">{label}</div>
      {items.map((p) => (
        <div key={p.dataKey} className="flex items-center gap-1.5 leading-5">
          <span className="size-2 shrink-0 rounded-full" style={{ background: p.color ?? p.stroke }} />
          <span className="text-muted-foreground">{p.dataKey}</span>
          <span className="ml-auto pl-4 font-mono">{Number(p.value).toLocaleString()}</span>
        </div>
      ))}
      {items.length > 1 && (
        <div className="mt-1 flex items-center gap-1.5 border-t pt-1 leading-5">
          <span className="text-muted-foreground">合计</span>
          <span className="ml-auto font-mono">{total.toLocaleString()}</span>
        </div>
      )}
    </div>
  )
}
