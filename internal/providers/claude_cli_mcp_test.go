package providers

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

// --- SignBridgeContext tests ---

func TestSignBridgeContext_Deterministic(t *testing.T) {
	key := "test-secret"
	sig1 := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/workspace", "tenant-abc")
	sig2 := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/workspace", "tenant-abc")
	if sig1 != sig2 {
		t.Errorf("expected deterministic output, got %q and %q", sig1, sig2)
	}
	if sig1 == "" {
		t.Error("expected non-empty signature")
	}
}

func TestSignBridgeContext_DifferentKey(t *testing.T) {
	sig1 := SignBridgeContext("key-a", "agent1", "user1", "", "", "", "", "")
	sig2 := SignBridgeContext("key-b", "agent1", "user1", "", "", "", "", "")
	if sig1 == sig2 {
		t.Error("different keys should produce different signatures")
	}
}

func TestSignBridgeContext_FieldOrder(t *testing.T) {
	key := "test-secret"
	sig1 := SignBridgeContext(key, "a", "b", "c", "d", "e", "f", "g")
	sig2 := SignBridgeContext(key, "b", "a", "c", "d", "e", "f", "g")
	if sig1 == sig2 {
		t.Error("swapping field values should produce different signatures")
	}
}

// --- VerifyBridgeContext tests ---

func TestVerifyBridgeContext_Level1_AllFields(t *testing.T) {
	key := "gateway-token"
	sig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant-123")

	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant-123", sig)
	if !ok {
		t.Error("expected ok=true for valid level 1 signature")
	}
	if !tenantVerified {
		t.Error("expected tenantVerified=true for level 1 match")
	}
}

func TestVerifyBridgeContext_Level2_OldSessionWithWorkspace(t *testing.T) {
	key := "gateway-token"
	// Pre-tenantID session: signed with workspace but empty tenantID.
	// Middleware now receives X-Tenant-ID header (e.g. new code adds it).
	// Level 1 fails (tenantID mismatch), level 2 matches (ignores tenantID).
	sig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "")

	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "new-tenant-id", sig)
	if !ok {
		t.Error("expected ok=true for level 2 fallback")
	}
	if tenantVerified {
		t.Error("expected tenantVerified=false — tenant was not in original signature")
	}
}

func TestVerifyBridgeContext_Level3_NoWorkspaceNoTenant(t *testing.T) {
	key := "gateway-token"
	// Signature from the oldest format (no workspace, no tenantID)
	sig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "", "")

	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant-123", sig)
	if !ok {
		t.Error("expected ok=true for level 3 fallback")
	}
	if tenantVerified {
		t.Error("expected tenantVerified=false for level 3 fallback")
	}
}

func TestVerifyBridgeContext_InvalidSig(t *testing.T) {
	ok, tenantVerified := VerifyBridgeContext("key", "agent1", "user1", "", "", "", "", "", "invalid-sig")
	if ok {
		t.Error("expected ok=false for invalid signature")
	}
	if tenantVerified {
		t.Error("expected tenantVerified=false for invalid signature")
	}
}

func TestVerifyBridgeContext_TenantNotTrustedOnFallback(t *testing.T) {
	key := "gateway-token"
	// Old session signed WITHOUT tenantID
	oldSig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "")

	// Attacker replays old sig but adds a fake tenantID header
	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "fake-tenant-id", oldSig)
	if !ok {
		t.Error("expected ok=true (sig valid via fallback)")
	}
	if tenantVerified {
		t.Error("expected tenantVerified=false — tenant header not covered by HMAC, must not be trusted")
	}
}

func TestVerifyBridgeContext_EmptyFields(t *testing.T) {
	key := "test-key"
	sig := SignBridgeContext(key, "", "", "", "", "", "", "")

	ok, tenantVerified := VerifyBridgeContext(key, "", "", "", "", "", "", "", sig)
	if !ok {
		t.Error("expected ok=true for empty fields with valid signature")
	}
	// Empty fields match at all levels; level 1 matches first → tenantVerified=true
	if !tenantVerified {
		t.Error("expected tenantVerified=true when all fields empty (level 1 matches)")
	}
}

