package vault

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"golang.org/x/sync/semaphore"
)

const (
	enrichMaxDedupEntries = 10000
	enrichContentMaxRunes = 8192
	enrichLLMTimeout      = 5 * time.Minute
	enrichSimilarityLimit = 10
	enrichSimilarityMin   = 0.7
	enrichMaxConcurrent   = 3 // max concurrent LLM summarize calls
)

// EnrichWorkerDeps bundles dependencies for the vault enrichment worker.
type EnrichWorkerDeps struct {
	VaultStore store.VaultStore
	Provider   providers.Provider
	Model      string
	EventBus   eventbus.DomainEventBus
}

// RegisterEnrichWorker subscribes the enrichment worker to vault doc events.
// Returns an unsubscribe function for cleanup.
func RegisterEnrichWorker(deps EnrichWorkerDeps) func() {
	w := &enrichWorker{
		vault:    deps.VaultStore,
		provider: deps.Provider,
		model:    deps.Model,
		dedup:    make(map[string]string),
		sem:      semaphore.NewWeighted(enrichMaxConcurrent),
	}
	return deps.EventBus.Subscribe(eventbus.EventVaultDocUpserted, w.Handle)
}

// enrichWorker processes vault document upsert events to generate summaries,
// embeddings, and semantic links between related documents.
type enrichWorker struct {
	vault    store.VaultStore
	provider providers.Provider
	model    string
	queue    enrichBatchQueue

	// Bounded dedup: docID → content_hash. Prevents re-processing unchanged files.
	dedupMu sync.Mutex
	dedup   map[string]string
	sem     *semaphore.Weighted // limits concurrent LLM summarize calls
}

// Handle is the EventBus handler for vault.doc_upserted events.
func (w *enrichWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(eventbus.VaultDocUpsertedPayload)
	if !ok {
		return nil
	}

	// Dedup: skip if same hash already processed.
	w.dedupMu.Lock()
	if prev, exists := w.dedup[payload.DocID]; exists && prev == payload.ContentHash {
		w.dedupMu.Unlock()
		return nil
	}
	w.dedupMu.Unlock()

	// Batch key: tenant + agent for agent-scoped docs. For team/shared docs
	// (empty AgentID), use tenant + docID to avoid collapsing all into one queue.
	batchScope := payload.AgentID
	if batchScope == "" {
		batchScope = payload.DocID
	}
	key := payload.TenantID + ":" + batchScope
	if !w.queue.Enqueue(key, payload) {
		return nil // another goroutine already processing this agent's queue
	}

	w.processBatch(ctx, key)
	return nil
}

// enriched holds a successfully summarized vault document pending embed+link.
type enriched struct {
	payload eventbus.VaultDocUpsertedPayload
	summary string
}

// processBatch drains and processes queued vault doc events in a loop.
func (w *enrichWorker) processBatch(ctx context.Context, key string) {
	for {
		items := w.queue.Drain(key)
		if len(items) == 0 {
			if w.queue.TryFinish(key) {
				return
			}
			continue
		}

		// Phase 1 — Summarize (parallel, bounded by semaphore).
		var (
			mu      sync.Mutex
			results []enriched
			wg      sync.WaitGroup
		)

		for _, item := range items {
			// Pre-check dedup before spawning goroutine (cheap).
			w.dedupMu.Lock()
			if prev, exists := w.dedup[item.DocID]; exists && prev == item.ContentHash {
				w.dedupMu.Unlock()
				continue
			}
			w.dedupMu.Unlock()

			wg.Add(1)
			go func(it eventbus.VaultDocUpsertedPayload) {
				defer wg.Done()
				if err := w.sem.Acquire(ctx, 1); err != nil {
					return // context cancelled
				}
				defer w.sem.Release(1)

				if r, ok := w.summarizeItem(ctx, it); ok {
					mu.Lock()
					results = append(results, r)
					mu.Unlock()
				}
			}(item)
		}
		wg.Wait()

		// Phase 2 — Embed: update summary + embed for all results.
		// Do NOT record dedup here (moved to Phase 4 after classify).
		var embedded []enriched
		for _, r := range results {
			if err := w.vault.UpdateSummaryAndReembed(ctx, r.payload.TenantID, r.payload.DocID, r.summary); err != nil {
				slog.Warn("vault.enrich: update_summary", "doc", r.payload.DocID, "err", err)
				continue
			}
			embedded = append(embedded, r)
		}

		// Phase 3 — Classify links (replaces autoLink).
		if len(embedded) > 0 {
			first := embedded[0].payload
			w.classifyLinks(ctx, first.TenantID, first.AgentID, embedded)
		}

		// Phase 4 — Record dedup + wikilinks.
		// Dedup recorded AFTER classify so failed classify allows re-enrichment.
		for _, r := range embedded {
			w.recordDedup(r.payload.DocID, r.payload.ContentHash)
			w.syncWikilinks(ctx, r.payload)
		}

		if w.queue.TryFinish(key) {
			return
		}
	}
}

