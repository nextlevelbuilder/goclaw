#!/usr/bin/env bash
# prepare-env.sh — Create or update .env with auto-generated secrets.
# Safe to run multiple times: only fills in missing values, never overwrites existing ones.
#
# Usage:  ./prepare-env.sh

set -euo pipefail

ENV_FILE=".env"

# --- helpers ---

gen_hex() { openssl rand -hex "$1" 2>/dev/null || head -c "$1" /dev/urandom | xxd -p | tr -d '\n'; }

# Read current value from .env (KEY=VALUE format, no export prefix).
get_env_val() {
  local key="$1"
  if [ -f "$ENV_FILE" ]; then
    grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -1 | cut -d'=' -f2-
  fi
}

# Set a key in .env. Appends if missing, replaces if empty.
set_env_val() {
  local key="$1" val="$2"
  if [ ! -f "$ENV_FILE" ]; then
    echo "${key}=${val}" >> "$ENV_FILE"
  elif grep -qE "^${key}=" "$ENV_FILE" 2>/dev/null; then
    # Key exists — only replace if current value is empty
    local current
    current="$(get_env_val "$key")"
    if [ -z "$current" ]; then
      if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
      else
        sed -i "s|^${key}=.*|${key}=${val}|" "$ENV_FILE"
      fi
    fi
  else
    echo "${key}=${val}" >> "$ENV_FILE"
  fi
}

# --- main ---

echo ""
echo "=== GoClaw Environment Preparation ==="
echo ""

# 1. Create .env from .env.example if it doesn't exist
if [ ! -f "$ENV_FILE" ]; then
  if [ -f ".env.example" ]; then
    # Strip 'export ' prefix for Docker Compose compatibility
    sed 's/^export //' .env.example > "$ENV_FILE"
    echo "  [created]   .env from .env.example"
  else
    touch "$ENV_FILE"
    echo "  [created]   .env (empty)"
  fi
else
  echo "  [exists]    .env"
fi

# 2. Auto-generate GOCLAW_ENCRYPTION_KEY if missing
current_enc="$(get_env_val GOCLAW_ENCRYPTION_KEY)"
if [ -z "$current_enc" ]; then
  new_key="$(gen_hex 32)"
  set_env_val "GOCLAW_ENCRYPTION_KEY" "$new_key"
  echo "  [generated] GOCLAW_ENCRYPTION_KEY"
else
  echo "  [exists]    GOCLAW_ENCRYPTION_KEY"
fi

# 3. Auto-generate GOCLAW_GATEWAY_TOKEN if missing
current_tok="$(get_env_val GOCLAW_GATEWAY_TOKEN)"
if [ -z "$current_tok" ]; then
  new_tok="$(gen_hex 16)"
  set_env_val "GOCLAW_GATEWAY_TOKEN" "$new_tok"
  echo "  [generated] GOCLAW_GATEWAY_TOKEN"
else
  echo "  [exists]    GOCLAW_GATEWAY_TOKEN"
fi

# 4. Check provider API key
has_provider=false
for key in GOCLAW_OPENROUTER_API_KEY GOCLAW_ANTHROPIC_API_KEY GOCLAW_OPENAI_API_KEY \
           GOCLAW_MINIMAX_API_KEY GOCLAW_GROQ_API_KEY GOCLAW_DEEPSEEK_API_KEY \
           GOCLAW_GEMINI_API_KEY GOCLAW_MISTRAL_API_KEY GOCLAW_XAI_API_KEY \
           GOCLAW_COHERE_API_KEY GOCLAW_PERPLEXITY_API_KEY; do
  val="$(get_env_val "$key")"
  if [ -n "$val" ]; then
    has_provider=true
    echo "  [exists]    $key"
    break
  fi
done

if [ "$has_provider" = false ]; then
  echo "  [missing]   No LLM provider API key found"
  echo ""
  echo "  Add at least one provider key to .env before starting:"
  echo "    GOCLAW_OPENROUTER_API_KEY=sk-or-..."
  echo "    GOCLAW_ANTHROPIC_API_KEY=sk-ant-..."
  echo "    GOCLAW_MINIMAX_API_KEY=..."
  echo ""
  echo "=== Done (action required) ==="
  echo ""
  exit 0
fi

echo ""
echo "=== Done ==="
echo ""
echo "  Run: docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml up -d --build"
echo ""
