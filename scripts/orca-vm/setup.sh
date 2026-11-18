#!/usr/bin/env bash
set -euo pipefail

for tool in git go node pnpm codex gh; do
  command -v "$tool" >/dev/null 2>&1 || { echo "Missing $tool on the VPS" >&2; exit 1; }
done

go mod download
pnpm --dir ui/web install --frozen-lockfile
pnpm --dir ui/desktop/frontend install --frozen-lockfile
