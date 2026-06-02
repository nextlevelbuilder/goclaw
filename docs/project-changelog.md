# Project Changelog

Significant changes, features, and fixes in reverse chronological order.

---

## 2026-05-28

### CLI credential adapter framework + git adapter (issue #82)

Refactors `credentialed_exec.go` from "flat env-var injection only" to a
generic `CredentialAdapter` interface that supports argv prefix injection,
ephemeral filesystem material, system-trusted env vars, and per-injection
redaction. Existing presets (`gh`, `aws`, `gcloud`, `kubectl`, `terraform`,
`gws`) route through a passthrough adapter and keep current behavior
bit-for-bit.

**New**

- `CredentialAdapter` interface + registry in `internal/tools/credential_adapter.go`.
  Default `passthrough` adapter preserves legacy behavior; named adapters
  register via `init() → RegisterAdapter`.
- `git` adapter (`internal/tools/credential_adapter_git.go`) covering both
  HTTPS PAT and SSH key paths. PAT injected via `GIT_CONFIG_COUNT` +
  `GIT_CONFIG_KEY_*`/`GIT_CONFIG_VALUE_*` env vars (never argv → keeps token
  off `ps`/`/proc/<pid>/cmdline`). SSH key materialized to 0600 tmpfile +
  `GIT_SSH_COMMAND` with `IdentitiesOnly=yes` and `StrictHostKeyChecking=accept-new`.
  Passphrase-protected SSH keys rejected at validation with
  `git.cred_ssh_passphrase_unsupported`.
- `psql` framework-validation stub adapter (`internal/tools/credential_adapter_psql.go`)
  proving the interface holds for non-git credential families.
- Shared `materializeEphemeral` helper (`internal/tools/credential_ephemeral.go`)
  with idempotent cleanup latch and explicit `0600` chmod.
- Per-injection audit log: `slog.Warn("security.system_env_injection", …)`
  with `adapter`, `binary`, `user_id`, sorted `env_keys` (names only),
  `argv_prefix_len`, `host_scope_hash` (SHA-256 first 8 hex chars — plaintext
  hostname intentionally omitted for PII safety). Schema pinned by
  `TestEmitSystemEnvInjectionAudit_*`.
- Typed-credential HTTP PUT path with `{error:{code,message}, error_key}`
  envelope so the web UI can drive field-level validation
  (`git.cred_host_scope_required`, `git.cred_ssh_passphrase_unsupported`,
  etc.).
- Web UI: `CliCredentialGitFields` extends the CLI Credentials dialog with
  `Personal Access Token` / `SSH Private Key` picker, host-scope input,
  CRLF→LF paste normalization, masked-secret edit flow (`••••••••`
  placeholder preserves stored value on save).
- 17 i18n keys × 3 locales (en/vi/zh).

**Docs**

- New: `docs/git-credential-adapter.md` — user-facing guide (when to use PAT
  vs SSH vs env, host-scope semantics, TOFU caveat with `ssh-keyscan`
  mitigation, SIGKILL residual material note, operator sweep recipe).
- New: `docs/credential-adapter-playbook.md` — implementer guide with worked
  mappings for `kubectl`, `docker`, `npm`, `aws`, `psql` and the three-question
  interface-validation gate.
- `docs/09-security.md` § 14 — trust-boundary diagram, audit field schema,
  SSH TOFU + SIGKILL caveats, v2 future-work list.
- `docs/03-tools-system.md` § 8a — adapter framework summary linking to the
  playbook.

**Security**

- User-paste denylist (`ValidateGrantEnvVars`) unchanged — first line of
  defense intact. Adapter path is the second, audit-trailed line; a typo in
  `adapter_name` falls back to passthrough (no silent bypass).
- AES-256-GCM at rest for stored PAT/SSH bodies; secrets cannot be read back
  via API/UI.
- `ScrubCredentials` redacts PAT bytes and SSH key path from stdout, stderr,
  `Result.Content`, and audit log JSON.

**Known v1 limitations**

- One credential per (user, binary, host_scope).
- No multi-host wildcard (`*.github.com`).
- No persistent `known_hosts` per credential (TOFU only).
- No sandbox support (adapter is incompatible with bind-mount-based sandbox
  path).
- No credential-refresh primitive (blocks future `aws sts assume-role`
  adapter).

---

## 2026-05-27

### zuey VPS ops scripts: repo-tracked + CI auto-sync

**Fixes**

- Patched `/usr/local/bin/goclaw-deploy` on zuey to survive a self-loop `/opt/goclaw/current` symlink (`readlink -f` now `2>/dev/null || true`, with a warning when `previous` is empty). Without this, `set -euo pipefail` aborted before `ln -sfn` could overwrite the symlink, silently failing every `deploy_zuey_beta` CI run.

