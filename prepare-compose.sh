#!/usr/bin/env bash
# Generates COMPOSE_FILE from .env-compose selection, writes to .env
set -euo pipefail

SCRIPT="${BASH_SOURCE[0]}"
SCRIPT_DIR="$(cd "$(dirname "${SCRIPT}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/.env"
ENV_COMPOSE="$SCRIPT_DIR/.env-compose"
EDITOR="${EDITOR:-${VISUAL:-nano}}"

loud() {
  [[ "${QUIET:-false}" != true ]] && echo "$@"
  true
}

# Find all compose yml files in root dir and options/ subdirectory
find_compose_files() {
  local dir="$1"
  (
    find "$dir" -maxdepth 1 -name "*.yml" 2>/dev/null
    if [[ -d "$dir/options" ]]; then
      find "$dir/options" -maxdepth 2 -name "*.yml" 2>/dev/null
    fi
  ) | sort
}

# Check if a file is a compose file by content
is_compose_file() {
  [[ "$1" == *.yml ]] || return 1
  grep -q "^services:\|^networks:\|^volumes:" "$1" 2>/dev/null
}

# Categorize a compose file by filename only
# . = service, + = overlay, otherwise = root
categorize_compose() {
  local name="${1%.yml}"
  [[ "$name" == *+* ]] && echo "overlay" && return
  [[ "$name" == *.* ]] && echo "service" && return
  echo "root"
}

# Read current COMPOSE_FILE from .env, return colon-separated list
read_compose_file() {
  if [[ -f "$ENV_FILE" ]]; then
    grep "^COMPOSE_FILE=" "$ENV_FILE" 2>/dev/null | head -1 | sed 's/^COMPOSE_FILE=//' | tr -d "'"
  fi
}

# Update a key=value line in .env safely
update_env() {
  local key="$1" value="$2"
  if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
    sed "s|^${key}=.*|${key}='${value}'|" "$ENV_FILE" > "$ENV_FILE.tmp" && mv "$ENV_FILE.tmp" "$ENV_FILE"
  else
    echo "${key}='${value}'" >> "$ENV_FILE"
  fi
}

# Write COMPOSE_FILE to .env
write_compose_file() {
  update_env "COMPOSE_FILE" "$1"
  loud "COMPOSE_FILE='$1'"
}

# Generate .env-compose from available files and current selection
do_generate() {
  local current_compose="${1:-}"
  local line

  echo "# Docker Compose file picker"
  echo "# Lines starting with # are disabled"
  echo "# Remove # to enable a file"
  echo ""

  local roots="" services="" overlays=""

  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    if is_compose_file "$line"; then
      local cat=$(categorize_compose "$line")
      local rel="${line#$SCRIPT_DIR/}"
      local enabled="# "
      if [[ -n "$current_compose" && "$current_compose" == *"$rel"* ]]; then
        enabled=""
      fi
      case "$cat" in
        root) roots="${roots}${roots:+$'\n'}${enabled}${rel}" ;;
        service) services="${services}${services:+$'\n'}${enabled}${rel}" ;;
        overlay) overlays="${overlays}${overlays:+$'\n'}${enabled}${rel}" ;;
      esac
    fi
  done < <(find_compose_files "$SCRIPT_DIR")

  if [[ -n "$roots" ]]; then
    echo "# === ROOT (required) ==="
    echo "$roots"
    echo ""
  fi
  if [[ -n "$services" ]]; then
    echo "# === SERVICE (optional) ==="
    echo "$services"
    echo ""
  fi
  if [[ -n "$overlays" ]]; then
    echo "# === OVERLAY (optional) ==="
    echo "$overlays"
  fi
}

