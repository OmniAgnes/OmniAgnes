//go:build windows

package tools

import (
	"syscall"
	"unsafe"
)

var (
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetDIBits    = gdi32.NewProc("GetDIBits")
	procCreateDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject = gdi32.NewProc("SelectObject")
	procDeleteDC     = gdi32.NewProc("DeleteDC")
	procDeleteObject = gdi32.NewProc("DeleteObject")
	procBitBlt       = gdi32.NewProc("BitBlt")
	procPatBlt       = gdi32.NewProc("PatBlt")

	procGetDC         = user32.NewProc("GetDC")
	procReleaseDC     = user32.NewProc("ReleaseDC")
	procGetWindowDC   = user32.NewProc("GetWindowDC")
	procReleaseWindowDC = user32.NewProc("ReleaseDC")
	procPrintWindow   = user32.NewProc("PrintWindow")
	procEnumWindows   = user32.NewProc("EnumWindows")
	procGetWindowTextW = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	procGetWindowRect = user32.NewProc("GetWindowRect")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procIsIconic      = user32.NewProc("IsIconic")
	procSetProcessDPIAware = user32.NewProc("SetProcessDPIAware")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
)

const (
	dibRGBColors = 0
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	dpiContextPerMonitorAwareV2 = ^uintptr(3) // -4 的补码表示

	// SRCCOPY=0x00CC0020 直接拷贝；叠加 CAPTUREBLT=0x40000000 才能抓到
	// DWM 合成的窗口内容（分层窗口/硬件加速 UI 必需）。
	srccopy    = 0x00CC0020
	captureBlt = 0x40000000
	// WHITENESS=0x00FF0062，用 PatBlt 把位图刷成纯白（等价清空+去黑）
	whiteness = 0x00FF0062
)

// rect 是 Win32 RECT。
type rect struct {
	Left, Top, Right, Bottom int32
}

// bitmapInfoHeader 对应 BITMAPINFOHEADER。
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapInfo 对应 BITMAPINFO：头 + 颜色表（1 项占位）。
type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// headerSize 是 BITMAPINFOHEADER 的字节数（40）。
const headerSize = uint32(unsafe.Sizeof(bitmapInfoHeader{}))

func setProcessDPIAware() {
	// 优先用 V2（Win10 1703+），失败退回旧 API
	if ret, _, _ := procSetProcessDpiAwarenessContext.Call(dpiContextPerMonitorAwareV2); ret != 0 {
		return
	}
	procSetProcessDPIAware.Call()
}

func createCompatibleDC(dc uintptr) uintptr {
	r, _, _ := procCreateDC.Call(dc)
	return r
}

func createCompatibleBitmap(dc uintptr, w, h int32) uintptr {
	r, _, _ := procCreateBitmap.Call(dc, uintptr(w), uintptr(h))
	return r
}

func selectObject(dc, obj uintptr) uintptr {
	r, _, _ := procSelectObject.Call(dc, obj)
	return r
}

func deleteDC(dc uintptr)       { procDeleteDC.Call(dc) }
func deleteObject(o uintptr)    { procDeleteObject.Call(o) }

func getWindowDC(hwnd uintptr) (uintptr, error) {
	r, _, err := procGetWindowDC.Call(hwnd)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func releaseWindowDC(hwnd, dc uintptr) {
	procReleaseWindowDC.Call(hwnd, dc)
}

// getDCZero 取屏幕 DC（hwnd=0 表示整个屏幕）。
func getDCZero() (uintptr, error) {
	r, _, err := procGetDC.Call(0)
	if r == 0 {
		return 0, err
	}
	return r, nil
}

func releaseDCZero(dc uintptr) {
	procReleaseDC.Call(0, dc)
}

// bitBlt 从 srcDC 拷贝矩形到 dstDC。rop 用 srccopy|captureBlt。
func bitBlt(dstDC uintptr, dstX, dstY int32, w, h int32, srcDC uintptr, srcX, srcY int32, rop uintptr) bool {
	r, _, _ := procBitBlt.Call(dstDC, uintptr(dstX), uintptr(dstY), uintptr(w), uintptr(h),
		srcDC, uintptr(srcX), uintptr(srcY), rop)
	return r != 0
}

// clearBitmap 把 memDC 上的位图刷成纯色（white=1 白色，否则黑色）。
// 抓取策略之间要重置，否则上一次的残留像素会被误判成"这次抓到了内容"。
func clearBitmap(memDC uintptr, w, h int32, white bool) {
	rop := uintptr(0x000000FF) // BLACKNESS
	if white {
		rop = uintptr(whiteness)
	}
	// 目标 hdc 必须是当前选中 bmp 的那个；调用方保证 bmp 已 SelectObject
	procPatBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), memDC, 0, 0, rop)
}

func printWindow(hwnd, hdc uintptr, x, y, w, h int, flags uintptr) bool {
	r, _, _ := procPrintWindow.Call(hwnd, hdc, uintptr(x), uintptr(y), uintptr(w), uintptr(h), flags)
	return r != 0
}

func getWindowRect(hwnd uintptr, r *rect) bool {
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(r)))
	return ret != 0
}

func isWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func getDIBits(dc, bmp uintptr, start, count uint32, bits []byte, bi *bitmapInfo, usage uint32) uint32 {
	if len(bits) == 0 {
		return 0
	}
	r, _, _ := procGetDIBits.Call(
		dc, bmp, uintptr(start), uintptr(count),
		uintptr(unsafe.Pointer(&bits[0])), uintptr(unsafe.Pointer(bi)), uintptr(usage),
	)
	return uint32(r)
}

// listWindows 枚举所有「可见 + 有标题 + 尺寸正常 + 未最小化」的窗口。
// 注意：syscall.NewCallback 必须在枚举期间保持存活，defer 保证不泄漏。
func listWindows() []winInfo {
	cbData := &struct {
		list []winInfo
		z    int
	}{}

	procEnumFunc := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if !isWindowVisible(hwnd) || isIconic(hwnd) {
			return 1 //继续枚举
		}
		n, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, n+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
		title := utf16ToString(buf)
		if title == "" {
			return 1
		}
		var r rect
		if !getWindowRect(hwnd, &r) {
			return 1
		}
		w, h := r.Right-r.Left, r.Bottom-r.Top
		// 过滤 1x1 之类的隐形小窗、任务栏托盘窗
		if w < 200 || h < 120 {
			return 1
		}
		cbData.z++
		cbData.list = append(cbData.list, winInfo{hwnd: hwnd, title: title, z: cbData.z})
		return 1
	})
	procEnumWindows.Call(procEnumFunc, 0)
	return cbData.list
}

func utf16ToString(buf []uint16) string {
	for i, v := range buf {
		if v == 0 {
			return string(utf16Decode(buf[:i]))
		}
	}
	return string(utf16Decode(buf))
}

func utf16Decode(buf []uint16) []rune {
	r := make([]rune, 0, len(buf))
	for _, v := range buf {
		r = append(r, rune(v))
	}
	return r
}
