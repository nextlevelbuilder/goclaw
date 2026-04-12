#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROFILES_DIR="${ROOT_DIR}/scripts/agent-shell-profiles"

usage() {
  cat <<'EOF'
Usage:
  GOCLAW_GATEWAY_TOKEN=... scripts/apply-agent-shell-profile.sh <profile> <agent-id-or-key>

Environment:
  GOCLAW_BASE_URL   Base URL for GoClaw API (default: http://127.0.0.1:18790)
  GOCLAW_USER_ID    User header value (default: system)
  GOCLAW_GATEWAY_TOKEN  Required bearer token for admin update

Example:
  GOCLAW_GATEWAY_TOKEN=... scripts/apply-agent-shell-profile.sh trusted_dev coder
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ $# -ne 2 ]]; then
  usage >&2
  exit 1
fi

PROFILE_NAME="$1"
AGENT_REF="$2"
BASE_URL="${GOCLAW_BASE_URL:-http://127.0.0.1:18790}"
USER_ID="${GOCLAW_USER_ID:-system}"
TOKEN="${GOCLAW_GATEWAY_TOKEN:-}"
PROFILE_PATH="${PROFILES_DIR}/${PROFILE_NAME}.json"

if [[ -z "${TOKEN}" ]]; then
  echo "GOCLAW_GATEWAY_TOKEN is required" >&2
  exit 1
fi

if [[ ! -f "${PROFILE_PATH}" ]]; then
  echo "Profile not found: ${PROFILE_PATH}" >&2
  exit 1
fi

api_get() {
  local path="$1"
  curl -fsS \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "X-GoClaw-User-Id: ${USER_ID}" \
    "${BASE_URL}${path}"
}

api_put() {
  local path="$1"
  local body="$2"
  curl -fsS \
    -X PUT \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "X-GoClaw-User-Id: ${USER_ID}" \
    -H "Content-Type: application/json" \
    --data "${body}" \
    "${BASE_URL}${path}"
}

AGENT_ID="$(
  api_get "/v1/agents" | python3 -c '
import json
import sys

agent_ref = sys.argv[1]
data = json.load(sys.stdin)
for agent in data.get("agents", []):
    if agent.get("id") == agent_ref or agent.get("agent_key") == agent_ref:
        print(agent["id"])
        break
else:
    raise SystemExit(1)
' "${AGENT_REF}"
)" || {
  echo "Agent not found: ${AGENT_REF}" >&2
  exit 1
}

PROFILE_JSON="$(tr -d '\n' < "${PROFILE_PATH}")"
REQUEST_BODY="{\"shell_deny_groups\":${PROFILE_JSON}}"

api_put "/v1/agents/${AGENT_ID}" "${REQUEST_BODY}" >/dev/null

api_get "/v1/agents/${AGENT_ID}" | python3 -c '
import json
import sys

profile_name = sys.argv[1]
data = json.load(sys.stdin)
groups = data.get("shell_deny_groups") or {}
agent_key = data.get("agent_key")
agent_id = data.get("id")

print(f"Applied profile: {profile_name}")
print(f"Agent: {agent_key} ({agent_id})")
for name in sorted(groups):
    state = "deny" if groups[name] else "allow"
    print(f"  {name}: {state}")
' "${PROFILE_NAME}"
