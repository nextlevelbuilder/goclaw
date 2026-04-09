package vault

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	enrichMaxDedupEntries = 10000
	enrichContentMaxRunes = 8192
	enrichLLMTimeout      = 5 * time.Minute
	enrichSimilarityLimit = 5
	enrichSimilarityMin   = 0.7
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

	key := payload.TenantID + ":" + payload.AgentID
	if !w.queue.Enqueue(key, payload) {
		return nil // another goroutine already processing this agent's queue
	}

	w.processBatch(ctx, key)
	return nil
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

		type enriched struct {
			payload eventbus.VaultDocUpsertedPayload
			summary string
		}
		var results []enriched

		for _, item := range items {
			// Dedup check.
			w.dedupMu.Lock()
			if prev, exists := w.dedup[item.DocID]; exists && prev == item.ContentHash {
				w.dedupMu.Unlock()
				continue
			}
			w.dedupMu.Unlock()

			// Check if doc already has a summary (e.g., media with caption).
			existing, err := w.vault.GetDocumentByID(ctx, item.TenantID, item.DocID)
			if err != nil {
				slog.Warn("vault.enrich: get_doc", "doc", item.DocID, "err", err)
				continue
			}
			if existing != nil && existing.Summary != "" {
				// Already has summary — skip LLM, still embed+link.
				results = append(results, enriched{payload: item, summary: existing.Summary})
				continue
			}

			// Read file content from disk.
			fullPath := filepath.Join(item.Workspace, item.Path)
			content, err := os.ReadFile(fullPath)
			if err != nil {
				slog.Warn("vault.enrich: read_file", "path", item.Path, "err", err)
				continue // file deleted or moved — don't record in dedup
			}

			// UTF-8 safe truncation.
			runes := []rune(string(content))
			if len(runes) > enrichContentMaxRunes {
				runes = runes[:enrichContentMaxRunes]
			}
			text := string(runes)

			// LLM summarize.
			sctx, cancel := context.WithTimeout(ctx, enrichLLMTimeout)
			summary, err := w.summarize(sctx, item.Path, text)
			cancel()
			if err != nil {
				slog.Warn("vault.enrich: summarize", "path", item.Path, "err", err)
				continue // don't record in dedup — allow retry on next write
			}

			results = append(results, enriched{payload: item, summary: summary})
		}

		// Update summary + embed + auto-link for each enriched doc.
		for _, r := range results {
			if err := w.vault.UpdateSummaryAndReembed(ctx, r.payload.TenantID, r.payload.DocID, r.summary); err != nil {
				slog.Warn("vault.enrich: update_summary", "doc", r.payload.DocID, "err", err)
				continue // don't record in dedup
			}

			// Record hash only after successful update.
			w.recordDedup(r.payload.DocID, r.payload.ContentHash)

			// Auto-link via vector similarity.
			w.autoLink(ctx, r.payload.TenantID, r.payload.AgentID, r.payload.DocID)
		}

		if w.queue.TryFinish(key) {
			return
		}
	}
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

// autoLink finds similar documents and creates semantic links.
func (w *enrichWorker) autoLink(ctx context.Context, tenantID, agentID, docID string) {
	neighbors, err := w.vault.FindSimilarDocs(ctx, tenantID, agentID, docID, enrichSimilarityLimit)
	if err != nil {
		slog.Warn("vault.enrich: find_similar", "doc", docID, "err", err)
		return
	}

	for _, n := range neighbors {
		if n.Score < enrichSimilarityMin {
			continue
		}
		link := &store.VaultLink{
			FromDocID: docID,
			ToDocID:   n.Document.ID,
			LinkType:  "semantic",
			Context:   fmt.Sprintf("auto-linked (score: %.2f)", n.Score),
		}
		if err := w.vault.CreateLink(ctx, link); err != nil {
			slog.Debug("vault.enrich: create_link", "from", docID, "to", n.Document.ID, "err", err)
		}
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
