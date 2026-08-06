package receiptexport

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileSink writes receipts as JSON Lines and keeps the latest checkpoint in its
// own file, so evidence can be collected with no infrastructure at all.
//
// It is deliberately the simplest thing that works, and it is not sufficient on
// its own. A checkpoint written next to the receipts it anchors is deletable by
// whoever deletes the receipts. To be worth something the checkpoint has to
// reach somewhere the operator cannot quietly rewrite: a build log, an object
// store with retention, or the commit that the run produced. CheckpointPath is
// separate from ReceiptPath so that publishing step is easy to bolt on.
type FileSink struct {
	mu             sync.Mutex
	receiptPath    string
	checkpointPath string
}

// NewFileSink creates the parent directories and returns a sink.
func NewFileSink(receiptPath, checkpointPath string) (*FileSink, error) {
	if receiptPath == "" || checkpointPath == "" {
		return nil, fmt.Errorf("receipt and checkpoint paths are required")
	}
	for _, path := range []string{receiptPath, checkpointPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create directory for %s: %w", path, err)
		}
	}
	return &FileSink{receiptPath: receiptPath, checkpointPath: checkpointPath}, nil
}

// WriteReceipt appends one receipt as a JSON line.
func (s *FileSink) WriteReceipt(_ context.Context, receipt SpanReceipt) error {
	line, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal receipt: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := os.OpenFile(s.receiptPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open receipt file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append receipt: %w", err)
	}
	// Durability matters more than throughput here: a receipt lost to the page
	// cache on a crash is indistinguishable from one that was never signed.
	return file.Sync()
}

// WriteCheckpoint replaces the checkpoint file. Only the latest matters, since
// each one commits to the full count and terminal hash.
func (s *FileSink) WriteCheckpoint(_ context.Context, checkpoint Checkpoint) error {
	encoded, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Write and rename, so a crash mid-write cannot leave a truncated anchor
	// that would fail verification of an otherwise honest run.
	temp := s.checkpointPath + ".tmp"
	if err := os.WriteFile(temp, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	if err := os.Rename(temp, s.checkpointPath); err != nil {
		return fmt.Errorf("replace checkpoint: %w", err)
	}
	return nil
}

// LoadBundle reads receipts and the checkpoint back for verification.
func LoadBundle(receiptPath, checkpointPath string) ([]SpanReceipt, *Checkpoint, error) {
	raw, err := os.ReadFile(receiptPath) // #nosec G304 -- operator-supplied evidence path
	if err != nil {
		return nil, nil, fmt.Errorf("read receipts: %w", err)
	}

	var receipts []SpanReceipt
	for _, line := range splitLines(raw) {
		var receipt SpanReceipt
		if err := json.Unmarshal(line, &receipt); err != nil {
			return nil, nil, fmt.Errorf("parse receipt: %w", err)
		}
		receipts = append(receipts, receipt)
	}

	var checkpoint *Checkpoint
	if checkpointPath != "" {
		rawCheckpoint, err := os.ReadFile(checkpointPath) // #nosec G304 -- operator-supplied evidence path
		if err != nil {
			return nil, nil, fmt.Errorf("read checkpoint: %w", err)
		}
		var parsed Checkpoint
		if err := json.Unmarshal(rawCheckpoint, &parsed); err != nil {
			return nil, nil, fmt.Errorf("parse checkpoint: %w", err)
		}
		checkpoint = &parsed
	}

	return receipts, checkpoint, nil
}

func splitLines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range raw {
		if b != '\n' {
			continue
		}
		if i > start {
			lines = append(lines, raw[start:i])
		}
		start = i + 1
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	return lines
}
