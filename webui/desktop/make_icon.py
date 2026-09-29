# 生成 omni 应用 LOGO：
#   - 圆角方形渐变底（靛蓝 → 紫）+ 白色圆环（"O"）+ 白色四角星光（AI 火花）
# 产出：
#   - icon.ico  多尺寸（16/24/32/48/64/128/256），electron-builder NSIS 打包用
#   - icon.png  512px，Electron 窗口图标（开发态）
#   - 预览 preview.png 1024px，便于人工核对
from PIL import Image, ImageDraw

N = 1024  # 画布尺寸

# 渐变色（对角线方向）
STOPS = [
    (0.0, (99, 102, 241)),    # indigo-500  #6366F1
    (1.0, (168, 85, 247)),    # purple-500 #A855F7
]


def gradient(size, stops):
    import numpy as np
    axis = np.linspace(0.0, 1.0, size)
    x = np.tile(axis, (size, 1))
    y = np.tile(axis[:, None], (1, size))
    t = ((x + y) / 2.0).clip(0.0, 1.0).ravel()
    pos = np.array([s[0] for s in stops], dtype=float)
    cols = np.array([s[1] for s in stops], dtype=float)
    rgb = np.stack(
        [np.interp(t, pos, cols[:, i]) for i in range(3)], axis=1
    ).reshape(size, size, 3)
    return Image.fromarray(rgb.astype("uint8"), "RGB")


img = gradient(N, STOPS).convert("RGBA")

# 圆角方形蒙版（圆角约 22.5%）
mask = Image.new("L", (N, N), 0)
ImageDraw.Draw(mask).rounded_rectangle([0, 0, N - 1, N - 1], radius=230, fill=255)
icon = Image.new("RGBA", (N, N), (0, 0, 0, 0))
icon.paste(img, (0, 0), mask)

d = ImageDraw.Draw(icon)
WHITE = (255, 255, 255, 255)
C = N // 2  # 中心

# 白色圆环（"O"）：中环半径 290，描边宽 100
ring_r, stroke = 290, 100
d.ellipse(
    [C - ring_r - stroke // 2, C - ring_r - stroke // 2,
     C + ring_r + stroke // 2, C + ring_r + stroke // 2],
    outline=WHITE, width=stroke,
)

# 白色四角星光（AI sparkle）：尖端半径 215，内凹点半径 100
tip, inner = 215, 100
ixy = round(inner * 0.70710678)
star = [
    (C, C - tip),                    # N 尖端
    (C + ixy, C - ixy),              # NE 内凹
    (C + tip, C),                    # E 尖端
    (C + ixy, C + ixy),              # SE 内凹
    (C, C + tip),                    # S 尖端
    (C - ixy, C + ixy),              # SW 内凹
    (C - tip, C),                    # W 尖端
    (C - ixy, C - ixy),              # NW 内凹
]
d.polygon(star, fill=WHITE)

import os

out_dir = os.path.join(os.path.dirname(__file__), "build")
os.makedirs(out_dir, exist_ok=True)

# 多尺寸 ICO
ico_sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
icon.save(os.path.join(out_dir, "icon.ico"), format="ICO", sizes=ico_sizes)

# 窗口图标 PNG（开发态 BrowserWindow 用）
icon.resize((512, 512), Image.LANCZOS).save(os.path.join(out_dir, "icon.png"))

# 1024 原尺寸预览
icon.save(os.path.join(out_dir, "preview.png"))

print("generated:", os.listdir(out_dir))
