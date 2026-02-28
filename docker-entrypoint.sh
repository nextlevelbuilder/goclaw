#!/bin/sh
set -e

case "${1:-serve}" in
  serve)
    # Managed mode: auto-upgrade (schema migrations + data hooks) before starting.
    if [ "$GOCLAW_MODE" = "managed" ] && [ -n "$GOCLAW_POSTGRES_DSN" ]; then
      echo "Managed mode: running upgrade..."
      goclaw upgrade || \
        echo "Upgrade warning (may already be up-to-date)"
    fi
    exec goclaw
    ;;
  upgrade)
    shift
    exec goclaw upgrade "$@"
    ;;
  migrate)
    shift
    exec goclaw migrate "$@"
    ;;
  onboard)
    exec goclaw onboard
    ;;
  version)
    exec goclaw version
    ;;
  *)
    exec goclaw "$@"
    ;;
esac
