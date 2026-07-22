package bitrix24

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// TestActivityIndicatorEnabled_Nil tests that nil config means enabled (default on).
func TestActivityIndicatorEnabled_Nil(t *testing.T) {
	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // default
		},
	}
	if !ch.activityIndicatorEnabled() {
		t.Errorf("activityIndicatorEnabled() with nil config should return true (default on)")
	}
}

// TestActivityIndicatorEnabled_ExplicitTrue tests that *true enables the indicator.
func TestActivityIndicatorEnabled_ExplicitTrue(t *testing.T) {
	trueVal := true
	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: &trueVal,
		},
	}
	if !ch.activityIndicatorEnabled() {
		t.Errorf("activityIndicatorEnabled() with *true should return true")
	}
}

// TestActivityIndicatorEnabled_ExplicitFalse tests that *false disables the indicator.
func TestActivityIndicatorEnabled_ExplicitFalse(t *testing.T) {
	falseVal := false
	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: &falseVal,
		},
	}
	if ch.activityIndicatorEnabled() {
		t.Errorf("activityIndicatorEnabled() with *false should return false")
	}
}

// TestOnActivityEvent_DisabledReturnsNilNoCall tests that disabled indicator returns nil and makes no call.
func TestOnActivityEvent_DisabledReturnsNilNoCall(t *testing.T) {
	falseVal := false
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: &falseVal,
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)

	err := ch.OnActivityEvent(context.Background(), "chat123", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with disabled indicator should return nil, got %v", err)
	}

	if len(rt.reqs) != 0 {
		t.Errorf("disabled indicator should make no REST calls, made %d", len(rt.reqs))
	}
}

// TestOnActivityEvent_EnabledMakesCall tests that enabled indicator makes a REST call.
func TestOnActivityEvent_EnabledMakesCall(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // default: enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)
	ch.botID = 123

	err := ch.OnActivityEvent(context.Background(), "chat123", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with enabled indicator should return nil, got %v", err)
	}

	if len(rt.reqs) != 1 {
		t.Errorf("enabled indicator should make 1 REST call, made %d", len(rt.reqs))
	}
}

// TestOnActivityEvent_NilClientReturnsNil tests that nil client returns nil without calling.
func TestOnActivityEvent_NilClientReturnsNil(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = nil // nil client

	err := ch.OnActivityEvent(context.Background(), "chat123", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with nil client should return nil, got %v", err)
	}

	if len(rt.reqs) != 0 {
		t.Errorf("nil client should make no REST calls, made %d", len(rt.reqs))
	}
}

// TestOnActivityEvent_InvalidBotIDReturnsNil tests that botID <= 0 returns nil without calling.
func TestOnActivityEvent_InvalidBotIDReturnsNil(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)
	ch.botID = 0 // invalid: <= 0

	err := ch.OnActivityEvent(context.Background(), "chat123", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with invalid botID should return nil, got %v", err)
	}

	if len(rt.reqs) != 0 {
		t.Errorf("invalid botID should make no REST calls, made %d", len(rt.reqs))
	}
}

// TestOnActivityEvent_EmptyChatIDReturnsNil tests that empty chatID returns nil without calling.
func TestOnActivityEvent_EmptyChatIDReturnsNil(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)
	ch.botID = 123

	// Empty chatID should fail guard
	err := ch.OnActivityEvent(context.Background(), "", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with empty chatID should return nil, got %v", err)
	}

	if len(rt.reqs) != 0 {
		t.Errorf("empty chatID should make no REST calls, made %d", len(rt.reqs))
	}

	// Whitespace-only chatID should also fail guard
	err = ch.OnActivityEvent(context.Background(), "  \t  ", "IMBOT_AGENT_ACTION_THINKING")
	if err != nil {
		t.Errorf("OnActivityEvent with whitespace chatID should return nil, got %v", err)
	}

	if len(rt.reqs) != 0 {
		t.Errorf("whitespace chatID should make no REST calls, made %d", len(rt.reqs))
	}
}

// TestOnActivityEvent_ValidCallIncludesParams tests that enabled/valid call includes all required params.
func TestOnActivityEvent_ValidCallIncludesParams(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)
	ch.botID = 456

	err := ch.OnActivityEvent(context.Background(), "chat789", "IMBOT_AGENT_ACTION_SEARCHING")
	if err != nil {
		t.Errorf("OnActivityEvent should return nil, got %v", err)
	}

	if len(rt.reqs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rt.reqs))
	}

	// Verify params
	params := rt.reqs[0]
	if params.Get("botId") != "456" {
		t.Errorf("botId param = %q, want 456", params.Get("botId"))
	}
	if params.Get("dialogId") != "chat789" {
		t.Errorf("dialogId param = %q, want chat789", params.Get("dialogId"))
	}
	if params.Get("statusMessageCode") != "IMBOT_AGENT_ACTION_SEARCHING" {
		t.Errorf("statusMessageCode param = %q, want IMBOT_AGENT_ACTION_SEARCHING", params.Get("statusMessageCode"))
	}
	if params.Get("duration") != "30" {
		t.Errorf("duration param = %q, want 30", params.Get("duration"))
	}
}

// TestOnActivityEvent_EmptyStatusCodeOmitsParam tests that empty status code omits the statusMessageCode param.
func TestOnActivityEvent_EmptyStatusCodeOmitsParam(t *testing.T) {
	rt := &captureRT{}

	ch := &Channel{
		cfg: bitrixInstanceConfig{
			ActivityIndicator: nil, // enabled
		},
		BaseChannel: channels.NewBaseChannel("test", bus.New(), nil),
	}
	ch.client = newStubClient("test.bitrix24.com", rt)
	ch.botID = 789

	err := ch.OnActivityEvent(context.Background(), "chat999", "")
	if err != nil {
		t.Errorf("OnActivityEvent with empty status should return nil, got %v", err)
	}

	if len(rt.reqs) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rt.reqs))
	}

	// Verify statusMessageCode is absent when empty
	params := rt.reqs[0]
	if params.Get("statusMessageCode") != "" {
		t.Errorf("empty status should omit statusMessageCode, but got %q", params.Get("statusMessageCode"))
	}
	// Other params should still be present
	if params.Get("botId") != "789" {
		t.Errorf("botId should be present, got %q", params.Get("botId"))
	}
}

// TestCompileTimeGuard verifies that *Channel satisfies ActivityIndicatorChannel.
func TestCompileTimeGuard(t *testing.T) {
	// This is a compile-time check, but we verify the interface at runtime here.
	var _ channels.ActivityIndicatorChannel = (*Channel)(nil)
	t.Log("*Channel implements ActivityIndicatorChannel interface")
}
