# Local-First Document Text Extraction (PDF/DOCX)

**Date**: 2026-05-29 17:26
**Severity**: Medium
**Component**: read_document tool / DocumentParser
**Status**: Shipped

## What Happened

Delivered local-first text extraction adapter for `read_document` tool. PDF/DOCX files now optionally extract text via `pdftotext` (poppler) / `pandoc` (DOCX sandbox mode) *before* falling back to cloud vision LLM chain. Trades off extraction quality (plaintext loses layout, tables, multi-column semantics) for privacy + token cost savings on text-rich documents.

Opt-in by default OFF — users must explicitly set `document_parser.local_first: true` in config. This deliberate choice avoids shipping a regression: scanned PDFs + image-only documents yield empty local output, forcing cloud fallback anyway, wasting latency on a guaranteed miss.

## The Brutal Truth

This feature feels overdue and also fragile simultaneously. On one hand, it's a no-brainer privacy win for local deployments — why burn Gemini's vision context on a 100-page plaintext PDF when you can `pdftotext` it in 50ms locally? On the other, plaintext extraction fundamentally cannot preserve document structure. Users who expect formatted table output from local parsing *will* be disappointed; they need the vision chain.

The default-OFF toggle was the pragmatic choice: let power users opt in, don't punish document quality for the majority. But that also means the feature ships with zero real-world usage data. We'll learn on production deployments whether the latency win justifies the extraction quality gap.

## Technical Details

**New interface:** `DocumentParser` in `internal/tools/document_parser.go` — two methods:
- `Extract(ctx, path, mime) (string, error)` — returns plaintext or sentinel error (`ErrParserUnsupported`, `ErrParserUnavailable`, `ErrParserEmpty`)
- `Supports(mime) bool` — runtime check: config enabled + binary resolvable via LookPath

**LocalExtractParser implementation:**

1. **No-shell subprocess** (`exec.Command` not `sh -c`) — direct argument array, never string concat.
2. **Process-group kill on timeout** — `SIGTERM` → 3s grace → `SIGKILL` via `setProcessGroup/killProcessGroup`. Pandoc forks helper processes; killing the group reaps grandchildren. NOT `exec.CommandContext` (simpler, but would only kill direct child, orphaning helpers).
3. **Minimal subprocess env** — `buildCredentialedEnv(nil)` captures PATH/HOME/LANG/USER only. Never inherit gateway env (holds DB DSNs, API keys, encryption keys). Same posture as exec tool.
4. **Bounded stdout** — `cappedWriter` buffers up to 500KB (`documentMaxTextBytes`), silently discards overflow, always reports full write so child never blocks on pipe. Appends truncation marker matching the existing direct-text truncation behavior.
5. **Safe binary args:**
   - `pdftotext` omits `--` terminator (poppler's parser doesn't treat it as option-end; would consume it as positional, clobbering the real path).
   - `pandoc --sandbox` for untrusted DOCX (prevents fetch/read-arbitrary-files during conversion).
6. **Extracted text = untrusted** — never changes trust model. Inline with existing direct-text fast-path; no new injection vector.

**Integration into read_document:**

Local-first block sits *after* existing guards (archive check, text-readable fast-path, 20MB read guard) *before* vision chain. Path validated at exec boundary (`validateExecPath` reuses `resolvePathWithAllowed` + `checkDeniedPath` so MediaRef-derived paths get same workspace confinement check).

Any miss (disabled / unsupported mime / missing binary / empty output / exec error / timeout) falls through silently to vision chain — never errors. Clean hit returns plaintext with **zero Provider/Model/Usage** (no LLM spend).

## What We Tried

N/A — new feature, no prior attempts.

## Root Cause Analysis

Motivation: issue #84 (token cost + privacy on local deployments with text-heavy PDFs). Vision LLM chains waste 250+ tokens per document page for purely textual content; local extraction avoids that spend + keeps data off-cloud for privacy-sensitive deployments.

Decision to default OFF stems from observing plaintext extraction limitations: scanned PDFs (image-only) + multi-column layouts + formatted tables all lose semantic information when extracted as flat text. Forcing this on all users regresses experience. Opt-in preserves quality for majority, enables cost savings for explicit users.

## Lessons Learned

1. **Subprocess timeout strategy matters.** Process-group kill is cleanest for extractors that fork helpers. CommandContext would orphan grandchildren. In production, timeouts are less about hung processes (modern tools timeout internally) and more about defense-in-depth; document a timeout will never be hit in practice, but it saves from catastrophic hangs.

2. **Untrusted input + subprocess = attack surface.** Spent care on: minimal env (no secrets), no-shell exec (no globbing), `pandoc --sandbox` (no file I/O), bounded output (no OOM), path validation at exec boundary (no traversal). These are defensive layers; a single layer's failure shouldn't cascade. Validation + env isolation are the hardest wins.

3. **Opt-in vs. opt-out cuts both ways.** Default-OFF means the feature ships with zero usage data. Production will tell us if local extraction is enough for the use cases pushing for it (and if not, whether vision fallback makes it moot). Default-ON would have been faster to adoption but would have regressed document quality perception immediately. Chose sustainable over frictionless.

4. **Truncation markers must match.** Both direct-text and local extraction append the same `[... truncated at 500KB ...]` marker. Downstream (agent, logs) sees identical output shape. Small detail, huge payoff: no code changes needed to handle truncation; it just works.

5. **Testing on stubbed binaries is essential.** Tests mock `LookPath` so `pdftotext`/`pandoc` don't need to be installed to verify logic (timeout kill, capped write, path validation). Leaves open the question of real binary output quality, but that's expected to vary by tool version.

## Next Steps

1. **Monitoring:** Instrument extraction hits vs. misses in production (`slog.Info` already in place). Monitor fallback rate to vision chain — if >90% miss, feature is unused; if <10% miss, local extraction is carrying load.

2. **Real-world feedback:** Opt-in means early power users will report quality issues or token savings. Collect feedback on whether plaintext extraction is good enough for their PDFs. May need advanced config (e.g., OCR pipeline for scanned docs, pandoc output format tuning).

3. **Binary availability docs:** Desktop Lite (`sqliteonly` build) doesn't include `pdftotext`/`pandoc`. Clarify that local-first requires full server variant (or binaries installed at runtime). Update install guides.

4. **Potential enhancements (deferred):**
   - OCR pipeline for scanned PDFs (tesseract) — adds latency, changes computation model
   - Fallback to LLM-only if local extraction is disabled (not yet exposed as easy toggle)
   - Per-document extraction strategy (hint: "scanned" vs. "text" → choose pipeline)

**Owned by:** Tools team.
**Deadline:** Monitor week 1 post-ship for adoption metrics.

---

**Status:** SHIPPED
**Test result:** 13 unit + integration tests pass (9 parser + 4 tool), 581 tools + 48 config tests pass, race-clean
**Code review:** 5/5 acceptance criteria + 10/10 security constraints verified
**Files changed:** 8 files, 686 insertions; new `document_parser.go` (222 LOC), test files, config update
