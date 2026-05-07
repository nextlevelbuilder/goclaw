package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultExtractPollSec    = 30
	extractMinPollSec        = 5
	extractBatchSize         = 50
	extractMaxConcurrent     = 3
	extractBacklogThreshold  = 100
)

type extractConfig struct {
	batchSize        int
	maxConcurrent    int
	defaultPollSec   int
	minPollSec       int
	backlogThreshold int
}

func defaultExtractConfig() extractConfig {
	return extractConfig{
		batchSize:        extractBatchSize,
		maxConcurrent:    extractMaxConcurrent,
		defaultPollSec:   defaultExtractPollSec,
		minPollSec:       extractMinPollSec,
		backlogThreshold: extractBacklogThreshold,
	}
}

func loadExtractConfig(ctx context.Context, sysCfg store.SystemConfigStore) extractConfig {
	cfg := defaultExtractConfig()
	if sysCfg == nil {
		return cfg
	}
	configs, err := sysCfg.List(ctx)
	if err != nil {
		return cfg
	}
	if v := configs["listen.extract.batch_size"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.batchSize = n
		}
	}
	if v := configs["listen.extract.max_concurrent"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxConcurrent = n
		}
	}
	if v := configs["listen.extract.poll_sec"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.defaultPollSec = n
		}
	}
	if v := configs["listen.extract.min_poll_sec"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.minPollSec = n
		}
	}
	if v := configs["listen.extract.backlog_threshold"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.backlogThreshold = n
		}
	}
	return cfg
}

// ExtractionWorkerDeps bundles dependencies for the listen-only KG extraction worker.
type ExtractionWorkerDeps struct {
	RawMsgStore   store.ListenRawMessageStore
	KGStore       store.KnowledgeGraphStore
	SystemConfigs store.SystemConfigStore
	BuiltinTools  store.BuiltinToolStore
	Registry      *providers.Registry
	TenantID      uuid.UUID
	PollSec       int
	MediaAnalyzer *MediaAnalyzer
}

// RegisterExtractionWorker starts a background goroutine that periodically polls
// listen_raw_messages for unprocessed batches and runs KG extraction.
// Returns a cleanup function that stops the worker.
func RegisterExtractionWorker(deps ExtractionWorkerDeps) func() {
	if deps.RawMsgStore == nil || deps.KGStore == nil {
		slog.Info("whatsapp extraction worker: skipped, missing stores")
		return func() {}
	}

	basePollSec := deps.PollSec
	if basePollSec <= 0 {
		basePollSec = defaultExtractPollSec
	}

	stopCh := make(chan struct{})
	go func() {
		for {
			ctx := store.WithTenantID(context.Background(), deps.TenantID)
			cfg := loadExtractConfig(ctx, deps.SystemConfigs)
			if basePollSec > 0 {
				cfg.defaultPollSec = basePollSec
			}

			processAllPendingBatches(deps, cfg)
			retryAbandonedMessages(ctx, deps, cfg)

			pending := pendingExtractionCount(ctx, deps)
			nextInterval := time.Duration(cfg.defaultPollSec) * time.Second
			if pending > cfg.backlogThreshold {
				nextInterval = time.Duration(cfg.minPollSec) * time.Second
			}

			select {
			case <-time.After(nextInterval):
			case <-stopCh:
				return
			}
		}
	}()

	slog.Info("whatsapp extraction worker: started",
		"poll_interval", fmt.Sprintf("%ds/%ds", basePollSec, extractMinPollSec),
		"batch_size", extractBatchSize, "max_concurrent", extractMaxConcurrent,
		"media_analyzer", deps.MediaAnalyzer != nil)
	return func() { close(stopCh) }
}

func pendingExtractionCount(ctx context.Context, deps ExtractionWorkerDeps) int {
	stats, err := deps.RawMsgStore.ExtractionStats(ctx)
	if err != nil {
		return 0
	}
	return stats[store.ExtractionStatusPending] + stats[store.ExtractionStatusFailed]
}

// processAllPendingBatches finds all (agentID, graphID) groups with pending
// messages and processes one batch per group concurrently.
func processAllPendingBatches(deps ExtractionWorkerDeps, cfg extractConfig) {
	ctx := store.WithTenantID(context.Background(), deps.TenantID)

	groups, err := deps.RawMsgStore.ListPendingGroups(ctx)
	if err != nil {
		slog.Warn("whatsapp extraction worker: failed to list pending groups", "error", err)
		return
	}
	if len(groups) == 0 {
		return
	}

	slog.Debug("whatsapp extraction worker: processing groups", "count", len(groups))

	sem := semaphore.NewWeighted(int64(cfg.maxConcurrent))
	var wg sync.WaitGroup

	for _, g := range groups {
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}
		wg.Add(1)
		go func(agentID, graphID string) {
			defer wg.Done()
			defer sem.Release(1)
			processGroupBatch(ctx, deps, agentID, graphID, cfg.batchSize)
		}(g.AgentID, g.GraphID)
	}
	wg.Wait()
}

