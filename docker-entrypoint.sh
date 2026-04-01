#!/bin/sh
set -e

# Set up writable runtime directories for agent-installed packages.
# Rootfs is read-only; /app/data is a writable Docker volume.
RUNTIME_DIR="/app/data/.runtime"
# Non-fatal: on first start with a fresh volume the directory may not be
# writable yet (volume initialisation race on some Docker runtimes).
# The app starts fine without .runtime; package installs will fail gracefully.
mkdir -p "$RUNTIME_DIR/pip" "$RUNTIME_DIR/npm-global/lib" "$RUNTIME_DIR/pip-cache" || true

# Fix .runtime ownership for split root/goclaw access.
# .runtime itself must be root-owned so pkg-helper (root) can write apk-packages.
# Subdirs pip/, npm-global/, pip-cache/ must be goclaw-owned for runtime installs.
# This also handles upgrades from older images where .runtime was fully goclaw-owned.
if [ "$(id -u)" = "0" ] && [ -d "$RUNTIME_DIR" ]; then
  chown root:goclaw "$RUNTIME_DIR" 2>/dev/null || true
  chmod 0750 "$RUNTIME_DIR" 2>/dev/null || true
  chown -R goclaw:goclaw "$RUNTIME_DIR/pip" "$RUNTIME_DIR/npm-global" "$RUNTIME_DIR/pip-cache" 2>/dev/null || true
fi

# Fix workspace directory ownership: handle dirs created by root in previous
# container lifecycle or via manual docker exec.
# Security: -type d = real directories only (not symlinks).
# find default -P mode = never follow symlinks. -maxdepth 5 limits traversal.
if [ "$(id -u)" = "0" ] && [ -d /app/workspace ]; then
  find /app/workspace -maxdepth 5 -type d -not -user goclaw \
    -exec chown goclaw:goclaw {} + 2>/dev/null || true
fi

# Python: allow agent to pip install to writable target dir
export PYTHONPATH="$RUNTIME_DIR/pip:${PYTHONPATH:-}"
export PIP_TARGET="$RUNTIME_DIR/pip"
export PIP_BREAK_SYSTEM_PACKAGES=1
export PIP_CACHE_DIR="$RUNTIME_DIR/pip-cache"

# Node.js: allow agent to npm install -g to writable prefix
# NODE_PATH includes both pre-installed system globals and runtime-installed globals.
export NPM_CONFIG_PREFIX="$RUNTIME_DIR/npm-global"
export NODE_PATH="/usr/local/lib/node_modules:$RUNTIME_DIR/npm-global/lib/node_modules:${NODE_PATH:-}"
export PATH="$RUNTIME_DIR/npm-global/bin:$RUNTIME_DIR/pip/bin:$PATH"

# System packages: re-install on-demand packages persisted across recreates.
# After chown above, root owns .runtime and can create this file.
APK_LIST="$RUNTIME_DIR/apk-packages"
if [ "$(id -u)" = "0" ]; then
  touch "$APK_LIST" 2>/dev/null || true
  chown root:goclaw "$APK_LIST" 2>/dev/null || true
  chmod 0640 "$APK_LIST" 2>/dev/null || true
fi
if [ -f "$APK_LIST" ] && [ -s "$APK_LIST" ]; then
  echo "Re-installing persisted system packages..."
  VALID_PKGS=""
  while IFS= read -r pkg || [ -n "$pkg" ]; do
    pkg="$(printf '%s' "$pkg" | tr -d '[:space:]')"
    case "$pkg" in
      [a-zA-Z0-9@]*) VALID_PKGS="$VALID_PKGS $pkg" ;;
      "") ;;
      *) echo "WARNING: skipping invalid package: $pkg" ;;
    esac
  done < "$APK_LIST"
  if [ -n "$VALID_PKGS" ]; then
    # shellcheck disable=SC2086
    apk add --no-cache $VALID_PKGS 2>/dev/null || \
      echo "Warning: some packages failed to install"
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

# Copy Claude CLI credentials from root-owned read-only mount to goclaw-accessible location.
# /app/.claude is a symlink → /app/data/.claude (writable volume, see Dockerfile).
# Uses su-exec to copy as goclaw user because sandbox overlay's cap_add override
# may remove CHOWN needed by install(1). umask 077 ensures file is created with 600.
if [ -f /app/.claude-host/.credentials.json ]; then
  (mkdir -p /app/data/.claude \
    && if command -v su-exec >/dev/null 2>&1 && [ "$(id -u)" = "0" ]; then
         su-exec goclaw sh -c 'umask 077 && cp /app/.claude-host/.credentials.json /app/data/.claude/.credentials.json'
       else
         ( umask 077 && cp /app/.claude-host/.credentials.json /app/data/.claude/.credentials.json )
       fi \
    && echo "Claude CLI credentials synced from host.") || echo "WARNING: Claude credentials copy failed (non-fatal)"
fi

# Warn if Claude credentials are mounted but CLI binary is missing (forgot --build).
if [ -d /app/.claude-host ] && ! command -v claude >/dev/null 2>&1; then
  echo "WARNING: Claude credentials mounted but claude CLI not installed. Rebuild with: --build"
fi

# Run command with privilege drop (su-exec in Docker, direct otherwise).
run_as_goclaw() {
  if command -v su-exec >/dev/null 2>&1 && [ "$(id -u)" = "0" ]; then
    exec su-exec goclaw "$@"
  else
    exec "$@"
  fi
}

case "${1:-serve}" in
  serve)
    # Auto-upgrade (schema migrations + data hooks) before starting.
    if [ -n "$GOCLAW_POSTGRES_DSN" ]; then
      echo "Running database upgrade..."
      if command -v su-exec >/dev/null 2>&1 && [ "$(id -u)" = "0" ]; then
        su-exec goclaw /app/goclaw upgrade || \
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
