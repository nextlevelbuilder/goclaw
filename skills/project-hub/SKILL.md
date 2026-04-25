---
name: project-hub
description: Single entry point quản lý toàn bộ vòng đời dự án trên AI Hub Knowledge. Dùng khi khởi tạo dự án, query thông tin, cập nhật task/risk/decision, ingest meeting/phỏng vấn, audit "claim X từ đâu ra". Mọi thao tác liên quan "dự án" → nghĩ skill này TRƯỚC. Hỗ trợ Agent-first mode với Goclaw integration.
version: "2.0.0"
---

# SKILL: project-hub

## Triggers
- **Tạo:** "tạo dự án X", "khởi tạo project X"
- **Query:** "dự án X status", "ai làm dự án X", "list dự án"
- **Cập nhật:** "thêm task/risk/decision", "sync dashboard"
- **Ingest:** "ghi meeting", "lưu phỏng vấn", user paste transcript
- **Audit:** "vì sao chốt X", "ai nói cái này"

## Không dùng cho
- One-off document không thuộc dự án → rule deploy knowledge
- Báo cáo KPI team không phải project tracking → skill khác

---

## 🔑 SOURCE OF TRUTH HIERARCHY (đọc TRƯỚC mọi thao tác)

```
Level 1 — RAW SOURCE (immutable evidence)
   ├─ External (Drive/Notion/Slack/Gmail/recording) → STUB only: metadata+link, KHÔNG copy
   ├─ Ephemeral (paste chat, không nơi khác lưu) → PRESERVE FULL, immutable, errata-only
   ├─ Goclaw Memory (episodic/semantic) → AUTO-EXTRACT, link session_id
   ├─ Goclaw Vault (knowledge graph) → AUTO-EXTRACT, link doc_id
   └─ Chat Channel (Telegram/Lark via Goclaw) → AUTO-EXTRACT, link message_id

Level 2 — DERIVED ITEMS (tasks/risks/decisions/facts)
   → BẮT BUỘC có field Source trỏ Level 1
   → External → 2-layer: stub (internal) + original (external)
   → Goclaw → 1-layer: session/doc/message ID (internal reference)

Level 3 — AGGREGATED VIEWS (overview/timeline/members)
   → Derive từ Level 2, auto-regen
```

### Bất di bất dịch
1. Không có Source → không vào Level 2
2. Level 1 KHÔNG BAO GIỜ modify — errata-only
3. **Reference over Duplicate** — external → LINK, KHÔNG copy
4. Level 3 derive từ Level 2 — không hardcode

### Phân loại raw source

| Loại | Ví dụ | Xử lý |
|------|-------|-------|
| External link | Google Doc, Meet recording, Notion, Jira, Slack permalink | **STUB ONLY** |
| File upload | PDF/docx/xlsx trong chat | Attach + stub metadata |
| Ephemeral | Paste text không có nơi khác | **PRESERVE FULL** |
| Hybrid | Paste + "file ở Drive X" | Ưu tiên reference external |
| Goclaw Memory | Episodic summary, semantic fact | **AUTO-EXTRACT** với session_id |
| Goclaw Vault | Knowledge doc, wikilink | **AUTO-EXTRACT** với doc_id |
| Chat Channel | Telegram/Lark message via Goclaw | **AUTO-EXTRACT** với message_id |

**Câu BẮT BUỘC hỏi khi không rõ (Human mode):**
> "Meeting này có lưu ở đâu ngoài chat không (Doc/Drive/Notion/recording)? Nếu có tao link về đó thay vì copy."

**Agent mode:** Không cần hỏi — auto-detect source type từ Goclaw context.

---

## 7 pages chuẩn hóa

| Type | Label | Emoji | Level |
|------|-------|-------|-------|
| overview | Tổng quan | 📊 | 3 |
| members | Thành viên | 👥 | 3 |
| tasks | Tasks | ✅ | 2+3 |
| timeline | Timeline | 📅 | 3 |
| risks | Rủi ro | ⚠️ | 2+3 |
| decisions | Quyết định | 📋 | 2+3 |
| documents | Tài liệu | 📁 | 3 |

**Spec chi tiết từng page:** xem `PAGE_SPECS.md` — BẮT BUỘC đọc trước khi generate.

---

## Convention

### Naming

