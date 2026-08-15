# Contract Breaking Change Policy

## What is a Breaking Change?

A breaking change is any modification to the API response schema that could cause existing clients to fail:

| Change Type | Breaking? | Example |
|------------|-----------|---------|
| Remove required field | YES | `{id, name}` → `{id}` |
| Change field type | YES | `"count": 5` → `"count": "5"` |
| Rename field | YES | `user_id` → `userId` |
| Add new required field | YES | Client must send new field |
| Add optional field | NO | Backward compatible |
| Add new enum value | MAYBE | If client validates strictly |

## Contract Test Categories

### P0 - Critical (WS Core)
- `connect` - Session establishment
- `chat.send` - Message handling

### P1 - High (Data APIs)
- `agents.list`, `sessions.list`, `skills.list`
- `/v1/chat/completions` - OpenAI compatibility
- `/v1/agents` - Agent management

### P2 - Medium (Config/Admin)
- `config.get`, `config.apply`
- `/v1/providers`

## Breaking Change Process

1. **Detect**: Contract test fails in CI
2. **Evaluate**: Is this intentional or accidental?
3. **If Intentional**:
   - Bump protocol version in `pkg/protocol/version.go`
   - Update JSON schemas in `tests/contracts/schemas/`
   - Update contract tests
   - Document in CHANGELOG.md
4. **If Accidental**: Fix the regression

## Running Contract Tests

```bash
# Set environment variables
export CONTRACT_TEST_WS_URL="ws://localhost:8080/ws"
export CONTRACT_TEST_HTTP_URL="http://localhost:8080"
export CONTRACT_TEST_TOKEN="your-test-token"

# Run all contract tests
go test -tags integration ./tests/contracts/...
```

## Schema Files

JSON Schema definitions in `tests/contracts/schemas/`:
- `ws_connect.json` - WS connect response
- `ws_chat_send.json` - WS chat.send response
- `http_chat_completions.json` - OpenAI-compatible response

These schemas serve as documentation and can be used for automated validation.
