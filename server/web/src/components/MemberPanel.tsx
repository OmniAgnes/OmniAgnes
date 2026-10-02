import { useEffect, useState } from "react"

import { api, type CreditRecord, type MemberUsage, type UsageLog, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Badge, Card, CardContent, CardHeader, CardTitle, Pager, StatCard, Table, Tbody, Td, Th, Thead, Tr } from "@/components/ui/layout"

export type MemberSection = "account" | "usage"

const SECTIONS: { id: MemberSection; label: string }[] = [
  { id: "account", label: "账号" },
  { id: "usage", label: "使用量" },
]

/** 积分流水原因展示：daily_claim=每日领取，其余原样（如 管理调整/AI 消耗） */
export function reasonLabel(reason: string): string {
  if (reason === "daily_claim") return "每日领取"
  return reason
}

/** 请求状态展示：ok=已计费，insufficient=积分不足，error=失败 */
export function StatusBadge({ status }: { status: string }) {
  if (status === "insufficient") return <Badge variant="destructive">积分不足</Badge>
  if (status === "error") return <Badge variant="outline">失败</Badge>
  return <Badge variant="secondary">已计费</Badge>
}

/** 会员面板：左侧菜单（账号/使用量）+ 右侧内容。section 由 URL 路由（#/me/xxx）驱动。 */
export function MemberPanel({
  section,
  user,
  onNav,
  onLogout,
}: {
  section: MemberSection
  user: User
  onNav: (s: MemberSection) => void
  onLogout: () => void
}) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:gap-6">
      <nav className="flex shrink-0 gap-1 overflow-x-auto md:w-40 md:flex-col">
        {SECTIONS.map((s) => (
          <button
            key={s.id}
            onClick={() => onNav(s.id)}
            className={`shrink-0 cursor-pointer rounded-md px-3 py-1.5 text-left text-sm transition-colors ${
              section === s.id
                ? "bg-secondary text-secondary-foreground font-medium"
                : "text-muted-foreground hover:bg-accent"
            }`}
          >
            {s.label}
          </button>
        ))}
      </nav>
      <div className="min-w-0 flex-1">
        {section === "usage" ? <UsageTab user={user} /> : <AccountTab user={user} onLogout={onLogout} />}
      </div>
    </div>
  )
}