| Item | Format |
|------|--------|
| Category | `DA: {Tên}` / slug `da-{slug}` |
| 7 pages | `DA: {Tên} - {Label}` / `da-{slug}-{type}` |
| Stub | `DA: {Tên} - Src: {type} {date}` |
| Preserved | `DA: {Tên} - Raw: {type} {date}` |
| Errata | `DA: {Tên} - Errata: {source}` |
| Doc derived | `DA: {Tên} - Doc: {Title}` |

### Tags BẮT BUỘC

- **7 pages chính:** `Dashboard` + `Project Tracking` + 1-2 domain
- **Stub:** `Raw Source - External Ref` + `Project Tracking` + domain
- **Preserved:** `Raw Source - Preserved` + `Project Tracking` + domain

### Metadata

**Project pages:**
```json
{"projects":["..."],"people":[...],"systems":[...],"periods":["MM/YYYY"]}
```

**Raw source pages (thêm):**
```json
{"source_type":"meeting|interview|email|chat|call","source_date":"DD/MM/YYYY HH:MM","attendees":["..."],"channel":"...","language":"vi|en|mixed","external_link":"https://...","kind":"stub|preserved|errata","ingested_at":"..."}
```

### Markdown rules

- **Nav bar đầu 7 page:** `**Dashboard:** [📊 Tổng quan](da-{slug}-overview) · [👥 Thành viên](da-{slug}-members) · [✅ Tasks](da-{slug}-tasks) · [📅 Timeline](da-{slug}-timeline) · [⚠️ Rủi ro](da-{slug}-risks) · [📋 Quyết định](da-{slug}-decisions) · [📁 Tài liệu](da-{slug}-documents)` + `---`
- Internal link: dùng slug, KHÔNG full URL
- Charts: Mermaid. > 30 tasks / > 8 phases → bảng
- Badges: 🟢🟡🔴⚪
- Alerts: `> [!WARNING]` / `> [!NOTE]` / `> [!IMPORTANT]`
- KHÔNG HTML tag

### Resources

| Resource | ID |
|----------|-----|
| Parent "Dự án" | `019ce184-a83c-78f9-96b7-2e4874aff92d` |
| Dashboard tag | `019be395-65f6-70e8-abed-4d5d14a3456c` |
| Operations | `019be395-65f6-750c-a257-7e927f13fbf1` |
| Finance | `019be395-65f9-79ff-bda9-bf55d236616a` |
| Tech | `019be395-65f6-75f3-ad42-e41e3fbf1b63` |
| Project Tracking | TBD tạo lần đầu |
| Raw Source - External Ref | TBD tạo lần đầu |
| Raw Source - Preserved | TBD tạo lần đầu |

---

## Workflow A: KHỞI TẠO

```
1. CLARIFY: tên, mục tiêu, owner, domain, stakeholders, timeline
2. PRE-CHECK: search_knowledges("DA: {tên}"), verify tags
3. CREATE CATEGORY dưới 019ce184-...
4. INIT MEMORY: overview/tasks/risks/decisions/people.md
5. GENERATE 7 pages theo PAGE_SPECS.md (rỗng → "Chưa có dữ liệu")
6. DEPLOY 7 create_knowledge
7. SAVE STATE
8. REPORT 7 URLs
```

### ✅ Verify
- [ ] Không trùng? Slug kebab-case unique?
- [ ] Category đúng parent?
- [ ] 7 pages có nav + 3 tag + metadata 4 field?
- [ ] Overview đủ charter sections (PAGE_SPECS)?
- [ ] Members có RACI?
- [ ] Visibility = internal?
- [ ] State file đủ IDs + Meta?

---

## Workflow B: READ / QUERY / AUDIT

```
CASE 1 — 1 dự án: search_knowledges → get_knowledge theo intent
CASE 2 — List: list_categories(parent) → get overview mỗi cate
CASE 3 — Tìm theo người/hệ thống: search filter metadata
CASE 4 — AUDIT ("vì sao X"):
  1. Tìm item trong decisions/tasks/risks
  2. Đọc Source → stub link
  3. get_knowledge stub → external_link nếu có
  4. Trả: item + raw quote + speaker + context + link gốc
```

### ✅ Verify
- [ ] READ AI Hub TRƯỚC?
- [ ] Đúng page theo intent?
- [ ] Có link nguồn?
- [ ] Audit trace về Level 1?

---

## Workflow C: CẬP NHẬT

```
1. IDENTIFY project
2. READ state (rebuild từ AI Hub nếu không có)
3. UPDATE memory file + header date
4. AFFECTED pages:
   tasks.md → tasks + overview + timeline + members
   risks.md → risks + overview
   decisions.md → decisions + overview
   people.md → members + overview
5. REGENERATE affected pages
6. update_knowledge(id, {markdownContent, changeSummary})
7. UPDATE state
8. REPORT
```

