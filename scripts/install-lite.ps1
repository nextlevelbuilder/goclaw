# GoClaw Lite (Desktop) installer for Windows
#
# Usage:
#   irm https://raw.githubusercontent.com/nextlevelbuilder/goclaw/main/scripts/install-lite.ps1 | iex
#   .\install-lite.ps1 -Version lite-v0.1.0

param([string]$Version = "")

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"  # Speeds up Invoke-WebRequest significantly

function Exit-WithPause {
    param([int]$Code = 0)
    Write-Host ""
    Write-Host "Press any key to exit..." -ForegroundColor Gray
    try { $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown") } catch { Start-Sleep -Seconds 5 }
    exit $Code
}
$Repo = "nextlevelbuilder/goclaw"
$InstallDir = Join-Path $env:LOCALAPPDATA "GoClaw Lite"

# ── Resolve version ──
if (-not $Version) {
    Write-Host "-> Fetching latest desktop release..." -ForegroundColor Cyan
    try {
        $releases = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases?per_page=100" -ErrorAction Stop
    } catch {
        Write-Host "Failed to fetch releases: $_" -ForegroundColor Red
        Write-Host "Check: https://github.com/$Repo/releases" -ForegroundColor Yellow
        Exit-WithPause 1
    }
    $latest = $releases | Where-Object { $_.tag_name -like "lite-v*" -and -not $_.prerelease -and -not $_.draft } | Select-Object -First 1
    if (-not $latest) {
        Write-Host "No desktop release found." -ForegroundColor Red
        Write-Host "Check: https://github.com/$Repo/releases" -ForegroundColor Yellow
        Exit-WithPause 1
    }
    $Version = $latest.tag_name
}

$Semver = $Version -replace "^lite-v", ""
Write-Host "-> Installing GoClaw Lite v$Semver..." -ForegroundColor Cyan

# ── Download ──
$Asset = "goclaw-lite-$Semver-windows-amd64.zip"
$Url = "https://github.com/$Repo/releases/download/$Version/$Asset"
$TmpZip = Join-Path $env:TEMP $Asset

Write-Host "-> Downloading $Asset..."
try {
    Invoke-WebRequest -Uri $Url -OutFile $TmpZip -UseBasicParsing -ErrorAction Stop
} catch {
    Write-Host "Download failed: $_" -ForegroundColor Red
    Write-Host "URL: $Url" -ForegroundColor Yellow
    Exit-WithPause 1
}

# ── Extract ──
Write-Host "-> Installing to $InstallDir..."
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Expand-Archive -Path $TmpZip -DestinationPath $InstallDir -Force
Remove-Item $TmpZip -Force -ErrorAction SilentlyContinue

# ── Create Start Menu shortcut ──
$ExePath = Join-Path $InstallDir "goclaw-lite.exe"
if (Test-Path $ExePath) {
    try {
        $StartMenu = [Environment]::GetFolderPath("StartMenu")
        $ShortcutPath = Join-Path $StartMenu "Programs\GoClaw Lite.lnk"
        $Shell = New-Object -ComObject WScript.Shell
        $Shortcut = $Shell.CreateShortcut($ShortcutPath)
        $Shortcut.TargetPath = $ExePath
        $Shortcut.WorkingDirectory = $InstallDir
        $Shortcut.Save()
        Write-Host "-> Start Menu shortcut created" -ForegroundColor Gray
    } catch {
        Write-Host "-> Could not create shortcut (non-fatal): $_" -ForegroundColor Yellow
    }
}

# ── Done ──
Write-Host ""
Write-Host "GoClaw Lite v$Semver installed!" -ForegroundColor Green
Write-Host "  Location: $InstallDir" -ForegroundColor Gray
Write-Host ""
Write-Host "-> Launching GoClaw Lite..."
if (Test-Path $ExePath) { Start-Process $ExePath }
Exit-WithPause 0
