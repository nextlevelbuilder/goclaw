package tools

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// withOperatorAllowlist installs an operator allowlist for the duration of t.
func withOperatorAllowlist(t *testing.T, spec string) {
	t.Helper()
	t.Cleanup(security.SetOperatorAllowlistForTest(spec))
}

// internal/tools carries its own private-range list, separate from
// internal/security. The operator allowlist has to reach both: relaxing only
// the security package left web_fetch rejecting exactly the addresses the
// operator had just permitted.
func TestCheckSSRF_HonorsOperatorAllowlist(t *testing.T) {
	// 198.18.0.0/15 is in the local list, so with no allowlist it must fail.
	if err := CheckSSRF("https://198.18.0.236/"); err == nil {
		t.Fatal("198.18.0.236 accepted with no allowlist configured")
	}

	withOperatorAllowlist(t, "198.18.0.0/15")

	if err := CheckSSRF("https://198.18.0.236/"); err != nil {
		t.Fatalf("allowlisted address still rejected by CheckSSRF: %v", err)
	}
}

// Whatever the operator allowlists, the ranges an SSRF actually targets stay
// unreachable.
func TestCheckSSRF_KeepsProtectedRangesBlocked(t *testing.T) {
	withOperatorAllowlist(t, "198.18.0.0/15")

	for _, target := range []string{
		"https://169.254.169.254/latest/meta-data/", // cloud metadata
		"https://10.0.0.1/",                         // RFC 1918
		"https://127.0.0.1/",                        // loopback
	} {
		if err := CheckSSRF(target); err == nil {
			t.Fatalf("%s was accepted while only 198.18.0.0/15 was allowlisted", target)
		}
	}
}
