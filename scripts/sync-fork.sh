#!/bin/bash
# Sync all branches from origin (nextlevelbuilder/goclaw) to fork (theanhbk081-max/goclaw)
# Runs via launchd twice daily

set -euo pipefail

REPO_DIR="/Users/nta7/nta-project-mac-mini/nta-goclaw"
LOG_FILE="/tmp/goclaw-sync-fork.log"
LOCK_FILE="/tmp/goclaw-sync-fork.lock"

# Telegram notification
TG_BOT_TOKEN="8619158131:AAFQVkLGe-cDTNimj-WAaEOSTTL-uyWu6GM"
TG_CHAT_ID="5894479966"

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*" >> "$LOG_FILE"; }

# Collect sync results for notification
SYNC_RESULTS=""
add_result() { SYNC_RESULTS="${SYNC_RESULTS}${1}\n"; }

notify_telegram() {
    local status="$1"
    local icon="✅"
    [ "$status" = "error" ] && icon="❌"
    local msg="${icon} <b>GoClaw Fork Sync</b>\n$(date '+%Y-%m-%d %H:%M')\n\n${SYNC_RESULTS}"
    python3 -c "
import urllib.request, json, ssl
ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
data = json.dumps({'chat_id': ${TG_CHAT_ID}, 'parse_mode': 'HTML', 'text': '''${msg}'''}).encode()
req = urllib.request.Request('https://api.telegram.org/bot${TG_BOT_TOKEN}/sendMessage', data=data, headers={'Content-Type': 'application/json'})
urllib.request.urlopen(req, context=ctx)
" 2>> "$LOG_FILE" || log "WARNING: Failed to send Telegram notification"
}

# Prevent concurrent runs
if [ -f "$LOCK_FILE" ]; then
    pid=$(cat "$LOCK_FILE" 2>/dev/null)
    if kill -0 "$pid" 2>/dev/null; then
        log "SKIP: Another sync is running (PID $pid)"
        exit 0
    fi
    rm -f "$LOCK_FILE"
fi
echo $$ > "$LOCK_FILE"
trap 'rm -f "$LOCK_FILE"' EXIT

log "=== Sync started ==="

cd "$REPO_DIR"

# Fetch latest from origin
log "Fetching origin..."
if ! git fetch origin --prune 2>> "$LOG_FILE"; then
    log "ERROR: Failed to fetch origin"
    exit 1
fi

# Sync main branches: main, dev
for branch in main dev; do
    if git rev-parse --verify "origin/$branch" >/dev/null 2>&1; then
        local_ref=$(git rev-parse "$branch" 2>/dev/null || echo "none")
        remote_ref=$(git rev-parse "origin/$branch")

        if [ "$local_ref" = "$remote_ref" ]; then
            log "$branch: already up-to-date"
            add_result "• <code>$branch</code>: up-to-date"
        else
            # Update local branch without checkout (safe for current branch)
            current=$(git symbolic-ref --short HEAD 2>/dev/null || echo "detached")
            if [ "$current" = "$branch" ]; then
                # We're on this branch — pull with ff-only
                if git merge --ff-only "origin/$branch" 2>> "$LOG_FILE"; then
                    log "$branch: fast-forwarded to $(git rev-parse --short HEAD)"
                    add_result "• <code>$branch</code>: fast-forwarded → $(git rev-parse --short HEAD)"
                else
                    log "$branch: SKIP merge (not fast-forward, needs manual resolve)"
                    add_result "• <code>$branch</code>: ⚠️ diverged, needs manual merge"
                    continue
                fi
            else
                # Not on this branch — update ref directly
                git update-ref "refs/heads/$branch" "$remote_ref" 2>> "$LOG_FILE"
                log "$branch: updated to $(git rev-parse --short $remote_ref)"
                add_result "• <code>$branch</code>: updated → $(git rev-parse --short $remote_ref)"
            fi
        fi

        # Push to fork
        if git push fork "$branch" 2>> "$LOG_FILE"; then
            log "$branch: pushed to fork"
        else
            log "$branch: ERROR pushing to fork"
            add_result "• <code>$branch</code>: ❌ push failed"
        fi
    else
        log "$branch: not found on origin, skipping"
    fi
done

# Push tags
log "Syncing tags..."
git fetch origin --tags 2>> "$LOG_FILE"
if git push fork --tags 2>> "$LOG_FILE"; then
    log "Tags: synced to fork"
    add_result "• Tags: synced"
else
    log "Tags: ERROR pushing to fork"
    add_result "• Tags: ❌ push failed"
fi

log "=== Sync completed ==="

# Send Telegram notification
notify_telegram "ok"
