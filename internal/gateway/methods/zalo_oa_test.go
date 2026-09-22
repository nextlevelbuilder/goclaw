package methods

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	zalooa "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// zaloOAStubStore is a ChannelInstanceStore stub with in-memory instances.
type zaloOAStubStore struct {
	mu        sync.Mutex
	instances map[uuid.UUID]store.ChannelInstanceData
}

func newZaloOAStubStore() *zaloOAStubStore {
	return &zaloOAStubStore{instances: make(map[uuid.UUID]store.ChannelInstanceData)}
}

func (s *zaloOAStubStore) Create(_ context.Context, inst *store.ChannelInstanceData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[inst.ID] = *inst
	return nil
}

func (s *zaloOAStubStore) Get(_ context.Context, id uuid.UUID) (*store.ChannelInstanceData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &inst, nil
}

func (s *zaloOAStubStore) GetByName(_ context.Context, _ string) (*store.ChannelInstanceData, error) {
	return nil, errors.New("unused")
}

func (s *zaloOAStubStore) Update(_ context.Context, id uuid.UUID, updates map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.instances[id]
	if !ok {
		return errors.New("not found")
	}
	if creds, ok := updates["credentials"]; ok {
		switch v := creds.(type) {
		case []byte:
			inst.Credentials = v
		case string:
			inst.Credentials = []byte(v)
		case json.RawMessage:
			inst.Credentials = v
		default:
			if b, err := json.Marshal(v); err == nil {
				inst.Credentials = b
			}
		}
	}
	s.instances[id] = inst
	return nil
}

func (s *zaloOAStubStore) MergeConfig(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *zaloOAStubStore) ListAll(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *zaloOAStubStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *zaloOAStubStore) ListEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *zaloOAStubStore) ListAllInstances(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *zaloOAStubStore) ListAllEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *zaloOAStubStore) ListPaged(_ context.Context, _ store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *zaloOAStubStore) CountInstances(_ context.Context, _ store.ChannelInstanceListOpts) (int, error) {
	return 0, nil
}

var _ store.ChannelInstanceStore = (*zaloOAStubStore)(nil)

func seedZaloOAInstance(t *testing.T, s *zaloOAStubStore, tenant uuid.UUID) (uuid.UUID, *zalooa.ChannelCreds) {
	t.Helper()
	id := uuid.New()
	creds := &zalooa.ChannelCreds{
		AppID:       "app-1",
		SecretKey:   "sec-1",
		RedirectURI: "https://gw.example.com/zalo-callback",
	}
	blob, err := creds.Marshal()
	if err != nil {
		t.Fatalf("marshal creds: %v", err)
	}
	if err := s.Create(context.Background(), &store.ChannelInstanceData{
		BaseModel:   store.BaseModel{ID: id},
		TenantID:    tenant,
		Name:        "zalo-oa-1",
		ChannelType: channels.TypeZaloOA,
		Credentials: blob,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return id, creds
}

func zaloOAReq(method string, params any) *protocol.RequestFrame {
	var raw json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		raw = b
	}
	return &protocol.RequestFrame{Type: protocol.FrameTypeRequest, ID: "r1", Method: method, Params: raw}
}

func readZaloOAResponse(t *testing.T, ch <-chan []byte) *protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-ch:
		var resp protocol.ResponseFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return &resp
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not respond")
		return nil
	}
}

// TestZaloOA_ConsentURL_Success builds a consent URL with a state token.
func TestZaloOA_ConsentURL_Success(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id, creds := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleConsentURL(store.WithTenantID(context.Background(), tenant), client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": id.String()}))

	resp := readZaloOAResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Payload)
	}
	url, _ := result["url"].(string)
	state, _ := result["state"].(string)
	if url == "" || state == "" {
		t.Fatalf("missing url/state: %+v", result)
	}
	if !strings.Contains(state, ".") {
		t.Fatal("state must be HKDF-signed, not a raw nonce")
	}
	okConsume, err := m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !okConsume {
		t.Error("state token not registered")
	}
}

