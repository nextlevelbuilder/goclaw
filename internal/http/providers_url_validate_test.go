package http

import (
	"net"
	"testing"
)

func TestValidateProviderURL(t *testing.T) {
	// Save and restore global state.
	origResolver := dnsResolverFn
	origAllowPrivate := allowPrivateProviderURLsFn
	defer func() {
		dnsResolverFn = origResolver
		allowPrivateProviderURLsFn = origAllowPrivate
	}()
	allowPrivateProviderURLsFn = func() bool { return false }

	// Stub DNS resolver: known hostnames → controlled IPs.
	dnsResolverFn = func(host string) ([]string, error) {
		switch host {
		case "api.openai.com":
			return []string{"104.18.6.192"}, nil
		case "10.10.27.30.nip.io":
			return []string{"10.10.27.30"}, nil
		case "192.168.1.1.sslip.io":
			return []string{"192.168.1.1"}, nil
		case "172.16.0.5.nip.io":
			return []string{"172.16.0.5"}, nil
		case "169.254.169.254.nip.io":
			return []string{"169.254.169.254"}, nil
		case "127.0.0.1.nip.io":
			return []string{"127.0.0.1"}, nil
		case "rebind.attacker.com":
			return []string{"10.0.0.1"}, nil
		case "legit-provider.example.com":
			return []string{"203.0.113.50"}, nil
		case "dual-stack.example.com":
			return []string{"203.0.113.50", "10.0.0.1"}, nil
		default:
			return nil, &net.DNSError{Err: "no such host", Name: host}
		}
	}

	tests := []struct {
		name         string
		rawURL       string
		providerType string
		wantErr      bool
		desc         string
	}{
		// --- Should PASS ---
		{
			name:         "empty URL allowed",
			rawURL:       "",
			providerType: "openai_compat",
			wantErr:      false,
		},
		{
			name:         "public HTTPS URL",
			rawURL:       "https://api.openai.com/v1",
			providerType: "openai_compat",
			wantErr:      false,
		},
		{
			name:         "public HTTP URL",
			rawURL:       "http://legit-provider.example.com/v1",
			providerType: "openai_compat",
			wantErr:      false,
		},
		{
			name:         "ollama localhost allowed (local type)",
			rawURL:       "http://localhost:11434/v1",
			providerType: "ollama",
			wantErr:      false,
		},
		{
			name:         "claude_cli local allowed (local type)",
			rawURL:       "http://127.0.0.1:8080",
			providerType: "claude_cli",
			wantErr:      false,
		},
		{
			name:         "acp local allowed (local type)",
			rawURL:       "http://10.0.0.1:9090",
			providerType: "acp",
			wantErr:      false,
		},

		// --- Should BLOCK: scheme ---
		{
			name:         "file scheme blocked",
			rawURL:       "file:///etc/passwd",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "gopher scheme blocked",
			rawURL:       "gopher://internal:25",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "file scheme blocked even for local types",
			rawURL:       "file:///etc/passwd",
			providerType: "ollama",
			wantErr:      true,
			desc:         "H-1 fix: scheme check enforced for all provider types",
		},
		{
			name:         "gopher blocked for local types",
			rawURL:       "gopher://localhost:25",
			providerType: "acp",
			wantErr:      true,
			desc:         "H-1 fix: scheme check enforced for all provider types",
		},

		// --- Should BLOCK: literal private IPs ---
		{
			name:         "localhost blocked",
			rawURL:       "http://localhost:8080",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "127.0.0.1 blocked",
			rawURL:       "http://127.0.0.1:8080",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "10.x private IP blocked",
			rawURL:       "http://10.0.0.1:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "192.168.x private IP blocked",
			rawURL:       "http://192.168.1.100:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "172.16.x private IP blocked",
			rawURL:       "http://172.16.0.5:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "169.254.169.254 metadata blocked",
			rawURL:       "http://169.254.169.254/latest/meta-data/",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         "::1 IPv6 loopback blocked",
			rawURL:       "http://[::1]:8080",
			providerType: "openai_compat",
			wantErr:      true,
		},

		// --- Should BLOCK: DNS bypass via nip.io / sslip.io (H-2 fix) ---
		{
			name:         "nip.io resolving to 10.x blocked",
			rawURL:       "http://10.10.27.30.nip.io:9999/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: DNS resolution check catches nip.io bypass",
		},
		{
			name:         "sslip.io resolving to 192.168.x blocked",
			rawURL:       "http://192.168.1.1.sslip.io:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: DNS resolution check catches sslip.io bypass",
		},
		{
			name:         "nip.io resolving to 172.16.x blocked",
			rawURL:       "http://172.16.0.5.nip.io:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: DNS resolution check catches private 172.16 via nip.io",
		},
		{
			name:         "nip.io resolving to link-local blocked",
			rawURL:       "http://169.254.169.254.nip.io/latest/",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: DNS resolution check catches metadata endpoint via nip.io",
		},
		{
			name:         "nip.io resolving to loopback blocked",
			rawURL:       "http://127.0.0.1.nip.io:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: DNS resolution check catches loopback via nip.io",
		},
		{
			name:         "attacker domain resolving to private IP blocked",
			rawURL:       "http://rebind.attacker.com:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: attacker-controlled domain resolving to 10.0.0.1 blocked",
		},
		{
			name:         "dual-stack with any private IP blocked",
			rawURL:       "http://dual-stack.example.com:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "H-2 fix: if any resolved address is private, reject",
		},

		// --- Should BLOCK: internal hostname suffix ---
		{
			name:         ".internal suffix blocked",
			rawURL:       "http://metadata.google.internal/computeMetadata/v1/",
			providerType: "openai_compat",
			wantErr:      true,
		},
		{
			name:         ".local suffix blocked",
			rawURL:       "http://myservice.local:8080",
			providerType: "openai_compat",
			wantErr:      true,
		},

		// --- Should BLOCK: unresolvable hostname ---
		{
			name:         "unresolvable hostname blocked",
			rawURL:       "http://nonexistent.invalid:8080/v1",
			providerType: "openai_compat",
			wantErr:      true,
			desc:         "fail-closed: unknown hostnames rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProviderURL(tt.rawURL, tt.providerType)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateProviderURL(%q, %q) error = %v, wantErr %v",
					tt.rawURL, tt.providerType, err, tt.wantErr)
			}
		})
	}
}

func TestValidateProviderURL_AllowPrivateFlag(t *testing.T) {
	origAllowPrivate := allowPrivateProviderURLsFn
	origResolver := dnsResolverFn
	defer func() {
		allowPrivateProviderURLsFn = origAllowPrivate
		dnsResolverFn = origResolver
	}()

	dnsResolverFn = func(host string) ([]string, error) {
		return []string{"10.0.0.1"}, nil
	}

	t.Run("private URL allowed when flag is true", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return true }
		err := validateProviderURL("http://my-vllm.lan:8080/v1", "openai_compat")
		if err != nil {
			t.Errorf("expected nil error with allow-private flag, got: %v", err)
		}
	})

	t.Run("private URL blocked when flag is false", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return false }
		err := validateProviderURL("http://my-vllm.lan:8080/v1", "openai_compat")
		if err == nil {
			t.Error("expected error for private URL without allow-private flag")
		}
	})

	t.Run("scheme still enforced even with allow-private flag", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return true }
		err := validateProviderURL("file:///etc/passwd", "openai_compat")
		if err == nil {
			t.Error("expected error for file:// scheme even with allow-private flag")
		}
	})
}
