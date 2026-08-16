package reliability

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics exposes counters and histograms for the reliability layer. It is a
// dependency-light, always-safe recorder: callers may use it without any
// observer attached (all calls become no-ops), then a real exporter can be
// plugged in later (e.g. an OTel meter in the tracing exporter) by swapping
// decode the interface.
//
// The design mirrors the project's existing "spans + usage events" style: we
// record raw counts in-memory so a sink can flush them. No external
// dependency is introduced here.

// Metrics is a concurrency-safe accumulator for reliability counters.
type Metrics struct {
	llmRequests        atomic.Uint64
	llmSuccesses       atomic.Uint64
	llmRetries         atomic.Uint64
	llmRateLimited     atomic.Uint64
	llmServerErrors    atomic.Uint64
	llmTimeouts        atomic.Uint64
	llmStreamStalls    atomic.Uint64
	llmLoop            atomic.Uint64
	llmRepeatedTool    atomic.Uint64
	llmEmptyOutput     atomic.Uint64
	llmPremature       atomic.Uint64
	agentRecovered     atomic.Uint64
	agentContinued     atomic.Uint64
	prematureCompleted atomic.Uint64
	loopDetected       atomic.Uint64

	// latency accumulation (nanoseconds) — cheap histogram substitute.
	llmLatencyNanos atomic.Uint64
	llmLatencyCount atomic.Uint64

	// guards swap-based flush so two concurrent Flush calls never both emit
	// and reset the same counters.
	flushMu sync.Mutex
}

// NewMetrics returns an initialized Metrics recorder.
func NewMetrics() *Metrics { return &Metrics{} }

// RecordLLMRequest increments the total LLM request counter.
func (m *Metrics) RecordLLMRequest() { m.llmRequests.Add(1) }

// RecordLLMSuccess increments the success counter.
func (m *Metrics) RecordLLMSuccess() { m.llmSuccesses.Add(1) }

// RecordLLMRetry increments the retry counter.
func (m *Metrics) RecordLLMRetry() { m.llmRetries.Add(1) }

// RecordLLMRateLimited increments the 429/rate-limit counter.
func (m *Metrics) RecordLLMRateLimited() { m.llmRateLimited.Add(1) }

// RecordLLMServerError increments the 5xx counter.
func (m *Metrics) RecordLLMServerError() { m.llmServerErrors.Add(1) }

// RecordLLMTimeout increments the timeout counter.
func (m *Metrics) RecordLLMTimeout() { m.llmTimeouts.Add(1) }

// RecordLLMStreamStall increments the stream-stall counter.
func (m *Metrics) RecordLLMStreamStall() { m.llmStreamStalls.Add(1) }

// RecordLLMLoop increments the model-looping counter. Called when the tool-loop
// detector hits the critical level and the run is force-stopped.
func (m *Metrics) RecordLLMLoop() { m.llmLoop.Add(1) }

// RecordLLMRepeatedToolCall increments the repeated-tool-call counter. Called
// when the tool-loop detector hits the warning level (repeated same args +
// same result, or same result with different args).
func (m *Metrics) RecordLLMRepeatedToolCall() { m.llmRepeatedTool.Add(1) }

// RecordLLMEmptyOutput increments the empty-output counter. Called when the
// model returns an empty or fallback-only reply after nudges are exhausted.
func (m *Metrics) RecordLLMEmptyOutput() { m.llmEmptyOutput.Add(1) }

// RecordLLMPrematureCompletion increments the premature-completion counter.
// Called when the run finishes without a deliverable despite tool usage.
func (m *Metrics) RecordLLMPrematureCompletion() { m.llmPremature.Add(1) }

// RecordLLMLatency records a request latency. It maintains a running total so
// a sink can compute averages; callers wanting a real histogram should plug a
// proper meter into the sink.
func (m *Metrics) RecordLLMLatency(d time.Duration) {
	m.llmLatencyNanos.Add(uint64(d))
	m.llmLatencyCount.Add(1)
}

// RecordAgentRecovered increments the run recovery counter.
func (m *Metrics) RecordAgentRecovered() { m.agentRecovered.Add(1) }

// RecordAgentContinued increments the run continuation counter.
func (m *Metrics) RecordAgentContinued() { m.agentContinued.Add(1) }

// RecordPrematureCompletion increments the premature-completion counter.
func (m *Metrics) RecordPrematureCompletion() { m.prematureCompleted.Add(1) }

// RecordLoopDetected increments the loop-detection counter.
func (m *Metrics) RecordLoopDetected() { m.loopDetected.Add(1) }

// Snapshot is an immutable point-in-time view of the counters.
type Snapshot struct {
	LLMRequests             uint64
	LLMSuccesses            uint64
	LLMRetries              uint64
	LLMRateLimited          uint64
	LLMServerErrors         uint64
	LLMTimeouts             uint64
	LLMStreamStalls         uint64
	LLMLoop                 uint64
	LLMRepeatedToolCalls    uint64
	LLMEmptyOutputs         uint64
	LLMPrematureCompletions uint64
	LLMLatencyNanos         uint64
	LLMLatencyCount         uint64
	AgentRecovered          uint64
	AgentContinued          uint64
	PrematureCompleted      uint64
	LoopDetected            uint64
}

