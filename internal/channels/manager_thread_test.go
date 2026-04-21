package channels

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// stubChannel is a minimal Channel implementation that optionally implements
// DiscordThreadCreator. The want flag toggles whether the assertion succeeds.
type stubChannel struct {
	name   string
	typ    string
	create func(ctx context.Context, params DiscordThreadParams) (DiscordThreadResult, error)
}

func (s *stubChannel) Name() string                                 { return s.name }
func (s *stubChannel) Type() string                                 { return s.typ }
func (s *stubChannel) Start(context.Context) error                  { return nil }
func (s *stubChannel) Stop(context.Context) error                   { return nil }
func (s *stubChannel) Send(context.Context, bus.OutboundMessage) error { return nil }
func (s *stubChannel) IsRunning() bool                              { return true }
func (s *stubChannel) IsAllowed(string) bool                        { return true }

// threadedStub additionally implements DiscordThreadCreator.
type threadedStub struct {
	stubChannel
}

func (t *threadedStub) CreateThread(ctx context.Context, p DiscordThreadParams) (DiscordThreadResult, error) {
	return t.create(ctx, p)
}

func TestManagerCreateDiscordThread_ChannelNotFound(t *testing.T) {
	m := NewManager(nil)
	_, err := m.CreateDiscordThread(context.Background(), "missing", DiscordThreadParams{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestManagerCreateDiscordThread_ChannelDoesNotImplementInterface(t *testing.T) {
	m := NewManager(nil)
	m.RegisterChannel("feishu", &stubChannel{name: "feishu", typ: "feishu"})

	_, err := m.CreateDiscordThread(context.Background(), "feishu", DiscordThreadParams{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "does not support thread creation") {
		t.Fatalf("expected does-not-support error, got %v", err)
	}
}

func TestManagerCreateDiscordThread_HappyPath(t *testing.T) {
	m := NewManager(nil)
	var gotParams DiscordThreadParams
	ch := &threadedStub{
		stubChannel: stubChannel{name: "disc", typ: "discord"},
	}
	ch.create = func(ctx context.Context, p DiscordThreadParams) (DiscordThreadResult, error) {
		gotParams = p
		return DiscordThreadResult{ThreadID: "42", Name: p.Name, ParentChannelID: p.ChannelID}, nil
	}
	m.RegisterChannel("disc", ch)

	params := DiscordThreadParams{ChannelID: "c1", Name: "discuss"}
	res, err := m.CreateDiscordThread(context.Background(), "disc", params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ThreadID != "42" || res.Name != "discuss" {
		t.Errorf("unexpected result: %+v", res)
	}
	if gotParams.ChannelID != "c1" {
		t.Errorf("params not propagated: %+v", gotParams)
	}
}

func TestManagerCreateDiscordThread_ChannelErrorPropagates(t *testing.T) {
	m := NewManager(nil)
	ch := &threadedStub{stubChannel: stubChannel{name: "disc", typ: "discord"}}
	ch.create = func(ctx context.Context, p DiscordThreadParams) (DiscordThreadResult, error) {
		return DiscordThreadResult{}, errors.New("boom")
	}
	m.RegisterChannel("disc", ch)

	_, err := m.CreateDiscordThread(context.Background(), "disc", DiscordThreadParams{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected underlying error, got %v", err)
	}
}
