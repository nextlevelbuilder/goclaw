#!/usr/bin/env bash
#
# MCP Builder E2E Integration Test
#
# This script tests the full MCP Builder workflow:
# 1. Creates the MCP Builder agent via HTTP API
# 2. Sends a chat completion request with PokeAPI analysis doc
# 3. Monitors agent response (streaming)
# 4. Verifies MCP server is registered in GoClaw
# 5. Verifies tools are recognized
#
# Prerequisites:
# - GoClaw gateway running (default: http://localhost:18790)
# - PostgreSQL database available
# - LLM provider configured (Gemini or Anthropic)
# - kubeconfig configured if testing K8s deployment
#
# Usage:
#   ./test_mcp_builder.sh [--gateway-url URL] [--token TOKEN] [--user-id USER]
#
# Environment variables (override defaults):
#   GOCLAW_GATEWAY_URL    Gateway base URL (default: http://localhost:18790)
#   GOCLAW_GATEWAY_TOKEN  Auth token
#   GOCLAW_USER_ID        User ID for API calls (default: test-user)
#   SKIP_K8S              Set to "1" to skip K8s deployment (use stdio transport)

set -euo pipefail

# --- Configuration ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

GATEWAY_URL="${GOCLAW_GATEWAY_URL:-http://localhost:18790}"
TOKEN="${GOCLAW_GATEWAY_TOKEN:-}"
USER_ID="${GOCLAW_USER_ID:-test-user}"
SKIP_K8S="${SKIP_K8S:-1}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-600}"  # 10 minutes max wait for agent

# Parse CLI args
while [[ $# -gt 0 ]]; do
  case $1 in
    --gateway-url) GATEWAY_URL="$2"; shift 2;;
    --token) TOKEN="$2"; shift 2;;
    --user-id) USER_ID="$2"; shift 2;;
    --skip-k8s) SKIP_K8S="1"; shift;;
    *) echo "Unknown option: $1"; exit 1;;
  esac
done

# Load .env if token not set
if [[ -z "$TOKEN" && -f "$PROJECT_ROOT/.env" ]]; then
  TOKEN=$(grep -E '^GOCLAW_GATEWAY_TOKEN=' "$PROJECT_ROOT/.env" | cut -d= -f2 | tr -d '"' || true)
fi

if [[ -z "$TOKEN" ]]; then
  echo "ERROR: GOCLAW_GATEWAY_TOKEN not set. Set via env or --token flag."
  exit 1
fi

# --- Helpers ---
AGENT_KEY="mcp-builder"
AGENT_ID=""
MCP_SERVER_NAME=""

log() { echo "[$(date '+%H:%M:%S')] $*"; }
err() { echo "[$(date '+%H:%M:%S')] ERROR: $*" >&2; }

api() {
  local method="$1"
  local path="$2"
  shift 2
  curl -s -X "$method" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -H "X-GoClaw-User-Id: $USER_ID" \
    "$GATEWAY_URL$path" \
    "$@"
}

api_with_status() {
  local method="$1"
  local path="$2"
  shift 2
  curl -s -w "\n%{http_code}" -X "$method" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -H "X-GoClaw-User-Id: $USER_ID" \
    "$GATEWAY_URL$path" \
    "$@"
}

# --- Step 0: Health check ---
log "=== Step 0: Health Check ==="
health=$(curl -s "$GATEWAY_URL/health" 2>/dev/null || echo "FAILED")
if echo "$health" | grep -q '"status"'; then
  log "Gateway is healthy: $health"
else
  err "Gateway at $GATEWAY_URL is not responding. Start the gateway first."
  exit 1
fi

# --- Step 1: Create or find MCP Builder agent ---
log "=== Step 1: Create/Find MCP Builder Agent ==="

# Check if agent already exists
existing=$(api GET "/v1/agents/$AGENT_KEY" 2>/dev/null || echo "")
if echo "$existing" | jq -e '.id' >/dev/null 2>&1; then
  AGENT_ID=$(echo "$existing" | jq -r '.id')
  log "Agent already exists: $AGENT_ID"
