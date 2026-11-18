#requires -Version 5.1
$ErrorActionPreference = 'Stop'

$ssh = Join-Path $env:WINDIR 'System32\OpenSSH\ssh.exe'
if (-not (Test-Path $ssh)) { throw 'Windows OpenSSH client is required.' }
$alias = 'tuyetchinh-vps'
$root = '/root/orca-workspaces/goclaw'
$repo = 'https://github.com/tuyetchinh1501/goclaw.git'

$config = @{}
& $ssh -G $alias | ForEach-Object {
    if ($_ -match '^(hostname|port|user|identityfile)\s+(.+)$') {
        if (-not $config.ContainsKey($Matches[1])) { $config[$Matches[1]] = $Matches[2] }
    }
}
if ($LASTEXITCODE -ne 0) { throw 'Could not read SSH target configuration.' }
foreach ($field in @('hostname', 'port', 'user')) {
    if (-not $config[$field]) { throw "SSH target has no $field." }
}

$remoteScript = @'
set -euo pipefail
root="$1"
repo="$2"
export GIT_TERMINAL_PROMPT=0
if [ ! -e "$root" ]; then
  mkdir -p "$(dirname "$root")"
  git clone "$repo" "$root"
fi
test -d "$root/.git" || { echo "Expected a Git checkout at $root" >&2; exit 1; }
current_remote="$(git -C "$root" remote get-url origin)"
if [ "$current_remote" = 'https://github.com/nextlevelbuilder/goclaw.git' ]; then
  git -C "$root" remote set-url origin "$repo"
elif [ "$current_remote" != "$repo" ]; then
  echo 'Checkout remote differs from recipe' >&2
  exit 1
fi
git -C "$root" fetch origin
'@
$encodedScript = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($remoteScript))
$remoteCommand = "echo $encodedScript | base64 -d | bash -s -- '$root' '$repo'"
& $ssh -o BatchMode=yes -o StrictHostKeyChecking=yes $alias $remoteCommand | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'VPS checkout preparation failed.' }

$target = @{
    label = $alias
    host = $config.hostname
    port = [int]$config.port
    username = $config.user
}
if ($config.identityfile) {
    $identity = $config.identityfile -replace '^~(?=[\\/])', $HOME
    $target.identityFile = $identity
    $target.identitiesOnly = $true
}
@{
    schemaVersion = 1
    connection = @{
        type = 'ssh'
        projectRoot = $root
        target = $target
    }
} | ConvertTo-Json -Compress -Depth 5
