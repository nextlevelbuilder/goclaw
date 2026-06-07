---
name: immortality-publisher
description: "Use this skill to publish Khai Trí Q&A entries to immortality.vn (battudao.com) Firestore. Triggers: user mentions 'post bài Khai Trí', 'đăng Khai Trí', 'publish khai tri', 'immortality publisher', 'battudao publisher', or asks to push markdown files from Claw/goclaw/inbox/khaitri/ to the immortality-vn site. Reads markdown files with YAML frontmatter from the inbox, signs in to Firebase Auth as a designated agent account, writes one Firestore document per file to the 'khaitri' collection (status: draft for human review), and moves processed files to _done/. Idempotent via sourceRef field. Do NOT use for: fly0 stories (use fly0-publisher), articles/topics on immortality-vn (separate publishers, not yet built), or non-Firebase targets."
license: Proprietary
exclude_deps:
  - npm:firebase
---

# Immortality Publisher — Khai Trí

Publish Q&A entries to `immortality.vn` Firestore from markdown inbox files.

## When to use

| Trigger | Action |
|---|---|
| User says "post Khai Trí" / "đăng Khai Trí" | Run `scripts/publish.mjs` |
| User drops `.md` files in `Claw/goclaw/inbox/khaitri/` and asks to publish | Run `scripts/publish.mjs` |
| User asks "agent post được chưa?" | Check inbox + run if files present |

## Quick reference

| Task | Command |
|------|---------|
| Install deps (first run only) | `cd skills/immortality-publisher && npm install` |
| Dry run (validate only, no creds needed) | `node scripts/publish.mjs --dry-run [--verbose]` |
| Publish — CLI args (goclaw runtime) | `node scripts/publish.mjs --email=X --password=Y --api-key=Z --project-id=W --auth-domain=V` |
| Publish — stdin JSON (preferred for runtime) | `echo '{"email":"X","password":"Y","apiKey":"Z","projectId":"W","authDomain":"V"}' \| node scripts/publish.mjs --stdin-creds` |
| Publish — env vars (manual debug only) | `node scripts/publish.mjs` (with .env populated) |

## Spec reference

Authoritative spec lives in target app repo:
`apps/immortality-vn/plans/260503-1302-immortality-publisher-spec/`
- `markdown-schema.md` — YAML frontmatter + body format
- `firestore-schema.md` — `khaitri` collection doc shape
- `agent-auth-and-write-flow.md` — auth + idempotency + error handling
- `content-guidelines-khaitri.md` — content quality rules

If spec disagrees with this SKILL.md, **trust the spec** — update SKILL.md to match.

## Configuration — credentials passing

Skill accepts credentials in priority order:

1. **CLI args** (highest priority — for goclaw runtime invocation):
   ```
   --email=agent@battudao.com
   --password=<from agent credential store>
   --api-key=<same as VITE_FIREBASE_API_KEY>
   --project-id=immortalityvn
   --auth-domain=immortalityvn.firebaseapp.com
   ```

2. **Stdin JSON** (preferred for goclaw — avoids password in process args/argv listing):
   ```bash
   echo '{
     "email": "agent@battudao.com",
     "password": "...",
     "apiKey": "...",
     "projectId": "immortalityvn",
     "authDomain": "immortalityvn.firebaseapp.com"
   }' | node scripts/publish.mjs --stdin-creds
   ```

3. **Env vars** (fallback — manual debug only):
   `IMMORTALITY_AGENT_EMAIL`, `IMMORTALITY_AGENT_PASSWORD`, `IMMORTALITY_FIREBASE_API_KEY`, `IMMORTALITY_FIREBASE_PROJECT_ID`, `IMMORTALITY_FIREBASE_AUTH_DOMAIN`. See `.env.example`.

**Goclaw runtime should use #2 (stdin JSON)** — keeps password out of `ps` / process listing. Agent credential store fetches per-app credentials and pipes as JSON.

**Never commit `.env`.** Goclaw `.gitignore` already covers `.env*`.

## Inbox layout

```
Claw/goclaw/inbox/khaitri/
├── khaitri-2026-05-03-001.md      # drop new entries here
├── khaitri-2026-05-03-002.md
├── _done/                          # successfully published (auto-moved)
├── _failed/                        # validation/write errors (auto-moved + .error.txt)
└── _logs/                          # per-run log file (run-YYYYMMDD-HHMMSS.log)
```

## File format

YAML frontmatter + Markdown body.

Required frontmatter fields: `sourceRef`, `order`, `date`, `tagVi`, `titleVi`, `questionVi`.

### Body format — STRICT

Body must contain ONLY `Hỏi:/Đáp:` (Vi) and/or `Question:/Answer:` (En) blocks separated by blank lines. Nothing else.

The website renderer (`renderText` in `apps/immortality-vn/src/components/shared/ReadingHelpers.jsx`) ONLY recognizes:
- Lines starting with `Hỏi:` / `Đáp:` / `Question:` / `Answer:` → styled Q&A cards
- Other paragraph blocks → plain `<p>`

