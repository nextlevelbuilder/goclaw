#!/usr/bin/env bash
# Generates COMPOSE_FILE from compose.d/*.yml
set -euo pipefail

SCRIPT="${BASH_SOURCE[0]}"
SCRIPT_DIR="$(cd "$(dirname "${SCRIPT}")" && pwd)"
ENV_FILE="${GOCLAW_ENV_FILE:-$SCRIPT_DIR/.env}"

loud() {
  [[ "${QUIET:-false}" != true ]] && echo "$@"
  true
}

# Show help
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  echo "Usage: $SCRIPT [--quiet] [--skip-validation]"
  echo ""
  echo "  --quiet           Suppress normal output"
  echo "  --skip-validation Skip docker compose config validation"
  echo ""
  echo "  Generates COMPOSE_FILE from compose.d/*.yml files (sorted)"
  echo "  Updates .env with the resulting COMPOSE_FILE value"
  echo ""
  echo "Note: docker-compose reads .env automatically"
  echo "      for podman-compose: source .env first"
  exit 0
fi

# Parse flags
SKIP_VALIDATION=false
for arg in "$@"; do
  case "$arg" in
    --quiet) QUIET=true ;;
    --skip-validation) SKIP_VALIDATION=true ;;
  esac
done

cd "$SCRIPT_DIR" >/dev/null 2>&1

[[ ! -f "docker-compose.yml" ]] && echo "docker-compose.yml not found" && exit 1

# Build COMPOSE_FILE from compose.d files (sorted)
COMPOSE_FILE=""
for f in compose.d/*.yml; do
  [[ -e "$f" ]] && COMPOSE_FILE="$COMPOSE_FILE${COMPOSE_FILE:+:}$f"
done
export COMPOSE_FILE

# Validate compose files
if [[ "$SKIP_VALIDATION" != true ]]; then
  DOCKER_CMD="${DOCKER_CMD:-docker}"
  $DOCKER_CMD compose config > /dev/null 2>&1 || { echo "Compose config validation failed"; exit 1; }
  loud "Compose config valid"
fi

# Update a key=value line in .env safely (via temp file)
update_env() {
  local key="$1" value="$2"
  if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
    sed "s|^${key}=.*|${key}='${value}'|" "$ENV_FILE" > "$ENV_FILE.tmp" && mv "$ENV_FILE.tmp" "$ENV_FILE"
  else
    echo "${key}='${value}'" >> "$ENV_FILE"
  fi
}

# Update .env with COMPOSE_FILE and GOCLAW_DIR
if [[ -f "$ENV_FILE" ]]; then
  update_env "COMPOSE_FILE" "$COMPOSE_FILE"
  update_env "GOCLAW_DIR" "$SCRIPT_DIR"
  loud "COMPOSE_FILE updated in $ENV_FILE"
  loud "  COMPOSE_FILE=$COMPOSE_FILE"
else
  loud "File not found: $ENV_FILE"
fi

loud "(run '${SCRIPT} --help' for help)"
