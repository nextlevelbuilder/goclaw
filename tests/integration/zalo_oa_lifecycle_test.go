//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	zalooa "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestZaloOALifecycle exercises the full feature against a real PG
// (store-layer encryption + tenant scope) and a mocked Zalo API.
// Skips automatically if TEST_DATABASE_URL is unset / unreachable.
//
//   1. Create channel_instance row (creds plaintext, store layer encrypts)
//   2. Read back via Get → LoadCreds → tokens absent (just app_id/secret)
//   3. Mock /v4/oa/access_token and call ExchangeCode through Persist
//   4. Re-read row → tokens decrypted + present
//   5. Build Channel via factory, Start
//   6. Send text → mock /v3.0/oa/message/cs receives expected body
//   7. Force-refresh + Send again → mock refresh hit + send hit
//   8. Force ErrAuthExpired on refresh → health flips Failed/Auth
//   9. Stop channel cleanly within bounded time
func TestZaloOALifecycle(t *testing.T) {
	db := testDB(t)

	tenantID, agentID := seedTenantAgent(t, db)
	ciStore := pg.NewPGChannelInstanceStore(db, testEncryptionKey)

	mock := newMockZaloServer(t)

	ctx := store.WithTenantID(context.Background(), tenantID)

	// ── 1. Create instance with plaintext creds JSON ──────────────────
	credsJSON, err := json.Marshal(map[string]any{
		"app_id":     "app-int",
		"secret_key": "sec-int",
	})
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	cfgJSON, err := json.Marshal(map[string]any{
		"poll_interval_seconds": 60,
		"media_max_mb":          5,
	})
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	inst := &store.ChannelInstanceData{
		TenantID:    tenantID,
		Name:        fmt.Sprintf("zalo-oauth-int-%d", time.Now().UnixNano()),
		DisplayName: "Zalo OAuth Integration",
		ChannelType: channels.TypeZaloOA,
		AgentID:     agentID,
		Credentials: credsJSON,
		Config:      cfgJSON,
		Enabled:     true,
		CreatedBy:   "test",
	}
	if err := ciStore.Create(ctx, inst); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = ciStore.Delete(ctx, inst.ID) })

	// ── 2. Read back; verify store decrypts blob round-trip ───────────
	got, err := ciStore.Get(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	creds, err := zalooa.LoadCreds(got.Credentials)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	if creds.AppID != "app-int" || creds.SecretKey != "sec-int" {
		t.Errorf("creds round-trip lost data: %+v", creds)
	}
	if creds.AccessToken != "" {
		t.Errorf("AccessToken should be empty pre-exchange, got %q", creds.AccessToken)
	}

	// ── 3+4. Simulate an exchange via direct creds.Persist + mock refresh
	// (We bypass the WS handler here — phase-01 unit tests cover its glue.)
	creds.AccessToken = "AT-initial"
	creds.RefreshToken = "RT-initial"
	creds.ExpiresAt = time.Now().Add(time.Hour)
	creds.OAID = "oa-int-1"
	if err := zalooa.Persist(ctx, ciStore, inst.ID, creds); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	// Read back again — verify Update wrote and Get decrypted.
	got2, _ := ciStore.Get(ctx, inst.ID)
	creds2, _ := zalooa.LoadCreds(got2.Credentials)
	if creds2.AccessToken != "AT-initial" || creds2.OAID != "oa-int-1" {
		t.Errorf("post-Persist round-trip mismatch: %+v", creds2)
	}

	// ── 5. Build Channel via factory, wire mock host, Start ───────────
	msgBus := bus.New()
	factory := zalooa.Factory(ciStore)
	ch, err := factory(inst.Name, got2.Credentials, got2.Config, msgBus, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	zch, ok := ch.(*zalooa.Channel)
	if !ok {
		t.Fatalf("factory returned %T, want *zalooa.Channel", ch)
	}
	zch.SetType(channels.TypeZaloOA)
	zch.SetTenantID(tenantID)
	zch.SetAgentID(agentID.String())
	zch.SetInstanceID(inst.ID)

	if err := zch.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() {
		stopDone := make(chan struct{})
		go func() { _ = zch.Stop(context.Background()); close(stopDone) }()
		select {
		case <-stopDone:
		case <-time.After(5 * time.Second):
			t.Errorf("Stop did not return within 5s")
		}
	}()

	// ── 6. Send text — assert mock receives it ────────────────────────
	mock.Override(zch)
	if _, err := zch.SendText(ctx, "user-1", "integration-hello", ""); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if got := mock.SendCount(); got != 1 {
		t.Errorf("send count = %d, want 1", got)
	}

	// ── 7. Force refresh + send — assert refresh hit + new token used ──
	mock.QueueRefreshOK("AT-rotated", "RT-rotated")
	zch.ForceRefreshForTest()
	if _, err := zch.SendText(ctx, "user-1", "post-refresh", ""); err != nil {
		t.Fatalf("SendText post-refresh: %v", err)
	}
	if got := mock.RefreshCount(); got != 1 {
		t.Errorf("refresh count = %d, want 1", got)
	}
	if mock.LastSendToken() != "AT-rotated" {
		t.Errorf("send used token %q, want AT-rotated", mock.LastSendToken())
	}

	// ── 8. Auth-expired refresh → health flips Failed/Auth ────────────
	mock.QueueRefreshAuthExpired()
	zch.ForceRefreshForTest()
	_, err = zch.SendText(ctx, "user-1", "this should fail", "")
	if err == nil {
		t.Error("expected SendText to fail after auth-expired refresh")
	}
	// Allow the safety ticker / send path to mark health.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := zch.HealthSnapshot()
		if snap.State == channels.ChannelHealthStateFailed && snap.FailureKind == channels.ChannelFailureKindAuth {
			return // pass
		}
		time.Sleep(50 * time.Millisecond)
	}
	snap := zch.HealthSnapshot()
	t.Errorf("health did not transition to Failed/Auth: state=%v kind=%v", snap.State, snap.FailureKind)
}

