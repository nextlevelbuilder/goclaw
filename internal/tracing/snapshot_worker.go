package tracing

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SnapshotWorker periodically aggregates trace/span data into usage_snapshots.
type SnapshotWorker struct {
	db        *sql.DB
	snapshots store.SnapshotStore
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func NewSnapshotWorker(db *sql.DB, snapshots store.SnapshotStore) *SnapshotWorker {
	return &SnapshotWorker{
		db:        db,
		snapshots: snapshots,
		stopCh:    make(chan struct{}),
	}
}

// Start launches the background aggregation loop.
func (w *SnapshotWorker) Start() {
	w.wg.Add(1)
	go w.loop()
	slog.Info("snapshot worker started")
}

// Stop signals the worker to stop and waits for completion.
func (w *SnapshotWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	slog.Info("snapshot worker stopped")
}

func (w *SnapshotWorker) loop() {
	defer w.wg.Done()

	// On startup, catch up any missed hours
	w.catchUp()

	// Tick at HH:05:00 UTC (5 min past the hour)
	now := time.Now().UTC()
	nextTick := now.Truncate(time.Hour).Add(time.Hour).Add(5 * time.Minute)
	if now.After(nextTick) {
		nextTick = nextTick.Add(time.Hour)
	}
	timer := time.NewTimer(time.Until(nextTick))
	defer timer.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-timer.C:
			w.catchUp()
			// Reset for next hour
			nextTick = nextTick.Add(time.Hour)
			timer.Reset(time.Until(nextTick))
		}
	}
}

// catchUp computes snapshots for all missed hours between latest bucket and current hour.
func (w *SnapshotWorker) catchUp() {
	ctx := context.Background()
	now := time.Now().UTC()
	targetHour := now.Truncate(time.Hour).Add(-time.Hour) // previous complete hour

	latest, err := w.snapshots.GetLatestBucket(ctx)
	if err != nil {
		slog.Warn("snapshot: get latest bucket", "error", err)
		return
	}

	var startHour time.Time
	if latest == nil {
		// No snapshots yet — only compute the previous hour (backfill handles history)
		startHour = targetHour
	} else {
		startHour = latest.Add(time.Hour)
	}

	for h := startHour; !h.After(targetHour); h = h.Add(time.Hour) {
		start := time.Now()
		if err := w.aggregateHour(ctx, h); err != nil {
			slog.Warn("snapshot: aggregate hour failed", "hour", h.Format(time.RFC3339), "error", err)
			return // stop catch-up on error, will retry next tick
		}
		slog.Info("snapshot computed", "hour", h.Format(time.RFC3339), "duration_ms", time.Since(start).Milliseconds())
	}
}

// Backfill populates usage_snapshots from historical trace/span data.
// Returns the number of hours processed.
func (w *SnapshotWorker) Backfill(ctx context.Context) (int, error) {
	latest, err := w.snapshots.GetLatestBucket(ctx)
	if err != nil {
		return 0, fmt.Errorf("get latest bucket: %w", err)
	}

	// Find earliest root trace
	var earliest sql.NullTime
	err = w.db.QueryRowContext(ctx,
		`SELECT MIN(start_time) FROM traces WHERE parent_trace_id IS NULL`,
	).Scan(&earliest)
	if err != nil || !earliest.Valid {
		return 0, nil // no traces to backfill
	}

	startHour := earliest.Time.UTC().Truncate(time.Hour)
	if latest != nil {
		startHour = latest.Add(time.Hour)
	}

	endHour := time.Now().UTC().Truncate(time.Hour)
	count := 0
	for h := startHour; h.Before(endHour); h = h.Add(time.Hour) {
		if err := w.aggregateHour(ctx, h); err != nil {
			slog.Warn("backfill: aggregate hour failed", "hour", h.Format(time.RFC3339), "error", err)
			continue
		}
		count++
	}
	return count, nil
}

func (w *SnapshotWorker) aggregateHour(ctx context.Context, bucketStart time.Time) error {
	bucketEnd := bucketStart.Add(time.Hour)

	// Query 1: trace-level metrics by (agent_id, channel)
	traceRows, err := queryTraceAggregates(ctx, w.db, bucketStart, bucketEnd)
	if err != nil {
		return fmt.Errorf("trace aggregates: %w", err)
	}

	// Query 2: span-level metrics by (agent_id, channel, provider, model)
	spanRows, err := querySpanAggregates(ctx, w.db, bucketStart, bucketEnd)
	if err != nil {
		return fmt.Errorf("span aggregates: %w", err)
	}

	// Memory & KG point-in-time counts
	memoryCounts, err := queryMemoryCounts(ctx, w.db)
	if err != nil {
		slog.Warn("snapshot: memory counts failed, continuing without", "error", err)
		memoryCounts = nil
	}
	kgCounts, err := queryKGCounts(ctx, w.db)
	if err != nil {
		slog.Warn("snapshot: kg counts failed, continuing without", "error", err)
		kgCounts = nil
	}

	// Merge into UsageSnapshot rows
	snapshots := mergeTraceAndSpanRows(bucketStart, traceRows, spanRows, memoryCounts, kgCounts)

	if len(snapshots) == 0 {
		return nil
	}

	return w.snapshots.UpsertSnapshots(ctx, snapshots)
}

