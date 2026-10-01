//go:build windows

package tools

import (
	"fmt"
	"image"
	"sort"
	"strings"
)

// captureScreen 抓取屏幕（或指定区域/窗口），返回 Go image。
//
// 为什么不用 GetDC(0)+BitBlt 抓整屏：在部分环境（无交互桌面会话、RDP 断开、
// 沙箱）里屏幕 DC 读出来是全黑，且 GetDIBits 直接返回 0 行。
// 改成走 PrintWindow 抓具体窗口的内容 —— 对 Electron 这类 DWM 合成窗口
// 反而更准，还能只截用户关心的那个窗口；不指定窗口时取最前面的可见窗口。
//
// opts.window 不为空时按标题关键词匹配窗口。
func captureScreen(region any, opts captureOpts) (image.Image, error) {
	// DPI 感知：否则高分屏下尺寸和实际像素对不上
	setProcessDPIAware()

	// 取目标窗口
	hwnd, err := pickTargetWindow(opts.window)
	if err != nil {
		return nil, err
	}

	rect, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	sx, sy, sw, sh := int(rect.Left), int(rect.Top), int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if sw <= 0 || sh <= 0 {
		return nil, fmt.Errorf("screenshot: 目标窗口尺寸异常 %dx%d", sw, sh)
	}

	// region 覆盖：以窗口左上为原点（相对坐标，符合直觉）
	if r, ok := region.(map[string]any); ok {
		rx := argInt(r, "x", 0)
		ry := argInt(r, "y", 0)
		rw := argInt(r, "width", 0)
		rh := argInt(r, "height", 0)
		if rw <= 0 {
			rw = sw
		}
		if rh <= 0 {
			rh = sh
		}
		// 允许负宽高（向左/向上扩），转成 BitBlt 需要的正尺寸
		if rw < 0 {
			rx += rw
			rw = -rw
		}
		if rh < 0 {
			ry += rh
			rh = -rh
		}
		if rx < 0 {
			rw += rx
			rx = 0
		}
		if ry < 0 {
			rh += ry
			ry = 0
		}
		if rx+rw > sw {
			rw = sw - rx
		}
		if ry+rh > sh {
			rh = sh - ry
		}
		if rw > 0 && rh > 0 {
			sx, sy, sw, sh = sx+rx, sy+ry, rw, rh
		}
	}

	srcDC, err := getWindowDC(hwnd)
	if err != nil || srcDC == 0 {
		return nil, fmt.Errorf("screenshot: GetWindowDC 失败: %v", err)
	}
	defer releaseWindowDC(hwnd, srcDC)

	memDC := createCompatibleDC(srcDC)
	if memDC == 0 {
		return nil, fmt.Errorf("screenshot: CreateCompatibleDC 失败")
	}
	defer deleteDC(memDC)

	bmp := createCompatibleBitmap(memDC, int32(sw), int32(sh))
	if bmp == 0 {
		return nil, fmt.Errorf("screenshot: CreateCompatibleBitmap 失败")
	}
	defer deleteObject(bmp)

	old := selectObject(memDC, bmp)
	defer selectObject(memDC, old)

	bi := bitmapInfo{}
	bi.Header = bitmapInfoHeader{
		Size:     headerSize,
		Width:    int32(sw),
		Height:   -int32(sh), // 负高度 = 自上而下
		Planes:   1,
		BitCount: 32,
	}
	raw := make([]byte, sw*sh*4)

	// 抓取策略按顺序尝试，取第一个拿到**非纯色**结果的那个。
	// 之所以要检测纯色：某些环境下 PrintWindow / BitBlt 会「成功」但返回全黑图
	// （无桌面会话、DWM 合成窗口、远程桌面最小化等），不检测就会把黑图当成功。
	strategies := []struct {
		name string
		fn   func(sw, sh, sx, sy int) bool
	}{
		{"PrintWindow(RENDERFULLCONTENT)", func(sw, sh, sx, sy int) bool {
			return printWindow(hwnd, memDC, 0, 0, sw, sh, 2)
		}},
		{"BitBlt(窗口DC)", func(sw, sh, sx, sy int) bool {
			// 从窗口 DC 拷贝，源偏移 0（窗口 DC 原点即窗口左上角）
			return bitBlt(memDC, 0, 0, int32(sw), int32(sh), srcDC, 0, 0, srccopy|captureBlt)

		}},
		{"PrintWindow(CLIENTONLY)", func(sw, sh, sx, sy int) bool {
			return printWindow(hwnd, memDC, 0, 0, sw, sh, 1)
		}},
		{"BitBlt(屏幕DC)", func(sw, sh, sx, sy int) bool {
			// 最后手段：直接抓屏幕 DC 的对应区域
			scrDC, err := getDCZero()
			if err != nil || scrDC == 0 {
				return false
			}
			defer releaseDCZero(scrDC)
			return bitBlt(memDC, 0, 0, int32(sw), int32(sh), scrDC, int32(sx), int32(sy), srccopy|captureBlt)
		}},
	}

	var errs []string
	for _, st := range strategies {
		// 每轮重置位图（上个策略可能已画了东西，避免残留被误判为内容）
		clearBitmap(memDC, int32(sw), int32(sh), true)
		if !st.fn(sw, sh, sx, sy) {
			errs = append(errs, st.name+" 失败")
			continue
		}
		if n := getDIBits(memDC, bmp, 0, uint32(sh), raw, &bi, dibRGBColors); n == 0 {
			errs = append(errs, st.name+": GetDIBits 未读到像素")
			continue
		}
		if isUniform(raw) {
			// 纯色 → 说明这次没抓到真实内容，换下一个策略
			errs = append(errs, st.name+" 返回纯色图")
			continue
		}
		return buildImage(raw, sw, sh), nil
	}
	return nil, fmt.Errorf("screenshot: 所有抓取策略都失败（%s）。"+
		"若在无交互桌面的环境（服务/容器/SSH）中运行，截屏本身不可用；"+
		"可改用 screenshot 的 path 参数直接读取已存在的图片",
		strings.Join(errs, "; "))
}