// processGroupBatch processes one batch of pending messages for a given (agentID, graphID).
func processGroupBatch(ctx context.Context, deps ExtractionWorkerDeps, agentID, graphID string, batchSize int) {
	msgs, err := deps.RawMsgStore.ListPending(ctx, agentID, graphID, batchSize)
	if err != nil {
		slog.Warn("whatsapp extraction worker: failed to list pending messages",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	var p providers.Provider
	var model string
	var minConfidence float64 = 0.75
	var providerSource string

	if deps.BuiltinTools != nil {
		p, model, minConfidence, providerSource = resolveKGProvider(ctx, deps)
	}

	if p == nil {
		p, model = providerresolve.ResolveBackgroundProvider(ctx, deps.TenantID, deps.Registry, deps.SystemConfigs)
		if p != nil {
			providerSource = "background"
		}
	}

	if p == nil {
		slog.Warn("whatsapp extraction worker: no LLM provider available",
			"agent_id", agentID, "graph_id", graphID)
		return
	}

	slog.Info("whatsapp extraction worker: extracting KG from batch",
		"agent_id", agentID, "graph_id", graphID,
		"messages", len(msgs),
		"provider", p.Name(), "model", model,
		"provider_source", providerSource, "min_confidence", minConfidence)

	fullText := buildConversationTextFromRaw(msgs)
	if fullText == "" {
		ids := rawMsgIDs(msgs)
		if err := deps.RawMsgStore.MarkProcessed(ctx, ids); err != nil {
			slog.Warn("whatsapp extraction worker: failed to mark empty-body batch",
				"agent_id", agentID, "graph_id", graphID, "error", err)
		}
		return
	}

	dateGroups := groupMessagesByDate(msgs)
	var combinedSummary strings.Builder
	summarizeOK := true

	for _, date := range dateGroups.order {
		dayMsgs := dateGroups.groups[date]
		dayText := buildConversationTextFromRaw(dayMsgs)
		if dayText == "" {
			continue
		}

		dayText = appendMediaAnalysis(ctx, deps, dayMsgs, dayText, agentID, graphID)

		summary, err := summarizeConversation(ctx, p, model, dayText)
		if err != nil {
			slog.Warn("whatsapp extraction worker: summarization failed for date, falling back to raw text",
				"agent_id", agentID, "graph_id", graphID, "date", date, "error", err)
			summarizeOK = false
			break
		}

		if combinedSummary.Len() > 0 {
			combinedSummary.WriteString("\n\n")
		}
		fmt.Fprintf(&combinedSummary, "== %s ==\n%s", date, summary)

		if knowledgegraph.VerboseLogging() {
			preview := summary
			if len(preview) > 300 {
				preview = preview[:300] + "..."
			}
			slog.Info("whatsapp extraction worker: summarized date",
				"agent_id", agentID, "graph_id", graphID,
				"date", date, "raw_len", len(dayText), "summary_len", len(summary), "summary", preview)
		}
	}

	if !summarizeOK {
		fullText = appendMediaAnalysis(ctx, deps, msgs, fullText, agentID, graphID)
		extractor := knowledgegraph.NewExtractorWithPrompt(p, model, minConfidence, listenExtractSystemPrompt)
		result, err := extractor.Extract(ctx, fullText)
		if err != nil {
			slog.Warn("whatsapp extraction worker: extraction failed",
				"agent_id", agentID, "graph_id", graphID, "error", err)
			markMsgsFailed(ctx, deps, msgs, err)
			return
		}
		ingestAndFinalize(ctx, deps, result, agentID, graphID, msgs)
		return
	}

	extractionText := combinedSummary.String()
	extractor := knowledgegraph.NewExtractor(p, model, minConfidence)
	result, err := extractor.Extract(ctx, extractionText)
	if err != nil {
		slog.Warn("whatsapp extraction worker: extraction failed",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		markMsgsFailed(ctx, deps, msgs, err)
		return
	}

	ingestAndFinalize(ctx, deps, result, agentID, graphID, msgs)
}

// retryAbandonedMessages finds messages stuck in 'failed' state with max attempts
// and resets them back to 'pending' for reprocessing.
func retryAbandonedMessages(ctx context.Context, deps ExtractionWorkerDeps, cfg extractConfig) {
	groups, err := deps.RawMsgStore.ListAbandonedGroups(ctx)
	if err != nil || len(groups) == 0 {
		return
	}
	for _, g := range groups {
		ids, err := deps.RawMsgStore.ListAbandonedIDs(ctx, g.AgentID, g.GraphID, cfg.batchSize)
		if err != nil || len(ids) == 0 {
			continue
		}
		slog.Info("whatsapp extraction worker: retrying abandoned messages",
			"agent_id", g.AgentID, "graph_id", g.GraphID, "count", len(ids))
		if _, err := deps.RawMsgStore.ResetProcessedByIDs(ctx, ids); err != nil {
			slog.Warn("whatsapp extraction worker: failed to reset abandoned messages",
				"agent_id", g.AgentID, "graph_id", g.GraphID, "error", err)
		}
	}
}

// rawMsgIDs extracts UUIDs from a slice of raw messages.
func rawMsgIDs(msgs []store.ListenRawMessage) []uuid.UUID {
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	return ids
}

// markMsgsFailed marks messages as failed with the given error for retry tracking.
func markMsgsFailed(ctx context.Context, deps ExtractionWorkerDeps, msgs []store.ListenRawMessage, extractErr error) {
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	errMsg := extractErr.Error()
	if len(errMsg) > 500 {
		errMsg = errMsg[:500]
	}
	if markErr := deps.RawMsgStore.MarkFailed(ctx, ids, errMsg); markErr != nil {
		slog.Warn("whatsapp extraction worker: failed to mark messages as failed", "error", markErr)
	}
}

// ingestAndFinalize handles entity scoping, KG ingestion, dedup, and marking messages as processed.
func ingestAndFinalize(ctx context.Context, deps ExtractionWorkerDeps, result *knowledgegraph.ExtractionResult, agentID, graphID string, msgs []store.ListenRawMessage) {
	if len(result.Entities) == 0 && len(result.Relations) == 0 {
		slog.Debug("whatsapp extraction worker: no entities extracted",
			"agent_id", agentID, "graph_id", graphID, "messages", len(msgs))
	} else {
		for i, e := range result.Entities {
			slog.Debug("whatsapp extraction worker: extracted entity",
				"agent_id", agentID, "graph_id", graphID,
				"idx", i, "name", e.Name, "type", e.EntityType, "confidence", fmt.Sprintf("%.2f", e.Confidence))
		}
		for i, r := range result.Relations {
			slog.Debug("whatsapp extraction worker: extracted relation",
				"agent_id", agentID, "graph_id", graphID,
				"idx", i, "source", r.SourceEntityID, "target", r.TargetEntityID, "type", r.RelationType)
		}
	}

	now := time.Now().UTC()
	for i := range result.Entities {
		result.Entities[i].AgentID = agentID
		result.Entities[i].UserID = graphID
		result.Entities[i].ValidFrom = &now
	}
	for i := range result.Relations {
		result.Relations[i].AgentID = agentID
		result.Relations[i].UserID = graphID
		result.Relations[i].ValidFrom = &now
	}

	for i := range result.Entities {
		if result.Entities[i].EntityType == "event" && result.Entities[i].EventTime == nil && len(msgs) > 0 {
			earliest := msgs[0].MsgTimestamp
			for _, m := range msgs[1:] {
				if m.MsgTimestamp.Before(earliest) {
					earliest = m.MsgTimestamp
				}
			}
			result.Entities[i].EventTime = &earliest
		}
	}

	if len(result.Entities) > 0 || len(result.Relations) > 0 {
		entityIDs, err := deps.KGStore.IngestExtraction(ctx, agentID, graphID,
			result.Entities, result.Relations)
		if err != nil {
			slog.Warn("whatsapp extraction worker: KG ingest failed",
				"agent_id", agentID, "graph_id", graphID, "error", err)
			markMsgsFailed(ctx, deps, msgs, err)
			return
		}
		slog.Info("whatsapp extraction worker: KG extraction complete",
			"agent_id", agentID, "graph_id", graphID,
			"entities", len(result.Entities),
			"relations", len(result.Relations),
			"ingested_ids", len(entityIDs))

		if len(entityIDs) > 0 {
			if merged, flagged, dedupErr := deps.KGStore.DedupAfterExtraction(ctx, agentID, graphID, entityIDs); dedupErr != nil {
				slog.Debug("whatsapp extraction worker: dedup failed",
					"agent_id", agentID, "graph_id", graphID, "error", dedupErr)
			} else if merged > 0 || flagged > 0 {
				slog.Info("whatsapp extraction worker: dedup results",
					"agent_id", agentID, "graph_id", graphID,
					"merged", merged, "flagged", flagged)
			}
		}
	}

	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := deps.RawMsgStore.MarkProcessed(ctx, ids); err != nil {
		slog.Warn("whatsapp extraction worker: failed to mark processed",
			"agent_id", agentID, "graph_id", graphID, "error", err)
	}
}

// summarizeConversation calls the LLM to summarize raw WhatsApp text into polished narrative
// while preserving specific details (names, IDs, timestamps, structured data).
func summarizeConversation(ctx context.Context, p providers.Provider, model, text string) (string, error) {
	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: listenSummarizePrompt},
			{Role: "user", Content: text},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  2048,
			"temperature": 0.3,
		},
	}

	resp, err := p.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarize conversation: %w", err)
	}
	return strings.TrimSpace(resp.Content), nil
}

