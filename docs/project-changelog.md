# Project Changelog

Significant changes, features, and fixes in reverse chronological order.

---

## 2026-06-13

### Cron NO_REPLY delivery suppression (issue #141)

**Fixes**

- Suppressed configured cron channel delivery when final agent output contains a
  standalone `NO_REPLY` sentinel anywhere in the text, while keeping normal chat
  silent-reply behavior unchanged.
- Added suppression logging with cron job/channel metadata for debugging.

**Tests**

- Added cron handler regression coverage for exact, prefix, suffix,
  mid-sentence, decorative, and glued-token `NO_REPLY` outputs.

---

## 2026-06-12

### Cron scheduler shutdown drain (post-merge CI follow-up)

**Fixes**

- Made cron service shutdown wait for scheduler-launched job executions to
  finish so in-flight status persistence cannot outlive `Stop()`.

**Tests**

- Added regression coverage proving `Stop()` waits for a blocked in-flight job
  before returning.

### Bundled GoClaw gateway administration skill (issue #175)

**Changes**

- Added a bundled `goclaw` system skill for version-aware GoClaw CLI/runtime
  discovery, read-only-first diagnostics, gateway administration workflows, and
  troubleshooting playbooks.
- Documented safety guidance for credentials, destructive actions,
  permissions, tenant scope, and trace redaction.

**Tests**

- Added bundled-skill regression coverage to keep the default core skill set
  discoverable in fresh runtimes.

### Bailian Coding qwen3.7-plus catalog (issue #169)

**Changes**

- Added `qwen3.7-plus` / `Qwen 3.7 Plus` to the hardcoded Bailian
  Coding provider model catalog.
- Documented the model's advertised Text Generation, Deep Thinking, and Visual
  Understanding capabilities while keeping Bailian on the existing
  OpenAI-compatible provider wrapper.

**Tests**

- Added provider model endpoint coverage to verify Bailian exposes
  `qwen3.7-plus` without adding unsupported OpenAI reasoning metadata.

### Multi-attachment outbound delivery (issue #172)

**Changes**

- Extended `send_file` with `attachments[]` so agents can queue multiple
  existing workspace files in one tool call while preserving order and captions.
- Added caption propagation from tool result media through agent media results
  into outbound channel attachments.
- Added Telegram media-group delivery for compatible batches with 2-10 item
  chunks, plus ordered fallback for singleton, voice, oversized-image, or mixed
  incompatible cases.
- Added explicit channel batch capability metadata for Telegram, Discord, and
  ordered fallback channels.

**Tests**

- Added batch `send_file` regressions for happy path, duplicate rejection, and
  all-or-nothing delivered-state handling.
- Added Telegram media-group chunking tests for compatible grouping, max-size
  chunking, and non-groupable fallback.

### Operator trace CLI (issue #158)

**Changes**

- Added first-class `goclaw traces` operator commands to the main binary:
  `list`, `get`, `export`, `follow`, and `timeline`.
- Added remote client overrides with `--server` and `--token`; trace commands
  support trace-scoped output selection via `--output` / `-o`.
- Shared the URL/token resolver with WebSocket/RPC-backed admin commands so
  `sessions`, `cron`, and `pairing` can use the same remote gateway overrides.
- Kept trace commands as thin wrappers over existing HTTP endpoints so the
  server/runtime and operator CLI share one binary without new API contracts.

**Tests**

- Added command/client regressions for gateway URL and token overrides, trace
  query serialization, follow scope validation, and timeline run ID handling.

### Skill self-evolution metrics and upgrades (issue #161, issue #142 foundation)

**Features**

- Added per-skill self-evolution settings, usage metrics, suggestions, immutable
  applied-version records, and activity logs.
- Added trusted runtime usage recording for `use_skill` and slash skill
  activations without exposing a public usage-write endpoint.
