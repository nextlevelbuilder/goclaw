package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Stub stores
// ---------------------------------------------------------------------------

type stubBitrixPortalStore struct {
	mu        sync.Mutex
	rows      map[string]*store.BitrixPortalData // key: tenantID:name
	createErr error
}

func newStubBitrixPortalStore() *stubBitrixPortalStore {
	return &stubBitrixPortalStore{rows: map[string]*store.BitrixPortalData{}}
}

func (s *stubBitrixPortalStore) key(tid uuid.UUID, name string) string {
	return tid.String() + ":" + name
}

func (s *stubBitrixPortalStore) Create(_ context.Context, p *store.BitrixPortalData) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(p.TenantID, p.Name)
	if _, exists := s.rows[k]; exists {
		return errors.New("duplicate key violates unique constraint")
	}
	if p.ID == uuid.Nil {
		p.ID = store.GenNewID()
	}
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	cp := *p
	s.rows[k] = &cp
	return nil
}

func (s *stubBitrixPortalStore) GetByName(_ context.Context, tid uuid.UUID, name string) (*store.BitrixPortalData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[s.key(tid, name)]
	if !ok {
		return nil, errors.New("not found")
	}
	cp := *row
	return &cp, nil
}

func (s *stubBitrixPortalStore) ListByTenant(_ context.Context, tid uuid.UUID) ([]store.BitrixPortalData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := tid.String() + ":"
	out := make([]store.BitrixPortalData, 0)
	for k, row := range s.rows {
		if strings.HasPrefix(k, prefix) {
			out = append(out, *row)
		}
	}
	return out, nil
}

func (s *stubBitrixPortalStore) ListAllForLoader(_ context.Context) ([]store.BitrixPortalData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.BitrixPortalData, 0, len(s.rows))
	for _, row := range s.rows {
		out = append(out, *row)
	}
	return out, nil
}

func (s *stubBitrixPortalStore) UpdateCredentials(_ context.Context, _ uuid.UUID, _ string, _ []byte) error {
	return nil
}

func (s *stubBitrixPortalStore) UpdateState(_ context.Context, _ uuid.UUID, _ string, _ []byte) error {
	return nil
}

func (s *stubBitrixPortalStore) Delete(_ context.Context, tid uuid.UUID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := s.key(tid, name)
	if _, ok := s.rows[k]; !ok {
		return errors.New("not found")
	}
	delete(s.rows, k)
	return nil
}

// stubChannelInstanceStore implements just the methods bitrix_portals needs.
type stubChannelInstanceStore struct {
	mu        sync.Mutex
	instances []store.ChannelInstanceData
}

func newStubChannelInstanceStore() *stubChannelInstanceStore {
	return &stubChannelInstanceStore{}
}

func (s *stubChannelInstanceStore) Create(_ context.Context, _ *store.ChannelInstanceData) error {
	return nil
}
func (s *stubChannelInstanceStore) Get(_ context.Context, _ uuid.UUID) (*store.ChannelInstanceData, error) {
	return nil, errors.New("unused")
}
func (s *stubChannelInstanceStore) GetByName(_ context.Context, _ string) (*store.ChannelInstanceData, error) {
	return nil, errors.New("unused")
}
func (s *stubChannelInstanceStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error {
	return nil
}
func (s *stubChannelInstanceStore) Delete(_ context.Context, _ uuid.UUID) error { return nil }
func (s *stubChannelInstanceStore) ListEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) ListAll(_ context.Context) ([]store.ChannelInstanceData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]store.ChannelInstanceData, len(s.instances))
	copy(out, s.instances)
	return out, nil
}
func (s *stubChannelInstanceStore) ListAllInstances(_ context.Context) ([]store.ChannelInstanceData, error) {
	return s.ListAll(context.Background())
}
func (s *stubChannelInstanceStore) ListAllEnabled(_ context.Context) ([]store.ChannelInstanceData, error) {
	return s.ListAll(context.Background())
}
func (s *stubChannelInstanceStore) ListPaged(_ context.Context, _ store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (s *stubChannelInstanceStore) CountInstances(_ context.Context, _ store.ChannelInstanceListOpts) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

// readResponse pulls and parses the single response frame the handler is
// expected to produce. Fails the test if no frame arrives within a short
// timeout — handlers must always respond.
func readResponse(t *testing.T, ch <-chan []byte) *protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-ch:
		var resp protocol.ResponseFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		return &resp
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout: handler did not send response")
		return nil
	}
}

func buildBitrixReq(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
		raw = b
	}
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "req-1",
		Method: method,
		Params: raw,
	}
}

