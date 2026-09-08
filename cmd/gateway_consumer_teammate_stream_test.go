package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// A teammate run must ask the provider to stream. Without it a slow reasoning
// model holds a silent connection for the whole generation and
// ResponseHeaderTimeout kills the request before a single token is produced.
//
// The same test pins the other half of the contract: streaming here is for
// connection liveness only. The run is deliberately never registered with the
// channel manager, so HandleAgentEvent drops its chunks and nothing reaches a
// user incrementally — the task result keeps coming from the final RunResult.
func TestHandleTeammateMessageSchedulesStreamedRun(t *testing.T) {
	// The scheduler runs its RunFunc on its own goroutine, and the announce loop
	// schedules a second run after the teammate one, so a shared variable here is
	// written concurrently with the assertions below. Hand the requests over a
	// channel instead: no shared state, and a second run cannot clobber the first.
	scheduled := make(chan agent.RunRequest, 4)

	sched := scheduler.NewScheduler(
		scheduler.DefaultLanes(),
		scheduler.QueueConfig{
			Mode:          scheduler.QueueModeQueue,
			Cap:           1,
			Drop:          scheduler.DropOld,
			MaxConcurrent: 1,
		},
		func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
			select {
			case scheduled <- req:
			default:
			}
			return &agent.RunResult{Content: "member deliverable"}, nil
		},
	)
	defer sched.Stop()

	channelMgr := channels.NewManager(nil)
	deps := &ConsumerDeps{
		Cfg:        &config.Config{},
		Sched:      sched,
		ChannelMgr: channelMgr,
	}
	// handleTeammateMessage hands the announce loop to a background goroutine that
	// keeps calling Schedule after this function returns. Drain it before the
	// deferred Stop above runs, mirroring the shutdown order the gateway itself
	// uses (gateway_consumer.go waits on BgWg; sched.Stop is an outer defer in
	// gateway.go). Without this the teardown races the announce loop inside the
	// lane's WaitGroup — Submit's Add against Stop's Wait — and -race fails the
	// test intermittently.
	defer deps.BgWg.Wait()

	msg := bus.InboundMessage{
		Channel:  tools.ChannelSystem,
		SenderID: "teammate:dashboard",
		AgentID:  "coder",
		Content:  "[Assigned task #1 (id: 00000000-0000-0000-0000-000000000001)]: build something",
		Metadata: map[string]string{
			tools.MetaOriginChannel: "telegram",
			tools.MetaOriginChatID:  "12345",
			tools.MetaFromAgent:     "brain",
			tools.MetaToAgent:       "coder",
		},
	}

	if !handleTeammateMessage(context.Background(), msg, deps) {
		t.Fatal("handleTeammateMessage() = false, want true for a teammate: message on the system channel")
	}

	var gotReq agent.RunRequest
	select {
	case gotReq = <-scheduled:
	case <-time.After(5 * time.Second):
		t.Fatal("teammate run was never scheduled")
	}

	if !gotReq.Stream {
		t.Error("teammate run requested a non-streamed provider call: a slow model then holds a silent " +
			"connection for the whole generation until ResponseHeaderTimeout kills it")
	}

	if delivered, last := channelMgr.InterimDeliverySnapshot(gotReq.RunID); delivered != 0 || last != "" {
		t.Errorf("teammate run is registered for channel delivery (delivered=%d, last=%q); "+
			"streamed chunks would reach a user incrementally", delivered, last)
	}
}