**Changes**

- Moved `scripts/goclaw-upgrade-release.sh` → `scripts/zuey/goclaw-upgrade-release.sh`.
- Added `scripts/zuey/goclaw-deploy.sh` (canonical source for the on-host `/usr/local/bin/goclaw-deploy`).
- Wired `Sync zuey ops scripts to VPS` step in `.github/workflows/dev-beta-release.yaml` to `scp + sudo install` both scripts before triggering the gateway upgrade endpoint on every beta release. Requires new repository secrets `ZUEY_SSH_PRIVATE_KEY_B64` (base64-encoded private key, single line) and `ZUEY_SUDO_PASS`; step skips with a warning if either is unset.
- Updated `docs/deployment-guide.md` with the self-loop guard rationale, manual sync recipe, and required-secrets table.

**Followup fix (run 26499549166)**

- First real `deploy_zuey_beta` run on `dev` failed with `Load key: error in libcrypto` because GitHub Secrets storage normalized newlines inside the multi-line PEM block. Switched the secret to base64 (`ZUEY_SSH_PRIVATE_KEY_B64`) and added pre-flight validation (`ssh-keygen -y -f`) that fails fast with a remediation hint if the decoded key is malformed.

---

## 2026-05-24

### Google Workspace CLI runtime integration

**Features**

- Added `gws` as a preinstalled Google Workspace CLI in the full runtime image.
- Added a SecureCLI `gws` preset with encrypted credential injection fields and guardrails for interactive auth/export commands.
- Documented Drive, Gmail, and Calendar command patterns plus live credential smoke-test requirements.

**Tests**

- Added runtime binary discovery, SecureCLI preset, deny-pattern, and Dockerfile contract coverage for Google Workspace CLI.

### Slash skill commands

**Features**

- Added explicit slash skill activation for `/<slug>`, `/use <slug-or-name>`, `/list-skills`, and `/help <slug-or-name>`.
- Added tenant settings for slash command enablement, similar-skill suggestions, partial matching, and custom prefix.

**Tests**

- Added backend coverage for parser false positives, exact/partial skill resolution, suggestions, help/list commands, and config overlays.

### Configurable skill upload limits

**Features**

- Added configurable skill ZIP upload limits with config/env, SKILL.md frontmatter, and tenant system setting support.
- Added dashboard settings and dynamic upload validation so the Web UI follows the tenant limit instead of hardcoding 20MB.

**Tests**

- Added backend coverage for limit precedence, clamping, oversized rejection, and frontend coverage for parameterized upload validation.

### CLI environment variable visibility

**Features**

- Added `sensitive` and `value` kinds for secure CLI environment variables across binary defaults, agent grant overrides, and user overrides.
- Plain value entries are visible to authorized admins for operational config review, while sensitive entries remain masked and replace-only.

**Fixes**

- Stopped per-user credential reads from returning legacy sensitive env values raw.
- Kept legacy `{"KEY":"value"}` env blobs backward-compatible by treating them as sensitive.

**Tests**

- Added backend regression coverage for env kind parsing, sanitized API responses, runtime flattening, and invalid kind rejection.
- Verified Web UI build after adding env-kind controls and warnings.

### Command keyword allowlist

**Features**

- Added scoped credentialed CLI keyword allowlist config for content arguments and positional arguments, with runtime reload and web config editing.

**Fixes**

- Kept credentialed CLI `deny_args` active for command paths such as `gh secret set` while allowing approved GitHub issue/PR prose to mention security vocabulary.
- Added security audit logging for real allowlisted pass-throughs without logging full argument values.

**Tests**

- Added regression coverage for scoped keyword masking, disabled rules, unsafe positional rules, config reload, and race-safe policy snapshots.

### Browser cookie sync and config UI

**Features**

- Added scoped browser cookie sync API, encrypted cookie store, browser runtime cookie application, dashboard browser settings, and a selected-cookie Chrome extension prototype.

### Cron SecureCLI credential context

**Fixes**

- Fixed cron-triggered agent turns so credentialed CLI lookups preserve the explicit tenant user credential identity captured when the cron job is created.
- Kept cron `user_id` ownership unchanged for group-scoped list/remove behavior while storing credential lookup identity separately in cron payload metadata.

**Tests**

- Added regression coverage for cron payload credential metadata, legacy payload compatibility, cron scheduler context injection, and SQLite cron persistence parity.

---

## 2026-05-22

### Usage Cap budget controls

**Features**

