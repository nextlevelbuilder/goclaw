package receiptexport

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Sink receives signed receipts and checkpoints. Implementations decide where
// evidence lands: a file, an object store, or a commit trailer.
//
// The sink is separated from the exporter because where the checkpoint goes is
// what determines whether any of this is worth having. A checkpoint written
// beside the database is deletable by the same person who deletes the
// receipts, so the anchor only means something once it reaches somewhere the
// operator cannot quietly rewrite.
type Sink interface {
	WriteReceipt(ctx context.Context, receipt SpanReceipt) error
	WriteCheckpoint(ctx context.Context, checkpoint Checkpoint) error
}

// Config configures the receipt exporter.
type Config struct {
	// Key signs receipts and checkpoints. Required.
	Key *SigningKey
	// Sink receives the output. Required.
	Sink Sink
	// CheckpointEvery emits an interim checkpoint after this many receipts.
	// Zero means only emit one at Shutdown, which leaves everything after the
	// last checkpoint truncatable if the process is killed.
	CheckpointEvery uint64
	// SpanTypes filters which spans are signed. Empty signs everything.
	// Signing only "tool_call" is the common case and keeps the evidence set
	// small enough to review by hand.
	SpanTypes []string
	// Logger defaults to slog.Default().
	Logger *slog.Logger
}

// Exporter signs exported spans and implements tracing.SpanExporter.
//
// It never blocks the collector and never panics into it. The SpanExporter
// contract has ExportSpans return nothing, so an exporter cannot report a
// failure upward; the only honest options are to log, count, and keep the
// counts in the checkpoint so a verifier sees that something went wrong.
type Exporter struct {
	key             *SigningKey
	sink            Sink
	checkpointEvery uint64
	spanTypes       map[string]struct{}
	log             *slog.Logger

	mu           sync.Mutex
	sequence     uint64
	previousHash string
	coverage     Coverage
	sinceLast    uint64
	closed       bool
}

// New builds a receipt exporter.
func New(cfg Config) (*Exporter, error) {
	if cfg.Key == nil {
		return nil, fmt.Errorf("signing key is required")
	}
	if cfg.Sink == nil {
		return nil, fmt.Errorf("sink is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	var types map[string]struct{}
	if len(cfg.SpanTypes) > 0 {
		types = make(map[string]struct{}, len(cfg.SpanTypes))
		for _, t := range cfg.SpanTypes {
			types[t] = struct{}{}
		}
	}
	return &Exporter{
		key:             cfg.Key,
		sink:            cfg.Sink,
		checkpointEvery: cfg.CheckpointEvery,
		spanTypes:       types,
		log:             logger,
		coverage: Coverage{
			Note: "Counts spans this exporter received. Spans dropped by the collector under buffer pressure never reach an exporter and are not counted here.",
		},
	}, nil
}

// ExportSpans signs and emits receipts. It implements tracing.SpanExporter.
//
// Returns nothing by interface contract, so every failure is logged and
// counted rather than propagated. A signing failure must never take down a
// run that was otherwise fine.
func (e *Exporter) ExportSpans(ctx context.Context, spans []store.SpanData) {
	if e == nil || len(spans) == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		e.log.Warn("receiptexport: spans arrived after shutdown, not signed", "count", len(spans))
		return
	}

	for _, span := range spans {
		if !e.wants(span) {
			continue
		}
		e.coverage.SpansReceived++

		receipt, hash, err := e.signLocked(span)
		if err != nil {
			e.coverage.SpansFailed++
			e.log.Error("receiptexport: could not sign span",
				"span_id", span.ID, "span_type", span.SpanType, "error", err)
			continue
		}
		if err := e.sink.WriteReceipt(ctx, receipt); err != nil {
			e.coverage.SpansFailed++
			e.log.Error("receiptexport: could not write receipt",
				"span_id", span.ID, "error", err)
			continue
		}

		// Advance only once the receipt has actually landed. Advancing before
		// the write would leave the next receipt chaining to a hash nobody
		// holds, turning a transient sink error into a permanent break that
		// looks identical to tampering.
		e.previousHash = hash
		e.sequence++
		e.coverage.SpansSigned++
		e.sinceLast++
	}

	if e.checkpointEvery > 0 && e.sinceLast >= e.checkpointEvery {
		if err := e.checkpointLocked(ctx); err != nil {
			e.log.Error("receiptexport: could not write interim checkpoint", "error", err)
		}
	}
}

// signLocked builds and signs a receipt against the current chain head and
// returns it with its hash. Caller must hold the mutex.
//
// Deliberately free of side effects: it does not advance the chain head or the
// sequence. The caller does that only after the receipt has been durably
// written, so a failed write leaves the next receipt chaining to the last one
// that actually landed.
func (e *Exporter) signLocked(span store.SpanData) (SpanReceipt, string, error) {
	payload, err := PayloadFromSpan(span, e.sequence, e.previousHash, time.Now().UTC())
	if err != nil {
		return SpanReceipt{}, "", err
	}
	receipt, err := SignReceipt(payload, e.key)
	if err != nil {
		return SpanReceipt{}, "", err
	}
	hash, err := ReceiptHash(receipt)
	if err != nil {
		return SpanReceipt{}, "", err
	}
	return receipt, hash, nil
}

func (e *Exporter) wants(span store.SpanData) bool {
	if e.spanTypes == nil {
		return true
	}
	_, ok := e.spanTypes[span.SpanType]
	return ok
}

// Checkpoint emits a completeness anchor immediately.
//
// Publish the result somewhere the operator cannot rewrite. A checkpoint
// sitting beside the receipts it anchors proves very little.
func (e *Exporter) Checkpoint(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.checkpointLocked(ctx)
}

func (e *Exporter) checkpointLocked(ctx context.Context) error {
	payload := CheckpointPayload{
		Type:                CheckpointType,
		Version:             1,
		Sequence:            e.sequence,
		ReceiptCount:        e.coverage.SpansSigned,
		TerminalReceiptHash: e.previousHash,
		Coverage:            e.coverage,
		SignedAt:            time.Now().UTC().Format(time.RFC3339Nano),
	}
	checkpoint, err := SignCheckpoint(payload, e.key)
	if err != nil {
		return fmt.Errorf("sign checkpoint: %w", err)
	}
	if err := e.sink.WriteCheckpoint(ctx, checkpoint); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	e.sinceLast = 0
	return nil
}

// Shutdown emits a final checkpoint. It implements tracing.SpanExporter.
//
// This is the anchor that makes truncation detectable for the whole run, so a
// failure here is returned rather than swallowed.
func (e *Exporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	if err := e.checkpointLocked(ctx); err != nil {
		return fmt.Errorf("final checkpoint: %w", err)
	}
	return nil
}

// Coverage reports what this exporter has seen so far.
func (e *Exporter) Coverage() Coverage {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.coverage
}
