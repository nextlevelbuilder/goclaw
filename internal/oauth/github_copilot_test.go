package oauth

import (
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestGetGitHubCopilotBaseURLFromToken(t *testing.T) {
	t.Parallel()
	token := "abc;proxy-ep=proxy.individual.githubcopilot.com;foo=bar"
	got := GetGitHubCopilotBaseURL(token, "")
	if got != "https://api.individual.githubcopilot.com" {
		t.Fatalf("GetGitHubCopilotBaseURL() = %q, want %q", got, "https://api.individual.githubcopilot.com")
	}
}

func TestGetGitHubCopilotBaseURLEnterpriseFallback(t *testing.T) {
	t.Parallel()
	got := GetGitHubCopilotBaseURL("", "https://github.example.com")
	if got != "https://copilot-api.github.example.com" {
		t.Fatalf("GetGitHubCopilotBaseURL() = %q, want %q", got, "https://copilot-api.github.example.com")
	}
}

func TestGitHubCopilotAccessTokenSecretKey(t *testing.T) {
	t.Parallel()
	if got := GitHubCopilotAccessTokenSecretKey(""); got != "oauth.github-copilot.github_token" {
		t.Fatalf("default secret key = %q", got)
	}
	if got := GitHubCopilotAccessTokenSecretKey("team-copilot"); got != "oauth.team-copilot.github_token" {
		t.Fatalf("custom secret key = %q", got)
	}
}

func TestMarshalGitHubCopilotOAuthSettingsIntoPreservesUnknownKeys(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"foo":"bar","expires_at":1}`)
	settings := store.GitHubCopilotOAuthProviderSettings{
		ExpiresAt:        42,
		EnterpriseDomain: "github.example.com",
		CopilotPlan:      "individual",
		BaseURL:          "https://api.individual.githubcopilot.com",
	}
	merged := marshalGitHubCopilotOAuthSettingsInto(raw, settings)
	var decoded map[string]any
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatalf("unmarshal merged settings: %v", err)
	}
	if decoded["foo"] != "bar" {
		t.Fatalf("expected unknown key to survive, got %#v", decoded["foo"])
	}
	if decoded["enterprise_domain"] != "github.example.com" {
		t.Fatalf("enterprise_domain = %#v", decoded["enterprise_domain"])
	}
	if decoded["copilot_plan"] != "individual" {
		t.Fatalf("copilot_plan = %#v", decoded["copilot_plan"])
	}
	if decoded["base_url"] != "https://api.individual.githubcopilot.com" {
		t.Fatalf("base_url = %#v", decoded["base_url"])
	}
	if decoded["expires_at"] != float64(42) {
		t.Fatalf("expires_at = %#v", decoded["expires_at"])
	}
}