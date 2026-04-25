#!/bin/bash
# watchdog.sh — Health check + auto-restart for GoClaw services
# Checks: GoClaw server, Vite dev, Tunnel, CF Pages
# Run via launchd every 2 hours
set -uo pipefail

PROJECT_DIR="/Users/nta7/nta-project-mac-mini/nta-goclaw"
LOG="/tmp/goclaw-watchdog.log"
ACCOUNT_ID="66c7a2cd2dfc18cf735cc0209cf34979"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "$LOG"; }

log "=== Watchdog run ==="

# 1. GoClaw server
if curl -sf http://localhost:18790/health > /dev/null 2>&1; then
    log "GoClaw: OK"
else
    log "GoClaw: DOWN — restarting..."
    lsof -ti:18790 | xargs kill -9 2>/dev/null
    sleep 1
    cd "$PROJECT_DIR"
    source .env.local
    ./goclaw > /tmp/goclaw.log 2>&1 &
    sleep 5
    if curl -sf http://localhost:18790/health > /dev/null 2>&1; then
        log "GoClaw: restarted OK"
    else
        log "GoClaw: FAILED to restart"
    fi
fi

# 2. Vite dev server
if curl -sf -o /dev/null http://localhost:5173 2>/dev/null; then
    log "Vite: OK"
else
    log "Vite: DOWN — restarting..."
    lsof -ti:5173 | xargs kill -9 2>/dev/null
    sleep 1
    cd "$PROJECT_DIR/ui/web"
    nohup bash -c 'VITE_BACKEND_PORT=18790 pnpm dev' > /tmp/vite.log 2>&1 &
    sleep 3
    if curl -sf -o /dev/null http://localhost:5173 2>/dev/null; then
        log "Vite: restarted OK"
    else
        log "Vite: FAILED to restart"
    fi
fi

# 3. Cloudflared tunnel
if pgrep -f "cloudflared tunnel" > /dev/null 2>&1; then
    TUNNEL_URL=$(cat /tmp/goclaw-tunnel-url 2>/dev/null || echo "")
    if [ -n "$TUNNEL_URL" ] && curl -sf "$TUNNEL_URL/health" > /dev/null 2>&1; then
        log "Tunnel: OK ($TUNNEL_URL)"
    else
        log "Tunnel: process alive but URL unreachable — restarting..."
        pkill -f "cloudflared tunnel" 2>/dev/null
        sleep 2
        # Let tunnel-manager handle restart via launchd
        launchctl kickstart -k "gui/$(id -u)/com.goclaw.tunnel" 2>/dev/null || true
        log "Tunnel: kickstarted via launchd"
    fi
else
    log "Tunnel: process dead — launchd should auto-restart"
    launchctl kickstart -k "gui/$(id -u)/com.goclaw.tunnel" 2>/dev/null || true
fi

# 4. CF Pages
if curl -sf -o /dev/null https://nta-goclaw.pages.dev/health 2>/dev/null; then
    log "Pages: OK"
else
    log "Pages: DOWN (530/unreachable) — redeploying..."
    TUNNEL_URL=$(cat /tmp/goclaw-tunnel-url 2>/dev/null || echo "")
    if [ -n "$TUNNEL_URL" ]; then
        echo "$TUNNEL_URL" | CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" npx wrangler pages secret put BACKEND_URL --project-name nta-goclaw 2>&1 | tail -1 >> "$LOG"
        CLOUDFLARE_ACCOUNT_ID="$ACCOUNT_ID" npx wrangler pages deploy "$PROJECT_DIR/internal/webui/dist" --project-name nta-goclaw --commit-dirty=true 2>&1 | tail -1 >> "$LOG"
        sleep 5
        if curl -sf -o /dev/null https://nta-goclaw.pages.dev/health 2>/dev/null; then
            log "Pages: redeployed OK"
        else
            log "Pages: redeploy may need propagation time"
        fi
    else
        log "Pages: no tunnel URL, skipping redeploy"
    fi
fi

log "=== Watchdog done ==="
