package http

import "encoding/json"

// webhookAuditPayload is the canonical shape stored in webhook_calls.request_payload.
// Both llm and message handlers produce this top-level structure so that
// extractBodyHash can parse it without handler-specific branching.
//
// Shape written to PG (jsonb) and SQLite (TEXT):
//
//	{"body_hash": "<sha256-hex-64-chars>", "meta": {...handler-specific...}}
type webhookAuditPayload struct {
	BodyHash string          `json:"body_hash"`
	Meta     json.RawMessage `json:"meta"`
}

// buildAuditPayload encodes a canonical audit payload.
// bodyBytes is the raw request body; meta is any JSON-serialisable value
// carrying handler-specific fields (channel_name, prompt excerpt, etc.).
//
// Returns the JSON bytes and any encoding error. An error here is non-fatal
// in callers (best-effort audit) but must never produce invalid JSON that
// would cause a PostgreSQL 22P02 error on jsonb insert.
func buildAuditPayload(bodyBytes []byte, meta any) ([]byte, error) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		// Fall back to an empty object — never silently drop body_hash.
		metaJSON = []byte("{}")
	}

	p := webhookAuditPayload{
		BodyHash: sha256Hex(bodyBytes),
		Meta:     json.RawMessage(metaJSON),
	}
	return json.Marshal(p)
}
