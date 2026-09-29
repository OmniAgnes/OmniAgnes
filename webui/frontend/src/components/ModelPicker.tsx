import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { CheckIcon, ChevronDownIcon } from "lucide-react"

import type { ModelGroup } from "@/components/Chat"
import { cn } from "@/lib/utils"

interface ModelPickerProps {
  groups: ModelGroup[]
  /** 当前选中的模型名 */
  model: string
  onChange: (model: string) => void
}

/**
 * 二级模型选择：一级供应商名，二级该供应商下的模型。
 * 只有一个供应商时自动省略一级，直接列模型。
 *
 * 自己实现弹层而不引 Radix Popover：项目里没装 @radix-ui/react-popover，
 * 为一个下拉装新依赖不划算；这里需求简单（受控开合 + 点外部/Esc 关闭 + 向上避让边界）。
 */
export function ModelPicker({ groups, model, onChange }: ModelPickerProps) {
  // 当前模型所属供应商；模型不在列表里时回落到第一个有模型的供应商
  const currentProvider = useMemo(() => {
    for (const g of groups) if (g.models.includes(model)) return g.provider
    return groups.find((g) => g.models.length > 0)?.provider ?? ""
  }, [groups, model])

  const [open, setOpen] = useState(false)
  const [provider, setProvider] = useState(currentProvider)
  // 贴近输入框右下方；空间不足时向上翻转
  const [dropUp, setDropUp] = useState(false)
  const [shift, setShift] = useState(0)
  const wrapRef = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  // 外部切换模型（如设置页改了当前模型）时同步一级选中项
  useEffect(() => {
    setProvider(currentProvider)
  }, [currentProvider])

  // 打开时按可用空间决定翻向，并做左右边界避让
  useLayoutEffect(() => {
    if (!open) return
    const anchor = wrapRef.current
    if (!anchor) return
    const rect = anchor.getBoundingClientRect()
    const PANEL_H = 288 // max-h-72
    setDropUp(window.innerHeight - rect.bottom < PANEL_H + 16 && rect.top > PANEL_H)
    const width = 256
    // 面板默认右对齐；左侧越界时左移
    const overflowLeft = rect.right - width
    setShift(overflowLeft < 8 ? Math.max(0, 8 + overflowLeft) : 0)
  }, [open])

  // 点外部 / Esc 关闭
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!wrapRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])

  const multi = groups.length > 1
  const active = groups.find((g) => g.provider === provider)
  const models = active?.models ?? []

  const pick = (m: string) => {
    onChange(m)
    setOpen(false)
  }

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        type="button"
        title="切换模型"
        onClick={() => setOpen((v) => !v)}
        className="bg-muted/60 hover:bg-muted flex max-w-30 cursor-pointer items-center gap-1 rounded-md px-2 py-1.5 text-xs transition-colors sm:max-w-52"
      >
        <span className="truncate font-medium">{model || "选择模型"}</span>
        <ChevronDownIcon className={cn("size-3.5 shrink-0 opacity-60 transition-transform", open && "rotate-180")} />
      </button>

      {open && (
        <div
          ref={panelRef}
          style={{ bottom: dropUp ? "calc(100% + 6px)" : undefined, top: dropUp ? undefined : "calc(100% + 6px)", left: shift || undefined, right: shift ? undefined : 0 }}
          className="bg-popover text-popover-foreground absolute z-50 flex w-64 overflow-hidden rounded-md border shadow-md"
        >
          {multi ? (
            <>
              {/* 一级：供应商 */}
              <div className="omni-scroll max-h-72 w-32 shrink-0 overflow-y-auto border-r p-1">
                {groups.map((g) => (
                  <button
                    key={g.provider}
                    type="button"
                    onClick={() => setProvider(g.provider)}
                    className={cn(
                      "flex w-full cursor-pointer items-center gap-1.5 rounded-[5px] px-2 py-1.5 text-left text-xs transition-colors",
                      g.provider === provider
                        ? "bg-accent text-accent-foreground font-medium"
                        : "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
                    )}
                  >
                    <span className="truncate">{g.provider}</span>
                    <span className="text-muted-foreground ml-auto text-[10px] tabular-nums">
                      {g.models.length}
                    </span>
                  </button>
                ))}
              </div>
              {/* 二级：模型 */}
              <div className="omni-scroll max-h-72 min-w-0 flex-1 overflow-y-auto p-1">{modelList(models, model, pick)}</div>
            </>
          ) : (
            /* 单供应商：省略一级，直接列模型 */
            <div className="omni-scroll max-h-72 w-full overflow-y-auto p-1">{modelList(models, model, pick)}</div>
          )}
        </div>
      )}
    </div>
  )
}

/** 二级模型列表：当前项打勾 */
function modelList(models: string[], model: string, pick: (m: string) => void) {
  if (models.length === 0) {
    return <div className="text-muted-foreground px-2 py-1.5 text-xs">该供应商下没有模型</div>
  }
  return models.map((m) => (
    <button
      key={m}
      type="button"
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
}