### ✅ Verify
- [ ] Update memory TRƯỚC regenerate?
- [ ] Affected pages đúng?
- [ ] Dùng update_knowledge?
- [ ] State dates+hash mới?

---

## Workflow D: SYNC

```
1. READ state
2. So sánh date header VÀ hash vs state
3. Không đổi → "SYNC_OK" → STOP
4. Có đổi → Workflow C step 4-7
```

---

## Workflow E: ADD DOCUMENT DERIVED

```
1. Convert → markdown
2. create_knowledge doc page
3. Update Documents page (thêm row)
4. Update state
```

---

## Workflow F: INGEST RAW SOURCE (Human mode — nhạy cảm nhất)

```
1. IDENTIFY project
2. DETERMINE TYPE:
   ├─ External link? → STUB (3A)
   ├─ File upload? → ATTACH (3C)
   ├─ Pure paste? → HỎI "có lưu nơi khác?"
   │   ├─ Có → STUB, paste chỉ để extract
   │   └─ Không → PRESERVE FULL (3B)
   └─ Chưa rõ → HỎI trước

3A. STUB (external):
    - metadata + link, KHÔNG copy content
    - Tag: Raw Source - External Ref
    - Callout: > [!IMPORTANT] EXTERNAL RAW SOURCE — REFERENCE ONLY
    - Summary optional, mark "by agent"

3B. PRESERVE FULL:
    - Giữ NGUYÊN VĂN, KHÔNG edit/beautify
    - Giữ filler/typo/[inaudible]/speaker markers
    - Tag: Raw Source - Preserved
    - Callout: > [!IMPORTANT] RAW SOURCE — DO NOT EDIT

3C. ATTACH: file + stub metadata như 3A

4. EXTRACT items:
   TASK/RISK/DECISION/QUESTION/FACT
   Classify DRAFT vs CONFIRMED

5. ATTRIBUTE:
   - speaker (hoặc unknown)
   - source 2-layer (stub + original nếu external)
   - raw_quote MAX 15 từ nguyên văn
   - confidence (high/medium/low)
   - extracted_at

6. PROPOSE bảng preview — KHÔNG auto-apply, user duyệt

7. APPLY items approved → Workflow C sync

8. UPDATE state: log ingest + link source → items

9. REPORT: source URL + items added + pending
```

### Errata (raw source sai)
```
1. Tạo page errata mới, link source cũ
2. Source cũ: thêm callout > [!WARNING] Có errata: [link]
3. Derived items: đổi source → errata, giữ history
```

### ✅ Verify Workflow F
- [ ] Đã hỏi "có link gốc không"?
- [ ] Type đúng (external/preserve/attach)?
- [ ] External → STUB, KHÔNG copy content?
- [ ] Preserve → nguyên văn, KHÔNG beautify?
- [ ] Metadata đủ (date/attendees/channel/language/kind)?
- [ ] Có immutability callout?
- [ ] Tag đúng?
- [ ] Derived items có Source 2-layer?
- [ ] Raw quote ≤ 15 từ?
- [ ] Items medium/low đã user confirm?
- [ ] KHÔNG duplicate content ở cả stub và full?
- [ ] Summary agent đã mark rõ?
- [ ] State log ingest event?

---

## Workflow F2: AGENT AUTO-INGEST (Goclaw Integration)

> Dùng khi Goclaw Agent tự động ingest từ memory/vault/channel — KHÔNG cần human confirm cho high-confidence items.

```
1. IDENTIFY project (từ Goclaw context hoặc channel mapping)

2. READ SOURCE từ Goclaw:
   ├─ Episodic Memory → session summaries, interview transcripts
   ├─ Semantic Memory → extracted facts, entities
   ├─ Vault Docs → knowledge documents với wikilinks
   └─ Channel Messages → Telegram/Lark messages từ project group

3. DETECT CHANGES:
   - Compare hash với state file
   - Nếu không đổi → SYNC_OK → STOP
   - Có đổi → tiếp tục

4. EXTRACT ITEMS:
   TASK/RISK/DECISION/QUESTION/FACT
   - Mỗi item có Source = goclaw-{type}:{id}
   - Classify confidence: high/medium/low
   - high = direct statement từ member
   - medium = inferred từ context
   - low = ambiguous, cần clarify

5. AUTO-APPLY (confidence=high):
   - Apply trực tiếp vào memory files
   - Sync lên AI Hub MCP
   - Log action cho audit

6. QUEUE REVIEW (confidence=medium/low):
   - Gửi notification qua project channel
   - Format: "[Review] {item_count} items cần PM confirm"
   - Include preview table

7. UPDATE STATE:
   - Log ingest event
   - Update hash
   - Link source → items

8. REPORT (qua channel):
   - "Synced: {applied_count} items"
   - "Pending review: {pending_count} items"
```

