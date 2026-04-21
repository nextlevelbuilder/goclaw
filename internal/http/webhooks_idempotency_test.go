package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestExtractBodyHash_canonical verifies that extractBodyHash correctly parses
// the canonical {"body_hash":"...","meta":{...}} JSON shape produced by buildAuditPayload.
func TestExtractBodyHash_canonical(t *testing.T) {
	body := []byte(`{"input":"hello"}`)
	payload, err := buildAuditPayload(body, map[string]string{"key": "val"})
	if err != nil {
		t.Fatalf("buildAuditPayload: %v", err)
	}

	got := extractBodyHash(payload)
	want := sha256Hex(body)
	if got != want {
		t.Errorf("extractBodyHash got %q, want %q", got, want)
	}
}

// TestExtractBodyHash_oldFormat ensures the old hex-prefix format (non-JSON bytes)
// is rejected (returns ""), preventing hash bypass via legacy records.
func TestExtractBodyHash_oldFormat(t *testing.T) {
	// Old format: 64 hex bytes + JSON suffix (not valid JSON at top level).
	body := []byte(`{"x":1}`)
	hexHash := sha256Hex(body)
	old := append([]byte(hexHash), []byte(`{"channel_name":"c"}`)...)

	got := extractBodyHash(old)
	if got != "" {
		t.Errorf("old hex-prefix format should return \"\", got %q", got)
	}
}

// TestExtractBodyHash_empty returns "" for nil/empty payload.
func TestExtractBodyHash_empty(t *testing.T) {
	if got := extractBodyHash(nil); got != "" {
		t.Errorf("nil payload: want \"\", got %q", got)
	}
	if got := extractBodyHash([]byte{}); got != "" {
		t.Errorf("empty payload: want \"\", got %q", got)
	}
}

// TestExtractBodyHash_missingField returns "" when body_hash field is absent.
func TestExtractBodyHash_missingField(t *testing.T) {
	payload := []byte(`{"meta":{"channel_name":"c"}}`)
	if got := extractBodyHash(payload); got != "" {
		t.Errorf("missing body_hash: want \"\", got %q", got)
	}
}

// TestExtractBodyHash_wrongLength returns "" when body_hash is not 64 chars.
func TestExtractBodyHash_wrongLength(t *testing.T) {
	payload := []byte(`{"body_hash":"abc123","meta":{}}`)
	if got := extractBodyHash(payload); got != "" {
		t.Errorf("short hash: want \"\", got %q", got)
	}
}

// TestExtractBodyHash_nonHexChars returns "" when body_hash contains non-hex chars.
func TestExtractBodyHash_nonHexChars(t *testing.T) {
	// 64 chars but contains uppercase G — not valid lowercase hex.
	badHash := "GGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGGG"
	payload, _ := json.Marshal(map[string]string{"body_hash": badHash})
	if got := extractBodyHash(payload); got != "" {
		t.Errorf("non-hex chars: want \"\", got %q", got)
	}
}

// TestBuildAuditPayload_shape verifies the top-level JSON structure.
func TestBuildAuditPayload_shape(t *testing.T) {
	body := []byte(`{"input":"test"}`)
	meta := map[string]string{"channel": "tg"}

	payload, err := buildAuditPayload(body, meta)
	if err != nil {
		t.Fatalf("buildAuditPayload: %v", err)
	}

	var p struct {
		BodyHash string          `json:"body_hash"`
		Meta     json.RawMessage `json:"meta"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		t.Fatalf("payload not valid JSON: %v\npayload: %s", err, payload)
	}
	if len(p.BodyHash) != 64 {
		t.Errorf("body_hash length %d, want 64", len(p.BodyHash))
	}
	if p.BodyHash != sha256Hex(body) {
		t.Errorf("body_hash mismatch")
	}
	if len(p.Meta) == 0 {
		t.Error("meta must not be empty")
	}
}

// TestCheckIdempotency_malformedStoredHash verifies that a stored row with
// an empty/malformed body_hash (extractBodyHash returns "") causes a 409 Conflict
// response rather than falling through to replay. This is the K3 fail-closed fix:
// storedHash != bodyHash includes the empty-string case, preventing a corrupt or
// tampered stored row from serving as a replay vehicle for arbitrary request bodies.
func TestCheckIdempotency_malformedStoredHash(t *testing.T) {
	webhookID := uuid.New()
	body := []byte(`{"input":"hello"}`)

	// Stored row has malformed request_payload (not valid canonical JSON).
	// extractBodyHash will return "" for this payload.
	malformedPayload := []byte(`not-valid-json`)
	existing := &store.WebhookCallData{
		ID:             uuid.New(),
		WebhookID:      webhookID,
		IdempotencyKey: strPtr("idem-key-1"),
		RequestPayload: malformedPayload,
		Status:         "completed",
	}

	calls := newStubCallStore(existing)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/llm", strings.NewReader(string(body)))
	req.Header.Set("Idempotency-Key", "idem-key-1")
	rec := httptest.NewRecorder()

	proceed, err := checkIdempotency(rec, req, body, webhookID, calls)

	if proceed {
		t.Error("expected proceed=false (409 written), got proceed=true")
	}
	if err == nil {
		t.Error("expected non-nil error for idempotency conflict")
	}
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", rec.Code)
	}
}

// strPtr is a test helper returning a pointer to s.
func strPtr(s string) *string { return &s }

// TestBuildAuditPayload_validJSON ensures the output is always valid JSON
// (the property that prevented PG 22P02 errors).
func TestBuildAuditPayload_validJSON(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		meta any
	}{
		{"string meta", []byte(`{}`), "just a string"},
		{"nil meta", []byte(`{}`), nil},
		{"nested meta", []byte(`{"a":1}`), map[string]any{"x": []int{1, 2, 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := buildAuditPayload(tc.body, tc.meta)
			if err != nil {
				t.Fatalf("buildAuditPayload: %v", err)
			}
			if !json.Valid(p) {
				t.Errorf("output not valid JSON: %s", p)
			}
		})
	}
}