// summarizeItem handles dedup check, file read, and LLM summarize for one item.
// Returns (enriched, true) on success, (zero, false) on skip/error.
func (w *enrichWorker) summarizeItem(ctx context.Context, item eventbus.VaultDocUpsertedPayload) (enriched, bool) {
	// Dedup re-check (another goroutine may have processed same docID).
	w.dedupMu.Lock()
	if prev, exists := w.dedup[item.DocID]; exists && prev == item.ContentHash {
		w.dedupMu.Unlock()
		return enriched{}, false
	}
	w.dedupMu.Unlock()

	// Check if doc already has a summary (e.g., media with caption).
	existing, err := w.vault.GetDocumentByID(ctx, item.TenantID, item.DocID)
	if err != nil {
		slog.Warn("vault.enrich: get_doc", "doc", item.DocID, "err", err)
		return enriched{}, false
	}
	if existing != nil && existing.Summary != "" {
		return enriched{payload: item, summary: existing.Summary}, true
	}

	// Media files without summary: skip LLM summarize (binary content is not text).
	// Still proceed to embed+link using title+path only.
	if existing != nil && existing.DocType == "media" {
		return enriched{payload: item, summary: ""}, true
	}

	// Read file content from disk.
	fullPath := filepath.Join(item.Workspace, item.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		slog.Warn("vault.enrich: read_file", "path", item.Path, "err", err)
		return enriched{}, false
	}

	// UTF-8 safe truncation.
	runes := []rune(string(content))
	if len(runes) > enrichContentMaxRunes {
		runes = runes[:enrichContentMaxRunes]
	}

	sctx, cancel := context.WithTimeout(ctx, enrichLLMTimeout)
	summary, err := w.summarize(sctx, item.Path, string(runes))
	cancel()
	if err != nil {
		slog.Warn("vault.enrich: summarize", "path", item.Path, "err", err)
		return enriched{}, false
	}

	return enriched{payload: item, summary: summary}, true
}

const vaultSummarizePrompt = `Summarize this document in 2-3 sentences. Focus on:
- Main topic and purpose
- Key concepts, entities, or decisions
- Actionable information

Be concise. Output only the summary, no preamble.`

// summarize calls LLM to generate a short summary of the document.
func (w *enrichWorker) summarize(ctx context.Context, path, content string) (string, error) {
	resp, err := w.provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: vaultSummarizePrompt},
			{Role: "user", Content: "File: " + path + "\n\n" + content},
		},
		Model:   w.model,
		Options: map[string]any{"max_tokens": 512, "temperature": 0.2},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// syncWikilinks extracts [[wikilinks]] from document content and syncs them as vault links.
// Only processes text files; binary/media files are silently skipped.
func (w *enrichWorker) syncWikilinks(ctx context.Context, p eventbus.VaultDocUpsertedPayload) {
	fullPath := filepath.Join(p.Workspace, p.Path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return // file may be deleted or moved
	}

	doc, err := w.vault.GetDocumentByID(ctx, p.TenantID, p.DocID)
	if err != nil || doc == nil {
		return
	}

	if err := SyncDocLinks(ctx, w.vault, doc, string(content), p.TenantID, p.AgentID); err != nil {
		slog.Warn("vault.enrich: sync_wikilinks", "path", p.Path, "err", err)
	}
}


// recordDedup stores a processed hash and evicts ~25% entries if over capacity.
func (w *enrichWorker) recordDedup(docID, hash string) {
	w.dedupMu.Lock()
	defer w.dedupMu.Unlock()
	w.dedup[docID] = hash
	if len(w.dedup) > enrichMaxDedupEntries {
		// Evict ~25% by iterating and deleting (map iteration order is random in Go).
		target := len(w.dedup) / 4
		evicted := 0
		for k := range w.dedup {
			if evicted >= target {
				break
			}
			delete(w.dedup, k)
			evicted++
		}
	}
}

// --- Inline batch queue (avoids import cycle with orchestration package) ---

type enrichBatchQueueState struct {
	mu      sync.Mutex
	running bool
	entries []eventbus.VaultDocUpsertedPayload
}

// enrichBatchQueue is a minimal producer-consumer queue keyed by string.
type enrichBatchQueue struct {
	queues sync.Map
}

func (bq *enrichBatchQueue) Enqueue(key string, entry eventbus.VaultDocUpsertedPayload) bool {
	v, _ := bq.queues.LoadOrStore(key, &enrichBatchQueueState{})
	q := v.(*enrichBatchQueueState)
	q.mu.Lock()
	defer q.mu.Unlock()
	q.entries = append(q.entries, entry)
	if q.running {
		return false
	}
	q.running = true
	return true
}

func (bq *enrichBatchQueue) Drain(key string) []eventbus.VaultDocUpsertedPayload {
	v, ok := bq.queues.Load(key)
	if !ok {
		return nil
	}
	q := v.(*enrichBatchQueueState)
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.entries
	q.entries = nil
	return out
}

func (bq *enrichBatchQueue) TryFinish(key string) bool {
	v, ok := bq.queues.Load(key)
	if !ok {
		return true
	}
	q := v.(*enrichBatchQueueState)
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) > 0 {
		return false
	}
	q.running = false
	bq.queues.Delete(key)
	return true
}