- Added skill evolution HTTP, CLI, and Web UI surfaces for settings, metrics,
  suggestions, approval/apply actions, and admin-visible activity.
- Added the shared foundation needed by self-improving skills: usage evidence,
  reference-file patches, versioned apply, and approval/audit surfaces. The
  consolidation learning extractor and auto user-scoped overlays remain out of
  this v1 scope.

**Safety**

- Blocked direct system skill mutation in the suggestion apply path.
- Reused a shared target-path validator for skill companion/reference patches.
- Sanitized failure evidence, draft patches, actor IDs, and activity details
  away from non-admin surfaces.

**Tests**

- Added focused skill path validator coverage and deep-link coverage for the new
  Web UI evolution tab.

### Skill lifecycle API and CLI (issue #159)

**Changes**

- Added per-skill dependency scan/check/install endpoints and CLI commands.
- Added access read/update, agent/user grant aliases, and effective-access
  inspection for operator workflows.
- Dependency status now reports system, pip, npm, and GitHub release deps in
  structured output.
- Skill user grants are now tenant-scoped so shared system skills can be
  granted to the same user ID in different tenants without conflicts.

**Tests**

- Added HTTP, CLI, PostgreSQL, and SQLite coverage for dependency lifecycle,
  access mode changes, grants, effective access, grant-driven visibility, and
  tenant-scoped user grants.

### Mid-flight request preservation (issue #137)

**Fixes**

- Busy-session `chat.send` follow-ups that arrive after a no-tool final answer
  now force one more pipeline iteration instead of finalizing the earlier answer.
- The earlier final answer is preserved as assistant context and the follow-up is
  appended after it, so the next model turn can answer both accepted requests.

**Tests**

- Added pipeline regressions for late injected follow-ups after a final answer.

---

## 2026-06-11

### Trace search and advanced filters (issue #152)

**Changes**

- Extended `GET /v1/traces` with keyword search, partial Trace ID search, date
  ranges, token ranges, tool-call filters, and agent/channel label search.
- Added equivalent PostgreSQL and SQLite query-builder behavior with tenant
  guards on joined channel/span search.
- Added Web Traces search/filter controls with active filter chips.

**Tests**

- Added HTTP contract coverage for advanced trace filter parsing and user scope.
- Added PostgreSQL and SQLite where-builder coverage for search, ranges, tool
  filters, tenant predicates, and wildcard escaping.
- Added Web filter serialization and trace i18n key coverage.

### Secure CLI GitHub credential runtime diagnostics (issues #138, #151)

**Fixes**

- Git remote commands now fail closed before raw `git` auth prompts when no
  typed host-scoped PAT/SSH credential is selected.
- Required SecureCLI env validation now applies to preset env vars such as
  `GH_TOKEN`, so credentialed `gh` commands report missing credential config
  instead of raw `gh auth login` guidance.

**Tests**

- Added regressions for `git push` without typed credentials and `gh` without
  `GH_TOKEN`.

---

## 2026-06-09

### Behavior UX sidecar delivery overrides (issue #144)

**Changes**

- Removed user-facing Tool Status Messages from Behavior settings and disabled
  deterministic tool-status channel text.
- Added sidecar-generated Quick Acknowledgement and Intermediate Replies with
  optional provider/model, timeout, token, and char caps.
- Added Channel > Agent > Workspace delivery-behavior resolution. Agent
  overrides use `other_config.delivery_behavior` without a schema migration.
- Kept legacy `block_reply` readable as a default for Intermediate Replies while
  removing its separate Web UI controls.
- Kept Show Reasoning as a separate debug/testing feature.

**Tests**

- Added config resolution coverage for Channel > Agent > Workspace and Quick
  Ack independent from Intermediate Replies.
- Added channel event coverage for sidecar quick ack, sidecar tool progress, and
  retired tool-status messages.
- Added Web UI schema coverage for sidecar delivery override fields.

---

## 2026-06-01