// dateGroups holds messages grouped by date string with insertion order preserved.
type dateGroups struct {
	order  []string
	groups map[string][]store.ListenRawMessage
}

// groupMessagesByDate splits messages into groups keyed by their local date (YYYY-MM-DD).
func groupMessagesByDate(msgs []store.ListenRawMessage) dateGroups {
	dg := dateGroups{groups: make(map[string][]store.ListenRawMessage)}
	for _, m := range msgs {
		date := m.MsgTimestamp.Format("2006-01-02")
		if _, exists := dg.groups[date]; !exists {
			dg.order = append(dg.order, date)
		}
		dg.groups[date] = append(dg.groups[date], m)
	}
	return dg
}

// appendMediaAnalysis analyzes media attachments for the given messages and appends
// descriptions to the text. Returns text unchanged if no media or no analyzer.
func appendMediaAnalysis(ctx context.Context, deps ExtractionWorkerDeps, msgs []store.ListenRawMessage, text, agentID, graphID string) string {
	mediaSummary := mediaRefsSummary(msgs)
	if mediaSummary == "" {
		return text
	}
	slog.Info("whatsapp extraction worker: analyzing media attachments",
		"agent_id", agentID, "graph_id", graphID, "media", mediaSummary)
	mediaDescs := analyzeMediaAttachments(ctx, msgs, deps.MediaAnalyzer)
	if len(mediaDescs) == 0 {
		return text
	}
	var mediaText strings.Builder
	mediaText.WriteString("\n\n[Media Content Analysis]\n")
	for _, m := range msgs {
		if desc, ok := mediaDescs[m.ID]; ok {
			ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
			fmt.Fprintf(&mediaText, "\n[%s] %s:\n%s\n", ts, m.Sender, desc)
		}
	}
	mediaStr := mediaText.String()
	slog.Info("whatsapp extraction worker: media analysis result",
		"agent_id", agentID, "graph_id", graphID,
		"media_text_len", len(mediaStr))
	return text + mediaStr
}

