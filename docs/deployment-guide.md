# Deployment Guide

## Overview

Production target uses a hybrid deployment:

- GoClaw gateway runs as a bare-metal `systemd` service.
- PostgreSQL 18 + pgvector runs in Docker.
- Nginx reverse proxies public HTTP/HTTPS traffic to GoClaw on localhost.
- Codex CLI is installed on the host for future agent-controlled CLI work.

Current VPS shape:

| Item | Value |
|---|---|
| Host | Private deployment value |
| SSH | Private deployment value |
| Domain | Private deployment value |
| Gateway listen | `127.0.0.1:18790` |
| Public proxy | Nginx on `80/443` |
| Postgres | Docker container `goclaw-postgres` |
| Postgres bind | `127.0.0.1:5432` |

Keep concrete hostnames, IPs, SSH users, SSH ports, and credentials in private operator notes or environment variables outside this public repository.

For command examples below:

```bash
export GOCLAW_HOST=<server-ip-or-hostname>
export GOCLAW_SSH_USER=<ssh-user>
export GOCLAW_SSH_PORT=<ssh-port>
export GOCLAW_DOMAIN=<public-domain>
```

## Server Layout

| Path | Purpose |
|---|---|
| `/opt/goclaw/releases/<release>` | Immutable GoClaw release directory |
| `/opt/goclaw/current` | Symlink to active release |
| `/opt/goclaw/shared/docker-compose.postgres.yml` | Postgres compose file |
| `/opt/goclaw/shared/postgres.env` | Postgres env file |
| `/opt/goclaw/backups/` | Uploaded DB backup files |
| `/etc/goclaw/config.json` | Gateway config |
| `/etc/goclaw/goclaw.env` | Gateway env and secrets |
| `/etc/goclaw/r2-backup.env` | R2 backup env and secrets |
| `/var/lib/goclaw/data` | GoClaw persistent data |
| `/var/lib/goclaw/workspace` | Agent workspace |
| `/var/lib/goclaw/postgres` | Postgres Docker data |
| `/usr/local/bin/goclaw-deploy` | Release switch, upgrade, health-check, rollback |
| `/usr/local/bin/goclaw-issue-ssl` | Certbot wrapper for the deployment domain |
| `/usr/local/bin/goclaw-backup-r2` | Postgres dump, R2 upload, retention cleanup |
| `/usr/local/bin/goclaw-upgrade-release` | Download and deploy a GitHub Release tarball |

Secrets are stored only in server env files. Do not copy tokens or database passwords into repo docs.

## Runtime Services

Check status:

```bash
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST"
systemctl status goclaw --no-pager
systemctl status nginx --no-pager
systemctl status goclaw-backup-r2.timer --no-pager
docker ps --filter name=goclaw-postgres
curl -fsS http://127.0.0.1:18790/health
```

Expected health:

```json
{"status":"ok","protocol":3}
```

## Initial Deployment Record

Completed on 2026-05-17:

- Installed Docker, Docker Compose v2, Nginx, Certbot, Node.js 22, Codex CLI.
- Added the operator workstation SSH public key to the deployment user.
- Installed Codex CLI. For agent-controlled `codex exec`, authenticate the Linux service user that runs GoClaw (`goclaw`), not only the SSH operator user.
- Restored the latest private PostgreSQL backup into Docker Postgres.
- Upgraded restored schema from `57` to `65`.
- Started the initial GoClaw release by `systemd`.
- Added automated database backup to a private Cloudflare R2 bucket.
- Verified local and Nginx health endpoints.

Current verification snapshot:

```text
goclaw_active=active
nginx_active=active
health_local={"status":"ok","protocol":3}
health_nginx={"status":"ok","protocol":3}
docker=goclaw-postgres pgvector/pgvector:pg18 healthy
schema=65
codex=codex-cli 0.130.0
```

## Codex CLI Auth For Agents

The gateway runs under the `goclaw` Linux user. Codex auth is stored under the
effective home directory, so an SSH-session login such as `codex login
--device-auth` under an operator user only writes that user's `~/.codex/auth.json`.
Agents invoking `codex` through the exec tool use the service user's home:

