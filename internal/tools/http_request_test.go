package tools

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const httpTestToken = "ghp_HttpRequestToken_42"

type httpVaultStore struct {
	store.SecretVaultStore
	entries map[string]store.SecretVaultResolved
	audit   []store.SecretVaultAuditEntry
}

func (s *httpVaultStore) ResolveForUse(_ context.Context, _ uuid.UUID, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	r, ok := s.entries[name]
	if !ok {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotFound
	}
	e := r.Entry
	return &e, r.Payload, nil
}

func (s *httpVaultStore) ListGrantedSecrets(context.Context, uuid.UUID) ([]store.SecretVaultResolved, error) {
	var out []store.SecretVaultResolved
	for _, r := range s.entries {
		out = append(out, r)
	}
	return out, nil
}

func (s *httpVaultStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	s.audit = append(s.audit, *a)
	return nil
}

// httpEnv wires a TLS test server on 127.0.0.1 and a tool that trusts it.
type httpEnv struct {
	tool *HTTPRequestTool
	ctx  context.Context
	st   *httpVaultStore
	srv  *httptest.Server
	hits atomic.Int32

	handler http.HandlerFunc
}

func newHTTPEnv(t *testing.T, domains []string, handler http.HandlerFunc) *httpEnv {
	t.Helper()
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })

	e := &httpEnv{handler: handler}
	e.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.hits.Add(1)
		e.handler(w, r)
	}))
	t.Cleanup(e.srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(e.srv.Certificate())
	e.tool = NewHTTPRequestTool()
	e.tool.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}

	e.st = &httpVaultStore{entries: map[string]store.SecretVaultResolved{
		"gh": {
			Entry:   store.SecretVaultEntry{ID: uuid.New(), Name: "gh", Kind: store.SecretKindAPIKey, AllowedDomains: domains},
			Payload: store.SecretVaultPayload{Secret: httpTestToken},
		},
		"bank": {
			Entry:   store.SecretVaultEntry{ID: uuid.New(), Name: "bank", Kind: store.SecretKindLogin, AllowedDomains: domains},
			Payload: store.SecretVaultPayload{Username: "alice", Secret: "bank-password-1"},
		},
	}}
	rt := secretvault.NewRuntime(e.st, uuid.New(), "user1")
	ctx := secretvault.WithRuntime(context.Background(), rt)
	if err := rt.Preload(ctx); err != nil {
		t.Fatal(err)
	}
	e.ctx = ctx
	return e
}

func (e *httpEnv) args(extra map[string]any) map[string]any {
	a := map[string]any{"url": e.srv.URL + "/v1/user"}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func echoAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Set-Cookie", "session=abc123")
	w.Header().Set("X-Echo-Auth", r.Header.Get("Authorization"))
	fmt.Fprintf(w, "you sent: %s query=%s", r.Header.Get("Authorization"), r.URL.RawQuery)
}

func TestHTTPRequest_SubstitutesSecretAndRedactsEverythingReturned(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	var seen string
	e.handler = (func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		echoAuth(w, r)
	})

	res := e.tool.Execute(e.ctx, e.args(map[string]any{
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	}))
	if res.IsError {
		t.Fatalf("error: %s", res.ForLLM)
	}
	if seen != "Bearer "+httpTestToken {
		t.Fatalf("server saw %q", seen)
	}
	if strings.Contains(res.ForLLM, httpTestToken) {
		t.Fatalf("result leaks the secret:\n%s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, secretvault.Mask) {
		t.Fatalf("expected the echo to be masked:\n%s", res.ForLLM)
	}
	if strings.Contains(strings.ToLower(res.ForLLM), "set-cookie") || strings.Contains(res.ForLLM, "abc123") {
		t.Fatalf("Set-Cookie must be hidden:\n%s", res.ForLLM)
	}
	if len(e.st.audit) != 1 || e.st.audit[0].Action != store.SecretAuditUsed || e.st.audit[0].TargetHost != "127.0.0.1" {
		t.Fatalf("audit %+v", e.st.audit)
	}
}

func TestHTTPRequest_SecretInQueryAndJSONBody(t *testing.T) {
	var gotQuery, gotBody string
	e := newHTTPEnv(t, []string{"127.0.0.1"}, nil)
	e.handler = (func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		buf := new(strings.Builder)
		_, _ = fmt.Fprint(buf, r.Header.Get("Content-Type"), "|")
		b := make([]byte, 512)
		n, _ := r.Body.Read(b)
		buf.Write(b[:n])
		gotBody = buf.String()
		fmt.Fprint(w, "ok")
	})
	res := e.tool.Execute(e.ctx, e.args(map[string]any{
		"method": "POST",
		"url":    e.srv.URL + "/v1/x?token={{secret:gh}}",
		"body":   map[string]any{"auth": "{{secret:gh}}", "n": 1},
	}))
	if res.IsError {
		t.Fatalf("error: %s", res.ForLLM)
	}
	if gotQuery != "token="+httpTestToken {
		t.Fatalf("query %q", gotQuery)
	}
	if !strings.HasPrefix(gotBody, "application/json|") || !strings.Contains(gotBody, `"auth":"`+httpTestToken+`"`) {
		t.Fatalf("body %q", gotBody)
	}
	if strings.Contains(res.ForLLM, httpTestToken) {
		t.Fatal("result leaks the secret")
	}
	if len(e.st.audit) != 1 {
		t.Fatalf("one secret used twice in a call must be audited once, got %d", len(e.st.audit))
	}
}

