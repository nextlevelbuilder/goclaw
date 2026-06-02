package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	runtimelogs "github.com/nextlevelbuilder/goclaw/internal/logs"
)

func TestLogTeeAggregateIncludesWithAttrsAndGroupEntries(t *testing.T) {
	tee := NewLogTee(slog.NewTextHandler(io.Discard, nil))
	root := slog.New(tee)
	root.Info("root", "source", "gateway")
	root.With("source", "worker").Warn("child")
	root.WithGroup("grouped").Error("grouped", "source", "jobs")

	result := tee.AggregateRuntimeLogs(runtimelogs.RuntimeAggregateOpts{GroupBy: "source"})
	if result.Capacity != 100 || result.Retention != "ring_buffer" {
		t.Fatalf("result metadata = %+v", result)
	}
	got := map[string]int{}
	for _, b := range result.Buckets {
		got[b.Key] = b.Count
	}
	if got["gateway"] != 1 || got["worker"] != 1 || got["jobs"] != 1 {
		t.Fatalf("buckets = %+v", result.Buckets)
	}
}

func TestLogTeeAggregateRedactsSensitiveAttrs(t *testing.T) {
	tee := NewLogTee(slog.NewTextHandler(io.Discard, nil))
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "secret", 0)
	rec.AddAttrs(slog.String("api_token", "leak"), slog.String("source", "test"))
	if err := tee.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	entries := tee.recentEntries()
	attrs, _ := entries[0]["attrs"].(map[string]any)
	if attrs["api_token"] != redactedValue {
		t.Fatalf("attrs = %+v", attrs)
	}
}
