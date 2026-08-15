# goclaw (qkhalk)

Repo cá nhân của **qkhalk** — theo dõi và đánh giá **GoClaw** agent runtime, kèm các cải tiến reliability cá nhân.

Upstream: [`nextlevelbuilder/goclaw`](https://github.com/nextlevelbuilder/goclaw)

## Cấu trúc

Source GoClaw nằm ở **repo root** (đã un-nest khỏi `goclaw-mod/`), giữ nguyên module path `github.com/nextlevelbuilder/goclaw` để merge upstream ngược không vỡ imports.

Các GitHub Actions workflow của upstream nằm trong `.github.disabled/` — **không tự chạy** trên repo này (an toàn, không deploy hay release tự động). Riêng `ci.yaml` được enable tại `.github/workflows/ci.yaml` (trigger `main` + `dev` + PR) để theo dõi build/test. `dev-beta-release.yaml` chuyển **manual-only** (`workflow_dispatch`) — không auto-deploy production khi push `dev`.

## Fork Features

Xem mục **Fork Features** trong [`README.md`](README.md): reliability layer (`internal/reliability/`) + cấu hình repo/CI.

## Quick Start

Xem [`README.md`](README.md) → **Quick Start**: one-liner install (`curl | bash` cho macOS/Linux/WSL, `irm | iex` cho Windows) hoặc build từ source.

## Quy trình merge từ upstream (thủ công)

Khi upstream có thay đổi hay, tôi chủ động merge (không có auto-sync):

```bash
git fetch upstream
git merge upstream/dev            # hoặc nhánh muốn merge
# giải quyết conflict nếu có
git push origin <branch>
```

Remote:
- `origin`   → `github.com/qkhalk/goclaw` (repo này)
- `upstream` → `github.com/nextlevelbuilder/goclaw` (gốc)

## Cải tiến kế hoạch

Xem [`GoClaw_Upgrade_Improvement_Plan.md`](GoClaw_Upgrade_Improvement_Plan.md) (workspace) và `plans/` cho roadmap cải tiến reliability.