- Added Standard/PostgreSQL usage caps for AI budget control by hour, day, week, or month.
- Caps support token and USD cost ceilings at tenant, agent, provider, provider type, and model scopes.
- Added OpenRouter catalog sync plus tenant/provider/model pricing overrides for input, output, cache read/write, reasoning, request, image, and web search units.
- Enforced caps in agent, fallback model, subagent, memory flush, compaction, and media reading tools (`read_image`, `read_document`, `read_audio`, `read_video`) with preflight reservation and post-call reconciliation.
- Added non-negative validation for catalog and override pricing fields.
- Added OpenRouter alias resolution for native model IDs, cached-input accounting normalization, and partial-stream failure reconciliation.
- Bridged legacy `budget_monthly_cents` into generated monthly agent USD cap policies, including migration backfill and save-time sync.
- Added web dashboard controls on Usage and Provider detail pages.
- Added Usage page editing for manual cap policies, including enable/disable, scope clearing, and token/USD limit clearing while keeping generated agent-budget caps read-only.
- Added usage-cap decision metadata to LLM spans, including allow/skip/block reason, policy IDs, estimates, actuals, and reconcile status.

**Tests**

- Added pricing and cap service coverage.
- Verified Go builds, SQLite build compatibility, full Go test suite, integration race suite, and Web UI production build.

### Messaging debounce hardening

**Features**

- Added per-agent inbound debounce override via `other_config.inbound_debounce_ms`; unset inherits the global gateway setting.
- Added Web Chat debounce for rapid text-only `chat.send` calls using `gateway.inbound_debounce_ms`.
- Clarified shared inbound debounce behavior in docs and Web UI config help text.

**Fixes**

- Fixed inbound debounce semantics so `gateway.inbound_debounce_ms=0` means no debounce and positive values set the wait window.
- Fixed Slack `debounce_delay: 0` so it disables per-thread batching instead of falling back to the default.

**Tests**

- Added regression coverage for channel inbound debounce, Web Chat debounce, media/cancel bypass handling, and Slack debounce config defaults.

### CLI P6 backend API unblock

**Features**

- Added HTTP session branch and history follow endpoints for automation clients.
- Added read-only channel writer permission testing.
- Added split activity-log and runtime-log aggregate endpoints.

**Tests**

- Added focused HTTP, store, and runtime log coverage for branch/follow, writer test, activity aggregate, and LogTee ring aggregation.

### CI/CD: zuey beta deploy

**Features**

- Added automatic zuey VPS deployment to the `Dev CI and Beta Release` workflow after beta prerelease assets are published.
- The deploy job triggers the protected gateway upgrade endpoint with the generated `vX.Y.Z-beta.N` tag, waits for upgrade status, and verifies public `/health`.
- Beta prereleases now upload `CHECKSUMS.sha256` alongside binary assets.

**Fixes**

- Updated the host release-upgrade script to support beta asset filenames with a leading `v`.
- Added checksum fallback to GitHub release asset SHA256 digests when beta releases do not publish `CHECKSUMS.sha256`.
- Detached gateway-triggered upgrades into a transient `systemd-run` unit so stopping `goclaw` during deploy no longer kills the upgrade job.
- Allowed stale upgrade `running` status records to be superseded after timeout and made the zuey deploy wait loop tolerate transient 502s during restart.

**Tests**

- Added regression coverage for stale gateway upgrade status recovery.

---

## 2026-05-21

### Tools: configurable exec timeout

**Features**

- Added `exec.settings.timeout_seconds` for configurable host command timeout through global and tenant built-in tool settings.
- Added dashboard controls for the `exec` timeout, defaulting missing settings to 60 seconds.
- Kept Docker sandbox execution on `sandbox_config.timeout_sec`; `exec.timeout_seconds` only affects the host built-in.

**Fixes**

- Added API validation for `exec` settings so invalid, zero, negative, fractional, or excessive timeout values are rejected before persistence.

**Tests**

- Added focused runtime and HTTP API regression coverage for configured `exec` timeouts and invalid settings.
- Verified web dashboard build with the typed `exec` settings form.

---

## 2026-05-20

### HTTP API contract hardening

**Fixes**

- Fixed memory API `{agentID}` handling so agent keys are resolved before storage access and invalid IDs return structured client errors instead of leaking UUID parse failures as HTTP 500.
- Allowed system/admin API-key automation to list agents and sessions without an extra `X-GoClaw-User-Id` header while preserving user filtering for non-admin callers.
- Added structured `/v1/*` not-found responses and a read-only `GET /v1/sessions` compatibility endpoint for automation clients.

**Tests**

- Added regression coverage for memory agent-key resolution, invalid memory IDs, session list auth behavior, and structured API 404s.

---

## 2026-05-18

### Packages: GitHub installer runtime path

**Fixes**

- Fixed GitHub Releases package installs on bare-metal gateways by defaulting the GitHub binary directory to `{runtimeDir}/bin` instead of Docker-only `/app/data/.runtime/bin`.
- The fix covers installs such as `github:nextlevelbuilder/goclaw-cli@v0.4.1` on the VPS, where `/app` is not writable or present.