```text
/var/lib/goclaw/.codex/auth.json
```

Verify both contexts when debugging auth:

```bash
codex login status
sudo -u goclaw -H codex login status
```

If the operator user is logged in but `goclaw` is not, either run device auth as
the service user or copy the operator auth file with strict ownership:

```bash
sudo install -d -o goclaw -g goclaw -m 700 /var/lib/goclaw/.codex
sudo install -o goclaw -g goclaw -m 600 ~/.codex/auth.json /var/lib/goclaw/.codex/auth.json
sudo -u goclaw -H codex login status
sudo -u goclaw -H sh -lc 'mkdir -p /var/lib/goclaw/codex-smoke && cd /var/lib/goclaw/codex-smoke && codex exec --skip-git-repo-check --sandbox read-only "Reply with exactly: CODEX_AUTH_OK"'
```

Do not store Codex auth material in repository docs. Treat `auth.json` as a
credential.

## DNS And SSL

Cloudflare DNS record:

| Type | Name | Value | Proxy |
|---|---|---|---|
| `A` | Deployment subdomain | Deployment host IP | Proxied |

SSL was issued with Certbot for the deployment domain on 2026-05-17 and Certbot installed automatic renewal.

Verify HTTPS:

```bash
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST"
curl -fsS "https://$GOCLAW_DOMAIN/health"
```

Re-issue manually if needed:

```bash
sudo /usr/local/bin/goclaw-issue-ssl
```

## Deploy A New Release

Preferred server-side upgrade flow:

```bash
sudo /usr/local/bin/goclaw-upgrade-release --dry-run latest
sudo /usr/local/bin/goclaw-upgrade-release latest
sudo /usr/local/bin/goclaw-upgrade-release v3.12.0
```

The script downloads the Linux amd64 GitHub Release tarball from `digitopvn/goclaw`, follows GitHub release redirects, verifies `CHECKSUMS.sha256` when present, falls back to the GitHub release asset SHA256 digest for beta assets without checksum files, extracts to `/opt/goclaw/releases/<tag>`, and calls `goclaw-deploy`. When invoked from the running gateway service, it first re-launches itself as a transient `systemd-run` unit so `goclaw-deploy` can stop/restart `goclaw` without killing the upgrade job.

The HTTP API still accepts only `tag`; it does not accept repo names or custom download URLs.

Remote API trigger is available in builds that include the gateway upgrade endpoint:

```bash
curl -fsS -X POST "https://$GOCLAW_DOMAIN/v1/system/gateway/upgrade" \
  -H "Authorization: Bearer <gateway-token>" \
  -H "X-GoClaw-Upgrade-Token: <upgrade-token>" \
  -H "Content-Type: application/json" \
  --data '{"tag":"latest"}'
```

Check status:

```bash
curl -fsS "https://$GOCLAW_DOMAIN/v1/system/gateway/upgrade/status" \
  -H "Authorization: Bearer <gateway-token>" \
  -H "X-GoClaw-Upgrade-Token: <upgrade-token>"
```

Keep upgrade tokens in server env files or secret managers. Do not put real tokens in docs.

The remote trigger endpoint fails closed unless `GOCLAW_UPGRADE_TRIGGER_TOKEN` is configured in the gateway environment.

### Automatic Beta Deploy From `dev`

Pushing or merging into `dev` runs `.github/workflows/dev-beta-release.yaml`. After Go/Web checks pass, the workflow creates the next semantic beta tag, publishes the prerelease assets, promotes beta Docker aliases, then deploys that exact beta tag to the zuey VPS through the gateway upgrade endpoint.

Required GitHub Actions configuration:

| Name | Type | Value |
|---|---|---|
| `ZUEY_GOCLAW_URL` | Secret | Public gateway URL, for example `https://goclaw.zuey.me` |
| `ZUEY_GOCLAW_GATEWAY_TOKEN` | Secret | Gateway bearer token from the server env |
| `ZUEY_GOCLAW_UPGRADE_TOKEN` | Secret | Upgrade trigger token from the server env |
| `ZUEY_GOCLAW_USER_ID` | Variable | Optional owner identity, defaults to `system` |

