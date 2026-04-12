package agent

import (
	"testing"
)

// TestInvalidateAgent_NoSubstringCollision verifies exact-segment match
// rejects substring collisions. A previous HasSuffix-based matcher would have
// removed "tenantX:sub-foo" when invalidating "foo" — this regression guards
// against that.
func TestInvalidateAgent_NoSubstringCollision(t *testing.T) {
	r := NewRouter()
	r.agents["tenantX:foo"] = &agentEntry{}
	r.agents["tenantX:sub-foo"] = &agentEntry{}

	r.InvalidateAgent("foo")

	if _, ok := r.agents["tenantX:foo"]; ok {
		t.Error("expected tenantX:foo to be removed")
	}
	if _, ok := r.agents["tenantX:sub-foo"]; !ok {
		t.Error("tenantX:sub-foo should NOT be removed by InvalidateAgent(foo) — substring collision")
	}
}

// TestRouterRemove_NoSubstringCollision — same fix applied to Router.Remove.
func TestRouterRemove_NoSubstringCollision(t *testing.T) {
	r := NewRouter()
	r.agents["tenantX:foo"] = &agentEntry{}
	r.agents["tenantX:sub-foo"] = &agentEntry{}

	r.Remove("foo")

	if _, ok := r.agents["tenantX:foo"]; ok {
		t.Error("expected tenantX:foo to be removed")
	}
	if _, ok := r.agents["tenantX:sub-foo"]; !ok {
		t.Error("tenantX:sub-foo should NOT be removed by Remove(foo) — substring collision")
	}
}

// TestMatchAgentCacheKey_EmptyAgentKeyRejected guards against wildcard match.
// An empty agentKey must never match any cache key (would be a global wipe).
func TestMatchAgentCacheKey_EmptyAgentKeyRejected(t *testing.T) {
	cases := []string{"foo", "tenant:foo", "", ":foo"}
	for _, ck := range cases {
		if matchAgentCacheKey(ck, "") {
			t.Errorf("matchAgentCacheKey(%q, \"\") = true, want false", ck)
		}
	}
}

// TestMatchAgentCacheKey_BareKeyMatches verifies that a bare (non-tenant-scoped)
// cache key still matches against its own agentKey for backwards compat.
func TestMatchAgentCacheKey_BareKeyMatches(t *testing.T) {
	if !matchAgentCacheKey("foo", "foo") {
		t.Error("matchAgentCacheKey(foo, foo) should return true")
	}
	if matchAgentCacheKey("bar", "foo") {
		t.Error("matchAgentCacheKey(bar, foo) should return false")
	}
}

// TestMatchAgentCacheKey_TenantScopedExactMatch verifies the canonical case:
// cacheKey "tenantID:agentKey", agentKey "agentKey" → match.
func TestMatchAgentCacheKey_TenantScopedExactMatch(t *testing.T) {
	if !matchAgentCacheKey("tenant-abc:foo", "foo") {
		t.Error("matchAgentCacheKey(tenant-abc:foo, foo) should return true")
	}
	if matchAgentCacheKey("tenant-abc:foobar", "foo") {
		t.Error("matchAgentCacheKey(tenant-abc:foobar, foo) should return false — substring")
	}
}
