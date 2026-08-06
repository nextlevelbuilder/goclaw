package receiptexport

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var testTime = time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

func testKeyring(t *testing.T, key *SigningKey) *Keyring {
	t.Helper()
	return NewKeyring(key.PublicEntry())
}

func testSpan(name string) store.SpanData {
	return store.SpanData{
		ID:            uuid.New(),
		TraceID:       uuid.New(),
		TenantID:      uuid.New(),
		SpanType:      "tool_call",
		Name:          name,
		Status:        "ok",
		ToolName:      "shell",
		ToolCallID:    "call_" + name,
		StartTime:     testTime,
		InputPreview:  "ls -la",
		OutputPreview: "total 0",
	}
}

// signChain builds n chained receipts, so tests can attack a realistic sequence
// rather than a single artifact.
func signChain(t *testing.T, key *SigningKey, n int) ([]SpanReceipt, string) {
	t.Helper()
	receipts := make([]SpanReceipt, 0, n)
	prev := ""
	for i := 0; i < n; i++ {
		payload, err := PayloadFromSpan(testSpan(string(rune('a'+i))), uint64(i), prev, testTime.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		receipt, err := SignReceipt(payload, key)
		if err != nil {
			t.Fatalf("sign %d: %v", i, err)
		}
		hash, err := ReceiptHash(receipt)
		if err != nil {
			t.Fatalf("hash %d: %v", i, err)
		}
		receipts = append(receipts, receipt)
		prev = hash
	}
	return receipts, prev
}

func checkpointFor(t *testing.T, key *SigningKey, receipts []SpanReceipt, terminal string) *Checkpoint {
	t.Helper()
	cp, err := SignCheckpoint(CheckpointPayload{
		Type:                CheckpointType,
		Version:             1,
		Sequence:            uint64(len(receipts)),
		ReceiptCount:        uint64(len(receipts)),
		TerminalReceiptHash: terminal,
		Coverage:            Coverage{SpansReceived: uint64(len(receipts)), SpansSigned: uint64(len(receipts))},
		SignedAt:            testTime.Add(time.Hour).Format(time.RFC3339Nano),
	}, key)
	if err != nil {
		t.Fatalf("sign checkpoint: %v", err)
	}
	return &cp
}

func TestCanonicalJSONIsOrderIndependent(t *testing.T) {
	a, err := canonicalJSON(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatal(err)
	}
	b, err := canonicalJSON(map[string]any{"a": 2, "b": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("canonical form depends on insertion order: %q vs %q", a, b)
	}
	if string(a) != `{"a":2,"b":1}` {
		t.Fatalf("unexpected canonical form %q", a)
	}
}

func TestCanonicalJSONPreservesArrayOrder(t *testing.T) {
	a, _ := canonicalJSON([]int{1, 2})
	b, _ := canonicalJSON([]int{2, 1})
	if string(a) == string(b) {
		t.Fatal("array order must be significant")
	}
}

func TestKeyIDDerivesFromPublicKey(t *testing.T) {
	key, err := GenerateSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if KeyIDFromPublicKey(key.PublicKey) != key.Kid {
		t.Fatal("kid must be derivable from the public key it names")
	}
}

func TestKeyringEntryLyingAboutItsKeyIsRejected(t *testing.T) {
	key, _ := GenerateSigningKey()
	other, _ := GenerateSigningKey()

	lying := NewKeyring(KeyringEntry{Kid: key.Kid, Alg: SigningAlg, PublicKey: other.PublicKey})
	receipts, terminal := signChain(t, key, 1)

	report, err := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), lying)
	if err != nil {
		t.Fatal(err)
	}
	if report.Results[0].SignatureValid {
		t.Fatal("a kid that does not match its public key must not verify")
	}
	if !strings.Contains(report.Results[0].SignatureError, "does not match the public key") {
		t.Fatalf("unexpected error: %s", report.Results[0].SignatureError)
	}
}