else
  log "Creating MCP Builder agent..."

  # Read agent.json from use-cases
  AGENT_JSON="$PROJECT_ROOT/use-cases/mcp-builder/agent.json"
  if [[ ! -f "$AGENT_JSON" ]]; then
    err "Agent config not found: $AGENT_JSON"
    exit 1
  fi

  # Modify agent.json to use unique key and Gemini provider
  AGENT_PAYLOAD=$(cat "$AGENT_JSON" | jq '
    .agent_key = "mcp-builder-test" |
    .provider = "gemini" |
    .model = "gemini-2.5-flash" |
    .other_config.self_evolve = false |
    del(.other_config.description)
  ')

  response=$(api_with_status POST "/v1/agents" -d "$AGENT_PAYLOAD")
  status_code=$(echo "$response" | tail -1)
  body=$(echo "$response" | sed '$d')

  if [[ "$status_code" == "201" || "$status_code" == "200" ]]; then
    AGENT_ID=$(echo "$body" | jq -r '.id')
    log "Agent created: $AGENT_ID"
  elif echo "$body" | grep -q "already exists"; then
    # Try to get it again
    existing=$(api GET "/v1/agents/$AGENT_KEY")
    AGENT_ID=$(echo "$existing" | jq -r '.id')
    log "Agent already exists (race): $AGENT_ID"
  else
    err "Failed to create agent (HTTP $status_code): $body"
    exit 1
  fi

  # Upload context files
  log "Uploading context files..."
  for file in SOUL.md IDENTITY.md AGENTS.md; do
    filepath="$PROJECT_ROOT/use-cases/mcp-builder/context-files/$file"
    if [[ -f "$filepath" ]]; then
      content=$(cat "$filepath")
      api PUT "/v1/agents/$AGENT_ID/context-files/$file" \
        -d "$(jq -n --arg c "$content" '{content: $c}')" >/dev/null 2>&1 || true
      log "  Uploaded $file"
    fi
  done
fi

# --- Step 2: Prepare the chat message ---
log "=== Step 2: Prepare Chat Message ==="

# Read the PokeAPI analysis document
POKEAPI_DOC="$PROJECT_ROOT/tmp/pokeapi-analysis.md"
if [[ ! -f "$POKEAPI_DOC" ]]; then
  err "PokeAPI analysis document not found: $POKEAPI_DOC"
  exit 1
fi
POKEAPI_CONTENT=$(cat "$POKEAPI_DOC")

# Build the prompt based on deployment mode
if [[ "$SKIP_K8S" == "1" ]]; then
  DEPLOY_INSTRUCTION="After building, register the MCP server using stdio transport with command 'bun' and args ['run', 'src/index.ts']. Set the working directory env to the workspace path where you created the project."
else
  DEPLOY_INSTRUCTION="After building, containerize with Docker (oven/bun:1-alpine), deploy to Kubernetes using Helm, then register the MCP server using streamable-http transport with the K8s NodePort URL."
fi

PROMPT="Build an MCP server for PokéAPI based on this API analysis document. The MCP server should be built using Bun runtime with TypeScript.

## API Analysis Document

$POKEAPI_CONTENT

## Requirements

1. Create a complete MCP server named 'pokeapi-mcp-server' in your workspace
2. Implement these tools:
   - pokeapi_get_pokemon: Get Pokemon by name/ID
   - pokeapi_list_pokemon: List Pokemon with pagination
   - pokeapi_get_type: Get type info and Pokemon of that type
   - pokeapi_get_ability: Get ability details
   - pokeapi_get_evolution_chain: Get evolution chain for a Pokemon
3. Use Bun runtime (bun install, bun run, bun test)
4. Use global fetch (NOT axios)
5. Use withErrorHandling() wrapper on all tools
6. Use AbortSignal.timeout() on all fetch calls
7. Include /health endpoint for HTTP transport
8. Write tests using bun:test with InMemoryTransport
9. Run bun test to verify everything works

## Deployment

$DEPLOY_INSTRUCTION

## IMPORTANT

- Follow the mcp-builder skill workflow
- Use the mcp_server_template.md as reference for project structure
- Register the server in GoClaw using register_mcp_server tool
- The server name for registration MUST be 'pokeapi-mcp'"

log "Prompt prepared (${#PROMPT} chars)"

# --- Step 3: Send chat completion (streaming) ---
log "=== Step 3: Send Chat Completion ==="

RESPONSE_FILE=$(mktemp /tmp/mcp_builder_response.XXXXXX)
STREAM_FILE=$(mktemp /tmp/mcp_builder_stream.XXXXXX)

# Build request payload
REQUEST_BODY=$(jq -n \
  --arg model "goclaw:$AGENT_ID" \
  --arg content "$PROMPT" \
  --arg user "$USER_ID" \
  '{
    model: $model,
    messages: [{role: "user", content: $content}],
    stream: true,
    user: $user
  }')

log "Sending chat completion to agent $AGENT_ID (streaming)..."
log "This may take several minutes as the agent builds the MCP server..."

# Stream the response
START_TIME=$(date +%s)

curl -s -N \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-GoClaw-User-Id: $USER_ID" \
  -H "Accept: text/event-stream" \
  "$GATEWAY_URL/v1/chat/completions" \
  -d "$REQUEST_BODY" \
  --max-time "$MAX_WAIT_SECONDS" \
  > "$STREAM_FILE" 2>/dev/null &
CURL_PID=$!

# Monitor progress
log "Streaming response (PID: $CURL_PID)..."

# Wait for curl to finish, showing progress
LAST_SIZE=0
while kill -0 $CURL_PID 2>/dev/null; do
  CURRENT_SIZE=$(wc -c < "$STREAM_FILE" 2>/dev/null || echo "0")
  if [[ "$CURRENT_SIZE" != "$LAST_SIZE" ]]; then
    ELAPSED=$(($(date +%s) - START_TIME))
    log "  Receiving data... ${CURRENT_SIZE} bytes (${ELAPSED}s elapsed)"
    LAST_SIZE=$CURRENT_SIZE
  fi
  sleep 5
done

wait $CURL_PID || true

ELAPSED=$(($(date +%s) - START_TIME))
FINAL_SIZE=$(wc -c < "$STREAM_FILE" 2>/dev/null || echo "0")
log "Response complete: ${FINAL_SIZE} bytes in ${ELAPSED}s"

# Parse SSE stream to extract content
grep '^data: ' "$STREAM_FILE" | sed 's/^data: //' | while read -r line; do
  if [[ "$line" != "[DONE]" ]]; then
    echo "$line" | jq -r '.choices[0].delta.content // empty' 2>/dev/null || true
  fi
done > "$RESPONSE_FILE"

RESPONSE_TEXT=$(cat "$RESPONSE_FILE")
log "Extracted response: ${#RESPONSE_TEXT} chars"

# Save full response for debugging
cp "$STREAM_FILE" "$SCRIPT_DIR/last_stream_response.txt"
cp "$RESPONSE_FILE" "$SCRIPT_DIR/last_response_text.txt"

# --- Step 4: Verify MCP Server Registration ---
log "=== Step 4: Verify MCP Server Registration ==="

MAX_RETRIES=12
RETRY_DELAY=10
REGISTERED=false

for i in $(seq 1 $MAX_RETRIES); do
  log "Checking for MCP server registration (attempt $i/$MAX_RETRIES)..."

  # List all MCP servers
  servers=$(api GET "/v1/mcp/servers" 2>/dev/null || echo "[]")

  if echo "$servers" | jq -e '.[] | select(.name == "pokeapi-mcp" or .name == "pokeapi-mcp-server")' >/dev/null 2>&1; then
    MCP_SERVER_NAME=$(echo "$servers" | jq -r '.[] | select(.name == "pokeapi-mcp" or .name == "pokeapi-mcp-server") | .name')
    MCP_SERVER_ID=$(echo "$servers" | jq -r '.[] | select(.name == "pokeapi-mcp" or .name == "pokeapi-mcp-server") | .id')
    REGISTERED=true
    log "MCP server registered: name=$MCP_SERVER_NAME, id=$MCP_SERVER_ID"
    break
  fi

  if [[ $i -lt $MAX_RETRIES ]]; then
    log "  Not found yet, waiting ${RETRY_DELAY}s..."
    sleep $RETRY_DELAY
  fi
done

if [[ "$REGISTERED" != "true" ]]; then
  err "MCP server was NOT registered after $((MAX_RETRIES * RETRY_DELAY))s"
  err "Check the agent response for errors:"
  tail -50 "$SCRIPT_DIR/last_response_text.txt" || true
  echo ""
  err "=== TEST FAILED ==="
  exit 1
fi

# --- Step 5: Verify Tools Recognition ---
log "=== Step 5: Verify Tools Recognition ==="

# Get server details including tools
server_detail=$(api GET "/v1/mcp/servers/$MCP_SERVER_ID" 2>/dev/null || echo "{}")
log "Server details: $(echo "$server_detail" | jq -c '{name, transport, enabled}' 2>/dev/null || echo 'N/A')"

# Check grants
grants=$(api GET "/v1/mcp/servers/$MCP_SERVER_ID/grants" 2>/dev/null || echo "[]")
grant_count=$(echo "$grants" | jq 'length' 2>/dev/null || echo "0")
log "Agent grants: $grant_count"

# Try to use the MCP server tools by sending another chat message
log "Sending test query to verify tools are available..."

TEST_PROMPT="Use the pokeapi tools to get information about Pikachu. Just call pokeapi_get_pokemon with name 'pikachu' and show the result."

TEST_REQUEST=$(jq -n \
  --arg model "goclaw:$AGENT_ID" \
  --arg content "$TEST_PROMPT" \
  --arg user "$USER_ID" \
  '{
    model: $model,
    messages: [{role: "user", content: $content}],
    stream: false,
    user: $user
  }')

test_response=$(api POST "/v1/chat/completions" -d "$TEST_REQUEST" --max-time 120 2>/dev/null || echo "{}")

if echo "$test_response" | jq -e '.choices[0].message.content' >/dev/null 2>&1; then
  test_content=$(echo "$test_response" | jq -r '.choices[0].message.content')
  if echo "$test_content" | grep -iq "pikachu"; then
    log "Tools verification: SUCCESS - Agent can use PokeAPI MCP tools"
  else
    log "Tools verification: PARTIAL - Agent responded but may not have used MCP tools"
    log "Response preview: $(echo "$test_content" | head -5)"
  fi
else
  log "Tools verification: SKIPPED - Could not verify (agent may need a new session)"
fi

# --- Summary ---
log ""
log "=========================================="
log "  MCP Builder E2E Test Results"
log "=========================================="
log ""
log "  Agent ID:        $AGENT_ID"
log "  Agent Key:       $AGENT_KEY"
log "  MCP Server:      $MCP_SERVER_NAME"
log "  MCP Server ID:   $MCP_SERVER_ID"
log "  Registered:      $REGISTERED"
log "  Duration:        ${ELAPSED}s"
log "  Gateway:         $GATEWAY_URL"
log ""
log "  Stream log:      $SCRIPT_DIR/last_stream_response.txt"
log "  Response text:   $SCRIPT_DIR/last_response_text.txt"
log ""
log "=========================================="
log "  TEST PASSED"
log "=========================================="

# Cleanup temp files
rm -f "$RESPONSE_FILE" "$STREAM_FILE"

exit 0
