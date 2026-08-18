# WS-C Kit Manager — Implementation Report

Status: DONE_WITH_CONCERNS

## Summary

Kit `go-claw-engineer` now has a first-class versioned manifest plus a runtime
`KitManager` for version pin, checksum, inspect, and list. No DB, no command,
no writes to other skill folders. All files created, compile-sane, tests
written. Docker gate / git / PR / CI are handled by the controller (Bash tool
is broken in this environment — ENAMETOOLONG spawn failure).

## Files created

1. `skills/go-claw-engineer/kit.yaml` — manifest:
   ```yaml
   name: go-claw-engineer
   version: 1.0.0
   description: Built-in GoClaw Engineer kit
   skills:
     - plan
     - fix
     - cook
     - review
   ```
   No `checksum` field (optional by design — computed at runtime).

2. `internal/skills/kit_manager.go` — `KitManager`:
   - `NewKitManager(manifestPath, skillsRoot string) *KitManager`
   - `Load(ctx) (*KitManifest, error)` — yaml.v3 parse, validates name+version required.
   - `ListSkills(ctx) []string` — manifest slugs filtered by `skillsRoot/<slug>/SKILL.md` existence, sorted.
   - `Version() string`
   - `ComputeChecksum(ctx) (string, error)` — SHA-256, deterministic: sorted slugs, length-prefixed `len(slug):slug\n` + raw SKILL.md bytes + NUL separator.
   - `VerifyChecksum(ctx) (bool, error)` — if manifest `checksum` absent → `(true, nil)`; else compare computed vs pinned.
   - `RenderedManifest() string` — YAML-ish text for inspect/dry-run, annotates `# missing on disk`.
   - `Inspect(ctx) (*KitInfo, error)` — `KitInfo{Name, Version, Skills, Checksum, Verified, SkillCount}`.

3. `internal/skills/kit_manager_test.go` — 12 tests, all temp-dir based (no repo-path dependency): parse manifest, missing file, list filters by disk, empty skills root, version, checksum deterministic, checksum changes on content mutation, verify no-pin → true, verify pinned match/mismatch, rendered manifest, rendered manifest missing-skill annotation, inspect.

## Design decisions

- **Path resolution (per controller update):** `skills/` holds skill folders
  TOP-LEVEL (`{plan,fix,cook,review,goclaw,docx,...}/SKILL.md`) — there is no
  `skills/go-claw-engineer/` and `loader.go` reads `skillsDir/<name>/SKILL.md`.
  So `KitManager` takes `manifestPath` (→ `skills/go-claw-engineer/kit.yaml`,
  new dir I created, holds only kit.yaml) + `skillsRoot` (→ `skills/`). A
  missing `skillsRoot` yields an empty skill list; a manifest can still load.
- **Checksum NOT hardcoded** in kit.yaml — computed at runtime over skill
  content. WS-D adding skills later only needs to update the manifest `skills`
  list; checksum verification stays valid because it is derived, and a manifest
  without a pinned `checksum` verifies as `true`.
- **`ListSkills` reads manifest slugs, then filters by disk presence** rather
  than globbing the skills root, so non-kit skills (`goclaw`, `docx`, ...) are
  excluded even if they exist on disk.
- **No loader.go changes.** No `internal/commands/gc/**`, workflow, artifact,
  store, migration, or version.go changes. No phase-id/WS-code in file names,
  comments, or test names.

## Concerns

1. `NewKitManager` signature differs from the original task line
   (`NewKitManager(skillsDir string)`) — changed to two args per controller's
   explicit direction to match the real layout.
2. `Inspect` computes the checksum twice (once direct, once inside
   `VerifyChecksum`) — KISS trade-off, negligible cost.
3. `RenderedManifest` lists slugs in manifest order (not sorted) to match the
   manifest; `ListSkills` is sorted. Intentional.
4. Test suite uses `0o755`/`0o644` octal literals — fine on Go 1.26.

## Verification status

- Compile-sane reviewed by eye (imports, symbols, signatures, yaml round-trip
  in test helpers — rewritten to avoid duplicate YAML keys).
- Docker gate (`go build/vet/test ./internal/skills/`), git branch/commit/push,
  PR creation, and CI monitoring: controller-owned.