// gatewayURLFn returns a closure that yields a fixed URL — emulates the
// snapshot middleware having observed a request.
func gatewayURLFn(url string) func() string {
	return func() string { return url }
}

// ---------------------------------------------------------------------------
// handleList
// ---------------------------------------------------------------------------

func TestBitrixPortals_List_TenantIsolation(t *testing.T) {
	tidA := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tidB := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	pStore := newStubBitrixPortalStore()
	// Seed one portal per tenant.
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tidA, Name: "alpha", Domain: "alpha.bitrix24.com",
	})
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tidB, Name: "beta", Domain: "beta.bitrix24.com",
	})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))

	// Tenant A list should NOT see tenant B's portal.
	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tidA, "user-A", 4)
	ctx := store.WithTenantID(context.Background(), tidA)
	m.handleList(ctx, client, buildBitrixReq(t, protocol.MethodBitrixPortalsList, nil))

	resp := readResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("result not map: %T", resp.Payload)
	}
	portals, ok := result["portals"].([]any)
	if !ok {
		t.Fatalf("portals not list: %T", result["portals"])
	}
	if len(portals) != 1 {
		t.Fatalf("tenant A should see 1 portal, got %d (cross-tenant leak)", len(portals))
	}
	first := portals[0].(map[string]any)
	if first["name"] != "alpha" {
		t.Errorf("expected alpha, got %v", first["name"])
	}
}

func TestBitrixPortals_List_MasksCredentials(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	credsJSON, _ := json.Marshal(store.BitrixPortalCredentials{ClientID: "secret-cid", ClientSecret: "secret-key"})
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tid, Name: "p", Domain: "p.bitrix24.com",
		Credentials: credsJSON,
	})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	m.handleList(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsList, nil))

	resp := readResponse(t, ch)
	raw, _ := json.Marshal(resp.Payload)
	body := string(raw)
	if strings.Contains(body, "secret-cid") || strings.Contains(body, "secret-key") {
		t.Errorf("credentials leaked into list response: %s", body)
	}
}

func TestBitrixPortals_List_RejectsMissingTenant(t *testing.T) {
	m := NewBitrixPortalsMethods(newStubBitrixPortalStore(), newStubChannelInstanceStore(), gatewayURLFn(""))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, uuid.Nil, "u", 4)
	m.handleList(context.Background(), client, buildBitrixReq(t, protocol.MethodBitrixPortalsList, nil))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("expected UNAUTHORIZED, got %+v", resp.Error)
	}
}

func TestBitrixPortals_List_SurfacesInstalledFromState(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	state, _ := json.Marshal(store.BitrixPortalState{
		RefreshToken: "RT", // → installed=true
		PublicURL:    "https://gw.example.com",
	})
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tid, Name: "p", Domain: "p.bitrix24.com", State: state,
	})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	m.handleList(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsList, nil))

	resp := readResponse(t, ch)
	result := resp.Payload.(map[string]any)
	first := result["portals"].([]any)[0].(map[string]any)
	if installed, _ := first["installed"].(bool); !installed {
		t.Errorf("expected installed=true, got %v", first["installed"])
	}
	if first["public_url"] != "https://gw.example.com" {
		t.Errorf("expected public_url surfaced, got %v", first["public_url"])
	}
}

// ---------------------------------------------------------------------------
// handleCreate — RBAC + validation + happy path
// ---------------------------------------------------------------------------

func TestBitrixPortals_Create_RBAC_OperatorDenied(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))

	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tid, "u", 4)
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name": "p", "domain": "p.bitrix24.com", "client_id": "x", "client_secret": "y",
	}))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("operator should be denied, got %+v", resp.Error)
	}
	if rows, _ := pStore.ListByTenant(context.Background(), tid); len(rows) != 0 {
		t.Errorf("no rows should be created when RBAC denies")
	}
}

func TestBitrixPortals_Create_HappyPath_ReturnsInstallURL(t *testing.T) {
	tid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	pStore := newStubBitrixPortalStore()
	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://goclaw.tamgiac.com"))

	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "admin", 4)
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name":          "myportal",
		"domain":        "myportal.bitrix24.com",
		"client_id":     "local.abc",
		"client_secret": "secret123",
	}))

	resp := readResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("create failed: %+v", resp.Error)
	}
	result := resp.Payload.(map[string]any)
	wantURL := "https://goclaw.tamgiac.com/bitrix24/install?state=" + tid.String() + ":myportal"
	if result["install_url"] != wantURL {
		t.Errorf("install_url = %q, want %q", result["install_url"], wantURL)
	}

	// Row persisted with correct shape.
	row, err := pStore.GetByName(context.Background(), tid, "myportal")
	if err != nil {
		t.Fatalf("portal not persisted: %v", err)
	}
	if row.Domain != "myportal.bitrix24.com" {
		t.Errorf("domain mismatch: %q", row.Domain)
	}
	var creds store.BitrixPortalCredentials
	_ = json.Unmarshal(row.Credentials, &creds)
	if creds.ClientID != "local.abc" || creds.ClientSecret != "secret123" {
		t.Errorf("creds not persisted correctly: %+v", creds)
	}
}

