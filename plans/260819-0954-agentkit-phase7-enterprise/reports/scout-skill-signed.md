# Scout: Skill Registry + Signed Packages (Phase 7 Enterprise gap analysis)

Date: 2026-08-19
Scope: read-only investigation of GoClaw skill storage/management, publish flow, and any package signature/trust infrastructure.

## Status

Status: DONE
Summary: GoClaw already has a DB-backed skill registry with versioning, ownership, visibility, grants, self-evolution and an HTTP/WS/admin surface — but zero approval workflow, zero curation, and zero package signing (only SHA-256 content hashes + GitHub SHA256SUMS checksums for downloads). Signed packages / trust is a net-new build.
Concerns: none blocking.

---

## 1. Skill registry — existing state

### Storage model: hybrid DB-backed catalog + versioned filesystem

- **DB table `skills`** holds the authoritative catalog: `id, name, slug (UNIQUE), description, owner_id, visibility, version, status, frontmatter, file_path, file_size, file_hash, embedding, tags` — `migrations/000001_init_schema.up.sql:215-238`. Indexes on owner, visibility, slug, embedding (HNSW), tags (`:234-238`).
- **`skill_agent_grants`** (`:240-248`) + **`skill_user_grants`** (`:252-259`): per-agent and per-user access grants with `pinned_version`.
- **`skill_tenant_configs`** — `migrations/000027_tenant_foundation.up.sql:254`.
- **`skill_versions`** — append-only version/audit table (`migrations/000079_skill_self_evolution.up.sql:65-77`) with `content_hash`, `changed_files`, `created_by_actor_*`, backfilled from `skills` (`:81-89`). Used only by the self-evolution store (`internal/store/skill_evolution_store.go:122`, `internal/store/pg/skill_evolution.go:351,368,399`).
- **SQLite parity** — same tables in `internal/store/sqlitestore/schema.sql:371-449` and `:2320-2392` (dual-DB maintained).

### Skill kinds

1. **Bundled/system skills** — shipped in repo `skills/` (17 SKILL.md dirs), seeded into DB + managed dir by `internal/skills/seeder.go` (`Seed` at `:55`, copy at `:150-160`). Build-time source: `/app/bundled-skills/` in Docker, `skills/` in dev (`cmd/gateway_setup.go:573-600`). Read-only via API (`internal/http/skills_versions.go:306-309` rejects writes to system skills).
2. **User/agent-created managed skills** — versioned dirs `<dataDir>/skills-store/<slug>/<version>/SKILL.md` (`internal/skills/loader.go:70-71`; tenant-scoped via `config.TenantSkillsStoreDir`, `internal/tools/skill_manage.go:78-82`).
3. **Local filesystem skills** — 5-tier loader hierarchy (workspace → project `.agents/skills` → `~/.agents/skills` → `~/.goclaw/skills` → builtin) (`internal/skills/loader.go:4-9, 59-81`). These are NOT DB-backed.

### Publish flow (exists, self-serve, no approval)

- **`publish_skill` tool** (`internal/tools/publish_skill.go:42-209`): registers a skill directory → computes SHA-256 (`:120-122`) + dir size, gets next version, copies into tenant skills-store, inserts DB row (`CreateSkillManaged`, `:163`), auto-grants to calling agent (`:173-176`).
- **`skill_manage` tool** (`internal/tools/skill_manage.go:84-529`): agent-driven create/patch/delete from content strings. Patch creates a new immutable version (`GetNextVersionLocked`, `:367`), computes SHA-256 (`:436-438`), repoints DB row (`:449-461`). Delete soft-archives to `.trash/` + DB archive (`:498-520`).
- **HTTP admin surface** — `internal/http/skills.go:90-146`: upload ZIP (`POST /v1/skills/upload`, 20MB default), update/delete/toggle, tenant-config, access grants (agent+user), dependencies, export/import, evolution settings. Version/file read/write at `internal/http/skills_versions.go` (write creates new immutable version, `:275-340`).
- **WS methods** — `skills.list`, `skills.get`, `skills.update` (`internal/gateway/methods/skills.go:34-38`). Update has ownership check + visibility enum validation (`:241-268`).
- **Registration gating** — `skill_manage`/`publish_skill` are registered only when `edition.Current().TeamFullMode` (standard) with a PG store (`cmd/gateway_setup.go:645-657`). **Lite blocks them**: hidden from builtin tool defs (`cmd/gateway_builtin_tools.go:145-154`) and not registered in setup. Both are in the `goclaw` tool group (`internal/tools/policy.go:38`).

