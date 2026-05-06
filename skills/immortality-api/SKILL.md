---
name: immortality-api
description: "Use this skill to publish/edit content on immortality.vn / battudao.com via the agent-first REST API at https://battudao.com/api. Handles BOTH 'articles' (long-form essays) AND 'khaitri' (Q&A entries) — agent auto-classifies content, NEVER asks user which collection. Triggers: 'đăng bài', 'post bài', 'đăng Khai Trí', 'sửa bài', 'fix bài', 'list bài', 'xóa bài'. Workflow: (1) GET /api/agent-spec to know current schema + classification rules, (2) AUTO-CLASSIFY content (Hỏi:/Đáp: markers → khaitri, else → article), (3) POST /api/<collection>/validate dry-run, (4) POST/PATCH /api/<collection>. All validation centralized in apps/immortality-vn/schemas/ — no SKILL drift. Authoritative source of truth on every call."
license: Proprietary
exclude_deps:
  - npm:firebase
---

# Immortality API — Articles + Khai Trí

Manage content on `immortality.vn` via REST API. **Auto-classifies** between Article (essay) and Khai Trí (Q&A).

## QUY TẮC TỐI THƯỢNG

**KHÔNG ĐƯỢC HỎI USER LOẠI BÀI.** Auto-classify từ content shape:

| Signal | Classification |
|---|---|
| Body có `Hỏi:` / `Đáp:` (hoặc `Question:` / `Answer:`) markers | **khaitri** |
| Tựa là câu hỏi ("Vì sao...?", "Làm sao...?", "Cốt sống ... là gì?") | **khaitri** |
| Essay dài, tựa noun phrase ("Linh thai — vệ tinh tâm linh", "Phương pháp chữa mất ngủ") | **article** |
| Không chắc chắn | **article** (default) |

**Tie-breaker:** đếm số cặp Q/A. ≥2 cặp ⇒ khaitri. Còn lại ⇒ article.

Chỉ confirm với user khi confidence < 70%. Mặc định: tự quyết, post luôn, báo cáo.

## Authoritative spec — fetch fresh each task

```
GET https://battudao.com/api/agent-spec
```
Returns `{ collections: { khaitri, articles }, endpoints, examples, classification, auth }`. **READ THIS BEFORE EVERY TASK** — rules can change without skill update.

## Authentication

Bearer Firebase ID token. Email must be in `AGENT_ALLOWLIST_EMAILS` (default `agent@battudao.com`).

Credentials stored in goclaw vault — auto-loaded from `/app/data/.env.immortality`:
```
IMMORTALITY_AGENT_EMAIL=agent@battudao.com
IMMORTALITY_AGENT_PASSWORD=<set in vault UI>
IMMORTALITY_FIREBASE_API_KEY=AIzaSyAqORIPOvrGoBjFTelJcZQZtJutCS2p0rc
IMMORTALITY_FIREBASE_PROJECT_ID=immortalityvn
IMMORTALITY_FIREBASE_AUTH_DOMAIN=immortalityvn.firebaseapp.com
```

If `auth/invalid-credential`: password rotated. Tell user to update vault — DO NOT retry blindly.

## Commands (CLI script `khaitri.mjs`)

| Command | API call | Use when |
|---|---|---|
| `spec` | GET /agent-spec | Always first — learn current rules |
| `list` | GET /khaitri | See existing khaitri docs |
| `get <id>` | GET /khaitri/:id | Inspect single khaitri |
| `validate < data.json` | POST /khaitri/validate | Dry-run khaitri |
| `create < data.json` | POST /khaitri | Create khaitri. 409 on sourceRef collision |
| `update <id> < data.json` | PATCH /khaitri/:id | Partial khaitri update |
| `replace <id> < data.json` | PUT /khaitri/:id | Full khaitri update |
| `delete <id>` | DELETE /khaitri/:id | Hard delete khaitri |

**For articles:** use `articles.mjs` (mirror of khaitri.mjs):

| Command | API call |
|---|---|
| `node articles.mjs spec` | GET /agent-spec |
| `node articles.mjs list` | GET /articles |
| `node articles.mjs get <id>` | GET /articles/:id |
| `node articles.mjs create < data.json` | POST /articles (409 on dup sourceRef) |
| `node articles.mjs update <id> < patch.json` | PATCH /articles/:id |
| `node articles.mjs replace <id> < data.json` | PUT /articles/:id |
| `node articles.mjs delete <id>` | DELETE /articles/:id |
| `node articles.mjs upload-image < {url,intent,slug}` | POST /upload-from-url |

## Image upload (article hero or khaitri illustration)

Pre-step before POST: if content has external image URL (Telegram, etc.), upload to R2 first:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"url":"<source>","intent":"article","slug":"<slug>"}' \
  https://battudao.com/api/upload-from-url
