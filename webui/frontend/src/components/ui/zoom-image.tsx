import * as React from "react"
import { createPortal } from "react-dom"

import { cn } from "@/lib/utils"

/** 可点击放大的图片：默认限高限宽自适应，点击后灯罩遮罩铺满屏幕（点任意处关闭）。
 *
 * 统一「回答里的 Markdown 图片」与「工具产出的媒体」两处交互——原先只有 MediaList
 * 有放大功能，Markdown 里的图片点不动、也没限宽，大图会把气泡撑破。 */
export function ZoomImage({
  src,
  alt,
  className,
  onError,
}: {
  src: string
  alt?: string
  /** 追加在默认尺寸类之后，用于覆盖限高（如 Markdown 正文里想要更高） */
  className?: string
  onError?: () => void
}) {
  const [zoom, setZoom] = React.useState(false)
  return (
    <>
      <img
        src={src}
        alt={alt ?? ""}
        loading="lazy"
        onError={onError}
        onClick={() => setZoom(true)}
        className={cn(
          "max-h-64 w-auto max-w-full cursor-zoom-in self-start rounded-md object-contain",
          className,
        )}
      />
      {zoom &&
        createPortal(
          // 挂到 body：Markdown 里的图片位于 <p> 内，塞一个 div 进去是非法嵌套；
          // 且祖先若有 transform，fixed 会以它为参照导致遮罩错位。portal 两头都避开。
          <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-8"
            onClick={() => setZoom(false)}
          >
            <img
              src={src}
              alt={alt ?? ""}
              className="max-h-full max-w-full rounded-lg object-contain shadow-2xl"
              onClick={(e) => e.stopPropagation()}
            />
          </div>,
          document.body,
        )}
    </>
  )
}
