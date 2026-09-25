package oa

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestFlexSeconds_NumberAndString covers Zalo's mixed expires_in encoding.
func TestFlexSeconds_NumberAndString(t *testing.T) {
	var r tokenResponse
	if err := json.Unmarshal([]byte(`{"expires_in":3600}`), &r); err != nil {
		t.Fatalf("number form: %v", err)
	}
	if r.ExpiresIn != 3600 {
		t.Errorf("number form = %d, want 3600", r.ExpiresIn)
	}

	r = tokenResponse{}
	if err := json.Unmarshal([]byte(`{"expires_in":"3600"}`), &r); err != nil {
		t.Fatalf("string form: %v", err)
	}
	if r.ExpiresIn != 3600 {
		t.Errorf("string form = %d, want 3600", r.ExpiresIn)
	}

	r = tokenResponse{}
	if err := json.Unmarshal([]byte(`{"expires_in":null}`), &r); err != nil {
		t.Fatalf("null form: %v", err)
	}
	if r.ExpiresIn != 0 {
		t.Errorf("null form = %d, want 0", r.ExpiresIn)
	}
}

// TestExchangeCode_Success posts the OAuth form and parses the token pair.
func TestExchangeCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("secret_key") != "sk" {
			t.Errorf("secret_key header = %q, want sk", r.Header.Get("secret_key"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "c1" {
			t.Errorf("form = %#v", r.Form)
		}
		w.Write([]byte(`{"access_token":"at1","refresh_token":"rt1","expires_in":"3600","refresh_token_expires_in":2592000}`))
	}))
	defer srv.Close()

	cli := NewClient(5 * time.Second)
	cli.oauthBase = srv.URL + "/v4"
	tok, err := cli.ExchangeCode(context.Background(), "app1", "sk", "c1")
	if err != nil {
		t.Fatalf("ExchangeCode: %v", err)
	}
	if tok.AccessToken != "at1" || tok.RefreshToken != "rt1" {
		t.Errorf("tokens = %+v", tok)
	}
	if tok.RefreshTokenExpiresAt.IsZero() {
		t.Error("refresh expiry not set")
	}
}