func TestValidChainWithCheckpointVerifies(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 4)

	report, err := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), testKeyring(t, key))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid {
		t.Fatalf("expected valid, got %+v", report)
	}
	if !report.CheckpointValid || report.SignaturesInvalid != 0 || !report.ChainIntact {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestTamperedPayloadFailsSignature(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 2)
	receipts[1].Payload.ToolName = "rm"

	report, _ := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), testKeyring(t, key))
	if report.Results[1].SignatureValid {
		t.Fatal("editing a signed field must break the signature")
	}
	if report.Valid {
		t.Fatal("report must not be valid")
	}
}

func TestForgedSignatureUnderTrustedKidFails(t *testing.T) {
	key, _ := GenerateSigningKey()
	attacker, _ := GenerateSigningKey()

	payload, _ := PayloadFromSpan(testSpan("x"), 0, "", testTime)
	forged, _ := SignReceipt(payload, attacker)
	forged.Signature.Kid = key.Kid // claim a trusted key

	report, _ := VerifyReceipts([]SpanReceipt{forged}, nil, testKeyring(t, key))
	if report.Results[0].SignatureValid {
		t.Fatal("a signature by another key must not verify under a claimed kid")
	}
}

// A receipt signature must not be replayable as a checkpoint signature, which
// is what the separate signing domains are for.
func TestSigningDomainsAreSeparated(t *testing.T) {
	key, _ := GenerateSigningKey()
	payload := CheckpointPayload{
		Type: CheckpointType, Version: 1, ReceiptCount: 0,
		SignedAt: testTime.Format(time.RFC3339Nano),
	}

	input, err := signingInput(signingDomain, payload) // deliberately the wrong domain
	if err != nil {
		t.Fatal(err)
	}
	wrongDomain := Checkpoint{
		Payload: payload,
		Signature: Signature{
			Alg: SigningAlg, Kid: key.Kid,
			Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(key.privateKey, input)),
		},
	}

	report, _ := VerifyReceipts(nil, &wrongDomain, testKeyring(t, key))
	if report.CheckpointValid {
		t.Fatal("a signature made in the receipt domain must not verify as a checkpoint")
	}
}

func TestKeyValidityWindow(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 1)

	tooEarly := NewKeyring(KeyringEntry{
		Kid: key.Kid, Alg: SigningAlg, PublicKey: key.PublicKey,
		NotBefore: testTime.Add(time.Hour).Format(time.RFC3339Nano),
	})
	report, _ := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), tooEarly)
	if report.Results[0].SignatureValid {
		t.Fatal("a receipt signed before the key was valid must fail")
	}

	tooLate := NewKeyring(KeyringEntry{
		Kid: key.Kid, Alg: SigningAlg, PublicKey: key.PublicKey,
		NotAfter: testTime.Add(-time.Hour).Format(time.RFC3339Nano),
	})
	report, _ = VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), tooLate)
	if report.Results[0].SignatureValid {
		t.Fatal("a receipt signed after the key expired must fail")
	}
}

// Rotation must not silently void honest history, or nobody will ever rotate.
func TestRevocationIsNotRetroactive(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 1)

	revokedLater := NewKeyring(KeyringEntry{
		Kid: key.Kid, Alg: SigningAlg, PublicKey: key.PublicKey,
		Status: "revoked", RevokedAt: testTime.Add(time.Hour).Format(time.RFC3339Nano),
	})
	report, _ := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), revokedLater)
	if !report.Results[0].SignatureValid {
		t.Fatalf("a receipt signed before revocation must stay valid: %s", report.Results[0].SignatureError)
	}

	revokedBefore := NewKeyring(KeyringEntry{
		Kid: key.Kid, Alg: SigningAlg, PublicKey: key.PublicKey,
		Status: "revoked", RevokedAt: testTime.Add(-time.Hour).Format(time.RFC3339Nano),
	})
	report, _ = VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), revokedBefore)
	if report.Results[0].SignatureValid {
		t.Fatal("a receipt signed after revocation must fail")
	}
}

