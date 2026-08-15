# goclaw (qkhalk)

Repo cá nhân của **qkhalk** — theo dõi và đánh giá **GoClaw** agent runtime, kèm các cải tiến reliability cá nhân.

Upstream: [`nextlevelbuilder/goclaw`](https://github.com/nextlevelbuilder/goclaw)

## Cấu trúc

```
goclaw-mod/          # Toàn bộ source GoClaw (fork riêng của tôi)
                    #  - Giữ nguyên module path github.com/nextlevelbuilder/goclaw
                    #  - Để merge upstream ngược không vỡ imports
```

Vì source nằm trong `goclaw-mod/`, các GitHub Actions workflow của upstream (`.github/workflows/` bên trong `goclaw-mod/`) **không tự chạy** trên repo này — an toàn, không deploy hay release tự động. Đây là chủ ý.

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