### Discovery / search

- BM25 index (`internal/skills/search.go:44-230`) + optional hybrid embedding search via `EmbeddingSkillSearcher` (`internal/store/skill_store.go:81-85`). `skill_search` tool registered standard-only in PG path (`cmd/gateway_setup.go:660-669`).

### Versioning / approval gap in today's model

- Versioning = single `skills.version INT` + `skill_versions` audit table + on-disk `vN/` dirs. **No semver, no changelog, no per-version download/install surface for end users.**
- **No approval/curation workflow anywhere.** The only "approve" flows in the codebase are unrelated: skill self-evolution suggestion approve (`internal/http/skills.go:109-111`), exec approval, device pairing, WhatsApp opt-in. Publishing a skill (`publish_skill`, `skill_manage`, ZIP upload) immediately makes it live/discoverable — no review gate.
- Visibility = `private`/`public` per-tenant only (`internal/skills/visibility.go`); **no "published/catalog/approved" status tier** — `skills.status` is only `active/archived/deleted` (`store.SkillCreateParams.Status` comment, `internal/store/skill_store.go:95`).

---

## 2. Signed packages — existing state (≈ zero)

- **No public-key signing, no PKI, no trust concept for packages.** Grep for `crypto/ed25519`, `crypto/rsa`, `crypto/x509`, `ed25519.Sign/Verify` across the repo returns only *test* files using ed25519 for SSH keypair fixtures (`internal/http/secure_cli_typed_credentials_test.go:449,464`, `internal/tools/credential_adapter_git_ssh_test.go:27,43`, `tests/integration/git_adapter_ssh_test.go:42`). Nothing signs or verifies a package artifact.
- `internal/crypto/` is symmetric-only: AES-256-GCM `Encrypt/Decrypt` (`internal/crypto/aes.go:20-53`) for API-key/secret storage + `HashAPIKey`/`GenerateAPIKey` (`internal/crypto/apikey.go:14-27`). No asymmetric primitives.
- **What exists instead:**
  - SHA-256 **content hashes** recorded per skill on create/patch/publish (`internal/tools/skill_manage.go:231-233,436-438`, `internal/tools/publish_skill.go:120-122`), stored in `skills.file_hash` and `skill_versions.content_hash`. These are integrity records, not authenticity — anyone can compute them; they prove nothing about origin.
  - GitHub release **checksum verification** for the github-binaries package installer: parses `.sha256` / `checksums.txt` and rejects mismatches (`internal/skills/github_checksum.go:12-69`). This validates download integrity, not publisher identity (checksums come from the same untrusted release).
  - **KitManager checksum** — `internal/skills/kit_manager.go:16-26,120-213`: kit.yaml with optional `checksum`; `VerifyChecksum` compares a runtime-computed digest against the manifest-pinned value (`:143-162`). Self-referential digest, no key material — catches accidental drift, not tampering.
  - HMAC-SHA256 webhook signing (shared-secret, `internal/webhooks/sign.go`) — for outbound webhooks, unrelated to package trust.
- **No key store, no keyring, no "trusted publisher" registry, no signature column** on any package/skill/agent/plugin table.

---

## 3. Gaps vs Enterprise "skill registry + signed packages"

### Already exists (build on, don't rebuild)