**Tests**

- Added default-path regression coverage and made Unix-socket apk helper tests skip cleanly on Windows environments that cannot bind Unix sockets.

---

### Tools: built-in wait delay

**Features**

- Added a built-in `wait` tool with bounded millisecond delays, cancellation support, per-agent min/max settings, and runtime policy visibility.
- Preserved same-response ordering by making `wait` a sequential tool-call barrier.
- Added Web agent settings controls so per-agent wait limits are not dropped on save.

**Tests**

- Added focused wait validation, cancellation, policy, builtin seed, config parsing, and tool-stage ordering coverage.

---

### Providers: ChatGPT OAuth GPT-5.5 default

**Changed**

- Updated ChatGPT Subscription (OAuth) default model, provider model catalog, reasoning metadata, and docs examples to prefer `gpt-5.5`.

**Tests**

- Updated focused provider/model catalog, reasoning, registry, and token-window coverage for `gpt-5.5`.

---

### Deployment: Codex CLI service-user auth

**Fixes**

- Fixed agent-controlled Codex CLI auth on the VPS by ensuring the `goclaw` systemd service user has the ChatGPT login auth file under `/var/lib/goclaw/.codex/auth.json`.
- Documented the required service-user check: `sudo -u goclaw -H codex login status`.

---

### Packages: npm workspace protocol fallback

**Fixes**

- Fixed Node package installs for registry packages published with `workspace:` dependency ranges, such as `@agenttasks/cli`.
- GoClaw now retries npm `EUNSUPPORTEDPROTOCOL workspace:` failures by packing the registry tarball, rewriting workspace dependency ranges to published package versions, and installing the sanitized package folder.

**Tests**

- Added focused coverage for workspace protocol detection and package.json dependency rewrite behavior.

---

## 2026-05-17

### Skills: agent manage grants

**Fixes**

- Added explicit per-agent skill manage grants so agents can edit/delete skills they were authorized to maintain even when `owner_id` no longer matches their current actor identity.
- Auto-granted manage permission to the creating/publishing agent for new managed skills.

**UI**

- Show custom skill owner IDs in the Skills table.
- Added Skills page controls to grant agent skill access and edit/delete permission.

**Tests**

- Added PG/SQLite grant coverage and verified Go builds plus Web UI build.

---

### Agents: provider switch save fix

**Fixes**

- Fixed agent detail save after switching provider/model when the UI clears stale ChatGPT OAuth routing; typed JSON nulls now coerce to `{}` for NOT NULL agent JSON config columns in PostgreSQL and SQLite.

**Tests**

- Added regression coverage for typed `json.RawMessage(nil)` / JSON `null` agent config updates.

---

### Deployment: VPS hybrid GoClaw setup

**Operations**

- Deployed GoClaw to a VPS using bare-metal `systemd` gateway plus Dockerized PostgreSQL 18 pgvector.
- Restored the latest private PostgreSQL backup, then upgraded schema from `57` to `65`.
- Installed Node.js 22 and Codex CLI on the host; interactive `codex --login` remains manual.
- Configured Cloudflare-proxied deployment domain and issued SSL through Certbot/Nginx.
- Added `goclaw-backup-r2.timer` to dump PostgreSQL every 6 hours, upload to private Cloudflare R2 storage, and retain the latest 20 backups.
- Added deployment runbook in `docs/deployment-guide.md`.

**Features**

- Added a protected gateway upgrade HTTP API that triggers the fixed host-local upgrade script asynchronously.
- Added `scripts/goclaw-upgrade-release.sh` and installed the VPS copy at `/usr/local/bin/goclaw-upgrade-release`; dry-run verifies the latest stable server release asset and checksum before deploy.

---

### CI/CD: dev branch beta automation

**Features**

- Added a `Dev CI and Beta Release` GitHub Actions workflow for `dev` pushes that runs Go and Web UI checks before publishing a beta prerelease.
- Added semantic-release-style beta version calculation from Conventional Commits, creating `vX.Y.Z-beta.N` tags and prereleases automatically after tests pass.
- Beta automation uploads Linux binaries and publishes `beta` Docker image tags for the same release version, with Docker Hub publishing enabled when credentials are configured.

**Fixes**

- Made beta prerelease publishing independent of a local checkout by passing the repository explicitly to GitHub CLI release commands.

---

### Agent Permissions: channel and workspace matrix

**Features**

- Added `config.permissions.check` so the UI can preview the effective allow/deny decision for an agent, scope, config type, and user.
- Added Permissions UI support for `userId="*"` to grant all members in a selected group scope.
- Documented the cross-channel agent permission matrix, including Zalo group context writes and workspace/context file boundaries.