### Telegram Show Reasoning delivery modes (issues #132, #133)

**Fixes**

- Added `reasoning_delivery=streaming_only|always_bubbles|off` with backward compatibility for legacy `reasoning_stream`.
- HTTP and WebSocket channel-instance writes normalize explicit `reasoning_delivery` over legacy `reasoning_stream`.
- `always_bubbles` forces provider streaming internally so reasoning can be delivered as bounded channel bubbles even when Telegram live streaming is disabled.
- Stream-path final `resp.Thinking` is now emitted when no thinking chunk arrived during the stream.
- Terminal channel events preserve interim delivery state until the consumer reads the final dedup snapshot.

**UI**

- Web and desktop channel settings now expose Show Reasoning as a mode selector instead of a streaming-only boolean.

**Tests**

- Added channel delivery, config resolution, and agent stream final-thinking coverage.

---

## 2026-05-31

### CI/CD: zuey release asset race fix

**Fixes**

- Made release artifact completion wait for zuey beta deploy before refreshing GitHub Release assets with `--clobber`.
- Prevents the VPS upgrade script from seeing `linux amd64 release asset not found` while another workflow job is replacing the same asset.

**Tests**

- Updated the dev beta workflow structure test to lock the deploy-before-completion dependency.

---

### CI/CD: fast zuey beta deploy (issue #88)

**Changes**

- Split the dev beta release workflow so zuey deploy starts after the linux amd64 prerelease asset is published, instead of waiting for Docker multi-arch builds and beta alias promotion.
- Added stale beta tag guards for zuey deploy and Docker beta alias promotion so older runs cannot roll back a newer beta.
- Kept linux arm64 binaries, refreshed checksums, multi-arch Docker images, and beta aliases as required post-deploy completion jobs.

**Tests**

- Added a workflow-structure test covering the fast deploy graph, artifact completion graph, and stale-tag guards.

---

### Provider fallback content-policy recovery

**Fixes**

- Classifies provider content-policy rejections such as DashScope `data_inspection_failed` so model fallback can continue to the next configured candidate instead of stopping on `unknown`.
- Prevents Telegram runs from aborting silently when one fallback provider rejects a long/private history but another configured fallback remains available.

**Tests**

- Added provider classifier and model fallback coverage for continuing past a content-policy fallback failure.

---

### Empty Codex OAuth truncation recovery

**Fixes**

- Detects successful LLM calls that still return `finish_reason=length` with no text/tool/image output, the observed failure mode when long Codex OAuth runs spend the remaining budget on reasoning.
- Runs emergency history compaction and retries instead of finalizing empty assistant content into repeated `"..."` replies.
- Preserves configured/model context windows so future provider context upgrades do not require code changes.

**Tests**

- Added ThinkStage coverage for empty length responses, retry compaction, usage accounting, and repeated failure handling.

---

### Agent Access git credential follow-up (issue #117)

**Fixes**

- Replaced separate Agent Grants and Agent Credentials row actions with one
  Agent Access dialog containing Credential and Access policy tabs, preventing
  overlapping agent-access modals.
- Git PAT credentials now inject GitHub-compatible Basic auth extraheaders
  instead of Bearer headers.
- SSH private keys are now checked with OpenSSH at save time when `ssh-keygen`
  is available, catching keys that would later fail with `error in libcrypto`.

**Security**

- Git PAT redaction now includes the raw token, the base64 Basic auth payload,
  and the full injected header value.

---

### Tool-call announcements

**Fixes**

- Intermediate Replies now preserve natural assistant progress text as-is
  instead of appending a deterministic tool-name sentence.
- Empty-content tool calls no longer emit synthetic server-generated
  Intermediate Reply bubbles. This prevents repeated template messages and
  keeps visible progress model-generated.
- Tool-call progress keeps the `tool_announcement` source when the model does
  provide assistant text, so Quick acknowledgement suppression still treats it
  as explicit progress.
