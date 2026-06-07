# Tuyettruong / shop tools

Goclaw tools that call the Tuyettruong Next.js store admin API.

The current Go adapter is still backed by one Tuyettruong API credential set
via `TUYETTRUONG_*` env vars. Runtime tool names are generic `shop_*` so agents,
prompts, and provisioning stay brand-agnostic while the HTTP client is migrated
toward per-shop config.

## Files

| File | Purpose |
|---|---|
| `client.go` | Shared HTTP client. Reads env, builds actor header from goclaw ctx. |
| `product_search.go` | `shop_product_search` — keyword search (calls public `/store/products-search`) |
| `product_get.go` | `shop_product_get` — full product+variants (calls `/admin/products/{slug}`) |
| `product_create.go` | `shop_product_create` — create new product |
| `product_update.go` | `shop_product_update` — patch product metadata |
| `variant_update.go` | `shop_variant_update` — patch variant (price, stock, …) |
| `product_delete.go` | `shop_product_delete` — destructive, requires `confirm_token=XOA-<slug>` |
| `product_lookup_existing.go` | `shop_product_lookup_existing` — dedup pre-flight by AUST L / parentSku / name fuzzy |
| `product_draft_from_extracted.go` | `shop_product_draft_from_extracted` — create DRAFT (active=false) from vision-extracted fields |
| `order_list.go` | `shop_order_list` — filterable by status |
| `order_update_status.go` | `shop_order_update_status` — confirm payment, cancel, etc. |
| `quote_state.go` | Per-session draft store (in-memory, 24h TTL) |
| `quote_renderer.go` | Text quote formatter + chunker for Zalo's 2000-char limit |
| `quote_tools.go` | `shop_quote_add_item`, `_remove_item`, `_view`, `_set_customer`, `_finalize`, `_clear` |
| `order_place.go` | `shop_order_place` — submits draft as real order, idempotent |
| `order_customer_claimed_paid.go` | `shop_order_customer_claimed_paid` — customer says "đã CK" |
| `order_lookup.go` | `shop_order_lookup` — customer support questions |
| `notify_admin.go` | `shop_notify_admin` — v1 logs only; future push via goclaw bus |
| `register.go` | `RegisterAll(reg)` wires all of the above into a goclaw tool registry |
| `seeds/admin_agent_system_prompt.md` | Vietnamese system prompt for the admin-agent |
| `seeds/seed_admin_agent.sql` | Manual SQL to insert admin agent + Telegram channel |
| `seeds/sales_agent_system_prompt.md` | Vietnamese system prompt for the sales-agent |
| `seeds/seed_sales_agent.sql` | Manual SQL to insert sales agent + Zalo Personal channel |
| `seeds/shop-bot-template.md` | Generic single-agent shop prompt with `{{placeholders}}` |
| `seeds/shop-bot.example.json` | Example shop config for the provisioning script |

## Multi-shop provisioning

Do **not** clone Gia Han or hand-edit prompts for each shop. Keep three layers
separate:

| Layer | Owner | Example |
|---|---|---|
| Agent behavior | template | `shop-bot-template.md` |
| Chat identity + permission | GoClaw | `channel_contacts`, tenant users, channel allowlists, agent scopes |
| Shop data | shop API | products, variants, orders, quotes |

Generate reviewable SQL:

```bash
node scripts/provision-shop-agent.mjs \
  --config internal/tools/tuyettruong/seeds/shop-bot.example.json \
  > /tmp/tuyet-truong-shop-agent.sql
```

The generated SQL intentionally ends with `ROLLBACK;`. Review the output, switch
to `COMMIT;`, then apply. Channel credentials are intentionally empty and the
sample channel is disabled by default; add encrypted credentials via the GoClaw
UI/API, set a small allowlist, then enable the channel.

This gives each shop a clean `agent_key`, context files, tool allowlist, and
optional disabled channel instance without rebuilding Docker.

### Recommended rollout

1. Create one bot agent per shop with the generator.
2. Keep channels disabled until credentials + allowlist are set.
3. Link one admin contact to a GoClaw-managed identity and allowlist.
4. Move Zalo/Telegram channel from any personal assistant agent to the shop bot.
5. Keep prompts and allowlists on `shop_*`; migrate only adapter config when a
   new shop backend is added.

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

## Quick start: shop operator agent

1. Set env:
   ```
   TUYETTRUONG_API_BASE=https://tuyettruong.com
   TUYETTRUONG_SUPABASE_URL=<same as shop app>
   TUYETTRUONG_SUPABASE_ANON_KEY=<same as shop app>
   TUYETTRUONG_AGENT_EMAIL=<agent account>
   TUYETTRUONG_AGENT_PASSWORD=<agent password>
   ```
2. Restart goclaw → log line `shop tools registered` confirms wiring.
3. In goclaw admin UI, create or edit the shop agent and add:
   ```
   group:shop,
   web_fetch, web_search,
   ```
4. Route the shop Zalo/Telegram channel to the shop agent.
5. Put shop operators in the GoClaw channel allowlist/contact layer.
6. Test: "tìm son" → `shop_catalog_search`; "list đơn mới" → `shop_order_list`.

## Actor header

The tools derive `X-Bot-Actor-Id` from goclaw session context (channel + chatID).
For Telegram DMs: `tg:<userId>`. For Zalo DMs: `zalo_personal:<userId>`.
GoClaw owns channel routing and allowlists; the shop API receives actor context
for audit/compatibility.

## Adding a new tool

1. Create `<name>.go` implementing the `tools.Tool` interface
2. Add a `RegisterWithMetadata(...)` line in `register.go`
3. Add the tool name to the admin agent's `tools_config.allow` array (re-seed or update via UI)
4. Add a usage line to `seeds/admin_agent_system_prompt.md`
5. Rebuild goclaw
