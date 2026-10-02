import * as React from "react"

import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"

export function Card({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn("bg-card text-card-foreground rounded-xl border shadow-sm", className)} {...props} />
  )
}
export const CardHeader = ({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("flex flex-col space-y-1.5 p-6", className)} {...p} />
)
export const CardTitle = ({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("text-lg leading-none font-semibold", className)} {...p} />
)
export const CardContent = ({ className, ...p }: React.HTMLAttributes<HTMLDivElement>) => (
  <div className={cn("p-6 pt-0", className)} {...p} />
)

/** 统计小卡：使用量页/管理端总览的数字卡片 */
export function StatCard({ label, value, hint }: { label: string; value: React.ReactNode; hint?: string }) {
  return (
    <Card className="p-4">
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="mt-1 text-2xl leading-tight font-semibold tabular-nums">{value}</div>
      {hint && <div className="text-muted-foreground mt-0.5 text-xs">{hint}</div>}
    </Card>
  )
}

const PAGE_SIZES = [20, 50, 100]

/** 分页条：总数 + 每页条数 + 上/下一页。total=0 时不渲染 */
export function Pager({
  page,
  pageSize,
  total,
  onChange,
}: {
  page: number
  pageSize: number
  total: number
  onChange: (page: number, pageSize: number) => void
}) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (total === 0) return null
  return (
    <div className="mt-3 flex flex-wrap items-center justify-end gap-2 text-xs">
      <span className="text-muted-foreground">共 {total} 条</span>
      <select
        value={pageSize}
        onChange={(e) => onChange(1, Number(e.target.value))}
        className="border-input bg-background h-7 rounded-md border px-1 text-xs"
      >
        {PAGE_SIZES.map((n) => (
          <option key={n} value={n}>
            {n} 条/页
          </option>
        ))}
      </select>
      <Button
        size="sm"
        variant="outline"
        className="h-7 px-2"
        disabled={page <= 1}
        onClick={() => onChange(page - 1, pageSize)}
      >
        上一页
      </Button>
      <span className="text-muted-foreground tabular-nums">
        {page} / {pages}
      </span>
      <Button
        size="sm"
        variant="outline"
        className="h-7 px-2"
        disabled={page >= pages}
        onClick={() => onChange(page + 1, pageSize)}
      >
        下一页
      </Button>
    </div>
  )
}

export function Badge({
  variant = "default",
  className,
  ...p
}: React.HTMLAttributes<HTMLSpanElement> & { variant?: "default" | "secondary" | "destructive" | "outline" }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium",
        variant === "default" && "border-transparent bg-primary text-primary-foreground",
        variant === "secondary" && "border-transparent bg-secondary text-secondary-foreground",
        variant === "destructive" && "border-transparent bg-destructive text-white",
        variant === "outline" && "text-foreground",
        className,
      )}
      {...p}
    />
  )
}

export const Table = ({ className, ...p }: React.TableHTMLAttributes<HTMLTableElement>) => (
  <div className="w-full overflow-auto">
    <table className={cn("w-full caption-bottom text-sm", className)} {...p} />
  </div>
)
export const Thead = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <thead {...p} />
export const Tbody = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <tbody {...p} />
export const Tr = (p: React.HTMLAttributes<HTMLTableRowElement>) => (
  <tr className="border-b transition-colors hover:bg-muted/50" {...p} />
)
export const Th = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => (
  <th className={cn("h-10 px-3 text-left align-middle font-medium text-muted-foreground", className)} {...p} />
)
export const Td = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => (
  <td className={cn("px-3 py-2 align-middle", className)} {...p} />
)

/** 轻量对话框：居中的固定浮层（含遮罩与右上角关闭） */
export function Dialog({
  open,
  onOpenChange,
  title,
  children,
  className,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title?: React.ReactNode
  children: React.ReactNode
  className?: string
}) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="bg-background/70 fixed inset-0 backdrop-blur-[2px]" onClick={() => onOpenChange(false)} />
      <div
        className={cn(
          "bg-card text-card-foreground relative z-10 w-full max-w-md rounded-xl border p-6 shadow-lg",
          className,
        )}
      >
        <div className="mb-4 flex items-center justify-between">
          {title ? <h2 className="text-lg font-semibold">{title}</h2> : <span />}
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} className="text-muted-foreground">
            ✕
          </Button>
        </div>
        {children}
      </div>
    </div>
  )
}