/** 账号页：账号信息（积分余额/退出）。 */
function AccountTab({ user, onLogout }: { user: User; onLogout: () => void }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>账号信息</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-1 text-sm">
        <div className="flex justify-between">
          <span className="text-muted-foreground">手机号</span>
          <span>{user.phone}</span>
        </div>
        <div className="flex justify-between">
          <span className="text-muted-foreground">剩余积分</span>
          <span className="font-semibold">{user.balance}</span>
        </div>
        <div className="mt-2 flex justify-end gap-2">
          {user.is_admin && <span className="text-primary self-center text-xs font-medium">管理员</span>}
          <Button variant="outline" size="sm" onClick={onLogout}>
            退出登录
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

type UsageTabId = "ai" | "spent" | "got"

const USAGE_TABS: { id: UsageTabId; label: string }[] = [
  { id: "ai", label: "AI 请求记录" },
  { id: "spent", label: "消耗记录" },
  { id: "got", label: "获取记录" },
]

/** 使用量页：积分总览 + AI 请求记录 / 积分消耗 / 积分获取（各列表独立分页）。 */
function UsageTab({ user }: { user: User }) {
  const [tab, setTab] = useState<UsageTabId>("ai")
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [data, setData] = useState<MemberUsage | null>(null)

  // 切页签回到第一页；页签/页码/每页条数变化重新拉取
  const switchTab = (t: UsageTabId) => {
    setTab(t)
    setPage(1)
  }
  const onPage = (p: number, size: number) => {
    if (size !== pageSize) setPageSize(size)
    setPage(p)
  }
  useEffect(() => {
    api.usage(tab, page, pageSize).then(setData).catch(() => setData(null))
  }, [tab, page, pageSize])

  const s = data?.summary
  const credit = data?.credit
  const items = data?.items ?? []

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <StatCard label="当前积分" value={user.balance} />
        <StatCard label="累计获取" value={credit?.got ?? 0} />
        <StatCard label="累计消耗" value={credit?.spent ?? 0} />
        <StatCard label="AI 请求" value={s?.requests ?? 0} hint={`${fmtNum(s?.input_tokens ?? 0)} / ${fmtNum(s?.output_tokens ?? 0)} token`} />
        <StatCard label="请求消耗积分" value={s?.credits ?? 0} />
      </div>

      {/* 分段切换：AI 请求记录 / 消耗 / 获取 */}
      <div className="inline-flex w-fit rounded-lg border p-0.5">
        {USAGE_TABS.map((t) => (
          <button
            key={t.id}
            onClick={() => switchTab(t.id)}
            className={`cursor-pointer rounded-md px-3 py-1 text-sm transition-colors ${
              tab === t.id ? "bg-secondary text-secondary-foreground font-medium" : "text-muted-foreground hover:bg-accent"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {tab === "ai" ? (
        <Card>
          <CardHeader>
            <CardTitle>AI 请求记录</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <Thead>
                <Tr>
                  <Th>时间</Th>
                  <Th>模型</Th>
                  <Th>Token（入/出）</Th>
                  <Th>积分</Th>
                  <Th>状态</Th>
                </Tr>
              </Thead>
              <Tbody>
                {(items as UsageLog[]).map((l) => (
                  <Tr key={l.id}>
                    <Td className="text-muted-foreground text-xs">{new Date(l.created_at).toLocaleString()}</Td>
                    <Td className="text-xs">{l.model || "—"}</Td>
                    <Td className="text-muted-foreground text-xs tabular-nums">
                      {fmtNum(l.input_tokens)} / {fmtNum(l.output_tokens)}
                    </Td>
                    <Td className="tabular-nums">{l.credits}</Td>
                    <Td>
                      <StatusBadge status={l.status} />
                    </Td>
                  </Tr>
                ))}
                {items.length === 0 && (
                  <tr>
                    <Td colSpan={5} className="text-muted-foreground text-center">
                      暂无记录
                    </Td>
                  </tr>
                )}
              </Tbody>
            </Table>
            <Pager page={data?.page ?? 1} pageSize={data?.page_size ?? pageSize} total={data?.total ?? 0} onChange={onPage} />
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>{tab === "spent" ? "消耗记录" : "获取记录"}</CardTitle>
          </CardHeader>
          <CardContent>
            <CreditTable rows={items as CreditRecord[]} />
            <Pager page={data?.page ?? 1} pageSize={data?.page_size ?? pageSize} total={data?.total ?? 0} onChange={onPage} />
          </CardContent>
        </Card>
      )}
    </div>
  )
}

/** 积分流水表（会员视角）：时间/变动/说明/到期。 */
export function CreditTable({ rows }: { rows: CreditRecord[] }) {
  return (
    <Table>
      <Thead>
        <Tr>
          <Th>时间</Th>
          <Th>变动</Th>
          <Th>说明</Th>
          <Th>到期</Th>
        </Tr>
      </Thead>
      <Tbody>
        {rows.map((r) => {
          // 已到期：到期时间已过（含剩余未耗尽的），整行置灰
          const expired = !!r.expires_at && new Date(r.expires_at).getTime() < Date.now()
          return (
            <Tr key={r.id} className={expired ? "text-muted-foreground/60" : undefined}>
              <Td className="text-muted-foreground text-xs">{new Date(r.created_at).toLocaleString()}</Td>
              <Td className={expired ? "" : r.amount >= 0 ? "text-emerald-600" : "text-destructive"}>
                {r.amount >= 0 ? "+" : ""}
                {r.amount}
              </Td>
              <Td className="text-xs">
                {reasonLabel(r.reason)}
                {r.model && <span className="text-muted-foreground ml-1">({r.model})</span>}
              </Td>
              <Td className="text-muted-foreground text-xs">
                {r.expires_at ? new Date(r.expires_at).toLocaleString() : "—"}
              </Td>
            </Tr>
          )
        })}
        {rows.length === 0 && (
          <tr>
            <Td colSpan={4} className="text-muted-foreground text-center">
              暂无记录
            </Td>
          </tr>
        )}
      </Tbody>
    </Table>
  )
}

function fmtNum(n: number): string {
  return n.toLocaleString()
}
