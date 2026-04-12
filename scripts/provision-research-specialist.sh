#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_FILE="${GOCLAW_ENV_FILE:-}"

find_env_file() {
  if [[ -n "${ENV_FILE}" && -f "${ENV_FILE}" ]]; then
    printf '%s\n' "${ENV_FILE}"
    return 0
  fi
  if [[ -f ".env" ]]; then
    printf '%s\n' ".env"
    return 0
  fi
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    printf '%s\n' "${ROOT_DIR}/.env"
    return 0
  fi
  return 1
}

if env_path="$(find_env_file 2>/dev/null)"; then
  ENV_FILE="${env_path}"
fi

load_env_var() {
  local key="$1"
  if [[ -n "${!key:-}" ]]; then
    return 0
  fi
  if [[ -z "${ENV_FILE:-}" || ! -f "${ENV_FILE}" ]]; then
    return 0
  fi
  local value
  value="$(grep -E "^${key}=" "${ENV_FILE}" | head -n1 | cut -d= -f2- || true)"
  if [[ -n "${value}" ]]; then
    printf -v "${key}" '%s' "${value}"
    export "${key}"
  fi
}

load_env_var GOCLAW_GATEWAY_TOKEN
load_env_var TAVILY_API_KEY
load_env_var PERPLEXITY_API_KEY

BASE_URL="${GOCLAW_BASE_URL:-http://127.0.0.1:18790}"
USER_ID="${GOCLAW_USER_ID:-system}"
TOKEN="${GOCLAW_GATEWAY_TOKEN:-}"
TAVILY_KEY="${TAVILY_API_KEY:-}"
PERPLEXITY_KEY="${PERPLEXITY_API_KEY:-}"

AGENT_KEY="${GOCLAW_RESEARCH_AGENT_KEY:-researcher}"
AGENT_DISPLAY_NAME="${GOCLAW_RESEARCH_AGENT_DISPLAY_NAME:-🔬 Research Specialist}"
AGENT_WORKSPACE="${GOCLAW_RESEARCH_WORKSPACE:-/app/workspace/researcher}"
BASELINE_AGENT_KEY="${GOCLAW_RESEARCH_BASELINE_AGENT_KEY:-coder}"

TAVILY_SERVER_NAME="${GOCLAW_TAVILY_SERVER_NAME:-tavily-remote}"
TAVILY_SERVER_URL="${GOCLAW_TAVILY_MCP_URL:-https://mcp.tavily.com/mcp/}"
PERPLEXITY_SERVER_NAME="${GOCLAW_PERPLEXITY_SERVER_NAME:-perplexity-selfhost}"
PERPLEXITY_SERVER_COMMAND="${GOCLAW_PERPLEXITY_MCP_COMMAND:-perplexity-mcp}"

usage() {
  cat <<'EOF'
Usage:
  GOCLAW_GATEWAY_TOKEN=... TAVILY_API_KEY=... PERPLEXITY_API_KEY=... scripts/provision-research-specialist.sh

Environment:
  GOCLAW_BASE_URL                  GoClaw API base URL (default: http://127.0.0.1:18790)
  GOCLAW_USER_ID                   User header for provisioning (default: system)
  GOCLAW_GATEWAY_TOKEN             Bearer token for admin API access
  TAVILY_API_KEY                   Tavily API key for the remote MCP server
  PERPLEXITY_API_KEY               Perplexity API key for the stdio MCP server
  GOCLAW_RESEARCH_AGENT_KEY        Agent key to provision (default: researcher)
  GOCLAW_RESEARCH_AGENT_DISPLAY_NAME
                                   Agent display name (default: "🔬 Research Specialist")
  GOCLAW_RESEARCH_WORKSPACE        Agent workspace path
  GOCLAW_RESEARCH_BASELINE_AGENT_KEY
                                   Existing agent to copy provider/model/shell policy from (default: coder)
  GOCLAW_TAVILY_SERVER_NAME        MCP server slug for Tavily
  GOCLAW_TAVILY_MCP_URL            Tavily remote MCP URL
  GOCLAW_PERPLEXITY_SERVER_NAME    MCP server slug for self-hosted Perplexity
  GOCLAW_PERPLEXITY_MCP_COMMAND    Perplexity MCP command inside the GoClaw container
  GOCLAW_ENV_FILE                  Optional .env file to load GOCLAW_GATEWAY_TOKEN/TAVILY_API_KEY from

The script is idempotent:
  - creates or updates the Tavily + Perplexity MCP servers
  - creates or updates the research specialist agent
  - grants both MCP servers only to that agent
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ -z "${TOKEN}" ]]; then
  echo "GOCLAW_GATEWAY_TOKEN is required" >&2
  exit 1
