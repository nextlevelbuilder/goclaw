# Claude CLI Collaborative Audit Matrix

## Purpose

This matrix is the Phase 2 working artifact for issue `#596`. It exists to convert the Claude CLI hardening discussion from intuition into reproducible evidence.

## Scope

- Collaborative repository workflows in GoClaw
- Claude CLI provider behavior under shared-repo pressure
- Session, workspace, auth, and routing boundaries that affect correctness and safety

## Method

Each scenario should record:

1. Setup details
2. Reproduction steps
3. Expected invariant
4. Actual observed behavior
5. Severity and disposition

## Findings Summary

### Top blockers

1. Collaborative group sessions are shared by design on most platforms. GoClaw collapses non-Discord group users into one group-scoped `userID`, reuses one session key per chat, and allows up to three concurrent runs in that session. This is workable for shared repo work, but it is not per-collaborator isolation.
2. Claude account state is instance-global. Backend creation allows only one `claude_cli` provider per instance, and auth status is exposed through one server-level endpoint instead of a provider/account-aware health model.
3. There is no explicit no-send or failover gate. If Claude auth degrades, GoClaw discovers it only when the next CLI call fails; the current provider card UI can still imply healthy authentication.

### Likely-safe behaviors

1. Solo flows are deterministic: `sessionKey -> workDir -> CLI session UUID -> resume/reset lifecycle`.
2. The MCP bridge is materially hardened: the config lives outside the agent workdir, the request carries HMAC-signed context headers, and unsigned tenant headers are not trusted.
3. Thread and topic session keys isolate Claude history files correctly, even though they do not automatically isolate the underlying repo workspace.

### What later phases must solve

- `#598` must decide whether collaborative repo workflows remain intentionally shared or gain stricter session/workspace boundaries.
- `#597` must add provider/account-aware health, degraded state, and no-send behavior for Claude CLI.
- `#599` should stay deferred until the isolation and health model are explicit; current code does not provide a safe native Claude pool vocabulary.

## Scenario Matrix

