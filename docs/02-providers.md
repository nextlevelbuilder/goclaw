# 02 - LLM Providers

GoClaw abstracts LLM communication behind a single `Provider` interface, allowing the agent loop to work with any backend without knowing the wire format. Six concrete implementations exist: Anthropic (native HTTP+SSE), OpenAI-compatible (covering 10+ API endpoints), Claude CLI (local binary), Codex (OAuth-based), ACP (subagent orchestration), and DashScope (Alibaba Qwen with thinking).

---

## 1. Provider Architecture

All providers implement four methods: `Chat()`, `ChatStream()`, `Name()`, and `DefaultModel()`. The agent loop calls `Chat()` for non-streaming requests and `ChatStream()` for token-by-token streaming. Both return a unified `ChatResponse` with content, tool calls, finish reason, and token usage.

```mermaid
flowchart TD
    AL["Agent Loop"] -->|"Chat() / ChatStream()"| PI["Provider Interface"]

    PI --> ANTH["Anthropic Provider<br/>native net/http + SSE"]
    PI --> OAI["OpenAI-Compatible Provider<br/>generic HTTP client"]
    PI --> CLAUDE["Claude CLI Provider<br/>stdio subprocess"]
    PI --> CODEX["Codex Provider<br/>OAuth-based Responses API"]
    PI --> ACP["ACP Provider<br/>JSON-RPC 2.0 subagents"]
    PI --> DASH["DashScope Provider<br/>OpenAI-compat wrapper"]

    ANTH --> ANTHROPIC["Claude API<br/>api.anthropic.com/v1"]
    OAI --> OPENAI["OpenAI API"]
    OAI --> OR["OpenRouter API"]
    OAI --> GROQ["Groq API"]
    OAI --> DS["DeepSeek API"]
    OAI --> GEM["Gemini API"]
    OAI --> OTHER["Mistral / xAI / MiniMax<br/>Cohere / Perplexity / Ollama"]
    CLAUDE --> CLI["claude CLI binary<br/>stdio + MCP bridge"]
    CODEX --> CODEX_API["ChatGPT Responses API<br/>chatgpt.com/backend-api"]
    ACP --> AGENTS["Claude Code / Codex<br/>Gemini CLI agents"]
    DASH --> QWEN["Alibaba DashScope<br/>Qwen3 models"]
```

Authentication and timeouts vary by provider type:
- **Anthropic**: `x-api-key` header + `anthropic-version: 2023-06-01`
- **OpenAI-compatible**: `Authorization: Bearer` token
- **Claude CLI**: stdio subprocess (no auth; uses local CLI session)
- **Codex**: OAuth access token (auto-refreshed via TokenSource)
- **ACP**: JSON-RPC 2.0 over subprocess stdio
- **DashScope**: `Authorization: Bearer` token (inherits from OpenAI-compatible)

All HTTP-based providers (Anthropic, OpenAI-compatible, Codex) use 300-second timeout.

---

## 2. Supported Providers

### Six Core Provider Types

| Provider | Type | Configuration | Default Model |
|----------|------|----------|---------------|
| **anthropic** | Native HTTP + SSE | API key required | `claude-sonnet-4-5-20250929` |
| **claude_cli** | stdio subprocess + MCP | Binary path (default: `claude`) | `sonnet` |
| **codex** | OAuth Responses API | OAuth token source | `gpt-5.3-codex` |
| **acp** | JSON-RPC 2.0 subagents | Binary + workspace dir | `claude` |
| **dashscope** | OpenAI-compat wrapper | API key + custom models | `qwen3-max` |
| **openai** (+ 10+ variants) | OpenAI-compatible | API key + endpoint URL | Model-specific |

### OpenAI-Compatible Providers

