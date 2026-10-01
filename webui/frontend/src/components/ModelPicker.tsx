import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { CheckIcon, ChevronDownIcon, ChevronRightIcon } from "lucide-react"

import type { ModelGroup } from "@/components/Chat"
import { cn } from "@/lib/utils"

interface ModelPickerProps {
  groups: ModelGroup[]
  /** 当前选中的模型名 */
  model: string
  onChange: (model: string) => void
}

/** 一级（供应商）面板宽度，供二级定位用 */
const PANEL_W = 160 // w-40
const PANEL_GAP = 6
/** 一级面板最大高度 max-h-72，用于翻向判断 */
const PANEL_H = 288

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
export function ModelPicker({ groups, model, onChange }: ModelPickerProps) {
  // 当前模型所属供应商；模型不在列表里时回落到第一个有模型的供应商
  const currentProvider = useMemo(() => {
    for (const g of groups) if (g.models.includes(model)) return g.provider
    return groups.find((g) => g.models.length > 0)?.provider ?? ""
  }, [groups, model])

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

  // 外部切换模型（如设置页改了当前模型）时收起二级，避免残留别的供应商
  useEffect(() => {
    setHoverProvider("")
  }, [currentProvider])

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

  const pick = (m: string) => {
    onChange(m)
    close()
  }

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        type="button"
        title="切换模型"
        onClick={() => (open ? close() : setOpen(true))}
        className="bg-muted/60 hover:bg-muted flex max-w-30 cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors sm:max-w-52"
      >
        <span className="truncate font-medium">{model || "选择模型"}</span>
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
              {groups.length === 0 ? (
                <div className="text-muted-foreground px-2 py-1.5 text-xs">没有可用供应商</div>
              ) : (
                groups.map((g) => (
                  <button
                    key={g.provider}
                    type="button"
                    ref={(el) => {
                      if (el) rowRefs.current.set(g.provider, el)
                      else rowRefs.current.delete(g.provider)
                    }}
                    // 点击才展开二级；已展开时悬停可在供应商间快速切换
                    onMouseEnter={() => {
                      if (hoverProvider) setHoverProvider(g.provider)
                    }}
                    onClick={() => setHoverProvider((p) => (p === g.provider ? "" : g.provider))}
                    className={cn(
                      "flex w-full cursor-pointer items-center gap-1.5 rounded-[5px] px-2 py-1.5 text-left text-xs transition-colors",
                      g.provider === hoverProvider
                        ? "bg-accent text-accent-foreground font-medium"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    )}
                  >
                    <span className="truncate" title={g.provider}>
                      {g.provider}
                    </span>
                    <ChevronRightIcon className="text-muted-foreground ml-auto size-3 shrink-0 -scale-x-100" />
                  </button>
                ))
              )}
            </div>
          </div>

          {/* 二级：absolute 钉在一级左侧，垂直方向锚定到一级当前选中行的中心 */}
          {hoverProvider && (
            <Flyout
              models={models}
              model={model}
              pick={pick}
              anchor={rowRefs.current.get(hoverProvider)}
            />
          )}
        </div>
      )}
    </div>
  )
}

/** 二级：模型飞层，绝对定位在一级面板左侧，垂直方向锚定到一级当前选中行。 */
function Flyout({
  models,
  model,
  pick,
  anchor,
}: {
  models: string[]
  model: string
  pick: (m: string) => void
  /** 一级选中行按钮：二级 top 相对它（offsetTop 天然以一级面板为基准，无需再减 scrollTop） */
  anchor?: HTMLButtonElement | null
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const activeRef = useRef<HTMLButtonElement>(null)
  // 默认顶对齐；有锚点时中心对齐
  const [top, setTop] = useState(0)

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
          <div className="text-muted-foreground px-2 py-1.5 text-xs">该供应商下没有模型</div>
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
              <span className="truncate" title={m}>
                {m}
              </span>
            </button>
          ))
        )}
      </div>
    </div>
  )
}
