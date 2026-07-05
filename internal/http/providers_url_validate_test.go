package http

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// stubResolver returns a net.LookupHost-compatible function that maps known
// hostnames to controlled IPs. Unknown hostnames return a DNS error.
func stubResolver(m map[string][]string) func(host string) ([]string, error) {
	return func(host string) ([]string, error) {
		if addrs, ok := m[host]; ok {
			return addrs, nil
		}
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
}

// saveAndRestoreGlobals saves mutable package-level vars and restores them after
// the test. Call at the start of any test that touches dnsResolverFn,
// allowPrivateProviderURLsFn, or ollamaAllowedHostsFn.
func saveAndRestoreGlobals(t *testing.T) {
	t.Helper()
	origResolver := dnsResolverFn
	origAllow := allowPrivateProviderURLsFn
	origOllamaAllowedHosts := ollamaAllowedHostsFn
	t.Cleanup(func() {
		dnsResolverFn = origResolver
		allowPrivateProviderURLsFn = origAllow
		ollamaAllowedHostsFn = origOllamaAllowedHosts
	})
}

// --- Core merged test: all major cases in one table ---

func TestValidateProviderURL(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }
	ollamaAllowedHostsFn = func() []string { return nil }
	absClaudePath := filepath.Join(t.TempDir(), "claude")

	dnsResolverFn = stubResolver(map[string][]string{
		"api.openai.com":         {"104.18.6.192"},
		"legit-provider.com":     {"203.0.113.50"},
		"10.10.27.30.nip.io":     {"10.10.27.30"},
		"192.168.1.1.sslip.io":   {"192.168.1.1"},
		"172.16.0.5.nip.io":      {"172.16.0.5"},
		"169.254.169.254.nip.io": {"169.254.169.254"},
		"127.0.0.1.nip.io":       {"127.0.0.1"},
		"rebind.attacker.com":    {"10.0.0.1"},
		"dual-stack.example.com": {"203.0.113.50", "10.0.0.1"},
	})

	tests := []struct {
		name         string
		rawURL       string
		providerType string
		wantErr      bool
	}{
		// Empty URL always allowed
		{"empty URL", "", "openai_compat", false},
		{"empty URL ollama", "", "ollama", false},

		// Public remote URLs OK
		{"public HTTPS", "https://api.openai.com/v1", "openai_compat", false},
		{"public HTTP", "http://legit-provider.com/v1", "openai_compat", false},

		// --- Scheme check: unconditional for ALL types including local ---
		{"file scheme remote", "file:///etc/passwd", "openai_compat", true},
		{"gopher scheme remote", "gopher://internal:25", "openai_compat", true},
		{"file scheme ollama", "file:///etc/passwd", "ollama", true},       // H-1: scheme enforced even for local types
		{"gopher scheme acp", "gopher://localhost:25", "acp", true},        // H-1: scheme enforced even for local types
		{"file scheme claude_cli", "file:///bin/bash", "claude_cli", true}, // H-1: scheme enforced for URL-like Claude CLI values

		// --- Local type: allowlist-only ---
		{"ollama localhost", "http://localhost:11434/v1", "ollama", false},
		{"ollama 127.0.0.1", "http://127.0.0.1:11434/v1", "ollama", false},
		{"ollama ::1", "http://[::1]:11434/v1", "ollama", false},
		{"ollama host.docker.internal", "http://host.docker.internal:11434/v1", "ollama", false},
		{"acp 127.0.0.1", "http://127.0.0.1:9090", "acp", false},
		{"claude_cli command name", "claude", "claude_cli", false},
		{"claude_cli absolute path", absClaudePath, "claude_cli", false},

		// Local type with non-localhost hosts → blocked
		{"ollama 169.254.169.254", "http://169.254.169.254/latest/meta-data/", "ollama", true},
		{"ollama private IP", "http://10.0.0.5:11434/v1", "ollama", true},
		{"ollama evil.com", "http://evil.attacker.tld:9999/v1", "ollama", true},
		{"ollama postgres sidecar", "http://postgres:5432/v1", "ollama", true},
		{"ollama link-local", "http://169.254.1.1:8080/v1", "ollama", true},
		{"ollama .internal", "http://redis.internal:6379/v1", "ollama", true},
		{"ollama gcp metadata", "http://metadata.google.internal/computeMetadata/v1/", "ollama", true},
		{"acp private", "http://10.0.0.1:8080/v1", "acp", true},

		// --- Remote type literal blocked IPs ---
		{"remote localhost", "http://localhost:8080", "openai_compat", true},
		{"remote 127.0.0.1", "http://127.0.0.1:8080", "openai_compat", true},
		{"remote ::1", "http://[::1]:8080", "openai_compat", true},
		{"remote 10.x", "http://10.0.0.1:8080/v1", "openai_compat", true},
		{"remote 192.168.x", "http://192.168.1.100:8080/v1", "openai_compat", true},
		{"remote 172.16.x", "http://172.16.0.5:8080/v1", "openai_compat", true},
		{"remote 169.254.169.254", "http://169.254.169.254/latest/meta-data/", "openai_compat", true},

		// --- Regression: unspecified addresses (bug missed by #974 hand-rolled checks) ---
		{"0.0.0.0 unspecified", "http://0.0.0.0:8080/v1", "openai_compat", true},
		{":: IPv6 unspecified", "http://[::]:8080/v1", "openai_compat", true},

		// --- DNS bypass via wildcard services (nip.io, sslip.io) ---
		{"nip.io 10.x", "http://10.10.27.30.nip.io:9999/v1", "openai_compat", true},
		{"sslip.io 192.168.x", "http://192.168.1.1.sslip.io:8080/v1", "openai_compat", true},
		{"nip.io 172.16.x", "http://172.16.0.5.nip.io:8080/v1", "openai_compat", true},
		{"nip.io link-local", "http://169.254.169.254.nip.io/latest/", "openai_compat", true},
		{"nip.io loopback", "http://127.0.0.1.nip.io:8080/v1", "openai_compat", true},
		{"attacker rebind", "http://rebind.attacker.com:8080/v1", "openai_compat", true},
		{"dual-stack any private", "http://dual-stack.example.com:8080/v1", "openai_compat", true},

		// --- Internal hostname suffix ---
		{".internal suffix", "http://metadata.google.internal/computeMetadata/v1/", "openai_compat", true},
		{".local suffix", "http://myservice.local:8080", "openai_compat", true},

		// --- Unresolvable hostname ---
		{"unresolvable hostname", "http://nonexistent.invalid:8080/v1", "openai_compat", true},
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

// --- Env gate: GOCLAW_ALLOW_PRIVATE_PROVIDER_URLS ---

func TestValidateProviderURL_AllowPrivateFlag(t *testing.T) {
	saveAndRestoreGlobals(t)

	dnsResolverFn = stubResolver(map[string][]string{
		"my-vllm.lan": {"10.0.0.1"},
	})

	t.Run("private URL allowed when flag is true", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return true }
		if err := validateProviderURL("http://my-vllm.lan:8080/v1", "openai_compat"); err != nil {
			t.Errorf("expected nil with allow-private flag, got: %v", err)
		}
		if err := validateProviderURL("http://192.168.1.50:8080/v1", "openai_compat"); err != nil {
			t.Errorf("expected nil for private IP with allow-private flag, got: %v", err)
		}
		if err := validateProviderURL("http://localhost:8080/v1", "openai_compat"); err != nil {
			t.Errorf("expected nil for localhost with allow-private flag, got: %v", err)
		}
		if err := validateProviderURL("http://llm.internal/v1", "openai_compat"); err != nil {
			t.Errorf("expected nil for .internal with allow-private flag, got: %v", err)
		}
	})

	t.Run("private URL blocked when flag is false", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return false }
		if err := validateProviderURL("http://my-vllm.lan:8080/v1", "openai_compat"); err == nil {
			t.Error("expected error for private URL without allow-private flag")
		}
	})

	t.Run("scheme still enforced even with allow-private flag", func(t *testing.T) {
		allowPrivateProviderURLsFn = func() bool { return true }
		err := validateProviderURL("file:///etc/passwd", "openai_compat")
		if err == nil {
			t.Error("expected error for file:// scheme even with allow-private flag")
		}
		if !strings.Contains(err.Error(), "scheme") {
			t.Errorf("expected scheme error, got: %v", err)
		}
	})
}

