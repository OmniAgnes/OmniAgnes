#!/usr/bin/env sh
# omni 命令行版安装脚本（macOS / Linux）——不需要桌面端、不需要 Electron。
#
# 用法：
#   curl -fsSL https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.sh | sh
#
# 可选参数：
#   --version 1.0.28     安装指定版本（默认 latest）
#   --dir ~/.local/bin   安装目录（默认 ~/.local/bin）
#
# 环境变量：
#   OMNI_DOWNLOAD_BASE   覆盖下载基址（镜像/内网用，指向 releases 的 download 目录）
#
# 做的事：识别平台与架构 → 下 omni-cli-<平台>-<架构>.tar.gz → 用官方 checksums 校验 sha256
#        → 解到安装目录（内核与 11 个内置 MCP 插件必须同目录，压缩包本身就是这样打的）
#        → 跑一次 omni --version 自检 → 提示 PATH。
set -eu

REPO="OmniAgnes/OmniAgnes"
VERSION="latest"
INSTALL_DIR="${HOME}/.local/bin"

while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:?--version 需要值}"; shift 2 ;;
    --dir)     INSTALL_DIR="${2:?--dir 需要值}";     OMNI_DIR_GIVEN=1; shift 2 ;;
    -h|--help)
      cat <<'USAGE'
用法：install.sh [--version <版本>] [--dir <安装目录>]

  --version 1.0.28     安装指定版本（默认 latest）
  --dir ~/.local/bin   安装目录（默认 ~/.local/bin）

环境变量：OMNI_DOWNLOAD_BASE 覆盖下载基址（镜像/内网）
USAGE
      exit 0 ;;
    *) echo "omni-install: 未知参数 $1" >&2; exit 2 ;;
  esac
done

die() { echo "omni-install: $*" >&2; exit 1; }

# ---- 平台 / 架构 ----
OS_RAW=$(uname -s)
case "$OS_RAW" in
  Linux*)  OS=linux ;;
  Darwin*) OS=macos ;;
  MINGW*|MSYS*|CYGWIN*)
    # Git Bash / MSYS2 / Cygwin：用户习惯敲 `curl | sh`，但这里要装的是 Windows 原生版
    # （omni.exe + .exe 插件），直接转交给 PowerShell 安装器，别让他们看到「不支持的平台」。
    command -v powershell >/dev/null 2>&1 || \
      die "检测到 Git Bash/MSYS，但找不到 powershell。请在 PowerShell 里执行：
  irm https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.ps1 | iex"
    # 版本号透传；目录若由 --dir 指定，先转成 Windows 路径（cygpath 在 Git Bash 里一定有）
    [ "$VERSION" = "latest" ] || export OMNI_VERSION="$VERSION"
    if [ -n "${OMNI_DIR_GIVEN:-}" ]; then
      if command -v cygpath >/dev/null 2>&1; then
        export OMNI_DIR="$(cygpath -w "$INSTALL_DIR")"
      else
        die "在 Git Bash 里用 --dir 需要 cygpath；或在 PowerShell 里设置 \$env:OMNI_DIR 后直接跑 install.ps1"
      fi
    fi
    echo "→ 检测到 Git Bash/MSYS，转为运行 Windows 版安装脚本（powershell）"
    exec powershell -NoProfile -ExecutionPolicy Bypass -Command \
      "irm https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.ps1 | iex"
    ;;
  *) die "不支持的平台：$OS_RAW（Windows 请用 scripts/install.ps1）" ;;
esac

ARCH_RAW=$(uname -m)
case "$ARCH_RAW" in
  x86_64|amd64)  ARCH=x64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "不支持的架构：$ARCH_RAW" ;;
esac

ASSET="omni-cli-${OS}-${ARCH}.tar.gz"

# ---- 下载基址 ----
if [ -n "${OMNI_DOWNLOAD_BASE:-}" ]; then
  BASE="${OMNI_DOWNLOAD_BASE%/}"
elif [ "$VERSION" = "latest" ]; then
  BASE="https://github.com/${REPO}/releases/latest/download"
else
  BASE="https://github.com/${REPO}/releases/download/v${VERSION#v}"
fi

# ---- 下载工具 ----
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  die "需要 curl 或 wget"
fi

TMP=$(mktemp -d 2>/dev/null || mktemp -d -t omni)
trap 'rm -rf "$TMP"' EXIT INT TERM

echo "→ 平台 ${OS}/${ARCH}，版本 ${VERSION}"
echo "→ 下载 ${BASE}/${ASSET}"
fetch "${BASE}/${ASSET}" "${TMP}/${ASSET}" || die "下载失败：${BASE}/${ASSET}（该版本可能还没发布命令行包）"

# ---- sha256 校验（有 checksums 就校验；缺失不阻断，只提示） ----
if fetch "${BASE}/omni-cli-checksums.txt" "${TMP}/checksums.txt" 2>/dev/null; then
  want=$(awk -v f="${ASSET}" '{ n=$2; sub(/^\*/, "", n); if (n==f) { print $1; exit } }' "${TMP}/checksums.txt")
  [ -n "$want" ] || die "校验和文件里找不到 ${ASSET}，已中止"
  if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "${TMP}/${ASSET}" | awk '{print $1}')
  else
    got=$(shasum -a 256 "${TMP}/${ASSET}" | awk '{print $1}')
  fi
  [ "$got" = "$want" ] || die "sha256 不匹配（期望 $want，实际 $got），已中止"
  echo "→ sha256 校验通过"
else
  echo "! 未能下载 checksums，跳过校验"
fi

# ---- 安装 ----
mkdir -p "$INSTALL_DIR" || die "无法创建目录 $INSTALL_DIR"
tar -xzf "${TMP}/${ASSET}" -C "$INSTALL_DIR" || die "解包失败"
# 内核按自身 exe 同目录扫描注册 MCP 插件，压缩包里已含全部插件；这里只保证可执行位
chmod +x "$INSTALL_DIR"/omni "$INSTALL_DIR"/pdf "$INSTALL_DIR"/xlsx "$INSTALL_DIR"/docx \
         "$INSTALL_DIR"/pptx "$INSTALL_DIR"/csv "$INSTALL_DIR"/image "$INSTALL_DIR"/media \
         "$INSTALL_DIR"/fetch "$INSTALL_DIR"/agnes "$INSTALL_DIR"/mimo 2>/dev/null || true
chmod +x "$INSTALL_DIR"/omni

echo "✓ 已安装到 ${INSTALL_DIR}/omni"
"${INSTALL_DIR}/omni" --version || true

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "! ${INSTALL_DIR} 不在 PATH 里，把下面一行加进 shell 配置（~/.bashrc / ~/.zshrc）："
    echo "    export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac

cat <<'TIP'

下一步：
  omni                 # 进 TUI；首次运行用 /config 配置供应商与 API Key
  omni --web           # 只启网页工作台
  omni --print "问题"  # 非交互，给脚本/CI 用
配置与数据与桌面端共用：~/.config/omni/config.json、~/.local/share/omni/
TIP