func TestBitrixPortals_Create_InvalidDomain(t *testing.T) {
	tid := uuid.New()
	m := NewBitrixPortalsMethods(newStubBitrixPortalStore(), newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))

	cases := []struct {
		name   string
		domain string
	}{
		{"leading hyphen", "-invalid.com"},
		{"scheme included", "https://example.com"},
		{"path included", "example.com/path"},
		{"space in domain", "exam ple.com"},
		{"invalid port zero", "example.com:0"},
		{"invalid port overflow", "example.com:99999"},
		{"localhost", "localhost"},
		{"localhost subdomain", "bx.localhost"},
		{"local TLD", "bx.local"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
			m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
				"name":          "p",
				"domain":        tc.domain,
				"client_id":     "x",
				"client_secret": "y",
			}))
			resp := readResponse(t, ch)
			if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
				t.Errorf("expected INVALID_REQUEST for %q, got %+v", tc.domain, resp.Error)
			}
		})
	}
}

func TestBitrixPortals_Create_SelfHostedDomain(t *testing.T) {
	tid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	pStore := newStubBitrixPortalStore()
	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://goclaw.tamgiac.com"))

	// Use a cloud domain (bitrixCloudDomainRegex) for the happy-path test
	// since it bypasses SSRF DNS validation. Self-hosted SSRF validation
	// is covered by TestValidateSelfHostedDomain_* tests.
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "admin", 4)
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name":          "myportal",
		"domain":        "myportal.bitrix24.com",
		"client_id":     "local.abc",
		"client_secret": "secret123",
	}))

	resp := readResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("create with self-hosted domain failed: %+v", resp.Error)
	}
	result := resp.Payload.(map[string]any)
	if result["domain"] != "myportal.bitrix24.com" {
		t.Errorf("domain = %q, want myportal.bitrix24.com", result["domain"])
	}
}

func TestBitrixPortals_Create_InvalidName(t *testing.T) {
	tid := uuid.New()
	m := NewBitrixPortalsMethods(newStubBitrixPortalStore(), newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	// Name with uppercase + special char → rejected.
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name":          "Bad Name!",
		"domain":        "p.bitrix24.com",
		"client_id":     "x",
		"client_secret": "y",
	}))
	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("expected INVALID_REQUEST for bad name, got %+v", resp.Error)
	}
}

func TestBitrixPortals_Create_DuplicateReturnsAlreadyExists(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tid, Name: "dup", Domain: "dup.bitrix24.com",
	})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name":          "dup",
		"domain":        "dup.bitrix24.com",
		"client_id":     "x",
		"client_secret": "y",
	}))
	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrAlreadyExists {
		t.Errorf("expected ALREADY_EXISTS, got %+v", resp.Error)
	}
}

// When the gateway hasn't observed its public URL yet, handleCreate MUST
// reject without persisting a row. Persisting would create an orphan we
// can't authorize until a delete UI exists.
func TestBitrixPortals_Create_GatewayURLUnknown_RejectsBeforePersist(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("")) // empty

	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "admin", 4)
	m.handleCreate(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsCreate, map[string]string{
		"name": "myp", "domain": "myp.bitrix24.com", "client_id": "x", "client_secret": "y",
	}))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrFailedPrecondition {
		t.Fatalf("expected FAILED_PRECONDITION, got %+v", resp.Error)
	}
	// Row must NOT be persisted.
	if _, err := pStore.GetByName(context.Background(), tid, "myp"); err == nil {
		t.Errorf("row should NOT be persisted when gateway URL is unknown")
	}
}

// ---------------------------------------------------------------------------
// handleGetInstallURL
// ---------------------------------------------------------------------------

