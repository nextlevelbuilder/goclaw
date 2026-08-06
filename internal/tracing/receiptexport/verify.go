package receiptexport

import (
	"fmt"
	"time"
)

// ReceiptResult is the per-receipt outcome of verification.
//
// Signature validity and chain integrity are reported separately on purpose.
// A validly signed receipt pointing at the wrong predecessor means something
// was removed or reordered, which is a different failure from a forged
// signature and calls for a different response. Collapsing them into one
// boolean would hide the distinction that makes the chain worth having.
type ReceiptResult struct {
	Index          int    `json:"index"`
	SpanID         string `json:"span_id"`
	Sequence       uint64 `json:"sequence"`
	SignatureValid bool   `json:"signature_valid"`
	SignatureError string `json:"signature_error,omitempty"`
	ChainValid     bool   `json:"chain_valid"`
	ChainError     string `json:"chain_error,omitempty"`
}

// VerificationReport is the result of checking a set of receipts.
type VerificationReport struct {
	Type              string `json:"type"`
	ReceiptsChecked   int    `json:"receipts_checked"`
	SignaturesValid   int    `json:"signatures_valid"`
	SignaturesInvalid int    `json:"signatures_invalid"`
	ChainIntact       bool   `json:"chain_intact"`

	// CheckpointChecked is false when no anchor was supplied. Without one,
	// truncation is undetectable: remove the newest receipts and every
	// remaining link still verifies.
	CheckpointChecked bool   `json:"checkpoint_checked"`
	CheckpointValid   bool   `json:"checkpoint_valid"`
	CheckpointError   string `json:"checkpoint_error,omitempty"`

	Valid            bool            `json:"valid"`
	Coverage         *Coverage       `json:"coverage,omitempty"`
	Results          []ReceiptResult `json:"results"`
	Establishes      string          `json:"establishes"`
	DoesNotEstablish []string        `json:"does_not_establish"`
}

// VerifyReceipts checks signatures and chain links over an ordered sequence.
//
// A checkpoint is optional but strongly recommended. Without one the report
// says so via CheckpointChecked, and Valid can only mean "nothing I was given
// has been altered", never "nothing is missing".
func VerifyReceipts(receipts []SpanReceipt, checkpoint *Checkpoint, keyring *Keyring) (VerificationReport, error) {
	if keyring == nil {
		return VerificationReport{}, fmt.Errorf("keyring is required")
	}

	report := VerificationReport{
		Type:            "goclaw.receipt_verification.v1",
		ReceiptsChecked: len(receipts),
		ChainIntact:     true,
		Establishes:     "Each verified span was exported and signed by the named key while that key was trusted, and the verified sequence has not been altered.",
		DoesNotEstablish: []string{
			"It does not establish that these are all the spans the runtime produced. The collector drops spans under buffer pressure and an exporter can only sign what it receives.",
			"It does not establish that a tool call was correct, authorised, or safe.",
			"It does not establish that receipts signed by a compromised key were truthful.",
			"Without a checkpoint it does not establish that no receipts were removed from the end of the sequence.",
		},
	}

	previousHash := ""
	for index, receipt := range receipts {
		result := ReceiptResult{
			Index:    index,
			SpanID:   receipt.Payload.SpanID,
			Sequence: receipt.Payload.Sequence,
		}

		if receipt.Payload.Type != ReceiptType {
			result.SignatureError = fmt.Sprintf("unsupported receipt type %q", receipt.Payload.Type)
		} else {
			exportedAt, err := time.Parse(time.RFC3339Nano, receipt.Payload.ExportedAt)
			if err != nil {
				result.SignatureError = fmt.Sprintf("unparseable exported_at: %v", err)
			} else if err := verifySignature(signingDomain, receipt.Payload, receipt.Signature, keyring, exportedAt); err != nil {
				result.SignatureError = err.Error()
			} else {
				result.SignatureValid = true
				report.SignaturesValid++
			}
		}

		expected := ""
		if index > 0 {
			expected = previousHash
		}
		if receipt.Payload.PrevReceiptHash == expected {
			result.ChainValid = true
		} else {
			report.ChainIntact = false
			result.ChainError = fmt.Sprintf("expected prev_receipt_hash %q but found %q", expected, receipt.Payload.PrevReceiptHash)
		}

		// Chain from what is actually present, so one break does not cascade
		// into every later receipt reporting a failure it did not cause.
		hash, err := ReceiptHash(receipt)
		if err != nil {
			return VerificationReport{}, fmt.Errorf("hash receipt %d: %w", index, err)
		}
		previousHash = hash

		report.Results = append(report.Results, result)
	}

	report.SignaturesInvalid = report.ReceiptsChecked - report.SignaturesValid

	if checkpoint != nil {
		report.CheckpointChecked = true
		if err := verifyCheckpoint(*checkpoint, receipts, previousHash, keyring); err != nil {
			report.CheckpointError = err.Error()
		} else {
			report.CheckpointValid = true
			cov := checkpoint.Payload.Coverage
			report.Coverage = &cov
		}
	}

	report.Valid = report.SignaturesInvalid == 0 &&
		report.ChainIntact &&
		report.CheckpointChecked &&
		report.CheckpointValid

	return report, nil
}

// verifyCheckpoint closes the truncation hole a chain alone cannot.
func verifyCheckpoint(checkpoint Checkpoint, receipts []SpanReceipt, terminalHash string, keyring *Keyring) error {
	if checkpoint.Payload.Type != CheckpointType {
		return fmt.Errorf("unsupported checkpoint type %q", checkpoint.Payload.Type)
	}
	signedAt, err := time.Parse(time.RFC3339Nano, checkpoint.Payload.SignedAt)
	if err != nil {
		return fmt.Errorf("unparseable signed_at: %w", err)
	}
	if err := verifySignature(checkpointDomain, checkpoint.Payload, checkpoint.Signature, keyring, signedAt); err != nil {
		return fmt.Errorf("checkpoint signature: %w", err)
	}
	if uint64(len(receipts)) != checkpoint.Payload.ReceiptCount {
		return fmt.Errorf("checkpoint expects %d receipts but the bundle holds %d",
			checkpoint.Payload.ReceiptCount, len(receipts))
	}
	// An empty bundle is only acceptable when the anchor says the run produced
	// nothing. Otherwise handing someone zero receipts would verify clean.
	if len(receipts) == 0 {
		if checkpoint.Payload.TerminalReceiptHash != "" {
			return fmt.Errorf("checkpoint names a terminal receipt but the bundle is empty")
		}
		return nil
	}
	if checkpoint.Payload.TerminalReceiptHash != terminalHash {
		return fmt.Errorf("checkpoint terminal hash %q does not match the last receipt %q",
			checkpoint.Payload.TerminalReceiptHash, terminalHash)
	}
	return nil
}