It does NOT parse markdown. So if you put `# heading` or `## heading`, the page renders the literal `#` characters — looks like garbage.

**Allowed in body:**
```
Hỏi:
Câu hỏi của người hỏi...

Đáp:
Câu trả lời...

Hỏi:
Câu hỏi tiếp theo...

Đáp:
Trả lời tiếp theo...
```

**For bilingual files** — append the English Q&A block after Vi (no separator marker needed; publisher splits by detecting `Question:`/`Answer:` patterns):
```
Hỏi:
...
Đáp:
...

Question:
...
Answer:
...
```

**FORBIDDEN in body:**
- ❌ Markdown headings (`# Title`, `## Section`, `### Subsection`) — render as literal `#`
- ❌ "Bối cảnh" / "Context" framing sections — content not in Q&A pairs is wasted
- ❌ "Điểm Nhấn" / "Key Points" / "Bài Học" / "Lessons" sub-sections — pack key points INTO the `Đáp:` answer or split into separate Q&A pairs
- ❌ Bullet lists (`- item`) outside Q&A blocks
- ❌ Bold/italic markdown (`**bold**`, `*italic*`) — renders as literal asterisks

If source material has framing/lesson sections, EXTRACT the Q&A pairs only and put insights into the `Đáp:` portion. Don't ship the whole essay.

See `markdown-schema.md` in spec for full schema + examples.

## Behavior contract

1. **Auth:** sign in Firebase Auth with email/password. Abort batch on auth failure.
2. **Validate:** check schema before any write. Invalid → move to `_failed/` + write `.error.txt`.
3. **Idempotency:** query `khaitri` `where('sourceRef', '==', X)` before insert. If exists, **update** existing doc (merge fields) and move file to `_done/`. If not exists, `addDoc` and move to `_done/`.
4. **Status:** always set `status: 'draft'`. **Never set `published`** — admin promotes manually in UI.
5. **Source tag:** set `source: 'goclaw-publisher-v1'` for provenance.
6. **Atomic ops:** use Firestore client SDK `addDoc` / dot-notation `updateDoc` — avoid full-doc clobber races with admin's inline edits.
7. **Logging:** redact password in logs. Per-run log file in `_logs/`. Console output summary at end.

## What NOT to do

- ❌ Do NOT use Firebase admin SDK or service account keys
- ❌ Do NOT call any HTTP endpoint on immortality-vn (no ingest endpoint exists; use Firestore client SDK)
- ❌ Do NOT set `status: 'published'`
- ❌ Do NOT process files already in `_done/`
- ❌ Do NOT modify `firestore.rules` or any code in `apps/immortality-vn/`
- ❌ Do NOT use markdown headings or formatting in body (see "Body format — STRICT" above)
- ❌ Do NOT use timestamp-style order (`20260503001`) — order MUST be `max(existing orders) + 1`

## Translation policy (updated 2026-05-03)

Site is bilingual Vi+En. If frontmatter is missing `titleEn` / `questionEn` / `summaryEn` / `tagEn` OR body lacks the `Question:/Answer:` block:

- ✅ Agent SHOULD translate Vi → En faithfully (preserve tone, do not add facts) and write the file back via `write_file` BEFORE running publish.
- ✅ Append the English Q&A block after the Vietnamese block in body (publisher's `splitBilingualBody` will split by detecting `Question:`/`Answer:` headings).
- ✅ Tag mapping: "Khai Trí"→"Enlightenment", "Tâm Linh"→"Spiritual", "Mất Ngủ"→"Insomnia", "Năng Lượng"→"Energy", "Sức Khỏe"→"Health".
- ❌ Do NOT publish Vi-only entries — that breaks the En reading view (page goes blank when user clicks the EN flag).

## Smoke test (after first deploy)

1. Drop `khaitri-test-001.md` (unique sourceRef) into `inbox/khaitri/`
2. Run `node scripts/publish.mjs`
3. Verify:
   - File moved to `_done/`
   - Firebase Console → Firestore → `khaitri` → new doc with `status: 'draft'`, `source: 'goclaw-publisher-v1'`
   - immortality-vn admin (`/admin`) shows entry in KhaiTriTab
   - Public Khai Trí page does NOT show the draft (filter applied via `App.jsx:195`)
4. Re-run on same file (now in `_done/`) → expect "0 processed"
5. Move file back to `inbox/khaitri/` → re-run → expect "1 skipped (sourceRef exists)" → file → `_done/`

## Failure modes & recovery

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `auth/wrong-password` | Wrong env var or rotated password | Update `IMMORTALITY_AGENT_PASSWORD` |
| `auth/user-not-found` | Account not created in Firebase Console | Dang creates `agent@battudao.com` |
| `permission-denied` on Firestore write | Rules tightened to require custom claim | Set claim `admin=true` on agent account, or revert rules |
| File stuck in inbox after run | Transient network/Firestore error | Re-run; persistent → check `_logs/` |
| File in `_failed/` with `.error.txt` | Validation failure (schema/sourceRef dup) | Read `.error.txt`, fix file, move back to inbox |
