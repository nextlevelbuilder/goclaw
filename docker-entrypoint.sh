#!/bin/sh
set -e

# Set up writable runtime directories for agent-installed packages.
# Rootfs is read-only; /app/data is a writable Docker volume.
RUNTIME_DIR="/app/data/.runtime"
mkdir -p "$RUNTIME_DIR/pip" "$RUNTIME_DIR/npm-global/lib"

# Python: allow agent to pip install to writable target dir
export PYTHONPATH="$RUNTIME_DIR/pip:${PYTHONPATH:-}"
export PIP_TARGET="$RUNTIME_DIR/pip"
export PIP_BREAK_SYSTEM_PACKAGES=1
export PIP_CACHE_DIR="$RUNTIME_DIR/pip-cache"
mkdir -p "$RUNTIME_DIR/pip-cache"

# Node.js: allow agent to npm install -g to writable prefix
# NODE_PATH includes both pre-installed system globals and runtime-installed globals.
export NPM_CONFIG_PREFIX="$RUNTIME_DIR/npm-global"
export NODE_PATH="/usr/local/lib/node_modules:$RUNTIME_DIR/npm-global/lib/node_modules:${NODE_PATH:-}"
export PATH="$RUNTIME_DIR/npm-global/bin:$RUNTIME_DIR/pip/bin:$PATH"

# Docker socket: verify access if mounted (for build_docker_image tool).
# The socket is mounted from host via -v /var/run/docker.sock:/var/run/docker.sock.
if [ -S /var/run/docker.sock ]; then
  if docker info >/dev/null 2>&1; then
    echo "Docker socket accessible — docker build enabled"
  else
    echo "Warning: Docker socket exists but is not accessible (check permissions)"
  fi
fi

# Claude Code: seed credentials into writable volume if not present.
# Volume mount overrides /app/.claude, so we seed from /etc/claude/ on first run.
if [ -d /etc/claude ] && [ ! -f /app/.claude/.credentials.json ]; then
  cp -a /etc/claude/. /app/.claude/
fi

# System packages: re-install on-demand packages persisted across recreates.
# In Docker: entrypoint runs as root (then drops via gosu/su).
# Outside Docker: may run as non-root — skip privileged operations gracefully.
APT_LIST="$RUNTIME_DIR/apt-packages"
touch "$APT_LIST" 2>/dev/null || true
if [ "$(id -u)" = "0" ]; then
  chown root:goclaw "$APT_LIST" 2>/dev/null || true
  chmod 0640 "$APT_LIST" 2>/dev/null || true
fi
if [ -f "$APT_LIST" ] && [ -s "$APT_LIST" ]; then
  echo "Re-installing persisted system packages..."
  VALID_PKGS=""
  while IFS= read -r pkg || [ -n "$pkg" ]; do
    pkg="$(printf '%s' "$pkg" | tr -d '[:space:]')"
    case "$pkg" in
      [a-zA-Z0-9@]*) VALID_PKGS="$VALID_PKGS $pkg" ;;
      "") ;;
      *) echo "WARNING: skipping invalid package: $pkg" ;;
    esac
  done < "$APT_LIST"
  if [ -n "$VALID_PKGS" ]; then
    # shellcheck disable=SC2086
    apt-get update && apt-get install -y --no-install-recommends $VALID_PKGS 2>/dev/null || \
      echo "Warning: some packages failed to install"
    rm -rf /var/lib/apt/lists/*
  fi
fi

# Start the root-privileged package helper (listens on /tmp/pkg.sock).
# Only in Docker (running as root). Outside Docker, pkg-helper is not available.
if [ -x /app/pkg-helper ] && [ "$(id -u)" = "0" ]; then
  /app/pkg-helper &
  PKG_PID=$!
  for _i in 1 2 3 4; do
    [ -S /tmp/pkg.sock ] && break
    sleep 0.5
  done
  if ! [ -S /tmp/pkg.sock ]; then
    echo "ERROR: pkg-helper failed to start (PID $PKG_PID)"
    kill "$PKG_PID" 2>/dev/null || true
  fi
fi

# Run command with privilege drop.
# Debian uses su instead of su-exec; fall back to direct exec if not root.
run_as_goclaw() {
  if [ "$(id -u)" = "0" ]; then
    exec su -s /bin/sh goclaw -c '"$0" "$@"' -- "$@"
  else
    exec "$@"
  fi
}

case "${1:-serve}" in
  serve)
    # Auto-upgrade (schema migrations + data hooks) before starting.
    if [ -n "$GOCLAW_POSTGRES_DSN" ]; then
      echo "Running database upgrade..."
      if [ "$(id -u)" = "0" ]; then
        su -s /bin/sh goclaw -c '/app/goclaw upgrade' || \
          echo "Upgrade warning (may already be up-to-date)"
      else
        /app/goclaw upgrade || \
          echo "Upgrade warning (may already be up-to-date)"
      fi
    fi
    run_as_goclaw /app/goclaw
    ;;
  upgrade)
    shift
    run_as_goclaw /app/goclaw upgrade "$@"
    ;;
  migrate)
    shift
    run_as_goclaw /app/goclaw migrate "$@"
    ;;
  onboard)
    run_as_goclaw /app/goclaw onboard
    ;;
  version)
    run_as_goclaw /app/goclaw version
    ;;
  *)
    run_as_goclaw /app/goclaw "$@"
    ;;
esac
