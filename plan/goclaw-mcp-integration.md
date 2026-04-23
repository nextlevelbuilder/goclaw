# GoClaw × mcp-bx-syn Integration Plan

> Kế hoạch tích hợp MCP `mcp-bx-syn` với GoClaw chatbot để enforce per-user ACL khi user chat với bot Bitrix24.
>
> **Status**: ✅ Both sides implemented. Path B (access_token as auth anchor) shipped end-to-end. Remaining work is operational (backfill, marketplace rollout, Phase E shared-credential support for Open Channel).
>
> **Owner**: dangt
> **Last updated**: 2026-04-23 (rev5: Path B shipped — ADMIN_TOKEN removed, mapping table dropped, notify + rate limit + audit log added)

---

## 1. Mục tiêu & phạm vi

**Mục tiêu**: Mỗi user trong Bitrix chat với bot GoClaw → MCP `mcp-bx-syn` gọi Bitrix REST với token của **chính user đó** (enforce ACL tự nhiên). Triển khai an toàn ở quy mô marketplace: mỗi portal cài app độc lập, không có shared secret giữa MCP và GoClaw.

**Phạm vi (đã triển khai)**:
- Endpoint `POST /api/auto-onboard` trên MCP (Path B — xác thực bằng Bitrix `access_token` thay vì `ADMIN_TOKEN`)
- Lazy provisioning hook trong custom Bitrix24 channel của GoClaw
- Persist per-user OAuth tokens vào MCPUserCredentials để MCP proxy gọi REST API theo user
- Rate limit + audit log trên endpoint
- Debounce (60s) chống webhook retry storm, debounce (5 phút) cho user-facing degradation notice

**Không đụng vào**:
- `/oauth/join` flow cho Claude Desktop/Cursor
- Auth resolver hiện tại (API key / OAuth dual-path)
- Webhook install flow (OAuth dance vẫn như cũ)

**Ngoài phạm vi (theo Phase E/F)**:
- Shared-credential fallback cho Open Channel bot (bot `TYPE=O`)
- UI quản lý auto-created `goclaw-bot` keys
- Metrics / dashboard telemetry
- Credential refresh / rotation path (hiện tại dựa vào hourly re-verify trong `token-manager.ts`)

---

## 2. Quyết định đã chốt (Rev5)

- ✅ **Path B**: MCP xác thực mỗi call `/api/auto-onboard` bằng cách gọi Bitrix `profile` với `access_token` do caller supply, so khớp `profile.ID` với `bitrix_user_id`. Không cần `ADMIN_TOKEN` shared giữa GoClaw và MCP.
  - Đổi so với Rev4 (dùng `ADMIN_TOKEN` Bearer) vì không scale cho marketplace: mỗi portal chạy GoClaw riêng không thể share 1 secret với MCP worker.
- ✅ **Reject 404 `tenant_not_installed`** nếu portal chưa cài MCP app.
- ✅ **Idempotent theo `(tenant.domain, bitrix_user_id)`** — lần 2 refresh tokens, trả cùng USR_.
- ✅ **Label `"goclaw-bot"`** cho auto-created api_keys (phân biệt với `"default"` từ `/oauth/join`).
- ✅ **Tenant key = `domain`** (đã có `tenants.domain UNIQUE` trong MCP schema).
- ✅ **Forward OAuth tokens**: event Bitrix mang sẵn `auth[access_token/refresh_token/expires_in]` → GoClaw forward nguyên — MCP lưu vào `users` row và `users.access_token` được dùng cho mọi REST call sau đó.
- ✅ **KHÔNG thêm bảng mapping riêng phía GoClaw**: reuse `mcp_user_credentials` (partner store) — khoá `(mcp_server_id, user_id)` đủ để cache. Giảm surface ~300 LOC (store interface + 2 impl + migration + SchemaVersion bump).
- ✅ **KHÔNG migration MCP side**: `users.bitrix_user_id TEXT` + `UNIQUE(tenant_id, bitrix_user_id)` đã có trong schema gốc.
- ✅ **Debounce hai cấp độ**:
  - `mcpProvisionDebounceTTL = 60s` theo `(serverID, userID)` — chống webhook retry storm.
  - `mcpUserNotifyDebounceTTL = 5min` theo `userID` — chống flood DM notice khi MCP down.
- ✅ **Channel health stays Green on MCP failure**: degradation là silent (từ góc độ health page); chỉ user + slog.Warn thấy. Lý do: message routing vẫn hoạt động — agent mất MCP tools nhưng vẫn reply được.
- ✅ **Skip Open Channel bot (`TYPE=O`)**: khách vãng lai không có tenant_users mapping → không mint per-user credentials. Shared-credential defer Phase E.
- ✅ **Rate limit qua KV**: 600 req/min/IP + 120 req/min/domain, fail-open khi KV outage.
- ✅ **Audit log**: mỗi call `/api/auto-onboard` (success hoặc fail) ghi 1 row vào `auto_onboard_audit` — operator trace per-portal issue không cần debug hook.

---

## 3. Kiến trúc tổng thể

