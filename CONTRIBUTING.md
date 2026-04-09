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
- **Tenant isolation**: All queries scoped by `tenant_id`
- **i18n**: User-facing strings in all 3 locales (en/vi/zh)
- **SQLite parity**: Changes compile with `-tags sqliteonly`
- **Mobile UI**: `h-dvh` not `h-screen`, 16px input fonts, safe areas

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
