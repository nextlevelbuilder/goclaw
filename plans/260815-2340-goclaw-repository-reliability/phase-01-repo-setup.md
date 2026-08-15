# Phase 01 — Repo Setup (public goclaw + goclaw-mod + upstream)

> **Status: ✅ DONE** — Repo `qkhalk/goclaw` public, upstream remote set, code đã un-nest về root + push lên `dev`. CI/CD disabled (`.github.disabled`).

## Context

Repo `qkhalk/goclaw` hiện tại là PRIVATE, chưa rõ nội dung git history (được tạo 2026-06-20). User quyết định: **xóa repo cũ và tạo mới** từ dự án hiện tại. Code sẽ nằm trong folder `goclaw-mod/` để:
- Module path giữ nguyên `github.com/nextlevelbuilder/goclaw` → merge upstream ngược không vỡ imports.
- CI/CD workflow nằm ở root repo hiện tại sẽ **không còn được GitHub Actions kích hoạt** khi code nằm trong subfolder (Actions chỉ phát hiện workflow trong `.github/` ở root). → Thỏa yêu cầu "không để repo gốc chạy thẳng main hay CI/CD dự án".

## Requirements

1. Repo `qkhalk/goclaw` → chuyển **public** (không cần xóa → tạo lại, vì xóa repo PRIVATE tạo mới public cũng được nhưng mất git history; ở đây git history hiện tại của repo local này là source upstream — ta đẩy nguyên cây git hiện tại lên repo mới).
   - Lưu ý: nếu repo cũ có commits riêng không quan trọng, ta xóa và tạo mới sạch sẽ. Nếu có history quan trọng → giữ. **Cần user xác nhận**: xóa sạch hay giữ history hiện tại.
2. Tạo folder `goclaw-mod/` chứa **toàn bộ code hiện tại** (ngoại trừ các file meta/cấu trúc dự án: `.git/`, `plans/`, `docs/` vv? — chỉ cần code dự án).
3. Thêm remote `upstream = https://github.com/nextlevelbuilder/goclaw` để merge thủ công.
4. Push lên `origin` (repo public mới).
5. Không thêm auto-sync workflow.

## Files to create/modify

- `git mv` toàn bộ code vào `goclaw-mod/` (hoặc copy) + giữ `.gcloudignore`/gitignore phù hợp.
- Kiểm tra `.gitignore` hiện tại có bỏ lọt gì (tat ca file code phải được commit).
- `plans/`, `.git/`, `GoClaw_Upgrade_Improvement_Plan.md` **không** vào `goclaw-mod` (files project meta nằm ngoài).

## Implementation Steps

1. Kiểm tra git history hiện tại (git log --oneline --branches --decorate).
2. Quyết định: xóa remote origin cũ, tạo repo mới public `qkhalk/goclaw` (gh repo create --public), set remote.
3. Copy (giữ nguyên) toàn bộ code vào `goclaw-mod/`.
4. Git add goclaw-mod + git commit trên branch `dev` (hoặc branch mới `main`? user muốn dự án chạy trên main → set default branch).
5. Push lên origin; set default branch.
6. Thêm remote `upstream`.
7. Verify: repo public + code trong goclaw-mod + CI không auto-run.

## Outcome (khác biệt với current)

- Repo public mới tại `https://github.com/qkhalk/goclaw`.
- Code nằm tại `goclaw-mod/` trong repo.
- Remote `upstream` sẵn sàng để merge thủ công.
- CI/CD (nếu có workflow) KHÔNG tự chạy trên push thẳng main vì code nằm subfolder.

## Tests / Validation

- Kiểm tra trên GitHub UI/API: repo public, default branch đúng.
- `git remote -v` hiển thị origin + upstream.
- Push thành công (exit 0).
- Không có workflow chạy khi push (kiểm tra GitHub Actions tab).

## Risks / Rollback

- **Risk:** Xóa repo cũ mất commits gốc của user (nếu có). → Mitigation: hỏi user trước khi xóa; có thể đổi bảo trì public bằng cách xóa repo rồi tạo lại đúng tên (cần xác nhận rõ).
- **Risk:** Code trong subfolder có thể gãy tooling tìm file ở root (vd Go workspace). → Thực tế Go module hoạt động theo module path, không theo folder root; chỉ một số script/CI có thể giả định code ở root — cần rà soát.
- **Rollback:** Giữ nguyên bản git local hiện tại; chỉ cần tạo lại remote khác nếu cần.