// TestZaloOA_ConsentURL_Validation covers error branches: missing app_id,
// missing redirect_uri, wrong channel type, cross-tenant.
func TestZaloOA_ConsentURL_Validation(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	st := newZaloOAStubStore()
	id, _ := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)
	ctx := store.WithTenantID(context.Background(), tenant)

	// Cross-tenant access.
	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, other, "u1", 4)
	m.handleConsentURL(store.WithTenantID(context.Background(), other), client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": id.String()}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("cross-tenant must fail")
	}

	// Wrong channel type.
	badID := uuid.New()
	if err := st.Create(ctx, &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: badID}, TenantID: tenant, Name: "x", ChannelType: "telegram"}); err != nil {
		t.Fatalf("seed bad: %v", err)
	}
	client, ch = gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleConsentURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": badID.String()}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("non-zalo_oa type must fail")
	}

	// Missing app_id.
	noAppID := uuid.New()
	if err := st.Create(ctx, &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: noAppID}, TenantID: tenant, Name: "y", ChannelType: channels.TypeZaloOA}); err != nil {
		t.Fatalf("seed no-app: %v", err)
	}
	client, ch = gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleConsentURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": noAppID.String()}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("missing app_id must fail")
	}

	// Missing redirect_uri.
	noRedirect := uuid.New()
	if err := st.Create(ctx, &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: noRedirect}, TenantID: tenant, Name: "z", ChannelType: channels.TypeZaloOA,
		Credentials: []byte(`{"app_id":"a","secret_key":"s"}`),
	}); err != nil {
		t.Fatalf("seed no-redirect: %v", err)
	}
	client, ch = gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleConsentURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": noRedirect.String()}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("missing redirect_uri must fail")
	}
}

// TestZaloOA_ExchangeCode_Success runs the full exchange against a local
// token-endpoint fixture and asserts credential persistence.
func TestZaloOA_ExchangeCode_Success(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id, creds := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)
	ctx := store.WithTenantID(context.Background(), tenant)

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("secret_key") != "sec-1" {
			t.Errorf("secret_key header = %q", r.Header.Get("secret_key"))
		}
		w.Write([]byte(`{"access_token":"at-1","refresh_token":"rt-1","expires_in":"3600"}`))
	}))
	defer tokenSrv.Close()

	orig := newOAClient
	newOAClient = func() *zalooa.Client {
		c := zalooa.NewClient(5 * time.Second)
		c.SetOAuthBaseForTest(tokenSrv.URL + "/v4")
		return c
	}
	defer func() { newOAClient = orig }()

	state, err := m.newConsentState(id, tenant, creds.SecretKey)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleExchangeCode(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAExchangeCode,
		map[string]any{"instance_id": id.String(), "code": "c1", "state": state, "oa_id": "12345"}))

	resp := readZaloOAResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("exchange error: %+v", resp.Error)
	}
	result, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Payload)
	}
	if result["ok"] != true || result["oa_id"] != "12345" {
		t.Fatalf("unexpected result: %+v", result)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	loaded, err := zalooa.LoadCreds(got.Credentials)
	if err != nil {
		t.Fatalf("load creds: %v", err)
	}
	if loaded.AccessToken != "at-1" || loaded.RefreshToken != "rt-1" || loaded.OAID != "12345" {
		t.Errorf("creds = %+v", loaded)
	}
}

// TestZaloOA_ExchangeCode_StateValidation: replaying a consumed state fails.
func TestZaloOA_ExchangeCode_StateValidation(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id, creds := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)
	ctx := store.WithTenantID(context.Background(), tenant)

	state, err := m.newConsentState(id, tenant, creds.SecretKey)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	ok, err := m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Fatal("first consume must succeed")
	}
	ok, err = m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	if ok {
		t.Fatal("replay must fail")
	}

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleExchangeCode(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAExchangeCode,
		map[string]any{"instance_id": id.String(), "code": "c1", "state": state}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("consumed state must fail")
	}
}

// TestZaloOA_StateCaps: per-instance pending states capped at 5.
func TestZaloOA_StateCaps(t *testing.T) {
	m := NewZaloOAMethods(nil, nil)
	id := uuid.New()
	for i := range 10 {
		m.putState(id, string(rune('a'+i)))
	}
	m.stateMu.Lock()
	count := 0
	for _, v := range m.states {
		if v.instID == id {
			count++
		}
	}
	m.stateMu.Unlock()
	if count > 5 {
		t.Errorf("states per instance = %d, want <= 5", count)
	}
}

