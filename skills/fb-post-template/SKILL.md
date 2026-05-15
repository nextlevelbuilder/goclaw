---
name: fb-post-template
description: "Draft a Facebook post with hook + body + CTA in the user's voice. Triggers: 'viết bài FB', 'soạn bài Facebook', 'caption', 'hook', 'viết bài bán hàng', 'lên bài cho anh'. Workflow: pull user's voice/style from USER.md, classify post intent (sales, story, announcement, gratitude), apply matching template, return draft text. No image; pair with image-collage or product-swap if visuals are also needed."
license: Proprietary
---

# fb-post-template — Facebook post draft (Vietnamese)

Generate a publish-ready FB post tailored to the user's voice. Skill produces TEXT only — caller is responsible for pairing with images (separate skill: `image-collage`) and for posting (manual or via channel publisher).

## When to use

- "Viết bài FB hình về chuẩn bị giao khách"
- "Soạn bài bán áo HYFA cho team cầu lông"
- "Lên cho anh 1 bài về cảm ơn khách hàng cuối tuần"
- "Caption cho ảnh sản phẩm 16:9"
- "Hook 3 câu cho video sắp đăng"

## When NOT to use

- Long-form essay → use a writer agent directly (da-vinci or chomsky), no template needed.
- User just wants 1-line caption → write inline; this skill is overkill.

## Inputs

1. **topic** — what the post is about (free-form Vietnamese).
2. **intent** — agent infers from topic:
   - `sales` — selling product/service. CTA: "inbox", "đặt hàng", "comment".
   - `story` — share an experience, build trust. CTA: "share nếu thấy đúng".
   - `announcement` — event, change, news. CTA: "lưu lại", "đến tham gia".
   - `gratitude` — thank customer. CTA: "rate", "review".
3. **voice** — from `USER.md`. If empty, default: friendly + Vietnamese conversational.

## Workflow

```
1. Read USER.md of current user (if exists). Extract: brand name, audience, prior post examples, tone preference.
2. Classify intent from topic keywords.
3. Apply template (below).
4. Return draft as plain text. Ask user 1 line: "Anh OK đăng vầy hông, hay chỉnh lại?"
5. If user requests changes ("ngắn hơn", "thêm cảm xúc", "đổi CTA"), regenerate with tweak.
```

## Templates

### Sales (3-act)

```
🔥 [Hook — câu giật mình hoặc câu hỏi liên quan pain point]

[Body — 2-3 đoạn ngắn:
  - Vấn đề khách đang gặp
  - Giải pháp em đưa ra (sản phẩm/dịch vụ)
  - Tại sao chọn em (USP, social proof, giá)]

📩 [CTA — inbox / comment / đặt hàng / link]
[#hashtag1 #hashtag2]
```

### Story

```
[Hook — 1 câu kéo người đọc vào]

[Body — kể chuyện 3-5 câu, lesson learned, không bán]

❤️ Tag bạn nào thấy giống, hoặc share lưu cho mình.
```

### Announcement

```
📢 [Event/change tagline]

[Detail — what, when, where, why care]

✅ [Action — RSVP / lưu / inbox để biết thêm]
```

### Gratitude

```
🙏 Cảm ơn [audience] đã [hành động cụ thể]

[1-2 đoạn tâm tình — cụ thể, không sáo rỗng]

⭐ [CTA nhẹ — review / share / nhận quà nhỏ]
```

## Voice calibration (per-user)

Read USER.md keys:
- `brand_name` — em xưng "thương hiệu X" thay vì "shop em"
- `tone` — formal | warm | casual | edgy
- `signature_phrase` — câu chốt user hay dùng (vd: "trân trọng anh em đã ủng hộ")
- `audience` — "anh em cầu lông", "mẹ bỉm sữa", "dân văn phòng"…
- `forbidden_phrases` — list câu user ghét nghe

If USER.md empty after first post → propose to save 3-4 keys to USER.md so next post smoother. Ask 1 line: "Em ghi note lại style anh thường dùng được không?"

## Failure modes

- Topic mơ hồ ("viết bài FB cho anh") → hỏi 1 câu: chủ đề + đối tượng + mục tiêu là gì?
- User reject 2 lần liên tiếp → đề xuất user paste 1 bài cũ làm reference; lưu vào USER.md.

## TODO

- [x] Implement `scripts/build-post.mjs` that takes {topic, intent, voice} and returns formatted text (deterministic structure, LLM only for body content).
- [ ] Add 4 golden tests for 4 intents.