**Security**

- Protected group context file writes now require a real sender with `context_files` or legacy `file_writer` permission.
- Group file/context/cron permission-store errors now fail closed instead of silently allowing mutation.
- Backend config permission RPCs validate config types and permission values before storing rules.

**Tests**

- Added focused store and context interceptor coverage for permission preview and protected group context writes.

---

### CLI Credentials: per-agent env vars under Packages

**Features**

- Kept `CLI Credentials` as the Packages tab at `/packages?tab=cli-credentials` and preserved the legacy `/cli-credentials` redirect.
- Removed the duplicate standalone `CLI Credentials` item from the left sidebar.
- Added focused coverage for grant env payload semantics and routing contracts.

**Fixes**

- Made agent-grant environment variable controls easier to find by labeling the row action and adding a visible Environment Variables header inside the grant form.

**Security**

- Nested agent-grant get/update/delete/reveal routes now verify the grant belongs to the binary ID in the URL.
- Grant creation now validates both the CLI binary and target agent exist in the authenticated tenant before inserting.
- Grant updates now validate env payloads before scalar writes, preventing partial state changes on 400 responses.
- Runtime env precedence is covered: per-user env overrides per-agent grant env for duplicate keys.
- Credentialed exec now fails closed if per-user env JSON is invalid.
- SQLite add-column migrations for replayed schema snapshots now skip already-present columns.

**Tests**

- Focused backend, store compile, UI unit, and web build validation pass.
- Live PostgreSQL validation skipped because `TEST_DATABASE_URL` is not set.

---

## 2026-05-16

### Agents: per-agent model fallback

**Features**

- Added per-agent `model_fallback` config with ordered provider/model candidates.
- Agent advanced config UI now supports enabling fallback, adding backup provider/model pairs, and drag-and-drop ordering.
- Runtime wraps the resolved agent provider with fallback only for normal agent execution. Explicit provider/model overrides bypass the fallback chain.

**Migrations**

- **PG:** `000065_agent_model_fallback` adds `agents.model_fallback JSONB NOT NULL DEFAULT '{}'`.
- **SQLite:** schema v33 to v34 adds `agents.model_fallback TEXT NOT NULL DEFAULT '{}'`.

**Tests**

- Focused provider, provider resolver, store tests pass. Main app builds in default and `sqliteonly` modes. Web production build passes.

---

## v3.11.3 — 2026-04-26

### Fixes

