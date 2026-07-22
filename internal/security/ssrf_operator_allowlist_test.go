package security

import (
	"net"
	"strings"
	"testing"
)

// The escape hatch must be inert unless an operator opts in — an empty or
// absent env var has to leave the shipped block list untouched.
func TestParseOperatorAllowedCIDRs_EmptyIsInert(t *testing.T) {
	t.Parallel()

	for _, spec := range []string{"", "   ", ",", " , , "} {
		nets, rejected := parseOperatorAllowedCIDRs(spec)
		if len(nets) != 0 {
			t.Fatalf("spec %q produced %d allowed nets, want 0", spec, len(nets))
		}
		if len(rejected) != 0 {
			t.Fatalf("spec %q produced rejections %v, want none", spec, rejected)
		}
	}
}

// The case the hatch exists for: a TUN/fake-IP proxy hands out RFC 2544
// addresses that route to real public hosts.
func TestParseOperatorAllowedCIDRs_AcceptsBenchmarkingRange(t *testing.T) {
	t.Parallel()

	nets, rejected := parseOperatorAllowedCIDRs("198.18.0.0/15")
	if len(rejected) != 0 {
		t.Fatalf("unexpected rejections: %v", rejected)
	}
	if len(nets) != 1 {
		t.Fatalf("got %d nets, want 1", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("198.18.0.236")) {
		t.Fatal("parsed net does not contain the address it was configured for")
	}
}

// Cloud metadata is the canonical SSRF target. No operator setting may open a
// path to it — not directly, and not by way of a wider range that swallows it.
func TestParseOperatorAllowedCIDRs_RefusesProtectedRanges(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		spec string
	}{
		{"link-local exactly", "169.254.0.0/16"},
		{"cloud metadata host", "169.254.169.254/32"},
		{"a wider range swallowing link-local", "169.0.0.0/8"},
		{"everything", "0.0.0.0/0"},
		{"multicast", "224.0.0.0/4"},
		{"unspecified", "0.0.0.0/32"},
		{"ipv6 link-local", "fe80::/10"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			nets, rejected := parseOperatorAllowedCIDRs(tc.spec)
			if len(nets) != 0 {
				t.Fatalf("%q was accepted; it must never be allowlistable", tc.spec)
			}
			if len(rejected) != 1 {
				t.Fatalf("got %d rejections for %q, want 1", len(rejected), tc.spec)
			}
			if !strings.Contains(rejected[0], "overlaps") {
				t.Fatalf("rejection %q does not explain the overlap", rejected[0])
			}
		})
	}
}

// A rejected entry must be reported, never silently dropped — an operator who
// mistyped one range out of several would otherwise believe it took effect.
func TestParseOperatorAllowedCIDRs_ReportsBadEntriesAndKeepsGoodOnes(t *testing.T) {
	t.Parallel()

	nets, rejected := parseOperatorAllowedCIDRs("198.18.0.0/15, not-a-cidr, 169.254.0.0/16, 10.0.0.0/8")

	if len(nets) != 2 {
		t.Fatalf("got %d accepted nets, want 2 (198.18.0.0/15 and 10.0.0.0/8)", len(nets))
	}
	if len(rejected) != 2 {
		t.Fatalf("got %d rejections, want 2; rejected=%v", len(rejected), rejected)
	}

	joined := strings.Join(rejected, " | ")
	if !strings.Contains(joined, "not a CIDR") {
		t.Fatalf("malformed entry not reported: %v", rejected)
	}
	if !strings.Contains(joined, "169.254.0.0/16") {
		t.Fatalf("protected entry not reported: %v", rejected)
	}
}

// isBlocked is the single gate both the pre-flight check and the dial-time
// re-check go through, so the allowlist has to take effect inside it.
func TestIsBlocked_HonorsOperatorAllowlist(t *testing.T) {
	saved := operatorAllowedCIDRs
	t.Cleanup(func() { operatorAllowedCIDRs = saved })

	fakeIP := net.ParseIP("198.18.0.236")
	metadata := net.ParseIP("169.254.169.254")
	privateIP := net.ParseIP("10.1.2.3")

	if !isBlocked(fakeIP) {
		t.Fatal("198.18.0.236 should be blocked with no allowlist configured")
	}

	nets, _ := parseOperatorAllowedCIDRs("198.18.0.0/15")
	operatorAllowedCIDRs = nets

	if isBlocked(fakeIP) {
		t.Fatal("198.18.0.236 should be permitted once its range is allowlisted")
	}
	if !isBlocked(metadata) {
		t.Fatal("cloud metadata must stay blocked regardless of the allowlist")
	}
	if !isBlocked(privateIP) {
		t.Fatal("RFC 1918 must stay blocked when only 198.18.0.0/15 is allowlisted")
	}
}

// Validate is what web_fetch calls; it must agree with isBlocked so a URL that
// passes validation can actually be dialed.
func TestValidate_AllowsAllowlistedRangeEndToEnd(t *testing.T) {
	saved := operatorAllowedCIDRs
	t.Cleanup(func() { operatorAllowedCIDRs = saved })

	nets, _ := parseOperatorAllowedCIDRs("198.18.0.0/15")
	operatorAllowedCIDRs = nets

	// Resolution is what makes this range interesting, so drive isBlocked with
	// a literal-IP URL to keep the test off the network.
	if _, _, err := validate("https://198.18.0.236/", false, nil); err != nil {
		t.Fatalf("allowlisted address rejected by validate: %v", err)
	}
	if _, _, err := validate("https://169.254.169.254/", false, nil); err == nil {
		t.Fatal("cloud metadata passed validate while an allowlist was configured")
	}
}

func TestOperatorAllowlistStatus_ReportsConfiguredRanges(t *testing.T) {
	savedNets, savedRejected := operatorAllowedCIDRs, operatorAllowlistRejected
	t.Cleanup(func() {
		operatorAllowedCIDRs, operatorAllowlistRejected = savedNets, savedRejected
	})

	operatorAllowedCIDRs, operatorAllowlistRejected = parseOperatorAllowedCIDRs("198.18.0.0/15, 169.254.0.0/16")

	allowed, rejected := OperatorAllowlistStatus()
	if len(allowed) != 1 || allowed[0] != "198.18.0.0/15" {
		t.Fatalf("allowed = %v, want [198.18.0.0/15]", allowed)
	}
	if len(rejected) != 1 {
		t.Fatalf("rejected = %v, want one entry", rejected)
	}
}
