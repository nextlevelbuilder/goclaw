---
name: immortality-editor
description: "Use this skill to EDIT existing Khai Trí Q&A entries already published on immortality.vn / battudao.com. Triggers: user says 'sửa bài Khai Trí', 'edit khai tri', 'thêm bản tiếng anh cho bài', 'translate khai tri', 'update khai tri <name>', 'sửa order bài <name>', 'fix bài <name>'. Two-step workflow: (1) fetch.mjs exports a Firestore document by sourceRef back into a markdown file in the inbox; (2) agent edits the markdown (e.g. adds missing English translation, fixes order); (3) standard immortality-publisher publish.mjs detects the matching sourceRef and UPDATES the existing Firestore document via dot-notation (preserving any concurrent admin edits to fields not touched). Idempotent and safe — never inserts a duplicate document. Do NOT use for: creating brand new Khai Trí entries (use immortality-publisher), articles/topics on immortality-vn, or non-Firebase targets."
license: Proprietary
exclude_deps:
  - npm:firebase
  - npm:firebase-admin
---

# Immortality Editor — Khai Trí

Edit existing Khai Trí entries on `immortality.vn` Firestore. Pairs with `immortality-publisher`.

## When to use

| Trigger | Action |
|---|---|
| "sửa bài Khai Trí <name>" / "edit khai tri <name>" | Run `fetch.mjs --sourceRef <ref>` → edit md → `publish.mjs` |
| "thêm bản tiếng anh cho bài <name>" | Fetch, translate Vi→En, save back, publish |
| "sửa order bài <name>" | Fetch, set new order in frontmatter, save back, publish |
| "fix lỗi bài <name>" | Fetch, edit, publish |

## Architecture

`immortality-editor` does NOT have its own write path. It exports a doc back into the inbox in the same markdown shape that `immortality-publisher` expects. The publisher is upsert-based (`buildUpdate()` uses dot-notation), so re-running publish on an edited file UPDATES the existing doc.

This means:
- One source of truth for write logic (`immortality-publisher/scripts/publish.mjs`).
- Same credentials, same validation, same `_done`/`_failed` move semantics.
- Editor only adds the `fetch` capability.

## Workflow (5 steps — NEVER skip)

### Step 1 — Fetch the doc
```
exec(command="cd /app/data/skills-store/immortality-editor/1 && \
  node scripts/fetch.mjs --sourceRef <ref> \
    --service-account=/path/to/sa.json \
    --output /app/data/skills-store/inbox/khaitri/<filename>.md")
```
Or via CLI args:
```
node scripts/fetch.mjs --sourceRef <ref> \
  --api-key=... --project-id=... --auth-domain=... \
  --email=... --password=... \
  --output <path>
```
Output: a markdown file with full frontmatter (`sourceRef`, `order`, `date`, `tagVi`, `tagEn`, `titleVi`, `titleEn`, `questionVi`, `questionEn`, `summaryVi`, `summaryEn`) + body containing both Vi `Hỏi:/Đáp:` and En `Question:/Answer:` blocks.

### Step 2 — Edit the markdown
- Add missing fields (e.g. `titleEn` if doc only has Vi).
- Translate Vi → En faithfully if user asked for translation. Do NOT fabricate content beyond what the Vi version says.
- Adjust `order` if user asked. Use `max(orders)+1` semantics — never timestamp-based numbers.
- Use `write_file` to save changes back to the same path in inbox.

### Step 3 — Dry-run validate
```
exec(command="cd /app/data/skills-store/immortality-publisher/1 && \
  node scripts/publish.mjs --dry-run --verbose")
```
Read stdout. Fix any validation errors before proceeding.

### Step 4 — Apply update (publish)
```
exec(command="cd /app/data/skills-store/immortality-publisher/1 && \
  node scripts/publish.mjs --email=... --password=... \
    --api-key=... --project-id=... --auth-domain=...")
```
The publisher detects the matching `sourceRef`, builds a dot-notation update payload, and merges into the existing doc — preserving any admin manual edits to fields not present in the new file.

### Step 5 — Report
- Confirm `1 updated` in publish summary.
- File moves to `_done/`.
- Tell user: "Đã update bài <title>. Anh duyệt lại trong admin nếu cần."

## Required frontmatter for a valid edit

| Field | Required | Notes |
|---|---|---|
| `sourceRef` | YES | Must match an existing Firestore doc. Without this, publisher inserts a duplicate. |
| `order` | YES | Integer, ideally ≤ 1000 |
| `date` | YES | `'YYYY-MM-DD'` (quoted as string in YAML) |
| `tagVi` | YES | Vietnamese tag |
| `titleVi` | YES | Vietnamese title |
| `questionVi` | YES | Vietnamese question |
| `tagEn` | optional | Recommended for fully bilingual UX |
| `titleEn`/`questionEn`/`summaryEn` | optional | Same |

Body must contain at least one Hỏi:/Đáp: pair (Vi) OR Question:/Answer: pair (En).

## TUYỆT ĐỐI KHÔNG

1. **CẤM dùng skill này để TẠO bài mới.** New entries → `immortality-publisher` only. Editor only edits.
2. **CẤM bịa nội dung.** Translation must reflect Vi source faithfully. Do not add facts the Vi text does not contain.
3. **CẤM sửa `sourceRef`.** That is the idempotency key. Changing it creates a duplicate doc instead of updating.
4. **CẤM publish without dry-run.** Always validate before applying credentials-using publish.
5. **CẤM dùng order timestamp** (`20260503001`). Use `max(orders)+1` semantics.
