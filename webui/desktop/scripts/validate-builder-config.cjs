// 用 electron-builder 自带的 JSON Schema 校验 electron-builder.yml。
//
// 为什么需要（v1.0.12 踩过的坑）：
//   linux 段里平铺一个非法键（desktop.StartupWMClass）就足以让 CI 在
//   `electron-builder --linux` 起步时抛 "Invalid configuration object"，
//   本地 Windows 又打不出 AppImage，于是只能等 CI 才发现。
//   electron-builder 的 schema 在 node_modules/app-builder-lib/scheme.json，
//   这里直接复用它做静态校验，改完配置先跑本脚本，不必发版试错。
//
// 用法：node scripts/validate-builder-config.cjs
// 退出码 0 = 通过；1 = 有非法字段（打印错误路径与原因）。
const fs = require("node:fs")
const path = require("node:path")

const yaml = require("js-yaml")
const Ajv = require("ajv")

const root = path.join(__dirname, "..")
const configPath = path.join(root, "electron-builder.yml")
const schemaPath = path.join(root, "node_modules", "app-builder-lib", "scheme.json")

for (const [label, p] of [["配置", configPath], ["schema", schemaPath]]) {
  if (!fs.existsSync(p)) {
    console.error(`[validate] 找不到${label}文件：${p}`)
    process.exit(1)
  }
}

const config = yaml.load(fs.readFileSync(configPath, "utf8"))
const schema = JSON.parse(fs.readFileSync(schemaPath, "utf8"))

const ajv = new Ajv({ allErrors: true, strict: false, allowUnionTypes: true })
const validate = ajv.compile(schema)
if (!validate(config)) {
  console.error("[validate] electron-builder.yml 不符合 schema：")
  for (const err of validate.errors ?? []) {
    const at = err.instancePath || "(根)"
    console.error(`  · ${at} ${err.message}`)
    if (err.params && Object.keys(err.params).length) {
      console.error(`    params: ${JSON.stringify(err.params)}`)
    }
  }
  process.exit(1)
}

// electron-builder 会把 `${...}` 原样传给产物命名，这些值不应被当成占位符校验。
console.log("[validate] electron-builder.yml 通过 schema 校验")
const linux = config.linux
if (linux) {
  const targets = Array.isArray(linux.target) ? linux.target.join(",") : linux.target
  console.log(`[validate] linux.target = ${targets}`)
  if (linux.desktop) {
    console.log(`[validate] linux.desktop 键 = ${Object.keys(linux.desktop).join(", ")}`)
  }
}