---
name: image-batch-edit
description: "Apply the SAME edit instruction to N images in one go. Triggers: 'edit loạt ảnh kiểu này', 'làm cho hết các tấm', 'apply X cho 5 tấm', 'đổi tông ấm cho tất cả ảnh'. Workflow: agent collects N image paths + 1 instruction, loops create_image per image with shared prompt, returns N output paths. Saves user from repeating 'lại tấm tiếp theo' for each photo."
license: Proprietary
---

# image-batch-edit — Apply 1 edit to many images

When user wants the SAME edit applied to multiple photos. One confirm turn, one ACK, then loop. Prevents the 4-turn-per-image pattern.

## When to use

- "Đổi tông nắng vàng cho 5 tấm này"
- "Thay nền đồng cỏ cho hết bộ ảnh"
- "Thêm logo HYFA góc phải cho 8 ảnh sản phẩm"
- "Tăng độ sáng + thêm vintage filter cho cả album"

## When NOT to use

- Each image needs different edit → call `create_image` per image, no skill.
- Only 1 image → use `create_image` directly.
- User wants 1 edited output combining N images → use `image-collage` or `product-swap`.

## Inputs

1. **images** — array of N workspace-relative image paths (max 10 to bound runtime + cost).
2. **instruction** — what to do to each image. Free-form Vietnamese.
3. **aspect_ratio** — preserve source (default) OR force target (e.g., "all to 1:1").

## Workflow

```
1. Confirm with user 1 turn:
   "Em sẽ apply '<instruction>' lên N tấm: <list 3-5 tên ảnh đầu, ...>. Mất tầm N×4 phút. OK em làm?"
2. ACK 1 turn: "Em làm liền nha anh, đang xử <N> tấm song song."
3. Loop for each image i:
   create_image(
     prompt=<instruction expanded>,
     input_images=[images[i]],
     aspect_ratio=<resolved>,
     filename_hint="batch-<idx>-<slug>"
   )
4. Return all N MEDIA: paths in 1 reply.
5. Each output saved under generated/<date>/batch-<idx>-...png.
```

## Concurrency

- Sequential by default (each gpt-image-2 call 3-5 min × N).
- If goclaw supports tool parallelism for this agent, run 3-at-a-time (configurable per-skill).
- Hard cap: 10 images per batch to keep total runtime under 30 min + cost predictable.

## Prompt expansion

User says: _"Đổi tông nắng vàng cho 5 tấm này"_

Expanded per-image prompt:
```
Giữ nguyên người + bố cục + chủ thể chính của ảnh tham chiếu. 
Đổi tông màu sang nắng vàng ấm áp (warm golden hour). 
Ánh sáng chiếu nghiêng từ một bên, làm nổi viền chủ thể.
Không thay đổi mặt, trang phục, vật thể.
```

If user instruction is vague ("đẹp hơn"), agent must ask 1 follow-up: _"Anh muốn đẹp kiểu nào — sáng hơn, ấm hơn, hay đổi style?"_ before loop.

## Output format

Return as compact list:
```
Xong N tấm rồi anh:
1. MEDIA:<path1>
2. MEDIA:<path2>
...
```

OR send each as separate Zalo message if N small (≤ 3). Configurable via `--delivery=batch|stream`.

## Failure modes

- 1 image in batch fails (timeout, gpt-image-2 reject) → continue rest, report which failed. Don't abort whole batch.
- All fail → report root cause (likely model issue) + suggest retry.
- Cost overrun → at 5 images mid-loop, ping user to confirm continuing.

## TODO

- [ ] ~~scripts/batch-edit.mjs~~ — không cần. Specialist `editor` agent tự loop `create_image` qua iterations; node script không gọi tool Go được.
- [ ] Add cost estimator: report total cost upfront ("~$0.40 cho 5 tấm").