// --- Extra params (localKey, sessionKey) tests ---

func TestSignBridgeContext_WithExtraParams(t *testing.T) {
	key := "test-secret"
	// Without extra params
	sig1 := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1")
	// With extra params
	sig2 := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1", "-100123:topic:42", "session-abc")

	if sig1 == sig2 {
		t.Error("signature with extra params should differ from signature without")
	}
}

func TestVerifyBridgeContext_WithExtraParams(t *testing.T) {
	key := "gateway-token"
	localKey := "-100123:topic:42"
	sessionKey := "session-abc"
	sig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1", localKey, sessionKey)

	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1", sig, localKey, sessionKey)
	if !ok {
		t.Error("expected ok=true for valid signature with extra params")
	}
	if !tenantVerified {
		t.Error("expected tenantVerified=true for full match")
	}
}

func TestVerifyBridgeContext_FallbackWithoutExtraParams(t *testing.T) {
	key := "gateway-token"
	// Pre-localKey session: signed without extra params
	sig := SignBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1")

	// New code passes localKey/sessionKey but signature was created without them
	ok, tenantVerified := VerifyBridgeContext(key, "agent1", "user1", "telegram", "chat1", "direct", "/ws", "tenant1", sig, "-100123:topic:42", "session-abc")
	if !ok {
		t.Error("expected ok=true for fallback (pre-localKey session)")
	}
	if !tenantVerified {
		t.Error("expected tenantVerified=true — base fields match at fallback level")
	}
}

func TestVerifyBridgeContext_ExtraParamOrderMatters(t *testing.T) {
	key := "gateway-token"
	sig := SignBridgeContext(key, "agent1", "user1", "", "", "", "", "", "localKey", "sessionKey")

	// Verify with same order
	ok, _ := VerifyBridgeContext(key, "agent1", "user1", "", "", "", "", "", sig, "localKey", "sessionKey")
	if !ok {
		t.Error("expected ok=true for same order")
	}

	// Verify with swapped order
	ok2, _ := VerifyBridgeContext(key, "agent1", "user1", "", "", "", "", "", sig, "sessionKey", "localKey")
	if ok2 {
		t.Error("expected ok=false for swapped extra param order")
	}
}

func TestBridgeContextFromOptsCarriesDelegationArtifactContext(t *testing.T) {
	got := bridgeContextFromOpts(map[string]any{
		OptAgentID:          "agent-id",
		OptWorkspace:        "/runtime/outputs",
		OptDelegationID:     "delegation-id",
		OptDelegationInputs: "/runtime/inputs",
	})

	if got.DelegationID != "delegation-id" || got.DelegationInputs != "/runtime/inputs" {
		t.Fatalf("delegation bridge context = %#v", got)
	}
}

func TestWriteMCPConfigSignsDelegationArtifactHeaders(t *testing.T) {
	t.Setenv("GOCLAW_DATA_DIR", t.TempDir())
	const (
		token      = "gateway-token"
		sessionKey = "session-key"
	)
	data := &MCPConfigData{GatewayAddr: "127.0.0.1:18790", GatewayToken: token}
	bc := BridgeContext{
		AgentID:          "agent-id",
		Workspace:        "/runtime/outputs",
		DelegationID:     "delegation-id",
		DelegationInputs: "/runtime/inputs",
	}
	path := data.WriteMCPConfig(context.Background(), sessionKey, bc)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	headers := cfg.MCPServers["goclaw-bridge"].Headers
	if headers["X-Delegation-ID"] != bc.DelegationID ||
		headers["X-Delegation-Inputs"] != bc.DelegationInputs {
		t.Fatalf("delegation headers = %#v", headers)
	}
	wantSig := SignBridgeContext(
		token,
		bc.AgentID,
		"",
		"",
		"",
		"",
		bc.Workspace,
		"",
		"",
		sessionKey,
		bc.DelegationID,
		bc.DelegationInputs,
	)
	if headers["X-Bridge-Sig"] != wantSig {
		t.Fatal("delegation headers were not covered by the exact bridge signature")
	}
}
