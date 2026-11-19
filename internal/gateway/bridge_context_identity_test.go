package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const bridgeTestToken = "test-gateway-token"

// stubConfigPermStore records grant lookups and answers with a fixed decision.
type stubConfigPermStore struct {
	store.ConfigPermissionStore
	allow   bool
	calls   int
	lastUID string
}

func (s *stubConfigPermStore) CheckPermission(_ context.Context, _ uuid.UUID, _, _, userID string) (bool, error) {
	s.calls++
	s.lastUID = userID
	return s.allow, nil
}

// bridgeGroupRequest describes a Telegram group bridge request; optional
// headers are set only when their field is non-empty.
type bridgeGroupRequest struct {
	agentID, tenantID uuid.UUID
	sessionKey        string
	sender, role      string
	delegationID      string
	delegationInputs  string
}

const (
	bridgeGroupUser = "group:telegram:-100123"
	bridgeGroupChat = "-100123"
)

func newBridgeGroupRequest() bridgeGroupRequest {
	return bridgeGroupRequest{agentID: uuid.New(), tenantID: uuid.New(), sessionKey: "agent:main:telegram:group:-100123"}
}

func (b bridgeGroupRequest) sign(extras ...string) string {
	return providers.SignBridgeContext(bridgeTestToken, b.agentID.String(), bridgeGroupUser, "telegram",
		bridgeGroupChat, "group", "", b.tenantID.String(), extras...)
}

func (b bridgeGroupRequest) identitySig() string {
	return b.sign(providers.BridgeIdentityExtras("", b.sessionKey, b.delegationID, b.delegationInputs, b.sender, b.role)...)
}

func (b bridgeGroupRequest) build(sig string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp/bridge", nil)
	req.Header.Set("X-Agent-ID", b.agentID.String())
	req.Header.Set("X-User-ID", bridgeGroupUser)
	req.Header.Set("X-Channel", "telegram")
	req.Header.Set("X-Chat-ID", bridgeGroupChat)
	req.Header.Set("X-Peer-Kind", "group")
	req.Header.Set("X-Tenant-ID", b.tenantID.String())
	req.Header.Set("X-Session-Key", b.sessionKey)
	if b.sender != "" {
		req.Header.Set("X-Sender-ID", b.sender)
	}
	if b.role != "" {
		req.Header.Set("X-Role", b.role)
	}
	if b.delegationID != "" {
		req.Header.Set("X-Delegation-ID", b.delegationID)
	}
	if b.delegationInputs != "" {
		req.Header.Set("X-Delegation-Inputs", b.delegationInputs)
	}
	req.Header.Set("X-Bridge-Sig", sig)
	return req
}

type bridgeResult struct {
	code           int
	called         bool
	sender, role   string
	cronErr, fwErr error
}

func serveBridge(t *testing.T, perms store.ConfigPermissionStore, req *http.Request) bridgeResult {
	t.Helper()
	var res bridgeResult
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		res.called = true
		res.sender = store.SenderIDFromContext(ctx)
		res.role = store.RoleFromContext(ctx)
		res.cronErr = store.CheckCronPermission(ctx, perms)
		res.fwErr = store.CheckFileWriterPermission(ctx, perms)
	})
	rec := httptest.NewRecorder()
	bridgeContextMiddleware(bridgeTestToken, nil, next).ServeHTTP(rec, req)
	res.code = rec.Code
	return res
}

func TestBridgeContextMiddleware_SignedSenderReachesGroupPermissionChecks(t *testing.T) {
	b := newBridgeGroupRequest()
	b.sender = "424242|alice"

	granted := &stubConfigPermStore{allow: true}
	res := serveBridge(t, granted, b.build(b.identitySig()))
	if res.code != http.StatusOK || !res.called {
		t.Fatalf("status=%d called=%v", res.code, res.called)
	}
	if res.sender != b.sender {
		t.Fatalf("SenderIDFromContext = %q, want %q", res.sender, b.sender)
	}
	if res.cronErr != nil || res.fwErr != nil {
		t.Fatalf("granted sender denied: cron=%v file_writer=%v", res.cronErr, res.fwErr)
	}
	if granted.calls == 0 || granted.lastUID != "424242" {
		t.Fatalf("grant store calls=%d lastUID=%q, want lookups for 424242", granted.calls, granted.lastUID)
	}

	denied := &stubConfigPermStore{allow: false}
	res = serveBridge(t, denied, b.build(b.identitySig()))
	for name, err := range map[string]error{"cron": res.cronErr, "file_writer": res.fwErr} {
		if err == nil {
			t.Fatalf("%s: ungranted sender allowed", name)
		}
		if strings.Contains(err.Error(), "system context") {
			t.Fatalf("%s: got system-context denial, want per-user denial: %v", name, err)
		}
	}
}

