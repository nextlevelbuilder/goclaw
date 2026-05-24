#!/usr/bin/env bash
set -Eeuo pipefail

REPO="digitopvn/goclaw"
BASE_DIR="/opt/goclaw"
RELEASES_DIR="${BASE_DIR}/releases"
DEPLOY_BIN="/usr/local/bin/goclaw-deploy"
STATUS_DIR="/var/lib/goclaw/update-jobs"
STATUS_FILE="${STATUS_DIR}/current.json"
STATUS_OWNER="${GOCLAW_STATUS_OWNER:-goclaw:goclaw}"
DRY_RUN=0
DETACHED="${GOCLAW_UPGRADE_DETACHED:-0}"

log() { printf '[%s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"; }
json_escape() { python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$1"; }

write_status() {
  local state="$1" requested="$2" resolved="$3" error_msg="${4:-}"
  mkdir -p "$STATUS_DIR"
  local now job target before tmp
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  job="$(date -u +%Y%m%dT%H%M%SZ)-${resolved:-$requested}"
  target="${RELEASES_DIR}/${resolved:-$requested}"
  before=""
  if [ -L "${BASE_DIR}/current" ]; then
    before="$(readlink -f "${BASE_DIR}/current" || true)"
  fi
  local finished_json
  if [ "$state" = "running" ]; then
    finished_json="null"
  else
    finished_json="$(json_escape "$now")"
  fi
  tmp="$(mktemp "${STATUS_DIR}/current.XXXXXX")"
  cat > "$tmp" <<JSON
{
  "jobId": $(json_escape "$job"),
  "state": $(json_escape "$state"),
  "requestedTag": $(json_escape "$requested"),
  "resolvedTag": $(json_escape "$resolved"),
  "startedAt": $(json_escape "$now"),
  "finishedAt": ${finished_json},
  "currentReleaseBefore": $(json_escape "$before"),
  "targetRelease": $(json_escape "$target"),
  "error": $(json_escape "$error_msg")
}
JSON
  chown "$STATUS_OWNER" "$tmp" 2>/dev/null || true
  chmod 0640 "$tmp"
  mv "$tmp" "$STATUS_FILE"
}

fail() {
  local msg="$1"
  log "ERROR: $msg"
  if [ "$DRY_RUN" != "1" ]; then
    write_status "failed" "${REQUESTED_TAG:-}" "${RESOLVED_TAG:-}" "$msg" || true
  fi
  exit 1
}

usage() {
  cat <<'EOF'
usage: goclaw-upgrade-release [--dry-run] <latest|vMAJOR.MINOR.PATCH[-beta.N|-rc.N]>
EOF
}

if [ "${1:-}" = "--dry-run" ]; then
  DRY_RUN=1
  shift
fi

REQUESTED_TAG="${1:-}"
if [ -z "$REQUESTED_TAG" ] || [ "${2:-}" != "" ]; then
  usage >&2
  exit 2
fi

if [ "$REQUESTED_TAG" != "latest" ] && ! [[ "$REQUESTED_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(beta|rc)\.[0-9]+)?$ ]]; then
  fail "invalid tag"
fi

require_bin() { command -v "$1" >/dev/null 2>&1 || fail "missing dependency: $1"; }
require_bin curl
require_bin tar
require_bin sha256sum
require_bin python3

detach_into_systemd() {
  if [ "$DRY_RUN" = "1" ] || [ "$DETACHED" = "1" ]; then
    return
  fi
  if ! command -v systemd-run >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    return
  fi
  if ! grep -q 'goclaw.service' "/proc/$$/cgroup" 2>/dev/null; then
    return
  fi

  local script_path unit_tag unit_name
  script_path="$(readlink -f "$0" 2>/dev/null || true)"
  if [ -z "$script_path" ]; then
    script_path="$0"
  fi
  unit_tag="$(printf '%s' "$REQUESTED_TAG" | tr -c 'A-Za-z0-9_.-' '-')"
  unit_name="goclaw-upgrade-${unit_tag}-$(date -u +%Y%m%dT%H%M%SZ)"
  log "starting detached systemd upgrade unit=${unit_name}"
  systemd-run \
    --unit="$unit_name" \
    --collect \
    --property=Type=oneshot \
    --setenv=GOCLAW_UPGRADE_DETACHED=1 \
    "$script_path" "$REQUESTED_TAG"
  exit 0
}

detach_into_systemd

if [ "$DRY_RUN" != "1" ]; then
  require_bin flock
  mkdir -p "$STATUS_DIR"
  exec 9>"${STATUS_DIR}/upgrade.lock"
  flock -n 9 || fail "gateway upgrade already running"
fi

RESOLVED_TAG="$REQUESTED_TAG"
if [ "$REQUESTED_TAG" = "latest" ]; then
  log "resolving latest stable server release"
  RESOLVED_TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=50" | python3 -c '
import json, re, sys
for rel in json.load(sys.stdin):
    tag = rel.get("tag_name", "")
    if rel.get("draft") or rel.get("prerelease"):
        continue
    if re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        print(tag)
        raise SystemExit(0)
raise SystemExit("no stable server release found")
')"
fi

if ! [[ "$RESOLVED_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-(beta|rc)\.[0-9]+)?$ ]]; then
  fail "resolved invalid tag: $RESOLVED_TAG"
fi

VERSION="${RESOLVED_TAG#v}"
ASSET=""
ASSET_URL=""
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${RESOLVED_TAG}/CHECKSUMS.sha256"
TARGET_DIR="${RELEASES_DIR}/${RESOLVED_TAG}"

download_release_asset() {
  local candidate url
  for candidate in "goclaw-${VERSION}-linux-amd64.tar.gz" "goclaw-${RESOLVED_TAG}-linux-amd64.tar.gz"; do
    url="https://github.com/${REPO}/releases/download/${RESOLVED_TAG}/${candidate}"
    log "downloading release asset candidate=${candidate}"
    if curl -fsSL -o "$candidate" "$url"; then
      ASSET="$candidate"
      ASSET_URL="$url"
      return 0
    fi
    rm -f "$candidate"
  done
  fail "linux amd64 release asset not found for ${RESOLVED_TAG}"
}

github_release_asset_digest() {
  local asset_name="$1"
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/tags/${RESOLVED_TAG}" | python3 -c '
import json
import sys

name = sys.argv[1]
release = json.load(sys.stdin)
for asset in release.get("assets", []):
    if asset.get("name") == name:
        digest = asset.get("digest", "")
        if digest.startswith("sha256:"):
            print(digest.split(":", 1)[1])
            raise SystemExit(0)
raise SystemExit(1)
' "$asset_name"
}

verify_release_asset() {
  if curl -fsSLO "$CHECKSUM_URL"; then
    if grep " ${ASSET}$\|${ASSET}$" CHECKSUMS.sha256 | sha256sum -c -; then
      log "checksum verified via CHECKSUMS.sha256"
      return 0
    fi
    log "checksum file did not verify ${ASSET}; falling back to release asset digest"
  else
    log "CHECKSUMS.sha256 unavailable; falling back to release asset digest"
  fi

  local expected actual
  if ! expected="$(github_release_asset_digest "$ASSET")"; then
    fail "missing sha256 digest for ${ASSET}"
  fi
  read -r actual _ < <(sha256sum "$ASSET")
  if [ "$actual" != "$expected" ]; then
    fail "release asset digest verification failed"
  fi
  log "checksum verified via GitHub release asset digest"
}

log "requested=${REQUESTED_TAG} resolved=${RESOLVED_TAG}"

target_release_is_active() {
  [ -d "$TARGET_DIR" ] || return 1
  [ -L "${BASE_DIR}/current" ] || return 1
  [ "$(readlink -f "${BASE_DIR}/current")" = "$(readlink -f "$TARGET_DIR")" ]
}

if [ "$DRY_RUN" = "1" ]; then
  TMP_DIR="$(mktemp -d)"
  cleanup() { rm -rf "$TMP_DIR"; }
  trap cleanup EXIT
  cd "$TMP_DIR"
  download_release_asset
  verify_release_asset
  log "dry-run ok"
  exit 0
fi

if target_release_is_active; then
  if [ ! -x "$TARGET_DIR/goclaw" ] || [ ! -d "$TARGET_DIR/migrations" ]; then
    fail "active release is missing goclaw binary or migrations directory"
  fi
  log "target release already active: ${RESOLVED_TAG}"
  write_status "succeeded" "$REQUESTED_TAG" "$RESOLVED_TAG" ""
  exit 0
fi

write_status "running" "$REQUESTED_TAG" "$RESOLVED_TAG" ""

TMP_DIR="$(mktemp -d)"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

cd "$TMP_DIR"
download_release_asset
verify_release_asset

if [ -e "$TARGET_DIR" ]; then
  fail "target release already exists: $TARGET_DIR"
fi
mkdir -p "$TARGET_DIR"
tar -xzf "$ASSET" -C "$TARGET_DIR"
chmod +x "$TARGET_DIR/goclaw"

if [ ! -x "$TARGET_DIR/goclaw" ] || [ ! -d "$TARGET_DIR/migrations" ]; then
  fail "release archive missing goclaw binary or migrations directory"
fi

log "deploying ${RESOLVED_TAG}"
"$DEPLOY_BIN" "$TARGET_DIR" || fail "deploy failed"
write_status "succeeded" "$REQUESTED_TAG" "$RESOLVED_TAG" ""
log "upgrade complete: ${RESOLVED_TAG}"
