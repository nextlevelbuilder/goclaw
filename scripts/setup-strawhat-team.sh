#!/usr/bin/env bash
# setup-strawhat-team.sh — One-shot bootstrap for the Straw Hat Pirates agent team
#
# Creates 5 predefined agents, seeds IDENTITY.md, creates team, adds members,
# activates agents with provider/model, and verifies worker endpoints.
#
# Usage (from VPS host, same machine as docker):
#   cd ~/services/goclaw
#   bash scripts/setup-strawhat-team.sh
#
# Usage (remote — requires direct API access):
#   export GOCLAW_URL="https://goclaw.example.com"
#   export GOCLAW_TOKEN="your-gateway-token"
#   bash scripts/setup-strawhat-team.sh
#
# Flags:
#   --delete     Tear down agents + team
#   --verify     Only verify existing setup (no changes)
#
# Prerequisites:
#   - GoClaw running with at least 1 LLM provider configured
#   - curl and jq installed
#   - For team creation: Docker access to postgres container OR DATABASE_URL

set -euo pipefail

# ── Auto-detect local Docker setup ──────────────────────────────
if [[ -z "${GOCLAW_URL:-}" ]] && [[ -f .env ]]; then
    # Running on VPS host — detect token from .env, use container network
    export GOCLAW_TOKEN="${GOCLAW_TOKEN:-$(grep '^GOCLAW_GATEWAY_TOKEN=' .env | cut -d= -f2)}"
    DOCKER_MODE=true
    CONTAINER="${GOCLAW_CONTAINER:-goclaw-goclaw-1}"
    PG_CONTAINER="${PG_CONTAINER:-postgres-postgres-1}"
    PG_USER="${POSTGRES_USER:-goclaw}"
    PG_DB="${POSTGRES_DB:-goclaw}"
    # Source .env for PG credentials
    source .env 2>/dev/null || true
else
    DOCKER_MODE=false
fi

GOCLAW_URL="${GOCLAW_URL:-}"
GOCLAW_TOKEN="${GOCLAW_TOKEN:?Set GOCLAW_TOKEN or run from VPS with .env file}"

TEAM_NAME="Straw Hat Pirates"
TENANT_ID="0193a5b0-7000-7000-8000-000000000001"
AGENT_KEYS=(luffy zoro sanji nami chopper)

# ── Colors ──────────────────────────────────────────────────────
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

