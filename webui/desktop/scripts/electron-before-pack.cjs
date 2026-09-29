// electron-builder beforePack 钩子：交叉架构打包时把内核换成目标架构。
//
// 为什么需要（照搬 opencode_desk scripts/electron-before-pack.cjs 的结论）：
//   extraResources 从 bin/ 取内核（bin/omni.exe 或 bin/omni），而这份"正式名"文件
//   是按 runner 本机架构编的。CI 一条命令连打双架构包（--x64 --arm64），交叉架构
//   那个包若直接用 bin/ 里的内核，在目标机器上跑不起来——Electron 本体会按 arch
//   自动下载，唯独 extraResources 里我们自己拷的内核不会换。
//
// 处理规则：
//   · 只管 darwin / win32（linux 不发版），其他平台直接返回；
//   · **无条件**按目标架构从备料文件（bin/omni-<平台>-<架构>[.exe]，由 release.yml
//     交叉编译产出）覆盖 bin/ 里的正式名内核，不做"本机架构短路"：
//     双架构轮次的先后顺序没有保证——若本机架构轮短路不拷，交叉轮换掉 bin 后，
//     本机轮再打就会把错误架构的内核带进包。无条件覆盖则顺序无关、幂等。
//   · 备料文件不存在时跳过（本地单架构开发打包：bin 里就是本机内核，无需换）。
//
// 时机：beforePack 在 extraResources 拷贝**之前**触发，这里覆盖的就是最终进包的文件。
// 因此必须改写源（bin/omni(.exe)），不能写 appOutDir——写了也会被随后的 extraResources 拷贝盖掉。
//
// 用 .cjs 不用 .mjs：electron-builder 用 require() 加载钩子文件。
const { copyFileSync, existsSync, chmodSync } = require("node:fs")
const { join } = require("node:path")

/**
 * context.arch 是 builder-util 的数字枚举（ia32=0 / x64=1 / armv7l=2 / arm64=3 /
 * universal=4），不是字符串——直接拼备料名会得到 "omni-windows-3"。
 * 数字/字符串两种都认，兼容不同版本。
 */
const ARCH_NAMES = { 0: "ia32", 1: "x64", 2: "armv7l", 3: "arm64", 4: "universal" }

/** electron-builder 平台名 → 备料前缀 + 正式内核文件名 */
const PLATFORMS = {
  win32: { pkg: "windows", kernel: "omni.exe" },
  darwin: { pkg: "darwin", kernel: "omni" },
}

module.exports = async function beforePack(context) {
  const platform = PLATFORMS[context.electronPlatformName]
  if (!platform) return

  const arch = ARCH_NAMES[context.arch] ?? context.arch
  const binDir = join(context.packager.projectDir, "bin")
  const target = join(binDir, platform.kernel)
  const exe = platform.kernel.endsWith(".exe") ? ".exe" : ""
  const staging = join(binDir, `omni-${platform.pkg}-${arch}${exe}`)

  if (!existsSync(staging)) {
    // 本地开发/单架构打包：没有备料，bin 里就是本机内核，直接放行。
    // （CI 双架构轮必须两个备料都在，否则慢的那轮可能把对侧架构内核留进包——
    //   备料齐全时本钩子无条件覆盖，轮次顺序无关、幂等。）
    if (existsSync(target)) {
      console.log(`[beforePack] 无 bin/ 备料 omni-${platform.pkg}-${arch}${exe}，沿用现有内核（本机架构轮/本地打包）`)
      return
    }
    throw new Error(
      `[beforePack] bin/ 下既无备料 omni-${platform.pkg}-${arch}${exe} 也无内核 ${platform.kernel}——` +
        `先 go build 对应二进制（CI 见 .github/workflows/release.yml 交叉编译步骤）`,
    )
  }

  copyFileSync(staging, target)
  if (context.electronPlatformName === "darwin") {
    // mac 必须带 x 位，否则主进程 spawn 会被系统拒绝（Windows 不看权限位）
    chmodSync(target, 0o755)
  }
  console.log(`[beforePack] 已换入 ${context.electronPlatformName}/${arch} 内核：${staging} → ${target}`)
}
