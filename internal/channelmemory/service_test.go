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

func (f *fakeExtractionStore) CountItems(context.Context, store.ChannelMemoryItemListOptions) (int, error) {
	return 0, nil
}

func (f *fakeExtractionStore) UpdateItem(context.Context, uuid.UUID, map[string]any) error {
	return nil
}

type fakePendingStore struct {
	groups   []store.PendingMessageGroup
	messages map[string][]store.PendingMessage
}

func (f *fakePendingStore) AppendBatch(context.Context, []store.PendingMessage) error {
	return nil
}

func (f *fakePendingStore) ListByKey(_ context.Context, channelName, historyKey string) ([]store.PendingMessage, error) {
	return f.messages[channelName+":"+historyKey], nil
}

func (f *fakePendingStore) DeleteByKey(context.Context, string, string) error {
	return nil
}

func (f *fakePendingStore) Compact(context.Context, []uuid.UUID, *store.PendingMessage) error {
	return nil
}

func (f *fakePendingStore) DeleteStale(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (f *fakePendingStore) ListGroups(context.Context) ([]store.PendingMessageGroup, error) {
	return f.groups, nil
}

func (f *fakePendingStore) CountAll(context.Context) (int64, error) {
	return 0, nil
}

func (f *fakePendingStore) CountByKey(context.Context, string, string) (int, error) {
	return 0, nil
}

func (f *fakePendingStore) ResolveGroupTitles(context.Context, []store.PendingMessageGroup) (map[string]string, error) {
	return nil, nil
}

func TestShouldRunScheduledWhenMessageCapReached(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MessageCap = 10
	cfg.IntervalMinutes = 360
	svc := &Service{Extractions: &fakeExtractionStore{runs: []store.ChannelMemoryExtractionRun{{
		CreatedAt: time.Now().UTC(),
	}}}}
	if !svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 10}, cfg, 10) {
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
	if !svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 20}, cfg, 20) {
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
	if svc.shouldRunScheduled(context.Background(), uuid.New(), store.PendingMessageGroup{MessageCount: 20}, cfg, 20) {
		t.Fatal("expected scheduled run to wait")
	}
}

func TestRunAllSkipsLowVolumeGroupsAndContinues(t *testing.T) {
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "telegram",
		Config:    MergeIntoInstanceConfig(nil, Config{Enabled: true, MinMessages: 5}),
	}
	pending := &fakePendingStore{
		groups: []store.PendingMessageGroup{
			{ChannelName: "telegram", HistoryKey: "group-a", MessageCount: 1},
			{ChannelName: "telegram", HistoryKey: "group-b", MessageCount: 2},
		},
		messages: map[string][]store.PendingMessage{
			"telegram:group-a": {{ID: uuid.New(), ChannelName: "telegram", HistoryKey: "group-a", Body: "one", CreatedAt: time.Now().UTC()}},
			"telegram:group-b": {
				{ID: uuid.New(), ChannelName: "telegram", HistoryKey: "group-b", Body: "one", CreatedAt: time.Now().UTC()},
				{ID: uuid.New(), ChannelName: "telegram", HistoryKey: "group-b", Body: "two", CreatedAt: time.Now().UTC()},
			},
		},
	}
	svc := &Service{Pending: pending, Extractions: &fakeExtractionStore{}}

	result, err := svc.RunAll(context.Background(), inst, "scheduled")
	if err != nil {
		t.Fatalf("RunAll returned error: %v", err)
	}
	if result.SkippedGroupCount != 2 {
		t.Fatalf("expected both low-volume groups to be skipped, got %d", result.SkippedGroupCount)
	}
	if result.RunCount != 0 {
		t.Fatalf("expected no runs for low-volume groups, got %d", result.RunCount)
	}
}

func TestRunAllSkipsExcludedHistoryKeys(t *testing.T) {
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "discord",
		Config:    MergeIntoInstanceConfig(nil, Config{Enabled: true, MinMessages: 2, ExcludeHistoryKeys: []string{"excluded-channel"}}),
	}
	pending := &fakePendingStore{
		groups: []store.PendingMessageGroup{
			{ChannelName: "discord", HistoryKey: "excluded-channel", MessageCount: 3},
		},
		messages: map[string][]store.PendingMessage{
			"discord:excluded-channel": {
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "excluded-channel", Body: "one", CreatedAt: time.Now().UTC()},
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "excluded-channel", Body: "two", CreatedAt: time.Now().UTC()},
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "excluded-channel", Body: "three", CreatedAt: time.Now().UTC()},
			},
		},
	}
	svc := &Service{Pending: pending, Extractions: &fakeExtractionStore{}}

	result, err := svc.RunAll(context.Background(), inst, "scheduled")
	if err != nil {
		t.Fatalf("RunAll returned error: %v", err)
	}
	if result.RunCount != 0 || result.SkippedGroupCount != 0 {
		t.Fatalf("excluded group should not be processed or counted: %+v", result)
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
