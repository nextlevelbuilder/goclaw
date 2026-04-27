# Cron Jobs Audit — 2026-04-27 (v2)

**Total:** 13 active | **Agents:** nta-leader (3), finance (6), gp-pm-tracker (1), gearvn-v2 (1), media-mkt (1), nta-leader/security (1)
**Optimized from:** 31 → 13 (58% reduction). Peak hour nta-leader: 11 → 3 crons.

## Architecture

### Prompt Management
Crons dùng 2 mode:
- **promptFile** — prompt lưu trong workspace `~/.goclaw/workspace/{agent}/prompts/*.md`, version-controlled, dễ edit
- **inline** — prompt trong DB payload, dùng cho các cron ngắn/đơn giản (finance)

Template variables (expanded lúc runtime bởi `expandCronVars()`):
`{{date}}` `{{date_vn}}` `{{datetime}}` `{{weekday}}` `{{month}}` `{{agent}}` `{{project}}` `{{task}}` `{{job_name}}` `{{job_id}}`

### Naming Convention
Format: `{agent}/{project}/{task}` — enforced bởi `store.ValidateCronName()` tại cả WS handler và agent tool.

### Settings
- **stateless=true** cho tất cả 13 crons (tiết kiệm tokens — cron session không cần conversation history)
- **timezone** Asia/Ho_Chi_Minh hoặc Asia/Saigon cho mọi cron

---

## Nhóm 1: DAILY STANDUP / PROGRESS (6 crons)

| Cron | Agent | Schedule | Channel | Mode | Prompt File |
|------|-------|----------|---------|------|-------------|
| `gearvn-v2/dev/daily-standup` | gearvn-v2 | 09:10 T2-T7 | telegram/gearvn-v2 | promptFile | `prompts/daily-standup.md` |
| `media-mkt/marketing/daily-standup` | media-mkt | 09:20 T2-T7 | telegram/media-mkt | promptFile | `prompts/daily-standup.md` |
| `gp-pm-tracker/sales/daily-report` | gp-pm-tracker | 10:00 T2-T6 | telegram/gp-tracker-bot | promptFile | `prompts/daily-gp-report.md` |
| `nta-leader/thai-ha/morning-report` | nta-leader | 08:15 daily | nta-telegram-ops | promptFile | `prompts/thai-ha-morning-report.md` |
| `nta-leader/misa-api/checkin-progress` | nta-leader | 09:00 T2,T5 | nta-telegram-ops | promptFile | `prompts/misa-api-checkin.md` |
| `nta-leader/security/followup-data-leak` | nta-leader | 09:00 daily | nta-telegram-ops | promptFile | `prompts/security-followup.md` |

### Phân loại chức năng

**A. Daily Standup (gearvn-v2, media-mkt)**
- Template chung: đọc HEARTBEAT.md → tasks.md → daily-logs → tổng hợp Yesterday/Today/Blockers/Sprint%
- Output: log vào `memory/daily-logs/{{date}}.md`
- Khác biệt: gearvn-v2 thêm `git pull + git log`, media-mkt thêm scan `memory/risks/`

**B. Data Report (gp-pm-tracker, thai-ha)**
- Truy vấn external data (D1/BigQuery) → format → deliver
- gp-pm-tracker: 4 cate x brand x SKU từ D1
- thai-ha: 4 phần (tồn kho + tasks QLSR + doanh thu + NV target) từ BigQuery

**C. Checkin / Follow-up (misa-api, data-leak)**
- Đọc workspace data → report status → tag stakeholders
- misa-api: đọc `memory/misa-api/tasks.md` → tag team hỏi cụ thể
- data-leak: đọc `memory/security-data-leak/action-items-*.md` → quote nguyên văn

## Nhóm 2: SECURITY (1 cron inline)

| Cron | Agent | Schedule | Channel | Mode |
|------|-------|----------|---------|------|
| `nta-leader/security/scan-data-leak` | nta-leader | 16:00 daily | nta-telegram-ops | inline |

Fetch Google Sheet CSV → update case mới. Khác với `followup-data-leak` (sáng: report status) — cron này (chiều: scan new data).

