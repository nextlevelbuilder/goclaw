# Home Assistant Add-on: GoClaw Gateway

PostgreSQL multi-tenant AI agent gateway with WebSocket RPC, HTTP API,
browser automation, and an embedded web dashboard.

This addon extends the pre-built `ghcr.io/nextlevelbuilder/goclaw` image
with headless Chromium — equivalent to `make up WITH_BROWSER=1 WITH_REDIS=1`.
It leverages the **TimescaleDB addon** for PostgreSQL and optionally a
**Redis addon** for caching.

## Prerequisites

### 1. TimescaleDB Addon (PostgreSQL)

Install the TimescaleDB addon which provides PostgreSQL:

1. Add the repository: `https://github.com/expaso/hassos-addons`
2. Install **TimescaleDB** from the addon store
3. In the TimescaleDB addon configuration, add `goclaw` to the `databases` list:
   ```yaml
   databases:
     - homeassistant
     - goclaw
   ```
4. Start the TimescaleDB addon
5. Note the addon's **hostname** from its info page (e.g., `a0d7b954-timescaledb`)

### 2. Redis Addon (Optional)

If you have a Redis addon installed, configure `redis_host` to point to
its hostname. When no Redis host is configured, GoClaw uses in-memory
caching which is fine for single-instance deployments.

## Installation

1. Add the GoClaw repository to Home Assistant:
   `https://github.com/nextlevelbuilder/goclaw`
2. Install **GoClaw Gateway** from the addon store
3. Configure the addon (see Configuration below)
4. Start the addon
5. Open the web dashboard at `http://homeassistant.local:18790`

## Configuration

### PostgreSQL Connection

| Option | Default | Description |
|--------|---------|-------------|
| `postgres_host` | `a0d7b954-timescaledb` | TimescaleDB addon hostname. Find on the addon info page. |
| `postgres_port` | `5432` | PostgreSQL port |
| `postgres_user` | `postgres` | PostgreSQL username |
| `postgres_password` | `homeassistant` | PostgreSQL password |
| `postgres_database` | `goclaw` | Database name (must exist in TimescaleDB) |

**Finding the TimescaleDB hostname:**
Go to **Settings → Add-ons → TimescaleDB → Info**. The hostname is shown
on the page (format: `{hash}-timescaledb`).

### Redis (Optional)

| Option | Default | Description |
|--------|---------|-------------|
| `redis_host` | *(empty)* | Redis addon hostname. Leave empty to skip. |
| `redis_port` | `6379` | Redis port |

When `redis_host` is set, GoClaw connects to the external Redis addon
for caching. When empty, GoClaw uses in-memory caching.

### Browser Automation

| Option | Default | Description |
|--------|---------|-------------|
| `enable_browser` | `true` | Start headless Chromium for browser tools |

When enabled, a headless Chromium instance runs inside the container,
exposing Chrome DevTools Protocol (CDP) on port 9222. This enables
GoClaw agents to browse the web, take screenshots, and extract data.

**Memory impact:** Chromium adds ~200MB idle memory usage, more under load.
Disable if you don't need browser automation and want to save resources.

### Security

| Option | Default | Description |
|--------|---------|-------------|
| `gateway_token` | *(empty)* | WebSocket authentication token |
| `encryption_key` | *(empty)* | AES-256-GCM key for API key encryption |

**Gateway token:** Required for web dashboard and WebSocket client
authentication. If left empty, the addon **auto-generates a random 64-char
hex token** on first start and persists it at `/data/goclaw/gateway.token`.
Look for it in the addon logs (printed on each startup) or view the file
directly via the Samba/File editor addon. To use your own token, set this
option to any non-empty string.

**Encryption key:** Used to encrypt LLM provider API keys stored in the
database. Generate with:

```bash
openssl rand -hex 32
```

### Logging

| Option | Default | Description |
|--------|---------|-------------|
| `log_level` | `info` | Log level: trace, debug, info, warn, error |
| `trace_verbose` | `false` | Enable verbose LLM call tracing |

## Network Ports

| Port | Protocol | Description |
|------|----------|-------------|
| 18790 | TCP | GoClaw API & Web Dashboard |
| 9222 | TCP | Chrome DevTools Protocol (optional) |

Port 18790 is exposed by default. Port 9222 is disabled by default
(set to `null`); expose it only if you need external CDP access for
debugging.

## Data Storage

All persistent data is stored under `/data/` (HA addon data volume):

- `/data/goclaw/config.json` — GoClaw configuration
- `/data/goclaw/workspace/` — Agent workspace files
- `/data/goclaw/skills/` — Installed skills

Data survives addon restarts and updates. Back up via Home Assistant's
built-in backup system.

## How It Works

The addon uses the pre-built `ghcr.io/nextlevelbuilder/goclaw` Docker
image as its base (specified in `build.yaml`). On top of that, the
Dockerfile adds Chromium for browser automation. The HA Supervisor builds
this automatically when you install the addon — no source compilation needed.

PostgreSQL and Redis are provided by their respective HA addons, keeping
each concern in its own container.

## Notes on pgvector

GoClaw uses pgvector for its vector memory system (embeddings). The
TimescaleDB addon may not include pgvector by default. If pgvector is
not available, GoClaw will still function but vector-based memory
features (semantic search, embeddings) will be disabled.

To check if pgvector is available, connect to PostgreSQL and run:

```sql
CREATE EXTENSION IF NOT EXISTS vector;
```

## Troubleshooting

**Addon won't start — database connection refused:**
Ensure the TimescaleDB addon is running and the hostname is correct.
Check the addon logs for connection error details.

**"database does not exist" error:**
Add `goclaw` to the `databases` list in TimescaleDB addon configuration
and restart it.

**Chromium crashes or high memory usage:**
Set `enable_browser` to `false` if browser automation is not needed.
On devices with <2GB RAM, disabling the browser is recommended.

**Redis not connecting:**
Verify the Redis addon is running and the hostname is correct. Find the
hostname on the Redis addon info page (format: `{hash}-{slug}`).
