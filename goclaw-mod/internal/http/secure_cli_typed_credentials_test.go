// Phase 5 tests for the typed-credential PUT branches on
// handleSetUserCredentials. Backend equivalents of the frontend Vitest cases:
// PAT happy path, SSH passphrase rejection, missing host_scope, legacy env
// unchanged. Uses a recording fake store to assert exactly what landed.
package http

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// recordingSecureCLIStore captures the args of every Set*Credentials call so
// tests can assert exactly what bytes landed at the encryption boundary.
type recordingSecureCLIStore struct {
	fakeSecureCLIStore

	lastTypedEnv      []byte
	lastTypedType     *string
	lastTypedScope    *string
	lastLegacyEnv     []byte
	lastAgentTypedEnv []byte
	lastAgentType     *string
	lastAgentScope    *string
	lastAgentLegacy   []byte
	typedCalls        int
	legacyCalls       int
	agentTypedCalls   int
	agentLegacyCalls  int
	existingForGet    *store.SecureCLIUserCredential
	existingAgent     *store.SecureCLIAgentCredential
}

func (s *recordingSecureCLIStore) SetUserCredentialsTyped(_ context.Context, _ uuid.UUID, _ string, encryptedEnv []byte, credentialType, hostScope *string) error {
	s.lastTypedEnv = append([]byte(nil), encryptedEnv...)
	s.lastTypedType = credentialType
	s.lastTypedScope = hostScope
	s.typedCalls++
	return nil
}

func (s *recordingSecureCLIStore) SetUserCredentials(_ context.Context, _ uuid.UUID, _ string, encryptedEnv []byte) error {
	s.lastLegacyEnv = append([]byte(nil), encryptedEnv...)
	s.legacyCalls++
	return nil
}

func (s *recordingSecureCLIStore) GetUserCredentials(context.Context, uuid.UUID, string) (*store.SecureCLIUserCredential, error) {
	if s.existingForGet == nil {
		return nil, nil
	}
	cp := *s.existingForGet
	return &cp, nil
}

func (s *recordingSecureCLIStore) BinaryExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *recordingSecureCLIStore) AgentExists(context.Context, uuid.UUID) (bool, error) {
	return true, nil
}

func (s *recordingSecureCLIStore) SetAgentCredentialsTyped(_ context.Context, _ uuid.UUID, _ uuid.UUID, encryptedEnv []byte, credentialType, hostScope *string, _ string) error {
	s.lastAgentTypedEnv = append([]byte(nil), encryptedEnv...)
	s.lastAgentType = credentialType
	s.lastAgentScope = hostScope
	s.agentTypedCalls++
	return nil
}

func (s *recordingSecureCLIStore) SetAgentCredentials(_ context.Context, _ uuid.UUID, _ uuid.UUID, encryptedEnv []byte, _ string) error {
	s.lastAgentLegacy = append([]byte(nil), encryptedEnv...)
	s.agentLegacyCalls++
	return nil
}

func (s *recordingSecureCLIStore) GetAgentCredentials(context.Context, uuid.UUID, uuid.UUID) (*store.SecureCLIAgentCredential, error) {
	if s.existingAgent == nil {
		return nil, nil
	}
	cp := *s.existingAgent
	return &cp, nil
}

func (s *recordingSecureCLIStore) DeleteAgentCredentials(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *recordingSecureCLIStore) ListAgentCredentials(context.Context, uuid.UUID) ([]store.SecureCLIAgentCredential, error) {
	if s.existingAgent == nil {
		return nil, nil
	}
	cp := *s.existingAgent
	return []store.SecureCLIAgentCredential{cp}, nil
}

func newAgentCredentialTestHandler(st *recordingSecureCLIStore) *SecureCLIHandler {
	ts := newMockTenantStore()
	ts.addTenant(store.MasterTenantID, "master")
	ts.setUserRole(store.MasterTenantID, "system", store.TenantRoleAdmin)
	return NewSecureCLIHandler(st, nil, ts)
}

func withTenantAdminContext(req *http.Request) *http.Request {
	ctx := store.WithTenantID(req.Context(), store.MasterTenantID)
	ctx = store.WithUserID(ctx, "system")
	return req.WithContext(ctx)
}

