# Phase 1 — `/gc:` Foundation

## Context

AgentKit vision plan (`plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`)
§104/§105/§1.2/§5/§6/§7/§8/§9/§102. User đã chốt (2026-08-18):

- **4 command đầy đủ**: `/gc:plan`, `/gc:fix`, `/gc:cook`, `/gc:review`.
- **Reply surface**: chạy qua agent loop bình thường — không WS method mới, không UI render path mới.
- `/gc:cook` = instruction-driven flow qua tool loop (đã có `ExecuteToolCall`, `run.timeline`, `runs.*`),
  KHÔNG phải DAG/durable runtime (Phase 2).

## Requirements

1. Command parser `/gc:<command> <input>` (+ flags `--deep/--fast/--hard/--strict`, aliases không bắt buộc §2.2).
2. Skill registry: skill là first-class — metadata mở rộng `inputs/outputs/allowed-tools/quality-gates`.
3. Built-in engineer kit `go-claw-engineer` với 4 skill: `plan`, `fix`, `cook`, `review`.
4. `/gc:plan` → artifact `plans/<timestamp>-<slug>.md` (12 mục §6).
5. `/gc:fix` → RCA pipeline §8.
6. `/gc:review` → severity §9 + `review-report.md`.
7. `/gc:cook` → instruction-driven §7.

## Files

### Modify
| File | Change |
|------|--------|
| `internal/skills/loader.go` | Mở rộng `Metadata` (:34) + `parseMetadata` (:603)/`parseSimpleYAML` (:721) với `inputs`, `outputs`, `allowed-tools`, `quality-gates`. Giữ backward-compat (`name`/`description` vẫn đọc, thiếu key mới = zero value). |
| `internal/agent/loop_types.go` | Thêm `GCDispatcher gc.CommandDispatcher` vào `LoopConfig` (:337 block) + `Loop` (:75 struct), init trong `NewLoop` (:537). |
| `internal/agent/loop_history.go` | Thêm call `applyGCCommand` cạnh `applySkillSlashCommand` (:137). |
| `internal/agent/gc_command.go` (mới) | `applyGCCommand(ctx, req, message, extraPrompt, skillFilter)` theo pattern `applySkillSlashCommand` — resolve qua `l.gcDispatcher`, match → trả (transformedMessage, extraPrompt, skillFilter); no-match → passthrough. |
| `internal/agent/resolver.go` | Thêm `GCDispatcher gc.CommandDispatcher` vào `ResolverDeps` (:33 block) + truyền vào `LoopConfig` khi build loop. |
| `cmd/gateway_managed.go` | Wire GC executor vào `ResolverDeps` (cạnh `Skills:` :231, `SkillSlashCommands:` :95) + qua loop build. |
| `internal/skills/seeder.go` | **N/A (resolved — no change needed):** `Seeder.Seed` auto-walks `bundledDir` (seeder.go:56 `os.ReadDir`); `UpsertSystemSkill` (pg/skills_admin.go:18) nhận slug từ dir name, match master tenant. Seed `skills/{plan,fix,cook,review}/SKILL.md` tự động. Phase 1: seed-only, không hard-require DB (file-system fallback). |

### Create
| File | Change |
|------|--------|
| `internal/commands/gc/parser.go` | Parse `/gc:<cmd> <input>`, flags `--deep/--fast/--hard/--strict`. Trả về `Command{Kind CommandKind, Input string, Flags []string}`. Prefix `/gc:` độc lập (không đụng skill slash prefix). |
| `internal/commands/gc/registry.go` | `Registry`: map `CommandKind→SkillSlug` (plan→go-claw-engineer/plan...). `Register(kind, slug)`, `Lookup(kind)`, `KnownKinds()`. |
| `internal/commands/gc/executor.go` | `CommandDispatcher` interface `{ Resolve(ctx, msg) (*Dispatch, bool) }` + `Executor` (build ExtraSystemPrompt từ skill content + workflow steps + quality gates). `Dispatch{Kind, Skill, Content, Remaining}`. |
| `internal/commands/gc/errors.go` | Error taxonomy §101 sentinels + `UserError{Status, Code, Retryable, Message, NextAction}` §102 (dùng trong error paths của executor). |
| `skills/go-claw-engineer/plan/SKILL.md` | Skill `plan` — pipeline §6 (understand → inspect → plan → verify), 12-mục output contract. |
| `skills/go-claw-engineer/fix/SKILL.md` | Skill `fix` — RCA §8 (reproduce → evidence → hypothesis → root cause → minimal fix → regression). |
| `skills/go-claw-engineer/cook/SKILL.md` | Skill `cook` — §7 (read plan → modify → test → repair → verify; "code generated ≠ task completed"). |
| `skills/go-claw-engineer/review/SKILL.md` | Skill `review` — §9 (9 dimensions, severity BLOCKER→INFO). |
| `internal/commands/gc/parser_test.go` | Unit tests parser. |
| `internal/commands/gc/executor_test.go` | Unit tests executor (fake skill content). |
| `internal/skills/metadata_test.go` | Test mở rộng metadata parsing. |

## Implementation steps

