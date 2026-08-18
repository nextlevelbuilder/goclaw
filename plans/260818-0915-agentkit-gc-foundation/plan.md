# AgentKit Deep Integration — Phase 1 `/gc:` Foundation

Nguồn vision: `plans/260815-2340-goclaw-repository-reliability/GoClaw_AgentKit_2026_Deep_Integration_Plan.md`
(`§104` acceptance criteria, `§105` Phase 1, `§1.2` native pipeline, `§5` built-in kit, `§6/7/8/9` command specs).

## Status

- **Phase 1 (`/gc:` Foundation):** `[ ]` — plan approved, implementation pending.
- Phase 2–7: deferred (không trong scope repo này).

## Quyết định đã chốt (user)

1. **Command scope:** đóng gói đủ 4 command theo §105 — `/gc:plan`, `/gc:fix`, `/gc:cook`, `/gc:review`.
   - `/gc:cook` không có durable runtime (Phase 2) → implement như instruction-driven flow qua tool loop sẵn có, KHÔNG phải DAG.
2. **Reply surface:** `/gc:*` là message thường đi qua agent loop bình thường. Skill/kit được tiêm qua
   `ExtraSystemPrompt` (pattern `applySkillSlashCommand`), kết quả stream như chat + ghi timeline.
   Không thêm WS method mới, không thêm UI render path trong Phase 1.

## Phases

| Phase | Nội dung | Files chính | Deps | Trạng thái |
|-------|----------|-------------|------|------------|
| 1 | `/gc:` Foundation | `internal/commands/gc/`, `internal/skills` (metadata), `skills/go-claw-engineer/` | — | `[ ]` |

## Acceptance criteria (Phase 1)

- [ ] `/gc:plan|fix|cook|review <input>` chạy native (không shell-out `ak`), qua agent loop.
- [ ] Skill là first-class: metadata có `inputs/outputs/allowed-tools/quality-gates`, registry liệt kê được.
- [ ] Built-in engineer kit (4 skill `plan/fix/cook/review`, dir phẳng `skills/<slug>/SKILL.md`) được seed tự động qua `Seeder.Seed` (auto-walk `bundledDir`, không cần sửa seeder).
- [ ] `/gc:plan` tạo artifact `plans/<timestamp>-<slug>.md` với đủ 12 mục §6.
- [ ] `/gc:fix` buộc Root Cause Analysis (reproduce → evidence → hypothesis → fix → regression test).
- [ ] `/gc:review` trả severity BLOCKER/CRITICAL/HIGH/MEDIUM/LOW/INFO, ghi `review-report.md`.
- [ ] `/gc:cook` instruction-driven: đọc plan → modify → test → repair → verify.
- [ ] Regression: `go build ./...`, `go vet ./...`, `go test ./internal/...` xanh.

## Execution detail

- Phase file: [`phase-01-gc-foundation.md`](phase-01-gc-foundation.md)
- Reports: [`reports/`](reports/)