package mcp

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// The package init() enables the loopback/private bypass for most validation
// tests; the SSRF allowlist tests below disable it so the private-IP block is
// actually exercised, then restore it.

func TestSetAllowedHosts_ExemptsPrivateIPForMCP(t *testing.T) {
	security.SetAllowLoopbackForTest(false)
	defer security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { SetAllowedHosts(nil) })

	SetAllowedHosts([]string{"10.1.2.3"})
	if err := ValidateURL("http://10.1.2.3/mcp"); err != nil {
		t.Fatalf("allowlisted private MCP host should validate, got %v", err)
	}
	if err := ValidateServerConfig("streamable-http", "", nil, "http://10.1.2.3/mcp"); err != nil {
		t.Fatalf("allowlisted private MCP host should pass ValidateServerConfig, got %v", err)
	}
}

func TestSetAllowedHosts_DefaultStillBlocksPrivate(t *testing.T) {
	security.SetAllowLoopbackForTest(false)
	defer security.SetAllowLoopbackForTest(true)
	SetAllowedHosts(nil)

	if err := ValidateURL("http://10.1.2.3/mcp"); err == nil {
		t.Fatal("private MCP host must be rejected when allowlist is empty")
	}
}

func TestSetAllowedHosts_NeverExemptsMetadata(t *testing.T) {
	security.SetAllowLoopbackForTest(false)
	defer security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { SetAllowedHosts(nil) })

	SetAllowedHosts([]string{"169.254.169.254"})
	if err := ValidateURL("http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("metadata endpoint must stay blocked even if allowlisted")
	}
}

func TestSetAllowedHosts_NormalizesEntries(t *testing.T) {
	t.Cleanup(func() { SetAllowedHosts(nil) })
	SetAllowedHosts([]string{"  MCP.Internal.Example  ", ""})
	if !allowedHosts()["mcp.internal.example"] {
		t.Fatal("expected entry to be trimmed and lowercased")
	}
	if _, ok := allowedHosts()[""]; ok {
		t.Fatal("empty entry must be dropped")
	}
}
