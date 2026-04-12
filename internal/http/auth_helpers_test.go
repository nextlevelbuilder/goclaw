package http

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Tests for auth helper functions not covered by the existing auth_test.go.
// auth_test.go covers resolveAuth/requireAuth at a higher level.
// These tests exercise the low-level helpers directly.

// ---- extractBearerToken ----

func TestExtractBearerToken_ValidBearer(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer my-secret-token")
	got := extractBearerToken(r)
	if got != "my-secret-token" {
		t.Errorf("extractBearerToken = %q, want %q", got, "my-secret-token")
	}
}

func TestExtractBearerToken_NoAuthHeader_ReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if got := extractBearerToken(r); got != "" {
		t.Errorf("extractBearerToken with no header = %q, want empty", got)
	}
}

func TestExtractBearerToken_BasicScheme_ReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if got := extractBearerToken(r); got != "" {
		t.Errorf("extractBearerToken with Basic scheme = %q, want empty", got)
	}
}

func TestExtractBearerToken_EmptyBearerValue(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer ")
	got := extractBearerToken(r)
	// "Bearer " → TrimPrefix("Bearer ") = "" — returns empty string
	if got != "" {
		t.Errorf("extractBearerToken with empty bearer value = %q, want empty", got)
	}
}

// ---- tokenMatch ----

func TestTokenMatch_EmptyExpected_AlwaysTrue(t *testing.T) {
	// No auth configured → tokenMatch returns true for any provided value.
	if !tokenMatch("anything", "") {
		t.Error("tokenMatch with empty expected should return true (no auth mode)")
	}
	if !tokenMatch("", "") {
		t.Error("tokenMatch with both empty should return true")
	}
}

func TestTokenMatch_CorrectToken_ReturnsTrue(t *testing.T) {
	if !tokenMatch("secret123", "secret123") {
		t.Error("tokenMatch with matching token should return true")
	}
}

func TestTokenMatch_WrongToken_ReturnsFalse(t *testing.T) {
	if tokenMatch("wrong", "secret123") {
		t.Error("tokenMatch with wrong token should return false")
	}
}

func TestTokenMatch_EmptyProvided_ReturnsFalse(t *testing.T) {
	if tokenMatch("", "secret123") {
		t.Error("tokenMatch with empty provided should return false when expected is set")
	}
}

// ---- extractUserID ----

func TestExtractUserID_ValidHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-GoClaw-User-Id", "user-abc-123")
	if got := extractUserID(r); got != "user-abc-123" {
		t.Errorf("extractUserID = %q, want %q", got, "user-abc-123")
	}
}

func TestExtractUserID_NoHeader_ReturnsEmpty(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	if got := extractUserID(r); got != "" {
		t.Errorf("extractUserID with no header = %q, want empty", got)
	}
}

func TestExtractUserID_TooLongID_ReturnsEmpty(t *testing.T) {
	// MaxUserIDLength is 255 — send 256 chars
	longID := strings.Repeat("a", 256)
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-GoClaw-User-Id", longID)
	if got := extractUserID(r); got != "" {
		t.Errorf("extractUserID with too-long ID = %q, want empty (security rejection)", got)
	}
}

// ---- extractAgentID ----

func TestExtractAgentID_FromModelPrefix_Goclaw(t *testing.T) {
	got := extractAgentID(httptest.NewRequest("GET", "/", nil), "goclaw:my-agent")
	if got != "my-agent" {
		t.Errorf("extractAgentID goclaw: prefix = %q, want %q", got, "my-agent")
	}
}

func TestExtractAgentID_FromModelPrefix_Agent(t *testing.T) {
	got := extractAgentID(httptest.NewRequest("GET", "/", nil), "agent:support-bot")
	if got != "support-bot" {
		t.Errorf("extractAgentID agent: prefix = %q, want %q", got, "support-bot")
	}
}

func TestExtractAgentID_FromHeader_XGoClawAgentId(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-GoClaw-Agent-Id", "header-agent")
	got := extractAgentID(r, "some-other-model")
	if got != "header-agent" {
		t.Errorf("extractAgentID from X-GoClaw-Agent-Id = %q, want %q", got, "header-agent")
	}
}

func TestExtractAgentID_FromHeader_XGoClawAgent(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-GoClaw-Agent", "legacy-agent")
	got := extractAgentID(r, "")
	if got != "legacy-agent" {
		t.Errorf("extractAgentID from X-GoClaw-Agent = %q, want %q", got, "legacy-agent")
	}
}

func TestExtractAgentID_DefaultsToDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	got := extractAgentID(r, "gpt-4o")
	if got != "default" {
		t.Errorf("extractAgentID no match = %q, want 'default'", got)
	}
}

func TestExtractAgentID_ModelPrefixTakesPriorityOverHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-GoClaw-Agent-Id", "header-agent")
	got := extractAgentID(r, "goclaw:model-agent")
	if got != "model-agent" {
		t.Errorf("extractAgentID model prefix should take priority, got %q", got)
	}
}

// ---- isHTTPOwnerID ----

func TestIsHTTPOwnerID_MatchesConfiguredOwner(t *testing.T) {
	if !isHTTPOwnerID("alice", []string{"alice", "bob"}) {
		t.Error("alice should be owner")
	}
}

func TestIsHTTPOwnerID_EmptyUserID_NotOwner(t *testing.T) {
	if isHTTPOwnerID("", []string{"alice"}) {
		t.Error("empty user ID should never be owner")
	}
}

func TestIsHTTPOwnerID_EmptyOwnerList_OnlySystemIsOwner(t *testing.T) {
	if !isHTTPOwnerID("system", nil) {
		t.Error("'system' is default owner when no owner IDs configured")
	}
	if isHTTPOwnerID("admin", nil) {
		t.Error("non-system user should not be owner with empty owner list")
	}
}

func TestIsHTTPOwnerID_UnknownUser_NotOwner(t *testing.T) {
	if isHTTPOwnerID("charlie", []string{"alice", "bob"}) {
		t.Error("charlie is not in owner list")
	}
}