- The full-mode system prompt now tells the LLM to write any short progress
  sentence naturally in the user's language and to describe user-visible action
  instead of internal tool names.
- Quick acknowledgement off still suppresses generic first acknowledgements,
  but no longer suppresses explicit model-generated tool progress.

**Tests**

- Added pipeline coverage for empty-content tool calls so they do not emit
  template `block.reply` messages.
- Added prompt coverage for natural, user-language progress guidance.
- Kept channel coverage for `tool_announcement` delivery when Quick
  acknowledgement is off.

---

### Channel intermediate reply gating

**Fixes**

- Quick acknowledgement off now suppresses the first pre-tool `block.reply`
  even when explicit `gateway.block_reply` is enabled, so the initial
  acknowledgement does not leak through the Intermediate Replies path.
- Final reply dedup now uses channel-delivered interim state instead of raw
  pipeline `block.reply` emit counts, avoiding false suppression when an
  interim event was skipped.

**Tests**

- Added channel event coverage for quick acknowledgement disabled and
  `quick_ack.mode = "off"` with explicit intermediate replies enabled.

---

### Agent-scoped git credentials (issue #117)

**New**

- Added agent-scoped Secure CLI credentials with PostgreSQL migration `000077`
  and SQLite schema version `46`.
- Added HTTP APIs under
  `/v1/cli-credentials/{id}/agent-credentials/{agentId}` for listing,
  reading metadata, saving, and deleting agent credentials.
- Web CLI Credentials now exposes Agent Credentials as the primary git PAT/SSH
  setup path, with User Credentials renamed to advanced personal overrides.

**Security**

- Runtime credential precedence is now user override, context credential, agent
  credential, then binary env defaults.
- Git adapter audit logs include `credential_source` without logging raw
  secrets or plaintext host scopes.

---

## 2026-05-29

### Passive channel memory extraction (issue #64)

**New**

- Added opt-in per-channel passive memory extraction from pending group buffers, with scheduled/manual runs, redaction before LLM extraction, review queue items, and deterministic channel memory source IDs.
- Added PostgreSQL migration `000076` and SQLite schema version `45` for `channel_memory_extraction_runs` and `channel_memory_extraction_items`.
- Added tenant-admin HTTP APIs and channel detail UI controls for settings, manual run, last run status, and approve/reject/delete review actions.

**Security**

- Feature is disabled by default and group-only in v1.
- Review mode is on by default; approved items write to `episodic_summaries` with `source_type='channel'` and then enter the existing KG consolidation event path.
- Delete removes the review item and associated episodic summary when present. Existing KG nodes created before deletion may require later dedup/cleanup; raw channel messages are not copied into the new extraction tables.

---

### Channel context admin surface (issue #66)

**New**

- Added channel context APIs under `/v1/channels/instances/{id}/contexts` for stored channel/group contexts, members, and effective MCP/Secure CLI capability matrix.
- Added tenant-scoped context grants and context credentials for MCP servers and Secure CLI binaries, with PostgreSQL migration `000075` and SQLite schema version `44`.
- Runtime tool execution now carries `ChannelContextScope`; MCP and Secure CLI resolution apply context grants/credentials before user credential overrides.
- Web dashboard channel detail page now has a `Contexts` tab showing contexts, stored members, effective granted tools, and scoped credential presence without exposing secret values.

**Security**

- Context capability write APIs use tenant-admin checks.
- Credential APIs return presence/metadata only; raw MCP API keys, headers/env values, and Secure CLI env values are never serialized.
- MCP grant-check cache key includes channel scope so scoped grants do not leak across channel contexts.

---

### Group chat context in agent prompts

**Features**

- Added `## Current Chat Context` to agent system prompts for channel runs. Group chats now show platform, chat type, optional group name, group ID, and sender identity when metadata is available.
- Preserved existing `<current_reply_target>` routing guard and group reply guidance while making the human-readable chat context explicit.
- Added best-effort Discord channel title forwarding from the local `discordgo.State` cache. No hot-path REST lookup or schema change.