| Provider | API Base | Default Model | Notes |
|----------|----------|---------------|-------|
| openai | `https://api.openai.com/v1` | `gpt-4o` | |
| openrouter | `https://openrouter.ai/api/v1` | `anthropic/claude-sonnet-4-5-20250929` | Model must contain `/` |
| groq | `https://api.groq.com/openai/v1` | `llama-3.3-70b-versatile` | |
| deepseek | `https://api.deepseek.com/v1` | `deepseek-chat` | |
| gemini | `https://generativelanguage.googleapis.com/v1beta/openai` | `gemini-2.0-flash` | Skips empty content fields |
| mistral | `https://api.mistral.ai/v1` | `mistral-large-latest` | |
| xai | `https://api.x.ai/v1` | `grok-3-mini` | |
| minimax | `https://api.minimax.io/v1` | `MiniMax-M2.5` | Uses custom chat path |
| cohere | `https://api.cohere.ai/compatibility/v1` | `command-a` | |
| perplexity | `https://api.perplexity.ai` | `sonar-pro` | |
| ollama | `http://localhost:11434/v1` | `llama3.3` | Local/configurable |
| bailian | `https://coding-intl.dashscope.aliyuncs.com/v1` | `qwen3.5-plus` | Alibaba Coding API |
| zai | `https://api.z.ai/api/paas/v4` | `glm-5` | |
| zai-coding | `https://api.z.ai/api/coding/paas/v4` | `glm-5` | |

---

## 3. Call Flow

### Non-Streaming (Chat)

```mermaid
sequenceDiagram
    participant AL as Agent Loop
    participant P as Provider
    participant R as RetryDo
    participant API as LLM API

    AL->>P: Chat(ChatRequest)
    P->>P: resolveModel()
    P->>P: buildRequestBody()
    P->>R: RetryDo(fn)

    loop Max 3 attempts
        R->>API: HTTP POST /messages or /chat/completions
        alt Success (200)
            API-->>R: JSON Response
            R-->>P: io.ReadCloser
        else Retryable (429, 500-504, network)
            API-->>R: Error
            R->>R: Backoff delay + jitter
        else Non-retryable (400, 401, 403)
            API-->>R: Error
            R-->>P: Error (no retry)
        end
    end

    P->>P: parseResponse()
    P-->>AL: ChatResponse
```

### Streaming (ChatStream)

```mermaid
sequenceDiagram
    participant AL as Agent Loop
    participant P as Provider
    participant R as RetryDo
    participant API as LLM API

    AL->>P: ChatStream(ChatRequest, onChunk)
    P->>P: buildRequestBody(stream=true)
    P->>R: RetryDo(connection only)

    R->>API: HTTP POST (stream: true)
    API-->>R: 200 OK + SSE stream
    R-->>P: io.ReadCloser

    loop SSE events (line-by-line)
        API-->>P: data: event JSON
        P->>P: Accumulate content + tool call args
        P->>AL: onChunk(StreamChunk)
    end

    P->>P: Parse accumulated tool call JSON
    P->>AL: onChunk(Done: true)
    P-->>AL: ChatResponse (final)
```

Key difference: non-streaming wraps the entire request in `RetryDo`. Streaming retries only the connection phase -- once SSE events start flowing, no retry occurs mid-stream.

---

## 4. Anthropic vs OpenAI-Compatible

| Aspect | Anthropic | OpenAI-Compatible |
|--------|-----------|-------------------|
| Base URL override | `WithAnthropicBaseURL()` option | Via config `api_base` field |
| Implementation | Native `net/http` | Generic HTTP client |
| System messages | Separate `system` field (array of text blocks) | Inline in `messages` array with `role: "system"` |
| Tool definitions | `name` + `description` + `input_schema` | Standard OpenAI function schema |
| Tool results | `role: "user"` with `tool_result` content block + `tool_use_id` | `role: "tool"` with `tool_call_id` |
| Tool call arguments | `map[string]interface{}` (parsed JSON object) | JSON string in `function.arguments` (manual marshal) |
| Tool call streaming | `input_json_delta` events | `delta.tool_calls[].function.arguments` fragments |
| Stop reason mapping | `tool_use` mapped to `tool_calls`, `max_tokens` mapped to `length` | Direct passthrough of `finish_reason` |
| Gemini compatibility | N/A | Skip empty `content` field in assistant messages with tool_calls |
| OpenRouter compatibility | N/A | Model must contain `/` (e.g., `anthropic/claude-...`); unprefixed falls back to default |

