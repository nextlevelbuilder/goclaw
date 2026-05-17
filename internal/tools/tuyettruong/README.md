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

## Env vars

| Name | Purpose |
|---|---|
| `TUYETTRUONG_API_BASE` | Base URL (e.g. `https://tuyettruong.com`). Required — without it, RegisterAll skips. |
| `TUYETTRUONG_ADMIN_BOT_API_KEY` | Matches `BOT_ADMIN_API_KEY` in tuyettruong env. Sent as `x-api-key`. |
| `TUYETTRUONG_SALES_BOT_API_KEY` | Matches `BOT_SALES_API_KEY` in tuyettruong env. Used by sales tools (quote_*, order_*, notify_admin). |

## Boot wiring

Already wired in `cmd/gateway_setup.go` (search for `tuyettruong.RegisterAll`).

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