| ID | Scenario | Expected invariant | Observed behavior | Code evidence | Severity | Classification | Follow-up |
|----|----------|--------------------|-------------------|---------------|----------|----------------|-----------|
| A1 | Single user, one repository, one Claude CLI session | Session history, workspace, and bridge context should stay tied to one user and one repo flow. | Meets the baseline. The loop passes session/user/channel/chat/workspace into the provider, the provider creates a stable workdir from `sessionKey`, and Claude resume state is deterministic. | `internal/agent/loop.go:246`, `internal/agent/loop_context.go:110`, `internal/providers/claude_cli_chat.go:18`, `internal/providers/claude_cli_session.go:87` | low | supported baseline | Preserve during `#598` hardening |
| A2 | Group chat or multi-user collaboration on one repository | Collaborators should have explicit, well-understood sharing boundaries. | Non-Discord groups intentionally collapse to one group-scoped `userID`, one chat-scoped session key, and one effective workspace lineage. Discord guilds are per-user, but other group channels are shared-by-design. | `cmd/gateway_consumer_normal.go:49`, `cmd/gateway_consumer_normal.go:83`, `internal/agent/loop_context.go:115`, `internal/agent/loop_utils.go:36` | high | operational limitation plus documentation gap | `#598` |
| A3 | Same repository, two concurrent runs, same session key | Concurrent work should serialize end-to-end or fail safely. | Group sessions allow `maxConcurrent = 3`, but Claude CLI only locks the subprocess call per session key. That means run scheduling can overlap while tool execution and workspace writes still interleave. | `cmd/gateway_consumer_normal.go:201`, `internal/scheduler/queue.go:36`, `internal/providers/claude_cli_chat.go:29` | high | product gap | `#598` |
| A4 | Same repository, two concurrent runs, different session keys | Different session keys should isolate both history and workspace if reviewers expect separate work streams. | History isolation is better than workspace isolation. Thread/topic session keys split Claude history, but workspace resolution is based on user/chat scope rather than session key, so different sessions can still touch the same repo files. | `cmd/gateway_consumer_normal.go:56`, `cmd/gateway_consumer_normal.go:64`, `internal/agent/loop_context.go:115`, `internal/store/pg/agents_context.go:238` | medium | product gap | `#598` |
| A5 | Different repositories on the same host | Repo A should not silently bleed into Repo B through session files or bridge config. | Mostly safe when the agents point at different base workspaces. Session workdirs and MCP config directories are derived from `sessionKey` and stored outside the repo workspace; the bridge carries the explicit workspace path. | `internal/providers/claude_cli_session.go:87`, `internal/providers/claude_cli_mcp.go:79`, `internal/providers/claude_cli_mcp.go:127`, `internal/gateway/server.go:214` | low | supported baseline with operator caveat | Reconfirm in `#598` tests |
| A6 | Resume after successful run | Resuming should continue the same Claude history without guessing. | Meets the baseline. Claude CLI resumes from the deterministic workdir/session-id pair whenever the `.jsonl` session file exists. | `internal/providers/claude_cli_session.go:45`, `internal/providers/claude_cli_session.go:87` | low | supported baseline | None |
| A7 | Reset after poisoned history or bad tool state | Reset should remove stale Claude history and prompt state. | Meets the baseline. `/reset` removes the Claude session file and `CLAUDE.md`, forcing a fresh session on the next run. | `internal/providers/claude_cli_session.go:243`, `cmd/gateway_consumer_handlers.go:459` | low | supported baseline | None |
| A8 | Auth loss before a run | GoClaw should detect degraded Claude eligibility before sending unsafe work. | Not implemented. Auth status is checked only through a global endpoint, and the agent loop does not gate Claude CLI calls on that signal before execution. | `internal/http/provider_verify.go:124`, `internal/providers/claude_cli_auth.go:17`, `internal/agent/loop.go:246` | high | product gap | `#597` |
| A9 | Auth loss during a resumed workflow | A resumed run should degrade into blocked/no-send instead of generic runtime failure. | Not implemented. The next Claude CLI subprocess call simply fails; there is no provider/account health state machine or no-send mode. | `internal/providers/claude_cli_chat.go:61`, `internal/http/provider_verify.go:127` | high | product gap | `#597` |
| A10 | Per-user override or account switch interaction | Account routing should be explicit, observable, and per-provider or per-user when promised. | Not implemented. Backend creation blocks a second `claude_cli` provider per instance, the setup UI disables adding another one, and the UI instructs operators to log out and log back in globally. | `internal/http/providers.go:293`, `ui/web/src/pages/providers/provider-form-dialog.tsx:57`, `ui/web/src/pages/providers/provider-cli-section.tsx:67` | high | unsupported architecture / deferred enhancement | `#599` after `#597` and `#598` |

## Doc and UI Drift

- `docs/02-providers.md` describes Claude CLI as a local CLI session path, but it does not state the stronger current invariant that only one Claude CLI provider is allowed per instance and that auth visibility is global rather than provider-aware.
- Provider list badges currently render Claude CLI as authenticated unconditionally, even though actual auth state is fetched separately through `/v1/providers/claude-cli/auth-status`. That overstates healthy account status in exactly the area `#597` needs to harden.

## Severity Guide

- `critical`: user data or repo integrity can cross tenants, users, or repositories
- `high`: unsafe or misleading behavior under normal collaborative usage
- `medium`: recoverable but still harmful ambiguity or operator burden
- `low`: documentation, observability, or UX gap with limited safety impact

## Notes

- This pass is a code-path audit. It gives Phase 3 a defensible starting point without pretending runtime chaos testing has already happened.
- Do not close `#596` until the matrix is backed by at least one targeted verification run for the high-severity rows.
