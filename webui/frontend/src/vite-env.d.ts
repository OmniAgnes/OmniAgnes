/// <reference types="vite/client" />

// Electron 桌面壳（webui/desktop/preload.js）注入的桥接能力；网页端不存在。
declare global {
  interface OmniUpdateInfo {
    version: string
    tag: string
    notes: string
    pageUrl: string
    publishedAt: string
    downloadUrl?: string
    downloadName?: string
    size: number
  }

  interface OmniUpdateState {
    dev: boolean
    current: string
    checking: boolean
    checkedAt: number
    error?: string
    latest?: OmniUpdateInfo
    progress?: { received: number; total: number }
    file?: string
    canInstall: boolean
    domain: string
  }

  interface OmniUpdateBridge {
    get(): Promise<OmniUpdateState>
    check(): Promise<OmniUpdateState>
    download(): Promise<OmniUpdateState>
    cancel(): Promise<OmniUpdateState>
    install(): Promise<{ ok: boolean; manual?: boolean; error?: string }>
    openRelease(tag?: string): Promise<void>
    getDomain(): Promise<string>
    setDomain(d: string): Promise<string>
    onChanged(cb: (s: OmniUpdateState) => void): () => void
  }

  interface Window {
    omniDesktop?: {
      platform: string
      pickFolder(): Promise<string | null>
      win(action: "minimize" | "maximize" | "close"): void
      setZoom(z: number): void
      browserOp(
        op: string,
        args?: Record<string, unknown>,
      ): Promise<{ ok: boolean; output?: string; error?: string }>
      browserBounds(b: { x: number; y: number; width: number; height: number }): void
      browserVisible(v: boolean): void
      update: OmniUpdateBridge
    }
  }
}

export {}