The deploy job sends:

```bash
POST /v1/system/gateway/upgrade {"tag":"vX.Y.Z-beta.N"}
GET /v1/system/gateway/upgrade/status
GET /health
```

The workflow fails if the upgrade status becomes `failed`, times out, or public health does not return `{"status":"ok"}`.

Manual local-build fallback:

Build locally with embedded web UI:

```bash
cd ui/web
pnpm install --frozen-lockfile
pnpm build
cd ../..
rm -rf internal/webui/dist
mkdir -p internal/webui/dist
cp -r ui/web/dist/* internal/webui/dist/
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -tags embedui \
  -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=<version>" \
  -o dist/goclaw-linux-amd64 .
```

Upload:

```bash
release=<version-or-date>
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST" "mkdir -p /opt/goclaw/releases/$release/migrations"
scp -P "$GOCLAW_SSH_PORT" dist/goclaw-linux-amd64 "$GOCLAW_SSH_USER@$GOCLAW_HOST:/opt/goclaw/releases/$release/goclaw"
scp -P "$GOCLAW_SSH_PORT" migrations/* "$GOCLAW_SSH_USER@$GOCLAW_HOST:/opt/goclaw/releases/$release/migrations/"
ssh -p "$GOCLAW_SSH_PORT" "$GOCLAW_SSH_USER@$GOCLAW_HOST" "chmod +x /opt/goclaw/releases/$release/goclaw && sudo /usr/local/bin/goclaw-deploy /opt/goclaw/releases/$release"
```

`goclaw-deploy` does:

1. Validate binary and migrations.
2. Switch `/opt/goclaw/current`.
3. Run `goclaw upgrade`.
4. Restart `goclaw`.
5. Poll `/health`.
6. Roll back symlink and restart if health fails.

## Backup And Restore

Automated backups:

| Item | Value |
|---|---|
| Timer | `goclaw-backup-r2.timer` |
| Schedule | Every 6 hours: `00:00`, `06:00`, `12:00`, `18:00` server time |
| Source | Docker Postgres container `goclaw-postgres` |
| Format | PostgreSQL custom dump, `pg_dump -Fc` |
| Local directory | `/opt/goclaw/backups/` |
| R2 bucket | Private deployment value |
| R2 prefix | Private deployment value |
| Retention | Keep latest 20 backups locally and in R2 |

Check timer and latest logs:

```bash
systemctl list-timers goclaw-backup-r2.timer --no-pager
journalctl -u goclaw-backup-r2.service -n 80 --no-pager
```

Run a manual backup:

```bash
sudo systemctl start goclaw-backup-r2.service
```

Create a database dump on server:

```bash
ts=$(date +%Y%m%d-%H%M%S)
docker exec goclaw-postgres pg_dump -U goclaw -Fc -d goclaw > /opt/goclaw/backups/goclaw-$ts.dump
```

Restore a dump:

```bash
systemctl stop goclaw
docker exec -i goclaw-postgres pg_restore -U goclaw -d goclaw --clean --if-exists --no-owner < /opt/goclaw/backups/<file>.dump
sudo /usr/local/bin/goclaw-deploy /opt/goclaw/current
```

## Operational Notes

- Gateway runs as Linux user `goclaw`.
- Host-control exceptions are deployment-specific and must be documented privately, not in this public runbook.
- Workspace restriction settings are deployment-specific and must be reviewed before enabling agent-controlled host operations.
- Postgres is bound only to localhost.
- UFW allows the private SSH port, `80/tcp`, and `443/tcp`.
- Reboot is recommended later because the VPS reports a pending kernel upgrade.

## Troubleshooting

Logs:

```bash
journalctl -u goclaw -n 200 --no-pager
docker logs goclaw-postgres --tail 100
tail -n 100 /var/log/nginx/error.log
```

Restart:

```bash
systemctl restart goclaw
docker compose --env-file /opt/goclaw/shared/postgres.env -f /opt/goclaw/shared/docker-compose.postgres.yml restart postgres
systemctl reload nginx
```

Unresolved questions: none.