// traceAggregate holds trace-level metrics for one (agent_id, channel) group.
type traceAggregate struct {
	AgentID       *uuid.UUID
	Channel       string
	RequestCount  int
	ErrorCount    int
	UniqueUsers   int
	InputTokens   int64
	OutputTokens  int64
	TotalCost     float64
	ToolCallCount int
	AvgDurationMS int
}

func queryTraceAggregates(ctx context.Context, db *sql.DB, from, to time.Time) ([]traceAggregate, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			agent_id,
			COALESCE(channel, '') as channel,
			COUNT(*) as request_count,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error_count,
			COUNT(DISTINCT user_id) as unique_users,
			COALESCE(SUM(total_input_tokens), 0) as input_tokens,
			COALESCE(SUM(total_output_tokens), 0) as output_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(tool_call_count), 0) as tool_call_count,
			CAST(COALESCE(AVG(duration_ms), 0) AS INTEGER) as avg_duration_ms
		FROM traces
		WHERE start_time >= $1 AND start_time < $2
		  AND parent_trace_id IS NULL
		GROUP BY agent_id, channel`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []traceAggregate
	for rows.Next() {
		var ta traceAggregate
		if err := rows.Scan(
			&ta.AgentID, &ta.Channel,
			&ta.RequestCount, &ta.ErrorCount, &ta.UniqueUsers,
			&ta.InputTokens, &ta.OutputTokens, &ta.TotalCost,
			&ta.ToolCallCount, &ta.AvgDurationMS,
		); err != nil {
			return nil, err
		}
		result = append(result, ta)
	}
	return result, rows.Err()
}

// spanAggregate holds span-level LLM metrics for one (agent_id, channel, provider, model) group.
type spanAggregate struct {
	AgentID           *uuid.UUID
	Channel           string
	Provider          string
	Model             string
	LLMCallCount      int
	InputTokens       int64
	OutputTokens      int64
	TotalCost         float64
	CacheReadTokens   int64
	CacheCreateTokens int64
	ThinkingTokens    int64
}

func querySpanAggregates(ctx context.Context, db *sql.DB, from, to time.Time) ([]spanAggregate, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			t.agent_id,
			COALESCE(t.channel, '') as channel,
			COALESCE(s.provider, '') as provider,
			COALESCE(s.model, '') as model,
			COUNT(*) as llm_call_count,
			COALESCE(SUM(s.input_tokens), 0) as span_input_tokens,
			COALESCE(SUM(s.output_tokens), 0) as span_output_tokens,
			COALESCE(SUM(s.total_cost), 0) as span_cost,
			COALESCE(SUM(CAST(s.metadata->>'cache_read_tokens' AS INTEGER)), 0) as cache_read_tokens,
			COALESCE(SUM(CAST(s.metadata->>'cache_creation_tokens' AS INTEGER)), 0) as cache_create_tokens,
			COALESCE(SUM(CAST(s.metadata->>'thinking_tokens' AS INTEGER)), 0) as thinking_tokens
		FROM traces t
		JOIN spans s ON s.trace_id = t.id AND s.span_type = 'llm_call'
		WHERE t.start_time >= $1 AND t.start_time < $2
		  AND t.parent_trace_id IS NULL
		GROUP BY t.agent_id, t.channel, s.provider, s.model`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []spanAggregate
	for rows.Next() {
		var sa spanAggregate
		if err := rows.Scan(
			&sa.AgentID, &sa.Channel,
			&sa.Provider, &sa.Model,
			&sa.LLMCallCount,
			&sa.InputTokens, &sa.OutputTokens, &sa.TotalCost,
			&sa.CacheReadTokens, &sa.CacheCreateTokens, &sa.ThinkingTokens,
		); err != nil {
			return nil, err
		}
		result = append(result, sa)
	}
	return result, rows.Err()
}

// agentMemoryCounts holds point-in-time memory counts for one agent.
type agentMemoryCounts struct {
	AgentID uuid.UUID
	Docs    int
	Chunks  int
}

// agentKGCounts holds point-in-time KG counts for one agent.
type agentKGCounts struct {
	AgentID  uuid.UUID
	Entities int
	Relations int
}

