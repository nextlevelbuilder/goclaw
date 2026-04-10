package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
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
	enrichSimilarityLimit = 10
	enrichSimilarityMin   = 0.7
	enrichMaxConcurrent      = 3 // max concurrent batch summarize calls across chunks
	enrichBatchSize          = 5 // docs per enrichment chunk (1 LLM call per chunk)
	enrichBatchItemMaxRunes  = 3000 // per-file content limit in batch summarize
	enrichMaxRetries         = 3    // shared retry count for LLM calls (summarize + classify)
)

// Shared retry config for all enrichment LLM calls.
var (
	enrichRetryTimeouts = [enrichMaxRetries]time.Duration{5 * time.Minute, 7 * time.Minute, 10 * time.Minute}
	enrichRetryBackoffs = [enrichMaxRetries]time.Duration{0, 2 * time.Second, 4 * time.Second}
)

// EnrichWorkerDeps bundles dependencies for the vault enrichment worker.
type EnrichWorkerDeps struct {
	VaultStore store.VaultStore
	Provider   providers.Provider
	Model      string
	EventBus   eventbus.DomainEventBus
	MsgBus     bus.EventPublisher // for WS event broadcast
}

// RegisterEnrichWorker subscribes the enrichment worker to vault doc events.
// Returns (unsubscribe func, progress tracker for WS broadcast).
func RegisterEnrichWorker(deps EnrichWorkerDeps) (func(), *EnrichProgress) {
	progress := NewEnrichProgress(deps.MsgBus)
	w := &enrichWorker{
		vault:    deps.VaultStore,
		provider: deps.Provider,
		model:    deps.Model,
		dedup:    make(map[string]string),
		sem:      semaphore.NewWeighted(enrichMaxConcurrent),
		progress: progress,
	}
	unsub := deps.EventBus.Subscribe(eventbus.EventVaultDocUpserted, w.Handle)
	return unsub, progress
}

