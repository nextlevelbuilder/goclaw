# 24 - Hybrid Memory/KG Plan

Kế hoạch này mô tả hướng chuyển hệ thống sang mô hình hybrid:

- chat/session vẫn giữ theo scope gốc
- memory và knowledge graph mặc định giữ theo từng user hoặc từng group
- tri thức dùng chung giữa nhiều nhóm sẽ đi qua một tầng global/canonical có kiểm soát

Mục tiêu là dùng được cho nhiều nhóm mà không làm nhiễu memory/KG do trộn toàn bộ hội thoại vào một scope chung.

---

## 1. Bối cảnh hiện tại

Qua audit code và dữ liệu local:

- `memory` hiện đã có cơ chế `local + fallback global + leader fallback`
- `KG` hiện vẫn thiên về scope hiện tại, chưa có fallback global đối xứng như memory
- một số agent production-like đang bật:
  - `workspace_sharing.share_memory = true`
  - `workspace_sharing.share_knowledge_graph = true`
- nhưng dữ liệu thật trong DB vẫn đang tồn tại chủ yếu theo nhiều scope riêng:
  - DM user
  - group chat
  - một số scope hệ thống

Điều này cho thấy:

1. nhu cầu dùng tri thức đa nhóm là có thật
2. nhưng việc bật shared toàn phần không phải hướng an toàn nhất
3. runtime và dữ liệu lịch sử hiện chưa đồng nhất hoàn toàn với kỳ vọng "global shared"

---

## 2. Mục tiêu

### 2.1. Mục tiêu chính

- Giữ ngữ cảnh riêng của từng user và từng group
- Cho phép tái sử dụng tri thức giữa nhiều nhóm
- Tránh memory/KG bị nhiễu do trộn toàn bộ chat vào một scope chung
- Giữ đường nâng cấp dần, không cần migration phá hủy dữ liệu cũ

### 2.2. Mục tiêu phụ

- Làm cho behavior của `memory` và `KG` nhất quán hơn
- Tách rõ:
  - local operational memory
  - canonical shared knowledge
- Cho phép rollout an toàn theo từng bước

### 2.3. Không nằm trong phase đầu

- Không tự động merge tất cả memory/KG cũ sang global
- Không tự động dedup toàn bộ entity cũ giữa mọi scope
- Không thay đổi schema lớn nếu chưa cần

---

## 3. Kiến trúc mục tiêu

### 3.1. Ba tầng dữ liệu

#### Tầng 1: Session / Chat

- giữ nguyên theo session key hiện tại
- DM vẫn theo user
- group vẫn theo group scope

#### Tầng 2: Local Memory / Local KG

- DM:
  - memory/KG theo từng user
- Group:
  - memory/KG theo từng group

Đây là nơi lưu:

- context vận hành hàng ngày
- task cụ thể của nhóm
- quyết định tạm thời
- ai đang nói gì trong scope hiện tại

#### Tầng 3: Global / Canonical Memory / KG

Đây là nơi chỉ chứa tri thức đã được chọn lọc:

- people canonical
- project canonical
- SOP/quy trình dùng chung
- glossary
- decision đã chốt
- reusable playbook

Tri thức chỉ được đưa lên tầng này qua bước promote có kiểm soát.

---

## 4. Nguyên tắc truy xuất

### 4.1. Memory search

Thứ tự ưu tiên:

1. scope hiện tại (per-user)
2. global canonical (`user_id = ""`)
3. leader fallback (cho team members)

Memory hiện đã implement đầy đủ chain này trong `memory_interceptor.go`.

### 4.2. KG search

Thứ tự ưu tiên (đã implement):

1. KG của scope hiện tại (per-user)
2. KG global canonical (`user_id = ""`)
3. traversal: local trước, fallback global nếu local rỗng (không trộn scope)

### 4.3. KG merge strategy

Khi kết hợp kết quả local + global:

- **GetEntity**: local trước, fallback global chỉ khi local trả sql.ErrNoRows
- **ListEntities/SearchEntities**: local trước, supplement global nếu local chưa đủ limit
- **ListRelations**: local trước, fallback global chỉ khi local rỗng
- **Traverse**: local trước, fallback global chỉ khi local rỗng (KHÔNG trộn path)
- **Dedup**: entity cùng ID → giữ local, bỏ global duplicate

### 4.4. Promotion rule

Không phải mọi chat đều được promote lên global.

