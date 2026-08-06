package receiptexport

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// memSink captures output and can be told to fail, so tests can prove the
// exporter survives a sink that is down without taking the run with it.
type memSink struct {
	mu          sync.Mutex
	receipts    []SpanReceipt
	checkpoints []Checkpoint
	failWrites  bool
}

func (s *memSink) WriteReceipt(_ context.Context, r SpanReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWrites {
		return fmt.Errorf("sink unavailable")
	}
	s.receipts = append(s.receipts, r)
	return nil
}

func (s *memSink) WriteCheckpoint(_ context.Context, c Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoints = append(s.checkpoints, c)
	return nil
}

func (s *memSink) snapshot() ([]SpanReceipt, []Checkpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SpanReceipt{}, s.receipts...), append([]Checkpoint{}, s.checkpoints...)
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestExporter(t *testing.T, cfg Config) (*Exporter, *memSink, *SigningKey) {
	t.Helper()
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	sink := &memSink{}
	cfg.Key = key
	cfg.Sink = sink
	if cfg.Logger == nil {
		cfg.Logger = quietLogger()
	}
	exp, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return exp, sink, key
}

func TestExporterSignsAndChainsSpans(t *testing.T) {
	exp, sink, key := newTestExporter(t, Config{})
	ctx := context.Background()

	exp.ExportSpans(ctx, []store.SpanData{testSpan("a"), testSpan("b"), testSpan("c")})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	receipts, checkpoints := sink.snapshot()
	if len(receipts) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(receipts))
	}
	if len(checkpoints) != 1 {
		t.Fatalf("expected a final checkpoint, got %d", len(checkpoints))
	}

	report, err := VerifyReceipts(receipts, &checkpoints[0], testKeyring(t, key))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("exported evidence should verify: %+v", report)
	}
}

func TestExporterFiltersBySpanType(t *testing.T) {
	exp, sink, _ := newTestExporter(t, Config{SpanTypes: []string{"tool_call"}})
	ctx := context.Background()

	llm := testSpan("llm")
	llm.SpanType = "llm_call"

	exp.ExportSpans(ctx, []store.SpanData{testSpan("tool"), llm})
	_ = exp.Shutdown(ctx)

	receipts, _ := sink.snapshot()
	if len(receipts) != 1 {
		t.Fatalf("expected only the tool_call span, got %d receipts", len(receipts))
	}
	if receipts[0].Payload.SpanType != "tool_call" {
		t.Fatalf("wrong span signed: %s", receipts[0].Payload.SpanType)
	}
}

// ExportSpans returns nothing by interface contract, so a broken sink must be
// counted and logged rather than propagated. A signing problem must never take
// down a run that was otherwise fine.
func TestSinkFailureIsCountedNotPropagated(t *testing.T) {
	exp, sink, key := newTestExporter(t, Config{})
	ctx := context.Background()

	sink.failWrites = true
	exp.ExportSpans(ctx, []store.SpanData{testSpan("a"), testSpan("b")})
	sink.failWrites = false
	exp.ExportSpans(ctx, []store.SpanData{testSpan("c")})

	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	coverage := exp.Coverage()
	if coverage.SpansReceived != 3 {
		t.Fatalf("expected 3 received, got %d", coverage.SpansReceived)
	}
	if coverage.SpansFailed != 2 {
		t.Fatalf("expected 2 failures, got %d", coverage.SpansFailed)
	}
	if coverage.SpansSigned != 1 {
		t.Fatalf("expected 1 signed, got %d", coverage.SpansSigned)
	}

	// The surviving receipt must still verify, and the checkpoint must carry
	// the failure count so a reader sees the gap rather than a clean run.
	receipts, checkpoints := sink.snapshot()
	report, _ := VerifyReceipts(receipts, &checkpoints[0], testKeyring(t, key))
	if !report.Valid {
		t.Fatalf("the receipts that did land should verify: %+v", report)
	}
	if report.Coverage == nil || report.Coverage.SpansFailed != 2 {
		t.Fatal("the checkpoint must carry the failure count")
	}
}

// A failed write must not advance the chain head, or a transient sink error
// would leave a permanent unexplained break.
func TestFailedWriteDoesNotBreakTheChain(t *testing.T) {
	exp, sink, key := newTestExporter(t, Config{})
	ctx := context.Background()

	exp.ExportSpans(ctx, []store.SpanData{testSpan("a")})
	sink.failWrites = true
	exp.ExportSpans(ctx, []store.SpanData{testSpan("lost")})
	sink.failWrites = false
	exp.ExportSpans(ctx, []store.SpanData{testSpan("c")})
	_ = exp.Shutdown(ctx)

	receipts, checkpoints := sink.snapshot()
	report, _ := VerifyReceipts(receipts, &checkpoints[0], testKeyring(t, key))
	if !report.ChainIntact {
		t.Fatalf("chain should remain intact across a failed write: %+v", report.Results)
	}
}

func TestCoverageNeverClaimsRuntimeCompleteness(t *testing.T) {
	exp, _, _ := newTestExporter(t, Config{})
	coverage := exp.Coverage()
	if coverage.Note == "" {
		t.Fatal("coverage must carry a note distinguishing exporter reach from runtime activity")
	}
}