---

## 5. Retry Logic

### RetryDo[T] Generic Function

`RetryDo` is a generic function that wraps any provider call with exponential backoff, jitter, and context cancellation support.

### Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| Attempts | 3 | Total tries (1 = no retry) |
| MinDelay | 300ms | Initial delay before first retry |
| MaxDelay | 30s | Upper cap on delay |
| Jitter | 0.1 (10%) | Random variation applied to each delay |

### Backoff Formula

```
delay = MinDelay * 2^(attempt - 1)
delay = min(delay, MaxDelay)
delay = delay +/- (delay * jitter * random)

Example:
  Attempt 1: 300ms (+/-30ms)  -> 270ms..330ms
  Attempt 2: 600ms (+/-60ms)  -> 540ms..660ms
  Attempt 3: 1200ms (+/-120ms) -> 1080ms..1320ms
```

If the response includes a `Retry-After` header (HTTP 429 or 503), the header value completely replaces the computed backoff. The header is parsed as integer seconds or RFC 1123 date format.

### Retryable vs Non-Retryable Errors

| Category | Conditions |
|----------|------------|
| Retryable | HTTP 429, 500, 502, 503, 504; network errors (`net.Error`); connection reset; broken pipe; EOF; timeout |
| Non-retryable | HTTP 400, 401, 403, 404; all other status codes |

### Retry Flow

```mermaid
flowchart TD
    CALL["fn()"] --> OK{Success?}
    OK -->|Yes| RETURN["Return result"]
    OK -->|No| RETRY{Retryable error?}
    RETRY -->|No| FAIL["Return error immediately"]
    RETRY -->|Yes| LAST{Last attempt?}
    LAST -->|Yes| FAIL
    LAST -->|No| DELAY["Compute delay<br/>(Retry-After header or backoff + jitter)"]
    DELAY --> WAIT{Context cancelled?}
    WAIT -->|Yes| CANCEL["Return context error"]
    WAIT -->|No| CALL
```

---

## 6. Schema Cleaning

Some providers reject tool schemas containing unsupported JSON Schema fields. `CleanSchemaForProvider()` recursively removes these fields from the entire schema tree, including nested `properties`, `anyOf`, `oneOf`, and `allOf`.

| Provider | Fields Removed |
|----------|---------------|
| Gemini | `$ref`, `$defs`, `additionalProperties`, `examples`, `default` |
| Anthropic | `$ref`, `$defs` |
| All others | No cleaning applied |

The Anthropic provider calls `CleanSchemaForProvider("anthropic", ...)` when converting tool definitions to the `input_schema` format. The OpenAI-compatible provider calls `CleanToolSchemas()` which applies the same logic per provider name.

---

## 7. Providers from Database

Providers are loaded from the `llm_providers` table in addition to the config file. Database providers override config providers with the same name.

### Loading Flow

```mermaid
flowchart TD
    START["Gateway Startup"] --> CFG["Step 1: Register providers from config<br/>(Anthropic, OpenAI, etc.)"]
    CFG --> DB["Step 2: Register providers from DB<br/>SELECT * FROM llm_providers<br/>Decrypt API keys"]
    DB --> OVERRIDE["DB providers override<br/>config providers with same name"]
    OVERRIDE --> READY["Provider Registry ready"]
```

### API Key Encryption

```mermaid
flowchart LR
    subgraph "Storing a key"
        PLAIN["Plaintext API key"] --> ENC["AES-256-GCM encrypt"]
        ENC --> DB["DB column: 'aes-gcm:' + base64(nonce + ciphertext + tag)"]
    end

    subgraph "Loading a key"
        DB2["DB value"] --> CHECK{"Has 'aes-gcm:' prefix?"}
        CHECK -->|Yes| DEC["AES-256-GCM decrypt"]
        CHECK -->|No| RAW["Return as-is<br/>(backward compatibility)"]
        DEC --> USE["Plaintext key for provider"]
        RAW --> USE
    end
```