Chỉ promote khi dữ liệu:

- đủ ổn định
- đủ tin cậy
- có tính tái sử dụng giữa nhiều nhóm
- không phải chỉ là vận hành tạm thời của một scope

---

## 5. Thay đổi đã thực hiện

> **Lưu ý:** Thứ tự phase đã được đảo so với bản gốc.
> UI fix (Phase 3 cũ) được đưa lên trước để tránh mất field `share_knowledge_graph` khi persist qua UI.

### 5.1. Phase A: Tách cấu hình UI (đã xong)

#### Mục tiêu

Tránh cấu hình backend có field nhưng UI không biểu diễn đúng.

#### Việc đã làm

- thêm `share_knowledge_graph` vào type UI (`WorkspaceSharingConfig` trong `types/agent.ts`)
- tách toggle "Shared Memory & KG" thành 2 toggle riêng biệt:
  - **Shared Memory** — chỉ ảnh hưởng memory store
  - **Shared Knowledge Graph** — chỉ ảnh hưởng KG store
- cập nhật i18n (en, vi, zh) với strings riêng cho Memory và KG

#### Kết quả

- config trong UI và runtime nhất quán
- không còn tình trạng "DB có field nhưng UI không lưu/không hiện"
- persist không drop `share_knowledge_graph` khi save qua UI

---

### 5.2. Phase B: Bổ sung fallback global cho KG (đã xong)

#### Mục tiêu

Đưa behavior của KG gần với memory hơn — local-first read, global canonical fallback.

#### Việc đã làm

Sửa pg store layer — mỗi read method có fallback khi `!IsSharedKG(ctx) && userID != ""`:

- `GetEntity` — local first, fallback global on ErrNoRows
- `ListEntities` — refactor thành `listEntitiesScoped()`, local first, supplement global nếu < limit
- `SearchEntities` — refactor thành `hybridSearchScoped()`, search cả 2 scope, merge với local priority
- `ListRelations` — refactor thành `listRelationsScoped()`, local first, fallback global nếu rỗng
- `ListAllRelations` — refactor thành `listAllRelationsScoped()`, local first, supplement global
- `Traverse` — refactor thành `traverseScoped()`, local first, fallback global nếu rỗng (KHÔNG trộn)

Helper functions:
- `shouldFallbackGlobal(ctx, userID)` — gate chung cho tất cả fallback logic
- `mergeEntitySlices(local, global, limit)` — dedup by entity ID, local wins
- `mergeRelationSlices(local, global, limit)` — dedup by relation ID

#### Yêu cầu hành vi (đã đảm bảo)

- local luôn được ưu tiên hơn global
- kết quả local không bị global "đè"
- traversal không trộn path giữa local và global
- write operations không thay đổi — vẫn write vào current scope

---

### 5.3. Phase C: Annotation trong KG tool (đã xong)

- search results annotate `[global]` cho entities từ global canonical scope
- list-all mode hiển thị count global entities trong summary

---

### 5.4. Phase D: Ổn định scope mặc định (chưa làm — cần ops)

#### Mục tiêu

Chuyển các agent chính sang mode hybrid an toàn.

#### Việc cần làm

- cập nhật config agent:
  - `share_memory = false`
  - `share_knowledge_graph = false`
- không xóa dữ liệu cũ
- không migrate dữ liệu cũ sang global ở phase này

---

### 5.5. Phase E: Cơ chế promote lên global (chưa làm)

#### Mục tiêu

Có đường đưa tri thức dùng chung lên global mà không bật shared toàn phần.

#### Hướng triển khai khuyến nghị

**Cách tối thiểu:**

- dùng chính storage global đã có:
  - memory với `user_id = ""`
  - KG với `user_id = ""`
- thêm thao tác promote thủ công qua admin/API

**Cách tốt hơn:**

- thêm action rõ ràng:
  - promote memory doc
  - promote KG entity/relation set
- ghi metadata nguồn:
  - `promoted_from_scope`
  - `source_group`
  - `source_user`
  - `promoted_at`
  - `confidence`

**Chưa làm trong bước đầu:**

- auto-promote toàn tự động từ mọi chat

---

## 6. Files đã sửa

### Backend (KG fallback)

- `internal/store/pg/knowledge_graph.go` — GetEntity, ListEntities, SearchEntities fallback + helpers
- `internal/store/pg/knowledge_graph_relations.go` — ListRelations, ListAllRelations fallback + helpers
- `internal/store/pg/knowledge_graph_traversal.go` — Traverse fallback
- `internal/tools/knowledge_graph.go` — global scope annotation

