// 打包前检查 omni 内核是否就绪（bin/omni.exe 或 bin/omni）。
import { existsSync, statSync } from "node:fs"
import path from "node:path"

const exe = process.platform === "win32" ? "omni.exe" : "omni"
const p = path.join(process.cwd(), "bin", exe)

if (!existsSync(p) || !statSync(p).isFile()) {
  console.error(`[check-bin] 缺少内核: ${p}`)
  console.error("先在仓库根目录执行: go build -o webui/desktop/bin/omni.exe .")
  process.exit(1)
}
console.log(`[check-bin] 内核就绪: ${p}`)