## Nhóm 3: FINANCE (6 crons inline)

| Cron | Agent | Schedule | Channel | Trigger |
|------|-------|----------|---------|---------|
| `finance/cashflow/monthly-summary` | finance | 08:00 ngày 1 | telegram/personal-cfo | Monthly |
| `finance/cashflow/scan-email-transactions` | finance | 21:00 mỗi 3 ngày | telegram/personal-cfo | Every 3d |
| `finance/credit-card/check-mb-visa-nta` | finance | 10:00 ngày 9 | telegram/personal-cfo | Monthly |
| `finance/credit-card/check-mb-visa-khang` | finance | 10:00 ngày 14 | telegram/personal-cfo | Monthly |
| `finance/credit-card/check-vcb-khang` | finance | 10:00 ngày 22 | telegram/personal-cfo | Monthly |
| `finance/credit-card/remind-tcb-payment` | finance | 09:00 ngày 7 | telegram/personal-cfo | Monthly |

Finance crons giữ inline mode vì prompt ngắn, chạy monthly, ít thay đổi.

---

## Peak Hour Load (nta-leader)

| Time | Cron | Type |
|------|------|------|
| 08:15 | thai-ha/morning-report | Data report (BigQuery) |
| 09:00 | misa-api/checkin-progress (T2,T5 only) | Checkin |
| 09:00 | security/followup-data-leak | Follow-up |

**3 crons max trong peak** (giảm từ 11). Claude-cli session ~2-5 min/turn → no contention.

## Workspace Structure

Prompt files đặt trong `prompts/` folder mỗi agent workspace:

```
~/.goclaw/workspace/
├── gearvn-v2/
│   ├── prompts/daily-standup.md
│   ├── memory/tasks.md
│   ├── memory/daily-logs/
│   ├── memory/risks/
│   └── HEARTBEAT.md
├── media-mkt/
│   ├── prompts/daily-standup.md
│   ├── memory/tasks.md
│   ├── memory/daily-logs/
│   ├── memory/risks/
│   └── HEARTBEAT.md
├── gp-pm-tracker/
│   ├── prompts/daily-gp-report.md
│   ├── memory/daily-logs/
│   └── HEARTBEAT.md
└── nta-leader/
    ├── prompts/
    │   ├── thai-ha-morning-report.md
    │   ├── misa-api-checkin.md
    │   └── security-followup.md
    ├── memory/misa-api/
    └── memory/security-data-leak/
```

## Prompt Template Convention

### Daily Standup template
```
DAILY STANDUP — {{agent}}/{{project}} — {{date}}

## Data Sources
1. Đọc HEARTBEAT.md → standup format
2. Đọc memory/tasks.md → task status
3. Đọc memory/daily-logs/ → log hôm qua
4. [Agent-specific: git pull, risks scan, etc.]

## Output
Yesterday done / Today plan / Blockers / Sprint %
Highlight overdue + deadline hôm nay.

## Post-standup
Log vào memory/daily-logs/{{date}}.md

## Rules
- KHÔNG dùng message tool. Chỉ reply text.
```

### Checkin/Follow-up template
```
CHECK-IN/FOLLOW-UP — {{job_name}} — {{date}}

## Data Sources
[Đọc workspace files cụ thể]

## Output
[Report format: Owner / Task / Status]
[Tag stakeholders]

## Rules
- KHÔNG dùng message tool. Chỉ reply text.
- Đọc data trước → hỏi cụ thể, không hỏi chung chung.
```

### Data Report template
```
[REPORT TITLE] — {{date}}

## Data Sources
[Query chi tiết: dataset, filters, partition]

## Output
[Format chi tiết]

## Post-report
Lưu vào [path]/{{date}}.md

## Rules
- KHÔNG dùng message tool. Chỉ reply text.
```

---

## Changes Log
- **2026-04-27:** v2 rewrite — 31→13 crons, naming convention, template vars, promptFile migration, stateless=true all
- **2026-04-26:** v1 initial audit — identified 31 crons, classified 5 groups, proposed optimizations
