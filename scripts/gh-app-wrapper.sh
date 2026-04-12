#!/bin/sh
set -eu

HELPER="${GOCLAW_GITHUB_APP_HELPER:-/app/scripts/github_app_token.py}"
REAL_GH="${GOCLAW_REAL_GH_PATH:-}"

if [ -z "$REAL_GH" ]; then
  echo "GOCLAW_REAL_GH_PATH is not configured" >&2
  exit 1
fi

GH_TOKEN="$("$HELPER" gh-token --cwd "$PWD" -- "$@")"
export GH_TOKEN

exec "$REAL_GH" "$@"
