// gen-builtin-skills：把仓库根 .omni/skills/ 同步进 internal/skills/embedded/（go:embed 源）。
//
// 背景：内置技能的唯一权威来源是仓库根的 .omni/skills/。但桌面版启动时的工作目录是
// 用户 home（webui/desktop/main.js），仓库目录根本不在加载路径上；TUI/网页版的用户工作区
// 也几乎永远不会是「omni 自己的仓库」。所以 .omni/skills 只会以 embed 打进内核二进制、
// 启动时 SeedBuiltIn 落进全局技能目录这一条路被装出来的用户看到。
//
// embedded/ 是生成物：改 .omni/skills 后在仓库根跑  go run ./build/gen-builtin-skills
// 再 go build。发版 CI（release.yml）在交叉编译前自动跑这一步。
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	srcRoot = ".omni/skills"
	dstRoot = "internal/skills/embedded"
)

// skillRoots 收集要嵌入的技能顶层目录：<名>（含 SKILL.md）或 @组（其下子目录含 SKILL.md）。
func skillRoots() []string {
	entries, err := os.ReadDir(srcRoot)
	if err != nil {
		return nil
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(srcRoot, e.Name(), "SKILL.md")); err == nil {
			roots = append(roots, e.Name())
			continue
		}
		subs, _ := os.ReadDir(filepath.Join(srcRoot, e.Name()))
		for _, se := range subs {
			if se.IsDir() {
				if _, err := os.Stat(filepath.Join(srcRoot, e.Name(), se.Name(), "SKILL.md")); err == nil {
					roots = append(roots, e.Name())
					break
				}
			}
		}
	}
	return roots
}

func main() {
	if _, err := os.Stat(srcRoot); err != nil {
		fmt.Printf("内置技能源 %s 不存在，跳过生成\n", srcRoot)
		return
	}
	roots := skillRoots()
	if len(roots) == 0 {
		fmt.Printf("%s 下没有可嵌入的技能（需 <名>/SKILL.md）\n", srcRoot)
	}
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		fmt.Println("创建目标目录失败:", err)
		os.Exit(1)
	}
	kept := map[string]bool{}
	for _, root := range roots {
		kept[root] = true
		srcDir := filepath.Join(srcRoot, root)
		dstDir := filepath.Join(dstRoot, root)
		err := filepath.WalkDir(srcDir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(srcDir, p)
			if err != nil {
				return err
			}
			target := filepath.Join(dstDir, rel)
			if d.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, 0o644)
		})
		if err != nil {
			fmt.Println("复制失败:", root, err)
			os.Exit(1)
		}
	}
	// 清掉目标里已不再存在的技能目录（.gitkeep 等占位文件保留）
	dentries, _ := os.ReadDir(dstRoot)
	for _, e := range dentries {
		if e.IsDir() && !kept[e.Name()] {
			if err := os.RemoveAll(filepath.Join(dstRoot, e.Name())); err != nil {
				fmt.Println("清理旧目录失败:", e.Name(), err)
				os.Exit(1)
			}
			fmt.Println("移除:", e.Name())
		}
	}
	fmt.Printf("已同步 %d 个内置技能: %s\n", len(roots), strings.Join(roots, " "))
}
