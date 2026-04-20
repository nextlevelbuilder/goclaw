package http

import (
	"strings"
	"testing"
)

func TestValidateProviderURL_PrivateIPBlockedByDefault(t *testing.T) {
	prev := allowPrivateProviderURLsFn
	allowPrivateProviderURLsFn = func() bool { return false }
	t.Cleanup(func() { allowPrivateProviderURLsFn = prev })

	err := validateProviderURL("http://10.10.27.27/v1", "openai")
	if err == nil {
		t.Fatalf("expected error for private IP, got nil")
	}
	if !strings.Contains(err.Error(), "private network") {
		t.Fatalf("expected private-network error, got %v", err)
	}
}

func TestValidateProviderURL_PrivateIPAllowedWithOptIn(t *testing.T) {
	prev := allowPrivateProviderURLsFn
	allowPrivateProviderURLsFn = func() bool { return true }
	t.Cleanup(func() { allowPrivateProviderURLsFn = prev })

	if err := validateProviderURL("http://10.10.27.27/v1", "openai"); err != nil {
		t.Fatalf("expected no error with opt-in, got %v", err)
	}
	if err := validateProviderURL("http://192.168.1.50:8080/v1", "openai"); err != nil {
		t.Fatalf("expected no error with opt-in, got %v", err)
	}
	if err := validateProviderURL("http://localhost:8080/v1", "openai"); err != nil {
		t.Fatalf("expected no error with opt-in for localhost, got %v", err)
	}
	if err := validateProviderURL("http://llm.internal/v1", "openai"); err != nil {
		t.Fatalf("expected no error with opt-in for .internal hostname, got %v", err)
	}
}

func TestValidateProviderURL_SchemeAlwaysEnforced(t *testing.T) {
	prev := allowPrivateProviderURLsFn
	allowPrivateProviderURLsFn = func() bool { return true }
	t.Cleanup(func() { allowPrivateProviderURLsFn = prev })

	err := validateProviderURL("file:///etc/passwd", "openai")
	if err == nil {
		t.Fatalf("expected scheme error even with opt-in, got nil")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestValidateProviderURL_PublicHostAlwaysOK(t *testing.T) {
	prev := allowPrivateProviderURLsFn
	allowPrivateProviderURLsFn = func() bool { return false }
	t.Cleanup(func() { allowPrivateProviderURLsFn = prev })

	if err := validateProviderURL("https://api.openai.com/v1", "openai"); err != nil {
		t.Fatalf("expected no error for public host, got %v", err)
	}
}

// --- H-1 SSRF fix: local provider types must enforce URL validation ---

func TestValidateProviderURL_OllamaLocalhostAllowed(t *testing.T) {
	for _, base := range []string{
		"http://localhost:11434/v1",
		"http://127.0.0.1:11434/v1",
		"http://[::1]:11434/v1",
		"http://host.docker.internal:11434/v1",
	} {
		if err := validateProviderURL(base, "ollama"); err != nil {
			t.Errorf("expected ollama localhost URL %q to be allowed, got %v", base, err)
		}
	}
}

func TestValidateProviderURL_OllamaArbitraryHostBlocked(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"metadata_endpoint", "http://169.254.169.254/latest/meta-data/"},
		{"private_ip", "http://10.0.0.5:11434/v1"},
		{"attacker_controlled", "http://evil.attacker.tld:9999/v1"},
		{"docker_internal_dns", "http://postgres:5432/v1"},
		{"link_local", "http://169.254.1.1:8080/v1"},
		{"internal_hostname", "http://redis.internal:6379/v1"},
		{"gcp_metadata", "http://metadata.google.internal/computeMetadata/v1/"},
		{"self_ssrf", "http://10.10.27.30:18790/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProviderURL(tc.url, "ollama")
			if err == nil {
				t.Fatalf("expected error for ollama with non-localhost URL %q, got nil", tc.url)
			}
			if !strings.Contains(err.Error(), "only allows localhost") {
				t.Fatalf("expected localhost-restriction error, got %v", err)
			}
		})
	}
}

func TestValidateProviderURL_ClaudeCLIArbitraryHostBlocked(t *testing.T) {
	err := validateProviderURL("http://169.254.169.254/", "claude_cli")
	if err == nil {
		t.Fatalf("expected error for claude_cli with metadata URL, got nil")
	}
	if !strings.Contains(err.Error(), "only allows localhost") {
		t.Fatalf("expected localhost-restriction error, got %v", err)
	}
}

func TestValidateProviderURL_ACPArbitraryHostBlocked(t *testing.T) {
	err := validateProviderURL("http://10.0.0.1:8080/v1", "acp")
	if err == nil {
		t.Fatalf("expected error for ACP with private IP, got nil")
	}
	if !strings.Contains(err.Error(), "only allows localhost") {
		t.Fatalf("expected localhost-restriction error, got %v", err)
	}
}

func TestValidateProviderURL_LocalTypeSchemeEnforced(t *testing.T) {
	err := validateProviderURL("file:///etc/passwd", "ollama")
	if err == nil {
		t.Fatalf("expected scheme error for ollama with file:// URL, got nil")
	}
	if !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("expected scheme error, got %v", err)
	}
}

func TestValidateProviderURL_LocalTypeEmptyURLAllowed(t *testing.T) {
	if err := validateProviderURL("", "ollama"); err != nil {
		t.Fatalf("expected empty URL to be allowed for ollama, got %v", err)
	}
	if err := validateProviderURL("", "claude_cli"); err != nil {
		t.Fatalf("expected empty URL to be allowed for claude_cli, got %v", err)
	}
}