`GOCLAW_ENCRYPTION_KEY` accepts three formats:
- **Hex**: 64 characters (32 bytes decoded)
- **Base64**: 44 characters (32 bytes decoded)
- **Raw**: 32 characters (32 bytes direct)

---

## 8. Extended Thinking

Extended thinking allows LLMs to generate internal reasoning tokens before producing a response, improving quality for complex tasks. GoClaw supports this across multiple providers with a unified `thinking_level` configuration. See [12-extended-thinking.md](./12-extended-thinking.md) for full details.

### Provider Mapping

```mermaid
flowchart TD
    LEVEL["thinking_level"] --> CHECK{"Provider<br/>supports thinking?"}
    CHECK -->|No| SKIP["Skip — normal request"]
    CHECK -->|Yes| TYPE{"Provider type?"}

    TYPE -->|Anthropic| ANTH["Budget tokens:<br/>low=4K, medium=10K, high=32K<br/>+ anthropic-beta header<br/>+ strip temperature"]
    TYPE -->|OpenAI-compat| OAI["reasoning_effort:<br/>low / medium / high"]
    TYPE -->|DashScope| DASH["enable_thinking: true<br/>Budget: low=4K, medium=16K, high=32K<br/>⚠ No streaming with tools"]
```

### Streaming

- **Anthropic**: `thinking_delta` events accumulate into `StreamChunk.Thinking`
- **OpenAI-compat**: `reasoning_content` in response delta
- **DashScope**: Falls back to non-streaming when tools are present, synthesizes chunk callbacks

### Tool Loop Handling

Anthropic requires thinking blocks (including cryptographic signatures) to be echoed back in subsequent tool-use turns. `RawAssistantContent` preserves these raw blocks for API passback. Other providers handle reasoning content as independent per-turn metadata.

---

## 9. DashScope and Bailian Providers

Two providers for the Alibaba Cloud AI ecosystem.

### DashScope (Alibaba Qwen)

Wraps the OpenAI-compatible provider with a critical override: when tools are present, streaming is disabled. The provider falls back to a single `Chat()` call and synthesizes chunk callbacks to maintain the event flow.

- **Default model**: `qwen3-max`
- **Thinking support**: Custom budget mapping (low=4,096, medium=16,384, high=32,768)
- **Known limitation**: No simultaneous streaming + tools

### Bailian Coding

Standard OpenAI-compatible provider targeting the Alibaba Coding API.

- **Default model**: `qwen3.5-plus`
- **Base URL**: `https://coding-intl.dashscope.aliyuncs.com/v1`

---

## 10. ACP Provider (Agent Client Protocol)

The ACP provider enables GoClaw to orchestrate external coding agents (Claude Code, Codex CLI, Gemini CLI, or any ACP-compatible agent) as subprocesses via JSON-RPC 2.0 over stdio. This allows delegating complex code generation tasks to specialized agents while maintaining GoClaw's unified interface.

### Architecture Overview

```mermaid
flowchart TD
    AL["Agent Loop"] -->|Chat / ChatStream| ACP["ACPProvider"]
    ACP --> PP["ProcessPool"]
    PP -->|spawn| PROC["Subprocess<br/>json-rpc 2.0 stdio"]
    PROC -->|initialize| AGT["Agent<br/>(Claude Code, Codex, etc.)"]

    AGT -->|fs/readTextFile| TB["ToolBridge"]
    AGT -->|fs/writeTextFile| TB
    AGT -->|terminal/*| TB
    AGT -->|permission/request| TB

    TB -->|enforce| SB["Workspace Sandbox"]
    TB -->|check| DEN["Deny Patterns"]
    TB -->|handle| PERM["Permission Mode"]
```

### Configuration

ACPConfig struct fields:

