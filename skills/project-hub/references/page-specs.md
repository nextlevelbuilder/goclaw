# Page Specs — project-hub (PMI-aligned + Source-traceable)

> Đọc file này TRƯỚC khi generate bất kỳ page nào. Dựa trên PMI/PMBOK + Miro/Wrike charter guides + nguyên tắc Source of Truth Hierarchy.

## Nguyên tắc chung

1. **Concise > exhaustive** — overview 3-5 màn hình, không nhồi nhét
2. **SMART** — mọi objective/task/risk phải Specific, Measurable, Achievable, Relevant, Time-bound
3. **Charter (ổn định) vs Plan (động)** — overview/members ít đổi; tasks/risks/decisions đổi liên tục
4. **Không trộn team charter vào project charter** — vai trò cá nhân ở `members`
5. **Anti scope-creep** — overview BẮT BUỘC có "Out of scope"
6. **Có sao viết vậy** — chưa biết → ghi "TBD + owner + deadline quyết định"
7. **Source-traceable** — mọi derived item (task/risk/decision) BẮT BUỘC có field Source trỏ Level 1

---

## Format Source field (BẮT BUỘC cho mọi derived item)

### Từ external raw source (có link gốc)
```markdown
- **Source:**
  - Stub: [Meeting 04/04/2026](da-{slug}-src-meeting-04-04-2026)
  - Original: [Google Doc](https://docs.google.com/...) §3
- **Raw quote:** "MISA khớp kế toán, NhanhVN chỉ operational" (max 15 từ)
- **Speaker:** Hùng (PM)
- **Confidence:** high
- **Extracted at:** 04/04/2026 16:30
```

### Từ preserved raw source (ephemeral)
```markdown
- **Source:** [Interview Lan 05/04/2026](da-{slug}-raw-interview-lan-05-04-2026) ¶5
- **Raw quote:** "data NhanhVN lệch do concurrent writes" (max 15 từ)
- **Speaker:** Lan (Analyst)
- **Confidence:** high
- **Extracted at:** 05/04/2026 10:00
```

### Multi-source (item confirm qua nhiều lần)
```markdown
- **Sources:**
  1. [Meeting 04/04/2026](slug) — Hùng raise
  2. [Meeting 11/04/2026](slug) — Duy confirm final
- **Confidence:** high (confirmed)
```

### Internal-origin (tự agent/user thêm không từ meeting)
```markdown
- **Source:** internal (added by {user/agent} on DD/MM/YYYY)
- **Confidence:** high
```

### Goclaw-origin (auto-extracted từ Goclaw memory/channel)

**Từ Episodic Memory (interview/session summary):**
```markdown
- **Source:** goclaw-episodic:{session_id}
- **Date:** DD/MM/YYYY HH:MM
- **Agent:** {agent_key}
- **Raw quote:** "{nguyên văn từ transcript}" (max 15 từ)
- **Speaker:** {member_name}
- **Confidence:** high|medium|low
- **Extracted at:** DD/MM/YYYY HH:MM
```

**Từ Semantic Memory (extracted fact):**
```markdown
- **Source:** goclaw-semantic:{fact_id}
- **Subject:** {entity}
- **Confidence:** {0.0-1.0}
- **Supporting sources:** [episodic_id_1, episodic_id_2]
```

**Từ Knowledge Vault (document):**
```markdown
- **Source:** goclaw-vault:{doc_id}
- **Title:** {doc_title}
- **Path:** {doc_path}
- **Section:** {heading or ¶ number}
```

**Từ Chat Channel (Telegram/Lark message):**
```markdown
- **Source:** goclaw-channel:{platform}:{chat_id}:{message_id}
- **From:** {username}
- **Date:** DD/MM/YYYY HH:MM
- **Raw quote:** "{message text}" (max 15 từ)
- **Confidence:** high (direct statement)
```

**Từ Goclaw Task (synced task):**
```markdown
- **Source:** goclaw-task:{task_id}
- **Created:** DD/MM/YYYY
- **Last updated:** DD/MM/YYYY HH:MM
```

