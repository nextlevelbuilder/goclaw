package http

import (
	"net/http"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *UsageHandler) aggregateTimeSeriesWithLive(r *http.Request, q store.SnapshotQuery, now time.Time) usageSummary {
	points, err := h.snapshots.GetTimeSeries(r.Context(), q)
	if err != nil {
		return usageSummary{}
	}
	if from, to, ok := liveUsageWindow(q, now); ok {
		livePoint, err := h.queryLiveTimeSeries(r, from, to, q)
		if err != nil {
			return summarizeTimeSeries(points)
		}
		if livePoint != nil {
			points = mergeSnapshotTimeSeries(points, *livePoint, q.GroupBy)
		}
	}
	return summarizeTimeSeries(points)
}

func summarizeTimeSeries(points []store.SnapshotTimeSeries) usageSummary {
	var s usageSummary
	var totalWeightedDuration int64
	for _, p := range points {
		s.Requests += p.RequestCount
		s.InputTokens += p.InputTokens
		s.OutputTokens += p.OutputTokens
		s.Cost += p.TotalCost
		s.UniqueUsers += p.UniqueUsers
		s.Errors += p.ErrorCount
		s.LLMCalls += p.LLMCallCount
		s.ToolCalls += p.ToolCallCount
		totalWeightedDuration += int64(p.AvgDurationMS) * int64(p.RequestCount)
	}
	if s.Requests > 0 {
		s.AvgDurationMS = int(totalWeightedDuration / int64(s.Requests))
	}
	return s
}

func mergeSnapshotTimeSeries(points []store.SnapshotTimeSeries, live store.SnapshotTimeSeries, groupBy string) []store.SnapshotTimeSeries {
	for i := range points {
		if points[i].BucketTime.Equal(live.BucketTime) {
			if groupBy == "day" {
				addPointToTimeSeries(&points[i], live)
			} else {
				points[i] = live
			}
			return points
		}
	}
	points = append(points, live)
	sort.Slice(points, func(i, j int) bool {
		return points[i].BucketTime.Before(points[j].BucketTime)
	})
	return points
}

func addPointToTimeSeries(dst *store.SnapshotTimeSeries, src store.SnapshotTimeSeries) {
	oldRequests := dst.RequestCount
	weightedDuration := int64(dst.AvgDurationMS)*int64(oldRequests) + int64(src.AvgDurationMS)*int64(src.RequestCount)
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheCreateTokens += src.CacheCreateTokens
	dst.ThinkingTokens += src.ThinkingTokens
	dst.TotalCost += src.TotalCost
	dst.RequestCount += src.RequestCount
	dst.LLMCallCount += src.LLMCallCount
	dst.ToolCallCount += src.ToolCallCount
	dst.ErrorCount += src.ErrorCount
	dst.UniqueUsers += src.UniqueUsers
	dst.MemoryDocs += src.MemoryDocs
	dst.MemoryChunks += src.MemoryChunks
	dst.KGEntities += src.KGEntities
	dst.KGRelations += src.KGRelations
	if dst.RequestCount > 0 {
		dst.AvgDurationMS = int(weightedDuration / int64(dst.RequestCount))
	}
}

func mergeSnapshotBreakdowns(base, extra []store.SnapshotBreakdown, groupBy string) []store.SnapshotBreakdown {
	if len(extra) == 0 {
		return base
	}
	byKey := make(map[string]int, len(base)+len(extra))
	var result []store.SnapshotBreakdown
	add := func(row store.SnapshotBreakdown) {
		if row.Key == "" && groupBy != "agent" {
			return
		}
		if idx, ok := byKey[row.Key]; ok {
			addBreakdown(&result[idx], row)
			return
		}
		result = append(result, row)
		byKey[row.Key] = len(result) - 1
	}
	for _, row := range base {
		add(row)
	}
	for _, row := range extra {
		add(row)
	}

	sort.Slice(result, func(i, j int) bool {
		if groupBy == "channel" {
			return result[i].RequestCount > result[j].RequestCount
		}
		return result[i].InputTokens > result[j].InputTokens
	})
	return result
}

func addBreakdown(dst *store.SnapshotBreakdown, src store.SnapshotBreakdown) {
	oldRequests := dst.RequestCount
	weightedDuration := int64(dst.AvgDurationMS)*int64(oldRequests) + int64(src.AvgDurationMS)*int64(src.RequestCount)
	dst.InputTokens += src.InputTokens
	dst.OutputTokens += src.OutputTokens
	dst.CacheReadTokens += src.CacheReadTokens
	dst.CacheCreateTokens += src.CacheCreateTokens
	dst.TotalCost += src.TotalCost
	dst.RequestCount += src.RequestCount
	dst.LLMCallCount += src.LLMCallCount
	dst.ToolCallCount += src.ToolCallCount
	dst.ErrorCount += src.ErrorCount
	if dst.RequestCount > 0 {
		dst.AvgDurationMS = int(weightedDuration / int64(dst.RequestCount))
	}
}

func hasTimeSeriesUsage(point store.SnapshotTimeSeries) bool {
	return point.RequestCount > 0 ||
		point.LLMCallCount > 0 ||
		point.ToolCallCount > 0 ||
		point.InputTokens > 0 ||
		point.OutputTokens > 0 ||
		point.CacheReadTokens > 0 ||
		point.CacheCreateTokens > 0 ||
		point.ThinkingTokens > 0 ||
		point.TotalCost > 0 ||
		point.ErrorCount > 0
}