```go
type ACPConfig struct {
	Binary   string   // agent binary name or path (e.g. "claude", "codex")
	Args     []string // extra spawn args
	Model    string   // default model/agent name (e.g. "claude")
	WorkDir  string   // base workspace dir
	IdleTTL  string   // process idle TTL (e.g. "5m")
	PermMode string   // "approve-all" (default), "approve-reads", "deny-all"
}
```

Example config.json:

```json5
{
  "providers": {
    "acp": {
      "binary": "claude",
      "args": ["--profile", "goclaw"],
      "model": "claude",
      "work_dir": "/tmp/workspace",
      "idle_ttl": "5m",
      "perm_mode": "approve-all"
    }
  }
}
```

Database-based provider registration:

- `provider_type = "acp"`
- `api_base = "claude"` (binary name)
- `settings = { "args": [...], "idle_ttl": "5m", "perm_mode": "approve-all", "work_dir": "..." }`

### Session Management

#### ProcessPool

Manages subprocess lifecycle with idle TTL reaping and crash recovery:

1. **GetOrSpawn** — Retrieve existing session or spawn new subprocess
2. **Idle TTL** — Reap idle processes after configured duration (default 5m)
3. **Crash Recovery** — Restart failed subprocesses transparently

#### ToolBridge

Handles agent → client requests for filesystem and terminal operations:

- **fs/readTextFile** — Read file within workspace sandbox
- **fs/writeTextFile** — Write file within workspace sandbox
- **terminal/createTerminal** — Spawn terminal subprocess
- **terminal/terminalOutput** — Fetch terminal output + exit status
- **terminal/waitForTerminalExit** — Block until terminal exit
- **terminal/releaseTerminal** — Clean up terminal resources
- **terminal/killTerminal** — Force-terminate terminal
- **permission/request** — Request user approval (approve-all, approve-reads, deny-all)

### Content Handling

ACP messages use `ContentBlock` with three types:

```go
type ContentBlock struct {
	Type     string // "text", "image", "audio"
	Text     string // text content
	Data     string // base64 for image/audio
	MimeType string // e.g., "image/png", "audio/wav"
}
```

Request extraction:

1. Extract system prompt + user message from GoClaw `ChatRequest.Messages`
2. Prepend system prompt to first user message (ACP agents lack separate system API)
3. Attach images as separate blocks

Response collection:

1. Accumulate `SessionUpdate` notifications during prompt execution
2. Collect text blocks into response content
3. Return finish reason mapped from `stopReason` ("maxContextLength" → "length", others → "stop")

### Security & Sandboxing

#### Workspace Isolation

All file operations are scoped to `WorkDir`. Attempts to escape (e.g., `../../../etc/passwd`) are rejected.

#### Deny Patterns

Regex patterns (from config or tools policy) prevent access to sensitive paths:

```
[
  "^/etc/",
  "^\\.env",
  "^secret",
  "^[Cc]redentials"
]
```

Each agent request is validated against deny patterns before execution.

#### Permission Modes

| Mode | Behavior |
|------|----------|
| `approve-all` | All requests approved (default) |
| `approve-reads` | Read-only; filesystem writes denied |
| `deny-all` | All requests denied |

### Session Sequencing

Per-session requests are serialized via `sessionMu` mutex to prevent concurrent tool access that could corrupt file state:

```go
unlock := p.lockSession(sessionKey)
defer unlock()
// ... execute Chat or ChatStream with guaranteed serial access
```

### Streaming vs Non-Streaming

#### Chat (Non-Streaming)

Returns complete response after agent execution finishes. Collects all text blocks and returns single `ChatResponse`.

#### ChatStream

Emits `StreamChunk` for each text delta via callback. Supports context cancellation by sending `session/cancel` notification. Returns combined response when complete.

---

## 11. Claude CLI Provider

The Claude CLI provider enables GoClaw to delegate requests to a local `claude` CLI binary. The CLI manages session history, context files, and tool execution independently; GoClaw only passes messages and streams responses back.

### Architecture Overview

