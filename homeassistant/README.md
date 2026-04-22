# GoClaw Add-ons for Home Assistant

This repository provides the **GoClaw Gateway** as a Home Assistant add-on —
a PostgreSQL multi-tenant AI agent gateway with WebSocket RPC, HTTP API,
browser automation, and a web dashboard.

The add-on is equivalent to running `make up WITH_BROWSER=1 WITH_REDIS=1`
but delegates PostgreSQL to the **TimescaleDB add-on** and (optionally)
caching to a **Redis add-on**, keeping each concern in its own container.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Home Assistant                                          │
│                                                          │
│  ┌────────────────────┐   ┌────────────────────────────┐ │
│  │ TimescaleDB add-on │   │ GoClaw Gateway add-on      │ │
│  │ (expaso)           │◄──┤  - Go binary               │ │
│  │  PostgreSQL :5432  │   │  - Embedded web UI :18790  │ │
│  └────────────────────┘   │  - Headless Chromium :9222 │ │
│                           └────────────┬───────────────┘ │
│  ┌────────────────────┐                │                 │
│  │ Redis add-on       │◄───────────────┘ (optional)      │
│  │  (optional)        │                                  │
│  └────────────────────┘                                  │
└──────────────────────────────────────────────────────────┘
```

## Installation

### Step 1 — Install the TimescaleDB add-on (required)

The GoClaw add-on needs PostgreSQL. We use the community TimescaleDB add-on
which includes `pgvector` and is the most widely maintained Postgres option
for Home Assistant OS.

1. Go to **Settings → Add-ons → Add-on Store**
2. Click the **⋮** menu (top right) → **Repositories**
3. Add: `https://github.com/expaso/hassos-addons`
4. Close and refresh. Find **PostgreSQL + TimescaleDB** in the store and
   click **Install**.
5. Before starting the add-on, open its **Configuration** tab and add
   `goclaw` to the `databases` list so the database is created automatically:

   ```yaml
   databases:
     - homeassistant
     - goclaw
   timescale_enabled:
     - homeassistant
   ```

6. Start the add-on and verify it's healthy in the **Log** tab.
7. Open the **Info** tab and copy the **Hostname** (format:
   `{hash}-timescaledb`, e.g. `a0d7b954-timescaledb`). You'll need it next.

### Step 2 — Install a Redis add-on (optional)

GoClaw works fine with in-memory caching. Skip this step unless you need
Redis for distributed caching or you're running multiple GoClaw instances.

There is no widely adopted official Redis add-on for Home Assistant. If
you want Redis, you have two options:

- **External Redis** — run Redis on your HA host or another server and
  expose it on the network. Set `redis_host` to its hostname/IP.
- **Bring your own add-on** — any community add-on that runs Redis works
  as long as it exposes port 6379 on the HA internal network. Copy its
  hostname from the add-on **Info** page.

### Step 3 — Install the GoClaw Gateway add-on

1. Go to **Settings → Add-ons → Add-on Store**
2. Click **⋮** → **Repositories**
3. Add: `https://github.com/nextlevelbuilder/goclaw`
4. Find **GoClaw Gateway** in the store and click **Install**.
5. On the **Configuration** tab, set at minimum the TimescaleDB hostname
   you copied in step 1:

   ```yaml
   postgres_host: a0d7b954-timescaledb   # ← replace with your hostname
   postgres_port: 5432
   postgres_user: postgres
   postgres_password: homeassistant
   postgres_database: goclaw
   ```

   If you set up a Redis add-on, also set:

   ```yaml
   redis_host: a0d7b954-redis   # ← your Redis add-on hostname
   redis_port: 6379
   ```

6. Start the add-on.

### Step 4 — First-boot onboarding

On first start the add-on **auto-generates a gateway token** and prints
it in the log:

```
────────────────────────────────────────────────────────────
  Gateway Token (use this to log in to the web dashboard):
  3e625fd87dbcb0b62b03bd534322e85f692e94920a2bca425cf3c1013dcd669f
────────────────────────────────────────────────────────────
```

Copy this token.

