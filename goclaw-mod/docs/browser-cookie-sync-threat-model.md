# Browser Cookie Sync Threat Model

Selected cookie sync lets a user copy specific Chrome cookies into a GoClaw server-side browser session for one agent.

## Assets

- Browser cookies and session tokens.
- Tenant, user, and agent isolation boundaries.
- Server-side browser incognito contexts.
- Audit logs showing sync and delete events.

## Trust Boundaries

- Chrome extension runs on the user's machine and only sends cookies after explicit selection.
- HTTP API derives tenant and user from gateway auth, API key auth, or paired-browser auth.
- Client payload may choose `agent_id`, but cannot choose `tenant_id` or `user_id`.
- Cookie values are encrypted before database persistence.

## Controls

- `POST /v1/browser/cookies/sync`, `GET /v1/browser/cookies`, and `DELETE /v1/browser/cookies` require operator auth.
- Sync fails when the request context has no user or no agent.
- Store unique key is `(tenant_id, user_id, agent_id, domain, path, name)`.
- Cookie values are encrypted at rest and never returned by list responses.
- Browser runtime applies cookies only to matching domain/path for the same tenant, user, and agent scope.
- Extension asks for active-site host permission and sends only checked cookies.

## Main Risks

| Risk | Mitigation |
|------|------------|
| Cross-user cookie leak | Store and browser scope include `tenant_id`, `user_id`, and `agent_id`. |
| Client spoofs user | API ignores user fields in JSON body; user comes from auth context. |
| Plaintext persistence | Cookie store fails closed when encryption key is missing. |
| Oversized cookie payload | API caps body size, cookie count, and per-cookie value size. |
| Overbroad extension access | Extension requests host permission for the current origin before reading cookies. |
| Accidental value exposure | List endpoint returns metadata only. Logs never include cookie values. |

## Operational Notes

- Set `GOCLAW_ENCRYPTION_KEY` before enabling cookie sync.
- Revoke synced cookies with `DELETE /v1/browser/cookies?agent_id=<agent>&domain=<domain>`.
- Restart the gateway after changing browser launch settings that affect manager startup.
