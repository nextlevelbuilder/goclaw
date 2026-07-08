package channelmemory

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeExtractionStore struct {
	runs          []store.ChannelMemoryExtractionRun
	items         map[uuid.UUID]*store.ChannelMemoryExtractionItem
	createdRun    *store.ChannelMemoryExtractionRun
	createdItems  []*store.ChannelMemoryExtractionItem
	createItemErr error
	countItems    int
	countOpts     store.ChannelMemoryItemListOptions
	updateRuns    []map[string]any
	updateItems   []map[string]any
}

func (f *fakeExtractionStore) CreateRun(ctx context.Context, run *store.ChannelMemoryExtractionRun) error {
	if run.ID == uuid.Nil {
		run.ID = uuid.New()
	}
	if run.TenantID == uuid.Nil {
		run.TenantID = store.TenantIDFromContext(ctx)
	}
	copied := *run
	f.createdRun = &copied
	f.runs = append([]store.ChannelMemoryExtractionRun{copied}, f.runs...)
	return nil
}

func (f *fakeExtractionStore) GetRun(context.Context, uuid.UUID) (*store.ChannelMemoryExtractionRun, error) {
	return nil, sql.ErrNoRows
}

func (f *fakeExtractionStore) ListRuns(context.Context, store.ChannelMemoryRunListOptions) ([]store.ChannelMemoryExtractionRun, error) {
	return f.runs, nil
}

func (f *fakeExtractionStore) UpdateRun(_ context.Context, id uuid.UUID, updates map[string]any) error {
	f.updateRuns = append(f.updateRuns, updates)
	if f.createdRun != nil && f.createdRun.ID == id {
		if status, ok := updates["status"].(string); ok {
			f.createdRun.Status = status
		}
		if errMsg, ok := updates["error_message"].(string); ok {
			f.createdRun.ErrorMessage = errMsg
		}
		if itemCount, ok := updates["item_count"].(int); ok {
			f.createdRun.ItemCount = itemCount
		}
	}
	return nil
}

func (f *fakeExtractionStore) CreateItem(_ context.Context, argumentsItem *store.ChannelMemoryExtractionItem) error {
	if f.createItemErr != nil {
		return f.createItemErr
	}
	if f.items == nil {
		f.items = make(map[uuid.UUID]*store.ChannelMemoryExtractionItem)
	}
	item := *argumentsItem
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
		argumentsItem.ID = item.ID
	}
	if item.Status == "" {
		item.Status = store.ChannelMemoryItemPendingReview
		argumentsItem.Status = item.Status
	}
	f.items[item.ID] = &item
	f.createdItems = append(f.createdItems, &item)
	return nil
}

func (f *fakeExtractionStore) GetItem(_ context.Context, id uuid.UUID) (*store.ChannelMemoryExtractionItem, error) {
	if item, ok := f.items[id]; ok {
		copied := *item
		return &copied, nil
	}
	return nil, sql.ErrNoRows
}

func (f *fakeExtractionStore) ListItems(context.Context, store.ChannelMemoryItemListOptions) ([]store.ChannelMemoryExtractionItem, error) {
	return nil, nil
}

func (f *fakeExtractionStore) CountItems(_ context.Context, opts store.ChannelMemoryItemListOptions) (int, error) {
	f.countOpts = opts
	return f.countItems, nil
}