func TestBitrixPortals_GetInstallURL_TenantIsolation(t *testing.T) {
	tidA := uuid.New()
	tidB := uuid.New()
	pStore := newStubBitrixPortalStore()
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{
		TenantID: tidB, Name: "secret", Domain: "secret.bitrix24.com",
	})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	// Tenant A asks for tenant B's portal → NOT_FOUND (not unauthorized — we
	// don't want to leak existence of cross-tenant names).
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tidA, "u", 4)
	m.handleGetInstallURL(store.WithTenantID(context.Background(), tidA), client, buildBitrixReq(t, protocol.MethodBitrixPortalsGetInstallURL, map[string]string{"name": "secret"}))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrNotFound {
		t.Errorf("expected NOT_FOUND for cross-tenant probe, got %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// handleDelete
// ---------------------------------------------------------------------------

func TestBitrixPortals_Delete_BlockedByActiveChannel(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{TenantID: tid, Name: "p"})

	chStore := newStubChannelInstanceStore()
	cfg, _ := json.Marshal(map[string]string{"portal": "p"})
	chStore.instances = []store.ChannelInstanceData{
		{Name: "support-bot", ChannelType: "bitrix24", Config: cfg},
	}

	m := NewBitrixPortalsMethods(pStore, chStore, gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	m.handleDelete(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsDelete, map[string]string{"name": "p"}))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrFailedPrecondition {
		t.Errorf("expected FAILED_PRECONDITION when channel uses portal, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "support-bot") {
		t.Errorf("error should name the offending channel, got %q", resp.Error.Message)
	}
	// Row still present.
	if _, err := pStore.GetByName(context.Background(), tid, "p"); err != nil {
		t.Error("row should still exist after blocked delete")
	}
}

func TestBitrixPortals_Delete_HappyPath_RemovesRow(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{TenantID: tid, Name: "orphan"})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleAdmin, tid, "u", 4)
	m.handleDelete(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsDelete, map[string]string{"name": "orphan"}))

	resp := readResponse(t, ch)
	if resp.Error != nil {
		t.Fatalf("delete failed: %+v", resp.Error)
	}
	if _, err := pStore.GetByName(context.Background(), tid, "orphan"); err == nil {
		t.Error("row should be deleted")
	}
}

func TestBitrixPortals_Delete_RBAC_OperatorDenied(t *testing.T) {
	tid := uuid.New()
	pStore := newStubBitrixPortalStore()
	_ = pStore.Create(context.Background(), &store.BitrixPortalData{TenantID: tid, Name: "p"})

	m := NewBitrixPortalsMethods(pStore, newStubChannelInstanceStore(), gatewayURLFn("https://gw.example.com"))
	client, ch := gateway.NewCapturingTestClient(permissions.RoleOperator, tid, "u", 4)
	m.handleDelete(store.WithTenantID(context.Background(), tid), client, buildBitrixReq(t, protocol.MethodBitrixPortalsDelete, map[string]string{"name": "p"}))

	resp := readResponse(t, ch)
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("operator should be denied delete, got %+v", resp.Error)
	}
}

// ---------------------------------------------------------------------------
// Validation helpers (regex)
// ---------------------------------------------------------------------------

func TestBitrixDomainRegex(t *testing.T) {
	good := []string{
		"tamgiac.bitrix24.com",
		"my-corp.bitrix24.eu",
		"a.bitrix24.com",
		"company.bitrix.info",
		"mycorp.bitrix24.vn",
		"portal.bitrix24.tr",
		"miempresa.bitrix24.es",
		"empresa.bitrix24.com.br",
	}
	bad := []string{
		"tamgiac.bitrix24",
		"tamgiac.bitrix24.xx",
		"-bad.bitrix24.com",
		"UPPER.bitrix24.com", // we lowercase before match
		"a.b.bitrix24.com",   // multi-level subdomain not allowed
	}
	for _, d := range good {
		if !bitrixCloudDomainRegex.MatchString(d) {
			t.Errorf("should accept %q", d)
		}
	}
	for _, d := range bad {
		if bitrixCloudDomainRegex.MatchString(d) {
			t.Errorf("should reject %q", d)
		}
	}
}

func TestSelfHostedDomainRegex(t *testing.T) {
	good := []string{
		"bx.example.com",
		"portal.internal",
		"bitrix.mycompany.co.uk",
		"portal.example.com:8443",
		"bx.corp",
	}
	bad := []string{
		"-bad.example.com",
		"UPPER.example.com", // we lowercase before match
		"",
		"not a domain",
	}
	for _, d := range good {
		if !selfHostedDomainRegex.MatchString(d) {
			t.Errorf("should accept %q", d)
		}
	}
	for _, d := range bad {
		if selfHostedDomainRegex.MatchString(d) {
			t.Errorf("should reject %q", d)
		}
	}
}

