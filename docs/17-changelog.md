# Changelog

All notable changes to GoClaw Gateway are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added

#### Runtime & Packages Management (2026-03-17)
- **Packages page**: New "Packages" page in Web UI under System group for managing installed packages
- **HTTP API endpoints**: GET/POST `/v1/packages`, `/v1/packages/install`, `/v1/packages/uninstall`, GET `/v1/packages/runtimes`
- **Three package categories**: System (apk), Python (pip), Node (npm) with version tracking
- **pkg-helper binary**: Root-privileged helper service for secure system package management via Unix socket `/tmp/pkg.sock`
- **Package persistence**: System packages persisted to `/app/data/.runtime/apk-packages` for container recreation
- **Input validation**: Regex + MaxBytesReader (4096 bytes) for package names to prevent injection

#### Docker Security Hardening (2026-03-17)
- **Privilege separation**: Entrypoint drops privileges to non-root goclaw user after installing packages
- **pkg-helper service**: Started as root, listens on Unix socket with 0660 permissions (root:goclaw group)
- **Runtime directories**: Python and Node.js packages install to writable `/app/data/.runtime` directories
- **su-exec integration**: Used instead of USER directive for cleaner privilege transition
- **Docker capabilities**: Added SETUID/SETGID/CHOWN/DAC_OVERRIDE for pkg-helper and user switching
- **Environment variables**: PIP_TARGET, NPM_CONFIG_PREFIX, PYTHONPATH configured for runtime installs

#### Auth Fix (2026-03-17)
- **Empty gateway token handling**: When GOCLAW_GATEWAY_TOKEN is empty (dev/single-user mode), all requests get admin role
- **CLI credentials access**: Admin-only endpoints (/v1/cli-credentials) now accessible in dev mode

#### Team Workspace Improvements (2026-03-16)
- **Team workspace resolution**: Lead agents resolve per-team workspace directories for both lead and member agents
- **WorkspaceInterceptor**: Transparently rewrites file tool requests to team workspace context
- **File tool access**: Member agents can access workspace files with automatic path resolution
- **Team workspace UI**: Workspace scope setting UI, file view/download, storage depth control
- **Lazy folder loading**: Improved performance with lazy-load folder UI and SSE size endpoint
- **Task enhancements**: Task snapshots in board view, task delete action, improved task dispatch concurrency
- **Board toolbar**: Moved workspace button and added agent emoji display
- **Status filter**: Default status filter changed to all with page size reduced to 30

#### Agent & Workspace Enhancements (2026-03-16)
- **Agent emoji**: Display emoji icon from `other_config` in agent list and detail views
- **Lead orchestration**: Improved leader orchestration prompt with better team context
- **Task blocking validation**: Validate blocked_by terminal state to prevent circular dependencies
- **Prevent premature task creation**: Team V2 leads cannot manually create tasks before spawn

#### Team System V2 & Task Workflow (2026-03-13 - 2026-03-15)
- **Kanban board layout**: Redesigned team detail page with visual task board
- **Card/list toggle**: Teams list with card/list view toggle
- **Member enrichment**: Team member info enriched with agent metadata
- **Task approval workflow**: Approve/reject/cancel tasks with new statuses and filtering
- **Workspace scope**: Per-agent DM/group/user controls with workspace sharing configuration
- **i18n for channels**: Channel config fields now support internationalization
- **Memory/KG sharing**: Decoupled memory and KG sharing from workspace folder sharing
- **Events API**: New /v1/teams/{id}/events endpoint for task lifecycle events

#### Security & Pairing Hardening (2026-03-16)
- **Browser approval fix**: Fixed browser approval stuck condition
- **Pairing auth hardening**: Fail-closed auth, rate limiting, TTL enforcement for pairing codes
- **DB error handling**: Handle transient DB errors in IsPaired check
- **Transient recovery**: Prevent spurious pair requests

