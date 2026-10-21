//go:build windows

package main

import (
	"os"
	"syscall"

	"github.com/mattn/go-isatty"
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleOutputCP = kernel32.NewProc("GetConsoleOutputCP")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

// setConsoleUTF8 把控制台输出码页切到 UTF-8（65001）并返回还原函数，非终端或
// 非 Windows 平台返回 nil。
//
// 注意 PowerShell 兼容：Windows PowerShell 5.1 的 conhost 默认输出码页是 CP936，
// 本程序按 UTF-8 写 stdout，TUI 的中文、框线与凭心符（✻ 等）会被误解成乱码；
// Windows Terminal 用户默认 UTF-8，此调用无副作用。stdout 被重定向到文件或
// 管道时码页无意义，跳过不动。
func setConsoleUTF8(f *os.File) (restore func()) {
	if f == nil || !isatty.IsTerminal(f.Fd()) {
		return nil
	}
	old, _, _ := procGetConsoleOutputCP.Call()
	procSetConsoleOutputCP.Call(65001)
	return func() { procSetConsoleOutputCP.Call(old) }
}
