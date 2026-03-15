# Changelog

All notable changes to GoClaw Gateway are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

### Added

#### Credentialed Exec — Secure CLI Credential Injection
- **New feature**: Direct Exec Mode for CLI tools with auto-injected credentials (GitHub, Google Cloud, AWS, Kubernetes, Terraform)
- **Security model**: No shell involved — credentials injected directly into process env; 4-layer defense (no shell, path verify, deny patterns, output scrub)
- **Presets**: 5 built-in binary configurations (gh, gcloud, aws, kubectl, terraform)
- **Database**: Migration 000019 adds `secure_cli_binaries` table for credential storage (encrypted with AES-256-GCM)
- **Tool integration**: ExecTool routes credentialed binaries to `executeCredentialed()` path, bypassing shell
- **HTTP API endpoints**:
  - `GET /v1/cli-credentials` — List all credentials
  - `POST /v1/cli-credentials` — Create credential
  - `GET /v1/cli-credentials/{id}` — Retrieve credential
  - `PUT /v1/cli-credentials/{id}` — Update credential
  - `DELETE /v1/cli-credentials/{id}` — Delete credential
  - `GET /v1/cli-credentials/presets` — Get preset templates
  - `POST /v1/cli-credentials/{id}/test` — Dry run with test command
- **Web UI**: Credential manager with preset selector, environment variable editor, dry run tester
- **Files added**:
  - `internal/tools/credentialed_exec.go` — Direct exec, shell operator detection, path verification
  - `internal/tools/credential_context.go` — Context injection helpers
  - `internal/store/secure_cli_store.go` — Store interface
  - `internal/store/pg/secure_cli.go` — PostgreSQL implementation
  - `internal/http/secure_cli.go` — HTTP endpoints
  - `migrations/000019_secure_cli_binaries.up.sql` — Database schema

#### API Key Management
- **Multi-key auth**: Multiple API keys with `goclaw_` prefix, SHA-256 hashed storage, show-once pattern
- **RBAC scopes**: `operator.admin`, `operator.read`, `operator.write`, `operator.approvals`, `operator.pairing`
- **HTTP + WS**: Full CRUD via `/v1/api-keys` and `api_keys.*` RPC methods
- **Web UI**: Create dialog with scope checkboxes, expiry options, revoke confirmation
- **Migration**: `000020_api_keys` — `api_keys` table with partial index on active key hashes
- **Backward compatible**: Existing gateway token continues to work as admin

#### Interactive API Documentation
- **Swagger UI** at `/docs` with embedded OpenAPI 3.0 spec at `/v1/openapi.json`
- **Coverage**: 130+ HTTP endpoints across 18 tag groups
- **Sidebar link**: API Docs entry in System group (opens in new tab)

### Documentation

- Added `18-http-api.md` — Complete HTTP REST API reference (all endpoints, auth, error codes)
- Added `19-websocket-rpc.md` — Complete WebSocket RPC method catalog (64+ methods, permission matrix)
- Added `20-api-keys-auth.md` — API key authentication, RBAC scopes, security model, usage examples

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