### UI / Config

- `ui/web/src/types/agent.ts` — add `share_knowledge_graph` to WorkspaceSharingConfig
- `ui/web/src/pages/agents/agent-detail/config-sections/workspace-sharing-section.tsx` — split into 2 toggles
- `ui/web/src/i18n/locales/en/agents.json` — separate Memory/KG strings
- `ui/web/src/i18n/locales/vi/agents.json` — same
- `ui/web/src/i18n/locales/zh/agents.json` — same

### Reference (không sửa, dùng làm reference)

- `internal/tools/memory_interceptor.go` — memory fallback chain pattern
- `internal/store/context.go` — IsSharedKG(), KGUserID(), shouldFallbackGlobal() context
- `internal/agent/loop_utils.go` — shouldShareKnowledgeGraph() config

---

## 7. Rollout đề xuất

### 7.1. Bước 1

- merge code (fallback global cho KG + UI/config fixes)
- không đụng dữ liệu cũ

### 7.2. Bước 2

- đổi config `nta-leader` và `nta-opus` sang:
  - `share_memory = false`
  - `share_knowledge_graph = false`

### 7.3. Bước 3

- test thực tế trên:
  - 1 DM user
  - 2 group chat khác nhau
- xác minh:
  - memory local vẫn sinh đúng
  - KG local vẫn sinh đúng
  - global fallback đọc được nếu có canonical data
  - search results annotate `[global]` đúng

### 7.4. Bước 4

- mới tính tới promote workflow

---

## 8. Kiểm thử cần có

### 8.1. Functional

- DM A không nhìn nhầm local memory của DM B
- Group X không dùng nhầm local KG của Group Y
- nếu global canonical tồn tại, local search vẫn đọc được global sau local
- traversal không trộn path giữa 2 scope

### 8.2. Regression

- memory search hiện tại không bị hỏng
- KG list/search/traverse không crash khi user scope rỗng
- UI lưu `workspace_sharing` không làm mất field khác
- `share_knowledge_graph` toggle hiển thị và persist đúng

### 8.3. Runtime

- update config agent phải invalidate cache đúng
- agent đang chạy sau update phải dùng behavior mới

---

## 9. Rủi ro

### Rủi ro 1: dữ liệu cũ vẫn còn ở per-scope

Điều này là chấp nhận được trong phase đầu. Không cần migration ngay.

### Rủi ro 2: người dùng hiểu nhầm global fallback là shared write

Plan này chỉ muốn:

- local write
- global read fallback

Không phải:

- global write từ mọi chat

### Rủi ro 3: traversal trộn sai scope

Đã implement traversal fallback theo kiểu an toàn:

- ưu tiên local traversal hoàn toàn
- nếu local rỗng mới fallback sang global riêng biệt
- KHÔNG merge path giữa 2 scope

### Rủi ro 4: performance — double queries

Fallback = thêm query khi local rỗng. Trong worst case (local luôn rỗng), mỗi read operation chạy 2x query. Chấp nhận được vì:

- chỉ fallback khi local thực sự rỗng/thiếu
- search là operation ít gọi (vài lần/conversation)
- không cần cache layer ở phase đầu

---

## 10. Rollback

Nếu rollout có vấn đề:

1. giữ nguyên code đã patch nhưng tắt global fallback bằng feature flag hoặc revert commit
2. bật lại config shared cũ cho agent nếu cần khôi phục behavior hiện tại
3. không cần rollback dữ liệu DB vì phase đầu không migration phá hủy

---

## 11. Kết luận

Hướng hybrid phù hợp hơn shared toàn phần vì:

- giữ sạch ngữ cảnh vận hành theo từng group/user
- vẫn mở đường tái sử dụng tri thức giữa nhiều nhóm
- giảm rủi ro memory/KG bị trộn nhiễu
- rollout được theo bước nhỏ, dễ kiểm thử và rollback

Trình tự đã thực hiện:

1. fix UI/config (tách toggle Memory vs KG)
2. fix KG fallback (local → global canonical read)
3. annotation trong tool output
4. (tiếp theo) tắt shared toàn phần cho agent đang chạy
5. (tiếp theo) kiểm thử local-first behavior
6. (sau đó) promote-to-global workflow
