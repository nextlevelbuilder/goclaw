#!/usr/bin/env bash
# seed-memory-kg.sh — Populate global canonical memory + KG for NTA Leader
# Usage: source .env.local && ./scripts/seed-memory-kg.sh
#
# Requires: server running on localhost:18790, GOCLAW_GATEWAY_TOKEN set

set -euo pipefail

BASE="http://localhost:18790"
AGENT_ID="019d1bc7-6d37-7d2f-b020-e91c4efd47e3"
TOKEN="${GOCLAW_GATEWAY_TOKEN:?Set GOCLAW_GATEWAY_TOKEN first}"
USER_ID=""  # empty = global canonical scope

AUTH=(-H "Authorization: Bearer $TOKEN" -H "X-GoClaw-User-Id: system" -H "Content-Type: application/json")

put_memory() {
  local path="$1"
  local content="$2"
  local payload
  payload=$(jq -n --arg c "$content" --arg u "$USER_ID" '{content: $c, user_id: $u}')
  local status
  status=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "${AUTH[@]}" \
    -d "$payload" \
    "$BASE/v1/agents/$AGENT_ID/memory/documents/$path")
  if [ "$status" -ge 200 ] && [ "$status" -lt 300 ]; then
    echo "  [OK] memory/$path"
  else
    echo "  [FAIL:$status] memory/$path"
  fi
}

extract_kg() {
  local text="$1"
  local desc="$2"
  local payload
  payload=$(jq -n --arg t "$text" --arg u "$USER_ID" '{
    text: $t,
    user_id: $u,
    provider: "openrouter",
    model: "anthropic/claude-sonnet-4-5",
    min_confidence: 0.8
  }')
  local status body
  body=$(curl -s -o /tmp/kg_resp.json -w '%{http_code}' -X POST "${AUTH[@]}" \
    -d "$payload" \
    "$BASE/v1/agents/$AGENT_ID/kg/extract")
  status="$body"
  body=$(</tmp/kg_resp.json)
  if [ "$status" -ge 200 ] && [ "$status" -lt 300 ]; then
    local ent rel
    ent=$(echo "$body" | jq -r '.entities // 0' 2>/dev/null || echo "?")
    rel=$(echo "$body" | jq -r '.relations // 0' 2>/dev/null || echo "?")
    echo "  [OK] KG extract: $desc → $ent entities, $rel relations"
  else
    echo "  [FAIL:$status] KG extract: $desc"
  fi
}

echo "=== Seeding Global Canonical Memory for NTA Leader ==="
echo "Agent: $AGENT_ID"
echo "Scope: global (user_id='')"
echo ""

# ─── 1. MEMORY.md — Master Index ────────────────────────────────────

echo "--- Step 1: Memory Documents ---"

put_memory "MEMORY.md" "# MEMORY.md — Global Canonical Memory Index

---

## Active Projects

### ♻️ Bán Hàng Cũ GEARVN — 2nd BU (\`secondhand-bu2/\`)
- **Status:** 🟡 Pilot delayed → replan target 05-07/04/2026
- **Owner:** NTA | **BU Head:** Thanh (ThanhLH)
- **Scope:** Thu mua + bán lại hàng công nghệ cũ (LCD, VGA, Main, CPU) tại showroom GearVN
- [Context](memory/secondhand-bu2/context.md) | [Team](memory/secondhand-bu2/team.md) | [Tasks](memory/secondhand-bu2/tasks.md) | [Decisions](memory/secondhand-bu2/decisions.md) | [Risks](memory/secondhand-bu2/risks.md)
- Dashboard: https://gvn-2nd-bu-dashboard.pages.dev
- Workspace: ~/.goclaw/workspace/nta-leader/secondhand-bu2/

### 🎯 Kiểm kê T04/2026 (\`kiem-ke-t04/\`)
- **Status:** 🔄 Development + Frontend Build
- **Owner:** NTA7 | **Tech Lead:** Nam
- Dashboard: https://nta-project-dashboards.pages.dev/kiem-ke-t04/

### 💰 Chuẩn hóa Quy trình Đối soát Thu tiền (\`doi-soat-thu-tien/\`)
- **Status:** 🔴 Khẩn cấp — 6 lỗ hổng, 12 action items
- **Owner:** NTA7 | **Tech Lead:** Nam