// enrichWorker processes vault document upsert events to generate summaries,
// embeddings, and semantic links between related documents.
type enrichWorker struct {
	vault    store.VaultStore
	provider providers.Provider
	model    string
	queue    enrichBatchQueue
	progress *EnrichProgress

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

	// Batch key: tenant + agent for agent-scoped docs.
	// Team/shared docs (empty AgentID) batch together per tenant so bulk rescan
	// benefits from chunked processing instead of 1-doc-per-queue.
	batchScope := payload.AgentID
	if batchScope == "" {
		batchScope = "_shared"
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
	title   string // carried from DB for classify phase (avoids refetch)
}

// processBatch drains and processes queued vault doc events in a loop.
// Items are chunked into enrichBatchSize groups so bulk rescan doesn't
// overwhelm the LLM provider with hundreds of concurrent requests.
func (w *enrichWorker) processBatch(ctx context.Context, key string) {
	var totalQueued int

	for {
		items := w.queue.Drain(key)
		if len(items) == 0 {
			if w.queue.TryFinish(key) {
				if totalQueued > 0 {
					w.progress.Finish()
				}
				return
			}
			continue
		}

		totalQueued += len(items)
		tenantID := uuid.Nil
		if len(items) > 0 {
			tenantID, _ = uuid.Parse(items[0].TenantID)
		}
		w.progress.Start(totalQueued, tenantID)

		// Process in chunks of enrichBatchSize, up to enrichMaxConcurrent in parallel.
		var wg sync.WaitGroup
		for start := 0; start < len(items); start += enrichBatchSize {
			end := min(start+enrichBatchSize, len(items))
			if err := w.sem.Acquire(ctx, 1); err != nil {
				break
			}
			wg.Add(1)
			go func(chunk []eventbus.VaultDocUpsertedPayload) {
				defer wg.Done()
				defer w.sem.Release(1)
				w.processChunk(ctx, chunk)
				w.progress.AddDone(len(chunk))
			}(items[start:end])
		}
		wg.Wait()

		if w.queue.TryFinish(key) {
			w.progress.Finish()
			return
		}
	}
}

// processChunk runs the 4-phase enrichment pipeline for a single chunk of docs.
// Phase 1 batches all files into a single LLM call for summarization.
func (w *enrichWorker) processChunk(ctx context.Context, items []eventbus.VaultDocUpsertedPayload) {
	// Phase 0 — Prepare: dedup check, batch-fetch existing docs, read file content.
	type prepared struct {
		payload eventbus.VaultDocUpsertedPayload
		content string // non-empty = needs LLM summarization
		summary string // non-empty = already summarized, skip LLM
		title   string // carried from DB for classify phase
	}

	// Filter dedup'd items first.
	var pending []eventbus.VaultDocUpsertedPayload
	for _, item := range items {
		w.dedupMu.Lock()
		if prev, exists := w.dedup[item.DocID]; exists && prev == item.ContentHash {
			w.dedupMu.Unlock()
			continue
		}
		w.dedupMu.Unlock()
		pending = append(pending, item)
	}
	if len(pending) == 0 {
		return
	}

	// Batch-fetch all existing docs in a single query.
	tenantID := pending[0].TenantID
	docIDs := make([]string, len(pending))
	for i, item := range pending {
		docIDs[i] = item.DocID
	}
	existingDocs, err := w.vault.GetDocumentsByIDs(ctx, tenantID, docIDs)
	if err != nil {
		slog.Warn("vault.enrich: batch_fetch_docs", "count", len(docIDs), "err", err)
		return
	}
	docMap := make(map[string]*store.VaultDocument, len(existingDocs))
	for i := range existingDocs {
		docMap[existingDocs[i].ID] = &existingDocs[i]
	}

	var all []prepared
	for _, item := range pending {
		existing := docMap[item.DocID]
		if existing != nil && existing.Summary != "" {
			all = append(all, prepared{payload: item, summary: existing.Summary, title: existing.Title})
			continue
		}
		if existing != nil && existing.DocType == "media" {
			all = append(all, prepared{payload: item, title: existing.Title})
			continue
		}

		fullPath := filepath.Join(item.Workspace, item.Path)
		raw, err := os.ReadFile(fullPath)
		if err != nil {
			slog.Warn("vault.enrich: read_file", "path", item.Path, "err", err)
			continue
		}
		runes := []rune(string(raw))
		if len(runes) > enrichBatchItemMaxRunes {
			runes = runes[:enrichBatchItemMaxRunes]
		}
		title := ""
		if existing != nil {
			title = existing.Title
		}
		all = append(all, prepared{payload: item, content: string(runes), title: title})
	}
	if len(all) == 0 {
		return
	}

	// Phase 1 — Batch summarize: 1 LLM call for all files needing summary.
	var needLLM []int
	for i, p := range all {
		if p.content != "" && p.summary == "" {
			needLLM = append(needLLM, i)
		}
	}
	if len(needLLM) > 0 {
		paths := make([]string, len(needLLM))
		contents := make([]string, len(needLLM))
		for i, idx := range needLLM {
			paths[i] = all[idx].payload.Path
			contents[i] = all[idx].content
		}
		summaries := w.batchSummarize(ctx, paths, contents)
		for i, idx := range needLLM {
			if i < len(summaries) && summaries[i] != "" {
				all[idx].summary = summaries[i]
			}
		}
	}

	// Build enriched results.
	var results []enriched
	for _, p := range all {
		results = append(results, enriched{payload: p.payload, summary: p.summary, title: p.title})
	}

	// Phase 2 — Embed: update summary + embed per doc.
	var embedded []enriched
	for _, r := range results {
		if err := w.vault.UpdateSummaryAndReembed(ctx, r.payload.TenantID, r.payload.DocID, r.summary); err != nil {
			slog.Warn("vault.enrich: update_summary", "doc", r.payload.DocID, "err", err)
			continue
		}
		embedded = append(embedded, r)
	}

	// Phase 3 — Classify links for this chunk.
	if len(embedded) > 0 {
		first := embedded[0].payload
		w.classifyLinks(ctx, first.TenantID, first.AgentID, embedded)
	}

	// Phase 4 — Record dedup + wikilinks.
	for _, r := range embedded {
		w.recordDedup(r.payload.DocID, r.payload.ContentHash)
		w.syncWikilinks(ctx, r.payload)
	}
}

const batchSummarizePrompt = `Summarize each document in 2-3 sentences. Focus on main topic, key concepts, and actionable information.
Output a JSON array: [{"idx":1,"summary":"..."},{"idx":2,"summary":"..."}]
idx is 1-based matching the document number. Output ONLY valid JSON, no preamble.`

// batchSummarize sends multiple files in a single LLM call and parses JSON summaries.
func (w *enrichWorker) batchSummarize(ctx context.Context, paths, contents []string) []string {
	var b strings.Builder
	for i := range paths {
		fmt.Fprintf(&b, "[%d] File: %s\n%s\n\n", i+1, paths[i], contents[i])
	}

	raw, err := w.chatWithRetry(ctx, "vault.batch_summarize", providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: batchSummarizePrompt},
			{Role: "user", Content: b.String()},
		},
		Model:   w.model,
		Options: map[string]any{"max_tokens": 1536, "temperature": 0.2},
	})
	if err != nil {
		slog.Warn("vault.enrich: batch_summarize", "count", len(paths), "err", err)
		return nil
	}
	return parseBatchSummaries(raw, len(paths))
}

