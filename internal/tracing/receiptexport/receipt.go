// Package receiptexport signs exported spans as Ed25519 receipts so a third
// party can verify what a GoClaw run did without trusting the collector, the
// database, or the operator who runs them.
//
// It answers a different question from OTLP. A trace says what the runtime
// emitted. A receipt says what a named key attested to, in what order, and
// whether that record has been altered since. That difference matters the
// moment a run crosses an organisational boundary or someone has to explain a
// quiet failure after the fact.
//
// # What a verified receipt establishes
//
// This span, with exactly these fields, was exported by the holder of this key
// at this time, and the verified sequence has not been altered since.
//
// # What it does not establish
//
// That the span is a complete record of what happened. The collector drops
// spans when its buffers fill (see Collector.EmitSpan), so an exporter can only
// sign what it receives. Coverage is therefore reported as spans this exporter
// saw, never as spans the runtime executed. See the Coverage type.
//
// It also does not establish that a tool call was correct or authorised, that
// the operator is honest, or that a key compromised at export time produced
// truthful receipts. Signing binds authorship and order, not judgement.
//
// # Dependencies
//
// Standard library only, plus google/uuid which the repository already uses.
package receiptexport

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// ReceiptType identifies the signed payload schema.
	ReceiptType = "goclaw.span_receipt.v1"
	// KeyringType identifies the trust document schema.
	KeyringType = "goclaw.receipt_keyring.v1"
	// CheckpointType identifies the completeness anchor schema.
	CheckpointType = "goclaw.receipt_checkpoint.v1"
	// SigningAlg is the only algorithm this package accepts.
	SigningAlg = "Ed25519"

	// signingDomain is mixed into every signature so a signature over one kind
	// of object can never verify as another.
	signingDomain = "goclaw/span-receipt/v1"
	// checkpointDomain separates checkpoint signatures from receipt signatures.
	checkpointDomain = "goclaw/receipt-checkpoint/v1"
)

// SpanReceiptPayload is the signed record of one exported span.
//
// Previews and metadata are committed to by digest rather than copied. The
// tracing package redacts previews for a reason, and duplicating that content
// into a second, separately stored artifact would widen the blast radius of a
// leak while adding nothing a digest does not already prove.
type SpanReceiptPayload struct {
	Type    string `json:"type"`
	Version int    `json:"version"`

	// Sequence is the exporter's own monotonic counter, not a timestamp.
	// Ordering evidence by a clock invites an older stamp recorded later to
	// make honest history look altered.
	Sequence uint64 `json:"sequence"`

	SpanID       string `json:"span_id"`
	TraceID      string `json:"trace_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	TenantID     string `json:"tenant_id"`
	TeamID       string `json:"team_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`

	SpanType string `json:"span_type"`
	Name     string `json:"name,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`

	// Tool identity is the reason this feature exists, so it is signed
	// explicitly rather than left inside an opaque content digest.
	ToolName   string `json:"tool_name,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`

	Model        string `json:"model,omitempty"`
	Provider     string `json:"provider,omitempty"`
	FinishReason string `json:"finish_reason,omitempty"`
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`

	StartTime  string `json:"start_time"`
	EndTime    string `json:"end_time,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`

	// ContentDigest commits to the previews, metadata and model params without
	// carrying them.
	ContentDigest string `json:"content_digest"`

	ExportedAt      string `json:"exported_at"`
	PrevReceiptHash string `json:"prev_receipt_hash,omitempty"`
}

// Signature is the detached signature over a payload.
type Signature struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Sig string `json:"sig"`
}

// SpanReceipt is a payload with its signature. Keeping them separate lets a
// verifier recompute the exact signed bytes without stripping fields out of a
// merged object.
type SpanReceipt struct {
	Payload   SpanReceiptPayload `json:"payload"`
	Signature Signature          `json:"signature"`
}

// SigningKey is an Ed25519 identity. Kid is derived from the public key so it
// cannot be reassigned to a different key.
type SigningKey struct {
	Kid        string             `json:"kid"`
	Alg        string             `json:"alg"`
	PublicKey  string             `json:"public_key"`
	privateKey ed25519.PrivateKey `json:"-"`
}

