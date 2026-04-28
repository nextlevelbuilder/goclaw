# Deploying GoClaw on TOSE

TOSE can deploy GoClaw from a GitHub repository. GoClaw is a Dockerized Go
service with an embedded React dashboard and listens on port `18790`.

## Requirements

- GitHub repository connected to the TOSE GitHub App.
- PostgreSQL database with `pgcrypto` and `pgvector` support.
- Project port set to `18790`.
- Recommended resources: `2` CPU, `4` GB RAM, `1` replica.

GoClaw migrations require:

```sql
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";
```

If TOSE managed PostgreSQL does not support `pgvector`, use an external
PostgreSQL provider that does, then set `GOCLAW_POSTGRES_DSN` to that database.

## Environment Variables

Set these variables in TOSE before deploying:

```env
GOCLAW_HOST=0.0.0.0
GOCLAW_PORT=18790
GOCLAW_CONFIG=/app/data/config.json
GOCLAW_SKILLS_DIR=/app/data/skills
GOCLAW_POSTGRES_DSN=postgresql://USER:PASSWORD@HOST:5432/DB?sslmode=require
GOCLAW_GATEWAY_TOKEN=replace-with-random-token
GOCLAW_ENCRYPTION_KEY=replace-with-64-char-hex-key
```

Optional provider/channel keys can be configured later from the dashboard or as
TOSE environment variables.

## CLI Flow

```bash
npm install -g @tosesh/tose
tose login
tose init
tose env set GOCLAW_HOST=0.0.0.0 GOCLAW_PORT=18790 GOCLAW_CONFIG=/app/data/config.json GOCLAW_SKILLS_DIR=/app/data/skills
tose env set GOCLAW_POSTGRES_DSN='postgresql://USER:PASSWORD@HOST:5432/DB?sslmode=require'
tose env set GOCLAW_GATEWAY_TOKEN='replace-with-random-token'
tose env set GOCLAW_ENCRYPTION_KEY='replace-with-64-char-hex-key'
tose deploy
tose logs --build -f
```

Generate production secrets locally, then paste them into TOSE:

```bash
openssl rand -hex 32  # GOCLAW_ENCRYPTION_KEY: 64 hex chars
openssl rand -hex 32  # GOCLAW_GATEWAY_TOKEN
```

After the first deployment, verify:

```bash
curl https://YOUR-TOSE-APP/health
```

The expected response contains `"status":"ok"`.

## Notes

- The Dockerfile defaults are set for TOSE: embedded web UI, Python runtime, and
  Node.js runtime are enabled by default.
- Do not commit local `.env`, `data/`, `backups/`, or `goclaw-volumes*.tar.gz`.
- If the dashboard loads but login/API calls fail, confirm that the project port
  is `18790` and that the TOSE deployment is forwarding to that port.
