package whatsapp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/providerresolve"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultExtractPollSec = 30
	extractBatchSize      = 20
)

// ExtractionWorkerDeps bundles dependencies for the listen-only KG extraction worker.
type ExtractionWorkerDeps struct {
	RawMsgStore   store.ListenRawMessageStore
	KGStore       store.KnowledgeGraphStore
	SystemConfigs store.SystemConfigStore
	BuiltinTools  store.BuiltinToolStore
	Registry      *providers.Registry
	TenantID      uuid.UUID
	PollSec       int // poll interval in seconds (default 30)
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

	pollSec := deps.PollSec
	if pollSec <= 0 {
		pollSec = defaultExtractPollSec
	}
	pollInterval := time.Duration(pollSec) * time.Second

	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				processAllPendingBatches(deps)
			case <-stopCh:
				return
			}
		}
	}()

	slog.Info("whatsapp extraction worker: started",
		"poll_interval", pollInterval, "batch_size", extractBatchSize,
		"media_analyzer", deps.MediaAnalyzer != nil)
	return func() { close(stopCh) }
}

// processAllPendingBatches finds all (agentID, graphID) groups with pending
// messages and processes one batch per group.
func processAllPendingBatches(deps ExtractionWorkerDeps) {
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

	for _, g := range groups {
		processGroupBatch(ctx, deps, g.AgentID, g.GraphID)
	}
}

// processGroupBatch processes one batch of pending messages for a given (agentID, graphID).
func processGroupBatch(ctx context.Context, deps ExtractionWorkerDeps, agentID, graphID string) {
	msgs, err := deps.RawMsgStore.ListPending(ctx, agentID, graphID, extractBatchSize)
	if err != nil {
		slog.Warn("whatsapp extraction worker: failed to list pending messages",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	text := buildConversationTextFromRaw(msgs)
	if text == "" {
		return
	}

	// Analyze media attachments and augment the text.
	mediaSummary := mediaRefsSummary(msgs)
	if mediaSummary != "" {
		slog.Info("whatsapp extraction worker: analyzing media attachments",
			"agent_id", agentID, "graph_id", graphID, "media", mediaSummary)
		mediaDescs := analyzeMediaAttachments(ctx, msgs, deps.MediaAnalyzer)
		if len(mediaDescs) > 0 {
			var mediaText strings.Builder
			mediaText.WriteString("\n\n[Media Content Analysis]\n")
			for _, m := range msgs {
				if desc, ok := mediaDescs[m.ID]; ok {
					ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
					fmt.Fprintf(&mediaText, "\n[%s] %s:\n%s\n", ts, m.Sender, desc)
				}
			}
			mediaStr := mediaText.String()
			text += mediaStr
			slog.Info("whatsapp extraction worker: media analysis result",
				"agent_id", agentID, "graph_id", graphID,
				"media_text_len", len(mediaStr))
		}
	}

	// Resolve KG extraction provider: prefer the KG-specific provider/model from
	// builtin_tools settings (same as the main KG extraction pipeline), fall back
	// to the background provider chain.
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
		"messages", len(msgs), "text_len", len(text),
		"provider", p.Name(), "model", model,
		"provider_source", providerSource, "min_confidence", minConfidence)

	// Log the full text being sent for extraction (truncated for readability).
	if len(text) > 500 {
		slog.Debug("whatsapp extraction worker: extraction text (truncated)",
			"agent_id", agentID, "graph_id", graphID,
			"text", text[:500]+"...")
	} else {
		slog.Debug("whatsapp extraction worker: extraction text",
			"agent_id", agentID, "graph_id", graphID,
			"text", text)
	}

	// Extract entities and relations using the conversation-optimized prompt.
	extractor := knowledgegraph.NewExtractorWithPrompt(p, model, minConfidence, listenExtractSystemPrompt)
	result, err := extractor.Extract(ctx, text)
	if err != nil {
		slog.Warn("whatsapp extraction worker: extraction failed",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}
	if len(result.Entities) == 0 && len(result.Relations) == 0 {
		slog.Debug("whatsapp extraction worker: no entities extracted",
			"agent_id", agentID, "graph_id", graphID, "messages", len(msgs))
		// Still mark as processed so we don't re-process empty batches.
	}

	// Log extracted entities and relations for debugging.
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

	// Scope entities/relations to (agentID, graphID).
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

	// Fallback: for event entities without extracted event_time, derive from message batch.
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

	// Ingest into KG store.
	if len(result.Entities) > 0 || len(result.Relations) > 0 {
		entityIDs, err := deps.KGStore.IngestExtraction(ctx, agentID, graphID,
			result.Entities, result.Relations)
		if err != nil {
			slog.Warn("whatsapp extraction worker: KG ingest failed",
				"agent_id", agentID, "graph_id", graphID, "error", err)
			return
		}

		slog.Info("whatsapp extraction worker: KG extraction complete",
			"agent_id", agentID, "graph_id", graphID,
			"entities", len(result.Entities),
			"relations", len(result.Relations),
			"ingested_ids", len(entityIDs))

		// Run inline dedup on newly upserted entities (best-effort).
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

	// Mark messages as processed.
	ids := make([]uuid.UUID, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID
	}
	if err := deps.RawMsgStore.MarkProcessed(ctx, ids); err != nil {
		slog.Warn("whatsapp extraction worker: failed to mark processed",
			"agent_id", agentID, "graph_id", graphID, "error", err)
	}
}

// buildConversationTextFromRaw formats raw messages into structured text for LLM extraction.
func buildConversationTextFromRaw(msgs []store.ListenRawMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	// Group messages by chatID for multi-group context.
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
// Returns the provider, model, min confidence, and source description.
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
