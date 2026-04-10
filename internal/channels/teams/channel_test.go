package teams

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestNew_ValidSingleTenant(t *testing.T) {
	cfg := config.TeamsConfig{
		BotID:       "test-bot-id",
		BotPassword: "test-secret",
		BotType:     "SingleTenant",
		TenantID:    "00000000-0000-0000-0000-000000000001",
	}
	ch, err := New(cfg, bus.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.Name() != "teams" {
		t.Errorf("Name() = %q, want %q", ch.Name(), "teams")
	}
	if ch.cfg.WebhookPath != defaultWebhookPath {
		t.Errorf("WebhookPath = %q, want %q", ch.cfg.WebhookPath, defaultWebhookPath)
	}
}

func TestNew_ValidMultiTenant(t *testing.T) {
	cfg := config.TeamsConfig{
		BotID:       "test-bot-id",
		BotPassword: "test-secret",
		BotType:     "MultiTenant",
		TenantID:    "00000000-0000-0000-0000-000000000002",
	}
	ch, err := New(cfg, bus.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// MultiTenant should zero out tenantID for validator
	if ch.validator.tenantID != "" {
		t.Errorf("validator.tenantID = %q, want empty for MultiTenant", ch.validator.tenantID)
	}
	// botClient should also have empty tenantID for MultiTenant
	if ch.client.tenantID != "" {
		t.Errorf("client.tenantID = %q, want empty for MultiTenant", ch.client.tenantID)
	}
}

func TestNew_DefaultBotType(t *testing.T) {
	cfg := config.TeamsConfig{
		BotID:       "test-bot-id",
		BotPassword: "test-secret",
		TenantID:    "00000000-0000-0000-0000-000000000003",
	}
	ch, err := New(cfg, bus.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ch.cfg.BotType != "SingleTenant" {
		t.Errorf("BotType = %q, want %q", ch.cfg.BotType, "SingleTenant")
	}
}

func TestNew_MissingBotID(t *testing.T) {
	cfg := config.TeamsConfig{BotPassword: "secret"}
	_, err := New(cfg, bus.New())
	if err == nil {
		t.Fatal("expected error for missing bot_id")
	}
}

func TestNew_MissingBotPassword(t *testing.T) {
	cfg := config.TeamsConfig{BotID: "id"}
	_, err := New(cfg, bus.New())
	if err == nil {
		t.Fatal("expected error for missing bot_password")
	}
}

func TestNew_SingleTenantMissingTenantID(t *testing.T) {
	cfg := config.TeamsConfig{
		BotID:       "id",
		BotPassword: "secret",
		BotType:     "SingleTenant",
	}
	_, err := New(cfg, bus.New())
	if err == nil {
		t.Fatal("expected error for SingleTenant without tenant_id")
	}
}

func TestIsValidServiceURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://smba.trafficmanager.net/teams/", true},       // real Teams serviceURL
		{"https://smba.trafficmanager.net", true},               // apex smba
		{"https://botframework.com/api", true},
		{"https://us.api.botframework.com", true},
		{"https://emea.api.botframework.com", true},
		{"https://teams.microsoft.com/webhook", true},
		{"https://us.teams.microsoft.com/api", true},
		{"http://us.api.botframework.com", false},               // not HTTPS
		{"https://evil.com", false},
		{"https://botframework.com.evil.com", false},
		{"https://attacker.trafficmanager.net/steal", false},    // MEDIUM-1: non-smba trafficmanager rejected
		{"https://evil.trafficmanager.net", false},              // any other TM subdomain rejected
		{"", false},
		{"not-a-url", false},
	}
	for _, tt := range tests {
		got := isValidServiceURL(tt.url)
		if got != tt.want {
			t.Errorf("isValidServiceURL(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestWebhookHandler_ReturnsPath(t *testing.T) {
	cfg := config.TeamsConfig{
		BotID:       "id",
		BotPassword: "secret",
		BotType:     "SingleTenant",
		TenantID:    "00000000-0000-0000-0000-000000000004",
		WebhookPath: "/custom/teams",
	}
	ch, err := New(cfg, bus.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path, handler := ch.WebhookHandler()
	if path != "/custom/teams" {
		t.Errorf("path = %q, want %q", path, "/custom/teams")
	}
	if handler == nil {
		t.Error("handler is nil")
	}
}
