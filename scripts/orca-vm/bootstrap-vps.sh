#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo 'Run this script as root on the approved VPS.' >&2
  exit 1
fi

go_version='1.26.0'
node_version='22.16.0'

apt-get update
apt-get install -y ca-certificates curl xz-utils gh

if ! command -v go >/dev/null 2>&1; then
  test ! -e /usr/local/go || { echo '/usr/local/go already exists; inspect it before installing Go.' >&2; exit 1; }
  archive="go${go_version}.linux-amd64.tar.gz"
  temp_dir="$(mktemp -d)"
  trap 'rm -rf "$temp_dir"' EXIT
  curl -fsSLo "$temp_dir/$archive" "https://go.dev/dl/$archive"
  printf '%s  %s\n' 'aac1b08a0fb0c4e0a7c1555beb7b59180b05dfc5a3d62e40e9de90cd42f88235' "$temp_dir/$archive" | sha256sum -c -
  tar -C /usr/local -xzf "$temp_dir/$archive"
  ln -s /usr/local/go/bin/go /usr/local/bin/go
  ln -s /usr/local/go/bin/gofmt /usr/local/bin/gofmt
fi

if ! command -v node >/dev/null 2>&1; then
  archive="node-v${node_version}-linux-x64.tar.xz"
  temp_dir="$(mktemp -d)"
  trap 'rm -rf "$temp_dir"' EXIT
  curl -fsSLo "$temp_dir/$archive" "https://nodejs.org/dist/v${node_version}/$archive"
  curl -fsSLo "$temp_dir/SHASUMS256.txt" "https://nodejs.org/dist/v${node_version}/SHASUMS256.txt"
  (cd "$temp_dir" && grep "  $archive\$" SHASUMS256.txt | sha256sum -c -)
  tar -C /opt -xJf "$temp_dir/$archive"
  for tool in node npm npx corepack; do
    ln -s "/opt/node-v${node_version}-linux-x64/bin/$tool" "/usr/local/bin/$tool"
  done
fi

command -v pnpm >/dev/null 2>&1 || npm install --global pnpm@10
command -v codex >/dev/null 2>&1 || npm install --global @openai/codex
for tool in pnpm codex; do
  if [ ! -e "/usr/local/bin/$tool" ]; then
    ln -s "/opt/node-v${node_version}-linux-x64/bin/$tool" "/usr/local/bin/$tool"
  fi
done

go version
node --version
pnpm --version
codex --version
gh --version | head -n 1