### 🔒 Sự cố Rò rỉ Data Khách hàng (\`security-ro-ri-data/\`)
- **Status:** 🔴 Khẩn cấp — 16 AIs, ~17tr thiệt hại
- **Owner:** NTA7 | **Tech Lead:** Nam

### 🔧 Warranty Optimization (\`warranty-optimization/\`)
- **Status:** 🟡 Ideation

---

## Cross-Project
- [Projects Registry](memory/projects.md)
- [People Directory](memory/people.md)

---

*Global canonical — accessible from all scopes via fallback*
*Last updated: 05/04/2026*"

# ─── 2. Projects Registry ────────────────────────────────────────────

put_memory "memory/projects.md" "# Projects Registry (Global Canonical)

## Active Projects

### 1. Bán Hàng Cũ GEARVN — 2nd BU
- **Slug:** \`secondhand-bu2\`
- **Status:** 🟡 Pilot delayed → replan target 05-07/04
- **Timeline:** Kickoff 15/03 → Pilot 29/03 (delayed) → Go-live TBD
- **Owner:** NTA (CEO) | **BU Head:** Thanh (ThanhLH)
- **Tech:** Nam (ERP/GSales) | **QC:** Tài | **Web:** Việt/Thiện
- **Scope:** Thu mua + bán lại LCD/VGA/Main/CPU cũ tại showroom
- **Key metrics:** GM 10-15%, tồn max 300tr, 3 tuần DT, return <5%
- **Dashboard:** https://gvn-2nd-bu-dashboard.pages.dev
- **Workspace:** secondhand-bu2/ (51 tasks, 37 decisions, 5 risks)

### 2. Kiểm kê T04/2026
- **Slug:** \`kiem-ke-t04\`
- **Status:** 🔄 Development + Frontend Build
- **Owner:** NTA7 | **Tech Lead:** Nam
- **Scope:** System + Process + Rules + Task Operations + Frontend UI
- **Dashboard:** https://nta-project-dashboards.pages.dev/kiem-ke-t04/

### 3. Chuẩn hóa Quy trình Đối soát Thu tiền
- **Slug:** \`doi-soat-thu-tien\`
- **Status:** 🔴 Khẩn cấp — 6 lỗ hổng
- **Trigger:** Phiếu thu bất thường tại showroom Nguyễn Cửu Vân

### 4. Sự cố Rò rỉ Thông tin Khách hàng
- **Slug:** \`security-ro-ri-data\`
- **Status:** 🔴 Khẩn cấp — 16 AIs

### 5. Warranty Optimization
- **Slug:** \`warranty-optimization\`
- **Status:** 🟡 Ideation

---
*Updated: 05/04/2026*"

# ─── 3. People Directory ─────────────────────────────────────────────

put_memory "memory/people.md" "# People Directory (Global Canonical)

## Leadership
### NTA7
- **Role:** CEO GearVN, Project Owner
- **Projects:** Tất cả (owner/sponsor)
- **Telegram:** 5894479966

## BU2 — Bán Hàng Cũ Team
### Thanh (ThanhLH) aka Qing, Xiao Qing
- **Role:** BU Head dự án Bán Hàng Cũ
- **Telegram:** 666380893
- **Trách nhiệm:** Nghiệp vụ, pricing, SOP, training, P&L
- **Backup:** Sanh HHT, Hoàng HHT

### Tài
- **Role:** QC/Ops Lead — Vận hành showroom
- **Telegram:** @heeeee111
- **Trách nhiệm:** QC checklist, vật tư showroom, liên hệ chuyên gia, đối tác B2B

### Nam
- **Role:** Tech Lead ERP + Kế toán
- **Telegram:** @namnguyen93
- **Trách nhiệm:** GSales, NhanhVN, SKU, luồng hóa đơn, VAT, pricing engine
- **Projects:** BU2 (Tech), Kiểm kê T04 (Tech Lead), Đối soát (Tech)

### Việt/Thiện
- **Role:** Web Dev
- **Trách nhiệm:** cu.gearvn.com frontend + backend

### Sanh HHT (Võ Trường Sanh)
- **Role:** Backup 1 cho Thanh, Trưởng nhóm KT Showroom
- **Telegram:** @vtsanh (1361203917)

### Hoàng HHT
- **Role:** Backup 2 cho Thanh

### Tùng
- **Role:** PM LCD — Phase 1 category lead
- **Telegram:** @PMD_Tung_CVNHMH

### Đạt Hoàng
- **Role:** PM Head — Quản lý tất cả ngành hàng

### HoàngNM
- **Role:** Quy chuẩn chụp hình SP cũ

## Operations Team (Kiểm kê + Đối soát)
### Việt — Tech Lead Workflow
### OPD Team — Operations Department
### QLSR — Quản lý Showroom (Tài NCV, Hoàng HHT, Khôi KVC, Quang Tôn TBT, Trịnh Nam TH)
### Bộ phận Kho — Warehouse Team

## Support
### Huyền — PM PERI (pháp lý)
### Băng — Operations / Khiếu nại
### Phương Anh — QL Kinh doanh Online
### Thắng — E-commerce Team

---
*Updated: 05/04/2026*"

# ─── 4. Secondhand BU2: Context ──────────────────────────────────────

put_memory "memory/secondhand-bu2/context.md" "# Bán Hàng Cũ GEARVN — 2nd BU: Business Context

## Tổng quan
- **Dự án:** Thu mua + bán lại hàng công nghệ cũ tại showroom GEARVN
- **Phase 1:** LCD 22\"+ FHD+ và PC components (VGA/Main/CPU)
- **Phase 2:** Laptop, Gear (sau pilot)
- **Timeline:** Pilot 29/03 (delayed) → Go-live TBD

## Mô hình kinh doanh
- Thu mua tại showroom (không online) → QC → Định giá hệ thống → Nhập kho → Bán Retail/B2B
- Giá thu ~28-30% giá mới, GM 10-15%, min 10%
- 2 grade: A (Đẹp/Như mới), B (Bình thường). Hàng xấu → thanh lý
- BH 30 ngày flat, DOA 7 ngày, BH Extra 3T=3%/6T=5%/12T=8%
- Tồn kho max 300tr, 3 tuần DT. Tồn >10 ngày → mở B2B

## Key Policies
- **Pricing:** Hệ thống quyết định giá, NV không override. Top-down 10 bước.
- **QC:** 2 grade A/B (bỏ C). QC Checklist LCD v1.1, PC v1.1 approved.
- **BH:** Follow quy trình BH hiện tại GearVN. DOA ưu tiên đổi, KHÔNG hoàn tiền.
- **B2B:** Kênh xả tồn thứ cấp. Giá thu standard 30-35%, thanh lý 35%.
- **SKU:** QSD + model, 1 SKU chung + IMEI, grade = thuộc tính IMEI

## Tech Stack
- **Pricing Engine:** HTML POC v2.2 → production cần build
- **ERP:** GSales + NhanhVN (flag hàng cũ, IMEI, giá vốn)
- **Kế toán:** MISA
- **Website:** cu.gearvn.com (subdomain + API từ OBS)
- **Dashboard:** gvn-2nd-bu-dashboard.pages.dev

## Workspace Files
- Location: ~/.goclaw/workspace/nta-leader/secondhand-bu2/
- docs/ — 40+ documents (approved + drafts)
- memory/ — tasks.md (51 tasks), daily-logs, decisions, risks
- rules/ — 7 rule files
- skills/ — project-ops skill"

# ─── 5. Secondhand BU2: Team ─────────────────────────────────────────

put_memory "memory/secondhand-bu2/team.md" "# BU2 Team & Contacts

## Core Team
| Vai trò | Người | Telegram | Trách nhiệm |
|---------|-------|----------|-------------|
| Owner/Sếp | NTA | 5894479966 | QĐ chiến lược, dashboard, data |
| BU Head | Thanh (ThanhLH/Qing) | 666380893 | Nghiệp vụ, pricing, SOP, training |
| QC/Showroom | Tài | @heeeee111 | QC checklist, vật tư, chuyên gia LCD |
| Kế toán/Tech | Nam | @namnguyen93 | GSales, hạch toán, SKU, database |
| Web | Việt/Thiện | pending | cu.gearvn.com |
| Backup 1 | Sanh HHT | @vtsanh | Backup Thanh, QC đồ cũ |
| Backup 2 | Hoàng HHT | — | Backup Thanh |
| PM LCD | Tùng | @PMD_Tung_CVNHMH | Ngành hàng LCD Phase 1 |
| PM Head | Đạt Hoàng | — | Quản lý tất cả ngành hàng |

## Approval Flow
- **NTA:** Full approve, final decision, go/no-go
- **Thanh:** Approve nghiệp vụ, pricing, QC, BH policy
- Others: Contributors — đóng góp thông tin, update task status

## Đối tác B2B
- Cần 2-3 đối tác (đang liên hệ: Ngôi Sao, TK Computer, Đông Hy)
- Bắt buộc VAT + TK công ty
- Pool hẹp giai đoạn đầu: chỉ brand top, date mới, BH hãng >1 năm"

# ─── 6. Secondhand BU2: Tasks (condensed) ────────────────────────────

put_memory "memory/secondhand-bu2/tasks.md" "# BU2 Tasks Summary (condensed)
> Full board: workspace secondhand-bu2/memory/tasks.md (51 tasks)
> Cập nhật: 02/04/2026

## Sprint Summary
| Tuần | Total | Done | Overdue |
|------|-------|------|---------|
| 1 (15-21/03) | 24 | 11 | 5 |
| 2 (22-28/03) | 10 | 4 | 5 |
| 3 (29/03-03/04) | 17 | 0 | 0 |
| **Total** | **51** | **15** | **10** |

## Critical Blockers (chưa done)
- **2.1** Meeting kế toán + Nam → ⚠️ OVERDUE 8 ngày
- **3.1-3.3** Review SKU, GSales update, DB flag → Block pilot
- **6.1** UAT end-to-end → MISSED (system chưa ready)
- **BH-053** GSales: flag hàng cũ + IMEI → Block bán hàng
- **BH-054** NhanhVN: setup kho QC + kho bán → Block nhập kho
- **BH-055** Pricing Engine production → target 07/04

## Key Milestones
- ✅ Meeting 22/03: chốt 10+ decisions (SKU, grade, BH, pricing)
- ✅ 27/03: approve 6 docs core (SOP, QC, Pricing, Config, Tracking)
- ✅ 29/03: 2 meetings (System SKU 91% chốt, B2B Partner)
- ⏳ Pilot: delayed → target 05-07/04
- ⏳ Go-live: TBD (depends on pilot success)"

# ─── 7. Secondhand BU2: Decisions ────────────────────────────────────

put_memory "memory/secondhand-bu2/decisions.md" "# BU2 Key Decisions (condensed)
> Full log: workspace secondhand-bu2/memory/decisions/DECISION_LOG.md (37 decisions)

## Top Decisions
| # | Date | Decision | By |
|---|------|----------|-----|
| D-001 | 15/03 | Ưu tiên Second-hand trước Service | NTA |
| D-002 | 15/03 | Phase 1: LCD + PC. Phase 2: Laptop, Gear | NTA |
| D-003 | 15/03 | Không bán hàng kém chất lượng — thanh lý | NTA |
| D-004 | 15/03 | Giá do hệ thống quyết định, không cảm tính | NTA |
| D-005 | 15/03 | Mua hàng cũ bắt buộc đến cửa hàng | NTA |
| D-014 | 21/03 | Tồn kho max 300tr / 3 tuần DT | NTA |
| D-019 | 22/03 | BH 30 ngày flat, gia hạn +30 sau sửa | Qing |
| D-020 | 22/03 | DOA 7 ngày, ưu tiên đổi, KHÔNG hoàn tiền | Qing |
| D-021 | 22/03 | Chỉ 2 grade A/B (bỏ C) | Qing |
| D-022 | 22/03 | BH Extra 3T=3%, 6T=5%, 12T=8% | Qing |
| D-024 | 22/03 | Giá thu ~28-30% giá mới, GM 10-15% min 10% | Qing |
| D-025 | 22/03 | NV KHÔNG override giá | Qing |
| D-031 | 25/03 | Ký gửi Laptop dời Phase 2 | NTA |

## Approved Documents (27/03)
- SOP Vận Hành v2.1
- QC Checklist LCD v1.1 + PC v1.1
- Pricing Framework v2.1
- Business Rules Config v1.2
- Tracking Báo cáo v1.1"

# ─── 8. Secondhand BU2: Risks ────────────────────────────────────────

put_memory "memory/secondhand-bu2/risks.md" "# BU2 Risk Register (condensed)
> Full register: workspace secondhand-bu2/memory/risks/RISK_REGISTER.md
> Cập nhật: 02/04/2026

## 🔴 HIGH
| ID | Risk | Owner | Status |
|----|------|-------|--------|
| R2 | Nam chưa clear kế toán/VAT | Nam, Thanh | OVERDUE 8+ ngày |
| R8 | Team communication gap — silent 5 ngày | NTA | CRITICAL |
| R10 | Build deadline MISS — UAT 26/03 MISSED | Nam | CRITICAL |
| R11 | Pilot 29/03 delay → recommend +1 tuần | NTA | PILOT DELAY |
| R12 | Kế toán chưa sẵn sàng cho đơn lẻ | Nam, KTT | NEW |

## 🟡 MEDIUM
| ID | Risk | Status |
|----|------|--------|
| R1 | Tài chưa gặp chuyên gia LCD | MITIGATED |
| R4 | NV showroom chưa training | UPGRADING |
| R5 | Pricing Engine production chưa build | MITIGATED (POC OK) |

## 🟢 RESOLVED
- R6: Backup Thanh → Sanh + Hoàng HHT
- R7: M1 Milestone trượt → Recovered 22/03
- R9: 20 tasks OVERDUE → Giảm còn 5"

# ─── Index all memory ────────────────────────────────────────────────

echo ""
echo "--- Step 2: Indexing memory for embeddings ---"

index_resp=$(curl -s -o /dev/null -w '%{http_code}' -X POST "${AUTH[@]}" \
  -d "{\"user_id\":\"$USER_ID\"}" \
  "$BASE/v1/agents/$AGENT_ID/memory/index-all")
echo "  Index all: HTTP $index_resp"

# ─── 3. KG Extract ──────────────────────────────────────────────────

echo ""
echo "--- Step 3: Knowledge Graph Extraction ---"

# Block 1: People + Organization
extract_kg "GearVN là một công ty bán lẻ công nghệ tại Việt Nam. CEO là NTA (NTA7), người quyết định chiến lược cho tất cả dự án.

Dự án Bán Hàng Cũ GEARVN 2nd BU do NTA sở hữu và Thanh (ThanhLH, biệt danh Qing/Xiao Qing, Telegram 666380893) làm BU Head, quản lý nghiệp vụ, pricing, SOP, training.

Team thành viên:
- Tài (@heeeee111): QC/Ops Lead, vận hành showroom, liên hệ chuyên gia LCD, đối tác B2B
- Nam (@namnguyen93): Tech Lead ERP + Kế toán, quản lý GSales, NhanhVN, SKU, pricing engine
- Việt/Thiện: Web Developer, xây dựng cu.gearvn.com
- Sanh HHT (Võ Trường Sanh, @vtsanh): Backup 1 cho Thanh, Trưởng nhóm KT Showroom, QC đồ cũ
- Hoàng HHT: Backup 2 cho Thanh
- Tùng (@PMD_Tung_CVNHMH): PM LCD, quản lý ngành hàng LCD Phase 1
- Đạt Hoàng: PM Head, quản lý tất cả ngành hàng
- HoàngNM: Quy chuẩn chụp hình SP cũ

NTA là owner dự án, Thanh báo cáo cho NTA. Tài báo cáo cho Thanh. Nam báo cáo cho NTA và Thanh. Sanh và Hoàng HHT là backup cho Thanh khi vắng." \
"People + Organization"

sleep 2

# Block 2: Projects + Dependencies
extract_kg "Dự án chính: Bán Hàng Cũ GEARVN 2nd BU (BU2). Mục tiêu: thu mua + bán lại hàng công nghệ cũ (LCD, VGA, Main, CPU) tại showroom GearVN. Kickoff 15/03/2026, pilot target 05-07/04/2026.

Phase 1 bao gồm LCD (22 inch trở lên, FHD+) và PC components (VGA, Main, CPU). Phase 2 sẽ mở rộng ra Laptop và Gear.

Sub-projects và dependencies:
1. Pricing Engine: POC v2.2 đã deploy, cần build production version tích hợp GSales. Owner: Nam. Deadline: 07/04.
2. cu.gearvn.com: Website bán hàng cũ, subdomain + API từ OBS. Owner: Việt/Thiện. Đang phát triển.
3. BU2 Dashboard: gvn-2nd-bu-dashboard.pages.dev, deploy trên Cloudflare Pages. Tracking KPI, tasks, risks, decisions.
4. QC System: QC Checklist LCD v1.1 và PC v1.1 đã approved. Training nhân viên cần system deploy trước.
5. GSales Integration: Flag hàng cũ, IMEI tracking, giá vốn riêng BU2. Owner: Nam. Deadline: 01/04.
6. NhanhVN Integration: Setup kho QC + kho bán (2 mã kho), tag hàng cũ. Owner: Nam. Deadline: 01/04.

Pricing Engine phụ thuộc vào GSales Integration. UAT phụ thuộc vào cả GSales và NhanhVN. Pilot phụ thuộc vào UAT." \
"Projects + Dependencies"

sleep 2

# Block 3: Concepts + Documents + Policies
extract_kg "Các tài liệu và quy trình quan trọng trong dự án BU2:

SOP Vận Hành v2.1 (approved 27/03): Quy trình vận hành toàn diện cho bán hàng cũ, bao gồm thu mua, QC, nhập kho, bán hàng, BH, đổi trả.

QC Checklist LCD v1.1 (approved 27/03): Checklist kiểm tra chất lượng cho LCD, phân loại Grade A (Đẹp/Như mới), Grade B (Bình thường), Reject (thanh lý).

QC Checklist PC v1.1 (approved 27/03): Checklist kiểm tra cho VGA, Main, CPU.

Pricing Framework v2.1 (approved 27/03): Khung định giá top-down 10 bước. Giá thu 28-30% giá mới. GM 10-15%, min 10%. Brand Y/N, Grade A/B, BH Extra.

Business Rules Config v1.2 (approved 27/03): 13 mục rules cấu hình hệ thống. BH 30 ngày flat, DOA 7 ngày, BH Extra 3/5/8%, Grade A/B/Reject, VAT 10%.

Chính sách bảo hành: 30 ngày flat tất cả ngành hàng. DOA 7 ngày ưu tiên đổi, KHÔNG hoàn tiền. BH Extra 3T=3%/6T=5%/12T=8% giá bán.

Chính sách tồn kho: Max 300 triệu, 3 tuần DT. Tồn >10 ngày mở B2B. Tồn >21 ngày cảnh báo. 3 layer: tổng BU, ngành hàng, SKU." \
"Concepts + Documents + Policies"

sleep 2

# Block 4: Technology
extract_kg "Hệ thống công nghệ dùng trong dự án BU2:

GSales: Hệ thống ERP chính của GearVN. Cần setup: flag hàng cũ, IMEI/Serial tracking, giá vốn riêng BU2, BH tracking + DOA + BH Extra. Owner: Nam.

NhanhVN: Hệ thống quản lý kho. Cần setup: kho QC + kho bán (2 mã kho), tag hàng cũ, IMEI tracking. Owner: Nam.

OBS (Order Business System): Hệ thống đặt hàng. Cung cấp API data SP cho website hàng cũ. Nhập kho qua OBS.

MISA: Phần mềm kế toán. Liên quan luồng nhập đầu vào, xuất hóa đơn.

Pricing Engine: POC v2.2 triển khai dưới dạng HTML. Logic top-down 10 bước. Cần tích hợp production vào GSales. Auto báo giá khi NV nhập SP.

Cloudflare Pages: Hosting cho BU2 Dashboard tại gvn-2nd-bu-dashboard.pages.dev.

cu.gearvn.com: Website bán hàng cũ. Subdomain + API từ OBS. Hiển thị listing SP, giá, trạng thái." \
"Technology"

# ─── 4. Verify ──────────────────────────────────────────────────────

echo ""
echo "--- Step 4: Verification ---"

# Check memory count
mem_count=$(curl -s "${AUTH[@]}" \
  "$BASE/v1/agents/$AGENT_ID/memory/documents?user_id=" | jq 'length' 2>/dev/null || echo "error")
echo "  Global memory documents: $mem_count"

# Check KG stats
kg_stats=$(curl -s "${AUTH[@]}" \
  "$BASE/v1/agents/$AGENT_ID/kg/stats?user_id=" 2>/dev/null)
kg_ent=$(echo "$kg_stats" | jq '.entity_count // 0' 2>/dev/null || echo "?")
kg_rel=$(echo "$kg_stats" | jq '.relation_count // 0' 2>/dev/null || echo "?")
echo "  Global KG entities: $kg_ent, relations: $kg_rel"

# Test memory search
search_hits=$(curl -s -X POST "${AUTH[@]}" \
  -d '{"query":"Thanh BU2 pricing","user_id":"","max_results":3}' \
  "$BASE/v1/agents/$AGENT_ID/memory/search" | jq 'length' 2>/dev/null || echo "error")
echo "  Memory search 'Thanh BU2 pricing': $search_hits hits"

echo ""
echo "=== Done! ==="
echo "Global canonical memory and KG seeded for NTA Leader."
echo "Data is accessible from all scopes via hybrid fallback chain."
