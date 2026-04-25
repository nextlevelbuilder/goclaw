#!/bin/bash
# tunnel-manager.sh — Manages cloudflared quick tunnel for GoClaw
# Auto-detects URL changes, updates CF Pages secret, redeploy

ACCOUNT_ID="66c7a2cd2dfc18cf735cc0209cf34979"
PROJECT="nta-goclaw"
TUNNEL_LOG="/tmp/goclaw-tunnel.log"
TUNNEL_URL_FILE="/tmp/goclaw-tunnel-url"
DIST_DIR="/Users/nta7/nta-project-mac-mini/nta-goclaw/internal/webui/dist"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

# Kill existing cloudflared (but not this script)
pkill -f "cloudflared tunnel --url" 2>/dev/null || true
sleep 2

# Start tunnel
log "Starting cloudflared quick tunnel..."
: > "$TUNNEL_LOG"
cloudflared tunnel --url http://localhost:18790 --no-autoupdate >> "$TUNNEL_LOG" 2>&1 &
TUNNEL_PID=$!

# Wait for tunnel URL (max 30s)
TUNNEL_URL=""
for i in $(seq 1 30); do
    TUNNEL_URL=$(grep -o 'https://[a-z0-9-]*\.trycloudflare\.com' "$TUNNEL_LOG" 2>/dev/null | tail -1 || true)
    if [ -n "$TUNNEL_URL" ]; then
        break
    fi
    sleep 1
done

if [ -z "$TUNNEL_URL" ]; then
    log "ERROR: Failed to get tunnel URL after 30s"
    cat "$TUNNEL_LOG"
    exit 1
fi

log "Tunnel URL: $TUNNEL_URL"
echo "$TUNNEL_URL" > "$TUNNEL_URL_FILE"

# Check if URL changed
OLD_URL=$(cat "${TUNNEL_URL_FILE}.prev" 2>/dev/null || true)

if [ "$TUNNEL_URL" != "$OLD_URL" ]; then
    log "URL changed, updating Pages secret..."
    printf '%s' "$TUNNEL_URL" | CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" npx wrangler pages secret put BACKEND_URL --project-name "$PROJECT" 2>&1 | tail -1 || true

    log "Redeploying Pages..."
    CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" npx wrangler pages deploy "$DIST_DIR" --project-name "$PROJECT" --commit-dirty=true 2>&1 | tail -1 || true

    echo "$TUNNEL_URL" > "${TUNNEL_URL_FILE}.prev"
    log "Deploy complete"
else
    log "URL unchanged, skipping deploy"
fi

# Wait for tunnel process (keeps launchd happy)
log "Tunnel running (PID $TUNNEL_PID), waiting..."
wait $TUNNEL_PID
