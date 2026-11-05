package secretvault

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// fakeStore implements only what the runtime needs; other methods panic via
// the nil embedded interface, which proves nothing else is called.
type fakeStore struct {
	store.SecretVaultStore
	mu       sync.Mutex
	entries  map[string]store.SecretVaultResolved // by name
	granted  map[string]bool
	audit    []store.SecretVaultAuditEntry
	auditErr error
}

func (f *fakeStore) ResolveForUse(_ context.Context, _ uuid.UUID, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	r, ok := f.entries[name]
	if !ok {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotFound
	}
	if !f.granted[name] {
		return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotGranted
	}
	e := r.Entry
	return &e, r.Payload, nil
}

func (f *fakeStore) ListGrantedSecrets(context.Context, uuid.UUID) ([]store.SecretVaultResolved, error) {
	var out []store.SecretVaultResolved
	for n, r := range f.entries {
		if f.granted[n] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.auditErr != nil {
		return f.auditErr
	}
	f.audit = append(f.audit, *a)
	return nil
}

func newFake() *fakeStore {
	mk := func(name, kind string, domains []string, p store.SecretVaultPayload) store.SecretVaultResolved {
		return store.SecretVaultResolved{
			Entry:   store.SecretVaultEntry{ID: uuid.New(), Name: name, Kind: kind, AllowedDomains: domains},
			Payload: p,
		}
	}
	return &fakeStore{
		entries: map[string]store.SecretVaultResolved{
			"gh":     mk("gh", store.SecretKindAPIKey, []string{"api.github.com", "*.example.com"}, store.SecretVaultPayload{Secret: "ghp_SuperSecretToken+/=1"}),
			"bank":   mk("bank", store.SecretKindLogin, []string{"bank.test"}, store.SecretVaultPayload{Username: "alice", Secret: "p@ss word&1"}),
			"hidden": mk("hidden", store.SecretKindAPIKey, []string{"a.com"}, store.SecretVaultPayload{Secret: "hidden-value-123"}),
		},
		granted: map[string]bool{"gh": true, "bank": true},
	}
}

func runtimeCtx(t *testing.T, f *fakeStore) (context.Context, *Runtime) {
	t.Helper()
	rt := NewRuntime(f, uuid.New(), "user1")
	if err := rt.Preload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return WithRuntime(context.Background(), rt), rt
}

func TestVariantsCoverCommonEncodings(t *testing.T) {
	v := `tok/en+"x"=&é`
	got := map[string]bool{}
	for _, s := range Variants(v) {
		got[s] = true
	}
	for _, want := range []string{
		v,
		url.QueryEscape(v),
		url.PathEscape(v),
		base64.StdEncoding.EncodeToString([]byte(v)),
		base64.RawURLEncoding.EncodeToString([]byte(v)),
		`tok/en+\"x\"=&é`,
	} {
		if !got[want] {
			t.Errorf("missing variant %q", want)
		}
	}
	if Variants("") != nil {
		t.Error("empty value must yield no variants")
	}
}

func TestRedactorRemovesValueAndEncodings(t *testing.T) {
	r := NewRedactor()
	r.AddPayload(store.SecretVaultPayload{Username: "alice", Secret: "p@ss word&1"})
	basic := base64.StdEncoding.EncodeToString([]byte("alice:p@ss word&1"))
	inputs := []string{
		"password is p@ss word&1 ok",
		"q=" + url.QueryEscape("p@ss word&1"),
		"Authorization: Basic " + basic,
		`{"pw":"p@ss word&1"}`,
		"user alice logged in",
	}
	for _, in := range inputs {
		out := r.Redact(in)
		for _, secret := range []string{"p@ss word&1", url.QueryEscape("p@ss word&1"), basic, "alice"} {
			if strings.Contains(out, secret) {
				t.Errorf("%q still contains %q -> %q", in, secret, out)
			}
		}
		if r.Redact(out) != out {
			t.Errorf("not idempotent for %q", in)
		}
	}
}

func TestRedactorEmptyIsIdentityAndNilSafe(t *testing.T) {
	var nilR *Redactor
	if nilR.Redact("x") != "x" || !nilR.Empty() || nilR.Func() != nil {
		t.Fatal("nil redactor must be identity")
	}
	r := NewRedactor()
	if r.Redact("hello") != "hello" || !r.Empty() {
		t.Fatal("empty redactor must be identity")
	}
}

func TestRedactorShortValuesOnlyOnBoundaries(t *testing.T) {
	r := NewRedactor()
	r.AddValue("abc")
	if got := r.Redact("pass abc end"); strings.Contains(got, "abc") {
		t.Errorf("standalone short value kept: %q", got)
	}
	if got := r.Redact("abcdef xabc"); got != "abcdef xabc" {
		t.Errorf("short value inside a word must not be rewritten: %q", got)
	}
	if got := r.Redact("(abc),abc."); strings.Contains(got, "abc") {
		t.Errorf("punctuated short value kept: %q", got)
	}
	r2 := NewRedactor()
	r2.AddValue("abc")
	r2.AddValue("abcabc12")
	if got := r2.Redact("abcabc12 abc"); got != Mask+" "+Mask {
		t.Errorf("longest-first / mixed: %q", got)
	}
}

func TestRedactorConcurrentUse(t *testing.T) {
	r := NewRedactor()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.AddValue("secret-value-" + strings.Repeat("x", i+j%3))
				_ = r.Redact("some secret-value-xx text")
			}
		}(i)
	}
	wg.Wait()
}