---

## 1. `overview` — Project Charter

**Mục đích:** High-level reference. "Dự án này là gì, vì sao, ai chịu trách nhiệm, đo bằng gì".

### Sections BẮT BUỘC

| # | Section | Nội dung | Anti-pattern |
|---|---------|----------|--------------|
| 1 | **Tóm tắt 1 dòng** | Mục đích 1-2 câu | Dài hơn 2 câu |
| 2 | **Business case** | Pain hiện tại + giá trị | "Vì sếp bảo" |
| 3 | **Mục tiêu SMART** | 3-5 objectives đo được | "Cải thiện hệ thống" |
| 4 | **Scope** | **In scope** + **Out of scope** bullet rõ | Chỉ in scope |
| 5 | **Deliverables** | Sản phẩm cụ thể bàn giao | Lẫn với tasks |
| 6 | **Success criteria** | Khi nào coi xong (đo được) | "Khi hài lòng" |
| 7 | **Sponsor / Owner / PM** | 3 vai trò tách bạch | Gộp 1 người |
| 8 | **Timeline cấp cao** | Start/end + 3-5 milestone | Lặp tasks |
| 9 | **Assumptions & Constraints** | Giả định + ràng buộc | Bỏ qua → blame sau |
| 10 | **Status hiện tại** | 🟢/🟡/🔴 + 1 dòng + last update | Không update |
| 11 | **Alerts** | `> [!WARNING]` overdue/blocked | Ẩn risk |

### Optional
- KPI dashboard (mermaid pie task status)
- Recent activity (3-5 events)
- Quick links tài liệu

### Cấm
- ❌ Liệt kê chi tiết từng task (dùng `tasks`)
- ❌ Liệt kê chi tiết từng risk (dùng `risks`)
- ❌ Quá 5 màn hình cuộn

---

## 2. `members` — Team Charter + RACI

**Mục đích:** Ai làm gì, trách nhiệm tới đâu, workload thế nào.

### Sections BẮT BUỘC

| # | Section | Nội dung |
|---|---------|----------|
| 1 | **Roster** | Tên · Role · Email/Slack · % allocation |
| 2 | **RACI Matrix** | Deliverable × People (R/A/C/I) |
| 3 | **Workload** | Người → số task active (bảng hoặc bar) |
| 4 | **Decision rights** | Ai quyết cái gì (budget/tech/scope) |
| 5 | **Escalation path** | Block → escalate ai, thứ tự |

### RACI
- **R** — Responsible (người làm)
- **A** — Accountable (chịu trách nhiệm cuối, **chỉ 1 người/deliverable**)
- **C** — Consulted (hỏi ý kiến)
- **I** — Informed (báo kết quả)

### Cấm
- ❌ Nhiều "A" cho 1 deliverable
- ❌ Role mơ hồ ("support", "help")
- ❌ Người không còn active

---

## 3. `tasks` — Work Breakdown

**Mục đích:** Toàn bộ công việc, status, owner, deadline. Source of truth execution.

### Sections BẮT BUỘC

| # | Section |
|---|---------|
| 1 | Summary counts: 🟢X 🟡Y 🔴Z ⚪W |
| 2 | Group by Phase |
| 3 | Task table: ID · Title · Owner · Status · Start · Deadline · Deps · Source · Notes |
| 4 | **Blocked items** riêng + lý do + người gỡ |
| 5 | **Overdue** callout `> [!WARNING]` |

### Quy tắc mỗi task

BẮT BUỘC có:
- **Owner duy nhất**
- **Deadline cụ thể** DD/MM/YYYY
- **Definition of done** 1 dòng
- **Source** (nếu từ meeting/interview → format Source ở trên; nếu internal → "internal")
- **ID format** `T{phase}-{seq}` (vd T1-03)

### Cấm
- ❌ Không owner / không deadline / không DoD
- ❌ Title mơ hồ ("Làm tài liệu")
- ❌ Mermaid gantt > 30 tasks (fallback bảng)
- ❌ Task không có Source field

