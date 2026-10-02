import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { CheckIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-react"

import type { ModelGroup } from "@/components/Chat"
import { useI18n } from "@/lib/i18n"
import { server, type ServerModel, type ServerUser } from "@/lib/serverAuth"
import { cn } from "@/lib/utils"

interface ModelPickerProps {
  groups: ModelGroup[]
  /** 当前选中的模型名 */
  model: string
  onChange: (model: string) => void
  /** 登录后台用户：有值时在列表最上方展示后台「内置模型」组 */
  serverUser?: ServerUser | null
}

/** 一级（供应商）面板宽度，供二级定位用 */
const PANEL_W = 160 // w-40
const PANEL_GAP = 6
/** 一级面板最大高度 max-h-72，用于翻向判断 */
const PANEL_H = 288
/** 内置模型组的特殊一级 key（不与任何供应商名冲突） */
const BUILTIN = "__builtin__"
/** 内置模型选中 key 的前缀：与用户自配的同名模型区分「选的是哪一个」 */
const BUILTIN_PREFIX = "server:"

/** 倍率展示：限免（现倍率 0）且原倍率不同 → 原倍率中划线 + 现倍率；否则只显示现倍率 */
function rateLabel(m: ServerModel) {
  const cur = m.free ? 0 : m.multiplier
  const differs = m.free && m.multiplier !== 0
  return differs ? (
    <span className="tabular-nums">
      <s className="text-muted-foreground">{m.multiplier}x</s>
      <span className="ml-1 text-emerald-600 dark:text-emerald-400">{cur}x</span>
    </span>
  ) : (
    <span className="tabular-nums">{cur}x</span>
  )
}

/**
 * 二级模型选择（点击展开）：
 * 一级 = 供应商名称列表（**始终显示**，哪怕只有一个供应商）；**点击**供应商后，
 * 二级模型列表才从左侧飞出，当前选中模型有高亮 + 对勾。再次点击同一项收起。
 *
 * 两条硬性约束（用户明确要求）：
 * 1. 关闭后再次打开**只显示一级**，二级永远要重新点一次，不记住上次展开项。
 * 2. 二级出现/消失时一级面板**不能位移**——所以一级用普通文档流，二级用
 *    absolute 钉在一级左侧（反过来用 flex 排二级会把一级往左挤）。
 *
 * 自实现弹层而不引 Radix Popover：项目没装对应依赖，需求简单（受控开合 +
 * 点外部/Esc 关闭 + 边界避让）。
 */
export function ModelPicker({ groups, model, onChange, serverUser }: ModelPickerProps) {
  const { t } = useI18n()
  // 内置模型选中 key 带前缀（server:xxx）：与用户自配的同名模型区分「选的是哪一个」。
  // plainModel 是去掉前缀的真名，供界面显示和内置组高亮用。
  const builtinSelected = model.startsWith(BUILTIN_PREFIX)
  const plainModel = builtinSelected ? model.slice(BUILTIN_PREFIX.length) : model

  // 登录后台用户：拉取后台启用模型，在一级列表最上方显示「内置模型」组
  const [builtin, setBuiltin] = useState<ServerModel[]>([])
  useEffect(() => {
    if (!serverUser) {
      setBuiltin([])
      return
    }
    let alive = true
    server
      .models()
      .then((ms) => {
        if (alive) setBuiltin(ms)
      })
      .catch(() => {
        if (alive) setBuiltin([])
      })
    return () => {
      alive = false
    }
  }, [serverUser])

  const [open, setOpen] = useState(false)
  // 已点击展开的供应商——空串 = 二级未展开。悬停不改变它（只在已展开时允许悬停切换）
  const [hoverProvider, setHoverProvider] = useState<string>("")
  // 贴近输入框右下方；空间不足时向上翻转
  const [dropUp, setDropUp] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  const rowRefs = useRef<Map<string, HTMLButtonElement>>(new Map())

  // 统一关闭：清掉展开项，保证下次打开只有一级
  const close = () => {
    setOpen(false)
    setHoverProvider("")
  }

  // 外部切换模型（如设置页改了当前模型）时收起二级，避免残留别的供应商。
  // 依赖 model 而非 currentProvider：内置与本地同名模型互相切换时后者不变。
  useEffect(() => {
    setHoverProvider("")
  }, [model])

  // 打开时按可用空间决定翻向
  useLayoutEffect(() => {
    if (!open) return
    const anchor = wrapRef.current
    if (!anchor) return
    const rect = anchor.getBoundingClientRect()
    setDropUp(window.innerHeight - rect.bottom < PANEL_H + 16 && rect.top > PANEL_H)
  }, [open])

  // 点外部 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) close()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close()
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])

  const active = groups.find((g) => g.provider === hoverProvider)
  const models = active?.models ?? []
  // 内置组选中的行：二级显示后台模型（带倍率）
  const showBuiltin = hoverProvider === BUILTIN

  const pick = (m: string) => {
    // 内置组的选择带上前缀保存：后端据此区分来源，不与本地同名模型混串
    onChange(showBuiltin ? BUILTIN_PREFIX + m : m)
    close()
  }

  // 一级行渲染（内置组在前，供应商在后）
  const renderRow = (key: string, label: React.ReactNode) => (
    <button
      key={key}
      type="button"
      ref={(el) => {
        if (el) rowRefs.current.set(key, el)
        else rowRefs.current.delete(key)
      }}
      // 点击才展开二级；已展开时悬停可在供应商间快速切换
      onMouseEnter={() => {
        if (hoverProvider) setHoverProvider(key)
      }}
      onClick={() => setHoverProvider((p) => (p === key ? "" : key))}
      className={cn(
        "flex w-full cursor-pointer items-center gap-1.5 rounded-[5px] px-2 py-1.5 text-left text-xs transition-colors",
        key === hoverProvider
          ? "bg-accent text-accent-foreground font-medium"
          : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
      )}
    >
      <span className="truncate" title={typeof label === "string" ? label : undefined}>
        {label}
      </span>
      <ChevronRightIcon className="text-muted-foreground ml-auto size-3 shrink-0 -scale-x-100" />
    </button>
  )

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        type="button"
        title={t("mp.title")}
        onClick={() => (open ? close() : setOpen(true))}
        className="bg-muted/60 hover:bg-muted flex max-w-30 cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors sm:max-w-52"
      >
        <span className="truncate font-medium">{plainModel || t("mp.ph")}</span>
        <ChevronDownIcon className={cn("size-3.5 shrink-0 opacity-60 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div
          style={{
            bottom: dropUp ? "calc(100% + 6px)" : undefined,
            top: dropUp ? undefined : "calc(100% + 6px)",
            right: 0,
          }}
          className="absolute z-50"
        >
          {/* 一级面板：在文档流里，位置由 right:0锚定，不受二级影响 */}
          <div ref={panelRef} className="bg-popover text-popover-foreground relative z-10 w-40 overflow-hidden rounded-md border shadow-md">
            <div ref={scrollRef} className="omni-scroll max-h-72 overflow-y-auto p-1">
              {builtin.length === 0 && groups.length === 0 && !serverUser ? (
                <div className="text-muted-foreground px-2 py-1.5 text-xs">{t("mp.noProvider")}</div>
              ) : (
                <>
                  {builtin.length > 0 && renderRow(BUILTIN, t("mp.builtin"))}
                  {groups.map((g) => renderRow(g.provider || t("a.other"), g.provider || t("a.other")))}
                </>
              )}
            </div>
          </div>

          {/* 二级：absolute 钉在一级左侧，垂直方向锚定到一级当前选中行的中心 */}
          {hoverProvider && (
            <Flyout
              models={showBuiltin ? builtin.map((b) => b.key) : models}
              // 选中态按来源比：内置组比去前缀真名，本地组比原 key，
              // 同名模型各自只高亮用户真正选的那一个
              model={showBuiltin ? plainModel : model}
              pick={pick}
              anchor={rowRefs.current.get(hoverProvider)}
              builtin={showBuiltin ? builtin : undefined}
            />
          )}
        </div>
      )}
    </div>
  )
}