// parseBatchSummaries extracts summaries from LLM JSON response.
func parseBatchSummaries(raw string, expected int) []string {
	raw = strings.TrimSpace(raw)
	// Strip markdown code fence if present.
	if strings.HasPrefix(raw, "```") {
		if i := strings.Index(raw[3:], "\n"); i >= 0 {
			raw = raw[3+i+1:]
		}
		if i := strings.LastIndex(raw, "```"); i >= 0 {
			raw = raw[:i]
		}
		raw = strings.TrimSpace(raw)
	}

	var results []struct {
		Idx     int    `json:"idx"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(raw), &results); err != nil {
		slog.Warn("vault.enrich: parse_batch_summaries", "err", err)
		return nil
	}

	out := make([]string, expected)
	for _, r := range results {
		if r.Idx >= 1 && r.Idx <= expected {
			out[r.Idx-1] = strings.TrimSpace(r.Summary)
		}
	}
	return out
}

// chatWithRetry is the shared retry loop for all enrichment LLM calls.
// Escalating timeouts and backoffs prevent transient provider failures
// (e.g. 529 overloaded) from permanently skipping documents.
func (w *enrichWorker) chatWithRetry(ctx context.Context, logPrefix string, req providers.ChatRequest) (string, error) {
	var lastErr error
	for attempt := range enrichMaxRetries {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(enrichRetryBackoffs[attempt]):
			}
		}
		cctx, cancel := context.WithTimeout(ctx, enrichRetryTimeouts[attempt])
		resp, err := w.provider.Chat(cctx, req)
		cancel()
		if err != nil {
			lastErr = err
			slog.Warn(logPrefix+": retry", "attempt", attempt+1, "err", err)
			continue
		}
		return strings.TrimSpace(resp.Content), nil
	}
	return "", fmt.Errorf("%s exhausted %d retries: %w", logPrefix, enrichMaxRetries, lastErr)
}

// syncWikilinks extracts [[wikilinks]] from document content and syncs them as vault links.
// Skips binary/media files to avoid parsing garbage data as wikilinks.
func (w *enrichWorker) syncWikilinks(ctx context.Context, p eventbus.VaultDocUpsertedPayload) {
	doc, err := w.vault.GetDocumentByID(ctx, p.TenantID, p.DocID)
	if err != nil || doc == nil {
		return
	}
	if doc.DocType == "media" {
		return
	}

	fullPath := filepath.Join(p.Workspace, p.Path)
	f, err := os.Open(fullPath)
	if err != nil {
		return
	}
	// Read up to 4MB — covers large docs while keeping RAM bounded.
	buf := make([]byte, 4<<20)
	n, _ := f.Read(buf)
	f.Close()
	if n == 0 {
		return
	}
	content := buf[:n]

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