#### Internationalization (i18n) Expansion (2026-03-15)
- **Complete web UI localization**: Full internationalization for en/vi/zh across all UI components
- **Config centralization**: Centralized hardcoded ~/.goclaw paths via config resolution
- **Channel DM streaming**: Enable DM streaming by default with i18n field support

#### Provider Enhancements (2026-03-14 - 2026-03-16)
- **Qwen 3.5 support**: Added Qwen 3.5 series support with per-model thinking capability
- **Anthropic prompt caching**: Corrected Anthropic prompt caching implementation
- **Anthropic model aliases**: Model alias resolution for Anthropic API
- **Datetime tool**: Added datetime tool for provider context
- **DashScope per-model thinking**: Simplified per-model thinking guard logic
- **OpenAI GPT-5/o-series**: Use max_completion_tokens and skip temperature for GPT-5/o-series models

#### ACP Provider (2026-03-14)
- **External coding agents**: ACP provider for orchestrating external agents (Claude Code, Codex CLI, Gemini CLI) as JSON-RPC subprocesses
- **ProcessPool management**: Subprocess lifecycle with idle TTL reaping and crash recovery
- **ToolBridge**: Agent→client requests for filesystem operations and terminal spawning
- **Workspace sandboxing**: Security features with deny pattern matching and permission modes
- **Streaming support**: Both streaming and non-streaming modes with context cancellation

#### Storage & Media Enhancements (2026-03-14)
- **Lazy folder loading**: Lazy-load folder UI for improved performance
- **SSE size endpoint**: Server-sent events endpoint for dynamic size calculation
- **Enhanced file viewer**: Improved file viewing capabilities with media preservation
- **Web fetch enhancement**: Increased limit to 60K with temp file save for oversized content
- **Discord media enrichment**: Persist media IDs for Discord image attachments

#### Knowledge Graph Improvements (2026-03-14)
- **LLM JSON sanitization**: Sanitize LLM JSON output before parsing to handle edge cases

#### CI/CD & Release Pipeline (2026-03-16)
- **Semantic release**: Automated versioning via `go-semantic-release` on push to `main`
- **Cross-platform binaries**: Build and attach `linux/darwin × amd64/arm64` tarballs to GitHub Releases
- **Discord webhook notification**: Post release embed to Discord with changelog, version, Docker pull command, and install script link after successful build
- **Install scripts**: One-liner binary installer (`scripts/install.sh`) and interactive Docker setup (`scripts/setup-docker.sh`) with variant selection (alpine/node/python/full)
- **Docker image publishing**: Publish multi-arch images to GHCR and Docker Hub via GitHub Actions

#### Traces & Observability (2026-03-16)
- **Trace UI improvements**: Added timestamps, copy button, syntax highlighting to trace/span views
- **Trace export**: Added gzip export with recursive sub-trace collection

#### Skills & System Tools (Previous releases)
- **System skills**: Toggle, dependency checking, per-item installation
- **Tool aliases**: Alias registry for Claude Code skill compatibility
- **Multi-skill upload**: Client-side validation for bulk skill uploads
- **Audio handling**: Fixed media tag enrichment and literal <media:audio> handling

#### Credential & Configuration (Previous releases)
- **Credential merge**: Handle DB errors to prevent silent data loss
- **OAuth provider routing**: Complete media provider type routing for Suno, DashScope, OAuth providers
- **API base resolution**: Respect API base when listing Anthropic models
- **Per-agent DB settings**: Honor per-agent restrictions, subagents, memory, sandbox, embedding provider settings

### Changed

- **Docker entrypoint**: Reimplemented for privilege separation with pkg-helper lifecycle management
- **Team workspace refactor**: Removed legacy `workspace_read`/`workspace_write` tools in favor of file tools for team workspace
- **Config hardcoding**: Centralized ~/goclaw paths via config resolution instead of hardcoded values
- **Workspace media files**: Preserve workspace media files during subtree lazy-loading

### Fixed