// isUniform 判断像素缓冲是否近似纯色（用于识别"成功但全黑"的情况）。
func isUniform(raw []byte) bool {
	if len(raw) < 64 {
		return true
	}
	// 抽样前 64KB 里的像素，每 4 字节取一个通道比较
	var r0, g0, b0 byte = raw[0], raw[1], raw[2]
	limit := len(raw)
	if limit > 65536 {
		limit = 65536
	}
	same := 0
	total := 0
	for i := 0; i+3 < limit; i += 4 {
		total++
		if raw[i] == r0 && raw[i+1] == g0 && raw[i+2] == b0 {
			same++
		}
	}
	// 99% 以上同色 → 视为纯色
	return total > 0 && float64(same)/float64(total) > 0.99
}

// buildImage 把 BGRA 原始像素转成 Go image。
func buildImage(raw []byte, w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for row := 0; row < h; row++ {
		src := raw[row*w*4:]
		for col := 0; col < w; col++ {
			d := img.PixOffset(col, row)
			img.Pix[d+0] = src[col*4+2] // R
			img.Pix[d+1] = src[col*4+1] // G
			img.Pix[d+2] = src[col*4+0] // B
			img.Pix[d+3] = 255// GDI 的 alpha 恒为 0
		}
	}
	return img
}

type winInfo struct {
	hwnd  uintptr
	title string
	z     int // z序，越大越靠前
}

// pickTargetWindow 找一个可截图的窗口。
// keyword 非空 → 按标题包含匹配（取 z 序最靠前的）；否则取最前面的可见窗口。
func pickTargetWindow(keyword string) (uintptr, error) {
	wins := listWindows()
	if len(wins) == 0 {
		return 0, fmt.Errorf("screenshot: 未找到可截图的窗口（可能没有可见窗口，或运行在无桌面会话的环境）")
	}
	// z 序倒序：最前面的窗口优先
	sort.Slice(wins, func(i, j int) bool { return wins[i].z > wins[j].z })

	if kw := strings.TrimSpace(keyword); kw != "" {
		low := strings.ToLower(kw)
		for _, w := range wins {
			if strings.Contains(strings.ToLower(w.title), low) {
				return w.hwnd, nil
			}
		}
		// 没匹配上：列出候选帮助用户
		names := make([]string, 0, 6)
		for i, w := range wins {
			if i >= 6 {
				break
			}
			names = append(names, w.title)
		}
		return 0, fmt.Errorf("screenshot: 没找到标题含 %q 的窗口。当前可见窗口：%s",
			kw, strings.Join(names, " / "))
	}

	return wins[0].hwnd, nil
}

// windowRect 取窗口矩形。
func windowRect(hwnd uintptr) (rect, error) {
	var r rect
	if !getWindowRect(hwnd, &r) {
		return r, fmt.Errorf("screenshot: GetWindowRect 失败")
	}
	return r, nil
}
