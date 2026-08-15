package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channelmemory"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type memoryExtractionPendingStore struct {
	groups []store.PendingMessageGroup
	titles map[string]string
}

func (s *memoryExtractionPendingStore) AppendBatch(context.Context, []store.PendingMessage) error {
	return nil
}

func (s *memoryExtractionPendingStore) ListByKey(context.Context, string, string) ([]store.PendingMessage, error) {
	return nil, nil
}

func (s *memoryExtractionPendingStore) DeleteByKey(context.Context, string, string) error {
	return nil
}

func (s *memoryExtractionPendingStore) Compact(context.Context, []uuid.UUID, *store.PendingMessage) error {
	return nil
}

func (s *memoryExtractionPendingStore) DeleteStale(context.Context, time.Duration) (int64, error) {
	return 0, nil
}

func (s *memoryExtractionPendingStore) ListGroups(context.Context) ([]store.PendingMessageGroup, error) {
	return s.groups, nil
}

func (s *memoryExtractionPendingStore) CountAll(context.Context) (int64, error) {
	return 0, nil
}

func (s *memoryExtractionPendingStore) CountByKey(context.Context, string, string) (int, error) {
	return 0, nil
}

func (s *memoryExtractionPendingStore) ResolveGroupTitles(context.Context, []store.PendingMessageGroup) (map[string]string, error) {
	return s.titles, nil
}

type liveTitleTrapChannel struct {
	*channels.BaseChannel
	called bool
}

func (c *liveTitleTrapChannel) Start(context.Context) error { return nil }
func (c *liveTitleTrapChannel) Stop(context.Context) error  { return nil }
func (c *liveTitleTrapChannel) Send(context.Context, bus.OutboundMessage) error {
	return nil
}
func (c *liveTitleTrapChannel) IsRunning() bool       { return true }
func (c *liveTitleTrapChannel) IsAllowed(string) bool { return true }
func (c *liveTitleTrapChannel) ResolveGroupTitle(context.Context, string) (string, error) {
	c.called = true
	return "", nil
}
func (c *liveTitleTrapChannel) ResolveGroupTitles(context.Context, []string) (map[string]string, error) {
	c.called = true
	return nil, nil
}

func TestMemoryExtractionGroupsUsesDBTitlesOnly(t *testing.T) {
	instID := uuid.New()
	channelName := "discord-main"
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{
			BaseModel:   store.BaseModel{ID: instID},
			Name:        channelName,
			ChannelType: channels.TypeDiscord,
		}},
		nil, nil, nil, nil, nil,
	)
	handler.memoryService = &channelmemory.Service{Pending: &memoryExtractionPendingStore{
		groups: []store.PendingMessageGroup{
			{
				ChannelName:      channelName,
				HistoryKey:       "thread-1",
				ParentHistoryKey: "parent-1",
				MessageCount:     4,
				LastActivity:     time.Date(2026, 7, 8, 8, 0, 0, 0, time.UTC),
			},
		},
		titles: map[string]string{
			channelName + ":thread-1": "launch-thread",
			channelName + ":parent-1": "product-planning",
		},
	}}
	manager := channels.NewManager(bus.New())
	trap := &liveTitleTrapChannel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, nil, nil)}
	trap.SetName(channelName)
	trap.SetType(channels.TypeDiscord)
	manager.RegisterChannel(channelName, trap)
	handler.SetChannelManager(manager)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+instID.String()+"/memory-extraction/groups", nil)
	req.SetPathValue("id", instID.String())
	rec := httptest.NewRecorder()
	handler.handleMemoryExtractionGroups(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if trap.called {
		t.Fatal("memory extraction groups called live channel title resolver")
	}
	var body struct {
		Groups []channelmemory.GroupOption `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(body.Groups))
	}
	if body.Groups[0].GroupTitle != "launch-thread" {
		t.Fatalf("group title = %q, want launch-thread", body.Groups[0].GroupTitle)
	}
	if body.Groups[0].ParentGroupTitle != "product-planning" {
		t.Fatalf("parent group title = %q, want product-planning", body.Groups[0].ParentGroupTitle)
	}
}

func TestPendingMessageGroupsIncludeParentTitle(t *testing.T) {
	pending := &memoryExtractionPendingStore{
		groups: []store.PendingMessageGroup{{
			ChannelName:      "discord-main",
			HistoryKey:       "thread-1",
			ParentHistoryKey: "parent-1",
		}},
		titles: map[string]string{
			"discord-main:thread-1": "launch-thread",
			"discord-main:parent-1": "product-planning",
		},
	}
	h := NewPendingMessagesHandler(pending, nil, nil)
	w := httptest.NewRecorder()
	h.handleListGroups(w, httptest.NewRequest(http.MethodGet, "/v1/pending-messages", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var body struct {
		Groups []store.PendingMessageGroup `json:"groups"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(body.Groups))
	}
	if got := body.Groups[0].ParentGroupTitle; got != "product-planning" {
		t.Fatalf("parent group title = %q, want product-planning", got)
	}
}
