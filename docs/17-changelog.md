# Changelog

All notable changes to GoClaw Gateway are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

---

## [Unreleased]

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
