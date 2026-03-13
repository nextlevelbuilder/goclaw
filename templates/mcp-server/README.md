# bun-mcp

MCP (Model Context Protocol) server template built with Bun and `@modelcontextprotocol/sdk`.

## Quick Start

```bash
bun install
bun run start        # stdio transport (default)
bun run start:http   # HTTP transport on port 3000
bun test             # run tests
bun run inspect      # interactive MCP Inspector
```

## Structure

```
src/
├── index.ts          # Server entry point + transport selection
├── tools/            # Tool handlers (greet, fetch_url)
├── resources/        # Resources (server-info)
├── prompts/          # Prompt templates (code_review)
├── errors.ts         # Error handling helpers
└── logging.ts        # Stderr logging (safe for stdio)
tests/
└── tools.test.ts     # Tests via InMemoryTransport
```

## Transports

| Transport | Command | Use Case |
|-----------|---------|----------|
| **stdio** | `bun run start` | Claude Desktop, Cursor, local integrations |
| **HTTP** | `bun run start:http` | Remote servers, multiple clients |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_TRANSPORT` | `stdio` | Transport: `stdio` or `http` |
| `MCP_PORT` | `3000` | HTTP transport port |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## Client Config

**Claude Desktop** (`claude_desktop_config.json`):
```json
{
  "mcpServers": {
    "bun-mcp": {
      "command": "bun",
      "args": ["run", "/path/to/src/index.ts"]
    }
  }
}
```

See `CLAUDE.md` for detailed development guide.