**Fixes**

- Normalized WhatsApp `user_name` metadata into the existing sender-name resolver so WhatsApp group prompts can include sender display names.
- Sanitized group titles and sender display names before prompt rendering to strip quotes/control whitespace and cap length.

**Tests**

- Added prompt contract coverage for group title, missing title, direct chats, and prompt-injection-shaped metadata.
- Added Discord cached-channel-title and sender-name resolver coverage.

---

### Archived run timeline (issue #76)

Adds persisted run archive timeline entries for session detail review.

**New**

- `run_timeline_items` schema for PostgreSQL and SQLite/Lite, with tenant/run
  sequence uniqueness and session/run indexes.
- Best-effort agent event recorder for `activity`, `assistant.message`,
  `tool.call`, `tool.result`, and `run.status` entries.
- HTTP `GET /v1/runs/{runID}/timeline` and WS `run.timeline.get` read APIs,
  including tenant/user visibility filtering.
- Web session detail timeline panel using the WS API and localized labels.

**Security**

- Tool arguments/results are persisted as bounded previews only.
- Raw thinking/reasoning is not persisted.
- Non-admin reads are filtered to the connected/effective user.

**Tests**

- Added store coverage for PG and SQLite, recorder tests, HTTP/WS API tests,
  permission classification coverage, and UI display mapping tests.

---

### Shell security group disabled state persistence (issue #75)

**Fixes**

- Added regression coverage proving `config.patch` preserves explicit
  `tools.shellDenyGroups` values set to `false` in memory, saved config JSON,
  and follow-up `config.get` responses.
- Aligned Claude CLI and ACP provider runtime registration with the saved
  shell deny-group config. Provider-side deny patterns now derive from
  `tools.ResolveDenyPatterns(cfg.Tools.ShellDenyGroups)` instead of static
  defaults, so disabled groups stay disabled after reload/provider registration.
- Re-registers existing Claude CLI/ACP provider runtimes on config changes so
  settings-page saves affect runtime enforcement without restart.
- Uses locked shell deny-group snapshots before HTTP provider registration, so
  provider creation cannot race config reload while reading the override map.
- Kept missing shell deny-group keys as inherited defaults; only explicit
  `false` disables a group.

**Tests**

- `go test ./internal/gateway/methods -run 'TestConfigPatchPersists(InboundDebounceMs|ShellDenyGroupsFalse)' -count=1`
- `go test ./internal/providers -run 'TestGenerateHookScript' -count=1`
- `go test ./internal/providers/... -run 'ClaudeCLI|ACP|DenyPatterns|ToolBridge' -count=1`
- `go test -race ./cmd -run 'TestShellDenyGroupsConfigReload_.*|TestConfiguredShellDenyPatternsDropsDisabledPackageInstall' -count=1`
- `go vet ./...`
- `go build ./...`
- `go build -tags sqliteonly ./...`

---

### Local-first document text extraction adapter (read_document privacy optimization)

Adds optional local extraction pipeline to the `read_document` tool, allowing
PDF and DOCX parsing via `pdftotext` (poppler-utils) and `pandoc` before
falling back to cloud vision. Reduces token costs and improves privacy for
document analysis when local binaries are available.

**New**

- `DocumentParser` interface + `LocalExtractParser` implementation in
  `internal/tools/document_parser.go`. Handles PDF (`pdftotext`) and DOCX
  (`pandoc`) with no-shell subprocess exec, binary detection, process-group
  timeout kill (SIGTERM → 3s grace → SIGKILL), minimal subprocess environment,
  and 500KB output limit.
- `DocumentParserConfig` in `internal/config/config_channels.go` under `tools` section:
  `local_first` (opt-in, default false), `max_pages` (default 200), `timeout_sec`
  (default 30), `min_text_len` (default 16). Config is read at tool construction;
  binary availability re-checked per call for runtime installs.
