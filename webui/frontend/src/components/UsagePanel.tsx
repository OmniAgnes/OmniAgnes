import { useEffect, useMemo, useState } from "react"
import { BarChart3Icon } from "lucide-react"

import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { UsageRecord } from "@/types"

/** 快捷区间 */
const RANGES = [
  { id: "today", label: "今天" },
  { id: "yesterday", label: "昨天" },
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
 * 快捷区间（今天/昨天/最近30天/总量）+ 自定义日期区间；
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
    if (range === "today") return { lo: today, hi: today }
    if (range === "yesterday") return { lo: yesterday, hi: yesterday }
    if (range === "30d") {
      const d = new Date()
      d.setDate(d.getDate() - 29)
      return { lo: localDate(d), hi: today }
    }
    if (range === "custom") {
      return { lo: from || "0000-01-01", hi: to || "9999-12-31" }
    }
    return { lo: "0000-01-01", hi: "9999-12-31" }
  }, [range, from, to, today, yesterday])

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

  const byModel = useMemo(() => {
    const m = new Map<string, number>()
    for (const r of filtered) m.set(r.model || "(未知)", (m.get(r.model || "(未知)") ?? 0) + r.input + r.output)
    return [...m.entries()].sort((a, b) => b[1] - a[1])
  }, [filtered])

  const chart = useMemo(() => {
    // 图表横轴：区间内出现过的日期（升序），以及每个模型在这些天的总量
    const days = [...new Set(filtered.map((r) => r.date))].sort()
    const idx = new Map(days.map((d, i) => [d, i]))
    const n = days.length
    const data = new Map<string, number[]>()
    for (const r of filtered) {
      const key = r.model || "(未知)"
      let arr = data.get(key)
      if (!arr) {
        arr = Array(n).fill(0)
        data.set(key, arr)
      }
      arr[idx.get(r.date)!] += r.input + r.output
    }
    return { days, data }
  }, [filtered])

  const maxVal = useMemo(() => {
    let m = 0
    for (const arr of chart.data.values()) for (const v of arr) if (v > m) m = v
    return m
  }, [chart])

  if (records === null) {
    return <div className="text-muted-foreground py-6 text-center text-sm">加载用量数据…</div>
  }

  return (
    <div className="flex flex-col gap-3">
      {/* 区间选择 */}
      <div className="flex flex-wrap items-center gap-1.5">
        <div className="bg-muted/50 flex rounded-md p-0.5">
          {RANGES.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setRange(t.id)}
              className={cn(
                "rounded-[5px] px-2.5 py-1 text-xs cursor-pointer transition-colors",
                range === t.id
                  ? "bg-background text-foreground shadow-xs"
                  : "text-muted-foreground hover:text-foreground",
              )}
            >
              {t.label}
            </button>
          ))}
        </div>
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

      {/* 汇总卡片 */}
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        <StatCard label="总 token" value={fmtK(totals.all)} sub={`${totals.calls} 次对话`} />
        <StatCard label="输入" value={fmtK(totals.input)} />
        <StatCard label="输出" value={fmtK(totals.output)} />
        <StatCard
          label="区间"
          value={
            range === "all"
              ? "全部"
              : range === "today"
                ? "今天"
                : range === "yesterday"
                  ? "昨天"
                  : range === "30d"
                    ? "30天"
                    : `${from || "…"}~${to || "…"}`
          }
        />
      </div>

      {/* 折线图 */}
      <div className="bg-muted/30 rounded-lg border p-3">
        <div className="text-muted-foreground mb-2 flex items-center gap-1.5 text-xs">
          <BarChart3Icon className="size-3.5" /> 每日 token 用量（按模型）
        </div>
        {chart.days.length === 0 || maxVal === 0 ? (
          <div className="text-muted-foreground py-8 text-center text-xs">该区间没有用量数据</div>
        ) : (
          <LineChart days={chart.days} data={chart.data} maxVal={maxVal} />
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

/** 轻量 SVG 多折线图：横轴等分日期，纵轴 0..maxVal */
function LineChart({ days, data, maxVal }: { days: string[]; data: Map<string, number[]>; maxVal: number }) {
  const W = 640
  const H = 180
  const PAD = { l: 34, r: 8, t: 8, b: 18 }
  const iw = W - PAD.l - PAD.r
  const ih = H - PAD.t - PAD.b
  const x = (i: number) => PAD.l + (days.length === 1 ? iw / 2 : (i / (days.length - 1)) * iw)
  const y = (v: number) => PAD.t + ih - (v / maxVal) * ih
  const yTicks = [0, 0.5, 1].map((f) => Math.round(maxVal * f))
  const xEvery = Math.max(1, Math.ceil(days.length / 8))

  return (
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full" role="img" aria-label="用量折线图">
      {/* 网格与 Y 轴刻度 */}
      {yTicks.map((v, i) => (
        <g key={i}>
          <line x1={PAD.l} x2={W - PAD.r} y1={y(v)} y2={y(v)} className="stroke-border" strokeWidth={1} />
          <text x={PAD.l - 4} y={y(v) + 3} textAnchor="end" className="fill-muted-foreground text-[9px]">
            {fmtK(v)}
          </text>
        </g>
      ))}
      {/* X 轴日期 */}
      {days.map((d, i) =>
        i % xEvery === 0 || i === days.length - 1 ? (
          <text key={d} x={x(i)} y={H - 4} textAnchor="middle" className="fill-muted-foreground text-[9px]">
            {d.slice(5)}
          </text>
        ) : null,
      )}
      {/* 折线 */}
      {[...data.entries()].map(([model, arr], mi) => {
        const pts = arr.map((v, i) => `${x(i)},${y(v)}`).join(" ")
        return (
          <g key={model}>
            <polyline
              points={pts}
              fill="none"
              stroke={MODEL_COLORS[mi % MODEL_COLORS.length]}
              strokeWidth={1.6}
              strokeLinejoin="round"
            />
            {days.length <= 31 &&
              arr.map((v, i) => (
                <circle key={i} cx={x(i)} cy={y(v)} r={2} fill={MODEL_COLORS[mi % MODEL_COLORS.length]}>
                  <title>{`${days[i]} ${model}: ${v.toLocaleString()}`}</title>
                </circle>
              ))}
          </g>
        )
      })}
    </svg>
  )
}
