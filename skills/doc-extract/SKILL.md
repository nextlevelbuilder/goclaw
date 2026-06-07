---
name: doc-extract
description: "Parse a document file (PDF, DOCX, XLSX, PPTX) and return structured text + key facts + 1-paragraph summary. Triggers: user sends a document, or asks 'đọc file này', 'tóm tắt tài liệu', 'trích xuất bảng', 'liệt kê ý chính'. Workflow: detect MIME, delegate to existing format-specific skills (docx/pdf/xlsx/pptx), normalize output into a common JSON shape, generate summary in user's language."
license: Proprietary
---

# doc-extract — Universal document reader

Thin orchestration layer over the existing `docx`, `pdf`, `xlsx`, `pptx` skills. One entry point, format-agnostic, always returns the same shape so callers don't branch by MIME type.

## When to use

- User attaches a document → "đọc giùm em file này"
- "Tóm tắt PDF báo cáo"
- "Trích cột B trong file Excel"
- "Lấy danh sách bullet trong slide 3"
- "File này có nội dung gì"

## When NOT to use

- User sends an IMAGE → use `read_image` tool, not this skill.
- User wants to CREATE a document → separate skill (not built yet).
- File is plain text / markdown → just `read_file`, no extraction needed.

## Inputs

1. **path** — workspace-relative path to document.
2. **task** — optional. One of:
   - `summarize` (default) — 1-paragraph TL;DR
   - `extract_tables` — pull all tables as JSON arrays
   - `extract_text` — full text dump, preserve structure
   - `find` + `query` — semantic search inside doc
   - `outline` — heading hierarchy

## Workflow

```
1. Detect format from extension + magic bytes (don't trust extension alone).
2. Route to format skill:
   - .pdf  → existing `pdf` skill
   - .docx → existing `docx` skill
   - .xlsx → existing `xlsx` skill
   - .pptx → existing `pptx` skill
   - other → reject with "format chưa hỗ trợ"
3. Normalize output to common shape (below).
4. Apply task (summarize / extract / find / outline).
5. Return JSON + human-readable Vietnamese reply.
```

## Common output shape

```json
{
  "format": "pdf|docx|xlsx|pptx",
  "pages": 12,
  "text": "<full text, structure preserved>",
  "tables": [
    { "page": 3, "rows": [["col1","col2"], ...] }
  ],
  "outline": [
    { "level": 1, "title": "Phần 1: ...", "page": 1 },
    { "level": 2, "title": "1.1 ...", "page": 2 }
  ],
  "metadata": { "author": "...", "created": "..." }
}
```

Caller can ignore fields not relevant to the task.

## Summary template (Vietnamese, ≤ 100 từ)

```
📄 <Tên file / loại doc> — <N trang>

Nội dung chính: <1-2 câu khái quát>

Điểm nổi bật:
- <bullet 1>
- <bullet 2>
- <bullet 3>

Em có thể: trích bảng / liệt kê outline / tìm trong file — anh nói em làm.
```

## Failure modes

- File corrupt / encrypted → report rõ + suggest user export lại không password.
- File quá lớn (> 50MB) → reject, suggest split.
- Scanned PDF (image-only, no OCR) → currently fail; flag as known gap, suggest OCR skill (not built).
- Hùng's pattern: gửi file kèm "lên bài cho anh chưa" → bot phải hiểu = (1) extract doc → (2) hand off content to `fb-post-template` skill. Document the chain.

## TODO

- [ ] Implement `scripts/extract.mjs` that dispatches to pdf/docx/xlsx/pptx skills + normalizes output.
- [ ] Add OCR fallback for scanned PDFs (call out to a Python script with `pytesseract` if available).
- [ ] Wire to `fb-post-template` skill when user pattern is "doc + write post".
