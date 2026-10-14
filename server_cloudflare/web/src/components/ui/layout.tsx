import * as React from "react"
import { InboxIcon } from "lucide-react"

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

/** 统计小卡：使用量页/管理端总览的数字卡片；icon 显示在主色圆角小底上 */
export function StatCard({
  label,
  value,
  hint,
  icon,
}: {
  label: string
  value: React.ReactNode
  hint?: string
  icon?: React.ReactNode
}) {
  return (
    <Card className="p-4 transition-shadow hover:shadow-md">
      <div className="flex items-center justify-between gap-2">
        <div className="text-muted-foreground text-xs">{label}</div>
        {icon && (
          <span className="bg-primary/10 text-primary flex size-7 shrink-0 items-center justify-center rounded-lg [&_svg]:size-4">
            {icon}
          </span>
        )}
      </div>
      <div className="mt-1 text-2xl leading-tight font-semibold tabular-nums">{value}</div>
      {hint && <div className="text-muted-foreground mt-0.5 truncate text-xs">{hint}</div>}
    </Card>
  )
}

/** 面板侧栏导航：管理端/会员端共用；H5 下横排滑动，桌面端竖排。选中项主色浅底。 */
export function PanelNav({
  items,
  active,
  onNav,
}: {
  items: { id: string; label: string; icon?: React.ReactNode }[]
  active: string
  onNav: (id: string) => void
}) {
  return (
    <nav className="flex shrink-0 gap-1 overflow-x-auto md:w-40 md:flex-col">
      {items.map((s) => (
        <button
          key={s.id}
          onClick={() => onNav(s.id)}
          className={`flex shrink-0 cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-left text-sm transition-colors ${
            active === s.id
              ? "bg-primary/10 text-primary font-medium"
              : "text-muted-foreground hover:bg-accent hover:text-foreground"
          }`}
        >
          {s.icon && <span className="[&_svg]:size-4 [&_svg]:shrink-0">{s.icon}</span>}
          <span className="whitespace-nowrap">{s.label}</span>
        </button>
      ))}
    </nav>
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
export const Thead = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <thead className="bg-muted/40" {...p} />
export const Tbody = (p: React.HTMLAttributes<HTMLTableSectionElement>) => <tbody {...p} />
export const Tr = (p: React.HTMLAttributes<HTMLTableRowElement>) => (
  <tr className="border-b transition-colors hover:bg-muted/50" {...p} />
)
export const Th = ({ className, ...p }: React.ThHTMLAttributes<HTMLTableCellElement>) => (
  <th className={cn("h-10 px-3 text-left align-middle text-xs font-medium text-muted-foreground", className)} {...p} />
)
export const Td = ({ className, ...p }: React.TdHTMLAttributes<HTMLTableCellElement>) => (
  <td className={cn("px-3 py-2.5 align-middle", className)} {...p} />
)

/** 表格空状态行：图标 + 文案居中 */
export function EmptyRow({ colSpan, label = "暂无数据" }: { colSpan: number; label?: string }) {
  return (
    <tr>
      <td colSpan={colSpan} className="text-muted-foreground px-3 py-10 text-center">
        <div className="flex flex-col items-center gap-2">
          <InboxIcon className="size-7 opacity-40" />
          <span className="text-sm">{label}</span>
        </div>
      </td>
    </tr>
  )
}

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
      <div className="bg-foreground/25 fixed inset-0 animate-in fade-in-0 duration-200" onClick={() => onOpenChange(false)} />
      <div
        className={cn(
          "bg-card text-card-foreground relative z-10 w-full max-w-md rounded-xl border p-6 shadow-xl animate-in fade-in-0 zoom-in-95 duration-200",
          className,
        )}
      >
        <div className="mb-4 flex items-center justify-between">
          {title ? <h2 className="text-base font-semibold">{title}</h2> : <span />}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onOpenChange(false)}
            className="text-muted-foreground hover:text-foreground"
          >
            ✕
          </Button>
        </div>
        {children}
      </div>
    </div>
  )
}