func TestDeriveZaloOAStateHMACKey_DiffersFromRawSecret(t *testing.T) {
	secret := "oa-secret-key"
	idA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	idB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	keyA, err := deriveZaloOAStateHMACKey(secret, idA)
	if err != nil {
		t.Fatalf("derive A: %v", err)
	}
	keyB, err := deriveZaloOAStateHMACKey(secret, idB)
	if err != nil {
		t.Fatalf("derive B: %v", err)
	}
	if len(keyA) != 32 || len(keyB) != 32 {
		t.Fatalf("key lengths = %d, %d; want 32", len(keyA), len(keyB))
	}
	if string(keyA) == string(keyB) {
		t.Fatal("same secret + different instance IDs must produce different keys")
	}
	if hmac.Equal(keyA, []byte(secret)) {
		t.Fatal("derived key must not equal the raw secret")
	}
	if _, err := deriveZaloOAStateHMACKey("", idA); err == nil {
		t.Fatal("empty secret must error")
	}
}

func TestZaloOA_HKDFState_BindsTenantAndInstance(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	st := newZaloOAStubStore()
	id, creds := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)

	result, flowErr := m.ConsentURL(context.Background(), tenant, id.String())
	if flowErr != nil {
		t.Fatalf("consent: %v", flowErr)
	}
	hkdfKey, err := deriveZaloOAStateHMACKey(creds.SecretKey, id)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if _, err := decodeZaloOAState(result.State, hkdfKey); err != nil {
		t.Fatalf("fresh state must verify with HKDF key: %v", err)
	}
	if _, err := decodeZaloOAState(result.State, []byte(creds.SecretKey)); err == nil {
		t.Fatal("HKDF-signed state must not verify with the raw secret alone")
	}

	tampered := result.State[:len(result.State)-1] + "0"
	if tampered == result.State {
		tampered = result.State[:len(result.State)-1] + "1"
	}
	ok, err := m.consumeConsentState(id, tenant, creds.SecretKey, tampered)
	if err != nil {
		t.Fatalf("consume tampered: %v", err)
	}
	if ok {
		t.Fatal("tampered state must fail")
	}

	ok, err = m.consumeConsentState(id, other, creds.SecretKey, result.State)
	if err != nil {
		t.Fatalf("consume other tenant: %v", err)
	}
	if ok {
		t.Fatal("cross-tenant signed state must fail")
	}

	ok, err = m.consumeConsentState(id, tenant, creds.SecretKey, result.State)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok {
		t.Fatal("valid state must consume once")
	}
	ok, err = m.consumeConsentState(id, tenant, creds.SecretKey, result.State)
	if err != nil {
		t.Fatalf("replay consume: %v", err)
	}
	if ok {
		t.Fatal("replay must fail")
	}
}

// TestZaloOA_ExchangeCode_ErrorPaths covers missing code, cross-tenant, and
// wrong-channel-type without burning the state token (consumeState moved
// after validation).
func TestZaloOA_ExchangeCode_ErrorPaths(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	other := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	st := newZaloOAStubStore()
	id, creds := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)
	ctx := store.WithTenantID(context.Background(), tenant)

	state, err := m.newConsentState(id, tenant, creds.SecretKey)
	if err != nil {
		t.Fatalf("state: %v", err)
	}

	// Missing code → error, and state NOT consumed.
	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleExchangeCode(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAExchangeCode,
		map[string]any{"instance_id": id.String(), "state": state}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("missing code must fail")
	}
	ok, err := m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("consume after missing code: %v", err)
	}
	if !ok {
		t.Fatal("state must survive the missing-code rejection")
	}
	state, err = m.newConsentState(id, tenant, creds.SecretKey)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}

	// Cross-tenant → error, state NOT consumed.
	client, ch = gateway.NewCapturingTestClient(permissions.RoleOperator, other, "u1", 4)
	m.handleExchangeCode(store.WithTenantID(context.Background(), other), client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAExchangeCode,
		map[string]any{"instance_id": id.String(), "code": "c1", "state": state}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("cross-tenant must fail")
	}
	ok, err = m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("consume after cross-tenant: %v", err)
	}
	if !ok {
		t.Fatal("state must survive the cross-tenant rejection")
	}
	state, err = m.newConsentState(id, tenant, creds.SecretKey)
	if err != nil {
		t.Fatalf("re-mint: %v", err)
	}

	// Wrong channel type → error.
	badID := uuid.New()
	if err := st.Create(ctx, &store.ChannelInstanceData{
		BaseModel: store.BaseModel{ID: badID}, TenantID: tenant, Name: "x", ChannelType: "telegram",
	}); err != nil {
		t.Fatalf("seed bad: %v", err)
	}
	client, ch = gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleExchangeCode(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAExchangeCode,
		map[string]any{"instance_id": badID.String(), "code": "c1", "state": state}))
	if resp := readZaloOAResponse(t, ch); resp.Error == nil {
		t.Error("wrong channel type must fail")
	}

	ok, err = m.consumeConsentState(id, tenant, creds.SecretKey, state)
	if err != nil {
		t.Fatalf("consume after wrong type: %v", err)
	}
	if !ok {
		t.Error("state consumed by a different instance's rejection")
	}
}

