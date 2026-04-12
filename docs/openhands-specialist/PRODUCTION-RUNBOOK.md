# OpenHands Specialist Production Runbook

## Services

- OpenHands adapter runtime: `linuxvm`
- Public endpoint: `https://openhands.blackbirdzzzz.art`
- GoClaw production: `ssh -p 44518 ubuntu@e1.chiasegpu.vn`

## Current production runtime

As of 2026-04-12, the live adapter is running with:

- `OPENHANDS_LLM_MODEL=openai/claude-opus-4.6`
- `OPENHANDS_LLM_BASE_URL=https://llm.chiasegpu.vn/v1`
- a gateway key that is entitled for `claude-opus-4.6`

Reason for this shape:

- the previous Anthropic-native style config failed with model entitlement errors
- OpenHands SDK works correctly here through the LiteLLM `openai/...` provider path
- the Linux VM should probe `http://127.0.0.1:8091/health` locally rather than relying on public DNS resolution inside the VM shell

## Required env for adapter

Create `/home/blackbird/services/openhands-adapter/deploy/.env` on `linuxvm` from [`services/openhands-adapter/deploy/.env.example`](/Users/nguyenquocthong/project/goclaw/services/openhands-adapter/deploy/.env.example), then set:

- `OPENHANDS_ADAPTER_AUTH_TOKEN`
- `OPENHANDS_LLM_MODEL`
- `OPENHANDS_LLM_API_KEY`
- `OPENHANDS_LLM_BASE_URL` when using an OpenAI-compatible gateway such as 9router
- `OPENHANDS_GITHUB_APP_ID` when OpenHands needs private GitHub repo access
- `OPENHANDS_GITHUB_APP_PRIVATE_KEY_B64` or `OPENHANDS_GITHUB_APP_PRIVATE_KEY_PATH`
- `OPENHANDS_GITHUB_APP_ALLOWED_OWNERS`

Notes:

- use GitHub App `App ID + private key` for server-to-server auth
- do not use the GitHub App `client secret` for this runtime path

## Deploy adapter

From this repo:

```bash
./scripts/deploy-openhands-adapter-linuxvm.sh
```

That script:

1. Syncs `services/openhands-adapter` to `linuxvm`
2. Builds and starts the Docker service
3. Ensures `openhands.blackbirdzzzz.art` points at `http://127.0.0.1:8091`
4. Verifies `/health`

## Deploy GoClaw overlay safely

Use the artifact-only deploy path so unrelated local changes do not get pushed:

```bash
./scripts/deploy-prod-overlay-artifacts.sh
```

That script only uploads:

- `.dist/linux-amd64/goclaw`
- `.dist/linux-amd64/pkg-helper`
- `Dockerfile.overlay`
- `docker-compose.prod.yml`
- `docker-entrypoint.sh`
- `docker/requirements-*.txt`
- `migrations/`
- `skills/`

## GoClaw env needed on production

Set these in `/home/ubuntu/services/goclaw/.env` on production:

- `GOCLAW_OPENHANDS_ENABLED=1`
- `GOCLAW_OPENHANDS_BASE_URL=https://openhands.blackbirdzzzz.art`
- `GOCLAW_OPENHANDS_BEARER_TOKEN=<same adapter token>`
- `GOCLAW_OPENHANDS_DEFAULT_WAIT_SEC=1800`
- `GOCLAW_OPENHANDS_MAX_WAIT_SEC=3600`
- `GOCLAW_OPENHANDS_DEFAULT_MAX_RUNTIME_SEC=1800`
- `GOCLAW_OPENHANDS_DEFAULT_MAX_ITERATIONS=150`
- `GOCLAW_GITHUB_APP_ID` when GoClaw agents need private GitHub repo access
- `GOCLAW_GITHUB_APP_PRIVATE_KEY_B64` or `GOCLAW_GITHUB_APP_PRIVATE_KEY_FILE`
- `GOCLAW_GITHUB_APP_ALLOWED_OWNERS`
- `GOCLAW_GITHUB_APP_DEFAULT_OWNER`

This enables:

- dynamic `git` auth through a credential helper
- dynamic `gh` auth through a wrapper that mints short-lived installation tokens per command

## Smoke checks

Adapter:

```bash
curl -fsS https://openhands.blackbirdzzzz.art/health
```

Linux VM local:

```bash
ssh linuxvm 'curl -fsS http://127.0.0.1:8091/health'
```

GoClaw:

```bash
curl -fsS https://goclaw.blackbirdzzzz.art/health
```

Then run a real request through the `coder` agent asking it to use `openhands_delegate`.

## Known-good production verification

Verified on 2026-04-12:

- adapter direct smoke job `d5006e8b-9be1-4c3b-9506-8ba2085ce22d`
  created `SMOKE_OPENHANDS.txt` in `octocat/Hello-World`
- GoClaw end-to-end smoke job `8f0da4d7-7c92-420c-978c-443f1a919f64`
  created `GOCLAW_PROBE.txt` through agent `openhands-coder`

The second check confirms the full chain:

`GoClaw -> openhands_delegate -> OpenHands adapter -> OpenHands workspace -> result back to GoClaw`
