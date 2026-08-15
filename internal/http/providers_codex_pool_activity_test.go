package http

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestEmptyProviderPoolActivityResponseDefaultsToPriorityOrder(t *testing.T) {
	got := emptyProviderPoolActivityResponse()

	if got["strategy"] != store.ChatGPTOAuthStrategyPriority {
		t.Fatalf("strategy = %v, want %q", got["strategy"], store.ChatGPTOAuthStrategyPriority)
	}
}

func TestCanonicalizeChatGPTOAuthRoutingForResponseMigratesLegacyStrategy(t *testing.T) {
	got := canonicalizeChatGPTOAuthRoutingForResponse(json.RawMessage(`{
		"override_mode": "custom",
		"strategy": "manual",
		"extra_provider_names": []
	}`))

	var routing map[string]any
	if err := json.Unmarshal(got, &routing); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if routing["strategy"] != store.ChatGPTOAuthStrategyPriority {
		t.Fatalf("strategy = %v, want %q", routing["strategy"], store.ChatGPTOAuthStrategyPriority)
	}
}

func TestCanonicalizeProviderSettingsForResponseMigratesLegacyPoolStrategy(t *testing.T) {
	got := canonicalizeProviderSettingsForResponse(json.RawMessage(`{
		"codex_pool": {
			"strategy": "primary_first",
			"extra_provider_names": ["codex-work"]
		},
		"embedding": {
			"enabled": true
		}
	}`))

	var settings map[string]any
	if err := json.Unmarshal(got, &settings); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	pool, ok := settings["codex_pool"].(map[string]any)
	if !ok {
		t.Fatalf("codex_pool = %#v, want object", settings["codex_pool"])
	}
	if pool["strategy"] != store.ChatGPTOAuthStrategyPriority {
		t.Fatalf("strategy = %v, want %q", pool["strategy"], store.ChatGPTOAuthStrategyPriority)
	}
	if !reflect.DeepEqual(pool["extra_provider_names"], []any{"codex-work"}) {
		t.Fatalf("extra_provider_names = %#v, want %#v", pool["extra_provider_names"], []any{"codex-work"})
	}
}
