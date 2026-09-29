import path from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

const here = path.dirname(fileURLToPath(import.meta.url))

// 构建产物输出到 internal/webui/dist，由 go:embed 打进二进制。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(here, "./src") },
  },
  server: {
    proxy: {
      // 前端调试：先在 omni TUI 里 /webui 拿到地址，OMNI_URL=http://127.0.0.1:PORT npm run dev
      "/api": { target: process.env.OMNI_URL || "http://127.0.0.1:1", changeOrigin: true },
    },
  },
  build: {
    outDir: "../../internal/webui/dist",
    emptyOutDir: true,
  },
})