func TestDeletionFromTheMiddleIsDetected(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, _ := signChain(t, key, 4)

	cut := append([]SpanReceipt{}, receipts[:2]...)
	cut = append(cut, receipts[3])

	report, _ := VerifyReceipts(cut, nil, testKeyring(t, key))
	if report.SignaturesInvalid != 0 {
		t.Fatal("remaining signatures should still be genuine")
	}
	if report.ChainIntact {
		t.Fatal("a missing link must be detected")
	}
}

func TestOneBreakDoesNotCascade(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, _ := signChain(t, key, 5)

	cut := append([]SpanReceipt{receipts[0]}, receipts[2:]...)
	report, _ := VerifyReceipts(cut, nil, testKeyring(t, key))

	broken := 0
	for _, r := range report.Results {
		if !r.ChainValid {
			broken++
		}
	}
	if broken != 1 {
		t.Fatalf("expected exactly one broken link, got %d", broken)
	}
}

// The failure a hash chain alone cannot catch. Removing the newest receipts
// leaves every remaining link and signature perfectly valid, so only an anchor
// committing to a count and terminal hash detects it.
func TestTruncationFromTheEndIsDetectedOnlyByTheCheckpoint(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 4)
	checkpoint := checkpointFor(t, key, receipts, terminal)

	truncated := receipts[:2]

	withoutAnchor, _ := VerifyReceipts(truncated, nil, testKeyring(t, key))
	if withoutAnchor.SignaturesInvalid != 0 || !withoutAnchor.ChainIntact {
		t.Fatal("truncation leaves signatures and internal links intact, which is the point")
	}
	if withoutAnchor.Valid {
		t.Fatal("a report without a checkpoint must never be valid, since completeness is unproven")
	}

	withAnchor, _ := VerifyReceipts(truncated, checkpoint, testKeyring(t, key))
	if withAnchor.CheckpointValid {
		t.Fatal("the checkpoint must reject a truncated bundle")
	}
	if !strings.Contains(withAnchor.CheckpointError, "expects 4 receipts") {
		t.Fatalf("unexpected checkpoint error: %s", withAnchor.CheckpointError)
	}
}

func TestEmptyBundleDoesNotVerify(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 3)
	checkpoint := checkpointFor(t, key, receipts, terminal)

	// Handing someone zero receipts must not read as success.
	report, _ := VerifyReceipts(nil, checkpoint, testKeyring(t, key))
	if report.CheckpointValid || report.Valid {
		t.Fatal("an empty bundle must fail against a checkpoint naming receipts")
	}

	bare, _ := VerifyReceipts(nil, nil, testKeyring(t, key))
	if bare.Valid {
		t.Fatal("an empty bundle with no checkpoint must not be valid either")
	}
}

func TestGenuinelyEmptyRunVerifies(t *testing.T) {
	key, _ := GenerateSigningKey()
	empty := checkpointFor(t, key, nil, "")

	report, _ := VerifyReceipts(nil, empty, testKeyring(t, key))
	if !report.Valid {
		t.Fatalf("a run that produced nothing, anchored as such, should verify: %+v", report)
	}
}

func TestReportStatesWhatItDoesNotEstablish(t *testing.T) {
	key, _ := GenerateSigningKey()
	receipts, terminal := signChain(t, key, 1)
	report, _ := VerifyReceipts(receipts, checkpointFor(t, key, receipts, terminal), testKeyring(t, key))

	if len(report.DoesNotEstablish) == 0 {
		t.Fatal("verification must state its limits")
	}
	joined := strings.Join(report.DoesNotEstablish, " ")
	if !strings.Contains(joined, "drops spans") {
		t.Fatal("the collector drop caveat must be carried in the report")
	}
}

func TestContentIsCommittedByDigestNotCopied(t *testing.T) {
	span := testSpan("secret")
	span.InputPreview = "sensitive-argument-value"
	payload, err := PayloadFromSpan(span, 0, "", testTime)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := canonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(canonical), "sensitive-argument-value") {
		t.Fatal("previews must be committed to by digest, not copied into the receipt")
	}
	if payload.ContentDigest == "" {
		t.Fatal("a content digest is required")
	}
}
