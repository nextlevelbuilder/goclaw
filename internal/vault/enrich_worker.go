package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/bgalert"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
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
	VaultStore    store.VaultStore
	SystemConfigs store.SystemConfigStore   // per-tenant provider config
	Registry      *providers.Registry       // provider resolution
	EventBus      eventbus.DomainEventBus
	MsgBus        bus.EventPublisher        // for WS event broadcast
	TeamStore     store.TaskCommentStore    // for Phase 2.5 task-based auto-linking (nil-safe)
	AlertDeps     bgalert.AlertDeps         // for reporting non-retryable LLM errors
}

// RegisterEnrichWorker subscribes the enrichment worker to vault doc events.
// Returns (unsubscribe func, progress tracker, EnrichWorker for stop/enqueue).
func RegisterEnrichWorker(deps EnrichWorkerDeps) (func(), *EnrichProgress, *EnrichWorker) {
	progress := NewEnrichProgress(deps.MsgBus)
	w := &EnrichWorker{
		vault:         deps.VaultStore,
		teamStore:     deps.TeamStore,
		systemConfigs: deps.SystemConfigs,
		registry:      deps.Registry,
		msgBus:        deps.MsgBus,
		alertDeps:     deps.AlertDeps,
		dedup:         make(map[string]string),
		sem:           semaphore.NewWeighted(enrichMaxConcurrent),
		progress:      progress,
		cancelFuncs:   &sync.Map{},
	}
	unsub := deps.EventBus.Subscribe(eventbus.EventVaultDocUpserted, w.Handle)
	return unsub, progress, w
}

// EnrichWorker processes vault document upsert events to generate summaries,
// embeddings, and semantic links between related documents.
// Exported so HTTP handlers can call Stop/EnqueueUnenriched.
type EnrichWorker struct {
	vault         store.VaultStore
	teamStore     store.TaskCommentStore      // nil-tolerant — Phase 2.5 disabled when nil
	systemConfigs store.SystemConfigStore     // per-tenant provider config
	registry      *providers.Registry         // provider resolution
	msgBus        bus.EventPublisher          // for error event broadcast
	alertDeps     bgalert.AlertDeps           // for reporting non-retryable LLM errors
	queue         enrichBatchQueue
	progress      *EnrichProgress

	// Bounded dedup: docID → content_hash. Prevents re-processing unchanged files.
	dedupMu sync.Mutex
	dedup   map[string]string
	sem     *semaphore.Weighted // limits concurrent LLM summarize calls

	// Per-tenant cancel functions for stop capability
	cancelFuncs *sync.Map // key: tenantID string, value: context.CancelFunc
}

// resolveProviderForTenant delegates to shared background provider resolution.
func (w *EnrichWorker) resolveProviderForTenant(ctx context.Context, tenantID string) (providers.Provider, string) {
	tid, err := uuid.Parse(tenantID)
	if err != nil {
		tid = providers.MasterTenantID
	}
	return providerresolve.ResolveBackgroundProvider(ctx, tid, w.registry, w.systemConfigs)
}

// Stop cancels in-flight enrichment for the given tenant.
// Safe to call even if no enrichment is running.
func (w *EnrichWorker) Stop(tenantID string) {
	if cancel, ok := w.cancelFuncs.LoadAndDelete(tenantID); ok {
		cancel.(context.CancelFunc)()
	}
	// Always finish progress — ensures UI resets even if cancelFuncs was empty.
	w.progress.Finish()
	slog.Info("vault.enrich: stopped by user", "tenant", tenantID)
}

// IsRunning returns true if enrichment is in progress for the tenant.
func (w *EnrichWorker) IsRunning(tenantID string) bool {
	if _, ok := w.cancelFuncs.Load(tenantID); ok {
		return true
	}
	return w.progress.Status().Running
}

// EnqueueUnenriched fetches documents with empty summary and emits enrichment events.
// Called after rescan when all files are unchanged but some still need enrichment.
// Returns the number of documents enqueued.
func (w *EnrichWorker) EnqueueUnenriched(ctx context.Context, tenantID, workspace string, bus eventbus.DomainEventBus, limit int) (int, error) {
	docs, err := w.vault.ListUnenrichedDocs(ctx, tenantID, limit)
	if err != nil {
		return 0, err
	}
	if len(docs) == 0 {
		return 0, nil
	}

	count := 0
	for _, doc := range docs {
		// Skip meaningless filenames — they create noise links.
		if shouldSkipEnrichment(filepath.Base(doc.Path)) {
			continue
		}
		agentID := ""
		if doc.AgentID != nil {
			agentID = *doc.AgentID
		}
		event := eventbus.DomainEvent{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Type:      eventbus.EventVaultDocUpserted,
			SourceID:  doc.ID + ":" + doc.ContentHash,
			TenantID:  tenantID,
			AgentID:   agentID,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       doc.ID,
				TenantID:    tenantID,
				AgentID:     agentID,
				Path:        doc.Path,
				ContentHash: doc.ContentHash,
				Workspace:   workspace,
			},
		}
		bus.Publish(event)
		count++
	}

	slog.Info("vault.enrich: enqueued unenriched", "tenant", tenantID, "count", count)
	return count, nil
}

