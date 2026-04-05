#!/bin/bash
# GoClaw Tunnel Manager — starts quick tunnel, auto-updates Pages secret + redeploy
# Designed to run as a launchd service
set -euo pipefail

ACCOUNT_ID="66c7a2cd2dfc18cf735cc0209cf34979"
PROJECT="nta-goclaw"
WEB_DIST="/Users/nta7/nta-project-mac-mini/nta-goclaw/ui/web/dist"
LOG_FILE="/tmp/goclaw-tunnel.log"
URL_FILE="/tmp/goclaw-tunnel-url"

# Ensure PATH includes npm/node/cloudflared
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"
export HOME="/Users/nta7"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" | tee -a "$LOG_FILE"; }

update_and_redeploy() {
  local url="$1"
  log "Updating Pages secret BACKEND_URL → $url"
  echo "$url" | CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" \
    npx wrangler pages secret put BACKEND_URL \
    --project-name "$PROJECT" 2>>"$LOG_FILE" && \
    log "Secret updated OK" || { log "WARN: Failed to update secret"; return 1; }

  # Quick redeploy (no rebuild — just re-upload existing dist to pick up new secret)
  log "Triggering quick redeploy..."
  CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" \
    npx wrangler pages deploy "$WEB_DIST" \
    --project-name "$PROJECT" \
    --branch dev \
    --commit-dirty=true 2>>"$LOG_FILE" && \
    log "Redeploy OK" || log "WARN: Redeploy failed"
}

start_tunnel() {
  log "Starting cloudflared quick tunnel..."

  # Use --config /dev/null to ignore any named tunnel config
  cloudflared tunnel --url http://localhost:18790 --no-autoupdate --no-tls-verify 2>&1 | while IFS= read -r line; do
    echo "$line" >> "$LOG_FILE"

    # Extract tunnel URL from output
    if echo "$line" | grep -qo 'https://[a-z0-9-]*\.trycloudflare\.com'; then
      TUNNEL_URL=$(echo "$line" | grep -o 'https://[a-z0-9-]*\.trycloudflare\.com')
      log "Tunnel URL: $TUNNEL_URL"
      echo "$TUNNEL_URL" > "$URL_FILE"

      # Check if URL changed from last known
      OLD_URL=""
      if [ -f "${URL_FILE}.prev" ]; then
        OLD_URL=$(cat "${URL_FILE}.prev" 2>/dev/null || true)
      fi

      if [ "$TUNNEL_URL" != "$OLD_URL" ]; then
        update_and_redeploy "$TUNNEL_URL"
        echo "$TUNNEL_URL" > "${URL_FILE}.prev"
      else
        log "URL unchanged, skipping update"
      fi
    fi
  done

  log "cloudflared exited"
}

log "=== Tunnel Manager starting ==="
start_tunnel
log "=== Tunnel Manager stopped ==="