```
User chat với bot trong Bitrix
         ↓
Bitrix gửi ONIMBOTMESSAGEADD
  - auth[domain]=tamgiac.bitrix24.com
  - auth[access_token], auth[refresh_token], auth[expires_in]=3600
  - data[PARAMS][FROM_USER_ID]=62  ← senderID / bitrix_user_id (GoClaw chỉ đọc chỗ này)
  - data[USER][NAME]=...           ← optional (thường không có trong webhook)
         ↓
GoClaw Channel.DispatchEvent → handleMessage (internal/channels/bitrix24/handle.go)
         ↓
(policy gate, mention strip, contact enrich) → c.provisionIfMissing(ctx, senderID, evt.Auth)
         ↓
┌───────────────────────────────────────────────────────────────────────────┐
│ provisionIfMissing (internal/channels/bitrix24/provisioner.go)            │
│   - Skip Open Channel bot (TYPE=O) → ErrProvisionSkippedOpenChannel       │
│   - Skip if mcpStore / mcpClient / mcpServerID unset → ErrProvisionDisabled│
│   - Cheap check: mcpStore.GetUserCredentials(serverID, userID) hit → return│
│   - Debounce 60s per (serverID, userID) → ErrProvisionDebounced           │
│   - POST /api/auto-onboard { domain, bitrix_user_id, access_token,        │
│                              refresh_token, expires_in, display_name }    │
│   - Persist: mcpStore.SetUserCredentials(serverID, userID,                │
│       { APIKey: USR_xxx, Env: { BITRIX_DOMAIN, ACCESS_TOKEN,              │
│                                  REFRESH_TOKEN, EXPIRES_AT } })           │
└───────────────────────────────────────────────────────────────────────────┘
         ↓
(handle.go) nếu provisionIfMissing return err ngoài các sentinel → slog.Warn
  + notifyUserOfMCPIssueOnce(ctx, userID, chatID) (5min debounce, best-effort)
         ↓
c.HandleMessage(...) → bus.InboundMessage → agent pipeline
         ↓
Agent gọi MCP tool → Manager.resolveServerCredentials() inject
  Authorization: Bearer USR_xxx
         ↓
┌───────────────────────────────────────────────────────────────────────────┐
│ mcp-bx-syn (Cloudflare Worker)                                            │
│   1. Receive /mcp call, resolveApiAuth → OAuthAuthContext{ user, tenant } │
│   2. ensureFreshToken (token-manager.ts):                                 │
│      - If access_token expiring soon → refresh qua oauth.bitrix.info      │
│      - If last_verified_at > 1h → verifyBitrixActive(profile) again       │
│        - !active → user_status='dismissed' + deactivateUserApiKeys        │
│   3. Bitrix REST: https://{domain}/rest/{method}?auth={fresh_access_token}│
│   4. Bitrix enforce ACL theo user đó                                      │
└───────────────────────────────────────────────────────────────────────────┘
```

### Verification table (đã kiểm chứng với code thật)

| Claim | Status | Ref |
|---|---|---|
| `resolveServerCredentials()` wrap `APIKey` → `Authorization: Bearer <APIKey>` | ✅ | `internal/mcp/manager.go` |
| `store.MCPUserCredentials{APIKey, Headers, Env}` | ✅ | `internal/store/mcp_store.go` |
| `SetUserCredentials(ctx, serverID, userID, creds)` signature | ✅ | `internal/store/mcp_store.go` |
| PG impl dùng `tenantIDForInsert(ctx)` → **ctx phải có tenant** | ✅ | `internal/store/pg/mcp_user_credentials.go` |
| Encryption at rest AES-256-GCM qua `encKey` | ✅ | `internal/store/pg/mcp_user_credentials.go` |
| `bitrix_user_id` = `EventParams.FromUserID` (EventAuth struct KHÔNG có `UserID` field) | ✅ | `events.go:44-55`, `handle.go:122` |
| MCP `profile` (không `user.get`) vì không yêu cầu `user` scope | ✅ | `src/auth/bitrix-user-verify.ts` |
| `ensureFreshToken` re-verify hourly + dismiss khi fail | ✅ | `src/auth/token-manager.ts` |
| GoClaw KHÔNG còn phụ thuộc ADMIN_TOKEN | ✅ | commit `07b48ef0` (goclaw-deploy/dev) |
| GoClaw channel đã wire Path B từ commit phase C | ✅ | commit `ea09c1ba` (goclaw-deploy/dev) |

---

## 4. Data model

### 4.1 MCP side — schema (không migration mới)

Schema gốc đã đủ. Các bảng Path B dùng:

| Bảng | Cột liên quan | Dùng cho |
|---|---|---|
| `tenants` | `domain UNIQUE` | `findTenantByDomain` — 404 gate |
| `users` | `tenant_id`, `bitrix_user_id TEXT`, `UNIQUE(tenant_id, bitrix_user_id)`, `access_token`, `refresh_token`, `token_expires_at`, `token_version` | Upsert theo (tenant, bitrix_user_id); lưu OAuth tokens để proxy Bitrix REST |
| `users` (Phase 04 columns, reuse) | `user_status` (`active`/`dismissed`), `last_verified_at` (unix seconds) | Đã có từ Phase 04; Path B reuse cho `ensureFreshToken` re-verify + dismiss flow |
| `api_keys` | `user_id`, `key`, `label`, `active` | Mint USR_ label `"goclaw-bot"`; `deactivateUserApiKeys` set `active=0` khi dismiss |
| `auto_onboard_audit` (mới — Path B) | `id`, `domain`, `bitrix_user_id`, `event`, `actor`, `metadata`, `created_at` | Audit trail cho `/api/auto-onboard` — mọi call (success + fail) ghi 1 row. Event taxonomy: `success`/`rate_limited`/`invalid_bitrix_user`/`bitrix_unreachable`/`tenant_not_installed`/`bad_request` |

### 4.2 GoClaw side — schema (KHÔNG migration mới) ✅

**Rev4 dự kiến** một bảng `bitrix_mcp_user_mapping` riêng để cache (tenant, domain, bitrix_user_id, goclaw_user_id, mcp_server_id). **Rev5 bỏ** vì partner's `mcp_user_credentials` đã đủ:

- `mcpStore.GetUserCredentials(ctx, serverID, userID)` key trên `(mcp_server_id, user_id)` — đúng thứ provisioner cần kiểm tra "đã mint chưa".
- GoClaw chỉ cần 1 lookup thay vì 2 (mapping table → user_credentials).
- Giảm ~300 LOC: interface `BitrixMappingStore`, 2 impl (PG + SQLite), migration `000057`, SchemaVersion bump, upgrade/version.go bump.
- Idempotency ở phía MCP vẫn đảm bảo bởi `UNIQUE(tenant_id, bitrix_user_id)` trên `users`.

