# GoClaw installer (Windows PowerShell) — downloads the latest binary from GitHub Releases.
#
# Usage:
#   powershell -c "irm https://raw.githubusercontent.com/qkhalk/goclaw/dev/scripts/install.ps1 | iex"
#   powershell -c "irm ... | iex" -ArgumentList @('--version', 'v1.30.0')
#   powershell -c "irm ... | iex" -ArgumentList @('--dir', 'C:\tools')
#
# Supported: Windows (amd64)

param(
  [string]$Version = "",
  [string]$Dir = ""
)

$ErrorActionPreference = "Stop"

$Repo = "qkhalk/goclaw"
if (-not $Dir) { $Dir = Join-Path $env:LOCALAPPDATA "Programs\goclaw" }
if (-not $Dir) { $Dir = "$env:USERPROFILE\.goclaw\bin" }

# ── Resolve version ──
if (-not $Version) {
  Write-Host "Fetching latest release..."
  $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "goclaw-installer" }
  $Version = $release.tag_name
}
Write-Host "Installing GoClaw $Version (windows/amd64)..."

$asset = "goclaw-$($Version.TrimStart('v'))-windows-amd64.zip"
$url = "https://github.com/$Repo/releases/download/$Version/$asset"
$tmp = Join-Path $env:TEMP ("goclaw-" + [guid]::NewGuid().ToString())

try {
  Write-Host "Downloading $url ..."
  $zip = Join-Path $tmp "$asset"
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  Invoke-WebRequest -Uri $url -OutFile $zip

  # ── Extract & install ──
  $extractDir = Join-Path $tmp "extract"
  New-Item -ItemType Directory -Force -Path $extractDir | Out-Null
  Expand-Archive -Path $zip -DestinationPath $extractDir -Force

  New-Item -ItemType Directory -Force -Path $Dir | Out-Null
  Copy-Item -Path (Join-Path $extractDir "goclaw.exe") -Destination $Dir -Force
  Copy-Item -Path (Join-Path $extractDir "migrations") -Destination $Dir -Recurse -Force -ErrorAction SilentlyContinue

  Write-Host ""
  Write-Host "GoClaw $Version installed to $Dir"
  Write-Host ""
  Write-Host "Next steps:"
  Write-Host "  1. Add to PATH (PowerShell):"
  Write-Host "     `$env:Path += `"$Dir`""
  Write-Host "  2. Start the gateway:"
  Write-Host "     goclaw onboard && goclaw"
  Write-Host ""
  Write-Host "  Web dashboard: http://localhost:18790"
  Write-Host "  Health check:  curl http://localhost:18790/health"
} finally {
  Remove-Item -Path $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
