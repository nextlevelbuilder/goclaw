#!/usr/bin/env bash
#
# deploy-forkdev.sh — build & run goclaw from a fresh "fork-dev" integration
# branch, independent of any published image or your current checkout.
#
# What it does, every run (idempotent):
#   1. Fetch the freshest upstream dev (origin/dev).
#   2. Rebuild integration branch `local/fork-dev` = origin/dev, in a dedicated
#      worktree OUTSIDE this repo (so your main checkout / git status stay clean).
#   3. Merge the un-merged fork features into it:
#        - feat/telegram-wake-words  (Telegram trigger-words + edit/topic)
#        - the Russian-locale delta   (whatever local `dev` has over upstream)
#   4. (optional) Push the result to YOUR fork's dev branch so you can see both
#      features in skensell201/goclaw:dev  (PUSH_FORK=1, on by default).
#   5. Build a local image `goclaw:forkdev` from that merged tree.
#   6. Bring the stack up with that image.
#
# Re-run any time to pull fresh upstream + re-merge + redeploy. On a merge
# conflict it stops and tells you which worktree to resolve in.
#
# Env knobs:
#   PUSH_FORK=0        skip pushing to your fork's dev (default: 1 = push)
#   FEATURE_BRANCHES   space-separated feature branches to merge
#                      (default: "feat/telegram-wake-words")
#   RU_FROM            ref carrying the ru-locale work (default: "dev" = local dev)
#   WT_DIR             integration worktree path (default: ../.goclaw-forkdev)
#   NO_BUILD=1         skip docker build/up (just prepare + push the branch)
#
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

INT_BRANCH="local/fork-dev"
FORK_REMOTE="fork"
UPSTREAM_REMOTE="origin"
WT_DIR="${WT_DIR:-${ROOT}/../.goclaw-forkdev}"
# Normalize to an absolute, ..-free path so the worktree-exists check matches
# `git worktree list --porcelain` output (which is always absolute + resolved).
WT_DIR="$(cd "$(dirname "${WT_DIR}")" && pwd)/$(basename "${WT_DIR}")"
FEATURE_BRANCHES="${FEATURE_BRANCHES:-feat/telegram-wake-words}"
RU_FROM="${RU_FROM:-dev}"
PUSH_FORK="${PUSH_FORK:-1}"

COMPOSE=(-f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.forkdev.yml)

say() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }
die() { printf '\n\033[1;31mERROR: %s\033[0m\n' "$*" >&2; exit 1; }

say "Fetching upstream ${UPSTREAM_REMOTE}/dev"
git fetch "${UPSTREAM_REMOTE}" dev

# ---- (re)create integration worktree pinned to fresh upstream dev ----------
# The integration branch is checked out INSIDE the worktree, so it must be moved
# via `reset --hard` there — `git branch -f` refuses to move a checked-out branch.
say "Pinning integration branch ${INT_BRANCH} to ${UPSTREAM_REMOTE}/dev"
if git worktree list --porcelain | grep -qx "worktree ${WT_DIR}"; then
  git -C "${WT_DIR}" checkout -q "${INT_BRANCH}" 2>/dev/null || true
  git -C "${WT_DIR}" reset --hard "${UPSTREAM_REMOTE}/dev"
  git -C "${WT_DIR}" clean -fdx >/dev/null 2>&1 || true
else
  git branch -f "${INT_BRANCH}" "${UPSTREAM_REMOTE}/dev"
  git worktree add -f "${WT_DIR}" "${INT_BRANCH}"
fi

merge_ref() {
  local ref="$1" label="$2"
  say "Merging ${label} (${ref})"
  git -C "${WT_DIR}" log --oneline "HEAD..${ref}" | sed 's/^/    /' || true
  if ! git -C "${WT_DIR}" merge --no-edit "${ref}"; then
    git -C "${WT_DIR}" merge --abort 2>/dev/null || true
    die "Conflict merging ${label} (${ref}). Resolve manually:
      cd ${WT_DIR}
      git merge ${ref}      # fix conflicts, git add, git commit
    then re-run this script (it will pick up the resolved branch)."
  fi
}

# ---- merge the un-merged fork features -------------------------------------
for b in ${FEATURE_BRANCHES}; do
  merge_ref "${b}" "feature branch ${b}"
done
# ru locale = whatever local dev carries over upstream dev (no-op if already upstream)
merge_ref "${RU_FROM}" "ru-locale delta"

say "Integration branch ready:"
git -C "${WT_DIR}" log --oneline -8 | sed 's/^/    /'

# ---- publish to your fork's dev so both features are visible there ---------
if [ "${PUSH_FORK}" = "1" ]; then
  say "Pushing ${INT_BRANCH} -> ${FORK_REMOTE}/dev (your fork)"
  git push --force-with-lease "${FORK_REMOTE}" "${INT_BRANCH}:dev"
else
  say "PUSH_FORK=0 — skipping fork push"
fi

if [ "${NO_BUILD:-0}" = "1" ]; then
  say "NO_BUILD=1 — branch prepared & pushed; skipping docker build/up"
  exit 0
fi

# ---- build local image from the merged tree & deploy -----------------------
say "Building goclaw:forkdev from ${WT_DIR}"
GOCLAW_DIR="${WT_DIR}" docker compose "${COMPOSE[@]}" build goclaw

say "Deploying"
GOCLAW_DIR="${WT_DIR}" docker compose "${COMPOSE[@]}" up -d

say "Done. Follow logs with:"
printf '    docker compose %s logs -f goclaw\n' "${COMPOSE[*]}"