fi
if [[ -z "${TAVILY_KEY}" ]]; then
  echo "TAVILY_API_KEY is required" >&2
  exit 1
fi
if [[ -z "${PERPLEXITY_KEY}" ]]; then
  echo "PERPLEXITY_API_KEY is required" >&2
  exit 1
fi

api_request() {
  local method="$1"
  local path="$2"
  local body="${3:-}"
  local args=(
    -fsS
    -X "${method}"
    -H "Authorization: Bearer ${TOKEN}"
    -H "X-GoClaw-User-Id: ${USER_ID}"
  )
  if [[ -n "${body}" ]]; then
    args+=(-H "Content-Type: application/json" --data "${body}")
  fi
  curl "${args[@]}" "${BASE_URL}${path}"
}

api_get() {
  api_request GET "$1"
}

api_post() {
  api_request POST "$1" "$2"
}

api_put() {
  api_request PUT "$1" "$2"
}

lookup_id_by_key() {
  local list_json="$1"
  local field_name="$2"
  local field_value="$3"
  python3 -c '
import json
import sys

field_name = sys.argv[1]
field_value = sys.argv[2]
data = json.load(sys.stdin)

items = []
for key in ("agents", "servers"):
    items.extend(data.get(key, []))

for item in items:
    if item.get(field_name) == field_value:
        print(item.get("id", ""))
        break
' "${field_name}" "${field_value}" <<<"${list_json}"
}

extract_baseline() {
  local agents_json="$1"
  local baseline_key="$2"
  python3 -c '
import json
import sys

baseline_key = sys.argv[1]
data = json.load(sys.stdin)

for agent in data.get("agents", []):
    if agent.get("agent_key") == baseline_key:
        print(agent.get("provider") or "")
        print(agent.get("model") or "")
        print(json.dumps(agent.get("shell_deny_groups") or {}, separators=(",", ":")))
        sys.exit(0)

raise SystemExit(f"baseline agent not found: {baseline_key}")
' "${baseline_key}" <<<"${agents_json}"
}

build_agent_payload() {
  local provider="$1"
  local model="$2"
  local shell_deny_json="$3"
  python3 - "${provider}" "${model}" "${shell_deny_json}" "${AGENT_KEY}" "${AGENT_DISPLAY_NAME}" "${AGENT_WORKSPACE}" <<'PY'
import json
import sys

provider, model, shell_deny_json, agent_key, display_name, workspace = sys.argv[1:7]
shell_deny = json.loads(shell_deny_json)

payload = {
    "agent_key": agent_key,
    "display_name": display_name,
    "frontmatter": (
        "Research specialist for competitor intelligence, market discovery, and "
        "citation-heavy synthesis. Uses Tavily for crawl/extract ground truth and "
        "Perplexity for deep web research, then delivers structured reports."
    ),
    "provider": provider,
    "model": model,
    "context_window": 200000,
    "max_tool_iterations": 30,
    "workspace": workspace,
    "agent_type": "predefined",
    "status": "active",
    "tools_config": {
        "profile": "research"
    },
    "memory_config": {
        "enabled": True
    },
    "compaction_config": {},
    "emoji": "🔬",
    "agent_description": (
        "Name: Research Specialist. A precise web research and competitor-intelligence analyst.\n\n"
        "Mission: Find current information quickly, separate evidence from inference, and turn messy web material into decision-ready output.\n\n"
        "Workflow: First use Tavily MCP to search, crawl, map, and extract ground-truth pages from competitor or market websites. Then use Perplexity MCP for broader synthesis, deep research, and citation-backed explanation. Use native browser/web tools only as fallback when MCP coverage is insufficient.\n\n"
        "Failure handling: If Perplexity or Tavily returns an auth, quota, billing, or upstream availability error, do not keep retrying the same failing tool call. State the tool limitation clearly, then continue with the best available fallback path when the task still makes sense.\n\n"
        "Output standard: Respond with a concise summary first, then clear sections for findings, pricing/features comparisons, risks or unknowns, and a source table. Every factual claim should be traceable to a cited source. If a detail is missing or uncertain, say so directly instead of filling gaps with guesses.\n\n"
        "File behavior: When the task is substantial or explicitly asks for a deliverable, write a Markdown report inside the workspace before returning the final summary.\n\n"
        "Interaction style: Do not ask for the user's name, location, or other personal details unless that information is directly required to complete the task."
    ),
    "reasoning_config": {
        "override_mode": "custom",
        "effort": "high",
        "fallback": "downgrade"
    },
    "self_evolve": False,
    "skill_evolve": False,
    "shell_deny_groups": shell_deny,
}

print(json.dumps(payload, separators=(",", ":")))
PY
}