func TestValidateProviderURL_LocalTypesIgnoreAllowPrivateFlag(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return true }

	cases := []struct {
		url          string
		providerType string
	}{
		{"http://ollama:11434/v1", "ollama"},
		{"http://host.lan:11434/v1", "ollama"},
		{"http://10.0.0.5:11434/v1", "ollama"},
		{"http://acp-sidecar:9090", "acp"},
	}
	for _, c := range cases {
		if err := validateProviderURL(c.url, c.providerType); err == nil {
			t.Errorf("expected local provider URL %s / %s to remain blocked despite allow-private flag", c.url, c.providerType)
		}
	}
}

// --- H-1: scheme enforced for local provider types (from PR #972) ---

func TestValidateProviderURL_LocalTypeSchemeEnforced(t *testing.T) {
	saveAndRestoreGlobals(t)

	cases := []struct {
		url          string
		providerType string
	}{
		{"file:///etc/passwd", "ollama"},
		{"gopher://localhost:25", "ollama"},
		{"file:///etc/passwd", "acp"},
	}
	for _, c := range cases {
		err := validateProviderURL(c.url, c.providerType)
		if err == nil {
			t.Errorf("expected scheme error for %s / %s, got nil", c.url, c.providerType)
			continue
		}
		if !strings.Contains(err.Error(), "scheme") {
			t.Errorf("expected scheme error for %s / %s, got: %v", c.url, c.providerType, err)
		}
	}
}

// --- Regression: 0.0.0.0 and :: unspecified (bug PR #974 missed) ---

func TestValidateProviderURL_UnspecifiedBlocked(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }

	cases := []string{
		"http://0.0.0.0:8080/v1",
		"http://[::]:8080/v1",
	}
	for _, u := range cases {
		err := validateProviderURL(u, "openai_compat")
		if err == nil {
			t.Errorf("expected error for unspecified address %q, got nil", u)
		}
	}
}

