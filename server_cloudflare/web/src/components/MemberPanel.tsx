import { useEffect, useState } from "react"
import { ActivityIcon, UserIcon } from "lucide-react"

import { api, type User } from "@/lib/api"
import { Badge, Card, CardContent, CardHeader, CardTitle, EmptyRow, Pager, PanelNav, Table, Tbody, Td, Th, Thead, Tr } from "@/components/ui/layout"

export type MemberSection = "account" | "usage"

const SECTIONS: { id: MemberSection; label: string; icon: React.ReactNode }[] = [
  { id: "account", label: "账号", icon: <UserIcon /> },
  { id: "usage", label: "使用量", icon: <ActivityIcon /> },
]

/** 积分流水行（point_logs） */
interface PointLog {
  id: number
  change: number
  after: number
  type: string
  type_label: string
  remark: string
  created_at: string
}
/** AI 用量行（ai_usages） */
interface UsageRow {
  id: number
  model: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  points: number
  created_at: string
}

/** 会员面板：左侧菜单（账号/使用量）+ 右侧内容。section 由 URL 路由（#/me/xxx）驱动。 */
export function MemberPanel({
  section,
  user,
  onNav,
}: {
  section: MemberSection
  user: User
  onNav: (s: MemberSection) => void
}) {
  return (
    <div className="flex flex-col gap-4 md:flex-row md:gap-6">
      <PanelNav items={SECTIONS} active={section} onNav={(id) => onNav(id as MemberSection)} />
      <div className="min-w-0 flex-1">
        {section === "usage" ? <UsageTab /> : <AccountTab user={user} />}
      </div>
    </div>
  )
}

/** 账号页：账号信息（邮箱/角色）+ 积分余额 + 积分流水。 */
function AccountTab({ user }: { user: User }) {
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<{ items: PointLog[]; total: number; page: number } | null>(null)

  useEffect(() => {
    let alive = true
    api
      .myPoints("", page, 20)
      .then((r) => alive && setRows({ items: r.items as PointLog[], total: r.total, page: r.page }))
      .catch(() => alive && setRows(null))
    return () => {
      alive = false
    }
  }, [user.id, page])

  const list = rows?.items ?? []
  const total = rows?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>账号信息</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4 text-sm">
          <div className="flex items-center justify-between">
            <span className="text-muted-foreground">邮箱</span>
            <span className="flex items-center gap-2">
              <span className="font-medium">{user.email}</span>
              {user.role === "admin" && <Badge>管理员</Badge>}
            </span>
          </div>
          <div className="border-primary/15 bg-primary/5 flex items-center justify-between rounded-lg border p-3">
            <span className="text-muted-foreground">剩余积分</span>
            <span className="text-primary text-2xl font-semibold tabular-nums">{user.points}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>积分流水</CardTitle>
        </CardHeader>
        <CardContent>
          <Table>
            <Thead>
              <Tr>
                <Th>时间</Th>
                <Th>变动</Th>
                <Th>说明</Th>
              </Tr>
            </Thead>
            <Tbody>
              {list.map((r) => (
                <Tr key={r.id}>
                  <Td className="text-muted-foreground text-xs">{new Date(r.created_at).toLocaleString()}</Td>
                  <Td className={r.change >= 0 ? "text-emerald-600" : "text-destructive"}>
                    {r.change >= 0 ? "+" : ""}
                    {r.change}
                  </Td>
                  <Td className="text-xs">{r.remark || r.type_label || r.type}</Td>
                </Tr>
              ))}
              {list.length === 0 && <EmptyRow colSpan={3} label="暂无记录" />}
            </Tbody>
          </Table>
          {pages > 1 && <Pager page={page} pageSize={20} total={total} onChange={(p) => setPage(p)} />}
        </CardContent>
      </Card>
    </div>
  )
}

/** 使用量页：AI 请求记录（ai_usages）。 */
function UsageTab() {
  const [page, setPage] = useState(1)
  const [rows, setRows] = useState<{ items: UsageRow[]; total: number; page: number } | null>(null)

  useEffect(() => {
    let alive = true
    api
      .myUsages(page, 20)
      .then((r) => alive && setRows({ items: r.items as UsageRow[], total: r.total, page: r.page }))
      .catch(() => alive && setRows(null))
    return () => {
      alive = false
    }
  }, [page])

  const list = rows?.items ?? []
  const total = rows?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / 20))

  return (
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
              <Th>Token</Th>
              <Th>积分</Th>
            </Tr>
          </Thead>
          <Tbody>
            {list.map((l) => (
              <Tr key={l.id}>
                <Td className="text-muted-foreground text-xs">{new Date(l.created_at).toLocaleString()}</Td>
                <Td className="text-xs">{l.model || "—"}</Td>
                <Td className="text-muted-foreground text-xs tabular-nums">{l.total_tokens}</Td>
                <Td className="tabular-nums">{l.points}</Td>
              </Tr>
            ))}
            {list.length === 0 && <EmptyRow colSpan={4} label="暂无请求记录" />}
          </Tbody>
        </Table>
        {pages > 1 && <Pager page={page} pageSize={20} total={total} onChange={(p) => setPage(p)} />}
      </CardContent>
    </Card>
  )
}