---

## 4. `timeline` — Timeline & Milestones

### Sections BẮT BUỘC

| # | Section |
|---|---------|
| 1 | Phase overview: Phase · Start · End · Status · Mục tiêu |
| 2 | Milestone list: M · Date · Owner · Status · Deliverable |
| 3 | **Critical path** — chuỗi task quyết định deadline |
| 4 | Visual: Mermaid gantt (< 8 phases) hoặc ASCII |
| 5 | Key dates 30 ngày tới (5 mốc) |

### Cấm
- ❌ Milestone không có deliverable cụ thể
- ❌ Phase chồng chéo không giải thích
- ❌ Quên update khi slip

---

## 5. `risks` — Risk Register (PMI standard)

### Sections BẮT BUỘC

| # | Section |
|---|---------|
| 1 | Summary: 🔴X 🟠Y 🟡Z 🟢W |
| 2 | Risk register: ID · Risk · Prob · Impact · Score · Owner · Mitigation · Status · **Source** |
| 3 | Critical/High cards chi tiết |
| 4 | Mitigation tracking: action · status · deadline |
| 5 | **Issues** (risk đã materialize) |

### Risk ID: `R{seq}` (R01, R02...)

### Risk score = Probability × Impact
- HH/HM → Critical 🔴
- HL/MH/MM → High 🟠
- ML/LM → Medium 🟡
- LL → Low 🟢

### Quy tắc mỗi risk

BẮT BUỘC có: owner, mitigation action, Source field

### Cấm
- ❌ Risk không owner
- ❌ Risk không mitigation
- ❌ Mô tả mơ hồ ("Có thể có vấn đề")
- ❌ Risk không có Source

---

## 6. `decisions` — Decision Log (audit trail)

**Mục đích:** Ai quyết gì, vì sao, khi nào, hệ quả. **Source-critical page** — mọi decision BẮT BUỘC trace về Level 1.

### Sections BẮT BUỘC

| # | Section |
|---|---------|
| 1 | **Pending decisions** — vấn đề · options · deadline · người quyết |
| 2 | **Confirmed decisions** — format dưới |
| 3 | **Reversed/Superseded** — giữ history |

### Format mỗi decision (BẮT BUỘC đủ 9 field)

```markdown
### D-{ID}: {Title}
- **Ngày:** DD/MM/YYYY
- **Người quyết:** {Name + role}
- **Bối cảnh:** Vì sao cần quyết
- **Options đã cân nhắc:** A / B / C
- **Quyết định:** Chọn X
- **Lý do:** Vì sao chọn X
- **Hệ quả / Action items:** Ai làm gì sau đó
- **Source:**
  - Stub: [Meeting 04/04/2026](slug)
  - Original: [Google Doc](https://...) §3
- **Raw quote:** "chốt dùng MISA làm SoT" (max 15 từ)
- **Speaker:** {ai đã ra quyết định/đề xuất}
- **Confidence:** high|medium|low
```

### Cấm
- ❌ Decision không có ngày
- ❌ Không ghi options đã loại
- ❌ **Sửa decision cũ** → phải tạo decision mới reverse
- ❌ **Không có Source** → không được confirm (chỉ pending)
- ❌ Raw quote quá 15 từ

---

## 7. `documents` — Index tài liệu

**Mục đích:** Index tất cả tài liệu liên quan dự án. Phân loại raw source vs derived document.

### Sections BẮT BUỘC

| # | Section | Nội dung |
|---|---------|----------|
| 1 | **Raw Sources - External** | Bảng: Date · Type · Title · Attendees · **Link (external)** · Stub page |
| 2 | **Raw Sources - Preserved** | Bảng: Date · Type · Title · Attendees · Preserved page |
| 3 | **Meeting notes (derived)** | Date · Title · Attendees · Action items · Link |
| 4 | **Reports** | Báo cáo định kỳ/ad-hoc · Link |
| 5 | **Specs / Designs** | Tài liệu kỹ thuật · Link |
| 6 | **Data / Exports** | File data · Link |
| 7 | **External refs** | Link out (Google Drive, Confluence...) |

