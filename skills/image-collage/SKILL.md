---
name: image-collage
description: "Compose N user-supplied images into a single grid/collage at a target aspect ratio (16:9 for Facebook posts, 1:1 for Instagram, 9:16 for TikTok/Stories). Triggers: 'ghép N tấm', 'design FB post', 'grid', 'khung', 'collage', 'thiết kế tấm chung'. Workflow: agent picks layout based on N + ratio, calls scripts/render-collage.mjs with image paths + ratio + optional brand label, returns single PNG path."
license: Proprietary
---

# image-collage — Multi-image grid layout

Render N input images into 1 composite PNG at target aspect ratio. Cheap deterministic pipeline (no LLM gen) using `sharp` for resize/composite — fast, predictable, no token burn.

## When to use

| User says | N | Ratio | Layout |
|---|---|---|---|
| "ghép 6 tấm FB 16:9" | 6 | 16:9 | 3×2 grid |
| "design poster 9:16 TikTok 4 tấm" | 4 | 9:16 | 2×2 |
| "ảnh sản phẩm 1:1 IG 3 tấm" | 3 | 1:1 | 1+2 split |
| "khung 2 tấm trước-sau" | 2 | 1:1 / 16:9 | side-by-side |

## When NOT to use

- User wants 1 image edited (face swap, product swap, background) → use `editor` agent + `create_image(input_images=...)` directly.
- User wants AI to *generate* new content for the collage → call `create_image` first to produce each panel, then this skill composes.

## Workflow

```
1. Verify N input image paths exist (workspace-restricted).
2. Pick layout based on N + ratio (deterministic; see scripts/layouts.json).
3. Optional: accept brand label / text overlay.
4. Run: node scripts/render-collage.mjs --ratio=16:9 --images=path1,path2,... [--label="..."] [--out=<path>]
5. Return single MEDIA: path.
```

## Output contract

- 1 PNG file in agent workspace under `generated/<date>/collage-<slug>-<ts>.png`.
- Default size: 1920×1080 (16:9), 1080×1920 (9:16), 1080×1080 (1:1).
- Gap between panels: 12px; padding around grid: 32px; background: white (override with `--bg`).

## Script API

`scripts/render-collage.mjs` — single entrypoint, deterministic, no LLM. Uses `sharp`.

```bash
node scripts/render-collage.mjs \
  --ratio=16:9 \
  --images=/path/a.jpg,/path/b.jpg,/path/c.jpg,/path/d.jpg,/path/e.jpg,/path/f.jpg \
  --label="Khang An Badminton" \
  --out=/app/workspace/.../generated/2026-05-15/collage-fb-banner.png
```

Layout rules (see `scripts/layouts.json`):
- 2 images, 16:9 → side-by-side
- 3 images, 16:9 → 1 large left + 2 stacked right (or 3 columns)
- 4 images → 2×2
- 6 images, 16:9 → 3×2
- 9 images → 3×3

Anything else → fallback to most square-ish grid.

## Failure modes

- Input image missing → error with specific path, no partial output.
- Ratio unsupported → list supported ratios, ask user.
- > 12 images → reject; suggest splitting into multiple collages.

## TODO (scaffolding)

- [x] Implement `scripts/render-collage.mjs` (sharp-based).
- [x] Create `scripts/layouts.json` with explicit grid recipes.
- [ ] Add tests/test-collage.mjs with 3 sample compositions (smoke-tested manually 16:9/1:1/9:16).
