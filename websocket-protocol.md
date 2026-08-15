# WebSocket Protocol (v3)

Frame types: `req` (client request), `res` (server response), `event` (server push).

## Authentication

The first request must be a `connect` handshake. Authentication supports three paths:

```json
// Path 1: Token-based (admin role)
{"type": "req", "id": 1, "method": "connect", "params": {"token": "your-gateway-token", "user_id": "alice"}}

// Path 2: Browser pairing reconnect (operator role)
{"type": "req", "id": 1, "method": "connect", "params": {"sender_id": "previously-paired-id", "user_id": "alice"}}

// Path 3: No token — initiates browser pairing flow (returns pairing code)
{"type": "req", "id": 1, "method": "connect", "params": {"user_id": "alice"}}
```

## Methods

| Method | Description |
|--------|-------------|
| `connect` | Authentication handshake (must be first request) |
| `health` | Server health check |
| `status` | Server status and metadata |
| `chat.send` | Send a message to an agent |
| `chat.history` | Retrieve session history |
| `chat.abort` | Abort a running agent request |
| `agent` | Get agent info |
| `sessions.list` | List active sessions |
| `sessions.delete` | Delete a session |
| `sessions.label` | Label a session |
| `skills.list` | List available skills |
| `cron.list` | List scheduled jobs |
| `cron.create` | Create a cron job |
| `cron.delete` | Delete a cron job |
| `cron.toggle` | Enable/disable a cron job |
| `models.list` | List available AI models |
| `browser.pairing.status` | Poll pairing approval status |
| `device.pair.request` | Request device pairing |
| `device.pair.approve` | Approve a pairing code |
| `device.pair.list` | List pending and approved pairings |
| `device.pair.revoke` | Revoke a pairing |

## Events (server push)

| Event | Description |
|-------|-------------|
| `chunk` | Streaming token from LLM (payload: `{content}`) |
| `tool.call` | Agent invoking a tool (payload: `{name, id}`) |
| `tool.result` | Tool execution result |
| `run.started` | Agent started processing |
| `run.completed` | Agent finished processing |
| `shutdown` | Server shutting down |

## Frame Format

### Request (client to server)
```json
{
  "type": "req",
  "id": "unique-request-id",
  "method": "chat.send",
  "params": { ... }
}
```

### Response (server to client)
```json
{
  "type": "res",
  "id": "matching-request-id",
  "ok": true,
  "payload": { ... }
}
```

### Error Response
```json
{
  "type": "res",
  "id": "matching-request-id",
  "ok": false,
  "error": {
    "code": "error_code",
    "message": "Human-readable error message"
  }
}
```

### Event (server push)
```json
{
  "type": "event",
  "event": "chunk",
  "payload": { "content": "streaming text..." }
}
```