// Redact reads the short-needle slice outside the lock, so adding a value must
// publish a new slice and leave the one a reader may hold untouched.
func TestRedactorRebuildDoesNotReuseTheShortSliceReadersHold(t *testing.T) {
	for range 20 {
		r := NewRedactor()
		r.AddValue("abc")
		r.AddValue("ab")
		r.mu.RLock()
		held := r.short
		r.mu.RUnlock()
		before := append([]string(nil), held...)

		r.AddValue("x")
		r.AddValue("yz")

		if !slices.Equal(held, before) {
			t.Fatalf("a slice held by a reader was rewritten: %q -> %q", before, held)
		}
		r.mu.RLock()
		now := r.short
		r.mu.RUnlock()
		if len(now) == 0 || &now[0] == &held[0] {
			t.Fatal("rebuild must publish a fresh short slice")
		}
	}
}

func TestHostAllowed(t *testing.T) {
	allowed := []string{"api.github.com", "*.example.com"}
	cases := map[string]bool{
		"api.github.com":        true,
		"API.GitHub.com.":       true,
		"github.com":            false,
		"evil.com":              false,
		"a.example.com":         true,
		"x.y.example.com":       true,
		"example.com":           false,
		"notexample.com":        false,
		"api.github.com.evil.x": false,
		"":                      false,
	}
	for host, want := range cases {
		if got := HostAllowed(host, allowed); got != want {
			t.Errorf("%q: got %v want %v", host, got, want)
		}
	}
}

func allowAuthHeader(path string) bool { return path == "headers.Authorization" || path == "body.token" }

func TestResolveArgs_SubstitutesIntoCopyOnly(t *testing.T) {
	f := newFake()
	ctx, _ := runtimeCtx(t, f)
	args := map[string]any{
		"url":     "https://api.github.com/user",
		"headers": map[string]any{"Authorization": "Bearer {{secret:gh}}", "Accept": "json"},
		"body":    map[string]any{"token": "{{secret:gh}}"},
	}
	out, err := ResolveArgs(ctx, args, ArgOptions{Kind: store.SecretKindAPIKey, Host: "api.github.com", Allow: allowAuthHeader})
	if err != nil {
		t.Fatal(err)
	}
	if got := out["headers"].(map[string]any)["Authorization"]; got != "Bearer ghp_SuperSecretToken+/=1" {
		t.Fatalf("not substituted: %v", got)
	}
	if args["headers"].(map[string]any)["Authorization"] != "Bearer {{secret:gh}}" {
		t.Fatal("input args must keep the placeholder")
	}
	// Same secret used twice in one call is audited once.
	used := 0
	for _, a := range f.audit {
		if a.Action == store.SecretAuditUsed {
			used++
		}
	}
	if used != 1 {
		t.Fatalf("want 1 used audit row, got %d", used)
	}
}

func TestResolveArgs_NoPlaceholderReturnsInput(t *testing.T) {
	args := map[string]any{"a": "b"}
	out, err := ResolveArgs(context.Background(), args, ArgOptions{})
	if err != nil || out["a"] != "b" {
		t.Fatalf("got %v %v", out, err)
	}
}