- DB-backed skill catalog (skills, grants, tenant configs, versions, embeddings) — both PG + SQLite.
- Versioned storage model (int version + immutable vN dirs + `skill_versions` audit).
- Ownership, RBAC (admin/owner), per-tenant visibility, agent/user grants, pinning.
- Upload/import/export, dependency scanning, self-evolution suggestion pipeline.
- SHA-256 content hashes as an integrity baseline.
- Full cross-surface wiring already present: WS methods, HTTP API, tools, UI (`ui/web` skill screens), CLI (`cmd/skills_cmd.go`, `cmd/skills_evolution_cmd.go`).

### Must be BUILT (Enterprise gaps)

1. **Approval/curation workflow** — no draft/review/publish status lifecycle; publish is instant self-serve.
2. **Real package format + distribution** — no signed/checksummed archive format for skills/agents/plugins; no central or tenant catalog index with `install`/`uninstall` verbs (only grants/pins).
3. **Public-key signing + verification** — no asymmetric key material, no signature column, no verification at load/install/import time. Trusted-publisher model absent.
4. **Trust/anchor store** — no keyring or trust anchor for "who may publish"; no verification failure handling policy.
5. **Semver / release channels** — versions are opaque ints; no stable/candidate channels for curated rollouts.

### Local-only reality to note

- `.claude/` is git-ignored (`.gitignore:64`) — `git check-ignore` confirms `.claude/skills`. The Claude Code skills kit (`.claude/skills`, the `ak-*`/`ck-*` catalog) is **local-only, never in the DB, never distributed**. The GoClaw bundled `skills/` dir (shipped in Docker image) is the only distribution path today, via seeder (`internal/skills/seeder.go`). So the "skills registry" that users actually run with is split: DB-managed GoClaw skills vs untracked local `.claude/skills` — an Enterprise registry must decide whether to fold `.claude/skills` into a distributed, signed format.

---

## Suggested approach per gap

| Gap | Approach |
|---|---|
| Approval/curation workflow | Extend `skills.status` enum (`draft → pending_review → approved → published` + `rejected`/`suspended`) with a `reviewed_by_actor_*` / `reviewed_at` audit columns mirroring `skill_improvement_suggestions` (`internal/store/pg/skill_evolution.go` pattern). Add admin approve/reject WS+HTTP methods (`skills.approve`/`skills.reject`), gate discovery/search on `status='published'` (already partial: `idx_skills_visibility ... WHERE status='active'`, `migrations/000001_init_schema.up.sql:235`). |
| Package format + distribution | Define a ZIP/tar package manifest (slug, semver, deps, checksums per file, SKILL.md) building on the existing ZIP upload (`internal/http/skills.go:104`) and `skill_versions` audit table. Add `install`/`uninstall`/`list` verbs (DB-backed install records per tenant/agent) — today only grants exist. |
| Public-key signing + verification | Add ed25519 signing layer in `internal/crypto` (keepsymmetric AES for secrets; add `internal/crypto/signature.go`). Signer key per publisher; signature column on `skills`/`skill_versions` + package manifest; verify at import/upload/install and on load (hook into `internal/skills/loader.go` + `archive_extract.go`). Reuse the SSRF/trust-anchor config patterns (`internal/security/ssrf.go`, `internal/config/config_load.go:351`) for a keyring config. |
| Trust/anchor store | New table `publisher_keys` (publisher_id, public_key, fingerprint, status, created_at) dual-DB (PG migration + `internal/store/sqlitestore/schema.sql` + schema.go patch, per CLAUDE.md dual-DB rule). Verification failure = hard reject for curated/published channel, warn for user-managed. |
| Semver / channels | Either replace int `version` with a `(major, minor, patch)` or add a `release_channel` column alongside; keep int for existing disk dirs. |

Dual-DB note: every schema change must be mirrored (PG `migrations/` + SQLite `schema.sql` + `schema.go` migrations map + version bumps), per project convention. i18n: new user-facing messages need keys in `internal/i18n/keys.go` + 3 catalogs.