func (f *fakeExtractionStore) UpdateItem(_ context.Context, id uuid.UUID, updates map[string]any) error {
	f.updateItems = append(f.updateItems, updates)
	if item, ok := f.items[id]; ok {
		if status, ok := updates["status"].(string); ok {
			item.Status = status
		}
		if episodicID, ok := updates["episodic_id"].(string); ok {
			item.EpisodicID = episodicID
		}
	}
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

type fakeChannelStore struct {
	store.ChannelInstanceStore
	inst *store.ChannelInstanceData
}

func (f *fakeChannelStore) Get(context.Context, uuid.UUID) (*store.ChannelInstanceData, error) {
	if f.inst == nil {
		return nil, sql.ErrNoRows
	}
	return f.inst, nil
}

type fakeEpisodicStore struct {
	store.EpisodicStore
	exists    bool
	bySource  *store.EpisodicSummary
	created   []*store.EpisodicSummary
	getCalls  int
	createErr error
	existsErr error
	getByErr  error
}

func (f *fakeEpisodicStore) Create(_ context.Context, ep *store.EpisodicSummary) error {
	if f.createErr != nil {
		return f.createErr
	}
	if ep.ID == uuid.Nil {
		ep.ID = uuid.New()
	}
	copied := *ep
	f.created = append(f.created, &copied)
	return nil
}

func (f *fakeEpisodicStore) ExistsBySourceID(context.Context, string, string, string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeEpisodicStore) GetBySourceID(context.Context, string, string, string) (*store.EpisodicSummary, error) {
	f.getCalls++
	if f.getByErr != nil {
		return nil, f.getByErr
	}
	return f.bySource, nil
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

	var events []ProcessAllEvent
	result, err := svc.RunAllWithProgress(context.Background(), inst, "scheduled", func(event ProcessAllEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("RunAll returned error: %v", err)
	}
	if result.SkippedGroupCount != 2 {
		t.Fatalf("expected both low-volume groups to be skipped, got %d", result.SkippedGroupCount)
	}
	if result.RunCount != 0 {
		t.Fatalf("expected no runs for low-volume groups, got %d", result.RunCount)
	}
	if len(events) < 2 {
		t.Fatalf("expected skipped events, got %d", len(events))
	}
	if events[0].Type != "group_skipped" || events[0].GroupMessageCount != 1 {
		t.Fatalf("first skipped event = %+v, want group_message_count 1", events[0])
	}
	if events[1].Type != "group_skipped" || events[1].GroupMessageCount != 2 {
		t.Fatalf("second skipped event = %+v, want group_message_count 2", events[1])
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

func TestRunAllSkipsThreadWhenParentHistoryKeyExcluded(t *testing.T) {
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "discord",
		Config:    MergeIntoInstanceConfig(nil, Config{Enabled: true, MinMessages: 2, ExcludeHistoryKeys: []string{"parent-channel"}}),
	}
	pending := &fakePendingStore{
		groups: []store.PendingMessageGroup{
			{ChannelName: "discord", HistoryKey: "thread-1", ParentHistoryKey: "parent-channel", MessageCount: 3},
		},
		messages: map[string][]store.PendingMessage{
			"discord:thread-1": {
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "thread-1", ParentHistoryKey: "parent-channel", Body: "one", CreatedAt: time.Now().UTC()},
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "thread-1", ParentHistoryKey: "parent-channel", Body: "two", CreatedAt: time.Now().UTC()},
				{ID: uuid.New(), ChannelName: "discord", HistoryKey: "thread-1", ParentHistoryKey: "parent-channel", Body: "three", CreatedAt: time.Now().UTC()},
			},
		},
	}
	svc := &Service{Pending: pending, Extractions: &fakeExtractionStore{}}

	result, err := svc.RunAll(context.Background(), inst, "scheduled")
	if err != nil {
		t.Fatalf("RunAll returned error: %v", err)
	}
	if result.RunCount != 0 || result.SkippedGroupCount != 0 {
		t.Fatalf("thread under excluded parent should not be processed or counted: %+v", result)
	}
	count, err := svc.UnprocessedMessageCount(context.Background(), inst)
	if err != nil {
		t.Fatalf("UnprocessedMessageCount returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("unprocessed count = %d, want 0 for thread under excluded parent", count)
	}
	options, err := svc.GroupOptions(context.Background(), inst)
	if err != nil {
		t.Fatalf("GroupOptions returned error: %v", err)
	}
	if len(options) != 1 || !options[0].Excluded || options[0].ParentHistoryKey != "parent-channel" {
		t.Fatalf("group option = %+v, want excluded thread with parent key", options)
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

func TestStatusCountsAllPendingItems(t *testing.T) {
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		Name:      "discord",
		Config:    MergeIntoInstanceConfig(nil, DefaultConfig()),
	}
	extractions := &fakeExtractionStore{countItems: 75}
	svc := &Service{
		Pending:     &fakePendingStore{},
		Extractions: extractions,
	}

	status, err := svc.Status(context.Background(), inst)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingCount != 75 {
		t.Fatalf("pending count = %d, want 75", status.PendingCount)
	}
	if extractions.countOpts.Status != store.ChannelMemoryItemPendingReview {
		t.Fatalf("CountItems status = %q, want pending_review", extractions.countOpts.Status)
	}
	if extractions.countOpts.ChannelInstanceID != inst.ID {
		t.Fatalf("CountItems channel = %s, want %s", extractions.countOpts.ChannelInstanceID, inst.ID)
	}
}

func TestRunMessagesCheckpointsOnlyExtractedBudget(t *testing.T) {
	tenantID := uuid.New()
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		TenantID:  tenantID,
		Name:      "discord",
		AgentID:   uuid.New(),
		CreatedBy: "user-1",
	}
	cfg := DefaultConfig()
	cfg.MinMessages = 2
	cfg.AllowedTypes = DefaultAllowedTypes
	cfg.ReviewMode = true
	messages := make([]store.PendingMessage, 0, 12)
	for i := range 12 {
		messages = append(messages, store.PendingMessage{
			ID:          uuid.New(),
			ChannelName: "discord",
			HistoryKey:  "group-a",
			Sender:      "tester",
			Body:        "This is durable project context. " + strings.Repeat("x", 900),
			CreatedAt:   time.Date(2026, 7, 7, 10, i, 0, 0, time.UTC),
		})
	}
	consumed := messagesWithinExtractionBudget(messages, extractionRetryMaxInputChars)
	if len(consumed) == 0 || len(consumed) == len(messages) {
		t.Fatalf("test fixture should be partially consumed, got %d of %d", len(consumed), len(messages))
	}
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{{Content: `[]`, FinishReason: "stop"}}}
	registry := providers.NewRegistry(store.TenantIDFromContext)
	registry.RegisterForTenant(tenantID, provider)
	extractions := &fakeExtractionStore{}
	svc := &Service{Extractions: extractions, Registry: registry}

	ctx := store.WithTenantID(context.Background(), tenantID)
	run, err := svc.runMessages(ctx, inst, cfg, store.PendingMessageGroup{ChannelName: "discord", HistoryKey: "group-a"}, messages, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.MessageCount != len(consumed) {
		t.Fatalf("run message_count = %d, want %d", run.MessageCount, len(consumed))
	}
	wantEndID := messageSourceID(consumed[len(consumed)-1])
	if run.SourceEndID != wantEndID {
		t.Fatalf("source_end_id = %s, want %s", run.SourceEndID, wantEndID)
	}
	if run.SourceEndID == messageSourceID(messages[len(messages)-1]) {
		t.Fatal("run checkpoint advanced to a message outside the extraction budget")
	}
}

func TestRunMessagesMarksRunFailedWhenItemWriteFails(t *testing.T) {
	tenantID := uuid.New()
	inst := &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: uuid.New()},
		TenantID:  tenantID,
		Name:      "discord",
		AgentID:   uuid.New(),
		CreatedBy: "user-1",
	}
	cfg := DefaultConfig()
	cfg.MinMessages = 2
	cfg.AllowedTypes = DefaultAllowedTypes
	cfg.ReviewMode = true
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{{
		Content:      `[{"type":"todos","summary":"Follow up on launch checklist","topics":["launch"],"entities":["GoClaw"],"confidence":0.91}]`,
		FinishReason: "stop",
	}}}
	registry := providers.NewRegistry(store.TenantIDFromContext)
	registry.RegisterForTenant(tenantID, provider)
	extractions := &fakeExtractionStore{createItemErr: errors.New("create item boom")}
	svc := &Service{Extractions: extractions, Registry: registry}

	ctx := store.WithTenantID(context.Background(), tenantID)
	_, err := svc.runMessages(ctx, inst, cfg, store.PendingMessageGroup{ChannelName: "discord", HistoryKey: "group-a"}, extractionTestMessages(3), "manual")
	if err == nil {
		t.Fatal("expected CreateItem error")
	}
	if len(extractions.updateRuns) == 0 {
		t.Fatal("expected failed run update")
	}
	last := extractions.updateRuns[len(extractions.updateRuns)-1]
	if last["status"] != store.ChannelMemoryRunFailed {
		t.Fatalf("run status update = %v, want failed", last["status"])
	}
	if last["error_message"] != "create item boom" {
		t.Fatalf("error_message = %v", last["error_message"])
	}
}