build_http_server_payload() {
  local name="$1"
  local display_name="$2"
  local url="$3"
  local api_key="$4"
  local tool_prefix="$5"
  local timeout_sec="$6"
  python3 - "${name}" "${display_name}" "${url}" "${api_key}" "${tool_prefix}" "${timeout_sec}" <<'PY'
import json
import sys

name, display_name, url, api_key, tool_prefix, timeout_sec = sys.argv[1:7]
payload = {
    "name": name,
    "display_name": display_name,
    "transport": "streamable-http",
    "url": url,
    "api_key": api_key,
    "tool_prefix": tool_prefix,
    "timeout_sec": int(timeout_sec),
    "enabled": True,
    "settings": {
        "require_user_credentials": False
    }
}
print(json.dumps(payload, separators=(",", ":")))
PY
}

build_stdio_server_payload() {
  local name="$1"
  local display_name="$2"
  local command="$3"
  local api_key="$4"
  local tool_prefix="$5"
  local timeout_sec="$6"
  python3 - "${name}" "${display_name}" "${command}" "${api_key}" "${tool_prefix}" "${timeout_sec}" <<'PY'
import json
import sys

name, display_name, command, api_key, tool_prefix, timeout_sec = sys.argv[1:7]
payload = {
    "name": name,
    "display_name": display_name,
    "transport": "stdio",
    "command": command,
    "args": [],
    "env": {
        "PERPLEXITY_API_KEY": api_key
    },
    "url": "",
    "headers": {},
    "api_key": "",
    "tool_prefix": tool_prefix,
    "timeout_sec": int(timeout_sec),
    "enabled": True,
    "settings": {
        "require_user_credentials": False
    }
}
print(json.dumps(payload, separators=(",", ":")))
PY
}

extract_tool_allow_json() {
  local tools_json="$1"
  python3 -c '
import json
import sys

tools = [tool.get("name") for tool in json.load(sys.stdin).get("tools", []) if tool.get("name")]
if not tools:
    raise SystemExit("no tools discovered")
print(json.dumps(tools, separators=(",", ":")))
' <<<"${tools_json}"
}

format_tool_names() {
  local tools_json="$1"
  python3 -c '
import json
import sys

tools = [tool.get("name") for tool in json.load(sys.stdin).get("tools", []) if tool.get("name")]
print(", ".join(tools))
' <<<"${tools_json}"
}

wait_for_tools() {
  local server_id="$1"
  local label="$2"
  local attempts="${3:-20}"
  local last_response=""

  for _ in $(seq 1 "${attempts}"); do
    if last_response="$(api_get "/v1/mcp/servers/${server_id}/tools" 2>/dev/null || true)"; then
      if python3 -c '
import json
import sys

tools = json.load(sys.stdin).get("tools", [])
raise SystemExit(0 if tools else 1)
' >/dev/null 2>&1 <<<"${last_response}"
      then
        printf '%s' "${last_response}"
        return 0
      fi
    fi
    sleep 3
  done

  echo "failed to discover tools for ${label}" >&2
  if [[ -n "${last_response}" ]]; then
    echo "${last_response}" >&2
  fi
  return 1
}

grant_server_to_agent() {
  local server_id="$1"
  local agent_id="$2"
  local tool_allow_json="$3"
  local grant_payload
  grant_payload="$(python3 - "${agent_id}" "${tool_allow_json}" <<'PY'
import json
import sys

agent_id, tool_allow_json = sys.argv[1:3]
payload = {
    "agent_id": agent_id,
    "tool_allow": json.loads(tool_allow_json),
}
print(json.dumps(payload, separators=(",", ":")))
PY
)"
  api_post "/v1/mcp/servers/${server_id}/grants/agent" "${grant_payload}" >/dev/null
}

