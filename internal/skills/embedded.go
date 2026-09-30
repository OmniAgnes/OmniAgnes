package skills

import "embed"

//go:embed all:embedded
var builtinFS embed.FS

// embedded/ 子树里就是内置技能本体（<名> 或 @组/<名> 布局，与全局技能目录一致）。
// 它是生成物，不手写：
//   · 仓库根目录的 .omni/skills/ 是「内置技能」的唯一权威来源；
//   · 改完后在仓库根跑  go run ./build/gen-builtin-skills  生成到 embedded/；
//   · 发版 CI（release.yml）交叉编译前自动跑这一步——同一份 embed 打进内核二进制。
// 未生成时 embedded/ 里只有 .gitkeep，SeedBuiltIn 静默跳过，不阻断启动。
