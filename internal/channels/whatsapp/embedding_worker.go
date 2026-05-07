package whatsapp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	defaultEmbeddingPollSec = 30
	embeddingBatchSize      = 50
	embeddingChunkSize      = 1000
	embeddingChunkOverlap   = 200
	embeddingEmbBatch       = 50
)

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

	pollSec := deps.PollSec
	if pollSec <= 0 {
		pollSec = defaultEmbeddingPollSec
	}
	pollInterval := time.Duration(pollSec) * time.Second

	stopCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				processAllEmbeddingBatches(deps)
			case <-stopCh:
				return
			}
		}
	}()

	slog.Info("whatsapp embedding worker: started",
		"poll_interval", pollInterval, "batch_size", embeddingBatchSize)
	return func() { close(stopCh) }
}

func processAllEmbeddingBatches(deps EmbeddingWorkerDeps) {
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

	for _, g := range groups {
		processEmbeddingGroupBatch(ctx, deps, g.AgentID, g.GraphID)
	}
}

func processEmbeddingGroupBatch(ctx context.Context, deps EmbeddingWorkerDeps, agentID, graphID string) {
	msgs, err := deps.RawMsgStore.ListPendingEmbeddings(ctx, agentID, graphID, embeddingBatchSize)
	if err != nil {
		slog.Warn("whatsapp embedding worker: failed to list pending embeddings",
			"agent_id", agentID, "graph_id", graphID, "error", err)
		return
	}
	if len(msgs) == 0 {
		return
	}

	// Get embedding provider from the PG chunk store
	provider, hasProvider := getEmbeddingProvider(deps.ChunkStore)
	if !hasProvider {
		slog.Warn("whatsapp embedding worker: no embedding provider, skipping batch",
			"agent_id", agentID, "graph_id", graphID)
		return
	}

	chatGroups := groupRawMsgsByChat(msgs)

	var allChunks []store.RawMessageChunk
	var allTexts []string
	var processedIDs []uuid.UUID

	for _, chatID := range chatGroups.order {
		chatMsgs := chatGroups.groups[chatID]
		chatText := buildEmbeddingTextFromRaw(chatMsgs)
		if strings.TrimSpace(chatText) == "" {
			continue
		}

		chunks := memory.ChunkText(chatText, embeddingChunkSize, embeddingChunkOverlap)
		for ci, chunk := range chunks {
			if strings.TrimSpace(chunk.Text) == "" {
				continue
			}

			timeFrom, timeTo := chunkTimeRange(chatMsgs)
			senders := uniqueSenders(chatMsgs)
			msgIDs := chunkSourceIDs(chatMsgs)

			allChunks = append(allChunks, store.RawMessageChunk{
				AgentID:      agentID,
				GraphID:      graphID,
				ChatID:       chatMsgs[0].ChatID,
				ChatName:     chatMsgs[0].ChatName,
				Sender:       senders,
				SenderID:     chatMsgs[0].SenderID,
				MsgTimeFrom:  timeFrom,
				MsgTimeTo:    timeTo,
				ChunkIndex:   ci,
				Text:         chunk.Text,
				ContentHash:  memory.ContentHash(chunk.Text),
				SourceMsgIDs: msgIDs,
			})
			allTexts = append(allTexts, chunk.Text)
		}

		for _, m := range chatMsgs {
			processedIDs = append(processedIDs, m.ID)
		}
	}

	if len(allChunks) == 0 {
		slog.Debug("whatsapp embedding worker: no chunks produced",
			"agent_id", agentID, "graph_id", graphID)
		// Still mark as embedded to avoid reprocessing empty batches
		_ = deps.RawMsgStore.MarkEmbedded(ctx, processedIDs)
		return
	}

	// Embed in batches
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