```mermaid
flowchart TD
    AL["Agent Loop"] -->|Chat / ChatStream| CLI["ClaudeCLIProvider"]
    CLI --> POOL["SessionPool"]
    POOL -->|spawn/reuse| PROC["Subprocess<br/>claude --server=stdio"]
    PROC -->|manages| SESS["Session<br/>(session ID, history)"]

    SESS -->|fs/readTextFile| TOOLS["CLI Tool Execution"]
    SESS -->|fs/writeTextFile| TOOLS
    SESS -->|exec/run| TOOLS
    SESS -->|web/fetch| TOOLS

    TOOLS -->|via MCP| MCP["MCP Servers<br/>(if configured)"]
```

### Configuration

ClaudeCLIProvider can be configured in `config.json`:

```json5
{
  "providers": {
    "claude_cli": {
      "cli_path": "claude",           // binary path or name
      "default_model": "sonnet",      // opus, sonnet, haiku
      "base_work_dir": "/tmp/agents", // workspace directory
      "perm_mode": "bypassPermissions", // permission mode
      "disable_hooks": false,         // disable security hooks if true
      "deny_patterns": ["^/etc/", "^\\.env"]
    }
  }
}
```

Or via database `llm_providers` table with `provider_type = "claude_cli"`.

### Session Management

Each conversation gets a persistent session tied to `session_key` option. Sessions survive across multiple requests and maintain:
- Conversation history
- Workspace directory (for file operations)
- MCP server connections
- Tool execution state

Idle sessions are automatically cleaned up after inactivity.

### Tool Execution

Claude CLI executes tools natively (filesystem, exec, web, memory). GoClaw forwards tool results back and lets the CLI loop continue. This differs from standard providers which return tool calls for the agent loop to execute.

### Model Aliases

Like the Anthropic provider, Claude CLI supports short aliases:
- `opus` → `claude-opus-4-6`
- `sonnet` → `claude-sonnet-4-6`
- `haiku` → `claude-haiku-4-5-20251001`

### MCP Configuration

Per-session MCP servers are configured via `MCPConfigData`. The CLI automatically loads and communicates with configured MCP servers for extended functionality.

### Streaming

- **Chat**: Returns complete response after CLI execution
- **ChatStream**: Streams text chunks as they are produced by the CLI

### Thinking Support

Claude CLI inherits thinking support from the underlying Claude model. Thinking blocks are passed through in streaming chunks if the model supports them.

---

## 12. Codex Provider

The Codex provider integrates with OpenAI's ChatGPT Responses API (OAuth-based), enabling access to gpt-5.3-codex model through the chatgpt.com backend. Unlike standard OpenAI endpoints, Codex uses OAuth token refresh and a custom response format with "phase" markers.

### Configuration

Codex requires an OAuth token source (handles auto-refresh):

```go
tokenSource := &MyTokenSource{} // implements TokenSource interface
provider := NewCodexProvider("codex", tokenSource, "", "")
// or specify custom API base and model:
provider := NewCodexProvider("codex", tokenSource,
  "https://chatgpt.com/backend-api", "gpt-5.3-codex")
```

### API Endpoint

```
POST https://chatgpt.com/backend-api/codex/responses
Authorization: Bearer {oauth_token}
```

The provider automatically handles token refresh via the TokenSource.

### Response Format

Codex returns structured responses with phase markers:

```json
{
  "id": "...",
  "model": "gpt-5.3-codex",
  "choices": [{
    "message": {
      "role": "assistant",
      "content": "...",
      "metadata": {
        "phase": "commentary"  // or "final_answer"
      }
    },
    "finish_reason": "stop"
  }],
  "usage": { ... }
}
```

### Phase Field

The `phase` field indicates message purpose:
- `"commentary"` — intermediate reasoning
- `"final_answer"` — closeout response

GoClaw persists this on assistant messages and passes it back in subsequent requests. Codex performance depends on this field being echoed correctly.

### Streaming

Codex supports SSE streaming similar to Anthropic:
- Each SSE event contains a partial response
- Phase marker included in final delta
- Tool calls streamed via `input_json_delta` equivalent