func TestBridgeContextMiddleware_SignedAdminRoleBypassesGroupGrants(t *testing.T) {
	b := newBridgeGroupRequest()
	b.role = "admin"

	perms := &stubConfigPermStore{allow: false}
	res := serveBridge(t, perms, b.build(b.identitySig()))
	if res.code != http.StatusOK || res.role != "admin" {
		t.Fatalf("status=%d role=%q", res.code, res.role)
	}
	if res.cronErr != nil || res.fwErr != nil || perms.calls != 0 {
		t.Fatalf("admin role not honored: cron=%v file_writer=%v calls=%d", res.cronErr, res.fwErr, perms.calls)
	}
}

func TestBridgeContextMiddleware_RejectsTamperedIdentity(t *testing.T) {
	b := newBridgeGroupRequest()
	b.sender = "424242|alice"
	b.role = "member"
	sig := b.identitySig()

	for name, mutate := range map[string]func(*bridgeGroupRequest){
		"sender": func(r *bridgeGroupRequest) { r.sender = "1|mallory" },
		"role":   func(r *bridgeGroupRequest) { r.role = "owner" },
	} {
		tampered := b
		mutate(&tampered)
		res := serveBridge(t, nil, tampered.build(sig))
		if res.code != http.StatusForbidden || res.called {
			t.Fatalf("%s tampered: status=%d called=%v, want forbidden", name, res.code, res.called)
		}
	}
}

func TestBridgeContextMiddleware_RejectsLegacySignatureWithIdentityHeaders(t *testing.T) {
	b := newBridgeGroupRequest()
	for name, legacySig := range map[string]string{
		"localKey+sessionKey": b.sign("", b.sessionKey),
		"no extras":           b.sign(),
	} {
		for _, hdr := range []struct{ sender, role string }{{"424242|alice", ""}, {"", "admin"}} {
			r := b
			r.sender, r.role = hdr.sender, hdr.role
			res := serveBridge(t, nil, r.build(legacySig))
			if res.code != http.StatusForbidden || res.called {
				t.Fatalf("%s with sender=%q role=%q: status=%d called=%v, want forbidden",
					name, hdr.sender, hdr.role, res.code, res.called)
			}
		}
	}
}

func TestBridgeContextMiddleware_LegacySignatureWithoutIdentityStillAccepted(t *testing.T) {
	b := newBridgeGroupRequest()
	res := serveBridge(t, nil, b.build(b.sign("", b.sessionKey)))
	if res.code != http.StatusOK || !res.called {
		t.Fatalf("status=%d called=%v", res.code, res.called)
	}
	if res.sender != "" || res.role != "" {
		t.Fatalf("legacy request injected sender=%q role=%q", res.sender, res.role)
	}
}

func TestBridgeContextMiddleware_RejectsIdentitySignatureReplayedAsDelegation(t *testing.T) {
	b := newBridgeGroupRequest()
	b.sender = "424242|alice"
	b.role = "admin"
	sig := b.identitySig()

	replay := b
	replay.sender, replay.role = "", ""
	replay.delegationID, replay.delegationInputs = b.sender, b.role
	res := serveBridge(t, nil, replay.build(sig))
	if res.code != http.StatusForbidden || res.called {
		t.Fatalf("status=%d called=%v, want forbidden", res.code, res.called)
	}
}

func TestBridgeContextMiddleware_InjectsSenderWithDelegation(t *testing.T) {
	b := newBridgeGroupRequest()
	b.sender = "424242|alice"
	b.delegationID = "delegation-123"
	b.delegationInputs = "/runtime/inputs"

	var gotDelegation string
	var res bridgeResult
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		res.called = true
		res.sender = store.SenderIDFromContext(r.Context())
		gotDelegation = tools.DelegationIDFromCtx(r.Context())
	})
	rec := httptest.NewRecorder()
	bridgeContextMiddleware(bridgeTestToken, nil, next).ServeHTTP(rec, b.build(b.identitySig()))
	if rec.Code != http.StatusOK || !res.called || res.sender != b.sender || gotDelegation != b.delegationID {
		t.Fatalf("status=%d called=%v sender=%q delegation=%q", rec.Code, res.called, res.sender, gotDelegation)
	}
}

func TestBridgeContextMiddleware_NoGatewayTokenIgnoresIdentity(t *testing.T) {
	b := newBridgeGroupRequest()
	b.sender = "424242|alice"
	b.role = "owner"

	var sender, role string
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sender = store.SenderIDFromContext(r.Context())
		role = store.RoleFromContext(r.Context())
	})
	bridgeContextMiddleware("", nil, next).ServeHTTP(httptest.NewRecorder(), b.build(""))
	if sender != "" || role != "" {
		t.Fatalf("unsigned identity injected: sender=%q role=%q", sender, role)
	}
}
