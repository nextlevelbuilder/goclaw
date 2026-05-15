---
name: product-swap
description: "Swap one product (item, package, can, bottle, box, logo, label) inside a photo for another while keeping the rest of the scene unchanged. Triggers: 'đổi thùng X thành Y', 'thay lon/chai', 'đổi nhãn', 'thay logo', 'đổi sản phẩm trong tấm này'. Workflow: agent identifies the old product + the new product (or reference image) + the target photo, builds a precise prompt for create_image with input_images, returns one edited PNG."
license: Proprietary
---

# product-swap — Replace a product in a photo

Targeted edit: change exactly ONE product / branded item in a photo while preserving subject, pose, lighting, scene. Wraps `create_image(input_images=...)` with a vetted prompt template so the result stays faithful to the source.

## When to use

- "Đổi thùng sữa TH thành thùng bia Heineken trong tấm này"
- "Thay logo Adidas thành Nike trên áo người mẫu"
- "Đổi lon Pepsi sang lon Coca trong ảnh đám tiệc"
- "Replace the wine bottle with a champagne bottle, keep label visible"

## When NOT to use

- Whole-image restyle / repaint → use `editor` agent's `create_image` directly with style prompt.
- Adding/removing a product that doesn't exist in the photo → not a "swap", call `create_image` add/remove instead.
- Changing person face → use face-swap prompt (different template).

## Required inputs

1. **target_image** — path to the photo containing the old product.
2. **old_product** — short description of the product to remove. E.g., "thùng sữa TH màu xanh trắng cầm bên trái".
3. **new_product** — either:
   - reference image path (preferred — model has visual ground truth), OR
   - text description (e.g., "thùng bia Heineken xanh lá có logo ngôi sao đỏ").

## Workflow

```
1. Confirm with user (1 turn): "Em sẽ đổi <old_product> trong tấm <target> thành <new_product>, giữ nguyên người và bối cảnh. OK em làm?"
2. Build prompt (template below).
3. Call create_image(
     prompt=<built>,
     input_images=[target_image, new_product_image?],  // 1 or 2 paths
     aspect_ratio=<inferred from target>,
     filename_hint="product-swap-<slug>"
   )
4. Return single MEDIA: path.
```

## Prompt template

```
Trong ảnh tham chiếu thứ nhất, thay <old_product> bằng <new_product_description>.
- Giữ nguyên: người trong ảnh, dáng đứng, biểu cảm, ánh sáng, bối cảnh, các vật khác xung quanh.
- Vật mới phải có tỉ lệ và góc nhìn khớp với vị trí cũ.
- Bóng đổ + phản chiếu trên bề mặt phải phù hợp với vật mới.
- KHÔNG thay đổi gì ngoài <old_product>.
- Chất lượng ảnh giữ đúng độ phân giải gốc.
```

If `new_product` is a reference image (ảnh thứ 2):
```
... bằng vật trong ảnh tham chiếu thứ hai (lấy đúng màu sắc, label, hình dạng của vật trong ảnh 2).
```

## Failure modes

- Old product can't be localized in image → result may swap wrong thing. Mitigation: in confirm turn, mention specific location ("thùng đứng bên trái cạnh chai nước"). User correct → rebuild prompt.
- New product too different in shape (vd: chai → ly cao chân) → bóng đổ sai. Re-run with explicit shape preservation note.
- gpt-image-2 ignores instruction and changes face → re-gen with stronger "DO NOT change face/body" clause.

## TODO

- [x] Add `scripts/build-prompt.mjs` to programmatically construct prompt from {target, old, new} args (so agent doesn't free-form).
- [ ] Add 3 golden examples in `tests/` (TH→Heineken, Adidas→Nike, Pepsi→Coca) for regression.
