package gateway

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// readEvent reads the next event frame from the client send buffer.
func readEvent(t *testing.T, client *Client) protocol.EventFrame {
	t.Helper()
	select {
	case raw := <-client.send:
		var evt protocol.EventFrame
		if err := json.Unmarshal(raw, &evt); err != nil {
			t.Fatalf("unmarshal event: %v", err)
		}
		return evt
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected event frame")
		return protocol.EventFrame{}
	}
}

// TestSendEventStampsConnectionSeq tests that frames without an explicit
// sequence are stamped with the per-connection monotonic counter.
func TestSendEventStampsConnectionSeq(t *testing.T) {
	server := NewServer(configForTest(), nil, nil, nil)
	client := NewClient(nil, server, "127.0.0.1")

	client.SendEvent(*protocol.NewEvent(protocol.EventHealth, map[string]any{"ok": true}))
	client.SendEvent(*protocol.NewEvent(protocol.EventTick, nil))

	if got := readEvent(t, client); got.Seq != 1 {
		t.Fatalf("first event Seq = %d, want 1", got.Seq)
	}
	if got := readEvent(t, client); got.Seq != 2 {
		t.Fatalf("second event Seq = %d, want 2", got.Seq)
	}
}

// TestSendEventPreservesPerRunSeq tests that a frame already carrying a
// per-run sequence (stamped by the agent loop emit path) keeps it untouched
// instead of being overwritten by the per-connection counter.
func TestSendEventPreservesPerRunSeq(t *testing.T) {
	server := NewServer(configForTest(), nil, nil, nil)
	client := NewClient(nil, server, "127.0.0.1")

	evt := protocol.NewEvent(protocol.EventAgent, map[string]any{"type": "chunk", "runId": "run-1"})
	evt.Seq = 7 // per-run seq from the loop emit path
	client.SendEvent(*evt)
	client.SendEvent(*protocol.NewEvent(protocol.EventHealth, map[string]any{"ok": true}))

	agentFrame := readEvent(t, client)
	if agentFrame.Seq != 7 {
		t.Fatalf("agent frame Seq = %d, want preserved 7", agentFrame.Seq)
	}
	// The connection counter must not have advanced for the stamped frame:
	// the next unstamped frame starts at 1.
	if next := readEvent(t, client); next.Seq != 1 {
		t.Fatalf("next frame Seq = %d, want 1 (counter skipped stamped frame)", next.Seq)
	}
}

// configForTest returns a minimal config for gateway tests that do not
// exercise auth (matches existing router_test usage).
func configForTest() *config.Config {
	return config.Default()
}