// TestZaloOA_CallbackURL_StoredWins verifies the stored redirect_uri takes
// precedence over the auto-generated one.
func TestZaloOA_CallbackURL_StoredWins(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id, _ := seedZaloOAInstance(t, st, tenant)
	m := NewZaloOAMethods(st, nil)
	m.SetPublicURL(func() string { return "https://gw.example.com" })
	ctx := store.WithTenantID(context.Background(), tenant)

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleCallbackURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOACallbackURL, map[string]any{"instance_id": id.String()}))
	resp := readZaloOAResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Payload)
	}
	if result["url"] != "https://gw.example.com/zalo-callback" {
		t.Errorf("url = %v, want stored redirect_uri", result["url"])
	}
}

// TestZaloOA_CallbackURL_GeneratedFallback verifies auto-generation from the
// public URL when no redirect_uri is stored.
func TestZaloOA_CallbackURL_GeneratedFallback(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id := uuid.New()
	if err := st.Create(context.Background(), &store.ChannelInstanceData{
		BaseModel:   store.BaseModel{ID: id},
		TenantID:    tenant,
		Name:        "zalo-oa-gen",
		ChannelType: channels.TypeZaloOA,
		Credentials: []byte(`{"app_id":"a","secret_key":"s"}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := NewZaloOAMethods(st, nil)
	m.SetPublicURL(func() string { return "https://agents.fronta.work/" })
	ctx := store.WithTenantID(context.Background(), tenant)

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleCallbackURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOACallbackURL, map[string]any{"instance_id": id.String()}))
	resp := readZaloOAResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result := resp.Payload.(map[string]any)
	if result["url"] != "https://agents.fronta.work/oauth/zalo/callback" {
		t.Errorf("url = %v, want generated callback", result["url"])
	}
}

// TestZaloOA_CallbackURL_NoPublicURL returns UNAVAILABLE when the snapshot
// is empty and no redirect_uri is stored.
func TestZaloOA_CallbackURL_NoPublicURL(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id := uuid.New()
	if err := st.Create(context.Background(), &store.ChannelInstanceData{
		BaseModel:   store.BaseModel{ID: id},
		TenantID:    tenant,
		Name:        "zalo-oa-nopub",
		ChannelType: channels.TypeZaloOA,
		Credentials: []byte(`{"app_id":"a","secret_key":"s"}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := NewZaloOAMethods(st, nil)
	m.SetPublicURL(func() string { return "" })
	ctx := store.WithTenantID(context.Background(), tenant)

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleCallbackURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOACallbackURL, map[string]any{"instance_id": id.String()}))
	resp := readZaloOAResponse(t, ch)
	if resp.Error == nil {
		t.Fatal("expected UNAVAILABLE when public URL unknown")
	}
}

// TestZaloOA_ConsentURL_GeneratedRedirect proves consent succeeds without a
// stored redirect_uri by falling back to the generated callback.
func TestZaloOA_ConsentURL_GeneratedRedirect(t *testing.T) {
	tenant := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	st := newZaloOAStubStore()
	id := uuid.New()
	if err := st.Create(context.Background(), &store.ChannelInstanceData{
		BaseModel:   store.BaseModel{ID: id},
		TenantID:    tenant,
		Name:        "zalo-oa-consent-gen",
		ChannelType: channels.TypeZaloOA,
		Credentials: []byte(`{"app_id":"a","secret_key":"s"}`),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := NewZaloOAMethods(st, nil)
	m.SetPublicURL(func() string { return "https://gw.example.com" })
	ctx := store.WithTenantID(context.Background(), tenant)

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tenant, "u1", 4)
	m.handleConsentURL(ctx, client, zaloOAReq(
		protocol.MethodChannelInstancesZaloOAConsentURL, map[string]any{"instance_id": id.String()}))
	resp := readZaloOAResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("consent must succeed with generated redirect: %+v", resp.Error)
	}
	result := resp.Payload.(map[string]any)
	url, _ := result["url"].(string)
	if !strings.Contains(url, "redirect_uri=https%3A%2F%2Fgw.example.com%2Foauth%2Fzalo%2Fcallback") {
		t.Errorf("consent url missing generated redirect: %s", url)
	}
}
