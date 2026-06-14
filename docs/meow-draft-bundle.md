# Meow draft bundle schema

The offline draft-prep workflow writes one **bundle** per channel-day into the
source project: `<project>/marketing/meow/drafts/<date>.json` next to its
`<date>.webp` image. Ingest reads the bundle server-side, validates it,
transports the image into the meow-assets root, and upserts an
`mp_content_posts` row as `draft`. Type: `meow.DraftBundle`.

## Shape

```json
{
  "handle": "@kingboardgamesofficial",
  "scheduled_date": "2026-06-16",
  "ko_text": "오늘의 대진표가 열립니다.",
  "en_text": "Today's bracket opens.",
  "image": "2026-06-16.webp",
  "buttons": [
    { "label": "Play now", "url": "https://t.me/holdemblitz_bot" }
  ]
}
```

| Field | Rule |
|---|---|
| `handle` | required, must start with `@`; resolved to a channel at ingest |
| `scheduled_date` | required, `YYYY-MM-DD` (channel-local calendar day) |
| `ko_text` / `en_text` | at least one non-empty; stacked KO-over-EN at publish |
| `image` | required; a **bare WebP filename** (no `/`, `\`, or `..`) within the bundle dir |
| `buttons` | each needs a non-empty `label` and an `https://` `url` |

Unknown JSON keys are rejected (strict decode) so a typo never silently drops
content.

## Image & path rules
- The bundle's `image` is just a filename. The real bytes are transported to the
  server and ingest copies them to the canonical container path
  `/app/data/meow-assets/drafts/<channel>/<date>.webp` (see
  `docs/meow-image-transport.md`). `mp_content_posts.image_path` stores **only**
  that container path — never a Mac path or a VPS host-volume path.
- Images are produced with `gpt-image-2-pro-max` + GPT Image 2 OAuth rendering,
  reference-first from project-local brand assets (`/codex-imagen` only as a
  compliant local wrapper).
- **Mascot rule:** include a mascot reference **only** for the four mascot
  channels — `@onedollar_project`, `@OneJackpotOfficial`, `@monkeytimeofficial`,
  `@MonkeyMatgo`.

## Button rules
- Structural validation (schema) requires `https://` URLs.
- **Exact-URL allowlist** is enforced at ingest against the channel's registered
  `button_set` (seeded from the plan's button appendix) plus the host allowlist —
  a link on an allowed host but not registered (e.g. `t.me/<other-bot>`) is
  rejected. See `meow.ValidateButtonURL`.

## Hold reasons (bundle does NOT become a draft)
Ingest holds — no `mp_content_posts` row — when:
- structural validation fails (bad handle/date, no text, image-as-path, non-https button);
- the handle does not resolve to a known channel;
- the image file is missing or not a readable file;
- a button URL is not in the channel's registered allowlist.

A held bundle is reported with its reason; it never becomes an approvable /
publishable post.
