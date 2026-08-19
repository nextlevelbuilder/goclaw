# Phase 3 — Skill Registry Approval/Curation + Signed Packages

**Depends on:** none
**Files in this phase:** skill approval (`internal/store/skill_store.go`, `internal/store/pg/skill_store.go` or `internal/store/pg/skill_evolution.go` pattern, `internal/http/skills.go`, `internal/gateway/methods/skills.go`, `internal/tools/skill_manage.go`, `internal/tools/publish_skill.go`), signed packages (`internal/crypto/`, `internal/store/` for `publisher_keys`)
**Ownership:** W1 = skill approval/curation, W2 = signed packages + trust anchor. Distinct files except `internal/crypto/` (W2 only). Controller verifies.

## Context / verified baseline

- Skill catalog solid: `skills` table (`000001:215-238`), `skill_versions` append-only audit (`000079`), grants, tenant configs. `skills.status` = active/archived/deleted only (`internal/store/skill_store.go:95`). Publish self-serve (`publish_skill.go:42-209`, `skill_manage.go:84-529`, ZIP upload `internal/http/skills.go:104`). Lite blocks skill_manage/publish_skill.
- Signed packages: ZERO. No ed25519/rsa/x509 (only test fixtures), `internal/crypto/` symmetric-only AES-256-GCM. SHA-256 content hashes exist (`skills.file_hash`, `skill_versions.content_hash`).
- Migration baseline: PG `000102`, SQLite `SchemaVersion=65`.

## Scope (from scout gaps)

### W1 — Skill approval/curation (scout gap 1)
1. **PG migration `000103_skill_review.up.sql`** (additive — coordinate number with Phase 1; if Phase 1 already uses 000103, use 000105; controller assigns final numbers):
   - Extend `skills.status` enum: `draft, pending_review, approved, published, rejected, suspended` — keep `active/archived/deleted` mapped: `active→published`, archive/deleted stay. New columns: `reviewed_by UUID NULL`, `reviewed_at TIMESTAMPTZ NULL`, `review_note TEXT NULL`.
   - ALTER TYPE or add CHECK? PG: if `status` is plain TEXT, just validate in store; if enum type, `ALTER TYPE ... ADD VALUE`. Verify current column def before choosing (KISS: add new statuses as TEXT constants, valid-whitelist in store, no ALTER TYPE).
2. **SQLite parity:** schema patch + `schema.sql` mirror columns. Bump `SchemaVersion`.
3. **Status lifecycle:** `store.SkillListParams.Status` accept new states; discovery/search gates on `status='published'` (index already partial WHERE active → update to published).
4. **Approve/reject methods:** `skills.approve` / `skills.reject` WS (admin-classified, `isAdminMethod`) + HTTP `POST /v1/skills/{id}/approve|reject`. On approve: status→published, set reviewed_by/at. On reject: status→rejected + note. Emit audit `skill.approved/rejected`.
5. **`skill_manage` / `publish_skill` / ZIP upload:** create/update lands in `draft` (owner-only) instead of going live; discoverable only when `published`. Update `internal/http/skills_versions.go` write path (immutable version on draft). Visibility + status interplay: private skill bypasses review (own-use), public skill requires review — document in code.
6. i18n keys for new user-facing errors (`skill.pending_review`, `skill.rejected`, etc.) — keys.go + 3 catalogs.

### W2 — Signed packages + trust anchor (scout gaps 2-4)
1. **`internal/crypto/signature.go`:** ed25519 helpers: `GeneratePublisherKeypair()`, `SignPackage(priv ed25519.PrivateKey, manifest []byte) ([]byte, error)`, `VerifyPackage(pub ed25519.PublicKey, manifest []byte, sig []byte) error`. Pure Go stdlib (`crypto/ed25519`), no new dep. Keep separate from AES secret path.
2. **PG migration `000104_publisher_keys.up.sql`** (additive): `publisher_keys(id UUID PK, publisher_id UUID NOT NULL, publisher_type TEXT, public_key BYTEA NOT NULL, fingerprint TEXT NOT NULL UNIQUE, status TEXT NOT NULL default 'active', created_at TIMESTAMPTZ NOT NULL default now())` + SQLite parity. (pgcrypto-less: fingerprint = hex(sha256(public_key)) computed in Go.)
3. **`internal/store/publisher_store.go`** + PG + SQLite impls: `UpsertKey`, `GetActiveKey(fingerprint)`, `ListKeys(publisherID)`, `DeactivateKey`.
4. **Signature column:** add `signature BYTEA NULL`, `publisher_id UUID NULL`, `signed_at TIMESTAMPTZ NULL` to `skills` + `skill_versions` (same PG migration + SQLite). On `publish_skill`/ZIP upload, sign the package manifest (SKILL.md + file list + content hashes) and store signature. Verify on install/import/download: `skill_manage` install path + `internal/skills/loader.go` parse — reject if `status='published'` AND signer key not in `publisher_keys` OR signature invalid. User-managed/local skills: warn-only (no hard reject).
5. **Package format:** define manifest JSON `{slug, semver, deps[], files[{path, sha256}], signed_at, signature}` appended/included in ZIP; validation at `internal/http/skills_upload.go` + `archive_extract.go` gate.
6. Semver: keep int `version` for disk-compat; optional add `release_channel` column (stable/candidate) — defer if not required (KISS, mark optional).
7. Tests: sign/verify round-trip, trust-anchor reject on unknown publisher, published-skill tamper detection, skill review lifecycle (draft→approved→published→rejected).

## Verification steps
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` (controller Docker).
- Unit: crypto sign/verify, publisher store, review lifecycle, approval-gated discovery.
- Integration: publish→review→discover flow; tampered published skill rejected on load.
- Report in `reports/phase-03-skill-signed.md`.

## Risks / rollback
- ALTER TYPE on PG is DDL lock; prefer TEXT constant whitelist (no enum change) to keep additive + low risk. If enum exists, `ADD VALUE` is atomic but NOT in transaction on old PG — verify PG version supports (PG18 does).
- Signature verify on load adds latency — do it once at install/import, cache result; load path keeps warn-only.
- `.claude/` skills remain local-only + unsigned (out of scope; document in report).