// --- DNS-rebinding: remote type resolving to private/loopback ---

func TestValidateProviderURL_DNSRebindBlocked(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }

	privateHosts := map[string][]string{
		"rebind-private.example.com":   {"10.0.0.1"},
		"rebind-loopback.example.com":  {"127.0.0.1"},
		"rebind-linklocal.example.com": {"169.254.169.254"},
	}
	dnsResolverFn = stubResolver(privateHosts)

	for host := range privateHosts {
		err := validateProviderURL("http://"+host+":8080/v1", "openai_compat")
		if err == nil {
			t.Errorf("expected DNS rebind to be blocked for %s, got nil", host)
		}
	}
}

// --- Local type: all allowed variants ---

func TestValidateProviderURL_LocalTypeAllowedHosts(t *testing.T) {
	saveAndRestoreGlobals(t)

	allowed := []struct {
		url          string
		providerType string
	}{
		{"http://localhost:11434/v1", "ollama"},
		{"http://127.0.0.1:11434/v1", "ollama"},
		{"http://[::1]:11434/v1", "ollama"},
		{"http://host.docker.internal:11434/v1", "ollama"},
		{"http://localhost:9090", "acp"},
		{"http://127.0.0.1:9090", "acp"},
	}
	for _, a := range allowed {
		if err := validateProviderURL(a.url, a.providerType); err != nil {
			t.Errorf("expected %s / %s to be allowed, got: %v", a.url, a.providerType, err)
		}
	}
}

func TestValidateProviderURL_ClaudeCLIExecutablePath(t *testing.T) {
	saveAndRestoreGlobals(t)
	absClaudePath := filepath.Join(t.TempDir(), "claude")

	allowed := []string{
		"",
		"claude",
		absClaudePath,
		filepath.Join(t.TempDir(), "Claude Code.app", "Contents", "MacOS", "claude"),
	}
	for _, raw := range allowed {
		if err := validateProviderURL(raw, "claude_cli"); err != nil {
			t.Errorf("expected Claude CLI executable %q to be allowed, got: %v", raw, err)
		}
	}

	blocked := []string{
		"file:///usr/local/bin/claude",
		"https://api.anthropic.com/v1",
		"relative/claude",
		"claude --dangerously-skip-permissions",
	}
	for _, raw := range blocked {
		if err := validateProviderURL(raw, "claude_cli"); err == nil {
			t.Errorf("expected Claude CLI executable %q to be rejected", raw)
		}
	}
}

// --- Public remote URL always OK ---

func TestValidateProviderURL_PublicHostOK(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }
	dnsResolverFn = stubResolver(map[string][]string{
		"api.openai.com": {"104.18.6.192"},
	})

	if err := validateProviderURL("https://api.openai.com/v1", "openai_compat"); err != nil {
		t.Errorf("expected public host to pass, got: %v", err)
	}
}

// --- GOCLAW_OLLAMA_ALLOWED_HOSTS opt-in for local provider types ---

func TestValidateProviderURL_OllamaAllowedHosts(t *testing.T) {
	saveAndRestoreGlobals(t)
	allowPrivateProviderURLsFn = func() bool { return false }

	t.Run("rejected when not configured", func(t *testing.T) {
		ollamaAllowedHostsFn = func() []string { return nil }
		if err := validateProviderURL("http://192.168.3.31:11434/v1", "ollama"); err == nil {
			t.Error("expected LAN host to be rejected when GOCLAW_OLLAMA_ALLOWED_HOSTS is not configured")
		}
	})

	t.Run("accepted when present in configured list", func(t *testing.T) {
		ollamaAllowedHostsFn = func() []string { return []string{"192.168.3.31", "ollama.lan"} }
		if err := validateProviderURL("http://192.168.3.31:11434/v1", "ollama"); err != nil {
			t.Errorf("expected configured LAN IP to be allowed, got: %v", err)
		}
		if err := validateProviderURL("http://ollama.lan:11434/v1", "acp"); err != nil {
			t.Errorf("expected configured LAN hostname to be allowed for acp, got: %v", err)
		}
		// Case-insensitive host match, matching the existing style in this file.
		if err := validateProviderURL("http://OLLAMA.LAN:11434/v1", "ollama"); err != nil {
			t.Errorf("expected case-insensitive host match to be allowed, got: %v", err)
		}
	})

	t.Run("rejected when host not in configured list", func(t *testing.T) {
		ollamaAllowedHostsFn = func() []string { return []string{"192.168.3.31"} }
		if err := validateProviderURL("http://10.0.0.9:11434/v1", "ollama"); err == nil {
			t.Error("expected host outside the configured list to be rejected")
		}
	})

	t.Run("default localhost behavior unchanged when list configured", func(t *testing.T) {
		ollamaAllowedHostsFn = func() []string { return []string{"192.168.3.31"} }
		if err := validateProviderURL("http://localhost:11434/v1", "ollama"); err != nil {
			t.Errorf("expected localhost to still be allowed, got: %v", err)
		}
	})
}
