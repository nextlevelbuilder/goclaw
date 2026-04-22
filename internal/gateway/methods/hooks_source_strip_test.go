package methods

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// Regression tests for code-review findings C1 + C2 (260415):
// the WS RPC must NOT trust caller-supplied `source`, `id`, `created_by`,
// or `version` when creating a hook. Without this strip, a tenant admin can
// POST {"source":"builtin"} and escalate their UI hook into the builtin
// capability tier (which the dispatcher allows to mutate event input).

func TestParseHookConfigParams_StripsForgeFields(t *testing.T) {
	caller := []byte(`{
		"event": "pre_tool_use",
		"scope": "tenant",
		"handler_type": "http",
		"source": "builtin",
		"id": "11111111-2222-3333-4444-555555555555",
		"created_by": "99999999-9999-9999-9999-999999999999",
		"version": 42
	}`)

	cfg, err := parseHookConfigParams(json.RawMessage(caller))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Source != "" {
		t.Errorf("Source not stripped: got %q (caller can forge builtin tier)", cfg.Source)
	}
	if cfg.ID != uuid.Nil {
		t.Errorf("ID not stripped: got %s (caller can collide with seeded UUIDv5)", cfg.ID)
	}
	if cfg.CreatedBy != nil {
		t.Errorf("CreatedBy not stripped: got %v (caller can lie about provenance)", *cfg.CreatedBy)
	}
	if cfg.Version != 0 {
		t.Errorf("Version not stripped: got %d", cfg.Version)
	}
	// Sanity: the legitimate fields should round-trip.
	if cfg.HandlerType != "http" || cfg.Scope != "tenant" {
		t.Errorf("legitimate fields lost: %+v", cfg)
	}
}
