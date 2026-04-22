#!/usr/bin/env bash
set -e

CONFIG_PATH=/data/options.json

# ── Read addon options ──
POSTGRES_HOST=$(jq -r '.postgres_host' "$CONFIG_PATH")
POSTGRES_PORT=$(jq -r '.postgres_port' "$CONFIG_PATH")
POSTGRES_USER=$(jq -r '.postgres_user' "$CONFIG_PATH")
POSTGRES_PASSWORD=$(jq -r '.postgres_password' "$CONFIG_PATH")
POSTGRES_DATABASE=$(jq -r '.postgres_database' "$CONFIG_PATH")
REDIS_HOST=$(jq -r '.redis_host // empty' "$CONFIG_PATH")
REDIS_PORT=$(jq -r '.redis_port' "$CONFIG_PATH")
ENABLE_BROWSER=$(jq -r '.enable_browser' "$CONFIG_PATH")
GATEWAY_TOKEN=$(jq -r '.gateway_token // empty' "$CONFIG_PATH")
ENCRYPTION_KEY=$(jq -r '.encryption_key // empty' "$CONFIG_PATH")
TRACE_VERBOSE=$(jq -r '.trace_verbose // false' "$CONFIG_PATH")

# ── Data directories ──
mkdir -p /data/goclaw/skills /data/goclaw/workspace

# ── Runtime paths (from docker-entrypoint.sh) ──
RUNTIME_DIR="/app/data/.runtime"
mkdir -p "$RUNTIME_DIR/pip" "$RUNTIME_DIR/npm-global/lib" "$RUNTIME_DIR/pip-cache" 2>/dev/null || true
export PYTHONPATH="$RUNTIME_DIR/pip:${PYTHONPATH:-}"
export PIP_TARGET="$RUNTIME_DIR/pip"
export PIP_BREAK_SYSTEM_PACKAGES=1
export PIP_CACHE_DIR="$RUNTIME_DIR/pip-cache"
export NPM_CONFIG_PREFIX="$RUNTIME_DIR/npm-global"
export NODE_PATH="/usr/local/lib/node_modules:$RUNTIME_DIR/npm-global/lib/node_modules:${NODE_PATH:-}"
export PATH="$RUNTIME_DIR/npm-global/bin:$RUNTIME_DIR/pip/bin:$PATH"

# ── Core environment ──
export GOCLAW_POSTGRES_DSN="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@${POSTGRES_HOST}:${POSTGRES_PORT}/${POSTGRES_DATABASE}?sslmode=disable"
export GOCLAW_CONFIG=/data/goclaw/config.json
export GOCLAW_WORKSPACE=/data/goclaw/workspace
export GOCLAW_DATA_DIR=/data/goclaw
export GOCLAW_SKILLS_DIR=/data/goclaw/skills
export GOCLAW_HOST=0.0.0.0
export GOCLAW_PORT=18790

# ── Gateway token (auto-generate & persist if unset) ──
TOKEN_FILE=/data/goclaw/gateway.token
if [ -z "$GATEWAY_TOKEN" ]; then
    if [ ! -f "$TOKEN_FILE" ]; then
        head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > "$TOKEN_FILE"
        chmod 600 "$TOKEN_FILE"
        echo "Generated new gateway token at $TOKEN_FILE"
    fi
    GATEWAY_TOKEN=$(cat "$TOKEN_FILE")
    echo "────────────────────────────────────────────────────────────"
    echo "  Gateway Token (use this to log in to the web dashboard):"
    echo "  $GATEWAY_TOKEN"
    echo "────────────────────────────────────────────────────────────"
fi
export GOCLAW_GATEWAY_TOKEN="$GATEWAY_TOKEN"

[ -n "$ENCRYPTION_KEY" ] && export GOCLAW_ENCRYPTION_KEY="$ENCRYPTION_KEY"
[ "$TRACE_VERBOSE" = "true" ] && export GOCLAW_TRACE_VERBOSE=1

# ── Redis (external addon only) ──
if [ -n "$REDIS_HOST" ]; then
    export GOCLAW_REDIS_DSN="redis://${REDIS_HOST}:${REDIS_PORT}/0"
    echo "Using Redis at ${REDIS_HOST}:${REDIS_PORT}"
fi

# ── Browser (headless Chromium) ──
CHROME_PID=""
if [ "$ENABLE_BROWSER" = "true" ]; then
    CHROME_BIN=$(command -v chromium-browser 2>/dev/null || command -v chromium 2>/dev/null || true)
    if [ -n "$CHROME_BIN" ]; then
        echo "Starting headless Chromium..."
        "$CHROME_BIN" \
            --headless \
            --no-sandbox \
            --remote-debugging-address=0.0.0.0 \
            --remote-debugging-port=9222 \
            --remote-allow-origins=* \
            --disable-gpu \
            --disable-dev-shm-usage \
            --disable-software-rasterizer \
            --disable-extensions &
        CHROME_PID=$!
        export GOCLAW_BROWSER_REMOTE_URL="ws://localhost:9222"

        for _ in $(seq 1 15); do
            if wget -qO- http://127.0.0.1:9222/json/version >/dev/null 2>&1; then
                echo "Chromium ready (CDP on :9222)"
                break
            fi
            sleep 1
        done
    else
        echo "WARNING: Chromium not found, browser automation disabled"
    fi
fi

# ── Cleanup handler ──
cleanup() {
    echo "Shutting down..."
    [ -n "$CHROME_PID" ] && kill "$CHROME_PID" 2>/dev/null || true
    wait 2>/dev/null || true
}
trap cleanup SIGTERM SIGINT

# ── Database upgrade ──
echo "Running database upgrade..."
/app/goclaw upgrade || echo "Upgrade notice: database may already be up-to-date"

# ── Start GoClaw ──
echo "Starting GoClaw Gateway on :18790..."
/app/goclaw &
GOCLAW_PID=$!
wait "$GOCLAW_PID"
