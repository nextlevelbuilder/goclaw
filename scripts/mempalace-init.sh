#!/bin/sh
# mempalace-init.sh — Register MemPalace as a DB-backed MCP server.
# Called before gateway startup when docker-compose.mempalace.yml is active.
#
# This script:
# 1. Creates the mempalace data directory and default identity
# 2. Registers mempalace in mcp_servers table (visible in admin dashboard)
# 3. Auto-grants mempalace to all existing agents
set -eu

PALACE_PATH="${MEMPALACE_PALACE_PATH:-/app/data/.mempalace/palace}"
IDENTITY_PATH="${MEMPALACE_IDENTITY_PATH:-/app/data/.mempalace/identity.txt}"
MEMPALACE_DIR="$(dirname "$PALACE_PATH")"
MEMPALACE_URL="${MEMPALACE_SSE_URL:-http://mempalace:8000/mcp}"

# ── 1. Initialize palace directory & identity ──

mkdir -p "$MEMPALACE_DIR"

if [ ! -f "$IDENTITY_PATH" ]; then
    cat > "$IDENTITY_PATH" << 'IDENTITY'
I am a GoClaw AI agent with MemPalace memory.
I remember conversations, decisions, and context across sessions.
I use the palace structure (wings, rooms, halls) to organize memories.
IDENTITY
    echo "MemPalace: created default identity at $IDENTITY_PATH"
fi

if [ ! -d "$PALACE_PATH" ]; then
    mkdir -p "$PALACE_PATH"
    echo "MemPalace: initialized palace at $PALACE_PATH"
fi

# ── 2. Remove config.json entry (migrate to DB-backed) ──
# DB-backed servers are visible in the admin dashboard; config-based are not.
CONFIG_FILE="${GOCLAW_CONFIG:-/app/data/config.json}"
if [ -f "$CONFIG_FILE" ] && grep -q '"mempalace"' "$CONFIG_FILE" 2>/dev/null; then
    python3 -c "
import json, os
cfg_path = os.environ.get('GOCLAW_CONFIG', '/app/data/config.json')
with open(cfg_path) as f:
    cfg = json.load(f)
servers = cfg.get('tools', {}).get('mcp_servers', {})
if 'mempalace' in servers:
    del servers['mempalace']
    with open(cfg_path, 'w') as f:
        json.dump(cfg, f, indent=2)
    print('MemPalace: removed config.json entry (migrated to DB-backed)')
" 2>/dev/null || true
fi

# ── 3. Register in database ──

# Install pg8000 (pure-Python PostgreSQL driver) if not present
python3 -c "import pg8000" 2>/dev/null || pip3 install --no-cache-dir --break-system-packages pg8000 2>/dev/null

python3 << PYEOF
import os, sys

mempalace_url = os.environ.get("MEMPALACE_SSE_URL", "http://mempalace:8000/mcp")
dsn = os.environ.get("GOCLAW_POSTGRES_DSN", "")

if not dsn:
    print("MemPalace: no GOCLAW_POSTGRES_DSN, skipping DB registration")
    sys.exit(0)

try:
    from urllib.parse import urlparse
    p = urlparse(dsn)
    db_host = p.hostname or "postgres"
    db_port = p.port or 5432
    db_user = p.username or "goclaw"
    db_pass = p.password or "goclaw"
    db_name = p.path.lstrip("/") or "goclaw"
except Exception as e:
    print(f"MemPalace: failed to parse DSN: {e}")
    sys.exit(0)

# Wait for postgres
import socket, time
for i in range(30):
    try:
        s = socket.create_connection((db_host, db_port), timeout=2)
        s.close()
        break
    except OSError:
        if i == 29:
            print("MemPalace: postgres not ready after 30s, skipping DB registration")
            sys.exit(0)
        time.sleep(1)

try:
    import pg8000.native
    conn = pg8000.native.Connection(
        user=db_user, password=db_pass,
        host=db_host, port=db_port, database=db_name
    )

    # Check if mempalace already registered
    rows = conn.run("SELECT id::text FROM mcp_servers WHERE name = 'mempalace'")

    if rows:
        server_id = rows[0][0]
        print(f"MemPalace: server already registered (id={server_id[:8]}...)")
    else:
        # Get tenant_id
        tenant_rows = conn.run("SELECT id::text FROM tenants LIMIT 1")
        tenant_id = tenant_rows[0][0] if tenant_rows else "0193a5b0-7000-7000-8000-000000000001"

        # Register MCP server
        result = conn.run(
            """INSERT INTO mcp_servers
               (name, display_name, transport, url, tool_prefix, timeout_sec, enabled, created_by, tenant_id)
               VALUES (:name, :display, :transport, :url, :prefix, :timeout, true, 'system', :tenant::uuid)
               RETURNING id::text""",
            name="mempalace",
            display="MemPalace",
            transport="streamable-http",
            url=mempalace_url,
            prefix="mempalace",
            timeout=30,
            tenant=tenant_id,
        )
        server_id = result[0][0]
        print(f"MemPalace: registered in database (id={server_id[:8]}...)")

    # Auto-grant to all agents without existing grants
    conn.run(
        """INSERT INTO mcp_agent_grants (server_id, agent_id, enabled, granted_by, tenant_id)
           SELECT :sid::uuid, a.id, true, 'system', a.tenant_id
           FROM agents a
           WHERE NOT EXISTS (
               SELECT 1 FROM mcp_agent_grants mag
               WHERE mag.server_id = :sid::uuid AND mag.agent_id = a.id
           )""",
        sid=server_id,
    )

    grant_rows = conn.run(
        "SELECT COUNT(*) FROM mcp_agent_grants WHERE server_id = :sid::uuid",
        sid=server_id,
    )
    total = grant_rows[0][0]
    print(f"MemPalace: {total} agent grant(s) active")

    conn.close()

except Exception as e:
    print(f"MemPalace: DB registration failed: {e}", file=sys.stderr)
    # Non-fatal — gateway can still start with config-based fallback
    sys.exit(0)
PYEOF

echo "MemPalace: init complete"
