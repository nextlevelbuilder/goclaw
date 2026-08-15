package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// TestApplyMediaFloor_NoMediaNoFloor: msg without media → debounceMs returned unchanged (0).
func TestApplyMediaFloor_NoMediaNoFloor(t *testing.T) {
	msg := bus.InboundMessage{SenderID: "user-1"}
	got := applyMediaFloor(0, msg)
	if got != 0 {
		t.Fatalf("applyMediaFloor(0, no-media) = %d, want 0", got)
	}
}

// TestApplyMediaFloor_MediaAppliesFloorWhenDisabled — Rule #2 happy path.
func TestApplyMediaFloor_MediaAppliesFloorWhenDisabled(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "user-1",
		Media:    []bus.MediaFile{{Path: "/x"}},
	}
	got := applyMediaFloor(0, msg)
	if got != mediaDebounceFloorMs {
		t.Fatalf("applyMediaFloor(0, media) = %d, want %d", got, mediaDebounceFloorMs)
	}
}

// TestApplyMediaFloor_AgentOverrideBelowFloorHonored — red-team Rule #2 precedence.
// Floor fires only when the post-override delay is exactly 0. A 500ms override
// for a media-bearing message MUST be honored verbatim.
func TestApplyMediaFloor_AgentOverrideBelowFloorHonored(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "user-1",
		Media:    []bus.MediaFile{{Path: "/x"}},
	}
	got := applyMediaFloor(500, msg)
	if got != 500 {
		t.Fatalf("applyMediaFloor(500, media) = %d, want 500 (override honored; floor must not raise)", got)
	}
}

// TestApplyMediaFloor_MediaRespectsConfigWhenAboveFloor: cfg already above floor → unchanged.
func TestApplyMediaFloor_MediaRespectsConfigWhenAboveFloor(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "user-1",
		Media:    []bus.MediaFile{{Path: "/x"}},
	}
	got := applyMediaFloor(2000, msg)
	if got != 2000 {
		t.Fatalf("applyMediaFloor(2000, media) = %d, want 2000", got)
	}
}

// TestApplyMediaFloor_SystemSenderExempt — Rule #3 internal-publisher exemption.
func TestApplyMediaFloor_SystemSenderExempt(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "system:tool-echo",
		Media:    []bus.MediaFile{{Path: "/x"}},
	}
	got := applyMediaFloor(0, msg)
	if got != 0 {
		t.Fatalf("applyMediaFloor(0, system:) = %d, want 0 (system: sender must be exempt from floor)", got)
	}
}

// TestApplyMediaFloor_SubagentSenderExempt — Rule #3 internal-publisher exemption.
func TestApplyMediaFloor_SubagentSenderExempt(t *testing.T) {
	msg := bus.InboundMessage{
		SenderID: "subagent:research",
		Media:    []bus.MediaFile{{Path: "/x"}},
	}
	got := applyMediaFloor(0, msg)
	if got != 0 {
		t.Fatalf("applyMediaFloor(0, subagent:) = %d, want 0 (subagent: sender must be exempt from floor)", got)
	}
}

// TestResolveInboundDebounceDelay_FloorIsWiredInE2E — Rule #5.
// Exercises the OUTER resolveInboundDebounceDelay function (not the helper) with
// global debounce_ms=0, AgentStore=nil, media present, normal user sender.
// Asserts the floor fires. Locks in that any helper refactor still routes through
// the public function — prevents "floor exists but isn't wired" regressions.
func TestResolveInboundDebounceDelay_FloorIsWiredInE2E(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 0
	deps := &ConsumerDeps{Cfg: cfg}

	msg := bus.InboundMessage{
		SenderID: "user-1",
		AgentID:  "", // skip AgentStore lookup path entirely
		Media:    []bus.MediaFile{{Path: "/x"}},
	}

	got := resolveInboundDebounceDelay(context.Background(), msg, deps)
	want := time.Duration(mediaDebounceFloorMs) * time.Millisecond
	if got != want {
		t.Fatalf("resolveInboundDebounceDelay e2e = %v, want %v (floor not wired into outer function)", got, want)
	}
}

// TestResolveInboundDebounceDelay_NoMediaNoFloorE2E: outer function, no media, returns 0.
func TestResolveInboundDebounceDelay_NoMediaNoFloorE2E(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.InboundDebounceMs = 0
	deps := &ConsumerDeps{Cfg: cfg}

	msg := bus.InboundMessage{SenderID: "user-1"}
	got := resolveInboundDebounceDelay(context.Background(), msg, deps)
	if got != 0 {
		t.Fatalf("resolveInboundDebounceDelay no-media = %v, want 0", got)
	}
}

// TestIsSystemOrSubagentSender_PrefixMatch covers helper directly.
func TestIsSystemOrSubagentSender_PrefixMatch(t *testing.T) {
	cases := []struct {
		senderID string
		want     bool
	}{
		{"system:escalation", true},
		{"system:tool:sessions_send", true},
		{"subagent:research", true},
		{"user-1", false},
		{"", false},
		{"systemic", false},  // not the colon-prefix form
		{"subagentX", false}, // not the colon-prefix form
	}
	for _, c := range cases {
		if got := isSystemOrSubagentSender(c.senderID); got != c.want {
			t.Errorf("isSystemOrSubagentSender(%q) = %v, want %v", c.senderID, got, c.want)
		}
	}
}