### Source format cho Goclaw items

```markdown
- **Source:** goclaw-episodic:{session_id} DD/MM/YYYY
- **Raw quote:** "{nguyên văn từ transcript}" (max 15 từ)
- **Speaker:** {member_name}
- **Confidence:** high|medium|low
- **Extracted at:** DD/MM/YYYY HH:MM
- **Agent:** {agent_key}
```

### ✅ Verify Workflow F2
- [ ] Có Goclaw context?
- [ ] Source type đúng (episodic/semantic/vault/channel)?
- [ ] High-confidence auto-applied?
- [ ] Medium/low queued for review?
- [ ] Notification gửi về channel?
- [ ] State updated với hash mới?
- [ ] Audit log đầy đủ?

---

## Workflow G: AGENT ALERT

> Goclaw Agent tự động generate và post alerts về project channel.

```
TRIGGERS:
├─ Task overdue: deadline < today && status != done
├─ Risk escalation: score tăng lên Critical/High (🔴🟠)
├─ Decision pending: > 3 ngày chưa confirm
├─ Blocker: > 24h chưa resolve
├─ Timeline slip: milestone miss projected date
└─ Inactivity: > 7 ngày không có update

ACTIONS:
1. DETECT trigger condition (run định kỳ hoặc on-event)

2. GENERATE alert message:
   Format: "[Alert] {type}: {title}"
   Include: owner, deadline/date, impact, action required

3. POST to project channel:
   - Via Goclaw Channels API (Telegram/Lark)
   - Tag relevant owner nếu có @mention mapping
   - Include link đến AI Hub page

4. LOG alert:
   - Append vào AI Hub alerts page (agent-activity)
   - Include: timestamp, trigger, recipients, status

5. TRACK response:
   - Nếu owner respond → mark alert acknowledged
   - Nếu không respond sau 24h → escalate (re-alert + tag PM)
```

### Alert templates

**Task overdue:**
```
[Alert] Task Overdue: {task_title}
Owner: {owner}
Deadline: {deadline} ({days_overdue} ngày trễ)
Action: Cập nhật status hoặc request extension
Link: {ai_hub_tasks_page}
```

**Risk escalation:**
```
[Alert] Risk Escalated: {risk_title}
Score: {old_score} → {new_score}
Owner: {owner}
Mitigation: {mitigation_action}
Link: {ai_hub_risks_page}
```

**Decision pending:**
```
[Alert] Decision Pending: {decision_title}
Waiting since: {date} ({days} ngày)
Options: {options_summary}
Decider: {decider}
Link: {ai_hub_decisions_page}
```

### ✅ Verify Workflow G
- [ ] Trigger condition đúng?
- [ ] Alert format clear và actionable?
- [ ] Posted qua đúng channel?
- [ ] Owner tagged?
- [ ] Logged trong AI Hub?
- [ ] Escalation rule set?

---

## State file

`memory/{slug}/dashboard-hub-state.md`

```markdown
# Project Hub State — {Tên}
Cập nhật: DD/MM/YYYY

## Meta
- Slug, Owner, Status 🟢/🟡/🔴/⚪, Domain

## Category
- ID: {uuid}

## Pages
| Type | Knowledge ID | Slug |

## Sources (change detection)
| File | Last Sync | Hash |

## Raw Sources
| Title | Type | Date | Kind | Knowledge ID | External Link |

## Ingest Log
| Date | Source | Items | Applied |

## Documents (derived)
| Title | Knowledge ID | Added |
```

---

## DO
- ✅ Thao tác dự án → skill này TRƯỚC
- ✅ Đọc Source of Truth Hierarchy TRƯỚC khi làm raw source
- ✅ READ AI Hub TRƯỚC, memory fallback
- ✅ Reference over Duplicate — external → stub
- ✅ LUÔN hỏi "có link gốc không" trước preserve
- ✅ Derived item phải có Source
- ✅ Raw quote max 15 từ nguyên văn
- ✅ Stub có metadata đầy đủ
- ✅ 7 pages + nav + 3 tag + metadata 4 field
- ✅ Save state sau mọi write

