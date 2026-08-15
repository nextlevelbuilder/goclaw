package security

import "testing"

// ---- ValidateAllowingHosts (operator MCP host allowlist) ----

func TestValidateAllowingHosts_AllowsAllowlistedPrivateIP(t *testing.T) {
	allowed := map[string]bool{"10.1.2.3": true}
	if _, _, err := ValidateAllowingHosts("http://10.1.2.3/mcp", allowed); err != nil {
		t.Fatalf("expected allowlisted private IP to pass, got %v", err)
	}
}

func TestValidateAllowingHosts_RejectsNonAllowlistedPrivateIP(t *testing.T) {
	allowed := map[string]bool{"10.9.9.9": true}
	if _, _, err := ValidateAllowingHosts("http://10.1.2.3/mcp", allowed); err == nil {
		t.Fatal("expected non-allowlisted private IP to be rejected")
	}
}

// The metadata / link-local range must never be exempted, even if an operator
// explicitly allowlists it — this is the crown-jewel SSRF target.
func TestValidateAllowingHosts_NeverExemptsMetadataEndpoint(t *testing.T) {
	allowed := map[string]bool{"169.254.169.254": true}
	if _, _, err := ValidateAllowingHosts("http://169.254.169.254/latest/meta-data", allowed); err == nil {
		t.Fatal("metadata/link-local must never be exempted by the allowlist")
	}
}

func TestValidateAllowingHosts_NilAllowlistMatchesValidate(t *testing.T) {
	if _, _, err := ValidateAllowingHosts("http://10.1.2.3/", nil); err == nil {
		t.Fatal("nil allowlist must reject private IPs like Validate")
	}
}

func TestValidateAllowingHosts_PublicIPUnaffected(t *testing.T) {
	if _, _, err := ValidateAllowingHosts("http://8.8.8.8/", map[string]bool{"10.0.0.1": true}); err != nil {
		t.Fatalf("public IP should pass regardless of allowlist, got %v", err)
	}
}

func TestValidateAllowingHosts_AllowsAllowlistedLoopback(t *testing.T) {
	allowed := map[string]bool{"127.0.0.1": true}
	if _, _, err := ValidateAllowingHosts("http://127.0.0.1/mcp", allowed); err != nil {
		t.Fatalf("expected allowlisted loopback to pass, got %v", err)
	}
}
