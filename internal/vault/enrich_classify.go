package vault

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	classifyMaxTokens       = 4096
	classifyTemperature     = 0.1
	classifyCtxMaxLen       = 256 // max context string length stored in DB
	classifySummaryMaxChars = 300 // max summary chars in prompt (validated: 300 for accuracy)
	classifyMaxSourceDocs   = 20  // max source docs per classifyLinks call (validated: cap unbounded time)
	classifyChunkSize       = 5   // candidates per LLM call to fit response within max_tokens
)

// validClassifyTypes — accepted link types stored directly in DB (aligned with UI vault-link-dialog.tsx).
var validClassifyTypes = map[string]bool{
	"reference": true, "depends_on": true, "extends": true,
	"related": true, "supersedes": true, "contradicts": true,
}

type classifyDoc struct {
	DocID, Title, Path, Summary string
}

type candidatePair struct {
	Source, Candidate classifyDoc
	Score             float64
}

// classifyLinks orchestrates LLM-based link classification for enriched docs.
func (w *EnrichWorker) classifyLinks(ctx context.Context, provider providers.Provider, model, tenantID, agentID string, results []enriched) {
	if provider == nil {
		return
	}

	capped := results
	if len(capped) > classifyMaxSourceDocs {
		capped = capped[:classifyMaxSourceDocs]
	}

	// On SQLite (desktop/Lite), FindSimilarDocs returns nil (no pgvector) — classify is a no-op.
	candidates := w.gatherCandidates(ctx, tenantID, agentID, capped)
	if len(candidates) == 0 {
		return
	}

	allTypes := slices.Collect(maps.Keys(validClassifyTypes))
	allTypes = append(allTypes, "semantic") // clean up legacy links

	for sourceDocID, pairs := range candidates {
		source := pairs[0].Source
		allCandidates := make([]classifyDoc, len(pairs))
		for i, p := range pairs {
			allCandidates[i] = p.Candidate
		}

		// Chunk candidates to keep LLM response within max_tokens.
		var newLinks []store.VaultLink
		for chunkStart := 0; chunkStart < len(allCandidates); chunkStart += classifyChunkSize {
			chunkEnd := min(chunkStart+classifyChunkSize, len(allCandidates))
			chunk := allCandidates[chunkStart:chunkEnd]

			system, user := buildClassifyPrompt(source, chunk)
			raw, err := w.callClassifyWithRetry(ctx, provider, model, system, user)
			if err != nil {
				slog.Warn("vault.classify: llm_failed", "doc", sourceDocID, "chunk", chunkStart, "err", err)
				if w.progress != nil {
					w.progress.AddError(fmt.Sprintf("classify failed for %s: %v", sourceDocID, err))
				}
				continue
			}

			parsed, err := parseClassifyResponse(raw, len(chunk))
			if err != nil {
				slog.Warn("vault.classify: parse_failed_first", "doc", sourceDocID, "err", err, "raw_len", len(raw), "raw", raw)
				hint := fmt.Sprintf("\n\nPrevious response was invalid JSON (error: %s). Output ONLY a valid JSON array.", err.Error())
				raw2, err2 := w.callClassifyWithRetry(ctx, provider, model, system, user+hint)
				if err2 != nil {
					slog.Warn("vault.classify: retry_parse_failed", "doc", sourceDocID, "err", err2)
					continue
				}
				parsed, err = parseClassifyResponse(raw2, len(chunk))
				if err != nil {
					slog.Warn("vault.classify: parse_still_failed", "doc", sourceDocID, "err", err, "raw_len", len(raw2), "raw", raw2)
					continue
				}
			}

			for _, r := range parsed {
				if r.Type == "SKIP" || !validClassifyTypes[r.Type] {
					continue
				}
				linkCtx := r.Ctx
				if len(linkCtx) > classifyCtxMaxLen {
					linkCtx = string([]rune(linkCtx)[:classifyCtxMaxLen])
				}
				// r.Idx is 1-based within chunk; map back to original candidate.
				newLinks = append(newLinks, store.VaultLink{
					FromDocID: sourceDocID,
					ToDocID:   chunk[r.Idx-1].DocID,
					LinkType:  r.Type,
					Context:   linkCtx,
				})
			}
		}

		// Only replace old links if LLM produced valid replacements (avoid data loss on all-SKIP).
		if len(newLinks) > 0 {
			if err := w.vault.DeleteDocLinksByTypes(ctx, tenantID, sourceDocID, allTypes); err != nil {
				slog.Warn("vault.classify: delete_old", "doc", sourceDocID, "err", err)
			}
			if err := w.vault.CreateLinks(ctx, newLinks); err != nil {
				slog.Debug("vault.classify: batch_create_links", "from", sourceDocID, "count", len(newLinks), "err", err)
			}
		}
	}
}

func (w *EnrichWorker) gatherCandidates(ctx context.Context, tenantID, _ string, results []enriched) map[string][]candidatePair {
	seen := make(map[string]bool)
	out := make(map[string][]candidatePair)

	for _, r := range results {
		// Search across ALL docs in the tenant (empty agentID = no agent filter).
		// Cross-agent links are created freely; access control is enforced at
		// query time so agents only see their own docs. Pre-built cross-agent
		// links enable future vault sharing without re-enrichment.
		neighbors, err := w.vault.FindSimilarDocs(ctx, tenantID, "", r.payload.DocID, enrichSimilarityLimit)
		if err != nil {
			slog.Warn("vault.classify: find_similar", "doc", r.payload.DocID, "err", err)
			continue
		}
		// Use title carried from Phase 0 batch-fetch (avoids per-doc refetch).
		title := r.title
		if title == "" {
			title = r.payload.Path
		}
		src := classifyDoc{
			DocID:   r.payload.DocID,
			Title:   title,
			Path:    r.payload.Path,
			Summary: truncateSummary(r.summary),
		}
		for _, n := range neighbors {
			if n.Score < enrichSimilarityMin || n.Document.Summary == "" {
				continue
			}
			// Skip meaningless filenames as link targets — they create noise.
			if shouldSkipEnrichment(n.Document.PathBasename) {
				continue
			}
			// Bidirectional dedup: only process each pair once.
			a, b := src.DocID, n.Document.ID
			if a > b {
				a, b = b, a
			}
			if key := a + ":" + b; seen[key] {
				continue
			} else {
				seen[key] = true
			}
			out[src.DocID] = append(out[src.DocID], candidatePair{
				Source: src,
				Candidate: classifyDoc{
					DocID:   n.Document.ID,
					Title:   n.Document.Title,
					Path:    n.Document.Path,
					Summary: truncateSummary(n.Document.Summary),
				},
				Score: n.Score,
			})
		}
	}
	return out
}

// callClassifyWithRetry calls the LLM with shared retry logic.
func (w *EnrichWorker) callClassifyWithRetry(ctx context.Context, provider providers.Provider, model, system, user string) (string, error) {
	return w.chatWithRetry(ctx, provider, "vault.classify", providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": classifyMaxTokens, "temperature": classifyTemperature},
	})
}