// KeyringEntry is one trusted key with its validity window.
type KeyringEntry struct {
	Kid       string `json:"kid"`
	Alg       string `json:"alg"`
	PublicKey string `json:"public_key"`
	// NotBefore and NotAfter are RFC3339 timestamps bounding when the key may
	// sign. Empty means unbounded.
	NotBefore string `json:"not_before,omitempty"`
	NotAfter  string `json:"not_after,omitempty"`
	Status    string `json:"status,omitempty"`
	RevokedAt string `json:"revoked_at,omitempty"`
}

// Keyring is the trust input a verifier supplies from its own side. It is
// published separately from receipts on purpose: an artifact that carried its
// own trust anchor would let a producer install trust in its own output.
type Keyring struct {
	Type    string         `json:"type"`
	Version int            `json:"version"`
	Keys    []KeyringEntry `json:"keys"`
}

// Coverage records what this exporter saw. It never claims to describe what the
// runtime executed, because spans dropped by the collector under buffer
// pressure never reach an exporter and leave no machine-readable trace.
type Coverage struct {
	SpansReceived uint64 `json:"spans_received"`
	SpansSigned   uint64 `json:"spans_signed"`
	SpansFailed   uint64 `json:"spans_failed"`
	// Note is carried in the artifact so a reader cannot mistake exporter
	// coverage for runtime coverage.
	Note string `json:"note"`
}

// CheckpointPayload is the completeness anchor.
//
// A hash chain detects modification and reordering, and detects deletion from
// the middle. It cannot detect truncation: remove the newest receipts and every
// remaining link still verifies. The checkpoint closes that by committing to a
// count and a terminal hash, and it is only worth anything if it is published
// somewhere the operator cannot quietly rewrite.
type CheckpointPayload struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	Sequence uint64 `json:"sequence"`

	ReceiptCount        uint64   `json:"receipt_count"`
	TerminalReceiptHash string   `json:"terminal_receipt_hash,omitempty"`
	Coverage            Coverage `json:"coverage"`
	SignedAt            string   `json:"signed_at"`
}

// Checkpoint is a signed completeness anchor.
type Checkpoint struct {
	Payload   CheckpointPayload `json:"payload"`
	Signature Signature         `json:"signature"`
}

// Bundle is a self-contained export for offline verification.
type Bundle struct {
	Type       string        `json:"type"`
	Version    int           `json:"version"`
	Receipts   []SpanReceipt `json:"receipts"`
	Checkpoint *Checkpoint   `json:"checkpoint,omitempty"`
	// KeyringHint is a convenience. Verifying an artifact with a key the
	// artifact supplied proves only internal consistency.
	KeyringHint      *Keyring `json:"keyring_hint,omitempty"`
	VerificationNote string   `json:"verification_note"`
}

// GenerateSigningKey creates a fresh Ed25519 identity.
//
// This is a convenience for getting started, not a recommendation for
// production. Where the private key lives is what determines whether a
// signature is worth anything.
func GenerateSigningKey() (*SigningKey, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	return &SigningKey{
		Kid:        KeyIDFromPublicKey(pubB64),
		Alg:        SigningAlg,
		PublicKey:  pubB64,
		privateKey: priv,
	}, nil
}

// NewSigningKey adopts an existing Ed25519 private key, for deployments that
// hold key material elsewhere.
func NewSigningKey(priv ed25519.PrivateKey) (*SigningKey, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("expected %d byte ed25519 private key, got %d", ed25519.PrivateKeySize, len(priv))
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("private key does not yield an ed25519 public key")
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	return &SigningKey{
		Kid:        KeyIDFromPublicKey(pubB64),
		Alg:        SigningAlg,
		PublicKey:  pubB64,
		privateKey: priv,
	}, nil
}

// KeyIDFromPublicKey derives a deterministic key id, so a kid always checks
// against the key it claims to identify.
func KeyIDFromPublicKey(publicKeyB64 string) string {
	raw, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "gck_invalid"
	}
	sum := sha256.Sum256(raw)
	return "gck_" + hex.EncodeToString(sum[:])[:16]
}

// PublicEntry returns the public half of a key for publication in a keyring.
func (k *SigningKey) PublicEntry() KeyringEntry {
	return KeyringEntry{Kid: k.Kid, Alg: k.Alg, PublicKey: k.PublicKey, Status: "active"}
}

// NewKeyring builds a trust document from public entries.
func NewKeyring(entries ...KeyringEntry) *Keyring {
	keys := make([]KeyringEntry, 0, len(entries))
	for _, e := range entries {
		if e.Alg == "" {
			e.Alg = SigningAlg
		}
		if e.Status == "" {
			e.Status = "active"
		}
		keys = append(keys, e)
	}
	return &Keyring{Type: KeyringType, Version: 1, Keys: keys}
}

