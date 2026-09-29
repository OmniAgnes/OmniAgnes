/// <reference types="vite/client" />

// Electron 桌面壳（webui/desktop/preload.js）注入的桥接能力；网页端不存在。
declare global {
  interface Window {
    omniDesktop?: {
      platform: string
      pickFolder(): Promise<string | null>
      win(action: "minimize" | "maximize" | "close"): void
    }
  }
}

export {}
