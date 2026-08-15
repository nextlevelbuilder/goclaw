## Summary
<!-- 1-2 sentences: What does this PR do and why? -->

## Type
<!-- Check one -->
- [ ] Feature
- [ ] Bug fix
- [ ] Hotfix (targeting `main`)
- [ ] Refactor
- [ ] Docs
- [ ] CI/CD

## Target Branch
<!--
  ⚠️  IMPORTANT: Read before submitting!

  - Features/bugfixes → `dev` (default)
  - Hotfixes only → `main` (cherry-pick back to `dev` after merge)
  - DO NOT target `main` for regular development
-->

## Checklist
- [ ] `go build ./...` passes
- [ ] `go build -tags sqliteonly ./...` passes (if Go changes)
- [ ] `go vet ./...` passes
- [ ] Tests pass: `go test -race ./...`
- [ ] Web UI builds: `cd ui/web && pnpm build` (if UI changes)
- [ ] No hardcoded secrets or credentials
- [ ] SQL queries use parameterized `$1, $2` (no string concat)
- [ ] New user-facing strings added to all 3 locales (en/vi/zh)
- [ ] Migration version bumped in `internal/upgrade/version.go` (if new migration)

## Test Plan
<!-- How was this tested? -->