**Kết quả**: GoClaw Phase C ship với migration counter vẫn là `000056_bitrix_portals`, không đụng tới upgrade version.

---

## 5. MCP side — `/api/auto-onboard` endpoint ✅ IMPLEMENTED

### 5.1 HTTP contract (rev5 — Path B)

**Route**: `POST /api/auto-onboard`

**Auth**: **Không có bearer token**. Thay vào đó, endpoint verify `access_token` trong body bằng cách gọi `https://{domain}/rest/profile?auth={access_token}`. Nếu `profile.ID === bitrix_user_id` → proceed; không thì 401.

**Headers**:
```
Content-Type: application/json
```

**Request body**:
```json
{
  "domain": "tamgiac.bitrix24.com",
  "bitrix_user_id": "62",
  "access_token": "<user access token từ event.auth>",
  "refresh_token": "<user refresh token từ event.auth>",
  "expires_in": 3600,
  "display_name": "Đặng Văn Tình"
}
```

| Field | Required | Type | Nguồn từ event |
|---|---|---|---|
| `domain` | ✅ | string | `auth[domain]` |
| `bitrix_user_id` | ✅ | string (MCP coerces number→string) | `data[PARAMS][FROM_USER_ID]` (GoClaw gửi raw; `EventAuth` không có `UserID` field để fallback) |
| `access_token` | ✅ | string | `auth[access_token]` |
| `refresh_token` | ✅ | string | `auth[refresh_token]` |
| `expires_in` | optional (default 3600) | number (seconds) | `auth[expires_in]` |
| `display_name` | optional | string | (hiện không có trong Bitrix webhook — để trống) |

**Response 200** (đã có hoặc mới tạo):
```json
{
  "api_key": "USR_aabbcc...",
  "user_id": "uuid-của-user-trong-mcp",
  "tenant_id": "uuid-của-tenant",
  "created": false
}
```

**Response codes**:

| Status | Body | Nguyên nhân |
|---|---|---|
| 400 | `{"error":"<reason>"}` | Invalid JSON / thiếu `domain`/`bitrix_user_id`/`access_token`/`refresh_token` |
| 401 | `{"error":"invalid_bitrix_user"}` | `profile` reachable nhưng `profile.ID ≠ bitrix_user_id` (token không thuộc user này) |
| 404 | `{"error":"tenant_not_installed","domain":"..."}` | Portal chưa cài MCP app |
| 429 | `{"error":"rate_limited"}` + `Retry-After: 60` | Quá 600 req/min cho IP hoặc 120 req/min cho domain |
| 503 | `{"error":"bitrix_unreachable"}` | `profile` fail (5xx / network / JSON parse fail) — **fail-closed** vì không xác thực được caller |
| 503 | `"DB not configured"` | `env.DB` chưa wire (ops misconfig) |

### 5.2 Logic (file `src/api/auto-onboard.ts`)

```
1. Parse + validate body                               → 400 nếu fail
2. Rate limit via RATE_LIMIT_KV                        → 429 nếu quá ngưỡng
   - byIP:     ratelimit:auto-onboard:ip:{ip}
   - byDomain: ratelimit:auto-onboard:domain:{domain}
   - KV unreachable → fail-open (không block portal thật)
3. verifyBitrixActive(domain, bitrix_user_id, access_token)
   - reachable && !active → 401 invalid_bitrix_user
   - !reachable           → 503 bitrix_unreachable
4. findTenantByDomain(domain)                          → 404 nếu thiếu
5. Upsert user:
   - existing → updateUserTokens + optional display_name
                → findOrCreateGoclawBotKey → 200 created: false
   - new      → createUser(...tokens) → createApiKey(label="goclaw-bot")
                → 200 created: true
6. Mọi bước ghi audit vào auto_onboard_audit (swallow errors)
```

### 5.3 Re-verify defence-in-depth (`src/auth/token-manager.ts`)

Path B xác thực 1 lần lúc onboard; nhưng nếu user bị deactive trên Bitrix sau khi onboard, USR_ vẫn sống. Mitigation:

- `ensureFreshToken()` chạy trên **mỗi** MCP call (via `resolveApiAuth` → `OAuthAuthContext`).
- Nếu `last_verified_at > 1h` → gọi `verifyBitrixActive` lại:
  - `reachable && !active` → `user_status='dismissed'` + `deactivateUserApiKeys` → next call 401.
  - `reachable && active` → `last_verified_at = now`.
  - `!reachable` → fail-open (transient outage không nên kick user).
- Token refresh qua `oauth.bitrix.info/oauth/token/` khi `token_expires_at < now + 60s` — optimistic lock bằng `token_version`.

Kết quả: user bị xoá khỏi Bitrix → trong vòng 1h MCP key của họ bị vô hiệu hoá.

### 5.4 Files changed (rev5)

| File | Action | Status |
|---|---|---|
| `src/api/auto-onboard.ts` | Rewrite — Path B (verify + rate limit + audit) | ✅ |
| `src/auth/bitrix-user-verify.ts` | Create — `verifyBitrixActive` via `profile` | ✅ |
| `src/auth/token-manager.ts` | Modify — hook re-verify vào `ensureFreshToken` | ✅ |
| `src/db/queries.ts` | Modify — thêm `logAutoOnboardEvent`, `updateUserVerifyStatus`, `deactivateUserApiKeys` | ✅ |
| `src/db/schema.sql` | Modify — thêm bảng `auto_onboard_audit` (Path B audit). `users.user_status` + `last_verified_at` đã có từ Phase 04 | ✅ |
| `src/api/api-router.ts` | Modify — route `POST /auto-onboard` (no auth gate) | ✅ |
| `wrangler.toml` | Modify — `RATE_LIMIT_KV` binding + drop `ADMIN_TOKEN` dependency | ✅ |

---

## 6. GoClaw side — custom Bitrix24 channel hook

### 6.0 Channel struct (shipped — `internal/channels/bitrix24/channel.go`)