## DON'T
- ❌ HTML tag — chỉ markdown
- ❌ Full URL — dùng slug
- ❌ Mermaid gantt > 30 tasks / > 8 phases
- ❌ Gộp pages
- ❌ Sửa raw source (errata-only)
- ❌ Copy content external vào AI Hub (stub only)
- ❌ Extract item không có Source
- ❌ Paraphrase raw "cho gọn"
- ❌ Auto-apply extracted — user confirm
- ❌ Xóa raw source (archive ok)
- ❌ Lưu cả link VÀ full content (double)
- ❌ Sửa knowledge trực tiếp trên UI
- ❌ Tạo dự án trùng
- ❌ Quên save state

## Error handling
- Deploy fail → save partial, resume qua pre-check
- Mermaid sai → fallback bảng
- AI Hub unreachable → fallback memory + warn "stale"
- External link chết → mark derived confidence=low, warn
- Paste > 50K chars → warn, chia nhỏ hoặc yêu cầu external
- Goclaw connection lost → queue changes, retry on reconnect
- Channel post fail → retry 3x, fallback log to AI Hub only

---

## 🤖 AGENT MODE (Goclaw Integration)

> Khi skill được invoke bởi Goclaw Agent (không phải human interaction), các behavior sau được áp dụng:

### Detection
Agent mode được detect khi:
- Context có `goclaw_agent_key`
- Hoặc source là `goclaw-*` type
- Hoặc channel là Telegram/Lark via Goclaw Channels

### Behavior changes

| Aspect | Human Mode | Agent Mode |
|--------|------------|------------|
| Source question | Hỏi "có link gốc không?" | Auto-detect từ context |
| Confirm items | User duyệt tất cả | Auto-apply high-confidence |
| Alert | Manual trigger | Auto-generate on trigger |
| Sync | On-demand | Scheduled (mỗi 1h hoặc on-event) |
| Report | Detailed to human | Summary to channel |
| Logging | Minimal | Full audit trail |

### Agent-specific DO
- ✅ Auto-apply items confidence=high
- ✅ Queue medium/low cho PM review qua chat
- ✅ Generate alerts proactively
- ✅ Log all actions cho human audit
- ✅ Post summaries to project channel
- ✅ Track response/acknowledgement

### Agent-specific DON'T
- ❌ Auto-apply confidence=medium/low
- ❌ Modify without logging
- ❌ Skip Source attribution
- ❌ Spam channel (batch alerts, max 1/hour per type)
- ❌ Escalate without waiting period (24h default)

### Channel mapping

```
Project → Channel
DA: {slug} → config lookup in state file

State file thêm:
## Channel Config
- Platform: telegram|lark
- Chat ID: {chat_id}
- Owner mention: @{username}
- Alert enabled: true|false
- Alert batch interval: 1h
```

### Audit requirements
Mọi action trong Agent mode PHẢI log:
```json
{
  "timestamp": "...",
  "agent_key": "...",
  "action": "apply|queue|alert|sync",
  "project": "...",
  "items": [...],
  "source": "goclaw-{type}:{id}",
  "result": "success|fail|partial",
  "details": "..."
}
```

---

## 🚨 RED FLAGS

| Dấu hiệu | Sai gì |
|----------|--------|
| `<div>/<span>/<table>` | HTML thay markdown |
| `https://ai-hub...` trong link | Phải slug |
| < 7 pages | Thiếu chuẩn |
| Không nav bar | Quên nav |
| Tag chỉ Dashboard | Thiếu Project Tracking |
| Metadata `{}` | Không tổ chức |
| Mermaid gantt + > 30 tasks | Vỡ render |
| Copy Google Doc vào page | Vi phạm Reference over Duplicate |
| Raw source không metadata | Không audit |
| Derived item không Source | Vi phạm Level 2 |
| Raw quote > 15 từ | Quá dài |
| Sửa raw source sau lưu | Phải errata |
| Auto-apply không confirm | Sai Workflow F |
| Stub có full transcript | Sai kind |
| Visibility=public nội bộ | Leak |
| Audit không trace Level 1 | Mất evidence |
| Agent auto-apply medium/low | Sai confidence rule |
| Agent không log action | Vi phạm audit |
| Alert spam (> 1/hour/type) | Quá tải channel |
| Goclaw source không session_id | Không trace được |