/** 二级：模型飞层，绝对定位在一级面板左侧，垂直方向锚定到一级当前选中行。
 * builtin 非空时行尾显示倍率：限免且原倍率不同 → 原倍率中划线 + 现倍率。 */
function Flyout({
  models,
  model,
  pick,
  anchor,
  builtin,
}: {
  models: string[]
  model: string
  pick: (m: string) => void
  /** 一级选中行按钮：二级 top 相对它（offsetTop 天然以一级面板为基准，无需再减 scrollTop） */
  anchor?: HTMLButtonElement | null
  builtin?: ServerModel[]
}) {
  const { t } = useI18n()
  const listRef = useRef<HTMLDivElement>(null)
  const activeRef = useRef<HTMLButtonElement>(null)
  // 默认顶对齐；有锚点时中心对齐
  const [top, setTop] = useState(0)
  const byKey = useMemo(() => new Map((builtin ?? []).map((b) => [b.key, b])), [builtin])

  // 当前模型滚到可视区（provider 下模型多时默认定位到选中项）
  useEffect(() => {
    const list = listRef.current
    const item = activeRef.current
    if (!list || !item) return
    const t = item.offsetTop
    if (t < list.scrollTop) list.scrollTop = t
    else if (t + item.offsetHeight > list.scrollTop + list.clientHeight) {
      list.scrollTop = t + item.offsetHeight - list.clientHeight
    }
  }, [models, model])

  // 垂直定位：二级中心对齐一级选中行；按视口 clamp（不能贴死一级面板——
  // 面板比二级矮时按面板 clamp 会把二级压回顶部，用户实测踩过）
  useLayoutEffect(() => {
    const list = listRef.current
    const fly = list?.parentElement // 二级面板根（absolute，offsetParent = 外层弹层容器）
    if (!list || !fly) return
    if (!anchor) {
      setTop(0)
      return
    }
    const h = list.offsetHeight
    const a = anchor.getBoundingClientRect()
    // 期望：选中行中心 = 二级中心
    let vpTop = a.top + a.height / 2 - h / 2
    // 视口内留 8px 边距
    vpTop = Math.max(8, Math.min(vpTop, window.innerHeight - h - 8))
    // 视口坐标 → 相对 offsetParent（外层弹层容器，可能与一级面板同顶，dropUp 时不同）
    const op = fly.offsetParent as HTMLElement | null
    const opTop = op ? op.getBoundingClientRect().top : 0
    setTop(vpTop - opTop)
  }, [anchor, models])

  return (
    <div
      className="bg-popover text-popover-foreground absolute z-0 w-56 rounded-md border shadow-md"
      style={{ right: PANEL_W + PANEL_GAP, top }}
    >
      <div ref={listRef} className="omni-scroll max-h-72 overflow-y-auto p-1">
        {models.length === 0 ? (
          <div className="text-muted-foreground px-2 py-1.5 text-xs">{t("mp.noModel")}</div>
        ) : (
          models.map((m) => (
            <button
              key={m}
              type="button"
              ref={m === model ? activeRef : undefined}
              onClick={() => pick(m)}
              className={cn(
                "flex w-full cursor-pointer items-center gap-1.5 rounded-[5px] px-2 py-1.5 text-left text-xs transition-colors",
                m === model
                  ? "bg-accent text-accent-foreground font-medium"
                  : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
              )}
            >
              <CheckIcon className={cn("size-3 shrink-0", m === model ? "opacity-100" : "opacity-0")} />
              <span className="min-w-0 truncate" title={m}>
                {m}
              </span>
              {byKey.has(m) && (
                <span className="ml-auto shrink-0 text-[10px]">{rateLabel(byKey.get(m)!)}</span>
              )}
            </button>
          ))
        )}
      </div>
    </div>
  )
}
