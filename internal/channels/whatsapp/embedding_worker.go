package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/semaphore"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultEmbeddingPollSec   = 30
	embeddingMinPollSec       = 5
	embeddingBatchSize        = 100
	embeddingEmbBatch         = 50
	embeddingMaxConcurrent    = 3
	embeddingBacklogThreshold = 100
)

type embeddingConfig struct {
	batchSize        int
	maxConcurrent    int
	defaultPollSec   int
	minPollSec       int
	backlogThreshold int
	maxChunkLen      int // 0 = evaluate from messages + model
	chunkOverlap     int // 0 = evaluate from messages + model
}

func defaultEmbeddingConfig() embeddingConfig {
	return embeddingConfig{
		batchSize:        embeddingBatchSize,
		maxConcurrent:    embeddingMaxConcurrent,
		defaultPollSec:   defaultEmbeddingPollSec,
		minPollSec:       embeddingMinPollSec,
		backlogThreshold: embeddingBacklogThreshold,
	}
}

func loadEmbeddingConfig(ctx context.Context, sysCfg store.SystemConfigStore) embeddingConfig {
	cfg := defaultEmbeddingConfig()
	if sysCfg == nil {
		return cfg
	}
	configs, err := sysCfg.List(ctx)
	if err != nil {
		return cfg
	}
	if v := configs["listen.embedding.batch_size"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.batchSize = n
		}
	}
	if v := configs["listen.embedding.max_concurrent"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxConcurrent = n
		}
	}
	if v := configs["listen.embedding.poll_sec"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.defaultPollSec = n
		}
	}
	if v := configs["listen.embedding.min_poll_sec"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.minPollSec = n
		}
	}
	if v := configs["listen.embedding.backlog_threshold"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.backlogThreshold = n
		}
	}
	if v := configs["listen.embedding.chunk_size"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxChunkLen = n
		}
	}
	if v := configs["listen.embedding.chunk_overlap"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.chunkOverlap = n
		}
	}
	return cfg
}

// EmbeddingWorkerDeps bundles dependencies for the raw message embedding worker.
type EmbeddingWorkerDeps struct {
	RawMsgStore   store.ListenRawMessageStore
	ChunkStore    store.RawMessageChunkStore
	SystemConfigs store.SystemConfigStore
	TenantID      uuid.UUID
	PollSec       int
}

// RegisterEmbeddingWorker starts a background goroutine that periodically polls
// listen_raw_messages for messages not yet embedded, chunks them, generates embeddings,
// and stores in raw_message_chunks.
func RegisterEmbeddingWorker(deps EmbeddingWorkerDeps) func() {
	if deps.RawMsgStore == nil || deps.ChunkStore == nil {
		slog.Info("whatsapp embedding worker: skipped, missing stores")
		return func() {}
	}

	basePollSec := deps.PollSec
	if basePollSec <= 0 {
		basePollSec = defaultEmbeddingPollSec
	}

	stopCh := make(chan struct{})
	go func() {
		for {
			ctx := store.WithTenantID(context.Background(), deps.TenantID)
			cfg := loadEmbeddingConfig(ctx, deps.SystemConfigs)
			if basePollSec > 0 {
				cfg.defaultPollSec = basePollSec
			}

			processAllEmbeddingBatches(deps, cfg)

			pending, _, _ := deps.RawMsgStore.EmbeddingStats(ctx)
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

	slog.Info("whatsapp embedding worker: started",
		"poll_interval", fmt.Sprintf("%ds/%ds", basePollSec, embeddingMinPollSec),
		"batch_size", embeddingBatchSize, "max_concurrent", embeddingMaxConcurrent,
		"chunk_eval", "auto")
	return func() { close(stopCh) }
}

func processAllEmbeddingBatches(deps EmbeddingWorkerDeps, cfg embeddingConfig) {
	ctx := store.WithTenantID(context.Background(), deps.TenantID)

	groups, err := deps.RawMsgStore.ListPendingEmbeddingGroups(ctx)
	if err != nil {
		slog.Warn("whatsapp embedding worker: failed to list pending groups", "error", err)
		return
	}
	if len(groups) == 0 {
		return
	}

	slog.Debug("whatsapp embedding worker: processing groups", "count", len(groups))

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
			processEmbeddingGroupBatch(ctx, deps, agentID, graphID, cfg)
		}(g.AgentID, g.GraphID)
	}
	wg.Wait()
}

