<#
  Builds the Windows desktop app into <OutDir> (default winapp-out):

    goclaw-server.exe   the full gateway with the web UI embedded (-tags embedui),
                        same as `make build-full`
    GoClawDesktop.exe          the native window shell (ui/desktop-full)
    pg_dump.exe         shim that forwards pg_dump into the PostgreSQL container

  Everything is built into a staging folder first and swapped in only when all
  three binaries exist, so a failed build never leaves partial output.

  The installer (GoClaw-Setup-<version>.exe, NSIS) is built from
  ui/desktop-full/installer/goclaw.nsi when makensis is installed.

  Usage:  powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-winapp.ps1
          [-Version v3.15.0] [-OutDir winapp-out] [-SkipWeb] [-NoInstaller]

  -SkipWeb reuses the web UI already in internal/webui/dist; it fails when that
  folder is missing or empty. -NoInstaller stops after the three binaries.
#>
[CmdletBinding()]
param(
  [string]$Version = '',
  [string]$OutDir = 'winapp-out',
  [switch]$SkipWeb,
  [switch]$NoInstaller
)

$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Set-Location $root

function Step([string]$Message) { Write-Host "==> $Message" }

# Runs a native command and stops on a non-zero exit code.
function Invoke-Native([string]$What, [scriptblock]$Command) {
  & $Command
  if ($LASTEXITCODE -ne 0) { throw "$What failed with exit code $LASTEXITCODE" }
}

if (-not $Version) {
  $Version = (git describe --tags --abbrev=0 --match 'v[0-9]*' 2>$null)
  if ($LASTEXITCODE -ne 0 -or -not $Version) { $Version = 'dev' }
}
$commit = (git rev-parse HEAD 2>$null)
if ($LASTEXITCODE -ne 0 -or -not $commit) { $commit = 'unknown' }
$ldflags = "-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=$Version -X github.com/nextlevelbuilder/goclaw/cmd.CommitSHA=$commit"

# Output paths: refuse anything outside the repository so a typo cannot delete elsewhere.
$final = [IO.Path]::GetFullPath((Join-Path $root $OutDir))
if (-not $final.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
  throw "OutDir must be inside the repository: $final"
}
$staging = "$final.staging"
if (Test-Path -LiteralPath $staging) { Remove-Item -LiteralPath $staging -Recurse -Force }
New-Item -ItemType Directory -Path $staging | Out-Null

$embedDir = Join-Path $root 'internal\webui\dist'

if ($SkipWeb) {
  Step 'Reusing the existing embedded web UI (-SkipWeb)'
  if (-not (Test-Path -LiteralPath (Join-Path $embedDir 'index.html'))) {
    throw 'internal/webui/dist has no index.html; run without -SkipWeb first'
  }
} else {
  Step 'Building the web UI (ui/web)'
  Push-Location (Join-Path $root 'ui\web')
  try {
    Invoke-Native 'pnpm install' { pnpm install --frozen-lockfile }
    Invoke-Native 'pnpm build' { pnpm build }
  } finally { Pop-Location }
  $webOut = Join-Path $root 'ui\web\dist'
  if (-not (Test-Path -LiteralPath (Join-Path $webOut 'index.html'))) { throw 'the web build produced no index.html' }

  Step 'Refreshing internal/webui/dist (delete, recreate, copy: a stale file would be embedded)'
  if (Test-Path -LiteralPath $embedDir) { Remove-Item -LiteralPath $embedDir -Recurse -Force }
  New-Item -ItemType Directory -Path $embedDir | Out-Null
  Copy-Item -Path (Join-Path $webOut '*') -Destination $embedDir -Recurse -Force
}

$env:CGO_ENABLED = '0'

Step "Building goclaw-server.exe ($Version, $($commit.Substring(0, [Math]::Min(8, $commit.Length))))"
Invoke-Native 'go build goclaw-server' {
  go build -tags embedui -ldflags $ldflags -o (Join-Path $staging 'goclaw-server.exe') .
}

Step 'Building GoClawDesktop.exe'
Invoke-Native 'go build GoClaw' {
  go build -tags 'desktop,production' -ldflags '-s -w -H windowsgui' -o (Join-Path $staging 'GoClawDesktop.exe') ./ui/desktop-full
}

Step 'Building pg_dump.exe'
Invoke-Native 'go build pg_dump shim' {
  go build -ldflags '-s -w' -o (Join-Path $staging 'pg_dump.exe') ./ui/desktop-full/pgdump-shim
}

$installerDir = Join-Path $root 'ui\desktop-full\installer'
# The window loads its title-bar icon from goclaw.ico next to the executable.
Copy-Item -LiteralPath (Join-Path $installerDir 'goclaw.ico') -Destination $staging

foreach ($name in 'goclaw-server.exe', 'GoClawDesktop.exe', 'pg_dump.exe', 'goclaw.ico') {
  $f = Join-Path $staging $name
  if (-not (Test-Path -LiteralPath $f) -or (Get-Item -LiteralPath $f).Length -eq 0) { throw "missing or empty output: $name" }
}
$reported = (& (Join-Path $staging 'goclaw-server.exe') version) -join ' '
if ($reported -notmatch [regex]::Escape($Version)) { throw "goclaw-server.exe reports '$reported', expected version $Version" }

if (-not $NoInstaller) {
  $makensis = (Get-Command makensis -ErrorAction SilentlyContinue).Source
  if (-not $makensis) {
    foreach ($c in "${env:ProgramFiles(x86)}\NSIS\makensis.exe", "$env:ProgramFiles\NSIS\makensis.exe") {
      if (Test-Path -LiteralPath $c) { $makensis = $c; break }
    }
  }
  if (-not $makensis) { throw 'makensis not found; install NSIS (winget install NSIS.NSIS) or pass -NoInstaller' }

  # Four-part numeric version for the installer: v3.15.0-beta.228 -> 3.15.0.228
  $viVersion = '0.0.0.0'
  if ($Version -match '^v?(\d+)\.(\d+)\.(\d+)(?:[-.][A-Za-z]+\.?(\d+))?') {
    $fourth = if ($Matches[4]) { $Matches[4] } else { '0' }
    $viVersion = "$($Matches[1]).$($Matches[2]).$($Matches[3]).$fourth"
  }
  $setup = Join-Path $staging "GoClaw-Setup-$Version.exe"
  Step "Building the installer ($viVersion)"
  Invoke-Native 'makensis' {
    & $makensis /V2 "/DVERSION=$Version" "/DVIVERSION=$viVersion" "/DSRCDIR=$staging" "/DOUTFILE=$setup" (Join-Path $installerDir 'goclaw.nsi')
  }
  if (-not (Test-Path -LiteralPath $setup)) { throw 'makensis produced no installer' }
}

if (Test-Path -LiteralPath $final) { Remove-Item -LiteralPath $final -Recurse -Force }
Move-Item -LiteralPath $staging -Destination $final

Step "Done: $final"
Get-ChildItem -LiteralPath $final | ForEach-Object { '{0,-20} {1,8:N1} MB' -f $_.Name, ($_.Length / 1MB) }
Write-Host $reported
