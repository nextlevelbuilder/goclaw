# Contributing to GoClaw

## Branch Strategy

```
main (stable, protected — owner-only merge)
  └── dev (default target for all PRs)
        ├── feat/xxx
        ├── fix/xxx
        └── ...
```

### Rules

1. **All PRs target `dev`** — `main` is frozen for stable releases
2. **Hotfixes** — PR to `main`, then cherry-pick to `dev`
3. **Releases** — owner merges `dev` → `main` when stable
4. **Direct push to `main`** — blocked (ruleset enforced)

### Branch Naming

- `feat/description` — new features
- `fix/description` — bug fixes
- `hotfix/description` — urgent production fixes (target `main`)
- `refactor/description` — code improvements
- `docs/description` — documentation changes

## PR Guidelines

### Before Submitting

```bash
go fix ./...                        # Apply Go upgrades
go build ./...                      # PG build check
go build -tags sqliteonly ./...     # Desktop build check
go vet ./...                        # Static analysis
go test -race ./...                 # Tests with race detector
```

For web UI changes:

```bash
cd ui/web && pnpm build
```

### PR Review Criteria

Based on our automated review checklist:

- **Correctness**: No logic errors, nil dereference, race conditions
- **Security**: Parameterized SQL, no hardcoded secrets, input validation
- **Breaking changes**: API contracts, DB migrations, config format
- **Tenant isolation**: All queries scoped by `tenant_id`. **Admin writes require the correct scope guard** — see section below
- **i18n**: User-facing strings in all 3 locales (en/vi/zh)
- **SQLite parity**: Changes compile with `-tags sqliteonly`
- **Mobile UI**: `h-dvh` not `h-screen`, 16px input fonts, safe areas

### Tenant-Scope Guards

`RoleAdmin` checks role, not tenant. A non-master tenant admin holds `RoleAdmin` in their own tenant and passes role-only middleware. Pick the guard by the **target table**:

| Target | Example | Guard |
|---|---|---|
| **Global** (no `tenant_id` column) | `builtin_tools`, disk config, `pip`/`npm`/`apk` | HTTP `requireMasterScope` · WS `requireMasterScope(requireOwner(...))` |
| **Tenant-scoped** (has `tenant_id` column) | `agents`, `skills`, `llm_providers` | `requireTenantAdmin` + store SQL `WHERE tenant_id = $N` |

Shared predicate: `store.IsMasterScope(ctx)` (`internal/store/context.go`).

**Anti-patterns flagged in review:**
- `store.Update(...)` on a no-`tenant_id` table without a master-scope check upstream
- Write SQL with `WHERE ... (tenant_id = $N OR tenant_id IS NULL)` — the `IS NULL` arm lets tenants reach system rows
- `requireAuth(RoleAdmin)` as the **sole** gate on a global-state write
- Admin revoke/delete handlers that skip pre-fetch ownership verification (store SQL alone is not enough when it matches `IS NULL` arms)

## Test Layers

Tests are organized by priority and purpose:

| Layer | Priority | Location | Blocking? | Purpose |
|-------|----------|----------|-----------|---------|
| **Invariants** | P0 | `tests/invariants/` | YES | Tenant isolation, permission enforcement |
| **Contracts** | P1 | `tests/contracts/` | YES | API schema validation |
| **Scenarios** | P2 | `tests/scenarios/` | NO | End-to-end user journeys |
| **Integration** | P1 | `tests/integration/` | YES | DB/pipeline integration |

### Running Tests

```bash
make test              # Unit tests (fast, no DB)
make test-invariants   # P0 invariants (requires pgvector)
make test-contracts    # P1 API contracts (requires server)
make test-scenarios    # P2 scenarios (requires server)
make test-critical     # P0 + P1 (run before merge)
```

### Test Layer Policy

- **P0 failures**: Block PR merge immediately
- **P1 failures**: Block merge, investigate contract breakage
- **P2 failures**: Warning only, may indicate flaky tests or environment issues

When adding new tests:
- Tenant isolation/permissions → `tests/invariants/`
- API response schemas → `tests/contracts/`
- User journeys (multi-step flows) → `tests/scenarios/`

### Commit Messages

Use conventional commits:

```
feat: add user preferences API
fix: prevent race condition in session cleanup
docs: update API reference for v2 endpoints
refactor: extract provider retry logic
```

## Workflow

```
Developer                    Reviewer                 Owner
    │                            │                      │
    ├─ create feat/xxx ──────────┤                      │
    ├─ PR → dev ─────────────────┤                      │
    │                            ├─ review + approve    │
    │                            ├─ CI passes ──────────┤
    │                            │                      ├─ merge to dev
    │                            │                      │
    │                            │        (when stable) ├─ PR dev → main
    │                            │                      ├─ merge → auto release
    │                            │                      │  (semantic-release)
```

## Releases

### Standard (automatic)

Merge `dev` → `main`. `go-semantic-release` analyzes commit messages and auto-creates:
- GitHub Release with version tag (`vX.Y.Z`)
- Cross-platform binaries (linux/darwin × amd64/arm64)
- Docker images (4 variants: latest, base, full, otel + web)
- SHA256 checksums
- Discord notification

### Beta (manual tag)

Push a beta tag from `dev` to create a prerelease:

```bash
# Standard beta — builds Docker + Linux binaries
git tag v2.67.0-beta.1
git push origin v2.67.0-beta.1

# Desktop beta — builds macOS .dmg + Windows .exe
git tag lite-v1.2.0-beta.1
git push origin lite-v1.2.0-beta.1
```

Beta releases are marked as **prerelease** on GitHub and use `:beta` rolling Docker tag.

### Desktop / Lite

Push a `lite-v*` tag to build desktop apps:

```bash
git tag lite-v1.1.0
git push origin lite-v1.1.0
```

Tags with `-beta` or `-rc` suffix automatically create prereleases.
