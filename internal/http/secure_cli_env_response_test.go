package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeSecureCLIStore struct {
	binary  *store.SecureCLIBinary
	user    *store.SecureCLIUserCredential
	created *store.SecureCLIBinary
}

func (s *fakeSecureCLIStore) Create(_ context.Context, b *store.SecureCLIBinary) error {
	cp := *b
	s.created = &cp
	return nil
}
func (s *fakeSecureCLIStore) Get(context.Context, uuid.UUID) (*store.SecureCLIBinary, error) {
	cp := *s.binary
	return &cp, nil
}
func (s *fakeSecureCLIStore) Update(context.Context, uuid.UUID, map[string]any) error { return nil }
func (s *fakeSecureCLIStore) Delete(context.Context, uuid.UUID) error                 { return nil }
func (s *fakeSecureCLIStore) List(context.Context) ([]store.SecureCLIBinary, error) {
	cp := *s.binary
	return []store.SecureCLIBinary{cp}, nil
}
func (s *fakeSecureCLIStore) LookupByBinary(context.Context, string, *uuid.UUID, string) (*store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *fakeSecureCLIStore) ListEnabled(context.Context) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *fakeSecureCLIStore) ListForAgent(context.Context, uuid.UUID) ([]store.SecureCLIBinary, error) {
	return nil, nil
}
func (s *fakeSecureCLIStore) IsRegisteredBinary(context.Context, string) (bool, error) {
	return false, nil
}
func (s *fakeSecureCLIStore) GetUserCredentials(context.Context, uuid.UUID, string) (*store.SecureCLIUserCredential, error) {
	cp := *s.user
	return &cp, nil
}
func (s *fakeSecureCLIStore) SetUserCredentials(context.Context, uuid.UUID, string, []byte) error {
	return nil
}
func (s *fakeSecureCLIStore) SetUserCredentialsTyped(context.Context, uuid.UUID, string, []byte, *string, *string) error {
	return nil
}
func (s *fakeSecureCLIStore) DeleteUserCredentials(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *fakeSecureCLIStore) ListUserCredentials(context.Context, uuid.UUID) ([]store.SecureCLIUserCredential, error) {
	cp := *s.user
	return []store.SecureCLIUserCredential{cp}, nil
}

func TestSecureCLIGetSanitizesMixedEnv(t *testing.T) {
	id := uuid.New()
	h := NewSecureCLIHandler(&fakeSecureCLIStore{
		binary: &store.SecureCLIBinary{
			BaseModel:    store.BaseModel{ID: id},
			BinaryName:   "gh",
			EncryptedEnv: []byte(`{"TOKEN":"secret-token","PUBLIC_BASE_URL":{"kind":"value","value":"https://goclaw.sh"}}`),
		},
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/cli-credentials/"+id.String(), nil)
	req.SetPathValue("id", id.String())
	rec := httptest.NewRecorder()

	h.handleGet(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("sensitive env leaked in response: %s", rec.Body.String())
	}
	var got struct {
		Env map[string]store.SecureCLIEnvResponseEntry `json:"env"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Env["TOKEN"].Masked || got.Env["TOKEN"].Value != nil {
		t.Fatalf("TOKEN not masked: %#v", got.Env["TOKEN"])
	}
	if got.Env["PUBLIC_BASE_URL"].Value == nil || *got.Env["PUBLIC_BASE_URL"].Value != "https://goclaw.sh" {
		t.Fatalf("value env not returned: %#v", got.Env["PUBLIC_BASE_URL"])
	}
}

func TestSecureCLIUserCredentialsGetDoesNotReturnLegacySensitiveRaw(t *testing.T) {
	binaryID := uuid.New()
	h := NewSecureCLIHandler(&fakeSecureCLIStore{
		user: &store.SecureCLIUserCredential{
			ID:           uuid.New(),
			BinaryID:     binaryID,
			UserID:       "user-1",
			EncryptedEnv: []byte(`{"TOKEN":"secret-token","REGION":{"kind":"value","value":"asia-southeast1"}}`),
		},
	}, nil)
	req := httptest.NewRequest(http.MethodGet, "/v1/cli-credentials/"+binaryID.String()+"/user-credentials/user-1", nil)
	req.SetPathValue("id", binaryID.String())
	req.SetPathValue("userId", "user-1")
	rec := httptest.NewRecorder()

	h.handleGetUserCredentials(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("legacy sensitive env leaked in response: %s", rec.Body.String())
	}
	var got struct {
		Env map[string]store.SecureCLIEnvResponseEntry `json:"env"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Env["TOKEN"].Masked || got.Env["TOKEN"].Value != nil {
		t.Fatalf("TOKEN not masked: %#v", got.Env["TOKEN"])
	}
	if got.Env["REGION"].Value == nil || *got.Env["REGION"].Value != "asia-southeast1" {
		t.Fatalf("REGION not returned: %#v", got.Env["REGION"])
	}
}
