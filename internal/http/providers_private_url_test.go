package http

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestValidateProviderURL_PrivateNetworkBlockedByDefault(t *testing.T) {
	t.Setenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS", "")

	err := validateProviderURL("http://192.168.1.10:11434/v1", store.ProviderOpenAICompat)
	if err == nil {
		t.Fatal("validateProviderURL() error = nil, want private-network rejection")
	}
	if got := err.Error(); got != "provider URL cannot point to private network: 192.168.1.10" {
		t.Fatalf("validateProviderURL() error = %q, want private network rejection", got)
	}
}

func TestValidateProviderURL_PrivateNetworkAllowedByEnv(t *testing.T) {
	t.Setenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS", "1")

	if err := validateProviderURL("http://192.168.1.10:11434/v1", store.ProviderOpenAICompat); err != nil {
		t.Fatalf("validateProviderURL() error = %v, want nil when GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS=1", err)
	}
}

func TestValidateProviderURL_InternalHostnameAllowedByEnv(t *testing.T) {
	t.Setenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS", "true")

	if err := validateProviderURL("http://llm.internal:11434/v1", store.ProviderOpenAICompat); err != nil {
		t.Fatalf("validateProviderURL() error = %v, want nil for internal hostname when env enabled", err)
	}
}

func TestValidateProviderURL_LoopbackStillBlockedWithEnv(t *testing.T) {
	t.Setenv("GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS", "1")

	err := validateProviderURL("http://127.0.0.1:11434/v1", store.ProviderOpenAICompat)
	if err == nil {
		t.Fatal("validateProviderURL() error = nil, want loopback rejection")
	}
	if got := err.Error(); got != "provider URL cannot point to 127.0.0.1" {
		t.Fatalf("validateProviderURL() error = %q, want loopback rejection", got)
	}
}
