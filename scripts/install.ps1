# omni CLI installer (Windows) -- no desktop app, no Electron needed.
#
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/OmniAgnes/OmniAgnes/master/scripts/install.ps1 | iex
#
# Optional settings (pass via environment variables, since iex takes no arguments):
#   $env:OMNI_VERSION = '1.0.28'             # default: latest
#   $env:OMNI_DIR     = 'D:\omni\bin'        # default: %LOCALAPPDATA%\omni\bin
#   $env:OMNI_DOWNLOAD_BASE = 'https://...'  # override download base (mirror / intranet)
#
# NOTE: keep this file ASCII-only and BOM-less on purpose.
#   * With a UTF-8 BOM, `irm ... | iex` fails on PowerShell 5.1: the BOM glues itself to the
#     first `#` and PowerShell tries to run "#..." as a command name.
#   * Without a BOM, PowerShell 5.1 reads the file as ANSI, so any non-ASCII text corrupts
#     the script (Chinese comments break string parsing: "string missing terminator").
#   ASCII + no BOM is the only combination that works both as `iex` input and as a file.
#
# What it does: detect arch -> download omni-cli-windows-<arch>.zip -> verify sha256 against
#   omni-cli-checksums.txt -> extract into the install dir (the kernel and its 11 built-in MCP
#   plugins must sit in the same folder, and the archive is already laid out that way) ->
#   add the dir to the user PATH -> run omni.exe --version as a self check.

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'OmniAgnes/OmniAgnes'
$Version = if ($env:OMNI_VERSION) { $env:OMNI_VERSION } else { 'latest' }
$InstallDir = if ($env:OMNI_DIR) { $env:OMNI_DIR } else { Join-Path $env:LOCALAPPDATA 'omni\bin' }

# On ARM64 Windows, an x64 PowerShell reports AMD64 in PROCESSOR_ARCHITECTURE,
# so the native arch has to be read from PROCESSOR_ARCHITEW6432.
$Arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64' -or $env:PROCESSOR_ARCHITEW6432 -eq 'ARM64') { 'arm64' } else { 'x64' }
$Asset = "omni-cli-windows-$Arch.zip"

if ($env:OMNI_DOWNLOAD_BASE) {
  $Base = $env:OMNI_DOWNLOAD_BASE.TrimEnd('/')
} elseif ($Version -eq 'latest') {
  $Base = "https://github.com/$Repo/releases/latest/download"
} else {
  $Base = "https://github.com/$Repo/releases/download/v$($Version -replace '^v', '')"
}

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ('omni-install-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Tmp -Force | Out-Null

try {
  Write-Host "-> platform windows/$Arch, version $Version"
  Write-Host "-> downloading $Base/$Asset"
  $Zip = Join-Path $Tmp $Asset
  try {
    Invoke-WebRequest -Uri "$Base/$Asset" -OutFile $Zip -UseBasicParsing
  } catch {
    throw "Download failed: $Base/$Asset (this version may not ship CLI packages yet)`n$($_.Exception.Message)"
  }

  # sha256 check: verify when checksums are available, otherwise warn and continue
  $SumFile = Join-Path $Tmp 'omni-cli-checksums.txt'
  try {
    Invoke-WebRequest -Uri "$Base/omni-cli-checksums.txt" -OutFile $SumFile -UseBasicParsing
    $Line = (Get-Content $SumFile) | Where-Object { $_ -match "\s+\*?$([regex]::Escape($Asset))$" } | Select-Object -First 1
    if (-not $Line) { throw "checksum file does not list $Asset" }
    $Want = ($Line -split '\s+')[0].ToLower()
    $Got = (Get-FileHash -Path $Zip -Algorithm SHA256).Hash.ToLower()
    if ($Want -ne $Got) { throw "sha256 mismatch (expected $Want, got $Got)" }
    Write-Host '-> sha256 verified'
  } catch {
    if ($_.Exception.Message -like '*mismatch*' -or $_.Exception.Message -like '*does not list*') { throw }
    Write-Host '! checksums unavailable, skipping integrity check'
  }

  New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
  Expand-Archive -Path $Zip -DestinationPath $InstallDir -Force

  # The kernel auto-registers every executable sitting next to it as an MCP plugin,
  # so the archive ships them together; just make sure the kernel really landed.
  $Exe = Join-Path $InstallDir 'omni.exe'
  if (-not (Test-Path $Exe)) { throw "omni.exe not found after extraction: $Exe" }

  # Append to the user PATH once, then refresh the current session
  $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
  if (-not (($UserPath -split ';') -contains $InstallDir)) {
    $NewPath = if ([string]::IsNullOrWhiteSpace($UserPath)) { $InstallDir } else { $UserPath.TrimEnd(';') + ';' + $InstallDir }
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
    Write-Host "-> added $InstallDir to the user PATH (takes effect in new terminals)"
  }
  $env:Path = "$InstallDir;$env:Path"

  Write-Host "OK: installed to $Exe"
  & $Exe --version
  Write-Host ''
  Write-Host 'Next steps:'
  Write-Host '  omni                 # TUI; run /config on first launch to set provider + API key'
  Write-Host '  omni --web           # web workbench only'
  Write-Host '  omni --print "..."   # non-interactive, for scripts / CI'
  Write-Host 'Config and data are shared with the desktop app: %USERPROFILE%\.config\omni\config.json'
} finally {
  Remove-Item -Path $Tmp -Recurse -Force -ErrorAction SilentlyContinue
}
