#!/usr/bin/env bash
# Canonical source for /usr/local/bin/goclaw-deploy on the zuey VPS.
#
# This repo is the source of truth; the VPS copy is a downstream replica.
# Auto-synced on every beta release by the `Sync zuey ops scripts to VPS`
# step in `.github/workflows/dev-beta-release.yaml`. See
# `docs/deployment-guide.md` for the manual sync recipe and required
# GitHub secrets.
#
# Self-loop guard (2026-05-27 incident): when `/opt/goclaw/current` is a
# self-referential symlink (current -> current), `readlink -f` exits non-zero.
# With `set -euo pipefail`, that aborts the script before `ln -sfn` can fix
# the symlink — causing silent CI deploy failures. The guard below swallows
# the readlink failure and logs a warning when the symlink resolves to empty,
# so the deploy still completes (without a rollback target).
set -euo pipefail
release_dir="${1:?usage: goclaw-deploy /opt/goclaw/releases/<release-dir>}"
if [ ! -x "$release_dir/goclaw" ]; then echo "missing executable: $release_dir/goclaw" >&2; exit 2; fi
if [ ! -d "$release_dir/migrations" ]; then echo "missing migrations dir" >&2; exit 2; fi
previous=""
if [ -L /opt/goclaw/current ]; then previous="$(readlink -f /opt/goclaw/current 2>/dev/null || true)"; fi
if [ -z "$previous" ] && [ -L /opt/goclaw/current ]; then
  echo "warn: /opt/goclaw/current is a symlink but readlink -f failed (likely self-loop or broken target); overwriting without rollback target" >&2
fi
ln -sfn "$release_dir" /opt/goclaw/current
chown -h goclaw:goclaw /opt/goclaw/current || true
chown -R root:root "$release_dir"
chmod 755 "$release_dir/goclaw"
set -a
. /etc/goclaw/goclaw.env
set +a
systemctl daemon-reload
if systemctl is-active --quiet goclaw; then systemctl stop goclaw; fi
/opt/goclaw/current/goclaw upgrade
systemctl start goclaw
for i in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:18790/health >/tmp/goclaw-health.json; then
    cat /tmp/goclaw-health.json
    echo
    exit 0
  fi
  sleep 2
done
echo "health check failed" >&2
journalctl -u goclaw -n 120 --no-pager >&2 || true
if [ -n "$previous" ] && [ -d "$previous" ]; then
  echo "rolling back to $previous" >&2
  ln -sfn "$previous" /opt/goclaw/current
  systemctl restart goclaw || true
fi
exit 1