- Integration in `read_document` tool: `SetLocalParser()` wired at gateway
  startup (`cmd/gateway_managed.go`). On a clean extraction hit, returns text
  with no LLM usage (zero tokens, no Provider/Model/Usage fields). Any miss
  (disabled, unsupported mime, missing binary, timeout, empty output, exec
  error) transparently falls back to the cloud vision chain unchanged.
- Security: subprocess exec uses `exec.Command` (no shell), docPath validated
  at the exec boundary, extracted text treated as untrusted, minimal env
  (no gateway secrets), `pandoc --sandbox` for DOCX parsing.

**Requirements**

- Requires `pdftotext` and `pandoc` on PATH for local extraction. Present in:
  - Docker `full` variant (`ghcr.io/nextlevelbuilder/goclaw:vX.Y.Z-full`)
  - Builds with `-tags "" -X main.enableFullSkills=true` (when available)
- Desktop Lite edition: local extraction wired but not user-configurable
  (remains in code for future mobile/CLI usage; server/Docker only now).

**Docs**

- `docs/03-tools-system.md` § 5 — new `document_parser` config block with all
  four fields, defaults, and fallback semantics.
- `docs/03-tools-system.md` § 4 (Media Reading) — updated `read_document`
  description to mention local-first extraction option.

**Testing**

- Behavior unchanged when `local_first: false` (default).
- Verify local extraction when enabled: create test PDF/DOCX, enable config,
  inspect tool output for zero-token response vs cloud vision (Gemini, etc.).

---

### Skill selected download export (issue #80)

**Features**

- Extended `GET /v1/skills/export` with selected skill IDs via repeated `id`
  or comma-separated `ids`, while preserving the no-selection full export as
  tenant custom-skill only by default.
- Added archive format selection: `tar.gz`, `tgz` alias, and `zip`. Direct and
  SSE download paths now return matching archive filenames and content types.
- Selected exports can include system/core skills explicitly without changing
  full backup defaults.
- Archive output now preserves skill directory content such as `SKILL.md`,
  `references/`, `scripts/`, and `assets/`, while skipping unsafe paths and
  symlinks. Nested files named `metadata.json` or `grants.jsonl` remain
  exportable; only generated root archive artifacts are skipped.
- Archive assembly streams file contents into the writer and revalidates opened
  files against the resolved skill root to avoid large in-memory reads and
  symlink-swap escapes.
- Web Skills page now supports Download from the detail dialog and Download
  selected from the bulk toolbar, with format selection and EN/VI/ZH labels.

**Tests**

- Added backend coverage for export request parsing, archive writers, safe
  directory walking, selected system/custom scope, and full export defaults.
- Added web helper coverage for selected export URLs and archive filenames.

---

### Discord thread history and attachment backfill (issue #69)

**Fixes**

- Discord thread mentions now fetch recent thread messages before the triggering mention and prepend them as context for the agent run.
- Prior thread image and document attachments are downloaded immediately and passed through the existing inbound media pipeline, so `read_image` and `read_document` can use files posted earlier in the thread.
- Backfill is limited to addressed Discord threads, 25 prior messages, 15 attachments, 5 MB per backfilled file, and a 30-second timeout. Missing Discord history permission or REST failures fall back to the current message without crashing.

---

### Telegram voice transcription and read_audio fallback (issue #85)

**Fixes**

- Telegram voice/audio STT now preserves Telegram's detected MIME type, including `audio/ogg; codecs=opus`, instead of forcing every STT request to `audio/ogg`.
- Telegram STT now resolves channel-scoped legacy proxy overrides by platform type `telegram`, so DB channel instances with custom names still select the Telegram override.
- `read_audio` no longer sends unsupported audio routes as `providers.ImageContent`; unsupported provider/model combinations now fail closed with an audio-specific error that names supported routes.

