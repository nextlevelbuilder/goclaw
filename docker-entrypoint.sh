#!/bin/sh
set -e

# Ensure data directories exist with correct ownership on mounted volumes.
# This runs on every start so externally-created volumes get the right structure.
for dir in /app/workspace /app/data /app/sessions /app/skills /app/tsnet-state /app/.goclaw; do
  mkdir -p "$dir"
done
chown -R goclaw:goclaw /app

case "${1:-serve}" in
  serve)
    # Managed mode: auto-upgrade (schema migrations + data hooks) before starting.
    if [ "$GOCLAW_MODE" = "managed" ] && [ -n "$GOCLAW_POSTGRES_DSN" ]; then
      echo "Managed mode: running upgrade..."
      su-exec goclaw goclaw upgrade || \
        echo "Upgrade warning (may already be up-to-date)"
    fi
    exec su-exec goclaw goclaw
    ;;
  upgrade)
    shift
    exec su-exec goclaw goclaw upgrade "$@"
    ;;
  migrate)
    shift
    exec su-exec goclaw goclaw migrate "$@"
    ;;
  onboard)
    exec su-exec goclaw goclaw onboard
    ;;
  version)
    exec su-exec goclaw goclaw version
    ;;
  *)
    exec su-exec goclaw goclaw "$@"
    ;;
esac
