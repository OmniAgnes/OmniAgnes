# omni 命令行版安装脚本（Windows）——不需要桌面端、不需要 Electron。
#
# 用法（PowerShell）：
#   irm https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.ps1 | iex
#
# 可选（脚本被 iex 执行，传不了参数，用环境变量）：
#   $env:OMNI_VERSION = '1.0.28'          # 指定版本，默认 latest
#   $env:OMNI_DIR     = 'D:\omni\bin'     # 安装目录，默认 %LOCALAPPDATA%\omni\bin
#   $env:OMNI_DOWNLOAD_BASE = 'https://...'  # 覆盖下载基址（镜像/内网）
#
# 做的事：识别架构 → 下 omni-cli-windows-<架构>.zip → 用官方 checksums 校验 sha256
#        → 解到安装目录（内核与 11 个内置 MCP 插件必须同目录，压缩包本身就是这样打的）
#        → 写入用户 PATH → 跑一次 omni.exe --version 自检。

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'OmniAgnes/OmniAgnes'
$Version = if ($env:OMNI_VERSION) { $env:OMNI_VERSION } else { 'latest' }
$InstallDir = if ($env:OMNI_DIR) { $env:OMNI_DIR } else { Join-Path $env:LOCALAPPDATA 'omni\bin' }

# ARM64 上跑 x64 PowerShell 时 PROCESSOR_ARCHITECTURE 仍是 AMD64，得看 ARCHITEW6432
$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'x64' }
$Asset = "omni-cli-windows-$Arch.zip"

if ($env:OMNI_DOWNLOAD_BASE) {
  $Base = $env:OMNI_DOWNLOAD_BASE.TrimEnd('/')
} elseif ($Version -eq 'latest') {
  $Base = "https://github.com/$Repo/releases/latest/download"
} else {
  $Base = "https://github.com/$Repo/releases/download/v$($Version -replace '^v', '')"
}

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ("omni-install-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Tmp -Force | Out-Null

try {
  Write-Host "→ 平台 windows/$Arch，版本 $Version"
  Write-Host "→ 下载 $Base/$Asset"
  $Zip = Join-Path $Tmp $Asset
  try {
    Invoke-WebRequest -Uri "$Base/$Asset" -OutFile $Zip -UseBasicParsing
  } catch {
    throw "下载失败：$Base/$Asset（该版本可能还没发布命令行包）`n$($_.Exception.Message)"
  }

  # sha256 校验：有 checksums 就校验，缺失只提示不阻断
  $SumFile = Join-Path $Tmp 'omni-cli-checksums.txt'
  $Verified = $false
  try {
    Invoke-WebRequest -Uri "$Base/omni-cli-checksums.txt" -OutFile $SumFile -UseBasicParsing
    $Line = (Get-Content $SumFile) | Where-Object { $_ -match "\s+\*?$([regex]::Escape($Asset))$" } | Select-Object -First 1
    if (-not $Line) { throw "校验和文件里找不到 $Asset，已中止" }
    $Want = ($Line -split '\s+')[0].ToLower()
    $Got = (Get-FileHash -Path $Zip -Algorithm SHA256).Hash.ToLower()
    if ($Want -ne $Got) { throw "sha256 不匹配（期望 $Want，实际 $Got），已中止" }
    Write-Host "→ sha256 校验通过"
    $Verified = $true
  } catch {
    if ($_.Exception.Message -like '*已中止*') { throw }
    Write-Host "! 未能校验 sha256，跳过"
  }
  if (-not $Verified) { Write-Host "! 未校验安装包完整性" }

  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Expand-Archive -Path $Zip -DestinationPath $InstallDir -Force

  # 内核按自身 exe 同目录扫描注册 MCP 插件，压缩包里已含全部插件，这里只做存在性自检
  $Exe = Join-Path $InstallDir 'omni.exe'
  if (-not (Test-Path $Exe)) { throw "解包后找不到 $Exe" }

  # 写入用户 PATH（只加一次），并刷新当前会话
  $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not (($UserPath -split ';') -contains $InstallDir)) {
    $NewPath = if ([string]::IsNullOrWhiteSpace($UserPath)) { $InstallDir } else { $UserPath.TrimEnd(';') + ';' + $InstallDir }
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
    Write-Host "→ 已把 $InstallDir 加入用户 PATH（新开的终端生效）"
  }
  $env:Path = "$InstallDir;$env:Path"

  Write-Host "✓ 已安装到 $Exe"
  & $Exe --version
  Write-Host ''
  Write-Host '下一步：'
  Write-Host '  omni                 # 进 TUI；首次运行用 /config 配置供应商与 API Key'
  Write-Host '  omni --web           # 只启网页工作台'
  Write-Host '  omni --print "问题"  # 非交互，给脚本/CI 用'
  Write-Host '配置与数据与桌面端共用：%USERPROFILE%\.config\omni\config.json、%LOCALAPPDATA%\omni\'
} finally {
  Remove-Item -Path $Tmp -Recurse -Force -ErrorAction SilentlyContinue
}