### Extended Thinking

Codex provider reports `SupportsThinking() = true`, allowing thinking_level to be injected. The provider maps thinking levels to reasoning_effort parameters as needed.

### Token Usage

Tracks prompt, completion, and total tokens. `CacheCreationTokens` and `CacheReadTokens` are supported for prompt caching if available.

---

## 14. File Reference

| File | Purpose |
|------|---------|
| `internal/providers/types.go` | Provider interface, ChatRequest, ChatResponse, Message, ToolCall, Usage types |
| `internal/providers/anthropic.go` | Anthropic provider: native HTTP + SSE, request/response marshaling |
| `internal/providers/anthropic_request.go` | Anthropic request builder: message formatting, tool schemas, system blocks |
| `internal/providers/anthropic_stream.go` | Anthropic SSE event parsing and response accumulation |
| `internal/providers/openai.go` | OpenAI-compatible provider: generic HTTP client for 10+ endpoints |
| `internal/providers/openai_types.go` | OpenAI request/response types and message formatting |
| `internal/providers/openai_gemini.go` | Gemini-specific compatibility: empty content handling, tool schema cleaning |
| `internal/providers/claude_cli.go` | ClaudeCLIProvider: orchestrates local claude CLI binary via stdio |
| `internal/providers/claude_cli_chat.go` | Chat/ChatStream implementation for CLI provider |
| `internal/providers/claude_cli_session.go` | Session management: per-session state, history, workspace |
| `internal/providers/claude_cli_mcp.go` | MCP configuration and server bridge for CLI provider |
| `internal/providers/claude_cli_auth.go` | Authentication and token handling for CLI |
| `internal/providers/claude_cli_parse.go` | Response parsing and message extraction from CLI output |
| `internal/providers/claude_cli_deny_patterns.go` | Path validation and deny pattern enforcement |
| `internal/providers/claude_cli_hooks.go` | Security hooks configuration for CLI tool execution |
| `internal/providers/claude_cli_types.go` | Internal types for CLI provider (session, config, options) |
| `internal/providers/codex.go` | CodexProvider: OAuth-based ChatGPT Responses API |
| `internal/providers/codex_build.go` | Codex request builder: message formatting, phase handling |
| `internal/providers/codex_types.go` | Codex request/response types and OAuth token management |
| `internal/providers/dashscope.go` | DashScope provider: OpenAI-compat wrapper with thinking budget, tools+streaming fallback |
| `internal/providers/acp_provider.go` | ACPProvider: orchestrates ACP-compatible agent subprocesses |
| `internal/providers/acp/types.go` | ACP protocol types: InitializeRequest, SessionUpdate, ContentBlock, etc. |
| `internal/providers/acp/process.go` | ProcessPool: subprocess lifecycle, idle TTL reaping, crash recovery |
| `internal/providers/acp/jsonrpc.go` | JSON-RPC 2.0 request/response marshaling over stdio |
| `internal/providers/acp/tool_bridge.go` | ToolBridge: handles fs and terminal requests, workspace sandboxing |
| `internal/providers/acp/terminal.go` | Terminal lifecycle: create, output, exit, release, kill |
| `internal/providers/acp/session.go` | Session state tracking per ACP agent |
| `internal/providers/retry.go` | RetryDo[T] generic function, RetryConfig, IsRetryableError, backoff computation |
| `internal/providers/schema_cleaner.go` | CleanSchemaForProvider, CleanToolSchemas, recursive schema field removal |
| `internal/providers/registry.go` | Provider registry: registration, lookup, lifecycle management |
| `cmd/gateway_providers.go` | Provider registration from config and database during gateway startup |

---

## Cross-References

| Document | Relevant Content |
|----------|-----------------|
| [12-extended-thinking.md](./12-extended-thinking.md) | Full extended thinking documentation |
| [01-agent-loop.md](./01-agent-loop.md) | LLM iteration loop, streaming chunk handling |