// canonicalJSON produces deterministic bytes: object keys sorted by code unit,
// arrays left in order, no insignificant whitespace.
//
// Both signer and verifier must derive identical bytes from the same value or
// every signature is a coin flip. Go marshals struct fields in declaration
// order, which is deterministic within this package but not something a
// verifier written in another language can rely on, so keys are sorted.
func canonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("decode for canonicalization: %w", err)
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, generic); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch value := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if value {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		buf.WriteString(value.String())
	case string:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal string: %w", err)
		}
		buf.Write(encoded)
	case []any:
		buf.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			encoded, err := json.Marshal(k)
			if err != nil {
				return fmt.Errorf("marshal key: %w", err)
			}
			buf.Write(encoded)
			buf.WriteByte(':')
			if err := writeCanonical(buf, value[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonicalize: unsupported value of type %T", v)
	}
	return nil
}

// DigestOf returns lowercase hex SHA-256 over canonical bytes.
func DigestOf(v any) (string, error) {
	canonical, err := canonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func signingInput(domain string, payload any) ([]byte, error) {
	canonical, err := canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(domain)+1+len(canonical))
	out = append(out, domain...)
	out = append(out, '\n')
	out = append(out, canonical...)
	return out, nil
}

// ReceiptHash is the digest the next receipt chains to. It covers the whole
// envelope including the signature, so replacing a signature breaks the chain.
func ReceiptHash(r SpanReceipt) (string, error) {
	return DigestOf(r)
}

// PayloadFromSpan builds the signed fields from a span.
func PayloadFromSpan(span store.SpanData, sequence uint64, prevReceiptHash string, exportedAt time.Time) (SpanReceiptPayload, error) {
	contentDigest, err := DigestOf(map[string]any{
		"input_preview":  span.InputPreview,
		"output_preview": span.OutputPreview,
		"metadata":       string(span.Metadata),
		"model_params":   string(span.ModelParams),
		"total_cost":     formatCost(span.TotalCost),
	})
	if err != nil {
		return SpanReceiptPayload{}, fmt.Errorf("content digest: %w", err)
	}

	payload := SpanReceiptPayload{
		Type:            ReceiptType,
		Version:         1,
		Sequence:        sequence,
		SpanID:          span.ID.String(),
		TraceID:         span.TraceID.String(),
		TenantID:        span.TenantID.String(),
		SpanType:        span.SpanType,
		Name:            span.Name,
		Status:          span.Status,
		Error:           span.Error,
		ToolName:        span.ToolName,
		ToolCallID:      span.ToolCallID,
		Model:           span.Model,
		Provider:        span.Provider,
		FinishReason:    span.FinishReason,
		InputTokens:     span.InputTokens,
		OutputTokens:    span.OutputTokens,
		StartTime:       span.StartTime.UTC().Format(time.RFC3339Nano),
		DurationMS:      span.DurationMS,
		ContentDigest:   contentDigest,
		ExportedAt:      exportedAt.UTC().Format(time.RFC3339Nano),
		PrevReceiptHash: prevReceiptHash,
	}
	if span.ParentSpanID != nil {
		payload.ParentSpanID = span.ParentSpanID.String()
	}
	if span.TeamID != nil {
		payload.TeamID = span.TeamID.String()
	}
	if span.AgentID != nil {
		payload.AgentID = span.AgentID.String()
	}
	if span.EndTime != nil {
		payload.EndTime = span.EndTime.UTC().Format(time.RFC3339Nano)
	}
	return payload, nil
}

// formatCost renders a nullable float deterministically. Floats are not signed
// directly because a signer and verifier can disagree on representation.
func formatCost(cost *float64) string {
	if cost == nil {
		return ""
	}
	return fmt.Sprintf("%.10f", *cost)
}

// SignReceipt signs a payload.
func SignReceipt(payload SpanReceiptPayload, key *SigningKey) (SpanReceipt, error) {
	if key == nil || key.privateKey == nil {
		return SpanReceipt{}, fmt.Errorf("signing key is required")
	}
	input, err := signingInput(signingDomain, payload)
	if err != nil {
		return SpanReceipt{}, err
	}
	sig := ed25519.Sign(key.privateKey, input)
	return SpanReceipt{
		Payload:   payload,
		Signature: Signature{Alg: SigningAlg, Kid: key.Kid, Sig: base64.StdEncoding.EncodeToString(sig)},
	}, nil
}

