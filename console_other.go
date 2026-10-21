//go:build !windows

package main

import "os"

// 非 Windows 平台终端默认 UTF-8，无需处理。
func setConsoleUTF8(f *os.File) (restore func()) {
	return nil
}
