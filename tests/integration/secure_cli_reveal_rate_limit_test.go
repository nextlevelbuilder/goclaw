//go:build integration

package integration

// C4 rate-limit test: verify the per-caller reveal rate limiter behavior.
// Uses SetEnvRevealLimiter to configure tight limits and HandleRevealEnvForTest
// to call the handler without the requireAuth middleware (auth is injected via ctx).

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	httphandler "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// buildRevealCtxRequest constructs a reveal request with owner-role context so
// requireTenantAdmin is bypassed (IsOwnerRole short-circuits the tenant check).
func buildRevealCtxRequest(binaryID, grantID uuid.UUID, tenantID uuid.UUID, userID string) *http.Request {
	path := "/v1/cli-credentials/" + binaryID.String() +
		"/agent-grants/" + grantID.String() + "/env:reveal"
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("grantId", grantID.String())

	ctx := store.WithTenantID(req.Context(), tenantID)
	ctx = store.WithUserID(ctx, userID)
	// Owner role bypasses requireTenantAdmin (ts.GetUserRole call) — safe for unit tests.
	ctx = store.WithRole(ctx, store.TenantRoleOwner)
	return req.WithContext(ctx)
}

// TestRevealRateLimit_PerCallerBuckets verifies:
// 1. Caller A hitting the burst limit gets 429 on subsequent calls.
// 2. Caller B (different UserID) is NOT affected by caller A's exhaustion.
func TestRevealRateLimit_PerCallerBuckets(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)

	g := &store.SecureCLIAgentGrant{BinaryID: binaryID, AgentID: agentID, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create grant: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	handler := httphandler.NewSecureCLIGrantHandler(grantStore, nil, nil)
	// Tight limit: 1 rpm, burst 1 → 2nd call must be rejected.
	handler.SetEnvRevealLimiter(1, 1)

	callerA := "user-a-" + uuid.New().String()[:8]
	callerB := "user-b-" + uuid.New().String()[:8]

	callReveal := func(userID string) int {
		rr := httptest.NewRecorder()
		req := buildRevealCtxRequest(binaryID, g.ID, tenantID, userID)
		handler.HandleRevealEnvForTest(rr, req)
		return rr.Code
	}

	// First call for A: within burst, must succeed (200 or 404 if no env).
	code1A := callReveal(callerA)
	if code1A == http.StatusTooManyRequests {
		t.Errorf("callerA call 1: should not be rate-limited on first call, got 429")
	}

	// Second call for A: over limit (burst=1, only 1 allowed).
	code2A := callReveal(callerA)
	if code2A != http.StatusTooManyRequests {
		t.Errorf("callerA call 2: want 429 (rate limited), got %d", code2A)
	}

	// First call for B: fresh bucket, must not be limited.
	code1B := callReveal(callerB)
	if code1B == http.StatusTooManyRequests {
		t.Errorf("callerB call 1: should not be rate-limited (different bucket), got 429")
	}
}

// TestRevealRateLimit_ContextUserIDNotHeader verifies that the rate limit key
// comes from the context-injected UserID (authenticated), not the X-GoClaw-User-Id header.
func TestRevealRateLimit_ContextUserIDNotHeader(t *testing.T) {
	t.Parallel()

	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	binaryID := seedSecureCLI(t, db, tenantID)

	grantStore := pg.NewPGSecureCLIAgentGrantStore(db, testEncryptionKey)

	g := &store.SecureCLIAgentGrant{BinaryID: binaryID, AgentID: agentID, Enabled: true}
	if err := grantStore.Create(tenantCtx(tenantID), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM secure_cli_agent_grants WHERE id = $1", g.ID) })

	handler := httphandler.NewSecureCLIGrantHandler(grantStore, nil, nil)
	handler.SetEnvRevealLimiter(1, 1)

	realUserA := "real-user-" + uuid.New().String()[:8]

	// Exhaust real user A.
	path := "/v1/cli-credentials/" + binaryID.String() +
		"/agent-grants/" + g.ID.String() + "/env:reveal"

	makeReq := func(contextUser, headerUser string) int {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.SetPathValue("id", binaryID.String())
		req.SetPathValue("grantId", g.ID.String())
		if headerUser != "" {
			req.Header.Set("X-GoClaw-User-Id", headerUser)
		}
		ctx := store.WithTenantID(req.Context(), tenantID)
		if contextUser != "" {
			ctx = store.WithUserID(ctx, contextUser)
		}
		ctx = store.WithRole(ctx, store.TenantRoleOwner)
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		handler.HandleRevealEnvForTest(rr, req)
		return rr.Code
	}

	// Exhaust user A's bucket.
	_ = makeReq(realUserA, "")                 // call 1 — within limit
	code2 := makeReq(realUserA, "")            // call 2 — over limit
	if code2 != http.StatusTooManyRequests {
		t.Errorf("real user A call 2: want 429, got %d", code2)
	}

	// Attempt to spoof a different user via header while context still has realUserA.
	// Context user wins → still rate-limited.
	codeSpoof := makeReq(realUserA, "attacker-different-user")
	if codeSpoof != http.StatusTooManyRequests {
		t.Errorf("header spoof should not escape rate limit when context user is exhausted; got %d", codeSpoof)
	}
}