1. **Metadata**: extend `internal/skills/loader.go` — `Metadata` (:34) thêm field; `parseSimpleYAML` (:721) hỗ trợ list; `parseMetadata` (:603) đọc thêm keys; giữ error-tolerant (thiếu key = zero value, skill cũ không vỡ).
2. **Kit skills**: tạo `skills/go-claw-engineer/{plan,fix,cook,review}/SKILL.md` với frontmatter mở rộng (`name`, `description`, `version`, `inputs`, `outputs`, `allowed-tools`, `quality-gates`) + body hướng dẫn workflow (tự document — LLM đọc file qua tool).
3. **Parser**: `internal/commands/gc/parser.go` — parse prefix `/gc:plan|fix|cook|review` (case-insensitive, chấp nhận alias nếu cấu hình), phần còn lại là input; flags `--deep/--fast/--hard/--strict` tách khỏi input.
4. **Registry + Executor**: map command→skill; `Executor` build `Dispatch{Kind, Skill, Content, Remaining}`; `applyGCCommand` trả `(message, extraPrompt, skillFilter)` — extraPrompt = skill content + workflow steps, skillFilter = skill slug (giới hạn tool list qua `FilterSkills` tên-based). `allowed-tools` được parse vào Metadata + expose trong content nhưng KHÔNG cưỡng chế tool filter trong Phase 1 (metadata-first; LLM tuân theo instruction trong SKILL.md).
5. **Loop hook**: `internal/agent/gc_command.go` — `applyGCCommand` gọi `l.gcDispatcher.Resolve(ctx, msg)`; match → transform + extra prompt; no-match → passthrough nguyên message (không block, không đổi hành vi message thường).
6. **Wiring**: `LoopConfig.GCDispatcher` + `Loop` + `NewLoop`; `ResolverDeps.GCDispatcher` + build loop; `cmd/gateway_managed.go` tạo executor với `skillsLoader` closure.
7. **Verify**: unit tests (parser/executor/metadata) + `go build ./...` + `go vet ./...` + `go test ./internal/...`.

## Validation

- `go build ./...` (PG), `go build -tags sqliteonly ./...` (Lite), `go vet ./...`.
- `go test ./internal/commands/... ./internal/skills/... ./internal/agent/...`.
- Manual (nếu có PG): gửi `/gc:plan <request>` qua WS → agent loop chạy, stream reply, artifact ghi workspace.
- Regression: `go test -tags integration ./tests/integration/` (nếu PG sẵn sàng).

## Risks & rollback

- **Skill frontmatter break**: metadata mở rộng phải error-tolerant — skill cũ thiếu key mới vẫn parse được (zero value). Rollback: revert metadata changes.
- **Loop regression**: `applyGCCommand` passthrough khi không match — không đổi hành vi message thường. Test `TestSkillSlashCommand` đảm bảo.
- **Kit không seed ở PG**: managed mode cần seeder; nếu bỏ qua, skill vẫn hoạt động qua `skills/` dir trên file-system (workspace/global). Phase 1 không hard-require DB seeding.
- **Cook không durable**: crash giữa verify loop mất tiến trình — chấp nhận cho Phase 1, ghi rõ limitation trong plan.

## Notes

- Không đổi WS protocol/UI trong Phase 1.
- Không shell-out `ak` — native Go (phù hợp §1.1/§1.2).
- Artifact path: workspace dir, dùng `plans/<ts>-<slug>.md` (§6) cho plan; `review-report.md` (§9).
- **Controller review WS-A/B (2026-08-18):** khớp contract. Ghi chú: có 2 nơi build system prompt (`gc.Executor.BuildSystemPrompt` trong commands/gc và `gcSystemPromptSection` trong agent) — agent dùng interface `CommandDispatcher.Resolve` (chỉ expose Resolve), không reach được concrete method, nên có duplication nhỏ. Chấp nhận được cho Phase 1; Phase 2 có thể expose `BuildSystemPrompt` qua interface nếu cần.
- **Kit layout:** loader yêu cầu FLAT 1-level (`skills/<slug>/SKILL.md`, slug = dir name). Kit `go-claw-engineer` là khái niệm, KHÔNG phải parent dir. WS-C đã được báo sửa.
- **WS-C runtime (2026-08-18):** agent chết vì Cloudflare 524 (inference gateway timeout) sau khi đã đưa toàn bộ 4 SKILL.md vào working tree. Không respawn — phần seeding còn lại được controller verify độc lập (không cần sửa seeder.go, xem phần trên).
- **CRLF/stash incident (2026-08-18):** lệnh `git stash` nằm trong Docker test container đã stash toàn bộ working tree (CRLF noise do `core.autocrlf=true`), sau đó crash trước khi pop → phase 1 code mất khỏi worktree. Khôi phục bằng `git checkout 'stash@{0}' -- <6 files>` (WS-A loader + WS-B wiring), reset index, re-add (git renormalize CRLF→LF), drop stash. Bài học: **không chạy git stash trong container**; verify line-endings trước commit khi làm việc cross-platform.
- **SHIPPED (2026-08-18):** PR #8 merged vào `dev` — merge commit `20139893523930ac5cb1b5d7dfc647e13c2e1afd`. CI green post-merge trên chính SHA merge (go/web/release-versioning success, run `32107516102`). 4 commits: `56ef2ba6` (WS-A) → `fece9a6a` (WS-B) → `b1fef1e4` (WS-C) → `7051ec10` (plan).