func TestResolveArgs_FailsClosed(t *testing.T) {
	f := newFake()
	ctx, _ := runtimeCtx(t, f)
	opt := ArgOptions{Kind: store.SecretKindAPIKey, Host: "api.github.com", Allow: allowAuthHeader}
	ph := func(name string) map[string]any {
		return map[string]any{"headers": map[string]any{"Authorization": "{{secret:" + name + "}}"}}
	}
	cases := []struct {
		name string
		ctx  context.Context
		args map[string]any
		opt  ArgOptions
		want error
	}{
		{"no runtime", context.Background(), ph("gh"), opt, ErrNoRuntime},
		{"runtime cleared for child", WithRuntime(ctx, nil), ph("gh"), opt, ErrNoRuntime},
		{"not granted", ctx, ph("hidden"), opt, ErrUnavailable},
		{"unknown", ctx, ph("nope"), opt, ErrUnavailable},
		{"login for api tool", ctx, ph("bank"), ArgOptions{Kind: store.SecretKindAPIKey, Host: "bank.test", Allow: allowAuthHeader}, ErrKindMismatch},
		{"wrong host", ctx, ph("gh"), ArgOptions{Kind: store.SecretKindAPIKey, Host: "evil.com", Allow: allowAuthHeader}, ErrDomainNotAllowed},
		{"placeholder in url", ctx, map[string]any{"url": "https://{{secret:gh}}.x/"}, opt, ErrPlaceholderLocation},
		{"placeholder in map key", ctx, map[string]any{"headers": map[string]any{"{{secret:gh}}": "x"}}, opt, ErrPlaceholderLocation},
		{"no allow func", ctx, ph("gh"), ArgOptions{Kind: store.SecretKindAPIKey, Host: "api.github.com"}, ErrPlaceholderLocation},
		{"malformed", ctx, map[string]any{"headers": map[string]any{"Authorization": "{{secret:Bad Name}}"}}, opt, ErrInvalidPlaceholder},
		{"unterminated", ctx, map[string]any{"headers": map[string]any{"Authorization": "{{secret:gh"}}, opt, ErrInvalidPlaceholder},
	}
	for _, c := range cases {
		out, err := ResolveArgs(c.ctx, c.args, c.opt)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
		if out != nil {
			t.Errorf("%s: must not return args on error", c.name)
		}
		if err != nil && (strings.Contains(err.Error(), "ghp_") || strings.Contains(err.Error(), "p@ss")) {
			t.Errorf("%s: error leaks a value: %v", c.name, err)
		}
	}
}

func TestUse_DenialAuditedAndAuditFailureFailsClosed(t *testing.T) {
	f := newFake()
	ctx, rt := runtimeCtx(t, f)
	if _, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "evil.com"); !errors.Is(err, ErrDomainNotAllowed) {
		t.Fatal(err)
	}
	if len(f.audit) != 1 || f.audit[0].Action != store.SecretAuditDenied || f.audit[0].Outcome != "domain_not_allowed" {
		t.Fatalf("denial not audited: %+v", f.audit)
	}
	f.auditErr = errors.New("db down")
	if _, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "api.github.com"); err == nil {
		t.Fatal("use must fail when the audit row cannot be written")
	}
}

func TestPreloadRedactsEveryGrantedSecretBeforeAnyUse(t *testing.T) {
	f := newFake()
	_, rt := runtimeCtx(t, f)
	out := rt.Redactor.Redact("a ghp_SuperSecretToken+/=1 b p@ss word&1 c hidden-value-123")
	if strings.Contains(out, "ghp_") || strings.Contains(out, "p@ss") {
		t.Fatalf("granted secret not preloaded: %q", out)
	}
	if !strings.Contains(out, "hidden-value-123") {
		t.Fatalf("ungranted secret must not be in the redactor: %q", out)
	}
}

type fakeTarget struct{}

func (fakeTarget) SecretKinds() []string { return []string{store.SecretKindAPIKey} }

func TestIsSecretTargetUsesTypeNotName(t *testing.T) {
	if !IsSecretTarget(fakeTarget{}) || IsSecretTarget(struct{}{}) {
		t.Fatal("type assertion detection broken")
	}
}
