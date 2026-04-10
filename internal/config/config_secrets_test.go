package config

import "testing"

func TestMaskedCopy_MasksWebSearchKeys(t *testing.T) {
	cfg := Default()
	cfg.Tools.Web.Brave.APIKey = "brave-secret"
	cfg.Tools.Web.Exa.APIKey = "exa-secret"
	cfg.Tools.Web.Tavily.APIKey = "tavily-secret"

	masked := cfg.MaskedCopy()

	if masked.Tools.Web.Brave.APIKey != secretMask {
		t.Fatalf("brave key = %q, want %q", masked.Tools.Web.Brave.APIKey, secretMask)
	}
	if masked.Tools.Web.Exa.APIKey != secretMask {
		t.Fatalf("exa key = %q, want %q", masked.Tools.Web.Exa.APIKey, secretMask)
	}
	if masked.Tools.Web.Tavily.APIKey != secretMask {
		t.Fatalf("tavily key = %q, want %q", masked.Tools.Web.Tavily.APIKey, secretMask)
	}
}

func TestApplyDBSecrets_RestoresWebSearchKeys(t *testing.T) {
	cfg := Default()
	cfg.ApplyDBSecrets(map[string]string{
		"tools.web.brave.api_key":  "brave-secret",
		"tools.web.exa.api_key":    "exa-secret",
		"tools.web.tavily.api_key": "tavily-secret",
	})

	if cfg.Tools.Web.Brave.APIKey != "brave-secret" {
		t.Fatalf("brave key = %q", cfg.Tools.Web.Brave.APIKey)
	}
	if cfg.Tools.Web.Exa.APIKey != "exa-secret" {
		t.Fatalf("exa key = %q", cfg.Tools.Web.Exa.APIKey)
	}
	if cfg.Tools.Web.Tavily.APIKey != "tavily-secret" {
		t.Fatalf("tavily key = %q", cfg.Tools.Web.Tavily.APIKey)
	}
}

func TestExtractDBSecrets_IncludesWebSearchKeysAndIgnoresMaskedValues(t *testing.T) {
	cfg := Default()
	cfg.Tools.Web.Brave.APIKey = "brave-secret"
	cfg.Tools.Web.Exa.APIKey = secretMask
	cfg.Tools.Web.Tavily.APIKey = "tavily-secret"

	secrets := cfg.ExtractDBSecrets()

	if secrets["tools.web.brave.api_key"] != "brave-secret" {
		t.Fatalf("brave secret = %q", secrets["tools.web.brave.api_key"])
	}
	if _, ok := secrets["tools.web.exa.api_key"]; ok {
		t.Fatalf("masked Exa key should not be persisted: %#v", secrets)
	}
	if secrets["tools.web.tavily.api_key"] != "tavily-secret" {
		t.Fatalf("tavily secret = %q", secrets["tools.web.tavily.api_key"])
	}
}