### Quy tắc

- **Raw sources** (stub + preserved) PHẢI list riêng khỏi derived docs
- Stub page row → link Stub knowledge + external link
- Preserved page row → link Preserved knowledge
- Derived docs → link knowledge
- Sort theo date desc
- Meeting note row PHẢI có action items

---

## Naming & ID conventions tổng hợp

| Object | ID format | Ví dụ |
|--------|-----------|-------|
| Task | `T{phase}-{seq}` | T1-03 |
| Risk | `R{seq}` | R05 |
| Decision | `D-{seq}` | D-12 |
| Milestone | `M{seq}` | M03 |

---

---

## 8. `agent-activity` — Agent Activity Log (Optional, Goclaw mode)

**Mục đích:** Track Goclaw agent actions, alerts, và sync history cho audit và debugging.

### Sections BẮT BUỘC

| # | Section | Nội dung |
|---|---------|----------|
| 1 | **Sync Status** | Last sync time, hash, result |
| 2 | **Recent Ingests** | Date · Source · Items extracted · Applied/Queued |
| 3 | **Alert History** | Date · Type · Target · Status (sent/ack/escalated) |
| 4 | **Pending Review** | Items queued for PM review (medium/low confidence) |
| 5 | **Audit Trail** | Agent actions log (last 7 days) |

### Sync Status format
```markdown
## Sync Status

- **Last sync:** DD/MM/YYYY HH:MM
- **Status:** OK | PARTIAL | FAILED
- **Changes:** {applied_count} applied, {queued_count} pending
- **Next sync:** in {minutes} minutes
```

### Recent Ingests table
```markdown
## Recent Ingests

| Date | Source | Type | Items | Applied | Queued |
|------|--------|------|-------|---------|--------|
| 18/04/2026 14:30 | goclaw-episodic:abc123 | Interview | 3 | 2 | 1 |
```

### Alert History table
```markdown
## Alert History

| Date | Type | Title | Owner | Status | Response |
|------|------|-------|-------|--------|----------|
| 18/04/2026 10:00 | task_overdue | Task T1-05 | @hung | ack | Updated to 80% |
```

### Pending Review table
```markdown
## Pending Review

> [!NOTE] PM cần review các items dưới đây (confidence < high)

| Item | Type | Source | Confidence | Raw Quote | Action |
|------|------|--------|------------|-----------|--------|
| "Cần thêm API endpoint" | TASK | episodic:xyz | medium | "có lẽ cần thêm API..." | [Approve] [Reject] [Edit] |
```

### Audit Trail format
```markdown
## Audit Trail (Last 7 days)

| Timestamp | Agent | Action | Project | Details | Result |
|-----------|-------|--------|---------|---------|--------|
| 18/04/2026 14:30:15 | goclaw-main | apply | da-xnt | 2 tasks from episodic:abc | success |
| 18/04/2026 14:30:16 | goclaw-main | alert | da-xnt | task_overdue T1-05 | sent |
```

### Quy tắc
- Auto-update sau mỗi sync/ingest/alert
- Giữ audit trail 7 ngày, archive older
- Pending items: highlight nếu > 24h chưa review
- Alert status: `sent` → `ack` → `resolved` hoặc `escalated`

### Cấm
- Không xóa audit entries (chỉ archive)
- Không edit pending items (PM phải approve/reject)
- Không hide failed syncs

---

## Update cadence

| Page | Cadence | Trigger |
|------|---------|---------|
| overview | Weekly + on-demand | Status đổi |
| members | On-change | Thêm/bớt người, đổi role |
| tasks | Daily / on-change | Task update |
| timeline | Weekly | Slip milestone |
| risks | Weekly + on-event | Risk mới, mitigation done |
| decisions | On-event | Có decision mới |
| documents | On-add | Có doc/raw source mới |
| agent-activity | Real-time | Mỗi sync/ingest/alert (Goclaw mode only) |