- **Teams status filter**: Default to all statuses instead of subset, reduced page size to 30
- **Select crash**: Filter empty chat_id scopes to prevent dropdown crash
- **File viewer**: Improved workspace file view/download and storage depth control
- **Pairing DB errors**: Handle transient errors gracefully
- **Provider thinking**: Corrected DashScope per-model thinking logic

### Documentation

- Updated `18-http-api.md` — Added section 17 for Runtime & Packages Management endpoints
- Updated `09-security.md` — Added Docker entrypoint documentation, pkg-helper architecture, privilege separation
- Updated `17-changelog.md` — New entries for packages management, Docker security, and auth fix
- Added `18-http-api.md` — Complete HTTP REST API reference (all endpoints, auth, error codes)
- Added `19-websocket-rpc.md` — Complete WebSocket RPC method catalog (64+ methods, permission matrix)
- Added `20-api-keys-auth.md` — API key authentication, RBAC scopes, security model, usage examples
- Updated `02-providers.md` — ACP provider documentation with architecture, configuration, security model
- Updated `00-architecture-overview.md` — Added ACP provider component and module references

---

## [ACP Provider Release]

### Added

#### ACP Provider (Agent Client Protocol)
- **New provider**: ACP provider enables orchestration of external coding agents (Claude Code, Codex CLI, Gemini CLI) as JSON-RPC 2.0 subprocesses over stdio
- **ProcessPool**: Manages subprocess lifecycle with idle TTL reaping and automatic crash recovery
- **ToolBridge**: Handles agent→client requests for filesystem operations and terminal spawning with workspace sandboxing
- **Security features**: Workspace isolation, deny pattern matching, configurable permission modes (approve-all, approve-reads, deny-all)
- **Streaming support**: Both streaming and non-streaming modes supported with context cancellation
- **Config integration**: New `ACPConfig` struct in configuration with binary, args, model, work_dir, idle_ttl, perm_mode
- **Database providers**: ACP providers can be registered in `llm_providers` table with encrypted credentials
- **Files added**:
  - `internal/providers/acp_provider.go` — ACPProvider implementation
  - `internal/providers/acp/types.go` — ACP protocol types
  - `internal/providers/acp/process.go` — Process pool management
  - `internal/providers/acp/jsonrpc.go` — JSON-RPC 2.0 marshaling
  - `internal/providers/acp/tool_bridge.go` — Request handling
  - `internal/providers/acp/terminal.go` — Terminal lifecycle
  - `internal/providers/acp/session.go` — Session tracking

### Changed

- Updated `02-providers.md` to document ACP provider architecture, configuration, session management, security, and streaming
- Updated `00-architecture-overview.md` component diagram to include ACP provider
- Updated Module Map in architecture overview to reference `internal/providers/acp/` package

### Documentation

- Added comprehensive ACP provider documentation with architecture diagrams, configuration examples, security model, and file reference
- Added `17-changelog.md` for tracking project changes

---

## [Previous Releases]

### v1.0.0 and Earlier

- Initial release of GoClaw Gateway with Anthropic and OpenAI-compatible providers
- WebSocket RPC v3 protocol and HTTP API
- PostgreSQL multi-tenant backend with pgvector embeddings
- Agent loop with think→act→observe cycle
- Tool system: filesystem, exec, web, memory, browser, MCP bridge, custom tools
- Channel adapters: Telegram, Discord, Feishu, Zalo, WhatsApp
- Extended thinking support for Anthropic and select OpenAI models
- Scheduler with lane-based concurrency control
- Cron scheduling system
- Agent teams with task delegation
- Skills system with hot-reload
- Tracing and observability with optional OpenTelemetry export
- Browser automation via Rod
- Code sandbox with Docker
- Text-to-speech (OpenAI, ElevenLabs, Edge, MiniMax)
- i18n support (English, Vietnamese, Chinese)
- RBAC permission system
- Device pairing with 8-character codes
- MCP server integration with stdio, SSE, streamable-HTTP transports