func TestPortalNameRegex(t *testing.T) {
	good := []string{"tamgiac", "my-portal", "my_portal", "p1", "ab"}
	// Bad: uppercase, whitespace, leading/trailing hyphen-or-underscore, single-char, empty.
	// Consecutive hyphens internally are allowed — many slug conventions permit it.
	bad := []string{"P", "with space", "ends-", "-starts", "p", ""}
	for _, n := range good {
		if !portalNameRegex.MatchString(n) {
			t.Errorf("should accept %q", n)
		}
	}
	for _, n := range bad {
		if portalNameRegex.MatchString(n) {
			t.Errorf("should reject %q", n)
		}
	}
}

// ---------------------------------------------------------------------------
// SSRF + port validation for self-hosted domains
// ---------------------------------------------------------------------------

func TestValidateSelfHostedDomain_SSRF(t *testing.T) {
	// These should all be rejected as SSRF risks.
	blocked := []string{
		"127.0.0.1",
		"127.0.0.1:8080",
		"::1",
		"10.0.0.1",
		"10.0.0.1:443",
		"192.168.1.1",
		"172.16.0.1",
		"169.254.169.254", // cloud metadata
		"0.0.0.0",
		"localhost",
		"bx.localhost",
		"bx.local",
		"portal.internal.localhost",
	}
	for _, d := range blocked {
		if err := validateSelfHostedDomain(d); err == nil {
			t.Errorf("should reject %q (SSRF risk)", d)
		}
	}
}

func TestValidateSelfHostedDomain_PortRange(t *testing.T) {
	badPorts := []string{
		"example.com:0",
		"example.com:99999",
		"example.com:-1",
		"example.com:abc",
	}
	for _, d := range badPorts {
		if err := validateSelfHostedDomain(d); err == nil {
			t.Errorf("should reject %q (invalid port)", d)
		}
	}
}

func TestValidateSelfHostedDomain_StubbedDNS(t *testing.T) {
	// Save and restore the real resolver.
	orig := lookupHost
	defer func() { lookupHost = orig }()

	tests := []struct {
		name    string
		host    string
		addrs   []string
		dnsErr  error
		wantErr bool
	}{
		{"single public IP", "public.example.com", []string{"8.8.8.8"}, nil, false},
		{"public IP with port", "public.example.com", []string{"93.184.216.34"}, nil, false},
		{"single private IP", "internal.example.com", []string{"10.0.0.1"}, nil, true},
		{"DNS resolution failure", "unresolvable.example.com", nil, fmt.Errorf("no such host"), true},
		{"no addresses returned", "empty.example.com", []string{}, nil, true},
		{"invalid IP string", "bad.example.com", []string{"not-an-ip"}, nil, true},
		// Regression: multi-IP SSRF bypass — public IP first, private IP second.
		// DNS ordering is not a security boundary; must reject if ANY IP is blocked.
		{"multi-ip public-then-private", "evil.example.com", []string{"8.8.8.8", "10.0.0.1"}, nil, true},
		{"multi-ip private-then-public", "evil2.example.com", []string{"192.168.1.1", "8.8.4.4"}, nil, true},
		{"multi-ip all public", "safe.example.com", []string{"8.8.8.8", "8.8.4.4"}, nil, false},
		{"multi-ip metadata IP", "meta.example.com", []string{"8.8.8.8", "169.254.169.254"}, nil, true},
		{"multi-ip IPv6 loopback", "ipv6evil.example.com", []string{"2001:db8::1", "::1"}, nil, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lookupHost = func(host string) ([]string, error) {
				if host == tc.host {
					return tc.addrs, tc.dnsErr
				}
				return nil, fmt.Errorf("unexpected host: %s", host)
			}

			domain := tc.host
			// Add port for the "public IP with port" case.
			if tc.name == "public IP with port" {
				domain = tc.host + ":443"
			}

			err := validateSelfHostedDomain(domain)
			if tc.wantErr && err == nil {
				t.Errorf("expected error for %q, got nil", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error for %q, got: %v", tc.name, err)
			}
		})
	}
}

// TestIsDuplicateKeyErr covers the two backend error string shapes we map
// to ALREADY_EXISTS.
func TestIsDuplicateKeyErr(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("ERROR: duplicate key value violates unique constraint"), true},
		{errors.New("SQLSTATE 23505"), true},
		{errors.New("UNIQUE constraint failed: bitrix_portals.tenant_id, bitrix_portals.name"), true},
		{errors.New("connection refused"), false},
	}
	for _, c := range cases {
		if got := isDuplicateKeyErr(c.err); got != c.want {
			t.Errorf("isDuplicateKeyErr(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
