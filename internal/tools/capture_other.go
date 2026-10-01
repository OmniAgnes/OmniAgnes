//go:build !windows

package tools

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // 触发解码器注册
	_ "image/jpeg" // 触发解码器注册
	_ "image/png"  // 触发解码器注册
	"os/exec"
	"runtime"
)

// captureScreen 在非 Windows 平台通过外部命令抓屏。
// 各平台可用工具：
//   - darwin: 内置 screencapture（系统自带）
//   - linux : grim(wayland) / import 或 scrot(x11) / gnome-screenshot
// 都没有时给出明确提示，并引导改用 path 参数读本地图片。
//
// opts.window 在非 Windows 平台不生效（无等价的按标题截图能力），仅区域截图可用。
func captureScreen(region any, opts captureOpts) (image.Image, error) {
	// 把 region 转成 x,y,w,h（外部命令用绝对坐标，因为多屏虚拟桌面左上角不一定是 0,0）
	rx, ry, rw, rh := parseRegion(region)

	switch runtime.GOOS {
	case "darwin":
		args := []string{"-x"} // -x = 不响铃
		if rw > 0 && rh > 0 {
			args = append(args, "-R", fmt.Sprintf("%d,%d,%d,%d", rx, ry, rw, rh))
		}
		args = append(args, "-t", "png", "-")
		out, err := exec.Command("screencapture", args...).Output()
		if err != nil {
			return nil, fmt.Errorf("screenshot: screencapture 失败: %w", err)
		}
		return decodeImage(out)

	default:
		// 依次尝试各平台常见工具
		type candidate struct {
			bin  string
			args func() []string
		}
		cands := []candidate{
			// wayland
			{"grim", func() []string {
				if rw > 0 && rh > 0 {
					return []string{"-g", fmt.Sprintf("%d,%d,%d,%d", rx, ry, rw, rh), "-"}
				}
				return []string{"-"}
			}},
			// x11
			{"import", func() []string {
				if rw > 0 && rh > 0 {
					return []string{"-window", "root", "-crop", fmt.Sprintf("%dx%d+%d+%d", rw, rh, rx, ry), "png:-"}
				}
				return []string{"-window", "root", "png:-"}
			}},
			{"scrot", func() []string {
				if rw > 0 && rh > 0 {
					return []string{"-a", fmt.Sprintf("%d,%d,%d,%d", rx, ry, rw, rh), "-o", "-"}
				}
				return []string{"-o", "-"}
			}},
			{"gnome-screenshot", func() []string { return []string{"-f", "-"} }},
			{"spectacle", func() []string { return []string{"-b", "-n", "-f", "-o", "/dev/stdout"} }},
		}
		var tried []string
		for _, c := range cands {
			if _, err := exec.LookPath(c.bin); err != nil {
				tried = append(tried, c.bin)
				continue
			}
			out, err := exec.Command(c.bin, c.args()...).Output()
			if err != nil {
				tried = append(tried, c.bin+"(执行失败)")
				continue
			}
			if img, err := decodeImage(out); err == nil {
				return img, nil
			}
			tried = append(tried, c.bin+"(解码失败)")
		}
		return nil, fmt.Errorf(
			"screenshot: 当前平台(%s)没有可用的截图工具（已尝试：%v）。"+
				"请安装其中之一，或改用 path 参数直接读取本地图片",
			runtime.GOOS, tried)
	}
}

// parseRegion 把 region 参数解析为 x,y,w,h；无效时返回 0,0,0,0。
func parseRegion(region any) (x, y, w, h int) {
	r, ok := region.(map[string]any)
	if !ok {
		return
	}
	x = argInt(r, "x", 0)
	y = argInt(r, "y", 0)
	w = argInt(r, "width", 0)
	h = argInt(r, "height", 0)
	// 允许负宽高（向左/向上扩）—— 必须先归一化再判有效性，
	// 否则 w=-500 会被当成"无区域"直接丢掉。
	if w < 0 {
		x += w
		w = -w
	}
	if h < 0 {
		y += h
		h = -h
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0, 0
	}
	return
}

func decodeImage(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("解码截图失败: %w", err)
	}
	return img, nil
}