func TestHTTPRequest_RejectsPlaceholderInHostBeforeAnyResolve(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	for _, u := range []string{
		"https://{{secret:gh}}.example.com/x",
		"https://api.example.com@{{secret:gh}}/x",
		"https://{{secret:gh}}/x",
	} {
		res := e.tool.Execute(e.ctx, map[string]any{"url": u})
		if !res.IsError || !strings.Contains(res.ForLLM, "host") {
			t.Errorf("%s: want host rejection, got %+v", u, res)
		}
	}
	if len(e.st.audit) != 0 || e.hits.Load() != 0 {
		t.Fatal("nothing may be resolved or sent")
	}
}

func TestHTTPRequest_DomainBindingAndKindAreEnforced(t *testing.T) {
	e := newHTTPEnv(t, []string{"api.other.com"}, echoAuth)
	res := e.tool.Execute(e.ctx, e.args(map[string]any{"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"}}))
	if !res.IsError || e.hits.Load() != 0 {
		t.Fatalf("secret sent to a host outside allowed_domains: %+v hits=%d", res, e.hits.Load())
	}
	e2 := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	res = e2.tool.Execute(e2.ctx, e2.args(map[string]any{"headers": map[string]any{"X-User": "{{secret:bank}}"}}))
	if !res.IsError || e2.hits.Load() != 0 {
		t.Fatalf("login secret must not be usable by http_request: %+v", res)
	}
}

func TestHTTPRequest_RedirectToDisallowedHostIsBlockedBeforeSending(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, nil)
	e.handler = (func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://127.0.0.2:9/steal", http.StatusFound)
	})
	res := e.tool.Execute(e.ctx, e.args(map[string]any{"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"}}))
	if !res.IsError {
		t.Fatalf("redirect to a host outside allowed_domains must fail: %s", res.ForLLM)
	}
	if strings.Contains(res.ForLLM, httpTestToken) {
		t.Fatal("error leaks the secret")
	}
	if e.hits.Load() != 1 {
		t.Fatalf("only the first hop may be requested, hits=%d", e.hits.Load())
	}
}

func TestHTTPRequest_RedirectWithinAllowedHostKeepsCredentialsAndDropsBodyOn302(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, nil)
	var auth2, method2 string
	e.handler = (func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		auth2, method2 = r.Header.Get("Authorization"), r.Method
		fmt.Fprint(w, "done")
	})
	res := e.tool.Execute(e.ctx, map[string]any{
		"method": "POST", "url": e.srv.URL + "/start", "body": "payload",
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}"},
	})
	if res.IsError || !strings.Contains(res.ForLLM, "done") {
		t.Fatalf("unexpected: %+v", res)
	}
	if auth2 != "Bearer "+httpTestToken || method2 != "GET" {
		t.Fatalf("second hop saw auth=%q method=%q", auth2, method2)
	}
}

func TestHTTPRequest_ProtocolAndInputLimits(t *testing.T) {
	e := newHTTPEnv(t, []string{"127.0.0.1"}, echoAuth)
	plainHTTP := "http://127.0.0.1:1/x"
	cases := map[string]map[string]any{
		"http with secret": {"url": plainHTTP, "headers": map[string]any{"Authorization": "{{secret:gh}}"}},
		"trace":            {"url": e.srv.URL, "method": "TRACE"},
		"get with body":    {"url": e.srv.URL, "body": "x"},
		"host header":      {"url": e.srv.URL, "headers": map[string]any{"Host": "evil.test"}},
		"no url":           {"method": "GET"},
		"huge body":        {"url": e.srv.URL, "method": "POST", "body": strings.Repeat("a", httpRequestMaxBodyBytes+1)},
	}
	for name, a := range cases {
		res := e.tool.Execute(e.ctx, a)
		if !res.IsError {
			t.Errorf("%s: expected an error, got %q", name, res.ForLLM)
		}
	}
	if e.hits.Load() != 0 {
		t.Fatalf("invalid requests reached the network (%d)", e.hits.Load())
	}
}

func TestHTTPRequest_SSRFTargetsAreRefused(t *testing.T) {
	// No test bypass here: the real block list is under test.
	security.SetAllowLoopbackForTest(false)
	tool := NewHTTPRequestTool()
	for _, u := range []string{
		"https://127.0.0.1/",
		"https://localhost/",
		"http://169.254.169.254/latest/meta-data/",
		"https://10.0.0.5/admin",
		"https://192.168.1.1/",
		"https://[fd00::1]/",
	} {
		if res := tool.Execute(context.Background(), map[string]any{"url": u}); !res.IsError {
			t.Errorf("%s was not refused: %s", u, res.ForLLM)
		}
	}
}

func TestHTTPRequest_RedactsBeforeTruncating(t *testing.T) {
	// The token straddles the 64 KiB output cut: redaction must run first.
	pad := strings.Repeat("x", httpRequestMaxOutputChars-10)
	e := newHTTPEnv(t, []string{"127.0.0.1"}, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, pad+httpTestToken+strings.Repeat("y", 100))
	})
	res := e.tool.Execute(e.ctx, e.args(nil))
	if res.IsError {
		t.Fatal(res.ForLLM)
	}
	if strings.Contains(res.ForLLM, httpTestToken[:12]) {
		t.Fatalf("part of the secret survived truncation")
	}
	if !strings.Contains(res.ForLLM, "[response truncated]") {
		t.Fatal("truncation not reported")
	}
}

func TestHTTPRequest_IsASecretTargetForApiKeysOnly(t *testing.T) {
	tool := NewHTTPRequestTool()
	if !secretvault.IsSecretTarget(tool) {
		t.Fatal("http_request must implement SecretTarget")
	}
	if k := tool.SecretKinds(); len(k) != 1 || k[0] != store.SecretKindAPIKey {
		t.Fatalf("kinds %v", k)
	}
}