func processEmbeddingGroupBatch(ctx context.Context, deps EmbeddingWorkerDeps, agentID, graphID string, cfg embeddingConfig) {
	msgs, err := deps.RawMsgStore.ListPendingEmbeddings(ctx, agentID, graphID, cfg.batchSize)
	if err != nil {
		slog.Warn("whatsapp embedding worker: failed to list pending embeddings",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	provider, hasProvider := getEmbeddingProvider(deps.ChunkStore)
	if !hasProvider {
		slog.Warn("whatsapp embedding worker: no embedding provider, skipping batch",
			"agent_id", agentID, "graph_id", graphID)
		return
	}

	// Resolve chunk params: system_configs override > evaluator from actual messages
	maxChunkLen := cfg.maxChunkLen
	chunkOverlap := cfg.chunkOverlap
	if maxChunkLen == 0 || chunkOverlap == 0 {
		bodies := make([]string, len(msgs))
		for i, m := range msgs {
			bodies[i] = m.Body
		}
		stats := memory.EvaluateRawMessages(bodies)
		evalLen, evalOverlap := memory.EvaluateChunkParams(provider.Model(), stats)
		if maxChunkLen == 0 {
			maxChunkLen = evalLen
		}
		if chunkOverlap == 0 {
			chunkOverlap = evalOverlap
		}
		slog.Debug("whatsapp embedding worker: evaluated chunk params",
			"model", provider.Model(), "max_chunk_len", maxChunkLen,
			"chunk_overlap", chunkOverlap, "msg_count", stats.TotalMsgs,
			"avg_body_len", stats.AvgBodyLen, "p95_body_len", stats.P95BodyLen)
	}

	chatGroups := groupRawMsgsByChat(msgs)

	var allChunks []store.RawMessageChunk
	var allTexts []string
	var processedIDs []uuid.UUID

	for _, chatID := range chatGroups.order {
		chatMsgs := chatGroups.groups[chatID]
		dayGroups := groupRawMsgsByDay(chatMsgs)

		for _, dayKey := range dayGroups.order {
			dayMsgs := dayGroups.groups[dayKey]
			dayText := buildEmbeddingTextFromRaw(dayMsgs)
			if strings.TrimSpace(dayText) == "" {
				continue
			}

			chunks := memory.ChunkText(dayText, maxChunkLen, chunkOverlap)
			for ci, chunk := range chunks {
				if strings.TrimSpace(chunk.Text) == "" {
					continue
				}

				timeFrom, timeTo := chunkTimeRange(dayMsgs)
				senders := uniqueSenders(dayMsgs)
				msgIDs := chunkSourceIDs(dayMsgs)

				allChunks = append(allChunks, store.RawMessageChunk{
					AgentID:      agentID,
					GraphID:      graphID,
					ChatID:       chatMsgs[0].ChatID,
					ChatName:     chatMsgs[0].ChatName,
					Sender:       senders,
					SenderID:     dayMsgs[0].SenderID,
					MsgTimeFrom:  timeFrom,
					MsgTimeTo:    timeTo,
					ChunkIndex:   ci,
					Text:         chunk.Text,
					ContentHash:  memory.ContentHash(chunk.Text),
					SourceMsgIDs: msgIDs,
				})
				allTexts = append(allTexts, chunk.Text)
			}

			for _, m := range dayMsgs {
				processedIDs = append(processedIDs, m.ID)
			}
		}
	}

	if len(allChunks) == 0 {
		slog.Debug("whatsapp embedding worker: no chunks produced",
			"agent_id", agentID, "graph_id", graphID)
		_ = deps.RawMsgStore.MarkEmbedded(ctx, processedIDs)
		return
	}

	var embeddings [][]float32
	for start := 0; start < len(allTexts); start += embeddingEmbBatch {
		end := min(start+embeddingEmbBatch, len(allTexts))
		batch := allTexts[start:end]

		batchEmb, embErr := provider.Embed(ctx, batch)
		if embErr != nil {
			slog.Warn("whatsapp embedding worker: embedding batch failed",
				"agent_id", agentID, "graph_id", graphID,
				"batch_start", start, "batch_size", len(batch), "error", embErr)
			for range batch {
				embeddings = append(embeddings, nil)
			}
			continue
		}
		embeddings = append(embeddings, batchEmb...)
	}

	if err := deps.ChunkStore.StoreChunks(ctx, allChunks, embeddings); err != nil {
		slog.Warn("whatsapp embedding worker: store chunks failed",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}

	if err := deps.RawMsgStore.MarkEmbedded(ctx, processedIDs); err != nil {
		slog.Warn("whatsapp embedding worker: failed to mark embedded",
			"agent_id", agentID, "graph_id", graphID, "error", err)
	}

	slog.Info("whatsapp embedding worker: batch complete",
		"agent_id", agentID, "graph_id", graphID,
		"messages", len(msgs), "chunks", len(allChunks))
}

type providerGetter interface {
	GetEmbeddingProvider() (store.EmbeddingProvider, bool)
}

func getEmbeddingProvider(chunkStore store.RawMessageChunkStore) (store.EmbeddingProvider, bool) {
	if pg, ok := chunkStore.(providerGetter); ok {
		return pg.GetEmbeddingProvider()
	}
	return nil, false
}

type chatGroupOrder struct {
	order  []string
	groups map[string][]store.ListenRawMessage
}

func groupRawMsgsByChat(msgs []store.ListenRawMessage) chatGroupOrder {
	cg := chatGroupOrder{groups: make(map[string][]store.ListenRawMessage)}
	for _, m := range msgs {
		if _, ok := cg.groups[m.ChatID]; !ok {
			cg.order = append(cg.order, m.ChatID)
		}
		cg.groups[m.ChatID] = append(cg.groups[m.ChatID], m)
	}
	return cg
}

type dayGroupOrder struct {
	order  []string
	groups map[string][]store.ListenRawMessage
}

func groupRawMsgsByDay(msgs []store.ListenRawMessage) dayGroupOrder {
	dg := dayGroupOrder{groups: make(map[string][]store.ListenRawMessage)}
	for _, m := range msgs {
		dayKey := m.MsgTimestamp.Format("2006-01-02")
		if _, ok := dg.groups[dayKey]; !ok {
			dg.order = append(dg.order, dayKey)
		}
		dg.groups[dayKey] = append(dg.groups[dayKey], m)
	}
	return dg
}

func buildEmbeddingTextFromRaw(msgs []store.ListenRawMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		ts := m.MsgTimestamp.Format("2006-01-02 15:04:05")
		fmt.Fprintf(&b, "[%s] %s: %s\n", ts, m.Sender, m.Body)
	}
	return b.String()
}

func chunkTimeRange(msgs []store.ListenRawMessage) (time.Time, time.Time) {
	if len(msgs) == 0 {
		return time.Time{}, time.Time{}
	}
	return msgs[0].MsgTimestamp, msgs[len(msgs)-1].MsgTimestamp
}

func uniqueSenders(msgs []store.ListenRawMessage) string {
	seen := make(map[string]bool)
	var senders []string
	for _, m := range msgs {
		if !seen[m.Sender] {
			seen[m.Sender] = true
			senders = append(senders, m.Sender)
		}
	}
	return strings.Join(senders, ", ")
}

func chunkSourceIDs(msgs []store.ListenRawMessage) []string {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.ID.String()
	}
	return ids
}
