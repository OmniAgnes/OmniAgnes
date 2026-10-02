import { useEffect, useState } from "react"
import { CalendarClockIcon, ChevronLeftIcon, Loader2Icon, PencilLineIcon, PlayIcon, Trash2Icon } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import { useI18n } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import type { Project, ScheduledTaskInfo } from "@/types"
import type { ModelGroup } from "@/components/Chat"

/** 时间展示：MM-DD HH:mm；空值显示「—」 */
function fmtTime(iso?: string): string {
  if (!iso) return "—"
  const d = new Date(iso)
  if (isNaN(d.getTime())) return "—"
  const p = (n: number) => String(n).padStart(2, "0")
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function scheduleDesc(s: ScheduledTaskInfo, t: (k: string, v?: Record<string, string | number>) => string): string {
  return s.interval_days <= 1 ? t("sc.descDaily", { t: s.at }) : t("sc.descEvery", { n: s.interval_days, t: s.at })
}

/** 表单字段（添加与编辑共用） */
function ScheduleFields(props: {
  name: string
  onName: (v: string) => void
  prompt: string
  onPrompt: (v: string) => void
  model: string
  onModel: (v: string) => void
  intervalDays: number
  onInterval: (v: number) => void
  at: string
  onAt: (v: string) => void
  projectId: string
  onProject: (v: string) => void
  models: ModelGroup[]
  projects: Project[]
}) {
  const { models, projects } = props
  const { t } = useI18n()
  return (
    <>
      <Input
        value={props.name}
        onChange={(e) => props.onName(e.target.value)}
        placeholder={t("sc.namePh")}
        className="h-8 text-sm"
      />
      <Textarea
        value={props.prompt}
        onChange={(e) => props.onPrompt(e.target.value)}
        placeholder={t("sc.promptPh")}
        className="min-h-16 resize-none"
      />
      <div className="grid grid-cols-2 gap-2">
        <div className="flex flex-col gap-1">
          <Label className="text-muted-foreground text-[11px]">{t("sc.model")}</Label>
          <select
            value={props.model}
            onChange={(e) => props.onModel(e.target.value)}
            className="bg-background h-8 rounded-md border px-2 text-xs outline-none"
          >
            <option value="">{t("sc.defaultModel")}</option>
            {models.map((g) => (
              <optgroup key={g.provider || "o"} label={g.provider || t("a.other")}>
                {g.models.map((m) => (
                  <option key={m} value={m}>
                    {m}
                  </option>
                ))}
              </optgroup>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-muted-foreground text-[11px]">{t("sc.workDir")}</Label>
          <select
            value={props.projectId}
            onChange={(e) => props.onProject(e.target.value)}
            className="bg-background h-8 rounded-md border px-2 text-xs outline-none"
          >
            <option value="">{t("sc.anyDir")}</option>
            {projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="flex items-end gap-2">
        <div className="flex flex-col gap-1">
          <Label className="text-muted-foreground text-[11px]">{t("sc.freq")}</Label>
          <div className="flex items-center gap-1">
            <span className="text-muted-foreground text-xs">{t("sc.every")}</span>
            <Input
              type="number"
              min={1}
              value={props.intervalDays}
              onChange={(e) => props.onInterval(Number(e.target.value) || 1)}
              className="h-8 w-16 px-2 text-xs"
            />
            <span className="text-muted-foreground text-xs">{t("sc.day")}</span>
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <Label className="text-muted-foreground text-[11px]">{t("sc.time")}</Label>
          <Input
            type="time"
            value={props.at}
            onChange={(e) => props.onAt(e.target.value)}
            className="h-8 w-28 px-2 text-xs"
          />
        </div>
      </div>
    </>
  )
}

interface SchedulesPageProps {
  /** 全部可选模型（供应商分组） */
  models: ModelGroup[]
  projects: Project[]
  /** 手动触发成功后回调：让 App 刷新任务列表，立即看到新执行的任务 */
  onRan?: () => void
  /** 返回聊天主界面（点页头返回按钮） */
  onBack: () => void
}

/** 计划任务页：占据聊天主区。添加表单在页首，计划列表在下（可滚动）；编辑走弹窗。 */
export function SchedulesPage({ models, projects, onRan, onBack }: SchedulesPageProps) {
  const { t } = useI18n()
  const [list, setList] = useState<ScheduledTaskInfo[]>([])
  const [loading, setLoading] = useState(false)
  const [err, setErr] = useState("")

  // 添加表单
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [model, setModel] = useState("")
  const [intervalDays, setIntervalDays] = useState(1)
  const [at, setAt] = useState("09:00")
  const [projectId, setProjectId] = useState("")
  const [saving, setSaving] = useState(false)

  // 编辑弹窗：editing 非空即打开
  const [editing, setEditing] = useState<ScheduledTaskInfo | null>(null)
  const [eName, setEName] = useState("")
  const [ePrompt, setEPrompt] = useState("")
  const [eModel, setEModel] = useState("")
  const [eInterval, setEInterval] = useState(1)
  const [eAt, setEAt] = useState("09:00")
  const [eProject, setEProject] = useState("")
  const [eSaving, setESaving] = useState(false)

  const [runningId, setRunningId] = useState("")

  const reload = () => {
    setLoading(true)
    api.schedules()
      .then((r) => setList(r.schedules))
      .catch((e) => setErr(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    setErr("")
    reload()
  }, [])

  const resetForm = () => {
    setName("")
    setPrompt("")
    setModel("")
    setIntervalDays(1)
    setAt("09:00")
    setProjectId("")
  }

  const create = async () => {
    if (!prompt.trim() || saving) return
    setSaving(true)
    setErr("")
    try {
      await api.createSchedule({
        name: name.trim(),
        prompt: prompt.trim(),
        model,
        interval_days: Math.max(1, Math.floor(intervalDays || 1)),
        at,
        project_id: projectId,
      })
      resetForm()
      reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const startEdit = (s: ScheduledTaskInfo) => {
    setEditing(s)
    setEName(s.name)
    setEPrompt(s.prompt)
    setEModel(s.model ?? "")
    setEInterval(s.interval_days)
    setEAt(s.at)
    setEProject(s.project_id ?? "")
    setErr("")
  }

  const saveEdit = async () => {
    if (!editing || !ePrompt.trim() || eSaving) return
    setESaving(true)
    setErr("")
    try {
      await api.updateSchedule(editing.id, {
        name: eName.trim(),
        prompt: ePrompt.trim(),
        model: eModel,
        interval_days: Math.max(1, Math.floor(eInterval || 1)),
        at: eAt,
        project_id: eProject,
      })
      setEditing(null)
      reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setESaving(false)
    }
  }

  const toggle = async (s: ScheduledTaskInfo) => {
    try {
      await api.updateSchedule(s.id, { enabled: !s.enabled })
      reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const remove = async (id: string) => {
    try {
      if (editing?.id === id) setEditing(null)
      await api.deleteSchedule(id)
      reload()
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    }
  }

  const runNow = async (id: string) => {
    setRunningId(id)
    setErr("")
    try {
      await api.runSchedule(id)
      // 后台立即建任务开跑：稍等片刻刷任务列表，让用户看到新任务出现
      onRan?.()
      setTimeout(() => onRan?.(), 2000)
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e))
    } finally {
      setRunningId("")
    }
  }

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
      <div className="omni-scroll min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto flex max-w-3xl flex-col gap-4 px-4 py-6">
          {/* 页头 */}
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon-sm" className="text-muted-foreground" onClick={onBack} title={t("sc.back")}>
              <ChevronLeftIcon />
            </Button>
            <CalendarClockIcon className="text-muted-foreground size-4" />
            <div className="min-w-0">
              <div className="text-base font-semibold">{t("sc.title")}</div>
              <div className="text-muted-foreground text-xs">
                {t("sc.sub")}
              </div>
            </div>
          </div>

          {/* 添加表单：页首 */}
          <div className="flex flex-col gap-2.5 rounded-xl border p-3">
            <div className="text-muted-foreground text-xs font-medium">{t("sc.addForm")}</div>
            <ScheduleFields
              name={name}
              onName={setName}
              prompt={prompt}
              onPrompt={setPrompt}
              model={model}
              onModel={setModel}
              intervalDays={intervalDays}
              onInterval={setIntervalDays}
              at={at}
              onAt={setAt}
              projectId={projectId}
              onProject={setProjectId}
              models={models}
              projects={projects}
            />
            <Button size="sm" className="ml-auto h-8 px-3" disabled={!prompt.trim() || saving} onClick={() => void create()}>
              {saving ? t("sc.adding") : t("sc.add")}
            </Button>
          </div>

          {/* 计划列表：任务多时滚动 */}
          <div className="flex flex-col gap-1.5">
            {loading && list.length === 0 && (
              <div className="text-muted-foreground flex items-center gap-2 px-1 py-2 text-xs">
                <Loader2Icon className="size-3.5 animate-spin" /> {t("sc.loading")}
              </div>
            )}
            {!loading && list.length === 0 && (
              <div className="text-muted-foreground px-1 py-2 text-xs">{t("sc.empty")}</div>
            )}
            {list.map((s) => (
              <div
                key={s.id}
                className={cn("group rounded-xl border px-3 py-2", !s.enabled && "opacity-60")}
              >
                <div className="flex items-center gap-2">
                  <CalendarClockIcon className="text-muted-foreground size-3.5 shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium" title={s.prompt}>
                    {s.name}
                  </span>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 gap-1 px-1.5 text-[11px]"
                    disabled={runningId === s.id}
                    onClick={() => void runNow(s.id)}
                    title={t("sc.runHint")}
                  >
                    {runningId === s.id ? (
                      <Loader2Icon className="size-3 animate-spin" />
                    ) : (
                      <PlayIcon className="size-3" />
                    )}
                    {t("sc.run")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 gap-1 px-1.5 text-[11px]"
                    onClick={() => startEdit(s)}
                    title={t("sc.edit")}
                  >
                    <PencilLineIcon className="size-3" />
                    {t("sc.edit")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="h-6 px-1.5 text-[11px]"
                    onClick={() => void toggle(s)}
                    title={s.enabled ? t("sc.pauseTitle") : t("sc.enable")}
                  >
                    {s.enabled ? t("sc.disable") : t("sc.enable")}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="text-muted-foreground hover:text-destructive h-6 px-1.5 text-[11px]"
                    onClick={() => void remove(s.id)}
                    title={t("sc.delete")}
                  >
                    <Trash2Icon className="size-3" />
                  </Button>
                </div>
                <div className="text-muted-foreground mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 pl-5 text-[11px]">
                  <span>{scheduleDesc(s, t)}</span>
                  <span>·</span>
                  <span>{s.model || t("sc.defaultModel")}</span>
                  <span>·</span>
                  <span>{t("sc.next", { t: fmtTime(s.next_run) })}</span>
                  {s.last_run && (
                    <>
                      <span>·</span>
                      <span>{t("sc.last", { t: fmtTime(s.last_run) })}</span>
                    </>
                  )}
                </div>
              </div>
            ))}
          </div>

          {err && <div className="text-destructive text-sm">{err}</div>}
        </div>
      </div>

      {/* 编辑弹窗 */}
      <Dialog open={!!editing} onOpenChange={(o) => !o && setEditing(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t("sc.editTitle", { name: editing?.name ?? "" })}</DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-2.5">
            <ScheduleFields
              name={eName}
              onName={setEName}
              prompt={ePrompt}
              onPrompt={setEPrompt}
              model={eModel}
              onModel={setEModel}
              intervalDays={eInterval}
              onInterval={setEInterval}
              at={eAt}
              onAt={setEAt}
              projectId={eProject}
              onProject={setEProject}
              models={models}
              projects={projects}
            />
            <div className="flex justify-end gap-2">
              <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>
                {t("sc.cancel")}
              </Button>
              <Button size="sm" disabled={!ePrompt.trim() || eSaving} onClick={() => void saveEdit()}>
                {eSaving ? t("sc.saving") : t("sc.saveChanges")}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
