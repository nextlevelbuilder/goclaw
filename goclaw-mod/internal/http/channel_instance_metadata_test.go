package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type metadataRefreshChannel struct {
	*channels.BaseChannel
	called bool
}

func (c *metadataRefreshChannel) Start(context.Context) error { return nil }
func (c *metadataRefreshChannel) Stop(context.Context) error  { return nil }
func (c *metadataRefreshChannel) Send(context.Context, bus.OutboundMessage) error {
	return nil
}
func (c *metadataRefreshChannel) IsRunning() bool { return true }
func (c *metadataRefreshChannel) IsAllowed(string) bool {
	return true
}
func (c *metadataRefreshChannel) RefreshContactCache(context.Context) (channels.MetadataRefreshReport, error) {
	c.called = true
	return channels.MetadataRefreshReport{OK: true, GroupsRefreshed: 1}, nil
}

func TestRefreshChannelMetadataCallsDiscordRefresher(t *testing.T) {
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
	manager := channels.NewManager(bus.New())
	ch := &metadataRefreshChannel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, nil, nil)}
	ch.SetName(channelName)
	ch.SetType(channels.TypeDiscord)
	manager.RegisterChannel(channelName, ch)
	handler.SetChannelManager(manager)

	req := httptest.NewRequest(http.MethodPost, "/v1/channels/instances/"+instID.String()+"/metadata/refresh", nil)
	req = req.WithContext(store.WithRole(req.Context(), store.RoleOwner))
	req.SetPathValue("id", instID.String())
	rec := httptest.NewRecorder()
	handler.handleRefreshChannelMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !ch.called {
		t.Fatal("RefreshContactCache was not called")
	}
}

func TestRefreshChannelMetadataRejectsNonDiscord(t *testing.T) {
	instID := uuid.New()
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{
			BaseModel:   store.BaseModel{ID: instID},
			Name:        "telegram-main",
			ChannelType: channels.TypeTelegram,
		}},
		nil, nil, nil, nil, nil,
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/channels/instances/"+instID.String()+"/metadata/refresh", nil)
	req = req.WithContext(store.WithRole(req.Context(), store.RoleOwner))
	req.SetPathValue("id", instID.String())
	rec := httptest.NewRecorder()
	handler.handleRefreshChannelMetadata(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}
