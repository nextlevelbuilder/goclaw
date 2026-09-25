# Zalo Official Account (OAuth v4)

Connect a Zalo Official Account to GoClaw. This is **not** the Zalo Bot API and
not Zalo Personal.

Create the Zalo application and Official Account in the
[Zalo Developers](https://developers.zalo.me) console.

| Surface | Type | Use when |
|---------|------|----------|
| Official Account | `zalo_oa` | Customer-facing OA chat (this guide) |
| Bot API | `zalo_bot` | A bot created in Zalo Bot Manager/Creator |
| Personal | `zalo_personal` | A personal Zalo account (unofficial; ban risk) |

Static `channels.zalo` in `config.json` is still the Bot API.

## What you need

1. A Zalo **app** and an **Official Account**.
2. Two different secrets from that app:
   - **Secret Key** — OAuth v4 app secret (`secret_key`). Used only to exchange
     tokens at `https://oauth.zaloapp.com/v4`.
   - **Webhook Secret Key** — signing secret under OA → Webhook
     (`webhook_secret_key`). Used only to verify `X-ZEvent-Signature`.
     Do not paste one into the other field.
3. A public **HTTPS** origin Zalo can reach.
4. `GOCLAW_ENCRYPTION_KEY` on the gateway. Channel credentials are stored
   encrypted with that key. If it is empty, secrets are saved in plaintext.

### Public origin

Zalo must call two different public URLs:

| URL | Served by | Register in Zalo as |
|-----|-----------|---------------------|
| Callback `/oauth/zalo/callback` | **Web UI** (the dashboard SPA) | OA Callback / redirect URI |
| Webhook `/channels/zalo/webhook/<slug>` | **Gateway** HTTP | OA → Webhook |

Set a public HTTPS origin in this order:

1. `GOCLAW_PUBLIC_URL` (environment; overrides `gateway.public_url`)
2. `gateway.public_url` in config
3. If both are unset, the gateway may infer an origin from an authenticated
   public request. Loopback and private hosts are rejected.

When `GOCLAW_PUBLIC_URL` or `gateway.public_url` is set, OA callback and
webhook URLs keep that origin. Later request `Host` values do not replace it.

If the dashboard is hosted on a **different origin** than the API, the callback
URL must be the **dashboard** origin (where `/oauth/zalo/callback` is served),
not the backend-only origin. The webhook URL still uses the gateway origin.
A mismatch with the URI registered in Zalo returns error `-14003`.

Copy the URLs the dashboard prints. Do not construct them by hand.

## Dashboard setup

1. Open the web dashboard → **Channels** → **Add** → **Zalo OA**.
2. Name the instance. The name becomes the webhook slug unless you set
   `webhook_path`.
3. Enter **App ID** and OAuth **Secret Key**.
4. Set **Transport** to Webhook (default) or Polling.
5. Set **DM Policy** to Pairing (default) or Allowlist. Pairing sends a pairing
   code to unknown senders and ignores that message until they are paired;
   allowlist accepts only IDs in **Allowed Users**.
6. The form previews **Callback URL** and, for webhook transport, **Webhook URL**
   before you create the channel. Copy them.
7. In [Zalo Developers](https://developers.zalo.me) → your app:
   - OA settings: paste the Callback URL exactly.
   - OA → Webhook: paste the Webhook URL and save. Zalo pings the URL first;
     only then does it show the webhook signing secret.
8. Paste that **Webhook Secret Key** into the form (webhook transport only;
   required before Create).
9. Create the channel, then **Connect**: approve the Official Account in the
   Zalo window. If the popup does not finish by itself, copy the full browser
   URL (it contains `code` and `state`) and paste it into step 2 of the dialog.

The channel stays degraded until consent completes. After connect,
`auth_connected` is true only when both access and refresh tokens are stored.

Interactive `goclaw channels add` can create **Zalo Bot** with a Bot Manager
token. Official Account consent stays on the dashboard (or the HTTP/WS
actions below). Desktop channel setup is not available.

## Configuration

Dashboard defaults match this instance config:

```json
{
  "dm_policy": "pairing",
  "transport": "webhook"
}
```

Useful knobs:

| Field | Default | Meaning |
|-------|---------|---------|
| `dm_policy` | `pairing` | `pairing`, `allowlist`, `open`, or `disabled` |
| `allow_from` | empty | Zalo user IDs; required for `allowlist` |
| `transport` | `webhook` | `webhook` or `polling` |
| `webhook_path` | derived from name | Webhook slug |
| `poll_interval_seconds` | 15 | Polling only |
| `quote_user_message` | off | Quote the user's last message in replies |

Polling does not use a webhook URL or webhook secret. It reads recent chats
after consent and is text-only.

## Two URLs, two secrets, two-phase webhook

Zalo will not show the webhook signing secret until the webhook URL answers a
save ping.

**Dashboard:** the create form previews those URLs first. For webhook
transport, **Webhook Secret Key is required** before Create. Register the
previewed webhook URL, paste the secret, then create, then Connect.

**HTTP API:** you may create the instance without `webhook_secret_key` so
Zalo can ping the route (bootstrap). Events stay unsigned and are not
processed until you save the secret. Then run consent.

OAuth Secret Key and Webhook Secret Key stay separate for the life of the
channel.

## Consent over HTTP

Same flow as the dashboard, for operators who prefer curl. Tenant-admin token
required.

```bash
curl -sS -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  "$GATEWAY/v1/channels/setup/zalo-oa?name=sales-oa"

curl -sS -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  "$GATEWAY/v1/channels/instances/$INSTANCE_ID/zalo-oa/consent"

curl -sS -X POST -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"code":"<callback-code>","state":"<state>","oa_id":"<oa-id>"}' \
  "$GATEWAY/v1/channels/instances/$INSTANCE_ID/zalo-oa/exchange-code"
```

WebSocket twins: `channels.instances.zalo_oa.callback_url`,
`channels.instances.zalo_oa.consent_url`,
`channels.instances.zalo_oa.exchange_code`.

`goclaw channels list --json` includes `auth_connected`, `webhook_url`, and
`callback_url`.

Consent state lives in the gateway process that minted it (about 10 minutes,
a few pending attempts per instance). Start Connect and finish the paste on
the **same** running gateway.

Refresh tokens are single-use. If the channel shows auth failure, use
**Reconnect** and approve the OA again.

## Media

The OA channel is media-capable. Several files in one reply are sent as
**successive messages**, in order, not as a Zalo album. Each file's caption
follows that file. Any remaining reply text is sent after the last file.
An unsupported file type becomes a short text note; the rest of the batch
still sends. If a later item fails after an earlier one succeeded, the
earlier files are already delivered.

Zalo rejects oversize uploads (error `-210`):

| Kind | Cap | Notes |
|------|-----|-------|
| Image | 1 MiB | jpeg/png |
| GIF | 5 MiB | |
| File | 5 MiB | PDF, DOC, DOCX |
| Text | 2,000 characters | Long replies are split |

Webhook inbound can include images and files. Polling inbound is text only.

## Moving off the old `zalo_oa` Bot type

Installations that stored the Bot API as `channel_type=zalo_oa` are retyped to
`zalo_bot`. New `zalo_oa` is OAuth OA only. Update any dashboard or alert that
filters on the old name. `channels.zalo` in config is unchanged (Bot).

## Attribution

Adapted from [nextlevelbuilder/dewee](https://github.com/nextlevelbuilder/dewee)
under [CC BY-NC 4.0](https://creativecommons.org/licenses/by-nc/4.0/legalcode):

- HEAD `760bf01ea9ac90bbc886f20475b267d44c00c217`
- `f1d300ed3d1ee1ee26c156042ed60be0a97477d5` — Official Account OAuth v4 channel
- `3e328542edaf01ff2d10d782530ec392d1a9de7d` — review follow-ups

Module path and public-URL wiring are GoClaw-specific. This is an adaptation,
not an endorsement by the dewee authors.