```go
type Channel struct {
    *channels.BaseChannel

    cfg         bitrixInstanceConfig
    portalStore store.BitrixPortalStore
    encKey      string
    router      *Router

    // ... start / portal / client / botID / mention regex ...

    // MCP lazy provisioner (Phase C)
    mcpStore     store.MCPServerStore
    mcpClient    *mcpClient
    mcpServerID  uuid.UUID
    mcpProvMu    sync.Mutex
    mcpDebounce  map[mcpDebounceKey]time.Time

    // User-facing degradation notice debounce (5 min per user)
    notifyMu       sync.Mutex
    notifyDebounce map[string]time.Time

    // Contact-name enrichment cache (Bitrix webhook không carry display_name)
    nameCacheMu sync.Mutex
    nameCache   map[string]nameCacheEntry
}
```

**Khác với Rev4**: KHÔNG có `mappingStore BitrixMappingStore`. Provisioner chỉ dùng `mcpStore.GetUserCredentials` / `SetUserCredentials`.

### 6.1 Config + credentials (shipped — `factory.go`)

```go
// bitrixInstanceConfig (trong channel_instances.config JSONB, plaintext OK)
type bitrixInstanceConfig struct {
    // ... existing (portal, bot_code, bot_name, policies, ...) ...
    MCPServerName string `json:"mcp_server_name,omitempty"` // mcp_servers.name
    MCPBaseURL    string `json:"mcp_base_url,omitempty"`    // HTTPS root (không có /api/auto-onboard)
}

// bitrixCreds (trong channel_instances.credentials, AES-GCM encrypted) — EMPTY
type bitrixCreds struct{}
```

**Khác với Rev4**: không còn `MCPAdminToken string`. `bitrixCreds` là empty struct, reserve shape cho future per-bot secret (e.g. HMAC) nhưng hiện chưa có gì.

Validation trong factory:
- `MCPServerName` + `MCPBaseURL` phải cùng set hoặc cùng empty (half-config = boot error).
- Nếu set mà `mcpStore == nil` (factory variant không có MCP) → provisioning silently disabled.

### 6.2 MCP HTTP client (shipped — `mcp_client.go`)

```go
type mcpClient struct {
    httpClient *http.Client
    baseURL    string
}

func newMCPClient(baseURL string, timeout time.Duration) *mcpClient {
    if timeout <= 0 { timeout = 10 * time.Second }
    return &mcpClient{
        httpClient: &http.Client{Timeout: timeout},
        baseURL:    strings.TrimRight(baseURL, "/"),
    }
}
```

**Khác với Rev4**: không có `adminToken` field, không set `Authorization` header. Retry policy: 1 auto-retry trên 5xx / network error với 250ms backoff; 4xx không retry; 404 với body `{"error":"tenant_not_installed"}` → `ErrTenantNotInstalled`.

### 6.3 Provisioner hook (shipped — `provisioner.go`)

`provisionIfMissing` được gọi từ `handle.go:189` **sau** contact enrich và **trước** `c.HandleMessage`. Logic:

```
1. IsOpenChannelBot() → ErrProvisionSkippedOpenChannel
2. mcpStore/mcpClient/mcpServerID chưa wire → ErrProvisionDisabled
3. mcpStore.GetUserCredentials(serverID, userID) hit → return nil (cache warm)
4. Debounce 60s trên (serverID, userID) → ErrProvisionDebounced
5. Validate auth.Domain + auth.AccessToken + auth.RefreshToken
6. mcpClient.autoOnboard({Domain, BitrixUserID: userID, Access/Refresh tokens, ExpiresIn})
7. mcpStore.SetUserCredentials(serverID, userID, {
     APIKey: resp.APIKey,
     Env: {
       BITRIX_DOMAIN, BITRIX_ACCESS_TOKEN, BITRIX_REFRESH_TOKEN, BITRIX_EXPIRES_AT
     }
   })
```

Sentinel errors — caller đối xử như warnings, không block message:

```go
var (
    ErrProvisionSkippedOpenChannel = errors.New("...")
    ErrProvisionDisabled           = errors.New("...")
    ErrProvisionDebounced          = errors.New("...")
)
```

Handler trong `handle.go`:

```go
if err := c.provisionIfMissing(ctx, senderID, evt.Auth); err != nil {
    switch {
    case errors.Is(err, ErrProvisionSkippedOpenChannel),
         errors.Is(err, ErrProvisionDisabled),
         errors.Is(err, ErrProvisionDebounced):
        // Silent — expected skip path
    default:
        slog.Warn("bitrix24 mcp: provisioning failed", "err", err, ...)
        c.notifyUserOfMCPIssueOnce(ctx, senderID, evt.Params.DialogID)
    }
}
c.HandleMessage(...)  // ALWAYS runs — MCP failure không block message
```

### 6.4 Why Env map thay vì Headers