// enrichTaskSiblingCap bounds the number of auto-linked siblings per
// (source_doc × task) pair. Tunable via VAULT_TASK_SIBLING_CAP env var so
// operators can raise/lower without a rebuild.
var enrichTaskSiblingCap = func() int {
	if v := os.Getenv("VAULT_TASK_SIBLING_CAP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 9
}()

// Handle is the EventBus handler for vault.doc_upserted events.
func (w *EnrichWorker) Handle(ctx context.Context, event eventbus.DomainEvent) error {
	payload, ok := event.Payload.(eventbus.VaultDocUpsertedPayload)
	if !ok {
		return nil
	}

	// Skip meaningless filenames — they create noise links.
	if shouldSkipEnrichment(filepath.Base(payload.Path)) {
		return nil
	}

	// Dedup: skip if same hash already processed.
	w.dedupMu.Lock()
	if prev, exists := w.dedup[payload.DocID]; exists && prev == payload.ContentHash {
		w.dedupMu.Unlock()
		return nil
	}
	w.dedupMu.Unlock()

	// Batch key: tenant-only. All docs for the same tenant share one queue
	// so a single processBatch goroutine drains everything in order.
	// Agent/team scope is carried in the payload for classify phase.
	key := payload.TenantID
	if !w.queue.Enqueue(key, payload) {
		return nil // another goroutine already processing this agent's queue
	}

	// Inject tenant context so store queries and bgalert scope correctly.
	if tid, parseErr := uuid.Parse(payload.TenantID); parseErr == nil {
		ctx = store.WithTenantID(ctx, tid)
	}

	// Create per-tenant cancel context for stop capability.
	cancelCtx, cancel := context.WithCancel(ctx)
	w.cancelFuncs.Store(payload.TenantID, cancel)
	w.processBatch(cancelCtx, key)
	// Clean up after batch completes naturally.
	w.cancelFuncs.Delete(payload.TenantID)
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
func (w *EnrichWorker) processBatch(ctx context.Context, key string) {
	for {
		if ctx.Err() != nil {
			w.queue.TryFinish(key)
			return
		}
		items := w.queue.Drain(key)
		if len(items) == 0 {
			if w.queue.TryFinish(key) {
				return
			}
			continue
		}

		// Process in chunks of enrichBatchSize, up to enrichMaxConcurrent in parallel.
		var wg sync.WaitGroup
		for start := 0; start < len(items); start += enrichBatchSize {
			end := min(start+enrichBatchSize, len(items))
			if err := w.sem.Acquire(ctx, 1); err != nil {
				// Context cancelled — count remaining as done so progress completes.
				w.progress.AddDone(len(items) - start)
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
			return
		}
	}
}

// processChunk runs the 4-phase enrichment pipeline for a single chunk of docs.
// Phase 1 batches all files into a single LLM call for summarization.
func (w *EnrichWorker) processChunk(ctx context.Context, items []eventbus.VaultDocUpsertedPayload) {
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

	// Resolve provider once per chunk (all items share tenantID)
	provider, model := w.resolveProviderForTenant(ctx, tenantID)
	if provider == nil {
		slog.Warn("vault.enrich: no provider available", "tenant", tenantID)
		return
	}
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
		// Preceding short-circuit: existing non-empty summary wins. This
		// protects LLM- or user-authored summaries from being clobbered by
		// the deterministic synthesizer below on subsequent rescans. Phase 6
		// idempotent embedding relies on this stability.
		if existing != nil && existing.Summary != "" {
			all = append(all, prepared{payload: item, summary: existing.Summary, title: existing.Title})
			continue
		}
		// Phase 02: deterministic pseudo-summary for media + new `document`
		// docType. Pure function — no file read, no LLM, works for any binary.
		if existing != nil && (existing.DocType == "media" || existing.DocType == "document") {
			mime, _ := existing.Metadata["mime_type"].(string)
			summary := SynthesizeMediaSummary(existing.Path, mime)
			all = append(all, prepared{
				payload: item,
				title:   existing.Title,
				summary: summary,
			})
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
		summaries := w.batchSummarize(ctx, provider, model, paths, contents)
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
	// Idempotent for media/document: if content hash and synthesized summary
	// are unchanged AND non-empty, skip the DB write + embedding call. Text
	// docs still re-embed every tick since their summary is LLM-generated.
	var embedded []enriched
	for _, r := range results {
		existing := docMap[r.payload.DocID]
		if existing != nil &&
			(existing.DocType == "media" || existing.DocType == "document") &&
			existing.ContentHash == r.payload.ContentHash &&
			existing.Summary == r.summary &&
			r.summary != "" {
			slog.Debug("vault.enrich: skip_reembed_unchanged",
				"doc", r.payload.DocID,
				"doc_type", existing.DocType)
			embedded = append(embedded, r)
			continue
		}
		if err := w.vault.UpdateSummaryAndReembed(ctx, r.payload.TenantID, r.payload.DocID, r.summary); err != nil {
			slog.Warn("vault.enrich: update_summary", "doc", r.payload.DocID, "err", err)
			continue
		}
		embedded = append(embedded, r)
	}

	// Phase 2.5 — Task-based auto-linking (deterministic, no LLM).
	// Runs BEFORE classify so the dedicated link_type `task_attachment`
	// (outside validClassifyTypes) survives DeleteDocLinksByTypes.
	// Piggybacks on docMap from Phase 0 — O(n) basename collection, one
	// batched query to BatchGetTaskSiblingsByBasenames, one batched
	// CreateLinks call. Nil-tolerant: disabled when teamStore is nil.
	w.phase25TaskLinking(ctx, embedded, docMap)

	// Phase 2.6 — Delegation-based auto-linking (deterministic, no LLM).
	// Same invariants as Phase 2.5 but keyed on metadata.delegation_id.
	w.phase26DelegationLinking(ctx, embedded, docMap)

	// Phase 3 — Classify links for this chunk.
	if len(embedded) > 0 {
		first := embedded[0].payload
		w.classifyLinks(ctx, provider, model, first.TenantID, first.AgentID, embedded)
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
func (w *EnrichWorker) batchSummarize(ctx context.Context, provider providers.Provider, model string, paths, contents []string) []string {
	var b strings.Builder
	for i := range paths {
		fmt.Fprintf(&b, "[%d] File: %s\n%s\n\n", i+1, paths[i], contents[i])
	}

	raw, err := w.chatWithRetry(ctx, provider, "vault.batch_summarize", providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: batchSummarizePrompt},
			{Role: "user", Content: b.String()},
		},
		Model:   model,
		Options: map[string]any{"max_tokens": 4096, "temperature": 0.2},
	})
	if err != nil {
		slog.Warn("vault.enrich: batch_summarize", "count", len(paths), "err", err)
		// Don't report context cancellation as enrichment error — expected during stop.
		if w.progress != nil && ctx.Err() == nil {
			w.progress.AddError(fmt.Sprintf("batch summarize failed: %v", err))
		}
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
		slog.Warn("vault.enrich: parse_batch_summaries", "err", err, "raw_len", len(raw), "raw", raw)
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
func (w *EnrichWorker) chatWithRetry(ctx context.Context, provider providers.Provider, logPrefix string, req providers.ChatRequest) (string, error) {
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
		resp, err := provider.Chat(cctx, req)
		cancel()
		if err != nil {
			lastErr = err
			slog.Warn(logPrefix+": retry", "attempt", attempt+1, "err", err)
			continue
		}
		if resp.FinishReason == "length" {
			slog.Warn(logPrefix+": truncated", "finish_reason", "length", "model", req.Model, "content_len", len(resp.Content), "max_tokens", req.Options["max_tokens"])
		}
		return strings.TrimSpace(resp.Content), nil
	}
	bgalert.ReportProviderError(ctx, w.alertDeps, "vault_enrich", lastErr)
	return "", fmt.Errorf("%s exhausted %d retries: %w", logPrefix, enrichMaxRetries, lastErr)
}

// syncWikilinks extracts [[wikilinks]] from document content and syncs them as vault links.
// Skips binary/media/document files to avoid parsing garbage data as wikilinks
// (PDFs and office docs are binary and would produce garbage [[...]] matches
// while wasting a 4MB read buffer).
func (w *EnrichWorker) syncWikilinks(ctx context.Context, p eventbus.VaultDocUpsertedPayload) {
	doc, err := w.vault.GetDocumentByID(ctx, p.TenantID, p.DocID)
	if err != nil || doc == nil {
		return
	}
	if doc.DocType == "media" || doc.DocType == "document" {
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
func (w *EnrichWorker) recordDedup(docID, hash string) {
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