// Take returns a consistent snapshot of all counters.
func (m *Metrics) Take() Snapshot {
	return Snapshot{
		LLMRequests:             m.llmRequests.Load(),
		LLMSuccesses:            m.llmSuccesses.Load(),
		LLMRetries:              m.llmRetries.Load(),
		LLMRateLimited:          m.llmRateLimited.Load(),
		LLMServerErrors:         m.llmServerErrors.Load(),
		LLMTimeouts:             m.llmTimeouts.Load(),
		LLMStreamStalls:         m.llmStreamStalls.Load(),
		LLMLoop:                 m.llmLoop.Load(),
		LLMRepeatedToolCalls:    m.llmRepeatedTool.Load(),
		LLMEmptyOutputs:         m.llmEmptyOutput.Load(),
		LLMPrematureCompletions: m.llmPremature.Load(),
		LLMLatencyNanos:         m.llmLatencyNanos.Load(),
		LLMLatencyCount:         m.llmLatencyCount.Load(),
		AgentRecovered:          m.agentRecovered.Load(),
		AgentContinued:          m.agentContinued.Load(),
		PrematureCompleted:      m.prematureCompleted.Load(),
		LoopDetected:            m.loopDetected.Load(),
	}
}

// AvgLLMLatency returns the average request latency in nanoseconds, or 0 when
// no samples exist.
func (m *Metrics) AvgLLMLatency() time.Duration {
	count := m.llmLatencyCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(m.llmLatencyNanos.Load() / count)
}

// Default sink is a no-op exporter that simply drops snapshots. Applications
// can replace it by re-implementing the Sink interface and registering.
type Sink interface {
	// Emit receives periodically-accumulated snapshots.
	Emit(s Snapshot)
}

// globalSink is stored as an atomic.Value so RegisterSink and Flush (which run
// on potentially different goroutines) never race on a plain field write. It
// always stores a sinkHolder so every Store holds the same concrete type (a
// requirement of atomic.Value).
var globalSink atomic.Value // holds sinkHolder

// sinkHolder boxes a Sink so the atomic.Value sees one concrete type no matter
// which sink implementation is registered.
type sinkHolder struct{ s Sink }

type nopSink struct{}

func (nopSink) Emit(Snapshot) {}

func currentSink() Sink {
	v, _ := globalSink.Load().(sinkHolder)
	if v.s == nil {
		return nopSink{}
	}
	return v.s
}

// RegisterSink sets the single global sink. It is safe to call while recorders
// are running (swaps are atomic across goroutines).
func RegisterSink(s Sink) {
	if s == nil {
		s = nopSink{}
	}
	globalSink.Store(sinkHolder{s})
}

// Reset zeroes all counters via per-counter atomic Swap, so values that
// increment between the swap read and now are preserved in the returned
// snapshot rather than silently lost.
func (m *Metrics) resetInto(s *Snapshot) {
	s.LLMRequests = m.llmRequests.Swap(0)
	s.LLMSuccesses = m.llmSuccesses.Swap(0)
	s.LLMRetries = m.llmRetries.Swap(0)
	s.LLMRateLimited = m.llmRateLimited.Swap(0)
	s.LLMServerErrors = m.llmServerErrors.Swap(0)
	s.LLMTimeouts = m.llmTimeouts.Swap(0)
	s.LLMStreamStalls = m.llmStreamStalls.Swap(0)
	s.LLMLoop = m.llmLoop.Swap(0)
	s.LLMRepeatedToolCalls = m.llmRepeatedTool.Swap(0)
	s.LLMEmptyOutputs = m.llmEmptyOutput.Swap(0)
	s.LLMPrematureCompletions = m.llmPremature.Swap(0)
	s.LLMLatencyNanos = m.llmLatencyNanos.Swap(0)
	s.LLMLatencyCount = m.llmLatencyCount.Swap(0)
	s.AgentRecovered = m.agentRecovered.Swap(0)
	s.AgentContinued = m.agentContinued.Swap(0)
	s.PrematureCompleted = m.prematureCompleted.Swap(0)
	s.LoopDetected = m.loopDetected.Swap(0)
}

// Flush atomically drains the counters into a snapshot and emits it to the
// registered sink, so a periodic flusher compresses totals into monotonic
// deltas. Each counter uses Swap(0), so increments arriving after the drain
// are retained for the next flush instead of being erased. flushMu serializes
// concurrent Flush calls so two flushers never emit competing resets.
func (m *Metrics) Flush() {
	m.flushMu.Lock()
	defer m.flushMu.Unlock()
	var s Snapshot
	m.resetInto(&s)
	currentSink().Emit(s)
}