// buildConversationTextFromRaw formats raw messages into structured text for LLM extraction.
func buildConversationTextFromRaw(msgs []store.ListenRawMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	grouped := make(map[string][]store.ListenRawMessage)
	var order []string
	for _, m := range msgs {
		if _, ok := grouped[m.ChatID]; !ok {
			order = append(order, m.ChatID)
		}
		grouped[m.ChatID] = append(grouped[m.ChatID], m)
	}

	var b strings.Builder
	for i, chatID := range order {
		msgs := grouped[chatID]
		if i > 0 {
			b.WriteString("\n\n")
		}
		chatName := msgs[0].ChatName
		if chatName == "" {
			chatName = chatID
		}
		fmt.Fprintf(&b, "[Messages from WhatsApp: %s (%s)]\n", chatName, chatID)
		for _, m := range msgs {
			ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
			fmt.Fprintf(&b, "\n[%s] %s:\n%s\n", ts, m.Sender, m.Body)
		}
	}
	return b.String()
}

// kgExtractionSettings mirrors the builtin_tools knowledge_graph_search settings JSON.
type kgExtractionSettings struct {
	ExtractionProvider string  `json:"extraction_provider"`
	ExtractionModel    string  `json:"extraction_model"`
	MinConfidence      float64 `json:"min_confidence"`
}

// resolveKGProvider reads KG extraction provider/model from builtin_tools settings.
func resolveKGProvider(ctx context.Context, deps ExtractionWorkerDeps) (providers.Provider, string, float64, string) {
	raw, err := deps.BuiltinTools.GetSettings(ctx, "knowledge_graph_search")
	if err != nil || raw == nil {
		slog.Debug("whatsapp extraction worker: no KG settings in builtin_tools", "error", err)
		return nil, "", 0.75, ""
	}
	var settings kgExtractionSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		slog.Debug("whatsapp extraction worker: invalid KG settings", "error", err)
		return nil, "", 0.75, ""
	}
	if settings.ExtractionProvider == "" {
		return nil, "", 0.75, ""
	}
	p, err := deps.Registry.Get(ctx, settings.ExtractionProvider)
	if err != nil || p == nil {
		slog.Warn("whatsapp extraction worker: KG provider not found",
			"provider", settings.ExtractionProvider, "error", err)
		return nil, "", 0.75, ""
	}
	model := settings.ExtractionModel
	if model == "" {
		model = p.DefaultModel()
	}
	minConf := settings.MinConfidence
	if minConf <= 0 {
		minConf = 0.75
	}
	return p, model, minConf, "kg_settings"
}