# Parse enabled files from .env-compose (uncommented, non-empty lines)
do_parse() {
  local result=""
  local line

  while IFS= read -r line || [[ -n "$line" ]]; do
    line="${line#"${line%%[![:space:]]*}"}"  # trim leading whitespace
    [[ -z "$line" || "$line" == \#* ]] && continue
    line="${line#\# }"  # remove "# " prefix
    [[ -z "$line" || "$line" == \#* ]] && continue
    [[ -n "$result" ]] && result="${result}:"
    result="${result}${line}"
  done < "$ENV_COMPOSE"

  echo "$result"
}

# Apply selection from .env-compose to .env
do_update() {
  if [[ ! -f "$ENV_COMPOSE" ]]; then
    echo "No .env-compose found. Run '$SCRIPT --generate' first."
    exit 1
  fi

  local selection
  selection=$(do_parse)

  if [[ -z "$selection" ]]; then
    echo "No compose files selected in .env-compose"
  fi

  write_compose_file "$selection"
  loud "Done. COMPOSE_FILE=$selection"
}

# Validate compose files with docker/podman compose config
do_check() {
  if [[ ! -f "$ENV_COMPOSE" ]]; then
    echo "No .env-compose found"
    exit 1
  fi

  # Source .env to get COMPOSE_FILE
  set -a
  source "$ENV_FILE" 2>/dev/null || true
  set +a

  local engine="${DOCKER_CMD:-docker}"
  if ! command -v "$engine" &>/dev/null; then
    echo "$engine not found"
    exit 1
  fi

  if "$engine" compose config >/dev/null 2>&1; then
    echo "✓ Compose config valid"
    exit 0
  else
    echo "✗ Compose config invalid"
    "$engine" compose config 2>&1 | head -5
    exit 1
  fi
}

# Open editor, then apply
do_edit() {
  if [[ ! -f "$ENV_COMPOSE" ]]; then
    loud "Regenerating $ENV_COMPOSE..."
    local current
    current=$(read_compose_file)
    do_generate "$current" > "$ENV_COMPOSE"
  fi

  if ! "$EDITOR" "$ENV_COMPOSE"; then
    echo "Editor failed (EDITOR=$EDITOR)"
    exit 1
  fi

  do_update
  do_check
}

# Show help
show_help() {
  cat << EOF
Usage: $SCRIPT [--quiet] [--generate] [--update] [--edit] [--check] [--file <file>]

  --generate   Create/replace .env-compose from available compose files
  --edit        Open .env-compose in \$EDITOR, then apply to .env
  --update      Apply current .env-compose to .env
  --check       Validate compose config using \$DOCKER_CMD (default: docker)
  --file <f>    Copy <f> to .env-compose (-f also works)
  --quiet       Suppress normal output

  Finds all compose *.yml files under this directory.
  Reads .env-compose for file selection (uncommented lines = enabled).
  Writes resulting COMPOSE_FILE to .env

  .env-compose format:
    docker-compose.yml   # enabled
    # docker-compose.postgres.yml  # disabled (commented)
EOF
  exit 0
}

# Main
QUIET=false
GENERATE=false
UPDATE=false
EDIT=false
CHECK=false
NEXT_FILE=""

for arg in "$@"; do
  if [[ "$NEXT_FILE" ]]; then
    cp "$arg" "$ENV_COMPOSE"
    echo "Copied $arg to $ENV_COMPOSE"
    NEXT_FILE=""
    UPDATE=true
    CHECK=true
  else
    case "$arg" in
      --quiet) QUIET=true ;;
      --generate) GENERATE=true ;;
      --update) UPDATE=true ;;
      --edit) EDIT=true ;;
      --check) CHECK=true ;;
      --help|-h) show_help ;;
      --file|-f) NEXT_FILE="yes" ;;
      --file=*|-f=*)
        src="${arg#--file=}"
        src="${src#-f=}"
        cp "$src" "$ENV_COMPOSE"
        echo "Copied $src to $ENV_COMPOSE"
        UPDATE=true
        CHECK=true
        ;;
      *) echo "Unknown: $arg" ;;
    esac
  fi
done

cd "$SCRIPT_DIR" >/dev/null 2>&1

# No args = help (unless FILE was set, which auto-sets UPDATE/CHECK)
if [[ "$GENERATE" == false && "$UPDATE" == false && "$EDIT" == false && "$CHECK" == false ]]; then
  show_help
fi

if [[ "$GENERATE" == true ]]; then
  loud "Generating $ENV_COMPOSE..."
  current=$(read_compose_file)
  do_generate "$current" > "$ENV_COMPOSE"
  loud "Generated $ENV_COMPOSE"
fi

if [[ "$EDIT" == true ]]; then
  do_edit
fi

if [[ "$UPDATE" == true ]]; then
  do_update
fi

if [[ "$CHECK" == true ]]; then
  do_check
fi
