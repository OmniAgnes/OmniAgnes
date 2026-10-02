import { type CreditRecord, type User } from "@/lib/api"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle, Table, Tbody, Td, Th, Thead, Tr } from "@/components/ui/layout"

/** 积分流水原因展示：daily_claim=每日领取，其余原样（如 管理调整/消耗） */
function reasonLabel(reason: string): string {
  if (reason === "daily_claim") return "每日领取"
  return reason
}

/** 我的页：账号信息 + 积分流水（无「我的」页标题，卡片即内容）。 */
export function MePanel({ user, records, onLogout }: { user: User; records: CreditRecord[]; onLogout: () => void }) {
  return (
    <div className="flex flex-col gap-4">
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

      <Card>
        <CardHeader>
          <CardTitle>积分使用记录</CardTitle>
        </CardHeader>
        <CardContent>
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
              {records.map((r) => {
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
              {records.length === 0 && (
                <tr>
                  <Td colSpan={4} className="text-muted-foreground text-center">
                    暂无记录
                  </Td>
                </tr>
              )}
            </Tbody>
          </Table>
        </CardContent>
      </Card>
    </div>
  )
}