- **`goclaw providers verify`** — empty body now triggers ping mode (provider registered/reachable check) and returns `{valid:true}` for registered providers. New `--model <alias>` flag for chat-verify against a specific model. CLI response parser switched from stale `{success, models}` to `{valid, error}`. Onboard auto-verify path fixed identically (was silently printing "FAILED" on every successful provider creation). (#1034)
- **`goclaw providers delete`** — succeeds when referenced by `agent_heartbeats`. FK changed to `ON DELETE SET NULL`; `DeleteProvider` (PG + SQLite) now wraps in a transaction that also disables affected heartbeats so the next scheduler tick cannot fire stale config. `slog.Warn("heartbeat.provider_cleared")` emitted with the disabled count. (#1034)
- **`goclaw doctor`** — provider rows with empty `display_name` now render the canonical `name` instead of a blank line. Query switched from `COALESCE(display_name, name)` to `COALESCE(NULLIF(display_name, ''), name)`. (#1034)

### Migrations

- **PG:** `000057_heartbeat_provider_fk_set_null` — defensive orphan cleanup, drop existing FK by lookup, re-add with `ON DELETE SET NULL`. Brief `ACCESS EXCLUSIVE` lock on `agent_heartbeats` during ALTER (sub-second on small tables; heartbeat workers may pause briefly).
- **SQLite:** schema v25 → v26 — full table rebuild for `agent_heartbeats` with new FK clause; explicit 25-column INSERT/SELECT preserves existing rows. `idx_heartbeats_due` recreated.

### Upgrade notes

- **Docker users:** MUST pull the new image (`ghcr.io/nextlevelbuilder/goclaw:v3.11.3`) AND run `goclaw upgrade` (or `goclaw migrate up`). Stale images on v3.11.2 will fail boot with `schema version mismatch: required 57, current 56` after the migration runs.
- **Bare-metal users:** rebuild and run `./goclaw upgrade`.

### OpenAPI

- `/v1/providers/{id}/verify` — `requestBody.required: false`; `model` documented as optional with ping-mode semantics.

---

## 2026-04-24

### Tools: Config-driven shell deny-groups + read_audio routing fixes

**Features**

- **`shellDenyGroups` runtime config:** `config.tools.shellDenyGroups` (map[string]bool) allows operators to toggle shell deny-groups (e.g. `package_install`, `env_dump`) from the /config Web UI without restarting. Merged with per-agent overrides with per-key agent precedence; multi-tenant invariant preserved. Subscribed to `bus.TopicConfigChanged` for live reload.

**Fixes**

- **Credentialed CLI wording scope:** "operation requires admin approval" error wording now scoped to `[CREDENTIALED EXEC]` marker only — was over-applied to generic shell failures, causing unjustified LLM pre-refusals.
- **read_audio transcription routing:** Fixed silent fallback on missing API credentials for transcription/gemini/openai paths — now hard-errors with clear message. Fixed openai_compat providers (e.g. DashScope) not reaching `/v1/audio/transcriptions` endpoint; moved transcription model check above provider type switch.

**Tests**

- 6 unit tests for shell deny-groups merge/defensive-copy semantics.
- 3 pub/sub dispatch tests for config reload lifecycle.
- 3 regression tests for read_audio fail-fast paths.

---

## 2026-04-22

### Providers: Native image generation for Codex + OpenAI-compat

**Features**

- **Codex native track:** `CodexProvider` now attaches the `image_generation` tool object to `POST /codex/responses` when the agent permits it. Streams `response.image_generation_call.partial_image` intermediate frames + `response.output_item.done` (type `image_generation_call`) final images; non-stream path walks `response.output[]`. Deduped per `item_id`, partial frames emitted as `ImageContent{Partial:true}` for UI progressive render.
- **OpenAI-compat track:** `tools[]` serializer passes `{type:"image_generation"}` entries through natively; response parser reads `choices[0].message.images[]` / `choices[0].delta.images[]` (data URLs) into `ChatResponse.Images`.
- **Media persistence:** `internal/agent/media.go` `persistAssistantImages()` writes final images to `{workspace}/media/{sha256}.{ext}`, returns `MediaRef` entries, clears inline base64. Idempotent on hash. Wired via `pipeline.Deps.PersistAssistantImages` callback from `FinalizeStage`. Partial frames skipped.
- **Capabilities + gate:** `ProviderCapabilities.ImageGeneration` flag, set true on Codex provider. Tri-level gate in agent loop: provider capability AND `AgentConfig.AllowImageGeneration` (read from `other_config.allow_image_generation`, default true) AND request not opted-out via `x-goclaw-no-image-gen` header.
- **Web UI:** Composer "Images" toggle chip (visible only when provider supports image gen, per-agent persistence in localStorage). Streaming placeholder skeleton in `ActiveRunZone` while partials arrive. `MediaGallery` assigns `generated-{timestamp}.png` filename for assistant-generated PNGs.

**Wire format**

Implementation is evidence-backed against the native ChatGPT Responses API event shape, not the compat shim shape. Research notes in `plans/reports/`.

**i18n**

- 1 UI key (`imageGenDownloadName`) in `ui/web/src/i18n/locales/{en,vi,zh}/chat.json` — download filename for generated images.

**Tests**

- Unit tests across providers (Codex native + OpenAI-compat), agent media persistence, store config. Full test sweep: 2618 pass.

**Internal docs**

- `plans/260422-1349-goclaw-chatgpt-image-gen/` — plan + phase files.
- `plans/reports/researcher-260422-1414-codex-native-image-events.md` — native event schema.

## 2026-04-20

### Pipeline: accurate context token tracking + dynamic compaction

**Features**

- **Session token display from metadata:** `sessions.metadata` now carries `last_prompt_tokens` and `last_message_count` on finalize. List query reads from metadata; fallback to octet/rune heuristic when absent. Fixes stale token display across session re-opens.
- **Tool-schema token accounting:** `TokenCounter.CountToolSchemas(model, tools)` new method counts tool definitions serialized as JSON. Tool-schema tokens included in `OverheadTokens` at ContextStage.
- **Dynamic compaction max_tokens:** Compaction `max_tokens` now derived from `in/25` with clamp `[1024, 8192]`. Applied to both summarization flow (`loop_compact.go`) and history sanitization (`loop_history_sanitize.go`). Replaces static 4096 limit — adapts budget to context size.

**Code**

- `internal/store/pg/sessions_list.go` — read/write `last_prompt_tokens` and `last_message_count` in metadata.
- `internal/store/sqlitestore/sessions*.go` — parity SQLite store updates.
- `internal/tokencount/token_counter.go` — `CountToolSchemas` interface method + `tiktoken_counter.go` impl.
- `internal/pipeline/context_stage.go` — include tool overhead in `OverheadTokens`.
- `internal/agent/loop_compact.go` — `dynamicSummaryMax` function; apply to compaction call.
- `internal/agent/loop_history_sanitize.go` — apply dynamic max to sanitization.

**Tests**

- `internal/tokencount/count_tool_schemas_test.go` — tool schema token counting.
- `internal/agent/loop_compact_dynamic_max_test.go` — dynamic max_tokens clamping.
- `internal/pipeline/context_stage_tool_overhead_test.go` — tool overhead integration.
- `internal/store/sqlitestore/sessions_display_tokens_integration_test.go` — metadata round-trip.

---

### TTS: timeout tenant-config + Gemini text-only 400 fix

**Features & Fixes**

- **Tenant-config timeout:** HTTP `/v1/tts/synthesize` and `/v1/tts/test-connection` now read `tts.timeout_ms` from system_configs (default 120s, was hardcoded 15s/10s). Gemini client default bumped 30s→120s for end-to-end alignment.
- **Gemini text-only error recovery:** Gemini preview models occasionally emit 400 "text generation" responses. Fixed by: (1) prepending inline prefix `"Speak naturally: "` to single-voice synthesis (multi-speaker untouched), (2) 1-retry with stronger prefix `"Read the following text aloud without translating, commenting, or modifying: "`, (3) new sentinel `gemini.ErrTextOnlyResponse` preserved through fallback chain via `errors.Join`.
- **Error UX:** HTTP returns 422 with localized `MsgTtsGeminiTextOnly` message. Agent TTS tool branches on sentinel to emit locale-translated ForLLM response.
- **Model default:** Gemini default model bumped `gemini-2.5-flash-preview-tts` → `gemini-3.1-flash-tts-preview` for higher stability.
- **UI bounds:** TTS timeout input now has `max=300000` (5 min).

**i18n**

- New key `MsgTtsGeminiTextOnly` in EN/VI/ZH catalogs for HTTP 422 + agent-tool ForLLM mapping.

**Code**

- `internal/audio/tts.go` — read tenant timeout in synthesize handlers.
- `internal/audio/gemini/` — inline prefix logic, retry budget, text-only sentinel.
- `internal/tools/tts.go` — agent-tool i18n branching on sentinel.
- `internal/http/methods/tts.go` — HTTP 422 error mapping.

---

### Tools: `send_file` — explicit workspace file delivery

**Features**

- **`send_file` tool** (`internal/tools/send_file.go`): dedicated tool for sending existing workspace files as chat attachments. Takes `path` (required) and `caption` (optional). Replaces implicit `message(MEDIA:path)` convention for re-delivering already-created files. Marks `DeliveredMedia` on success to prevent duplicate delivery.
- **`DeliveredMedia` mark on `message(MEDIA:)` sends** (`internal/tools/message.go`): patched to call `IsDelivered` / mark after successful MEDIA upload — closes the cross-tool duplicate-delivery gap where a file sent via `message(MEDIA:)` was not tracked and could be re-sent by `send_file`.
- Registered as builtin tool in `cmd/gateway_tools_wiring.go` and seeded in `cmd/gateway_builtin_tools.go`.

---

## 2026-04-22

### Codex OAuth pool routing strategy cleanup

**Changes**

- Removed `primary_first` from the public Codex OAuth routing strategy surface. The API, OpenAPI schema, and web UI now expose only `round_robin` and `priority_order`.
- Legacy `primary_first` and `manual` routing values now normalize to `priority_order` on read in the backend store layer.
- Activity endpoints now default empty/no-pool responses to `priority_order` instead of `primary_first`.
- Agent overrides that explicitly persist `extra_provider_names: []` continue to behave as single-account-only routing after the migration.

**Docs**

- Updated `docs/02-providers.md` and `docs/18-http-api.md` to describe the two-strategy model and the compatibility migration.

---

## 2026-04-21

### Webhook fixes (post-review security & idempotency hardening)

**Fixes**

- **K1: Auth context isolation** — Webhook auth middleware now resolves secret/HMAC signature before tenant injection (eliminating 401 due to tenant scope applied too early). Unscoped store methods `GetByHashUnscoped` + `GetByIDUnscoped` added to WebhookStore interface.
- **K7: IP allowlist enforcement** — Inbound webhook calls now check `ip_allowlist` field (CIDR + exact IP) after bearer/HMAC auth. Empty list = allow all (back-compat). Rejected requests return HTTP 403 with log `security.webhook.ip_denied`.
- **K8: HMAC replay protection** — Per-process nonce cache (key = `sha256(tenant_id + "|" + signature_hex)`) with 320s TTL rejects duplicate signatures within the skew window. Single-node caveat documented. Log: `security.webhook.hmac_replay`.
- **K2: `request_payload` canonical shape** — All webhook audit rows now store `{"body_hash":"<hex64>","meta":{...}}` JSON instead of raw bytes. Idempotency checker compares body hashes to detect replays with different payloads (409 Conflict).
- **K3: Body hash extraction** — `extractBodyHash()` now parses canonical audit payload structure (previously had parsing bugs leading to missed hash validation).
- **K9: Invariant test column fix** — Webhook tenant isolation test now references correct schema columns (`encrypted_secret`, `lease_token`).
- **K4: Worker slot drain** — Fixed channel leak in webhook worker that prevented slot release on successful claims. Concurrency now scales properly under load.
- **K5: Lease-token CAS on UpdateStatus** — Stale webhook receivers can no longer overwrite delivery status. Status updates use optimistic concurrency on `lease_token` (UUID), ensuring only the owning worker can mark the call done. Prevents duplicate delivery from slow receivers.
- **K6: HMAC signing key encryption** — Raw secret (from which `hmac_signing_key = hex(SHA-256(secret))` is derived) is now encrypted at rest via AES-256-GCM using `GOCLAW_ENCRYPTION_KEY`. Database compromise no longer = HMAC key compromise. Clients receive plaintext secret once (create/rotate response) and must store securely.
- **K10: Shared rate limiter instance** — Fixed duplicate `webhookLimiter` instantiation causing doubled RPM enforcement. Single limiter now shared across all webhook endpoints.

**Migrations**

- PostgreSQL: Migration `000057` adds `lease_token` column to `webhook_calls`. Migration `000058` adds `encrypted_secret` column to `webhooks`.
- SQLite: Schema v28 includes both new columns.

**Docs**

- `docs/webhooks.md`: Section 3 clarified bearer/HMAC auth contract + IP allowlist behavior. New Section 14 explains encryption at rest, key contract, DB compromise boundary.
- `docs/00-architecture-overview.md`: Section 12 (Webhook Subsystem) updated to mention lease-token CAS semantics and secret encryption.

**Environment**

- `GOCLAW_ENCRYPTION_KEY` is now **required** for webhook HMAC auth. Same key also encrypts LLM provider credentials.

---

## 2026-04-19

### TTS: Gemini provider + ProviderCapabilities schema engine

**Features**

- **Gemini TTS provider** (`internal/audio/gemini/`): supports `gemini-2.5-flash-preview-tts` and `gemini-2.5-pro-preview-tts`. 30 prebuilt voices, 70+ languages, multi-speaker mode (up to 2 simultaneous speakers with distinct voices), audio-tag styling, WAV output via PCM-to-WAV conversion.
- **`ProviderCapabilities` schema** (`internal/audio/capabilities.go`): dynamic per-provider param descriptor. Each provider exposes `Capabilities()` returning `[]ParamSchema` (type, range, default, dependsOn conditions, hidden flag) + `CustomFeatures` flags. UI reads `GET /v1/tts/capabilities` and renders param editors without hard-coded field lists.
- **Dual-read TTS storage**: tenant config read from both legacy flat keys (`tts.provider`, `tts.voice_id`, …) and new params blob (`tts.<provider>.params` JSON). Blob wins on conflict. Allows gradual migration; no data loss on downgrade.
- **`VoiceListProvider` interface** refactor: dynamic voice fetching (ElevenLabs, MiniMax) now via `ListVoices(ctx, ListVoicesOptions)` instead of per-provider ad-hoc methods. Unified `audio.Voice` type.
- **`POST /v1/tts/test-connection`**: ephemeral provider creation from request credentials + short synthesis smoke test. Returns `{ success, latency_ms }`. No provider registration; no config mutation. Operator role required.
- **`GET /v1/tts/capabilities`**: returns `ProviderCapabilities` JSON for all registered providers.

**i18n**

- Backend sentinel error keys (`MsgTtsGeminiInvalidVoice`, `MsgTtsGeminiInvalidModel`, `MsgTtsGeminiSpeakerLimit`, `MsgTtsParamOutOfRange`, `MsgTtsParamDependsOn`, `MsgTtsMiniMaxVoicesFailed`) in all 3 catalogs (EN/VI/ZH).
- HTTP 422 responses for Gemini sentinel errors now use `i18n.T(locale, key, args...)` — locale from `Accept-Language` header.
- ~80 param `label`/`help` keys across web + desktop locale files (EN/VI/ZH); parity enforced by `ui/web/src/__tests__/i18n-tts-key-parity.test.ts`.

**Security**

- SSRF guard on `api_base` override for test-connection (`validateProviderURL()`) — blocks `127.0.0.1` / `localhost` / RFC1918 ranges.

**Docs**

- `docs/tts-provider-capabilities.md` — schema reference + per-provider param tables + storage format + "Adding a new provider" checklist.
- `docs/codebase-summary.md` — TTS subsystem section documenting manager, providers, storage, endpoints.
