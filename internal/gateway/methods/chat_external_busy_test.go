package methods

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestDispatchFirstTurnBehindExternalRunGetsQueuedAck proves Phase 7 closure item
// 3 at the real WS dispatch seam: when a run registered OUTSIDE the chat FIFO
// queue already owns the session — an ordinary inbound run (RunKind ""), a
// workflow finalize run, or a workflow recovery run — the FIRST WS turn on that
// session does NOT run immediately. It creates the reservation but the worker
// waits on the external run's Done, so Submit returns submitStartedWaiting and the
// turn takes the SAME queued-ack contract as an ordinary busy follow-up:
//
//   - an immediate structural {queued:true} RPC ack + queued lifecycle, BEFORE the
//     external run's Done closes,
//   - NO classify/run while the external run is still in flight,
//   - after the external run completes: the batch dequeues, classifies+runs
//     exactly once (running → completed), and emits NO second RPC response for the
//     turn's original request ID (the queued ack already claimed its latch).
//
// The three RunKinds are covered as a table because the reservation's initial-wait
// decision keys only on "is the session owned by a run outside this queue" — it
// must be identical regardless of the external run's kind.
func TestDispatchFirstTurnBehindExternalRunGetsQueuedAck(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runKind string
	}{
		{"ordinary-inbound", ""},
		{"workflow-finalize", agent.RunKindWorkflowFinalize},
		{"workflow-recovery", agent.RunKindWorkflowRecovery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Team Work classification disabled (nil) so the gate short-circuits and
			// dispatch exercises the pure queue/register/run lifecycle; a labeled
			// session store so the completed success path does not nil-deref on
			// GetLabel (it isolates the queued-ack contract from persistence).
			router := agent.NewRouter()
			m := NewChatMethods(router, labeledSessionStore{}, &config.Config{}, nil, nil)
			m.debouncer = newChatDebouncer(m.dispatchChatSends)
			sessionKey := "session-external-" + tc.name

			// An external run (registered directly on the router, NOT through the chat
			// FIFO queue) owns the session — exactly the state produced by an inbound
			// consumer run or a workflow finalize/recovery run.
			externalRunID := "external-" + tc.name
			_, extCancel := context.WithCancel(context.Background())
			_, _ = router.RegisterRunWithKind(context.Background(), externalRunID, sessionKey, "agent-1", tc.runKind, extCancel)

			// The first WS turn on this busy session. It creates the reservation but the
			// worker must wait on the external run's Done → submitStartedWaiting → queued
			// ack contract.
			followup := newDispatchLifecycleLoop(false)
			c, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.Nil, "u", 8)
			m.dispatchChatSends([]chatSendRequest{
				lifecycleRequest(c, "r1", followup, sessionKey, "first WS turn"),
			})

			// (a) Immediate queued ack, while the external run is still in flight.
			ack := readResponse(t, ch)
			if ack.ID != "r1" {
				t.Fatalf("queued ack ID = %q, want r1", ack.ID)
			}
			assertQueuedAck(t, ack)

			// (b) The WS turn must NOT classify/run while the external run still owns the
			// session — the worker is blocked on the external Done.
			select {
			case <-followup.entered:
				t.Fatal("WS turn ran while the external run still owned the session")
			case <-time.After(100 * time.Millisecond):
			}

			// (c) External run completes: releases the FIFO worker.
			router.UnregisterRun(externalRunID)

			// (d) The WS turn now dequeues and runs exactly once.
			select {
			case <-followup.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("WS turn never ran after the external run completed")
			}
			close(followup.release)

			resps, turns := drainFrames(t, ch, 400*time.Millisecond)

			if got := followup.ranTimes(); got != 1 {
				t.Fatalf("WS turn ran %d times; a queued turn must classify+run exactly once", got)
			}

			// (e) Running → completed lifecycle for the turn, linked to a real runId.
			ackTurnID, _ := ack.Payload.(map[string]any)["turnId"].(string)
			var running, completed *turnEvent
			completedCount := 0
			for i := range turns {
				if ackTurnID != "" && turns[i].turnID != ackTurnID {
					continue
				}
				switch turns[i].state {
				case protocol.ChatTurnRunning:
					running = &turns[i]
				case protocol.ChatTurnCompleted:
					completed = &turns[i]
					completedCount++
				}
			}
			if completedCount != 1 {
				t.Fatalf("expected exactly one completed lifecycle event, got %d: %+v", completedCount, turns)
			}
			if running == nil {
				t.Fatalf("no running lifecycle event for the WS turn: %+v", turns)
			}
			if running.runID == "" || running.runID != completed.runID {
				t.Fatalf("running/completed runId mismatch: running=%q completed=%q", running.runID, completed.runID)
			}

			// (f) No second RPC response for r1 — the queued ack was its single terminal RPC.
			for _, r := range resps {
				if r.ID == "r1" {
					t.Fatalf("unexpected second RPC response for r1 (queued ack must be the only one): %+v", r)
				}
			}

			waitChatReservationCleared(t, m.runQueue, sessionKey)
		})
	}
}
