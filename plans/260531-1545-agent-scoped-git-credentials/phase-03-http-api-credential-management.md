---
phase: 3
title: HTTP API credential management
status: completed
effort: ''
---

# Phase 3: HTTP API credential management

## Context Links

- Route registration: `internal/http/secure_cli.go:38`
- Current user credential handlers: `internal/http/secure_cli_user_credentials.go:13`
- Typed credential validator: `internal/http/secure_cli_typed_credentials.go:54`
- API docs table: `docs/18-http-api.md:1176`

## Overview

Add HTTP API endpoints for agent-scoped CLI credential management. This is required by the user request and must not be left as a UI-only feature.

Priority: P1.

Status: pending.

## Key Insights

- The validator for `{credential_type, host_scope, blob}` already exists for user credentials and should be reused for agent credentials.
- API must make clear that credentials are not grants. A credential row stores secret material; `agent-grants` still controls non-global binary access.
- Responses should mirror the user credential API, but use `agent_id` and optional agent display metadata.

## Requirements

- Add routes:
  - `GET /v1/cli-credentials/{id}/agent-credentials`
  - `GET /v1/cli-credentials/{id}/agent-credentials/{agentId}`
  - `PUT /v1/cli-credentials/{id}/agent-credentials/{agentId}`
  - `DELETE /v1/cli-credentials/{id}/agent-credentials/{agentId}`
- Reuse typed payload body:
  - `credential_type: "pat" | "ssh_key" | "env"`
  - `host_scope`
  - `blob: {"token": "..."} | {"key": "..."}`
  - legacy `env` for env-only CLIs
- Return masked metadata only.
- Emit audit events with credential type and IDs, never secret values.
- Require admin auth and tenant scope.

## Architecture

Response list shape:

```json
{
  "agent_credentials": [
    {
      "id": "...",
      "binary_id": "...",
      "agent_id": "...",
      "agent_key": "builder",
      "has_secret": true,
      "credential_type": "pat",
      "host_scope": "github.com",
      "created_at": "...",
      "updated_at": "..."
    }
  ]
}
```

Detail response:

```json
{
  "agent_id": "...",
  "credential_type": "pat",
  "host_scope": "github.com",
  "has_secret": true
}
```

Legacy env credentials may include sanitized `env` entries. Typed credentials must not return `blob`, `token`, or `key`.

## Related Code Files

- Add `internal/http/secure_cli_agent_credentials.go`.
- Extend `internal/http/secure_cli_typed_credentials.go` if shared helpers need neutral names.
- Update `internal/http/secure_cli.go` route registration.
- Add tests near `internal/http/secure_cli_typed_credentials_test.go`.
- Update API docs in `docs/18-http-api.md` and `docs/20-api-keys-auth.md`.

## Implementation Steps

1. Refactor `typedCredentialBody`, `prepareTypedCredentialEnv`, and `writeTypedCredentialError` only if needed to support both user and agent handlers.
2. Add handler methods for list/get/put/delete agent credentials.
3. Validate `binaryID` and `agentID` path params with `uuid.Parse`.
4. Verify binary and agent exist through store methods before writing.
5. For PUT:
   - typed branch: validate blob and call `SetAgentCredentialsTyped`
   - env branch: merge env object and call `SetAgentCredentials`
6. For GET/list:
   - return metadata and `has_secret`
   - suppress `env` for typed credentials
7. Emit audit events:
   - `secure_cli.agent_credentials.updated`
   - `secure_cli.agent_credentials.deleted`
8. Invalidate SecureCLI cache after update/delete.
9. Run HTTP tests.

## Todo List

- [ ] Agent credential routes registered.
- [ ] Handler tests cover list/get/put/delete.
- [ ] Typed validation shared with user credential API.
- [ ] Responses mask typed secrets.
- [ ] API docs updated with endpoint table and body examples.

## Success Criteria

- [ ] API can fully manage agent-scoped PAT and SSH credentials.
- [ ] API can edit legacy env credentials for non-git binaries.
- [ ] Invalid agent/binary returns a clear 404 or 400.
- [ ] Non-admin cannot manage credentials.
- [ ] No API response leaks raw credential material.

## Risk Assessment

- Risk: new credential endpoint may be mistaken for grant endpoint. Mitigation: docs and UI state credential does not grant access.
- Risk: duplicate validation forks. Mitigation: reuse the existing typed credential validator.

## Security Considerations

- Use `http.MaxBytesReader` or existing JSON binding limits.
- Do not log raw request body.
- Audit should include `credential_type` and resource IDs only.
- All writes must be tenant-scoped.

## Next Steps

- Phase 4 consumes these endpoints from the Web UI.