`MCPUserCredentials.Env` (encrypted at rest qua partner's `encKey`) lưu:

```
BITRIX_DOMAIN        = auth.Domain
BITRIX_ACCESS_TOKEN  = auth.AccessToken
BITRIX_REFRESH_TOKEN = auth.RefreshToken
BITRIX_EXPIRES_AT    = now + auth.ExpiresIn (RFC3339)
```

Lý do KHÔNG dùng Headers:
- `Headers` được inject vào HTTP call MCP (client → server) — dùng cho thông tin cần xuất hiện trên wire.
- Env dùng để backfill data vào `users` row khi MCP gọi Bitrix REST. Tokens là per-user state, không phải HTTP contract.
- Giữ Env cho phép future: rotate tokens phía MCP mà không cần GoClaw re-onboard (Phase E/F).

Trên MCP side, các biến này hiện chưa đọc (MCP dùng `users.access_token` từ DB — ghi vào lúc `createUser`/`updateUserTokens`). Env GoClaw ghi là redundant nhưng rẻ — giữ cho an toàn nếu MCP muốn đọc credentials thay cho DB row ở Phase E (multi-token per user).

### 6.5 User-facing degradation notice

Khi `provisionIfMissing` fail **ngoài** các sentinel (HTTP 5xx, persist fail, malformed response), channel gửi 1 tin nhắn ngắn cho user:

```
⚠️ Hệ thống đang gặp vấn đề với MCP tools nội bộ. Một số chức năng có thể không
hoạt động như mong đợi. Vui lòng liên hệ admin kỹ thuật để xem lại. Tôi vẫn có
thể trả lời các câu hỏi cơ bản khác.
```

Debounce 5 phút per `userID` (không phải dialogID — 1 user có thể DM bot ở nhiều chat, spam 1 notice per user là OK, per-dialog sẽ spam nếu user chuyển chat).

Không đụng health state — channel vẫn Green vì routing vẫn work.

### 6.6 Files changed (GoClaw side, rev5 snapshot)

| File | Action | Commit |
|---|---|---|
| `internal/channels/bitrix24/channel.go` | Modify — add `mcpStore/mcpClient/mcpServerID/mcpProvMu/mcpDebounce/notifyMu/notifyDebounce/nameCacheMu/nameCache` fields | `ea09c1ba` (phase C) |
| `internal/channels/bitrix24/factory.go` | Modify — `FactoryWithPortalStoreAndMCP` variant + half-config validation; `bitrixCreds` empty struct | `ea09c1ba`, `07b48ef0` |
| `internal/channels/bitrix24/mcp_client.go` | Create — thin HTTP client; Path B no-auth | `ea09c1ba`, `07b48ef0` |
| `internal/channels/bitrix24/provisioner.go` | Create — `provisionIfMissing` + `notifyUserOfMCPIssueOnce` + sentinel errors | `ea09c1ba`, `07b48ef0` |
| `internal/channels/bitrix24/contact_enrich.go` | Create — lazy `user.get` cache cho display_name | `ea09c1ba` |
| `internal/channels/bitrix24/handle.go` | Modify — call `provisionIfMissing` trước `HandleMessage` | `ea09c1ba` |
| `cmd/gateway.go` | Modify — switch to `FactoryWithPortalStoreAndMCP` | `ea09c1ba` |
| `ui/web/src/pages/channels/channel-schemas.ts` | Modify — add `mcp_server_name` + `mcp_base_url` fields; drop `mcp_admin_token` | `ea09c1ba`, `07b48ef0` |

**KHÔNG cần** (khác với Rev4):
- ~~`migrations/000057_bitrix_mcp_user_mapping.up.sql`~~ — không thêm bảng mapping
- ~~`internal/store/bitrix_mapping.go`~~ — reuse MCPServerStore
- ~~`internal/store/pg/bitrix_mapping.go`~~ / ~~`sqlitestore/bitrix_mapping.go`~~
- ~~`internal/upgrade/version.go` bump~~
- ~~SchemaVersion bump~~

### 6.7 Cấu hình MCP server trong GoClaw UI (shipped)

1. **Add MCP Server** qua GoClaw UI:
   - Name: `mcp-bx-syn` (hoặc gì đó khớp với `mcp_server_name` trong channel config)
   - URL: `https://mcp-bx-syn.<account>.workers.dev/mcp`
   - Transport: `streamable_http`
   - Settings (JSON): `{"require_user_credentials": true}`
   - **Không set** server-level API Key (để buộc dùng user credentials)

2. **Channel instance config** (`channel_instances.config`):
   ```json
   {
     "portal": "main",
     "bot_code": "assistant",
     "bot_name": "GoClaw",
     "mcp_server_name": "mcp-bx-syn",
     "mcp_base_url": "https://mcp-bx-syn.<account>.workers.dev"
   }
   ```

3. **Channel instance credentials** (`channel_instances.credentials`): **để trống** (`bitrixCreds` là empty struct).

**Khác với Rev4**: không còn cần `mcp_admin_token` ở đâu cả.

---

## 7. Environment variables

### 7.1 MCP side (Cloudflare Worker)

| Secret | Dùng cho | Rev5 status |
|---|---|---|
| `BITRIX_CLIENT_ID` / `BITRIX_CLIENT_SECRET` | OAuth dance (install + refresh) | Giữ nguyên |
| ~~`ADMIN_TOKEN`~~ | ~~Auth `/api/auto-onboard`~~ | **Bỏ** (Path B không cần) |
| `ENCRYPTION_KEY` | D1 field encryption | Giữ nguyên |
| KV binding `RATE_LIMIT_KV` | Rate limit `/api/auto-onboard` | **Mới** |

### 7.2 GoClaw side

Không cần env var riêng cho MCP integration. Tất cả config sống trong DB:

- `channel_instances.config` (JSONB plaintext): `mcp_server_name`, `mcp_base_url`
- `channel_instances.credentials` (BYTEA AES-GCM): **để trống**
- `mcp_servers` row: operator thêm qua UI 1 lần

**Khác với Rev4**: bỏ `GOCLAW_BITRIX_MCP_ADMIN_TOKEN` env + `mcp_admin_token` credential (commit `07b48ef0`).

---

## 8. Test plan

### 8.1 Unit test MCP side (Path B)

1. Body invalid JSON → 400 `bad_request` + audit row `reason:"invalid_json"`
2. Thiếu `domain` / `bitrix_user_id` / `access_token` / `refresh_token` → 400 + audit `missing:"<field>"`
3. Quá rate limit IP (>600/min) → 429 `rate_limited` + `Retry-After: 60` + audit `scope:"ip"`
4. Quá rate limit domain (>120/min) → 429 + audit `scope:"domain"`
5. KV unreachable → fail-open, request đi tiếp (không 429)
6. `profile` trả về user ID khác → 401 `invalid_bitrix_user` + audit `invalid_bitrix_user`
7. `profile` trả 5xx → 503 `bitrix_unreachable` + audit `bitrix_unreachable`
8. `profile` network fail → 503 `bitrix_unreachable`
9. `profile` OK + ID khớp + domain chưa cài → 404 `tenant_not_installed` + audit
10. User mới → 200 `created:true`, `api_key` prefix `USR_`, label `"goclaw-bot"`, audit `success`
11. User đã có → 200 `created:false`, tokens được update, cùng USR_, audit `success`
12. **Idempotency stampede**: 5 goroutine song song cùng `(domain, bitrix_user_id)` → tất cả trả cùng USR_, không vi phạm `UNIQUE(tenant_id, bitrix_user_id)`

### 8.2 Re-verify test (token-manager.ts)

1. `ensureFreshToken` với `last_verified_at < 1h` → skip verify, chỉ refresh nếu cần
2. `ensureFreshToken` với `last_verified_at > 1h`, profile reachable + active → update `last_verified_at=now`
3. `ensureFreshToken` với profile reachable + !active → throw + `user_status='dismissed'` + deactivate keys
4. `ensureFreshToken` với profile unreachable → fail-open, last_verified_at không update
5. Feature flag `FEATURE_VERIFY_BITRIX_ACTIVE="0"` → skip verify hoàn toàn

### 8.3 Unit test GoClaw side

1. `provisionIfMissing` với Open Channel bot (`TYPE=O`) → `ErrProvisionSkippedOpenChannel`, không call MCP
2. `provisionIfMissing` với mcpStore nil → `ErrProvisionDisabled`, không call MCP
3. `provisionIfMissing` với cred đã tồn tại → return nil, 0 HTTP call
4. `provisionIfMissing` cache miss + thành công → 1 HTTP call + 1 `SetUserCredentials`, Env map có 4 keys
5. 5 goroutine song song cùng `(serverID, userID)` → chỉ 1 HTTP call nhờ debounce, các call sau trả `ErrProvisionDebounced`
6. MCP 503 → slog.Warn, `HandleMessage` **vẫn** được gọi (fail-open)
7. `ErrTenantNotInstalled` (404) → slog.Warn, `HandleMessage` vẫn gọi, `notifyUserOfMCPIssueOnce` được gọi
8. **Notify debounce**: 5 fail liên tiếp cùng user trong 5 phút → `sendChunk` gọi đúng **1 lần**
9. **Notify per-user isolation**: user A fail + user B fail → 2 notice (mỗi user 1)
10. Auth missing `domain`/`access_token`/`refresh_token` → return error trước khi call MCP

### 8.4 Integration test end-to-end

1. Install MCP app lên Bitrix portal test (OAuth dance hoàn tất → 1 row trong `tenants`)
2. User#62 gửi tin nhắn cho bot GoClaw
3. Verify: GoClaw log `bitrix24 mcp: provisioned user credentials` — `created:true`
4. Verify: MCP audit `auto_onboard_audit` có 1 row `event:"success"` cho user#62
5. Agent gọi tool `search` → MCP inject `Authorization: Bearer USR_xxx` → MCP resolve USR_ → gọi Bitrix REST với `access_token` của user#62
6. Bitrix trả về dữ liệu theo ACL của user#62 (verify: data chỉ user#62 thấy được)
7. User#62 gửi tin nhắn lần 2 trong 60s → log `bitrix24 mcp: provisioning debounced` (không call MCP)
8. User#62 gửi tin nhắn lần 3 sau 60s → cache hit (`GetUserCredentials` hit), không call MCP
9. User#63 gửi tin nhắn → onboard lần đầu, `created:true`, không collision với user#62

### 8.5 Edge cases

- User#62 đổi role trong Bitrix → trong vòng 1h, `ensureFreshToken` verify lại → nếu bị dismiss, USR_ invalidate.
- Portal uninstall MCP → tenant bị xoá trong MCP D1 → auto-onboard kế tiếp 404 → user nhận degradation notice 1 lần, sau đó silent.
- MCP worker 503 1 tiếng → debounce 60s giữ retry ở mức 1 call/phút per user; user thấy notice 1 lần (5 phút debounce).
- `MCPServerName` rỗng + `MCPBaseURL` set (half-config) → factory fail boot, admin phải fix config.
- Open Channel bot (`TYPE=O`) → provisionIfMissing always skip; agent gọi MCP tool → không có credentials → tool skip silently (pipeline đã handle).
- Bitrix webhook gửi event với `auth.access_token` rỗng → `provisionIfMissing` return error trước khi call MCP, `HandleMessage` vẫn chạy.

---

## 9. Rollout sequence

### ✅ Phase A — MCP Path B shipped
- [x] Schema: thêm bảng `auto_onboard_audit` (Phase 04 đã có sẵn `user_status` + `last_verified_at` — reuse, không migrate lại)
- [x] `verifyBitrixActive` via `profile` (thay cơ chế `user.get?FILTER[ID]=…&ACTIVE=true` cũ)
- [x] `/api/auto-onboard` rewrite Path B (bỏ Bearer ADMIN_TOKEN gate)
- [x] Rate limit KV + audit log
- [x] Hook `verifyBitrixActive` vào `ensureFreshToken` (re-verify hourly — cơ chế hourly đã có Phase 04, chỉ đổi backend verify)
- [x] Deploy + smoke test (end-to-end user#62)

### ✅ Phase B — GoClaw channel integration shipped (commit `ea09c1ba`)
- [x] Channel struct + Factory MCP variant
- [x] `mcp_client.go` + `provisioner.go` + `contact_enrich.go`
- [x] Hook `provisionIfMissing` trước `HandleMessage`
- [x] `FactoryWithPortalStoreAndMCP` thay `FactoryWithPortalStore`
- [x] UI form: `mcp_server_name` + `mcp_base_url`

### ✅ Phase C — Drop ADMIN_TOKEN (commit `07b48ef0`, local `dev` branch, pending push)
- [x] Remove `adminToken` from `mcpClient`
- [x] Remove `resolveMCPAdminToken` + 2 env consts
- [x] Remove `BITRIX_MCP_ADMIN_TOKEN` UI field + docstrings
- [x] Update tests (drop admin-token branches)
- [x] `go test -race` passes, `go vet` clean

### Phase D — Backfill + cleanup (pending)
- [ ] **Migration cho `bitrix_user_id "62.0"` → `"62"`**: một số row cũ bị coerce qua float (schema là TEXT nhưng input trước đây là number). Script một lần: `UPDATE users SET bitrix_user_id = CAST(CAST(bitrix_user_id AS REAL) AS INTEGER) WHERE bitrix_user_id LIKE '%.0'`.
- [ ] Push commit `07b48ef0` lên `origin/dev` sau khi rev5 được approve.
- [ ] Marketplace rollout checklist: update docs cho customer, announce breaking change (nếu ai đang dùng ADMIN_TOKEN trong prod tự host).

### Phase E — Shared-credential cho Open Channel (future)
- [ ] Design: 1 shared USR_ key per bot cho khách Open Channel, ACL theo scope của bot (không theo user vì không có tenant_users).
- [ ] Thay sentinel `ErrProvisionSkippedOpenChannel` bằng shared-creds lookup.
- [ ] Test với real Open Channel từ widget external chat.

---

## 10. Security considerations

### 10.1 Path B auth anchor

- **Trust boundary**: MCP tin `access_token` là thật vì Bitrix `profile` xác nhận nó thuộc user nào. Attacker muốn mint USR_ cho user X phải có access_token hợp lệ của user X — mà access_token chỉ leak được nếu Bitrix portal đã bị compromise (trong trường hợp đó attacker đã có quyền cao hơn nhiều so với USR_).
- Không còn "master key" → không có rotate periodic; cũng không có single point of credential leak.

### 10.2 Rate limiting

- 600/min/IP chống brute force từ 1 IP.
- 120/min/domain chống 1 portal bị compromise spam onboard user giả.
- Fail-open trên KV outage (uptime ưu tiên hơn): KV outage hiếm và rate limit không phải security control duy nhất — profile verify vẫn chạy.

### 10.3 Audit log

- `auto_onboard_audit` ghi mọi attempt kèm IP (`cf-connecting-ip`), event kind, metadata. Operator query được "portal X có bao nhiêu onboard fail hôm qua" không cần grep Cloudflare logs.
- Swallow error khi ghi audit — không block onboard nếu D1 tạm 5xx.

### 10.4 Dismissed user revocation

- `ensureFreshToken` chạy trên mỗi MCP call. Nếu user bị dismiss khỏi Bitrix → trong vòng 1h (`VERIFY_STALE_MS`) MCP call tiếp theo sẽ verify lại → fail → `user_status='dismissed'` + `deactivateUserApiKeys` → tất cả USR_ của user đó die.
- Không có cơ chế "revoke on user-delete" realtime (không subscribe Bitrix event user.delete). 1h delay là tradeoff giữa độ trễ revoke và load lên Bitrix `profile`.

### 10.5 Cross-tenant isolation (GoClaw side)

- `provisionIfMissing` gọi `mcpStore.SetUserCredentials(ctx, serverID, userID, creds)`. PG impl dùng `tenantIDForInsert(ctx)` → nếu ctx không có tenant_id sẽ ghi vào tenant sai / nil.
- Tenant injection xảy ra **ở webhook handler**, không phải ở channel: `webhook.go:436` wrap `ctx := store.WithTenantID(context.WithoutCancel(req.Context()), portal.TenantID())` trước khi dispatch event. Ctx này propagates qua `DispatchEvent` → `handleMessage` → `provisionIfMissing` → `SetUserCredentials`, nên tenant luôn đúng với portal khớp `auth[domain]`.
- Test `8.3 #4` (SetUserCredentials insert thành công) + unit test riêng cho `tenantIDForInsert(ctx)` đảm bảo không leak.

### 10.6 Token leak surface

- OAuth tokens (access/refresh) lưu 2 chỗ:
  1. MCP D1 `users.access_token`/`refresh_token` — plaintext trong D1 (xem xét field encryption Phase F nếu cần)
  2. GoClaw `mcp_user_credentials.env_json` — AES-GCM encrypted by partner store
- Log: KHÔNG log full USR_ hoặc access_token. `mcp_client.go` redact body trong error message tới 500 ký tự và chỉ trong error path.

---

## 11. Open questions

1. **Phase D migration `"62.0" → "62"`**: bao nhiêu row bị ảnh hưởng trong D1 production? Cần query trước rồi script UPDATE một lần, hay đợi tự nhiên qua token refresh cycle? → đang nghiêng về script một lần vì idempotency không gặp vấn đề (bitrix_user_id là TEXT — `"62"` và `"62.0"` là 2 row khác nhau → user gửi lần kế tiếp sẽ tạo row mới, row cũ mồ côi). Cần schedule sớm.
2. **Phase E Open Channel shared creds**: một bot có thể gắn vào nhiều Open Channel queue khác nhau. 1 USR_ per bot đủ, hay cần 1 USR_ per (bot, queue)? Phụ thuộc use case — nếu permissions per queue khác nhau thì cần (bot, queue) key.
3. **Field encryption cho `users.access_token` trong D1**: hiện plaintext. Cloudflare D1 hỗ trợ at-rest encryption ở storage layer, nhưng không phải application-level. Nếu compliance yêu cầu → thêm field-level AES-GCM tương tự partner store của GoClaw. Chưa urgent — D1 access đã bị throttle qua Worker RBAC + Cloudflare account access.
4. **Webhook uninstall → revoke users**: khi admin uninstall MCP app khỏi portal, tenant row bị xoá, users của portal đó mồ côi. Hiện `users.tenant_id REFERENCES tenants(id)` **không có** `ON DELETE CASCADE` (verified trong schema.sql) — chỉ `idempotency_keys` có. Mitigation: (a) thêm CASCADE trên `users.tenant_id` + `api_keys.user_id` hoặc (b) hook webhook uninstall → explicit deactivate keys. (a) đơn giản hơn nhưng breaking với audit (xoá users = mất trace), (b) giữ row chỉ set `active=0`.
5. **`display_name` enrichment**: Bitrix webhook không carry `USER[NAME]` — GoClaw `contact_enrich.go` lazy `user.get` tại channel, nhưng MCP `createUser` không nhận display_name từ webhook → user rows có `display_name = NULL` trong MCP. Không critical (không ai hiển thị MCP user list ở UI hiện tại) nhưng nếu cần, GoClaw có thể forward `display_name` từ cache khi gọi `/api/auto-onboard`.
6. **Latency đo thực tế**: tin nhắn đầu của mỗi user onboard + verify = 2 Bitrix REST call (profile) + 1 D1 insert ≈ 300-600ms. Cache miss mỗi user chỉ 1 lần (lifetime). Nếu đo thấy spike khó chịu ở tin đầu → pre-warm khi bot được add vào chat (`handleJoin`), nhưng Bot Join event không có user_id của tất cả member → chỉ pre-warm được người add bot. Giữ lazy cho đơn giản.

---

## 12. Changelog

- **2026-04-23 (rev5)**: Path B shipped end-to-end. Bỏ ADMIN_TOKEN.
  - MCP side: `/api/auto-onboard` rewrite dùng `verifyBitrixActive(profile)` làm auth anchor thay vì Bearer ADMIN_TOKEN. Thêm rate limit KV (600/min IP + 120/min domain), audit log `auto_onboard_audit`. Hourly re-verify trong `ensureFreshToken` để revoke USR_ của user bị dismiss khỏi Bitrix.
  - GoClaw side: `mcpClient` bỏ `adminToken` field + Authorization header. `provisioner.go` bỏ `resolveMCPAdminToken` + 2 env consts (`GOCLAW_BITRIX_MCP_ADMIN_TOKEN`, `BITRIX_MCP_ADMIN_TOKEN`). `bitrixCreds` chuyển về empty struct. UI form drop `mcp_admin_token` field. (commit `07b48ef0`)
  - Bỏ bảng mapping `bitrix_mcp_user_mapping` khỏi plan — reuse partner's `mcp_user_credentials` store. Tiết kiệm ~300 LOC: interface + 2 store impls + migration + SchemaVersion bump.
  - Thêm user-facing degradation notice (`notifyUserOfMCPIssueOnce`) với 5-phút debounce per-user khi MCP fail ngoài sentinel.
  - Status summary: MCP side ✅ deployed; GoClaw phase C ✅ landed (commit `ea09c1ba`); GoClaw ADMIN_TOKEN cleanup ✅ on local `dev` (commit `07b48ef0`, pending push).
- **2026-04-22 (rev4)**: MCP side implemented. Đồng bộ plan với MCP schema thật + live event payload:
  - Tenant key đổi từ `member_id` → `domain` (MCP schema có `tenants.domain UNIQUE`, không có `member_id`)
  - Contract body bổ sung `access_token`, `refresh_token`, `expires_in` — forward từ `auth[...]` của Bitrix bot event (cần thiết vì `users.access_token NOT NULL` trong `createUser`)
  - `bitrix_user_id` lưu dạng `TEXT` (khớp schema) — handler coerces number → string
  - Migration 4.1 **skipped** (`users.bitrix_user_id` + `UNIQUE(tenant_id, bitrix_user_id)` đã tồn tại)
  - GoClaw mapping table đổi `bitrix_member_id` → `bitrix_domain`, `bitrix_user_id BIGINT` → `TEXT`
  - `AutoOnboardReq` Go struct cập nhật 6 field (Domain, BitrixUserID string, AccessToken, RefreshToken, ExpiresIn, DisplayName)
  - `ensureMCPCredentials` đọc thêm `evt.Auth.{Domain,UserID,AccessToken,RefreshToken,ExpiresIn}` — fallback `evt.Params.FromUserID` nếu `Auth.UserID` vắng
  - Files changed: `src/api/auto-onboard.ts` (new), `src/api/api-router.ts` (route), `wrangler.toml` (comment)
- **2026-04-22 (rev3)**: Thêm debounce `replyError` (section 6.3.1):
  - Chống chat spam khi MCP down trong group chat đông member
  - Chống self-DoS qua Bitrix rate-limit trên `imbot.message.add` (~2 RPS cap)
  - Key theo `dialogID` (chat room), không phải `senderID`, để 10 user trong cùng 1 group = 1 reply
  - Thêm field `mcpErrorMu, mcpErrorLast map[string]time.Time` vào Channel struct
  - Thêm 3 test case unit (§8.2 #8-10)
- **2026-04-22 (rev2)**: Rewrite sau khi verify plan với live source `goclaw-deploy/goclaw/`. Sửa:
  - `event.Auth.UserID` không tồn tại → dùng `evt.Params.FromUserID` + `strconv.Atoi`
  - Bỏ `event.User.Email` (Event không có field này)
  - Thêm section 6.0 plumbing Channel fields + factory signature
  - Bắt buộc `store.WithTenantID(ctx, tid)` trước `SetUserCredentials` (PG impl dùng `tenantIDForInsert(ctx)`)
  - Migration: PG + SQLite dual-DB, thêm FK cascade, thêm `updated_at`, align `uuid_generate_v7()` pattern
  - Đổi tên migration thành `000057_bitrix_mcp_user_mapping`
  - `ADMIN_TOKEN` vào `bitrixCreds` (encrypted), không phải `bitrixInstanceConfig`
  - `Upsert` dùng `ON CONFLICT DO UPDATE last_used_at` cho stampede safety
  - Thêm helper `replyError`, fail-open logic
  - Flag bug có sẵn `MESSAGE_TYPE==chat` (Bitrix thực gửi `P|B`) — fix cùng PR
- **2026-04-22 (rev1)**: Plan khởi tạo, đã chốt 4 quyết định chính.
