#!/usr/bin/env python3
"""Fix MCP server auth headers in production goclaw database."""
import json
import subprocess
import sys

MCP_JSON = "/home/patate/.mcp.json"
SSH_HOST = "root@10.255.255.13"
SSH_PORT = "22022"
PG_CONTAINER = "database"
PG_USER = "goclaw"
PG_DB = "goclaw"

def main():
    with open(MCP_JSON) as f:
        cfg = json.load(f)

    servers = cfg.get("mcpServers", cfg)

    sql_statements = []
    skipped = []

    for name, config in servers.items():
        headers = config.get("headers", {})
        auth = headers.get("Authorization", "")
        if auth:
            # Escape single quotes in the JSON value
            headers_json = json.dumps({"Authorization": auth}).replace("'", "''")
            sql = f"UPDATE mcp_servers SET headers = '{headers_json}' WHERE name = '{name}';"
            sql_statements.append((name, sql))
        else:
            skipped.append(name)

    if not sql_statements:
        print("No servers with auth headers found.")
        sys.exit(1)

    full_sql = "\n".join(sql for _, sql in sql_statements)

    ssh_cmd = [
        "ssh", "-p", SSH_PORT, SSH_HOST,
        f"docker exec -i {PG_CONTAINER} psql -U {PG_USER} -d {PG_DB}"
    ]

    result = subprocess.run(
        ssh_cmd,
        input=full_sql,
        capture_output=True,
        text=True
    )

    if result.returncode != 0:
        print(f"ERROR: {result.stderr}", file=sys.stderr)
        sys.exit(1)

    for name, _ in sql_statements:
        print(f"Updated: {name}")
    for name in skipped:
        print(f"Skipped (no auth): {name}")

    print(f"\nDone. Updated {len(sql_statements)} servers.")

if __name__ == "__main__":
    main()
