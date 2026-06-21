package channelmemory

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeExtractionStore struct {
	runs []store.ChannelMemoryExtractionRun
}

func (f *fakeExtractionStore) CreateRun(context.Context, *store.ChannelMemoryExtractionRun) error {
	return nil
}

func (f *fakeExtractionStore) GetRun(context.Context, uuid.UUID) (*store.ChannelMemoryExtractionRun, error) {
	return nil, sql.ErrNoRows
}

func (f *fakeExtractionStore) ListRuns(context.Context, store.ChannelMemoryRunListOptions) ([]store.ChannelMemoryExtractionRun, error) {
	return f.runs, nil
}

func (f *fakeExtractionStore) UpdateRun(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

func (f *fakeExtractionStore) CreateItem(context.Context, *store.ChannelMemoryExtractionItem) error {
	return nil
}

func (f *fakeExtractionStore) GetItem(context.Context, uuid.UUID) (*store.ChannelMemoryExtractionItem, error) {
	return nil, sql.ErrNoRows
}

func (f *fakeExtractionStore) ListItems(context.Context, store.ChannelMemoryItemListOptions) ([]store.ChannelMemoryExtractionItem, error) {
	return nil, nil
}

func (f *fakeExtractionStore) UpdateItem(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

func TestShouldRunScheduledWhenMessageCapReached(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MessageCap = 10
	cfg.IntervalMinutes = 360
	svc := &Service{Extractions: &fakeExtractionStore{runs: []store.ChannelMemoryExtractionRun{{
		CreatedAt: time.Now().UTC(),
	}}}}
	if !svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 10}, cfg) {
		t.Fatal("expected scheduled run at message cap")
	}
}

func TestShouldRunScheduledWhenIntervalElapsed(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MessageCap = 100
	cfg.IntervalMinutes = 60
	svc := &Service{Extractions: &fakeExtractionStore{runs: []store.ChannelMemoryExtractionRun{{
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
	}}}}
	if !svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 20}, cfg) {
		t.Fatal("expected scheduled run after interval")
	}
}

func TestShouldSkipScheduledBelowCapAndBeforeInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MessageCap = 100
	cfg.IntervalMinutes = 60
	svc := &Service{Extractions: &fakeExtractionStore{runs: []store.ChannelMemoryExtractionRun{{
		CreatedAt: time.Now().UTC(),
	}}}}
	if svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 20}, cfg) {
		t.Fatal("expected scheduled run to wait")
	}
}

func TestItemHashIsStableAcrossRuns(t *testing.T) {
	runA := &store.ChannelMemoryExtractionRun{ID: uuid.New(), ChannelInstanceID: uuid.New(), HistoryKey: "group"}
	runB := *runA
	runB.ID = uuid.New()
	svc := &Service{}
	itemA := svc.itemFromExtracted(runA, ExtractedItem{Type: "decision", Summary: "Ship beta"})
	itemB := svc.itemFromExtracted(&runB, ExtractedItem{Type: "decision", Summary: "Ship beta"})
	if itemA.ItemHash != itemB.ItemHash {
		t.Fatalf("hash changed across runs: %s != %s", itemA.ItemHash, itemB.ItemHash)
	}
}