func TestApproveUsesConfiguredRetentionHours(t *testing.T) {
	instID := uuid.New()
	agentID := uuid.New()
	itemID := uuid.New()
	cfg := DefaultConfig()
	cfg.RetentionHours = 2
	extractions := &fakeExtractionStore{items: map[uuid.UUID]*store.ChannelMemoryExtractionItem{
		itemID: {
			ID:                itemID,
			TenantID:          uuid.New(),
			ChannelInstanceID: instID,
			AgentID:           agentID,
			UserID:            "user-1",
			SourceID:          "channel:item-1",
			Status:            store.ChannelMemoryItemPendingReview,
			Summary:           "Remember the deployment window.",
		},
	}}
	episodic := &fakeEpisodicStore{}
	svc := &Service{
		Extractions: extractions,
		Episodic:    episodic,
		Channels: &fakeChannelStore{inst: &store.ChannelInstanceData{
			BaseModel: store.BaseModel{ID: instID},
			Config:    MergeIntoInstanceConfig(nil, cfg),
		}},
	}

	before := time.Now().UTC()
	if _, err := svc.Approve(context.Background(), itemID, "admin"); err != nil {
		t.Fatal(err)
	}
	if len(episodic.created) != 1 {
		t.Fatalf("created episodic count = %d, want 1", len(episodic.created))
	}
	expires := episodic.created[0].ExpiresAt
	if expires == nil {
		t.Fatal("episodic ExpiresAt is nil")
	}
	min := before.Add(2*time.Hour - time.Second)
	max := time.Now().UTC().Add(2*time.Hour + time.Second)
	if expires.Before(min) || expires.After(max) {
		t.Fatalf("expires_at = %s, want around 2h from approval", expires)
	}
}