// ─── Mock Zalo API ──────────────────────────────────────────────────────

type mockZaloServer struct {
	t            *testing.T
	srv          *httptest.Server
	sendCount    atomic.Int32
	refreshCount atomic.Int32

	mu             sync.Mutex
	lastSendToken  string
	refreshAccess  string
	refreshRefresh string
	refreshError   string // if non-empty, return as APIError envelope (HTTP 200)
}

func newMockZaloServer(t *testing.T) *mockZaloServer {
	t.Helper()
	m := &mockZaloServer{t: t}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.srv.Close)
	return m
}

// Override points the channel's HTTP client at the mock for both the OAuth
// host and the API host. Uses test-only setters added on the Channel.
func (m *mockZaloServer) Override(ch *zalooa.Channel) {
	// OAuth base carries /v4 in production (defaultOAuthBase); mirror it here
	// so refresh URLs end at /v4/oa/access_token like the real upstream.
	ch.SetTestEndpointsForTest(m.srv.URL+"/v4", m.srv.URL)
}

func (m *mockZaloServer) QueueRefreshOK(access, refresh string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshAccess = access
	m.refreshRefresh = refresh
	m.refreshError = ""
}

func (m *mockZaloServer) QueueRefreshAuthExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshError = `{"error":-118,"message":"invalid_grant"}`
	m.refreshAccess = ""
	m.refreshRefresh = ""
}

func (m *mockZaloServer) SendCount() int    { return int(m.sendCount.Load()) }
func (m *mockZaloServer) RefreshCount() int { return int(m.refreshCount.Load()) }
func (m *mockZaloServer) LastSendToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSendToken
}

func (m *mockZaloServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/v4/oa/access_token"):
		m.refreshCount.Add(1)
		m.mu.Lock()
		errBody, accTok, refTok := m.refreshError, m.refreshAccess, m.refreshRefresh
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if errBody != "" {
			_, _ = w.Write([]byte(errBody))
			return
		}
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"access_token":%q,"refresh_token":%q,"expires_in":3600}`, accTok, refTok)))
	case r.URL.Path == "/v3.0/oa/message/cs":
		m.sendCount.Add(1)
		m.mu.Lock()
		// Production sends access_token in the HTTP header, not query
		// (per d580d490 — Zalo's query-token form returned a generic 404).
		m.lastSendToken = r.Header.Get("access_token")
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"data":{"message_id":"int-mid"}}`))
	case strings.HasPrefix(r.URL.Path, "/v2.0/oa/listrecentchat"):
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":0,"data":[]}`)) // no inbound traffic this test
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

