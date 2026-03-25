package googlechat

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestResolveServiceAccountJSON_ValidInline(t *testing.T) {
	cfg := config.GoogleChatConfig{
		ServiceAccountJSON: `{"type":"service_account","project_id":"test"}`,
	}
	data, err := resolveServiceAccountJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"type":"service_account","project_id":"test"}` {
		t.Fatalf("unexpected data: %s", data)
	}
}

func TestResolveServiceAccountJSON_TrimsWhitespace(t *testing.T) {
	cfg := config.GoogleChatConfig{
		ServiceAccountJSON: `  {"type":"service_account"}  `,
	}
	data, err := resolveServiceAccountJSON(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != `{"type":"service_account"}` {
		t.Fatalf("unexpected data: %s", data)
	}
}

func TestResolveServiceAccountJSON_Empty(t *testing.T) {
	cfg := config.GoogleChatConfig{}
	_, err := resolveServiceAccountJSON(cfg)
	if err == nil {
		t.Fatal("expected error for empty config")
	}
	if err.Error() != "service_account_json is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveServiceAccountJSON_InvalidJSON(t *testing.T) {
	cfg := config.GoogleChatConfig{
		ServiceAccountJSON: "not-json",
	}
	_, err := resolveServiceAccountJSON(cfg)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if err.Error() != "service_account_json is not valid JSON" {
		t.Fatalf("unexpected error: %v", err)
	}
}