**Tests**

- Added Telegram STT regression coverage for channel-type override selection and MIME preservation.
- Added `read_audio` regression coverage proving unsupported providers are not called through chat/image fallback.

---

### RapidAPI cron SecureCLI diagnostics (issue #74)

**Fixes**

- Added a built-in `rapidapi` SecureCLI preset with required `RAPIDAPI_KEY`, 60s timeout, and verbose/debug flag denials.
- Credentialed exec now validates required preset env keys before binary resolution, so missing RapidAPI credentials return a GoClaw diagnostic instead of downstream `RAPIDAPI_KEY required`.
- Added safe SecureCLI env diagnostics that log env key names/count context only, never credential values.
- Added `RAPIDAPI_KEY` to fall-through exec env scrubbing.

**Tests**

- Added RapidAPI preset contract and deny-pattern coverage.
- Added credentialed exec regressions for missing RapidAPI env and successful direct exec with injected `RAPIDAPI_KEY`.

---

### Sandbox tenant workspace isolation (issue #68)

**Security**

- Scoped Docker sandbox workspace mounts to the effective tenant/session workspace from tool context instead of the global workspace root.
- Kept the in-container UX stable: the effective workspace is mounted at `/workspace`.
- Made Docker sandbox reuse workspace/config-aware so shared or agent-scoped containers cannot cross workspace boundaries.
- Added fail-closed behavior when tenant-scoped sandbox execution has no effective workspace.

**Tests**

- Added unit coverage for effective sandbox workspace selection, `/workspace` cwd mapping, normal and credentialed sandbox exec, file-tool sandbox bridge mount selection, and Docker cache-key isolation.

---

### Human-like channel delivery MVP (issue #67)

**New**

- Added `gateway.chat_behavior` runtime config for global quick acknowledgement and safe final multi-message splitting.
- Added per-channel `chat_behavior` override support for channel instances that already participate in channel delivery settings.
- Quick acknowledgements are emitted only for non-streaming channel runs and are cancelled when a block reply or terminal event arrives.
- Final splitting applies only to non-streaming text-only final replies; unsafe Markdown, code, tables, lists, quotes, JSON, and URL-only paragraphs stay as one message.
- Added `chat_behavior.preview` RPC plus dashboard controls and per-channel override fields.

**Validation**

- Added Go coverage for config resolution, preview, conservative splitting, and non-streaming quick acknowledgement delivery.
- Verified focused Go packages, both Go builds, `go vet`, web Vitest, web production build, and `git diff --check`.

**Out of scope**

- No archive/timeline storage, renderer, share/export, or interleaved run history changes. Those remain issue #76 scope.

---

### GitHub Releases update scratch dir fallback (issue #94)

- Changed GitHub Releases package updates to prefer `{runtimeDir}/tmp` for
  scratch extraction and staging, instead of deriving tmp from a release or
  binary directory.
- If `packages.scratch_dir` is configured but cannot be created, the update
  executor now logs a warning and falls back to runtime tmp before failing.
- Added regression tests for default scratch-dir selection and fallback from an
  unusable configured scratch path.

---

### CLI Credentials git preset null-env crash (issue #93)

**Fixes**

- Stopped `/v1/cli-credentials/presets` from returning nullable preset arrays
  for adapter-managed CLIs such as `git`; frontend now also normalizes older
  `null` payloads defensively.
- Allowed the `git` preset to create a SecureCLI binary without legacy env
  vars and persisted its `adapter_name=git`, so the PAT/SSH user credential
  flow activates after creation.
- Guarded the preset env-var renderer against nullish `env_vars` to keep the
  Runtime & Packages → CLI Credentials tab renderable.

**Tests**

- Added backend regression coverage for stable preset arrays and git preset
  creation without legacy env.
- Added frontend normalization coverage for nullable adapter-managed preset
  arrays.

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