func TestApproveExistingSourceWritesExistingEpisodicID(t *testing.T) {
	itemID := uuid.New()
	existingID := uuid.New()
	extractions := &fakeExtractionStore{items: map[uuid.UUID]*store.ChannelMemoryExtractionItem{
		itemID: {
			ID:                itemID,
			TenantID:          uuid.New(),
			ChannelInstanceID: uuid.New(),
			AgentID:           uuid.New(),
			UserID:            "user-1",
			SourceID:          "channel:item-1",
			Status:            store.ChannelMemoryItemPendingReview,
			Summary:           "Remember the duplicate item.",
		},
	}}
	episodic := &fakeEpisodicStore{
		exists:   true,
		bySource: &store.EpisodicSummary{ID: existingID, SourceID: "channel:item-1"},
	}
	svc := &Service{Extractions: extractions, Episodic: episodic}

	item, err := svc.Approve(context.Background(), itemID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodic.created) != 0 {
		t.Fatalf("created episodic count = %d, want 0", len(episodic.created))
	}
	if episodic.getCalls != 1 {
		t.Fatalf("GetBySourceID calls = %d, want 1", episodic.getCalls)
	}
	if item.EpisodicID != existingID.String() {
		t.Fatalf("item episodic_id = %q, want %q", item.EpisodicID, existingID)
	}
	if len(extractions.updateItems) == 0 || extractions.updateItems[len(extractions.updateItems)-1]["episodic_id"] != existingID.String() {
		t.Fatalf("UpdateItem did not persist existing episodic_id: %#v", extractions.updateItems)
	}
}