# → returns { url: "https://pub-xxx.r2.dev/immortality-vn/articles/<slug>-<ts>.<ext>" }
```

Stamp returned URL into the doc's `image` field.

## Workflow — publish new content

1. **Receive content from user.**
2. **GET /api/agent-spec** → read schema + classification rules + tag_map.
3. **AUTO-CLASSIFY** (do NOT ask user).
4. **Translate** Vi → En faithfully if En missing. Both required (`vi.title`, `vi.body`, `en.title`, `en.body`).
5. **Build doc JSON** matching schema for chosen collection:
   - Article: `{ sourceRef, topic, date, image?, tag {vi,en}, vi {title, summary?, body}, en {title, summary?, body}, status: "draft" }`
   - Khai Trí: `{ sourceRef, order, date, tag {vi,en}, vi {title, question, summary?, body with Hỏi:/Đáp:}, en {...}, status: "draft" }`
6. **Validate** (POST /api/<collection>/validate). If errors[] → fix → retry validate.
7. **Upload image** if any (POST /api/upload-from-url).
8. **Create** (POST /api/<collection>). On 409 sourceRef_exists → PATCH instead.
9. **Self-verify**: WebFetch public page. Confirm title renders, body shows, no raw markdown garbage.
10. **Report** to user:
    > Đã đăng dạng **<Article|Khai Trí>**: "<title>". ID: <id>. Status: draft. Anh duyệt trong admin: https://battudao.com/admin

## Workflow — edit existing entry

1. GET /agent-spec.
2. List + find target by sourceRef/title.
3. GET /api/<collection>/:id to see current state.
4. Build PATCH (only fields to change).
5. Validate dry-run.
6. PATCH /api/<collection>/:id.
7. WebFetch verify.

## Workflow — attach image to existing article (after user approval)

This is step 2 of the multi-mod chain: **content mod posts draft first, illustrator generates image, user approves, then content mod PATCHes the image URL**.

Trigger: orchestrator (Gia Hân) sends task with `{ articleId, imageUrl, slug }` after user approved the illustration preview.

1. **Receive task** with article id + approved image URL (typically Telegram or other temporary URL).
2. **Upload to R2** (gives permanent URL):
   ```bash
   curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"url":"<approved telegram url>","intent":"article","slug":"<slug>"}' \
     https://battudao.com/api/upload-from-url
   # → { url: "https://pub-xxx.r2.dev/immortality-vn/articles/<slug>-<ts>.jpg" }
   ```
3. **PATCH article**:
   ```bash
   curl -X PATCH -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
     -d '{"image":"<r2 url from step 2>"}' \
     https://battudao.com/api/articles/<articleId>
   ```
4. **Verify**: WebFetch the public page → confirm hero image renders.
5. **Report** to orchestrator: `image attached to <id>, R2 url: <url>`. Article still `status: draft` — admin promotes manually.

**Do NOT** patch image into khaitri docs unless explicitly asked — khaitri is text-only by convention.

## Multi-mod orchestration (info for orchestrator agent)

Two-step publish flow when user wants illustrated article:

```
[user content] → orchestrator
    │
    ├─ 1. dispatch immortality-mod (this skill) → POST /api/articles { ..., status: "draft" }
    │     returns: { id, sourceRef, slug, adminUrl }
    │
    ├─ 2. dispatch illustrator-mod with { title, summary, slug } → returns: { telegramImageUrl }
    │
    ├─ 3. orchestrator forwards image preview to user → "duyệt không?"
    │
    ├─ 4. on user "ok" → dispatch immortality-mod (this skill, image-patch workflow)
    │     with { articleId: id, imageUrl: telegramImageUrl, slug }
    │     → uploads R2, PATCHes article.image
    │
    └─ 5. orchestrator notifies user: "Bài đã có hình, vào admin duyệt published: <adminUrl>"
```

This skill does NOT call sibling agents — orchestrator handles dispatching. This skill only owns the API write path.

## TUYỆT ĐỐI KHÔNG

1. **CẤM hỏi user "Article hay Khai Trí?"** — auto-classify. Vi phạm = bug.
2. **CẤM dùng Firestore client SDK** (legacy publisher path). Use HTTP API only.
3. **CẤM hardcode rules** from this SKILL. Always fetch `/api/agent-spec` for current truth.
4. **CẤM bịa nội dung dịch En**. Translate Vi→En faithfully — preserve tone, don't add facts.
5. **CẤM skip self-verify (WebFetch sau publish)**. Agent must close the loop.
6. **CẤM publish khi validate trả errors**. Fix first, retry validate, only then write.
7. **CẤM set `status: "published"`**. Always `draft`. Admin promotes manually in UI.
8. **CẤM sửa `sourceRef`**. That is the idempotency key.
9. **CẤM dùng order timestamp** cho khaitri (`20260503001`). Use `max(orders)+1` semantics.
10. **CẤM markdown headings/bullets** trong khaitri body. Renderer chỉ hiểu `Hỏi:`/`Đáp:`.

## Lỗi thường gặp

| Symptom | Cause | Fix |
|---|---|---|
| `auth/invalid-credential` | Pass rotated/wrong | Tell user to update vault entry |
| `auth/user-not-found` | Wrong email | Must be `agent@battudao.com` |
| 401 + `agent_not_authorized` | Email not in allowlist | Tell user to add to `AGENT_ALLOWLIST_EMAILS` |
| 409 `sourceRef_exists` | Doc with same sourceRef exists | PATCH instead of POST |
| 422 `validation_failed` | Schema mismatch | Read `errors[]`, fix payload |
| 403 `permission-denied` on Firestore | Role missing in `/admins/{uid}` | Tell user to grant `agent` role |