echo "Fetching existing agents and MCP servers..."
agents_json="$(api_get "/v1/agents")"
servers_json="$(api_get "/v1/mcp/servers")"

mapfile -t baseline < <(extract_baseline "${agents_json}" "${BASELINE_AGENT_KEY}")
if [[ "${#baseline[@]}" -lt 3 ]]; then
  echo "failed to resolve provider/model baseline from agent ${BASELINE_AGENT_KEY}" >&2
  exit 1
fi

provider="${GOCLAW_RESEARCH_PROVIDER:-${baseline[0]}}"
model="${GOCLAW_RESEARCH_MODEL:-${baseline[1]}}"
shell_deny_json="${baseline[2]}"

if [[ -z "${provider}" || -z "${model}" ]]; then
  echo "unable to resolve provider/model for research agent" >&2
  exit 1
fi

agent_payload="$(build_agent_payload "${provider}" "${model}" "${shell_deny_json}")"
agent_id="$(lookup_id_by_key "${agents_json}" "agent_key" "${AGENT_KEY}")"

if [[ -z "${agent_id}" ]]; then
  echo "Creating agent ${AGENT_KEY}..."
  create_response="$(api_post "/v1/agents" "${agent_payload}")"
  agent_id="$(python3 -c '
import json
import sys
print(json.load(sys.stdin)["id"])
' <<<"${create_response}")"
else
  echo "Updating agent ${AGENT_KEY} (${agent_id})..."
  api_put "/v1/agents/${agent_id}" "${agent_payload}" >/dev/null
fi

echo "Provisioning Tavily MCP server..."
tavily_payload="$(build_http_server_payload "${TAVILY_SERVER_NAME}" "Tavily Remote MCP" "${TAVILY_SERVER_URL}" "${TAVILY_KEY}" "tavily" "120")"
tavily_id="$(lookup_id_by_key "${servers_json}" "name" "${TAVILY_SERVER_NAME}")"
if [[ -z "${tavily_id}" ]]; then
  tavily_response="$(api_post "/v1/mcp/servers" "${tavily_payload}")"
  tavily_id="$(python3 -c '
import json
import sys
print(json.load(sys.stdin)["id"])
' <<<"${tavily_response}")"
else
  api_put "/v1/mcp/servers/${tavily_id}" "${tavily_payload}" >/dev/null
fi
api_post "/v1/mcp/servers/${tavily_id}/reconnect" '{}' >/dev/null || true

echo "Provisioning Perplexity MCP server..."
perplexity_payload="$(build_stdio_server_payload "${PERPLEXITY_SERVER_NAME}" "Perplexity Self-Hosted MCP" "${PERPLEXITY_SERVER_COMMAND}" "${PERPLEXITY_KEY}" "perplexity" "600")"
perplexity_id="$(lookup_id_by_key "${servers_json}" "name" "${PERPLEXITY_SERVER_NAME}")"
if [[ -z "${perplexity_id}" ]]; then
  perplexity_response="$(api_post "/v1/mcp/servers" "${perplexity_payload}")"
  perplexity_id="$(python3 -c '
import json
import sys
print(json.load(sys.stdin)["id"])
' <<<"${perplexity_response}")"
else
  api_put "/v1/mcp/servers/${perplexity_id}" "${perplexity_payload}" >/dev/null
fi
api_post "/v1/mcp/servers/${perplexity_id}/reconnect" '{}' >/dev/null || true

echo "Waiting for Tavily tool discovery..."
tavily_tools_json="$(wait_for_tools "${tavily_id}" "Tavily Remote MCP")"
tavily_allow_json="$(extract_tool_allow_json "${tavily_tools_json}")"
grant_server_to_agent "${tavily_id}" "${agent_id}" "${tavily_allow_json}"

echo "Waiting for Perplexity tool discovery..."
perplexity_tools_json="$(wait_for_tools "${perplexity_id}" "Perplexity Self-Hosted MCP")"
perplexity_allow_json="$(extract_tool_allow_json "${perplexity_tools_json}")"
grant_server_to_agent "${perplexity_id}" "${agent_id}" "${perplexity_allow_json}"

echo
echo "Research specialist provisioned successfully."
echo "Agent: ${AGENT_KEY} (${agent_id})"
echo "Provider: ${provider}"
echo "Model: ${model}"
echo "Tavily tools: $(format_tool_names "${tavily_tools_json}")"
echo "Perplexity tools: $(format_tool_names "${perplexity_tools_json}")"