// TestExchangeCode_EmptyAccessToken rejects responses without a token.
func TestExchangeCode_EmptyAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"access_token":"","expires_in":3600}`))
	}))
	defer srv.Close()

	cli := NewClient(5 * time.Second)
	cli.oauthBase = srv.URL + "/v4"
	if _, err := cli.ExchangeCode(context.Background(), "app1", "sk", "c1"); err == nil {
		t.Fatal("expected error on empty access_token")
	}
}

// TestClassify covers the auth family + unknown fallback.
func TestClassify(t *testing.T) {
	for _, code := range []int{-216, 216, -401, 401} {
		info := Classify(code)
		if info.Family != FamilyAuth || !info.Retryable {
			t.Errorf("Classify(%d) = %+v, want auth+retryable", code, info)
		}
	}
	if info := Classify(codeInvalidGrant); info.Family != FamilyAuth || info.Retryable {
		t.Errorf("Classify(invalidGrant) = %+v, want auth non-retryable", info)
	}
	if info := Classify(-210); info.Family != FamilySize {
		t.Errorf("Classify(-210) = %+v, want size", info)
	}
	if info := Classify(-14003); info.Family != FamilyConfig {
		t.Errorf("Classify(-14003) = %+v, want config", info)
	}
	if info := Classify(999999); info.Family != FamilyUnknown {
		t.Errorf("Classify(unknown) = %+v, want unknown", info)
	}
}

// TestAPIError_IsAuth checks the code family + substring fallback.
func TestAPIError_IsAuth(t *testing.T) {
	if !(&APIError{Code: -216}).isAuth() {
		t.Error("-216 should be auth")
	}
	if !(&APIError{Code: 0, Message: "Access_token expired"}).isAuth() {
		t.Error("substring fallback failed")
	}
	if (&APIError{Code: -210}).isAuth() {
		t.Error("-210 should not be auth")
	}
}

// TestClassifyRefreshError maps invalid-grant codes to ErrAuthExpired.
func TestClassifyRefreshError(t *testing.T) {
	if err := classifyRefreshError(&APIError{Code: codeInvalidGrant, Message: "bad"}); !errors.Is(err, ErrAuthExpired) {
		t.Errorf("invalid_grant: got %v, want ErrAuthExpired", err)
	}
	if err := classifyRefreshError(errors.New("network")); errors.Is(err, ErrAuthExpired) {
		t.Errorf("plain error must not escalate: %v", err)
	}
	if err := classifyRefreshError(nil); err != nil {
		t.Errorf("nil must stay nil: %v", err)
	}
}

// TestCredsWithTokens_PreservesOmittedRefreshExpiry ensures a one-time
// omission doesn't blank a previously set deadline.
func TestCredsWithTokens_PreservesOmittedRefreshExpiry(t *testing.T) {
	base := time.Now().UTC().Add(30 * 24 * time.Hour)
	c := &ChannelCreds{RefreshTokenExpiresAt: base}
	c.WithTokens(&Tokens{
		AccessToken:  "at",
		RefreshToken: "rt",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	})
	if !c.RefreshTokenExpiresAt.Equal(base) {
		t.Errorf("refresh expiry blanked: %v vs %v", c.RefreshTokenExpiresAt, base)
	}
	if c.LastRefreshAt.IsZero() {
		t.Error("LastRefreshAt not stamped")
	}

	c2 := &ChannelCreds{}
	newExp := time.Now().UTC().Add(10 * 24 * time.Hour)
	c2.WithTokens(&Tokens{AccessToken: "at2", RefreshToken: "rt2", ExpiresAt: time.Now().UTC().Add(time.Hour), RefreshTokenExpiresAt: newExp})
	if !c2.RefreshTokenExpiresAt.Equal(newExp) {
		t.Error("new refresh expiry not applied")
	}
}

// TestCredsRoundTrip covers Marshal/LoadCreds symmetry.
func TestCredsRoundTrip(t *testing.T) {
	c := &ChannelCreds{AppID: "a", SecretKey: "s", OAID: "o", RedirectURI: "https://x/cb"}
	raw, err := c.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := LoadCreds(raw)
	if err != nil {
		t.Fatalf("LoadCreds: %v", err)
	}
	if got.AppID != "a" || got.SecretKey != "s" || got.OAID != "o" || got.RedirectURI != "https://x/cb" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

// TestPersist_NilGuards asserts the Persist preconditions fail loudly.
func TestPersist_NilGuards(t *testing.T) {
	if err := Persist(context.Background(), nil, uuid.New(), &ChannelCreds{}); err == nil {
		t.Error("nil store must error")
	}
	if err := Persist(context.Background(), &noopCIStore{}, uuid.Nil, &ChannelCreds{}); err == nil {
		t.Error("nil instance ID must error")
	}
}

type noopCIStore struct{}

func (noopCIStore) Create(context.Context, *store.ChannelInstanceData) error { return nil }
func (noopCIStore) Get(context.Context, uuid.UUID) (*store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) GetByName(context.Context, string) (*store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) Update(context.Context, uuid.UUID, map[string]any) error { return nil }
func (noopCIStore) MergeConfig(context.Context, uuid.UUID, map[string]any) error {
	return nil
}
func (noopCIStore) Delete(context.Context, uuid.UUID) error { return nil }
func (noopCIStore) ListEnabled(context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) ListAll(context.Context) ([]store.ChannelInstanceData, error) { return nil, nil }
func (noopCIStore) ListAllInstances(context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) ListAllEnabled(context.Context) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) ListPaged(context.Context, store.ChannelInstanceListOpts) ([]store.ChannelInstanceData, error) {
	return nil, nil
}
func (noopCIStore) CountInstances(context.Context, store.ChannelInstanceListOpts) (int, error) {
	return 0, nil
}

var _ store.ChannelInstanceStore = noopCIStore{}
