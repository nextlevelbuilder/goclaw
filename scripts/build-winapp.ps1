<#
  Builds the Windows desktop app into <OutDir> (default winapp-out):

    goclaw-server.exe   the full gateway with the web UI embedded (-tags embedui),
                        same as `make build-full`
    GoClaw.exe          the native window shell (ui/desktop-full)
    pg_dump.exe         shim that forwards pg_dump into the PostgreSQL container

  Everything is built into a staging folder first and swapped in only when all
  three binaries exist, so a failed build never leaves partial output.

  Usage:  powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-winapp.ps1
          [-Version v3.15.0] [-OutDir winapp-out] [-SkipWeb]

  -SkipWeb reuses the web UI already in internal/webui/dist; it fails when that
  folder is missing or empty.
#>
[CmdletBinding()]
param(
  [string]$Version = '',
  [string]$OutDir = 'winapp-out',
  [switch]$SkipWeb
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

Step 'Building GoClaw.exe'
Invoke-Native 'go build GoClaw' {
  go build -tags 'desktop,production' -ldflags '-s -w -H windowsgui' -o (Join-Path $staging 'GoClaw.exe') ./ui/desktop-full
}

Step 'Building pg_dump.exe'
Invoke-Native 'go build pg_dump shim' {
  go build -ldflags '-s -w' -o (Join-Path $staging 'pg_dump.exe') ./ui/desktop-full/pgdump-shim
}

foreach ($name in 'goclaw-server.exe', 'GoClaw.exe', 'pg_dump.exe') {
  $f = Join-Path $staging $name
  if (-not (Test-Path -LiteralPath $f) -or (Get-Item -LiteralPath $f).Length -eq 0) { throw "missing or empty output: $name" }
}
$reported = (& (Join-Path $staging 'goclaw-server.exe') version) -join ' '
if ($reported -notmatch [regex]::Escape($Version)) { throw "goclaw-server.exe reports '$reported', expected version $Version" }

if (Test-Path -LiteralPath $final) { Remove-Item -LiteralPath $final -Recurse -Force }
Move-Item -LiteralPath $staging -Destination $final

Step "Done: $final"
Get-ChildItem -LiteralPath $final | ForEach-Object { '{0,-20} {1,8:N1} MB' -f $_.Name, ($_.Length / 1MB) }
Write-Host $reported
