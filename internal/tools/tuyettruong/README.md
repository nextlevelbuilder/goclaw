# Tuyettruong tools

Goclaw tools that call the tuyettruong Next.js store admin API.

## Files

| File | Purpose |
|---|---|
| `client.go` | Shared HTTP client. Reads env, builds actor header from goclaw ctx. |
| `product_search.go` | `tt_product_search` — keyword search (calls public `/store/products-search`) |
| `product_get.go` | `tt_product_get` — full product+variants (calls `/admin/products/{slug}`) |
| `product_create.go` | `tt_product_create` — create new product |
| `product_update.go` | `tt_product_update` — patch product metadata |
| `variant_update.go` | `tt_variant_update` — patch variant (price, stock, …) |
| `product_delete.go` | `tt_product_delete` — destructive, requires `confirm_token=XOA-<slug>` |
| `product_lookup_existing.go` | `tt_product_lookup_existing` — dedup pre-flight by AUST L / parentSku / name fuzzy |
| `product_draft_from_extracted.go` | `tt_product_draft_from_extracted` — create DRAFT (active=false) from vision-extracted fields |
| `order_list.go` | `tt_order_list` — filterable by status |
| `order_update_status.go` | `tt_order_update_status` — confirm payment, cancel, etc. |
| `quote_state.go` | Per-session draft store (in-memory, 24h TTL) |
| `quote_renderer.go` | Text quote formatter + chunker for Zalo's 2000-char limit |
| `quote_tools.go` | `quote_add_item`, `_remove_item`, `_view`, `_set_customer`, `_finalize`, `_clear` |
| `order_place.go` | `order_place` — submits draft as real order, idempotent |
| `order_customer_claimed_paid.go` | `order_customer_claimed_paid` — customer says "đã CK" |
| `order_lookup.go` | `order_lookup` — customer support questions |
| `notify_admin.go` | `notify_admin` — v1 logs only; future push via goclaw bus |
| `register.go` | `RegisterAll(reg)` wires all of the above into a goclaw tool registry |
| `seeds/admin_agent_system_prompt.md` | Vietnamese system prompt for the admin-agent |
| `seeds/seed_admin_agent.sql` | Manual SQL to insert admin agent + Telegram channel |
| `seeds/sales_agent_system_prompt.md` | Vietnamese system prompt for the sales-agent |
| `seeds/seed_sales_agent.sql` | Manual SQL to insert sales agent + Zalo Personal channel |

## Auth model

The agent has its own **Supabase user account** (role=admin) and authenticates the same way a logged-in human admin would. Client logs in once at process start, caches the JWT, refreshes 5 minutes before expiry. Every API call sends `Authorization: Bearer <jwt>`. No bot-specific API keys; no machine tokens.

### Create the agent's user account (once)

In tuyettruong repo:
```bash
npm run auth:create-supabase-admin -- --email=gia-han@tuyettruong.bot --password=<strong-pwd> --role=admin
```

(Email can be anything not used by a real customer — `*.bot` subdomain is a clear convention.)

### Env vars

| Name | Purpose |
|---|---|
| `TUYETTRUONG_API_BASE` | e.g. `https://tuyettruong.com` |
| `TUYETTRUONG_SUPABASE_URL` | Same as `NEXT_PUBLIC_SUPABASE_URL` in tuyettruong |
| `TUYETTRUONG_SUPABASE_ANON_KEY` | Same as `NEXT_PUBLIC_SUPABASE_ANON_KEY` |
| `TUYETTRUONG_AGENT_EMAIL` | The agent's account email |
| `TUYETTRUONG_AGENT_PASSWORD` | The agent's account password |

Missing any of the above → `RegisterAll` logs the list and skips tool registration cleanly. Goclaw still boots.

## Boot wiring

Already wired in `cmd/gateway_setup.go` (search for `tuyettruong.RegisterAll`).

## Quick start: test with existing agent (gia-han)

If you already have a working test agent in goclaw (e.g. `gia-han`), you don't need to seed `tuyettruong-admin` or `tuyettruong-sales` yet. Single-agent setup:

1. Set env (point both keys to the same value — admin perms cover sales too):
   ```
   TUYETTRUONG_API_BASE=https://tuyettruong.com
   TUYETTRUONG_ADMIN_BOT_API_KEY=<same as BOT_ADMIN_API_KEY in tuyettruong>
   TUYETTRUONG_SALES_BOT_API_KEY=<same as above>
   ```
2. Restart goclaw → log line `tuyettruong tools registered` confirms wiring
3. In goclaw admin UI, edit `gia-han` and add to `tools_config.allow`:
   ```
   tt_product_search, tt_product_get, tt_product_create, tt_product_update,
   tt_variant_update, tt_product_delete,
   tt_product_lookup_existing, tt_product_draft_from_extracted,
   tt_order_list, tt_order_update_status,
   web_fetch, web_search,
   sales_product_search,
   quote_add_item, quote_remove_item, quote_view, quote_set_customer,
   quote_finalize, quote_clear,
   order_place, order_customer_claimed_paid, order_lookup, notify_admin
   ```
4. Paste system prompt from `seeds/combined_test_agent_system_prompt.md`
5. In tuyettruong `/admin/bot-identities`: add anh's TG user_id with role=admin
6. DM gia-han: "tìm áo trắng" → should work as sales; "list đơn mới" → admin flow
7. Send a product photo (e.g. HAPPi Baby Lactoferrin box) → agent extracts fields, fetches manufacturer page via `web_fetch` for clean images, drafts as inactive. Note: `web_fetch` + `web_search` are required for the photo-ingest flow.

When ready to split into 2 dedicated agents later, use `seed_admin_agent.sql` + `seed_sales_agent.sql` and switch the system prompts accordingly.

## Setup checklist (admin agent on Telegram)

1. `@BotFather` → `/newbot` → save the token
2. Set env vars in goclaw (`.env` or systemd unit):
   ```
   TUYETTRUONG_API_BASE=https://tuyettruong.com
   TUYETTRUONG_ADMIN_BOT_API_KEY=<same as tuyettruong BOT_ADMIN_API_KEY>
   ```
3. Boot goclaw once — logs should show `tuyettruong admin tools registered`
4. Open the goclaw admin UI (or use `seed_admin_agent.sql`) to create the agent + channel_instance
5. In tuyettruong: `Admin → Bot identities → Thêm identity` (platform=telegram, user_id=<anh's TG id>, role=admin)
6. Find your TG user_id by DMing `@userinfobot`
7. Test by DM'ing your bot: try "tìm áo tuyết" — should call `tt_product_search`

## Actor header

The tools derive `X-Bot-Actor-Id` from goclaw session context (channel + chatID).
For Telegram DMs: `tg:<userId>`. The tuyettruong middleware (`require-permission.ts`)
looks this up in `bot_identities` and rejects if not found.

## Adding a new tool

1. Create `<name>.go` implementing the `tools.Tool` interface
2. Add a `RegisterWithMetadata(...)` line in `register.go`
3. Add the tool name to the admin agent's `tools_config.allow` array (re-seed or update via UI)
4. Add a usage line to `seeds/admin_agent_system_prompt.md`
5. Rebuild goclaw