1. Open the dashboard at `http://<your-ha>:18790` (or click **Open Web UI**).
2. Enter **User ID**: `system`, **Gateway Token**: (paste the token).
3. Complete the onboarding wizard: configure an LLM provider, pick a model,
   name your agent.

The token is persisted to `/data/goclaw/gateway.token` and survives
add-on restarts. Set the `gateway_token` configuration option to override
it with your own value.

## Configuration reference

| Option | Default | Description |
|--------|---------|-------------|
| `postgres_host` | `a0d7b954-timescaledb` | TimescaleDB add-on hostname |
| `postgres_port` | `5432` | PostgreSQL port |
| `postgres_user` | `postgres` | PostgreSQL user |
| `postgres_password` | `homeassistant` | PostgreSQL password |
| `postgres_database` | `goclaw` | Database name (must exist in TimescaleDB) |
| `redis_host` | *(empty)* | Redis add-on hostname. Leave empty to skip. |
| `redis_port` | `6379` | Redis port |
| `enable_browser` | `true` | Start headless Chromium (~200MB RAM) |
| `gateway_token` | *(empty)* | Auto-generated on first boot if empty |
| `encryption_key` | *(empty)* | AES-256-GCM key for API key encryption (`openssl rand -hex 32`) |
| `log_level` | `info` | trace, debug, info, warn, error |
| `trace_verbose` | `false` | Verbose LLM call tracing |

See [`goclaw/DOCS.md`](./goclaw/DOCS.md) for the full configuration guide.

## Verification

After starting, run these checks from the Home Assistant host:

```bash
# Health endpoint
curl http://localhost:18790/health
# → {"status":"ok","protocol":3}

# Authenticated API call (replace TOKEN with your gateway token)
curl -H "Authorization: Bearer TOKEN" \
     -H "X-GoClaw-User-Id: system" \
     http://localhost:18790/v1/agents
# → {"agents":[...]}
```

## Local testing (for developers)

The repository includes a compose-based test harness that simulates the
TimescaleDB and Redis add-ons using vanilla Postgres and Redis containers.

```bash
# Start (Postgres + GoClaw, no Redis)
make ha-test

# Or with Redis profile
make ha-test-redis

# Tear down
make ha-test-down
```

Dashboard at `http://localhost:18790`. The gateway token is printed in the
compose logs on startup. Compose files are in `homeassistant/`:

- `docker-compose.test.yaml` — simulates the HA add-on environment
- `test-options.json` — stands in for `/data/options.json` (what the HA
  Supervisor would provide)

## Architecture notes

- The add-on extends the pre-built `ghcr.io/nextlevelbuilder/goclaw:latest`
  image, adding Chromium, `bash`, and `jq`. No Go or web compilation
  happens at install time.
- PostgreSQL migrations run automatically on each start (`goclaw upgrade`).
- The gateway token is generated once per data volume and survives add-on
  upgrades and restarts.
- Chromium runs as a subprocess inside the add-on container; GoClaw
  connects to it via `ws://localhost:9222`. The external port `9222` is
  disabled by default — enable it only for debugging.

## Troubleshooting

**Add-on won't start — "connection refused" on Postgres**
The TimescaleDB add-on isn't running or the hostname is wrong. Verify in
the TimescaleDB **Info** tab and copy the exact hostname.

**"database does not exist"**
Add `goclaw` to the `databases` list in TimescaleDB configuration, then
restart the TimescaleDB add-on.

**Login shows "Invalid credentials"**
Make sure you pasted the full 64-character token from the add-on log, with
no leading/trailing whitespace. If you've changed the `gateway_token`
configuration option, restart the add-on and use the new value.

**Chromium crashes / high memory**
Set `enable_browser: false` to disable the browser. Recommended on hosts
with less than 2GB RAM.

**pgvector not available**
GoClaw's vector memory (embeddings) requires pgvector. The TimescaleDB
add-on may not include it. To check, connect via `psql` and run
`CREATE EXTENSION IF NOT EXISTS vector;`. Non-vector features work
without pgvector.

## Links

- [GoClaw main repository](https://github.com/nextlevelbuilder/goclaw)
- [TimescaleDB add-on (expaso)](https://github.com/expaso/hassos-addons)
- [Add-on configuration reference](./goclaw/DOCS.md)