info()  { echo -e "${GREEN}[✓]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
error() { echo -e "${RED}[✗]${NC} $*"; }
step()  { echo -e "${CYAN}[→]${NC} $*"; }
header(){ echo -e "\n${BOLD}$*${NC}"; }

# ── Dependency check ────────────────────────────────────────────
for cmd in curl jq; do
    command -v "$cmd" >/dev/null 2>&1 || { error "$cmd is required but not installed"; exit 1; }
done

# ── API helpers ─────────────────────────────────────────────────
# Calls GoClaw HTTP API. In Docker mode, uses wget inside container.
# In remote mode, uses curl directly.
api() {
    local method=$1 path=$2
    shift 2

    if $DOCKER_MODE; then
        local url="http://localhost:18790${path}"
        local wget_args="--header=Authorization: Bearer $GOCLAW_TOKEN"
        wget_args="$wget_args --header=Content-Type: application/json"
        wget_args="$wget_args --header=X-GoClaw-User-Id: system"

        if [[ "$method" == "GET" ]]; then
            docker exec "$CONTAINER" wget -qO- \
                --header="Authorization: Bearer $GOCLAW_TOKEN" \
                --header="X-GoClaw-User-Id: system" \
                "$url" 2>/dev/null
        elif [[ "$method" == "DELETE" ]]; then
            docker exec "$CONTAINER" wget -qO- --method=DELETE \
                --header="Authorization: Bearer $GOCLAW_TOKEN" \
                --header="X-GoClaw-User-Id: system" \
                "$url" 2>/dev/null
        else
            local body_data="${1:-{\}}"
            docker exec "$CONTAINER" wget -qO- \
                --header="Authorization: Bearer $GOCLAW_TOKEN" \
                --header="Content-Type: application/json" \
                --header="X-GoClaw-User-Id: system" \
                --post-data="$body_data" \
                "$url" 2>/dev/null
        fi
    else
        local response http_code
        response=$(curl -s -w "\n%{http_code}" -X "$method" \
            -H "Authorization: Bearer $GOCLAW_TOKEN" \
            -H "Content-Type: application/json" \
            -H "X-GoClaw-User-Id: system" \
            "${GOCLAW_URL}${path}" \
            "$@") || { error "curl failed for $method $path"; return 1; }

        http_code=$(echo "$response" | tail -n1)
        local body
        body=$(echo "$response" | sed '$d')

        if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
            echo "$body"
        elif [[ "$http_code" == "409" ]]; then
            echo "$body"
            return 2
        else
            error "API $method $path → HTTP $http_code"
            echo "$body" | jq . 2>/dev/null || echo "$body"
            return 1
        fi
    fi
}

# Execute SQL against postgres
run_sql() {
    local sql=$1
    if $DOCKER_MODE; then
        docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -tA -c "$sql" 2>/dev/null
    elif [[ -n "${DATABASE_URL:-}" ]]; then
        psql "$DATABASE_URL" -tA -c "$sql" 2>/dev/null
    else
        error "No database access — need Docker mode or DATABASE_URL"
        return 1
    fi
}

run_sql_verbose() {
    local sql=$1
    if $DOCKER_MODE; then
        docker exec "$PG_CONTAINER" psql -U "$PG_USER" -d "$PG_DB" -c "$sql" 2>/dev/null
    elif [[ -n "${DATABASE_URL:-}" ]]; then
        psql "$DATABASE_URL" -c "$sql" 2>/dev/null
    else
        error "No database access"
        return 1
    fi
}

get_agent_id() {
    local key=$1
    run_sql "SELECT id FROM agents WHERE agent_key = '$key' AND tenant_id = '$TENANT_ID' LIMIT 1"
}

# ── IDENTITY.md content per agent ───────────────────────────────

read -r -d '' IDENTITY_LUFFY << 'EOF' || true
# Luffy — Team Lead / Router / Summarizer

- **Name:** Luffy
- **Emoji:** 🏴‍☠️
- **Role:** Captain of the Straw Hat Pirates team
- **Purpose:** Receive requests, decompose into tasks, route to crew members, track progress

## Routing Rules
- For coding/test tasks: route to Zoro (backend), Sanji (frontend), Chopper (QA)
- For research/specs: route to Nami
- Track progress milestones at 25/50/75%
- 3-strike circuit breaker: 3 failures then stop, require manual review
- When worker offline: explicitly reassign, do not wait silently

## CRITICAL — Coding Task Flow
For ALL coding tasks (implement, debug, test, review):
1. Create task and assign to the appropriate member agent (Zoro, Sanji, Chopper)
2. The member agent will pre-audit the task, enrich the brief, and release to the external worker
3. After worker completes, the member agent reviews and approves/rejects
4. You receive the final result after approval

DO NOT set execution_target yourself — member agents handle this via release_to_worker.
DO NOT create unassigned tasks for the worker — always assign to a member agent.

## Crew Roster

| Agent   | Role                          | Routes to for                    |
|---------|-------------------------------|----------------------------------|
| Zoro    | Backend debug & implementation| Go code, API, DB, agent loop     |
| Sanji   | Frontend / UX                 | React, TypeScript, UI components |
| Nami    | Research & planning           | Specs, architecture, analysis    |
| Chopper | QA & testing                  | Test plans, validation, review   |
EOF

read -r -d '' IDENTITY_ZORO << 'EOF' || true
# Zoro — Backend Debug & Implementation

- **Name:** Zoro
- **Emoji:** ⚔️
- **Role:** Backend specialist, debug expert
- **Purpose:** Pre-audit coding tasks, release to external worker, post-audit results

## Coding Task Flow
When assigned a coding task:
1. **Pre-audit**: Review the task requirements, analyze the codebase context
2. **Enrich brief**: Write a detailed execution brief with:
   - Specific files to modify
   - Expected behavior changes
   - Build/test verification commands
   - Timeout expectation (debug=900s, implement=1800s)
3. **Release**: `team_tasks(action="release_to_worker", text="<enriched brief>")`
4. **Wait**: Task goes to external worker. You'll be notified when submitted.
5. **Post-audit**: Review worker result and changed_files
   - Check if changes match requirements
   - Verify no regressions
   - `team_tasks(action="approve")` or `team_tasks(action="reject", text="feedback")`

## Rules
- No direct shell/file access to product repo from VPS
- Distinguish: task_failed vs worker_offline vs blocked_by_dependency
- For research or analysis tasks without code changes, complete the task directly
EOF

read -r -d '' IDENTITY_SANJI << 'EOF' || true
# Sanji — Frontend / UX

- **Name:** Sanji
- **Emoji:** 🍳
- **Role:** Frontend and UX specialist
- **Purpose:** Pre-audit UI tasks, release to external worker, post-audit results

## Coding Task Flow
When assigned a coding task:
1. **Pre-audit**: Review the task requirements, check UI/UX context and component patterns
2. **Enrich brief**: Write a detailed execution brief with:
   - Specific component files to modify
   - Expected UI behavior and acceptance criteria
   - Verification: `pnpm typecheck && pnpm build`
   - Timeout expectation (implement=1800s)
3. **Release**: `team_tasks(action="release_to_worker", text="<enriched brief>")`
4. **Wait**: Task goes to external worker. You'll be notified when submitted.
5. **Post-audit**: Review worker result and changed_files
   - Check if UI changes match requirements
   - Verify no regressions in component behavior
   - `team_tasks(action="approve")` or `team_tasks(action="reject", text="feedback")`

## Rules
- No direct shell/file access to product repo from VPS
- For design specs or research tasks without code changes, complete the task directly
EOF

read -r -d '' IDENTITY_NAMI << 'EOF' || true
# Nami — Research / Planning

- **Name:** Nami
- **Emoji:** 🗺️
- **Role:** Research and planning specialist
- **Purpose:** Write specs to VPS workspace only, produce implementation-ready briefs

## Rules
- Write specs to VPS team workspace ONLY, no product code edits
- Produce implementation-ready briefs for Zoro/Sanji
- Check dependency chain before research that depends on other task output
- Flag circular dependencies to Luffy
- For research tasks, complete directly without release_to_worker
EOF

read -r -d '' IDENTITY_CHOPPER << 'EOF' || true
# Chopper — QA / Testing

- **Name:** Chopper
- **Emoji:** 🩺
- **Role:** QA and verification specialist
- **Purpose:** Pre-audit test tasks, release to external worker, post-audit results

## Coding Task Flow
When assigned a test/verification task:
1. **Pre-audit**: Review the task, define a verification plan with specific test commands
2. **Enrich brief**: Write a detailed execution brief with:
   - Test commands: `go build ./... && go vet ./... && go test -race ./...`
   - Coverage expectations and pass/fail criteria
   - Timeout expectation (test=900s)
3. **Release**: `team_tasks(action="release_to_worker", text="<enriched brief>")`
4. **Wait**: Task goes to external worker. You'll be notified when submitted.
5. **Post-audit**: Review worker result and test output
   - Render PASS/FAIL/TIMEOUT verdict with rationale
   - `team_tasks(action="approve")` or `team_tasks(action="reject", text="feedback")`

## Rules
- TIMEOUT is explicit state, never report as just blocked
- For test plan design or analysis without code execution, complete the task directly
EOF

# ── Delete mode ─────────────────────────────────────────────────
delete_all() {
    header "🗑️  Tearing down Straw Hat team..."

    step "Deleting team members..."
    run_sql "DELETE FROM agent_team_members WHERE team_id IN (SELECT id FROM agent_teams WHERE name = '$TEAM_NAME' AND tenant_id = '$TENANT_ID')" && info "Members removed" || warn "No members to remove"

    step "Deleting team..."
    run_sql "DELETE FROM agent_teams WHERE name = '$TEAM_NAME' AND tenant_id = '$TENANT_ID'" && info "Team removed" || warn "No team to remove"

    for key in "${AGENT_KEYS[@]}"; do
        local agent_id
        agent_id=$(get_agent_id "$key") || true
        if [[ -n "$agent_id" ]]; then
            step "Deleting $key ($agent_id)..."
            run_sql "DELETE FROM agent_context_files WHERE agent_id = '$agent_id'" || true
            run_sql "DELETE FROM agents WHERE id = '$agent_id'" && info "Deleted $key" || warn "Failed to delete $key"
        else
            warn "$key not found"
        fi
    done
    info "Teardown complete"
    exit 0
}

# ── Verify mode ─────────────────────────────────────────────────
verify_all() {
    header "🔍 Verifying Straw Hat setup..."

    step "Checking agents..."
    run_sql_verbose "
        SELECT agent_key, status, provider, model,
               substring(cf.content, 1, 40) AS identity
        FROM agents a
        LEFT JOIN agent_context_files cf ON cf.agent_id = a.id AND cf.file_name = 'IDENTITY.md'
        WHERE a.agent_key IN ('luffy','zoro','sanji','nami','chopper')
          AND a.tenant_id = '$TENANT_ID'
        ORDER BY a.agent_key"

    step "Checking team..."
    run_sql_verbose "
        SELECT t.name, t.status, t.settings, m.role, a.agent_key
        FROM agent_teams t
        JOIN agent_team_members m ON m.team_id = t.id
        JOIN agents a ON a.id = m.agent_id
        WHERE t.name = '$TEAM_NAME'
        ORDER BY m.role DESC, a.agent_key"

    step "Checking skills..."
    run_sql_verbose "SELECT slug, version FROM skills WHERE slug LIKE 'strawhat%' ORDER BY slug"

    step "Checking worker endpoint..."
    local team_id
    team_id=$(run_sql "SELECT id FROM agent_teams WHERE name = '$TEAM_NAME' AND tenant_id = '$TENANT_ID' LIMIT 1")
    if [[ -n "$team_id" ]]; then
        local resp
        resp=$(api GET "/v1/teams/$team_id/worker/tasks?status=pending" 2>/dev/null) || true
        if [[ -n "$resp" ]]; then
            info "Worker endpoint OK — $(echo "$resp" | jq -r '.count') pending tasks"
        else
            warn "Worker endpoint not responding"
        fi
    fi

    info "Verification complete"
    exit 0
}

[[ "${1:-}" == "--delete" ]] && delete_all
[[ "${1:-}" == "--verify" ]] && verify_all

# ══════════════════════════════════════════════════════════════════
#  Main — One-Shot Setup
# ══════════════════════════════════════════════════════════════════

header "🏴‍☠️  Setting up Straw Hat Pirates team (one-shot)"
echo "   Mode: $(if $DOCKER_MODE; then echo 'Docker (local VPS)'; else echo "Remote ($GOCLAW_URL)"; fi)"
echo ""

# ── Step 1: Detect provider/model from existing default agent ───
header "1️⃣  Detecting provider & model"

DEFAULT_PROVIDER=$(run_sql "SELECT provider FROM agents WHERE agent_key = 'default' AND tenant_id = '$TENANT_ID' LIMIT 1") || true
DEFAULT_MODEL=$(run_sql "SELECT model FROM agents WHERE agent_key = 'default' AND tenant_id = '$TENANT_ID' LIMIT 1") || true

if [[ -z "$DEFAULT_PROVIDER" || -z "$DEFAULT_MODEL" ]]; then
    # Fallback: pick first enabled provider
    DEFAULT_PROVIDER=$(run_sql "SELECT name FROM llm_providers WHERE enabled = true AND tenant_id = '$TENANT_ID' LIMIT 1") || true
    DEFAULT_MODEL="${GOCLAW_MODEL:-claude-sonnet-4-20250514}"
fi

if [[ -z "$DEFAULT_PROVIDER" ]]; then
    error "No LLM provider found. Configure a provider first."
    exit 1
fi

info "Provider: $DEFAULT_PROVIDER, Model: $DEFAULT_MODEL"

# ── Step 2: Create agents via HTTP API ──────────────────────────
header "2️⃣  Creating agents"

declare -A AGENT_IDS

create_agent() {
    local key=$1 display=$2 emoji=$3 desc=$4

    local existing_id
    existing_id=$(get_agent_id "$key") || true
    if [[ -n "$existing_id" ]]; then
        warn "$key already exists ($existing_id)"
        AGENT_IDS[$key]="$existing_id"
        return 0
    fi

    step "Creating $emoji $display ($key)..."
    local payload
    payload=$(jq -n \
        --arg key "$key" \
        --arg display "$display" \
        --arg emoji "$emoji" \
        --arg desc "$desc" \
        '{agent_key:$key, display_name:$display, agent_type:"predefined", other_config:{emoji:$emoji, description:$desc}}')

    local resp
    resp=$(api POST "/v1/agents" "$payload") || {
        error "Failed to create $key"
        return 1
    }
    local agent_id
    agent_id=$(echo "$resp" | jq -r '.id')
    AGENT_IDS[$key]="$agent_id"
    info "Created $key → $agent_id"
}

create_agent "luffy"   "Luffy"   "🏴‍☠️" "Team lead, task router, and summarizer for the Straw Hat Pirates crew"
create_agent "zoro"    "Zoro"    "⚔️"    "Backend debug and implementation specialist. Produces execution briefs for Windows local worker."
create_agent "sanji"   "Sanji"   "🍳"    "Frontend and UX implementation specialist. Produces UI briefs for Windows local worker."
create_agent "nami"    "Nami"    "🗺️"    "Research, planning, and spec author. Writes to VPS team workspace only."
create_agent "chopper" "Chopper" "🩺"    "QA, testing, and verification specialist. Reviews build and test output from Windows worker."

# ── Step 3: Activate agents + set provider/model ────────────────
header "3️⃣  Activating agents (provider=$DEFAULT_PROVIDER, model=$DEFAULT_MODEL)"

for key in "${AGENT_KEYS[@]}"; do
    _aid="${AGENT_IDS[$key]:-}"
    if [[ -z "$_aid" ]]; then continue; fi
    run_sql "UPDATE agents SET status = 'active', provider = '$DEFAULT_PROVIDER', model = '$DEFAULT_MODEL', updated_at = NOW() WHERE id = '$_aid' AND (status != 'active' OR provider = '' OR model = '')" >/dev/null
done
info "All agents active"

# ── Step 4: Seed IDENTITY.md ───────────────────────────────────
header "4️⃣  Seeding IDENTITY.md"

seed_identity() {
    local key=$1 content=$2
    local agent_id="${AGENT_IDS[$key]:-}"
    if [[ -z "$agent_id" ]]; then
        warn "No agent ID for $key, skipping"
        return 0
    fi
    step "Setting IDENTITY.md for $key..."
    local escaped
    escaped=$(echo "$content" | sed "s/'/''/g")
    run_sql "UPDATE agent_context_files SET content = '$escaped', updated_at = NOW() WHERE agent_id = '$agent_id' AND file_name = 'IDENTITY.md'" >/dev/null
    info "IDENTITY.md set for $key"
}

seed_identity "luffy"   "$IDENTITY_LUFFY"
seed_identity "zoro"    "$IDENTITY_ZORO"
seed_identity "sanji"   "$IDENTITY_SANJI"
seed_identity "nami"    "$IDENTITY_NAMI"
seed_identity "chopper" "$IDENTITY_CHOPPER"

# ── Step 5: Create team ────────────────────────────────────────
header "5️⃣  Creating team"

EXISTING_TEAM=$(run_sql "SELECT id FROM agent_teams WHERE name = '$TEAM_NAME' AND tenant_id = '$TENANT_ID' LIMIT 1") || true

if [[ -n "$EXISTING_TEAM" ]]; then
    warn "Team already exists: $EXISTING_TEAM"
    TEAM_ID_RESULT="$EXISTING_TEAM"
else
    step "Creating team '$TEAM_NAME'..."
    TEAM_ID_RESULT=$(run_sql "
        INSERT INTO agent_teams (id, name, lead_agent_id, description, status, settings, created_by, tenant_id, created_at, updated_at)
        VALUES (
            gen_random_uuid(),
            '$TEAM_NAME',
            '${AGENT_IDS[luffy]}',
            'VPS control plane team for orchestrating Windows local coding bridge.',
            'active',
            '{\"version\": 2}'::jsonb,
            'system',
            '$TENANT_ID',
            NOW(), NOW()
        )
        RETURNING id")
    info "Team created → $TEAM_ID_RESULT"
fi

# ── Step 6: Add members ────────────────────────────────────────
header "6️⃣  Adding team members"

add_member() {
    local key=$1 role=$2
    local agent_id="${AGENT_IDS[$key]:-}"
    if [[ -z "$agent_id" ]]; then return; fi

    local exists
    exists=$(run_sql "SELECT 1 FROM agent_team_members WHERE team_id = '$TEAM_ID_RESULT' AND agent_id = '$agent_id' LIMIT 1") || true
    if [[ -n "$exists" ]]; then
        warn "$key already a member"
        return 0
    fi

    run_sql "INSERT INTO agent_team_members (team_id, agent_id, role, joined_at, tenant_id) VALUES ('$TEAM_ID_RESULT', '$agent_id', '$role', NOW(), '$TENANT_ID')" >/dev/null
    info "Added $key as $role"
}

add_member "luffy"   "lead"
add_member "zoro"    "member"
add_member "sanji"   "member"
add_member "nami"    "member"
add_member "chopper" "member"

# ── Step 7: Verify worker endpoint ─────────────────────────────
header "7️⃣  Verifying worker endpoints"

step "Testing GET /worker/tasks..."
resp=$(api GET "/v1/teams/$TEAM_ID_RESULT/worker/tasks?status=pending" 2>/dev/null) || true
if [[ -n "$resp" ]]; then
    count=$(echo "$resp" | jq -r '.count // 0')
    info "Worker endpoint OK — $count pending tasks"
else
    warn "Worker endpoint not responding — check deployment"
fi

# ── Summary ─────────────────────────────────────────────────────
header "✅ Setup Complete"
echo ""
printf "  %-10s %-6s %-38s %s\n" "Agent" "Emoji" "ID" "Status"
printf "  %-10s %-6s %-38s %s\n" "─────" "─────" "──────────────────────────────────────" "──────"
for key in "${AGENT_KEYS[@]}"; do
    local_emoji=""
    case "$key" in
        luffy)  local_emoji="🏴‍☠️" ;;
        zoro)   local_emoji="⚔️" ;;
        sanji)  local_emoji="🍳" ;;
        nami)   local_emoji="🗺️" ;;
        chopper) local_emoji="🩺" ;;
    esac
    printf "  %-10s %-6s %-38s %s\n" "$key" "$local_emoji" "${AGENT_IDS[$key]:-?}" "active"
done
echo ""
echo "  Team: $TEAM_NAME"
echo "  Team ID: $TEAM_ID_RESULT"
echo "  Provider: $DEFAULT_PROVIDER / $DEFAULT_MODEL"
echo ""
info "Straw Hat Pirates crew is ready! 🏴‍☠️"
echo ""
echo "  Next steps:"
echo "  1. Create API key (dashboard → API Keys → scope: operator.write)"
echo "  2. Configure Windows worker (see scripts/local-coding-worker.ps1)"
echo "  3. Run: bash scripts/setup-strawhat-team.sh --verify"