func queryMemoryCounts(ctx context.Context, db *sql.DB) (map[uuid.UUID]agentMemoryCounts, error) {
	result := make(map[uuid.UUID]agentMemoryCounts)

	// Document counts
	rows, err := db.QueryContext(ctx, `SELECT agent_id, COUNT(*) FROM memory_documents GROUP BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID uuid.UUID
		var count int
		if err := rows.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		mc := result[agentID]
		mc.AgentID = agentID
		mc.Docs = count
		result[agentID] = mc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Chunk counts
	rows2, err := db.QueryContext(ctx, `SELECT agent_id, COUNT(*) FROM memory_chunks GROUP BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var agentID uuid.UUID
		var count int
		if err := rows2.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		mc := result[agentID]
		mc.AgentID = agentID
		mc.Chunks = count
		result[agentID] = mc
	}
	return result, rows2.Err()
}

func queryKGCounts(ctx context.Context, db *sql.DB) (map[uuid.UUID]agentKGCounts, error) {
	result := make(map[uuid.UUID]agentKGCounts)

	rows, err := db.QueryContext(ctx, `SELECT agent_id, COUNT(*) FROM kg_entities GROUP BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var agentID uuid.UUID
		var count int
		if err := rows.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		kc := result[agentID]
		kc.AgentID = agentID
		kc.Entities = count
		result[agentID] = kc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := db.QueryContext(ctx, `SELECT agent_id, COUNT(*) FROM kg_relations GROUP BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var agentID uuid.UUID
		var count int
		if err := rows2.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		kc := result[agentID]
		kc.AgentID = agentID
		kc.Relations = count
		result[agentID] = kc
	}
	return result, rows2.Err()
}

// agentChannelKey is a composite key for the merge map.
type agentChannelKey struct {
	AgentID uuid.UUID // zero UUID for nil agent_id
	Channel string
}

func mergeTraceAndSpanRows(
	bucketStart time.Time,
	traceRows []traceAggregate,
	spanRows []spanAggregate,
	memoryCounts map[uuid.UUID]agentMemoryCounts,
	kgCounts map[uuid.UUID]agentKGCounts,
) []store.UsageSnapshot {
	var snapshots []store.UsageSnapshot
	seenAgents := make(map[agentChannelKey]bool)

	// 1. Create "totals" rows from trace data (provider='', model='')
	for _, tr := range traceRows {
		key := agentChannelKey{Channel: tr.Channel}
		if tr.AgentID != nil {
			key.AgentID = *tr.AgentID
		}
		seenAgents[key] = true

		snap := store.UsageSnapshot{
			BucketHour:    bucketStart,
			AgentID:       tr.AgentID,
			Provider:      "",
			Model:         "",
			Channel:       tr.Channel,
			RequestCount:  tr.RequestCount,
			ErrorCount:    tr.ErrorCount,
			UniqueUsers:   tr.UniqueUsers,
			ToolCallCount: tr.ToolCallCount,
			AvgDurationMS: tr.AvgDurationMS,
		}

		// Attach memory/KG counts to totals row
		if tr.AgentID != nil {
			if mc, ok := memoryCounts[*tr.AgentID]; ok {
				snap.MemoryDocs = mc.Docs
				snap.MemoryChunks = mc.Chunks
			}
			if kc, ok := kgCounts[*tr.AgentID]; ok {
				snap.KGEntities = kc.Entities
				snap.KGRelations = kc.Relations
			}
		}

		snapshots = append(snapshots, snap)
	}

	// 2. Create detail rows from span data (with actual provider/model)
	for _, sp := range spanRows {
		snapshots = append(snapshots, store.UsageSnapshot{
			BucketHour:        bucketStart,
			AgentID:           sp.AgentID,
			Provider:          sp.Provider,
			Model:             sp.Model,
			Channel:           sp.Channel,
			LLMCallCount:      sp.LLMCallCount,
			InputTokens:       sp.InputTokens,
			OutputTokens:      sp.OutputTokens,
			TotalCost:         sp.TotalCost,
			CacheReadTokens:   sp.CacheReadTokens,
			CacheCreateTokens: sp.CacheCreateTokens,
			ThinkingTokens:    sp.ThinkingTokens,
		})
	}

	// 3. Create memory/KG-only totals rows for agents without traces this hour
	if memoryCounts != nil || kgCounts != nil {
		allAgents := make(map[uuid.UUID]bool)
		for id := range memoryCounts {
			allAgents[id] = true
		}
		for id := range kgCounts {
			allAgents[id] = true
		}
		for agentID := range allAgents {
			key := agentChannelKey{AgentID: agentID}
			if seenAgents[key] {
				continue
			}
			aid := agentID
			snap := store.UsageSnapshot{
				BucketHour: bucketStart,
				AgentID:    &aid,
				Provider:   "",
				Model:      "",
				Channel:    "",
			}
			if mc, ok := memoryCounts[agentID]; ok {
				snap.MemoryDocs = mc.Docs
				snap.MemoryChunks = mc.Chunks
			}
			if kc, ok := kgCounts[agentID]; ok {
				snap.KGEntities = kc.Entities
				snap.KGRelations = kc.Relations
			}
			snapshots = append(snapshots, snap)
		}
	}

	return snapshots
}