func putUserCred(t *testing.T, h *SecureCLIHandler, binaryID uuid.UUID, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/v1/cli-credentials/"+binaryID.String()+"/user-credentials/u1", bytes.NewReader(buf))
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("userId", "u1")
	rec := httptest.NewRecorder()
	h.handleSetUserCredentials(rec, req)
	return rec
}

func putAgentCred(t *testing.T, h *SecureCLIHandler, binaryID, agentID uuid.UUID, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/v1/cli-credentials/"+binaryID.String()+"/agent-credentials/"+agentID.String(), bytes.NewReader(buf))
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("agentId", agentID.String())
	rec := httptest.NewRecorder()
	h.handleSetAgentCredentials(rec, withTenantAdminContext(req))
	return rec
}

// 9. Handler accepts new PAT payload and routes through SetUserCredentialsTyped.
func TestPutUserCredential_PATPayload(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := NewSecureCLIHandler(st, nil)
	binaryID := uuid.New()

	rec := putUserCred(t, h, binaryID, map[string]any{
		"credential_type": "pat",
		"host_scope":      "github.com",
		"blob":            map[string]string{"token": "ghp_abcDEF123456"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.typedCalls != 1 || st.legacyCalls != 0 {
		t.Fatalf("expected typed=1 legacy=0, got typed=%d legacy=%d", st.typedCalls, st.legacyCalls)
	}
	if st.lastTypedType == nil || *st.lastTypedType != "pat" {
		t.Fatalf("type mismatch: %#v", st.lastTypedType)
	}
	if st.lastTypedScope == nil || *st.lastTypedScope != "github.com" {
		t.Fatalf("scope mismatch: %#v", st.lastTypedScope)
	}
	// Stored bytes must be exactly the blob the runtime decodes.
	var got map[string]string
	if err := json.Unmarshal(st.lastTypedEnv, &got); err != nil {
		t.Fatal(err)
	}
	if got["token"] != "ghp_abcDEF123456" {
		t.Fatalf("stored token mismatch: %#v", got)
	}
	// Response body must never echo the secret.
	if strings.Contains(rec.Body.String(), "ghp_abcDEF") {
		t.Fatalf("response leaked token: %s", rec.Body.String())
	}
}

func TestPutAgentCredential_PATPayload(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := newAgentCredentialTestHandler(st)
	binaryID := uuid.New()
	agentID := uuid.New()

	rec := putAgentCred(t, h, binaryID, agentID, map[string]any{
		"credential_type": "pat",
		"host_scope":      "github.com",
		"blob":            map[string]string{"token": "ghp_agentABC123456"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.agentTypedCalls != 1 || st.agentLegacyCalls != 0 {
		t.Fatalf("expected agent typed=1 legacy=0, got typed=%d legacy=%d", st.agentTypedCalls, st.agentLegacyCalls)
	}
	if st.lastAgentType == nil || *st.lastAgentType != "pat" {
		t.Fatalf("type mismatch: %#v", st.lastAgentType)
	}
	if st.lastAgentScope == nil || *st.lastAgentScope != "github.com" {
		t.Fatalf("scope mismatch: %#v", st.lastAgentScope)
	}
	var got map[string]string
	if err := json.Unmarshal(st.lastAgentTypedEnv, &got); err != nil {
		t.Fatal(err)
	}
	if got["token"] != "ghp_agentABC123456" {
		t.Fatalf("stored token mismatch: %#v", got)
	}
	if strings.Contains(rec.Body.String(), "ghp_agent") {
		t.Fatalf("response leaked token: %s", rec.Body.String())
	}
}

func TestPutAgentCredential_LegacyEnvPreservesMaskedSensitiveValue(t *testing.T) {
	binaryID := uuid.New()
	agentID := uuid.New()
	st := &recordingSecureCLIStore{existingAgent: &store.SecureCLIAgentCredential{
		ID:           uuid.New(),
		BinaryID:     binaryID,
		AgentID:      agentID,
		EncryptedEnv: []byte(`{"GITHUB_TOKEN":{"kind":"sensitive","value":"ghp_existing"},"PUBLIC_URL":{"kind":"value","value":"https://old.example"}}`),
	}}
	h := newAgentCredentialTestHandler(st)

	rec := putAgentCred(t, h, binaryID, agentID, map[string]any{
		"env": map[string]any{
			"GITHUB_TOKEN": map[string]string{"kind": "sensitive", "value": ""},
			"PUBLIC_URL":   map[string]string{"kind": "value", "value": "https://new.example"},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.agentLegacyCalls != 1 || st.agentTypedCalls != 0 {
		t.Fatalf("expected agent legacy=1 typed=0, got legacy=%d typed=%d", st.agentLegacyCalls, st.agentTypedCalls)
	}
	env, err := store.ParseSecureCLIEnv(st.lastAgentLegacy)
	if err != nil {
		t.Fatalf("ParseSecureCLIEnv(lastAgentLegacy): %v", err)
	}
	if got := env["GITHUB_TOKEN"].Value; got != "ghp_existing" {
		t.Fatalf("GITHUB_TOKEN value = %q, want preserved existing secret", got)
	}
	if got := env["PUBLIC_URL"].Value; got != "https://new.example" {
		t.Fatalf("PUBLIC_URL value = %q", got)
	}
	if got := env["PUBLIC_URL"].Kind; got != store.SecureCLIEnvKindValue {
		t.Fatalf("PUBLIC_URL kind = %q", got)
	}
}

func TestPutAgentCredential_LegacyEnvRejectsDeniedKeysAfterMerge(t *testing.T) {
	binaryID := uuid.New()
	agentID := uuid.New()
	st := &recordingSecureCLIStore{existingAgent: &store.SecureCLIAgentCredential{
		ID:           uuid.New(),
		BinaryID:     binaryID,
		AgentID:      agentID,
		EncryptedEnv: []byte(`{"GITHUB_TOKEN":{"kind":"sensitive","value":"ghp_existing"}}`),
	}}
	h := newAgentCredentialTestHandler(st)

	rec := putAgentCred(t, h, binaryID, agentID, map[string]any{
		"env": map[string]any{
			"GITHUB_TOKEN": map[string]string{"kind": "sensitive", "value": ""},
			"PATH":         map[string]string{"kind": "value", "value": "/tmp/bin"},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.agentLegacyCalls != 0 {
		t.Fatalf("agent legacy save called despite denied env key")
	}
	if !strings.Contains(rec.Body.String(), "PATH") {
		t.Fatalf("response did not mention denied key: %s", rec.Body.String())
	}
}

func TestGetAgentCredential_TypedDoesNotReturnSecretBlob(t *testing.T) {
	credType := "pat"
	hostScope := "github.com"
	binaryID := uuid.New()
	agentID := uuid.New()
	st := &recordingSecureCLIStore{existingAgent: &store.SecureCLIAgentCredential{
		ID:             uuid.New(),
		BinaryID:       binaryID,
		AgentID:        agentID,
		EncryptedEnv:   []byte(`{"token":"ghp_secret_agent_token"}`),
		CredentialType: &credType,
		HostScope:      &hostScope,
	}}
	h := newAgentCredentialTestHandler(st)
	req := httptest.NewRequest(http.MethodGet, "/v1/cli-credentials/"+binaryID.String()+"/agent-credentials/"+agentID.String(), nil)
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("agentId", agentID.String())
	rec := httptest.NewRecorder()

	h.handleGetAgentCredentials(rec, withTenantAdminContext(req))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "ghp_secret") || strings.Contains(rec.Body.String(), "token") {
		t.Fatalf("typed agent credential leaked blob material: %s", rec.Body.String())
	}
}

func TestAgentCredentialsRouteRequiresTenantAdmin(t *testing.T) {
	oldToken := pkgGatewayToken
	oldFallback := pkgNoAuthFallbackAllowed
	pkgGatewayToken = ""
	pkgNoAuthFallbackAllowed = true
	defer func() {
		pkgGatewayToken = oldToken
		pkgNoAuthFallbackAllowed = oldFallback
	}()

	st := &recordingSecureCLIStore{}
	ts := newMockTenantStore()
	ts.addTenant(store.MasterTenantID, "master")
	ts.setUserRole(store.MasterTenantID, "system", store.TenantRoleViewer)
	h := NewSecureCLIHandler(st, nil, ts)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	binaryID := uuid.New()
	agentID := uuid.New()
	body, _ := json.Marshal(map[string]any{
		"credential_type": "pat",
		"host_scope":      "github.com",
		"blob":            map[string]string{"token": "ghp_routeABC123456"},
	})
	req := httptest.NewRequest(http.MethodPut, "/v1/cli-credentials/"+binaryID.String()+"/agent-credentials/"+agentID.String(), bytes.NewReader(body))
	req.Header.Set("X-GoClaw-User-Id", "browser-user")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.agentTypedCalls != 0 || st.agentLegacyCalls != 0 {
		t.Fatalf("tenant viewer must not write credentials, typed=%d legacy=%d", st.agentTypedCalls, st.agentLegacyCalls)
	}
}

// 10. Handler rejects passphrase-protected SSH key with error_key.
func TestPutUserCredential_RejectsPassphraseKey(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := NewSecureCLIHandler(st, nil)
	binaryID := uuid.New()

	pem := genPassphrasePEM(t, "topsecret")
	rec := putUserCred(t, h, binaryID, map[string]any{
		"credential_type": "ssh_key",
		"host_scope":      "github.com",
		"blob":            map[string]string{"key": string(pem)},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error_key"] != "git.cred_ssh_passphrase_unsupported" {
		t.Fatalf("expected error_key=git.cred_ssh_passphrase_unsupported, got %#v", resp)
	}
	if st.typedCalls != 0 || st.legacyCalls != 0 {
		t.Fatalf("no DB write expected on passphrase reject, got typed=%d legacy=%d", st.typedCalls, st.legacyCalls)
	}
}

// 11. Handler rejects PAT/SSH payload missing host_scope.
func TestPutUserCredential_PATNoHostScope(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := NewSecureCLIHandler(st, nil)
	binaryID := uuid.New()

	rec := putUserCred(t, h, binaryID, map[string]any{
		"credential_type": "pat",
		"blob":            map[string]string{"token": "ghp_xyz"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["error_key"] != "git.cred_host_scope_required" {
		t.Fatalf("expected error_key=git.cred_host_scope_required, got %#v", resp)
	}
	if st.typedCalls != 0 {
		t.Fatalf("no DB write expected on validation reject")
	}
}

// 12. Legacy env-paste body keeps the legacy code path unchanged.
func TestPutUserCredential_LegacyEnvUnchanged(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := NewSecureCLIHandler(st, nil)
	binaryID := uuid.New()

	rec := putUserCred(t, h, binaryID, map[string]any{
		"env": map[string]string{"GH_TOKEN": "ghp_legacy"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if st.legacyCalls != 1 || st.typedCalls != 0 {
		t.Fatalf("expected legacy=1 typed=0, got legacy=%d typed=%d", st.legacyCalls, st.typedCalls)
	}
}

// 11b. SSH key CRLF normalization — pasted Windows-encoded key must be saved
// as LF so ssh.ParsePrivateKey at exec time succeeds. Belt-and-suspenders
// since the validator already runs on the normalized bytes.
func TestPutUserCredential_SSHKeyCRLFNormalized(t *testing.T) {
	st := &recordingSecureCLIStore{}
	h := NewSecureCLIHandler(st, nil)
	binaryID := uuid.New()

	pem := genUnencryptedPEM(t)
	crlf := strings.ReplaceAll(string(pem), "\n", "\r\n")
	rec := putUserCred(t, h, binaryID, map[string]any{
		"credential_type": "ssh_key",
		"host_scope":      "github.com",
		"blob":            map[string]string{"key": crlf},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var stored map[string]string
	if err := json.Unmarshal(st.lastTypedEnv, &stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored["key"], "\r") {
		t.Fatalf("CRLF leaked into stored key: %q", stored["key"])
	}
}

// genUnencryptedPEM mirrors the helper in tools/credential_adapter_git_ssh_test
// but lives here to avoid cross-package test deps.
func genUnencryptedPEM(t *testing.T) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		base64Wrap(block.Bytes) +
		"\n-----END OPENSSH PRIVATE KEY-----\n")
}

func genPassphrasePEM(t *testing.T, pw string) []byte {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	block, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(pw))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		base64Wrap(block.Bytes) +
		"\n-----END OPENSSH PRIVATE KEY-----\n")
}

func base64Wrap(b []byte) string {
	s := base64.StdEncoding.EncodeToString(b)
	var out strings.Builder
	for i := 0; i < len(s); i += 70 {
		end := min(i+70, len(s))
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(s[i:end])
	}
	return out.String()
}
