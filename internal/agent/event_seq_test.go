package agent

import (
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TestEmitStampsPerRunSeq tests that events emitted through the loop's emit
// path receive a monotonic per-run sequence starting at 1, in emit order.
func TestEmitStampsPerRunSeq(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}

	for i := 0; i < 5; i++ {
		loop.emit(AgentEvent{Type: protocol.ChatEventChunk, AgentID: "test-agent", RunID: "run-1"})
	}

	got := col.filter(protocol.ChatEventChunk)
	if len(got) != 5 {
		t.Fatalf("emitted %d events, want 5", len(got))
	}
	for i, e := range got {
		if want := int64(i + 1); e.Seq != want {
			t.Fatalf("event %d Seq = %d, want %d", i, e.Seq, want)
		}
	}
}

// TestEmitIndependentRunsSequences tests that two concurrent runs each start
// their per-run sequence at 1 (counters are keyed by run, not shared).
func TestEmitIndependentRunsSequences(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}

	loop.emit(AgentEvent{Type: protocol.AgentEventRunStarted, AgentID: "test-agent", RunID: "run-a"})
	loop.emit(AgentEvent{Type: protocol.AgentEventRunStarted, AgentID: "test-agent", RunID: "run-b"})
	loop.emit(AgentEvent{Type: protocol.ChatEventChunk, AgentID: "test-agent", RunID: "run-a"})

	if seq := col.filter(protocol.AgentEventRunStarted); seq[0].Seq != 1 || seq[1].Seq != 1 {
		t.Fatalf("run.started seqs = %d,%d, want 1,1", seq[0].Seq, seq[1].Seq)
	}
	if chunk := col.filter(protocol.ChatEventChunk); chunk[0].Seq != 2 {
		t.Fatalf("run-a second event Seq = %d, want 2", chunk[0].Seq)
	}
}

// TestEmitTerminalEventResetsPerRunSeq tests that after a run's terminal
// event its counter is released, so a subsequent run with the same RunID
// restarts at 1 (matching the RunTimelineRecorder cleanup pattern).
func TestEmitTerminalEventResetsPerRunSeq(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}

	loop.emit(AgentEvent{Type: protocol.ChatEventChunk, AgentID: "test-agent", RunID: "run-1"})
	loop.emit(AgentEvent{Type: protocol.AgentEventRunCompleted, AgentID: "test-agent", RunID: "run-1"})
	// New run reusing the same RunID — must restart from 1, not continue.
	loop.emit(AgentEvent{Type: protocol.AgentEventRunStarted, AgentID: "test-agent", RunID: "run-1"})

	chunks := col.filter(protocol.ChatEventChunk)
	if chunks[0].Seq != 1 {
		t.Fatalf("chunk before terminal Seq = %d, want 1", chunks[0].Seq)
	}
	started := col.filter(protocol.AgentEventRunStarted)
	if started[0].Seq != 1 {
		t.Fatalf("new run.started Seq = %d, want 1 (counter released by terminal event)", started[0].Seq)
	}
}

// TestEmitConcurrentSeqUnique tests that concurrent emits on the same run
// produce a gapless, unique sequence — the per-run counter is atomic.
func TestEmitConcurrentSeqUnique(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}

	const goroutines = 8
	const perGoroutine = 200
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				loop.emit(AgentEvent{Type: protocol.ChatEventChunk, AgentID: "test-agent", RunID: "run-race"})
			}
		}()
	}
	wg.Wait()

	events := col.events
	if len(events) != goroutines*perGoroutine {
		t.Fatalf("collected %d events, want %d", len(events), goroutines*perGoroutine)
	}
	seen := make(map[int64]bool, len(events))
	for _, e := range events {
		if e.Seq < 1 || e.Seq > int64(len(events)) {
			t.Fatalf("Seq %d out of range 1..%d", e.Seq, len(events))
		}
		if seen[e.Seq] {
			t.Fatalf("duplicate Seq %d under concurrency", e.Seq)
		}
		seen[e.Seq] = true
	}
}

// TestEmitNoSeqWithoutRunID tests that events not belonging to a run (empty
// RunID) carry no per-run sequence and pass through unchanged.
func TestEmitNoSeqWithoutRunID(t *testing.T) {
	col := &eventCollector{}
	loop := &Loop{id: "test-agent", onEvent: col.onEvent}

	loop.emit(AgentEvent{Type: protocol.AgentEventActivity, AgentID: "test-agent"})

	events := col.events
	if len(events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(events))
	}
	if events[0].Seq != 0 {
		t.Fatalf("event without RunID Seq = %d, want 0", events[0].Seq)
	}
}
