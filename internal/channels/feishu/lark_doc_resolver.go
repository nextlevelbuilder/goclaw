package feishu

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"
)

// resolveLarkDocs expands Lark docx URLs in the inbound text into inline
// context blocks the LLM can read directly. Called once per inbound Feishu
// message, before the content is published to the agent. Transparent to the
// agent — no tool call round-trip, no new LLM turn.
//
// Behavior:
//   - Extracts distinct docx URLs via extractLarkDocURLs.
//   - Fetches each distinct doc with bounded concurrency (larkDocFetchMaxConc).
//   - Reuses the channel's docCache for repeat lookups within the TTL.
//   - Truncates content to larkDocMaxContentLen so a single large doc cannot
//     blow the LLM context window.
//   - On ErrDocAccessDenied, injects a "[Lark Doc ...: access denied]" marker
//     instead of failing the pipeline.
//   - On any other fetch error, injects a "[Lark Doc ...: fetch failed]"
//     marker and logs a warning for operators.
//
// Returns the original text with the expansion blocks appended. When no URLs
// are found, returns the original text unchanged.
func (c *Channel) resolveLarkDocs(ctx context.Context, text string) string {
	refs := extractLarkDocURLs(text)
	if len(refs) == 0 {
		return text
	}

	// Spam guard: cap the number of doc fetches per inbound message so one
	// message dumping 100 links cannot burn Lark API quota or block the
	// channel handler. Extra refs get a visible "skipped" marker appended so
	// the user knows why their doc wasn't expanded.
	var skippedNotice string
	if len(refs) > larkDocMaxPerMessage {
		skipped := len(refs) - larkDocMaxPerMessage
		refs = refs[:larkDocMaxPerMessage]
		skippedNotice = fmt.Sprintf("\n\n[... %d more Lark doc URLs in this message were skipped (per-message cap %d)]", skipped, larkDocMaxPerMessage)
	}

	blocks := make([]string, len(refs))
	sem := make(chan struct{}, larkDocFetchMaxConc)
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, ref DocRef) {
			defer wg.Done()
			defer func() { <-sem }()
			body := c.fetchOrCacheLarkDoc(ctx, ref.DocID)
			blocks[i] = formatLarkDocBlock(ref, body)
		}(i, ref)
	}
	wg.Wait()

	return text + "\n\n" + strings.Join(blocks, "\n\n") + skippedNotice
}

// fetchOrCacheLarkDoc returns the doc body from cache if present, otherwise
// fetches from Lark, applies truncation, caches the result, and returns the
// truncated body. Soft-failure markers are returned directly and NOT cached
// so a transient permission issue doesn't get locked in for the TTL window.
func (c *Channel) fetchOrCacheLarkDoc(ctx context.Context, docID string) string {
	if c.docCache != nil {
		if cached, ok := c.docCache.Get(docID); ok {
			return cached
		}
	}
	content, err := c.client.GetDocRawContent(ctx, docID)
	if err != nil {
		if errors.Is(err, ErrDocAccessDenied) {
			slog.Warn("feishu.doc_fetch_denied", "doc_id", docID, "error", err)
			return fmt.Sprintf("[Lark Doc %s: access denied — grant the bot app read permission on this document]", docID)
		}
		slog.Warn("feishu.doc_fetch_failed", "doc_id", docID, "error", err)
		return fmt.Sprintf("[Lark Doc %s: fetch failed]", docID)
	}
	// Rune-safe truncation so CJK content (common in Lark deployments) never
	// gets split mid-rune into invalid UTF-8. The cap is measured and applied
	// in runes, and the notice reports the original rune count.
	if utf8.RuneCountInString(content) > larkDocMaxContentLen {
		runes := []rune(content)
		content = string(runes[:larkDocMaxContentLen]) +
			fmt.Sprintf("\n\n[... truncated, original size %d runes]", len(runes))
	}
	if c.docCache != nil {
		c.docCache.Set(docID, content)
	}
	return content
}

// formatLarkDocBlock wraps doc content in clearly-marked delimiters so the
// LLM can distinguish injected document text from the user's own words —
// this is a mitigation against prompt-injection-via-doc-content.
func formatLarkDocBlock(ref DocRef, body string) string {
	return fmt.Sprintf("[Lark Doc: %s]\n%s\n[End of Lark Doc]", ref.OriginalURL, body)
}