func TestInterimCheckpoints(t *testing.T) {
	exp, sink, _ := newTestExporter(t, Config{CheckpointEvery: 2})
	ctx := context.Background()

	exp.ExportSpans(ctx, []store.SpanData{testSpan("a"), testSpan("b")})
	_, checkpoints := sink.snapshot()
	if len(checkpoints) != 1 {
		t.Fatalf("expected an interim checkpoint after 2 receipts, got %d", len(checkpoints))
	}

	exp.ExportSpans(ctx, []store.SpanData{testSpan("c"), testSpan("d")})
	_ = exp.Shutdown(ctx)

	_, checkpoints = sink.snapshot()
	if len(checkpoints) != 3 {
		t.Fatalf("expected two interim checkpoints plus a final one, got %d", len(checkpoints))
	}
}

func TestSpansAfterShutdownAreNotSigned(t *testing.T) {
	exp, sink, _ := newTestExporter(t, Config{})
	ctx := context.Background()

	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	exp.ExportSpans(ctx, []store.SpanData{testSpan("late")})

	receipts, _ := sink.snapshot()
	if len(receipts) != 0 {
		t.Fatal("a span arriving after the final checkpoint must not be silently appended")
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	exp, sink, _ := newTestExporter(t, Config{})
	ctx := context.Background()

	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown should be a no-op, got %v", err)
	}
	_, checkpoints := sink.snapshot()
	if len(checkpoints) != 1 {
		t.Fatalf("expected exactly one final checkpoint, got %d", len(checkpoints))
	}
}

func TestNewRequiresKeyAndSink(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("a key is required")
	}
	key, _ := GenerateSigningKey()
	if _, err := New(Config{Key: key}); err == nil {
		t.Fatal("a sink is required")
	}
}

func TestConcurrentExportKeepsTheChainConsistent(t *testing.T) {
	exp, sink, key := newTestExporter(t, Config{})
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			exp.ExportSpans(ctx, []store.SpanData{testSpan(fmt.Sprintf("s%d", n))})
		}(i)
	}
	wg.Wait()
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	receipts, checkpoints := sink.snapshot()
	if len(receipts) != 8 {
		t.Fatalf("expected 8 receipts, got %d", len(receipts))
	}
	report, _ := VerifyReceipts(receipts, &checkpoints[0], testKeyring(t, key))
	if !report.Valid {
		t.Fatalf("concurrent export must still produce a verifiable chain: %+v", report)
	}
}

func TestFileSinkRoundTrip(t *testing.T) {
	dir := t.TempDir()
	receiptPath := dir + "/receipts.jsonl"
	checkpointPath := dir + "/checkpoint.json"

	sink, err := NewFileSink(receiptPath, checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	exp, err := New(Config{Key: key, Sink: sink, Logger: quietLogger()})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	exp.ExportSpans(ctx, []store.SpanData{testSpan("a"), testSpan("b")})
	if err := exp.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// Verify from disk with a keyring holding only public material, which is
	// what an auditor would actually have.
	receipts, checkpoint, err := LoadBundle(receiptPath, checkpointPath)
	if err != nil {
		t.Fatal(err)
	}
	independent := NewKeyring(KeyringEntry{Kid: key.Kid, Alg: key.Alg, PublicKey: key.PublicKey})

	report, err := VerifyReceipts(receipts, checkpoint, independent)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("evidence read back from disk should verify: %+v", report)
	}
	if report.ReceiptsChecked != 2 {
		t.Fatalf("expected 2 receipts, got %d", report.ReceiptsChecked)
	}
}

func TestFileSinkDetectsTruncatedEvidenceFile(t *testing.T) {
	dir := t.TempDir()
	receiptPath := dir + "/receipts.jsonl"
	checkpointPath := dir + "/checkpoint.json"

	sink, _ := NewFileSink(receiptPath, checkpointPath)
	key, _ := GenerateSigningKey()
	exp, _ := New(Config{Key: key, Sink: sink, Logger: quietLogger()})

	ctx := context.Background()
	exp.ExportSpans(ctx, []store.SpanData{testSpan("a"), testSpan("b"), testSpan("c")})
	_ = exp.Shutdown(ctx)

	// Someone removes the last line of the evidence file. Every remaining
	// signature and internal link stays valid; only the anchor catches it.
	receipts, checkpoint, _ := LoadBundle(receiptPath, checkpointPath)
	truncated := receipts[:len(receipts)-1]

	independent := NewKeyring(KeyringEntry{Kid: key.Kid, Alg: key.Alg, PublicKey: key.PublicKey})
	report, _ := VerifyReceipts(truncated, checkpoint, independent)

	if report.SignaturesInvalid != 0 || !report.ChainIntact {
		t.Fatal("truncation leaves signatures and links intact, which is why the anchor is needed")
	}
	if report.Valid || report.CheckpointValid {
		t.Fatal("the checkpoint must reject a truncated evidence file")
	}
}
