import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"
import path from "node:path"

// 构建产物输出到 server/dist，由 server 后端服务（SPA）。
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  build: { outDir: "../dist", emptyOutDir: true },
  server: { host: "127.0.0.1", port: 9517 },
})