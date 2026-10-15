import { useRef } from "react"

import { ZoomImage } from "./zoom-image"

/** 按 URL 后缀判断内联媒体类型（不含则返回 null，交给普通链接处理） */
export function mediaKind(url: string): "image" | "video" | "audio" | null {
  const clean = (url || "").split(/[?#]/)[0].toLowerCase()
  if (/\.(png|jpe?g|gif|webp|bmp|avif|ico)$/.test(clean)) return "image"
  if (/\.(mp4|webm|ogv|mov|m4v|mkv)$/.test(clean)) return "video"
  if (/\.(mp3|wav|m4a|aac|ogg|oga|flac|opus)$/.test(clean)) return "audio"
  return null
}

/**
 * 内联媒体：Markdown 里指向图片/视频/音频的链接或图片，直接渲染成可看/可播的元素，
 * 而不是丢一个文件名让用户自己去开。图片点击可放大（与工具媒体同一套 ZoomImage）。
 *
 * 用 <span> 而不是 <div>：Markdown 会把它塞进 <p> 里，块级元素嵌在段落里是非法结构。
 */
export function InlineMedia({
  url,
  kind,
  label,
}: {
  url: string
  kind: "image" | "video" | "audio"
  label?: string
}) {
  if (kind === "image") return <ZoomImage src={url} alt={label} />
  const caption = label && label !== url ? <span className="text-muted-foreground text-xs break-all">{label}</span> : null
  return (
    <span className="flex flex-col gap-1">
      {kind === "video" ? (
        <video src={url} controls preload="metadata" className="max-h-80 w-auto max-w-full self-start rounded-md" />
      ) : (
        <AudioPlayer src={url} />
      )}
      {caption}
    </span>
  )
}

/** 音频播放器：AI 生成的 WAV 头里长度字段常常未知，duration 会读成 Infinity、
 * 进度条/时长不显示。这里在 loadedmetadata 时探测并做一次"快进回跳"逼浏览器算出真实时长。 */
export function AudioPlayer({ src }: { src: string }) {
  const ref = useRef<HTMLAudioElement | null>(null)
  const fixDuration = () => {
    const el = ref.current
    if (!el) return
    if (Number.isFinite(el.duration) && el.duration > 0) return
    el.ontimeupdate = () => {
      el.ontimeupdate = null
      el.currentTime = 0
    }
    el.currentTime = 1e101 // 跳到"末尾"迫使浏览器解析全文件长度
  }
  return <audio ref={ref} onLoadedMetadata={fixDuration} src={src} controls preload="metadata" className="w-full max-w-md" />
}