// SignCheckpoint signs a completeness anchor.
func SignCheckpoint(payload CheckpointPayload, key *SigningKey) (Checkpoint, error) {
	if key == nil || key.privateKey == nil {
		return Checkpoint{}, fmt.Errorf("signing key is required")
	}
	input, err := signingInput(checkpointDomain, payload)
	if err != nil {
		return Checkpoint{}, err
	}
	sig := ed25519.Sign(key.privateKey, input)
	return Checkpoint{
		Payload:   payload,
		Signature: Signature{Alg: SigningAlg, Kid: key.Kid, Sig: base64.StdEncoding.EncodeToString(sig)},
	}, nil
}

// resolveKey finds the key a receipt names and decides whether it was trusted
// at the moment the receipt claims to have been signed.
//
// Revocation is deliberately not retroactive: a receipt signed while the key
// was trusted stays valid, because the alternative silently voids honest
// history every time an operator rotates. RevokedAt exists for the compromise
// case, where everything from that moment on should fail.
func resolveKey(keyring *Keyring, kid string, at time.Time) (KeyringEntry, error) {
	if keyring == nil {
		return KeyringEntry{}, fmt.Errorf("keyring is required")
	}
	if keyring.Type != KeyringType {
		return KeyringEntry{}, fmt.Errorf("unsupported keyring type %q", keyring.Type)
	}
	for _, key := range keyring.Keys {
		if key.Kid != kid {
			continue
		}
		if key.Alg != SigningAlg {
			return KeyringEntry{}, fmt.Errorf("key %s uses unsupported algorithm %q", kid, key.Alg)
		}
		if KeyIDFromPublicKey(key.PublicKey) != key.Kid {
			return KeyringEntry{}, fmt.Errorf("key %s does not match the public key it names", kid)
		}
		if err := checkWindow(key, at, kid); err != nil {
			return KeyringEntry{}, err
		}
		return key, nil
	}
	return KeyringEntry{}, fmt.Errorf("no key in keyring for kid %s", kid)
}

func checkWindow(key KeyringEntry, at time.Time, kid string) error {
	if key.NotBefore != "" {
		nb, err := time.Parse(time.RFC3339Nano, key.NotBefore)
		if err != nil {
			return fmt.Errorf("key %s has an unparseable not_before: %w", kid, err)
		}
		if at.Before(nb) {
			return fmt.Errorf("receipt predates the validity window of key %s", kid)
		}
	}
	if key.NotAfter != "" {
		na, err := time.Parse(time.RFC3339Nano, key.NotAfter)
		if err != nil {
			return fmt.Errorf("key %s has an unparseable not_after: %w", kid, err)
		}
		if at.After(na) {
			return fmt.Errorf("receipt postdates the validity window of key %s", kid)
		}
	}
	if strings.EqualFold(key.Status, "revoked") && key.RevokedAt != "" {
		ra, err := time.Parse(time.RFC3339Nano, key.RevokedAt)
		if err != nil {
			return fmt.Errorf("key %s has an unparseable revoked_at: %w", kid, err)
		}
		if !at.Before(ra) {
			return fmt.Errorf("key %s was revoked before this receipt was signed", kid)
		}
	}
	return nil
}

func verifySignature(domain string, payload any, sig Signature, keyring *Keyring, at time.Time) error {
	if sig.Alg != SigningAlg {
		return fmt.Errorf("unsupported signature algorithm %q", sig.Alg)
	}
	key, err := resolveKey(keyring, sig.Kid, at)
	if err != nil {
		return err
	}
	pubRaw, err := base64.StdEncoding.DecodeString(key.PublicKey)
	if err != nil {
		return fmt.Errorf("public key for %s could not be decoded: %w", sig.Kid, err)
	}
	if len(pubRaw) != ed25519.PublicKeySize {
		return fmt.Errorf("public key for %s is not an ed25519 key", sig.Kid)
	}
	sigRaw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil {
		return fmt.Errorf("signature could not be decoded: %w", err)
	}
	input, err := signingInput(domain, payload)
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(pubRaw), input, sigRaw) {
		return fmt.Errorf("signature does not verify over the payload")
	}
	return nil
}
