package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/secretvault"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const captureValue = "eyJ0eXAiOiJKV1QifQ.capture-test-payload.c2lnbmF0dXJl"

// captureStore records agent writes like the real stores: pending entries,
// unique names, an audit trail.
type captureStore struct {
	store.SecretVaultStore
	mu      sync.Mutex
	entries map[string]store.SecretVaultEntry
	values  map[string]store.SecretVaultPayload
	grants  map[uuid.UUID][]uuid.UUID
	audit   []store.SecretVaultAuditEntry
}

func newCaptureStore() *captureStore {
	return &captureStore{
		entries: map[string]store.SecretVaultEntry{},
		values:  map[string]store.SecretVaultPayload{},
		grants:  map[uuid.UUID][]uuid.UUID{},
	}
}

func (s *captureStore) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[e.Name]; ok {
		return store.ErrSecretVaultNameTaken
	}
	e.ID = uuid.New()
	if e.CreatedByAgent != nil {
		e.Status = store.SecretVaultStatusPending
	}
	s.entries[e.Name] = *e
	s.values[e.Name] = p
	return nil
}

func (s *captureStore) List(context.Context) ([]store.SecretVaultEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.SecretVaultEntry
	for _, e := range s.entries {
		out = append(out, e)
	}
	return out, nil
}

func (s *captureStore) SetGrants(_ context.Context, id uuid.UUID, agents []uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.grants[id] = agents
	return nil
}

func (s *captureStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audit = append(s.audit, *a)
	return nil
}

func (s *captureStore) outcomes() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	for _, a := range s.audit {
		b.WriteString(a.Outcome + " ")
	}
	return b.String()
}

func captureRuntime(st store.SecretVaultStore, allow bool) (context.Context, *secretvault.Runtime) {
	rt := secretvault.NewRuntime(st, uuid.New(), "u1")
	rt.AllowWrite = allow
	ctx := store.WithUserID(store.WithTenantID(context.Background(), uuid.New()), "u1")
	ctx = secretvault.WithRuntime(ctx, rt)
	ctx = WithScope(ctx, BrowserScope{TenantID: store.TenantIDFromContext(ctx).String(), UserID: "u1", AgentID: rt.AgentID.String()})
	return ctx, rt
}

func captureOpts() CaptureSecretOpts {
	return CaptureSecretOpts{Source: captureLocalStorage, Key: "access_token", SecretName: "app-jwt"}
}

// reader fakes one page read from a document of the given origin.
func reader(origin, value string, found bool, err error) (func() (pageRead, error), *int) {
	calls := 0
	return func() (pageRead, error) {
		calls++
		if err != nil {
			return pageRead{}, err
		}
		return pageRead{value: value, found: found, origin: origin}, nil
	}, &calls
}

func noCaptureValue(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), captureValue) {
		t.Fatalf("error leaks the value: %v", err)
	}
}

func TestCaptureSecret_RefusesBeforeTouchingThePage(t *testing.T) {
	m := New() // not started: any page access would fail differently

	if _, err := m.CaptureSecret(context.Background(), "", captureOpts()); err == nil {
		t.Fatal("no runtime must be an error")
	}

	st := newCaptureStore()
	ctx, _ := captureRuntime(st, false)
	if _, err := m.CaptureSecret(ctx, "", captureOpts()); !errors.Is(err, secretvault.ErrWriteDisabled) {
		t.Fatalf("flag off: %v", err)
	}

	ctx, _ = captureRuntime(st, true)
	for name, mutate := range map[string]func(*CaptureSecretOpts){
		"bad source": func(o *CaptureSecretOpts) { o.Source = "indexedDB" },
		"no key":     func(o *CaptureSecretOpts) { o.Key = "" },
		"ctrl key":   func(o *CaptureSecretOpts) { o.Key = "a\nb" },
		"long key":   func(o *CaptureSecretOpts) { o.Key = strings.Repeat("k", maxCaptureKeyLen+1) },
		"login kind": func(o *CaptureSecretOpts) { o.SecretKind = store.SecretKindLogin },
		"no name":    func(o *CaptureSecretOpts) { o.SecretName = "" },
	} {
		o := captureOpts()
		mutate(&o)
		if _, err := m.CaptureSecret(ctx, "", o); err == nil || errors.Is(err, ErrCaptureUnscoped) {
			t.Errorf("%s: want a validation error, got %v", name, err)
		}
	}

	// The shared main browser and a scope of another agent are refused.
	for name, scope := range map[string]BrowserScope{
		"main browser":  {},
		"master tenant": {TenantID: MasterTenantID},
		"other agent":   {TenantID: "t", UserID: "u1", AgentID: uuid.NewString()},
	} {
		if _, err := m.CaptureSecret(WithScope(ctx, scope), "", captureOpts()); !errors.Is(err, ErrCaptureUnscoped) {
			t.Errorf("%s: want ErrCaptureUnscoped, got %v", name, err)
		}
	}
	if len(st.entries) != 0 {
		t.Fatal("nothing may be stored")
	}
	for _, o := range []string{"agent_write_disabled", "agent_capture_invalid", "agent_capture_unscoped_browser"} {
		if !strings.Contains(st.outcomes(), o) {
			t.Errorf("refusal %s not audited: %s", o, st.outcomes())
		}
	}
}

func TestCaptureSecret_StoresPendingBoundToThePageHost(t *testing.T) {
	st := newCaptureStore()
	ctx, rt := captureRuntime(st, true)
	read, _ := reader("https://app.example.com:8443", captureValue, true, nil)

	status, err := captureSecret(ctx, rt, "https://App.Example.com:8443/dashboard?x=1", captureOpts(), read)
	if err != nil || status != secretvault.WritePending {
		t.Fatalf("status=%q err=%v", status, err)
	}
	e := st.entries["app-jwt"]
	if e.Status != store.SecretVaultStatusPending || e.Kind != store.SecretKindAPIKey ||
		len(e.AllowedDomains) != 1 || e.AllowedDomains[0] != "app.example.com" {
		t.Fatalf("entry %+v", e)
	}
	if e.CreatedByAgent == nil || *e.CreatedByAgent != rt.AgentID || st.grants[e.ID][0] != rt.AgentID {
		t.Fatal("entry must be the agent's and granted to it")
	}
	if st.values["app-jwt"].Secret != captureValue {
		t.Fatal("value not stored")
	}
	if strings.Contains(rt.Redactor.Redact("v="+captureValue), captureValue) {
		t.Fatal("redactor must know the captured value")
	}
	text := captureResultText("app-jwt", status)
	if !strings.Contains(text, "{{secret:app-jwt}}") || !strings.Contains(text, "pending") || strings.Contains(text, captureValue) {
		t.Fatalf("result text %q", text)
	}

	// Same name again: refused, value never echoed.
	again, _ := reader("https://app.example.com", captureValue, true, nil)
	_, err = captureSecret(ctx, rt, "https://app.example.com/", captureOpts(), again)
	if !errors.Is(err, secretvault.ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}
	noCaptureValue(t, err)
}

func TestCaptureSecret_OriginNotFoundTooLargeAndReadErrors(t *testing.T) {
	st := newCaptureStore()
	ctx, rt := captureRuntime(st, true)

	read, calls := reader("https://a.example.com", captureValue, true, nil)
	if _, err := captureSecret(ctx, rt, "http://evil.example.net/", captureOpts(), read); err == nil || *calls != 0 {
		t.Fatalf("plain http must be refused before reading (calls=%d, err=%v)", *calls, err)
	}
	o := captureOpts()
	o.SecretName = "Bad Name"
	if _, err := captureSecret(ctx, rt, "https://a.example.com/", o, read); err == nil || *calls != 0 {
		t.Fatalf("invalid name must be refused before reading (calls=%d)", *calls)
	}

	missing, _ := reader("https://a.example.com", "", false, nil)
	if _, err := captureSecret(ctx, rt, "https://a.example.com/", captureOpts(), missing); err == nil || !strings.Contains(err.Error(), "no localStorage value") {
		t.Fatalf("missing key: %v", err)
	}
	big, _ := reader("https://a.example.com", "", true, errCaptureTooLarge)
	if _, err := captureSecret(ctx, rt, "https://a.example.com/", captureOpts(), big); !errors.Is(err, secretvault.ErrValueTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	broken, _ := reader("https://a.example.com", "", false, errors.New("Runtime.evaluate: "+captureValue))
	_, err := captureSecret(ctx, rt, "https://a.example.com/", captureOpts(), broken)
	if err == nil {
		t.Fatal("read error must fail")
	}
	noCaptureValue(t, err)

	// Loopback http is allowed, like fill_secret.
	ok, _ := reader("http://127.0.0.1:9000", captureValue, true, nil)
	if _, err := captureSecret(ctx, rt, "http://127.0.0.1:9000/", captureOpts(), ok); err != nil {
		t.Fatalf("loopback: %v", err)
	}
	if len(st.entries) != 1 {
		t.Fatalf("only the loopback capture may be stored, got %d", len(st.entries))
	}
}

// The tab may navigate between the address check and the read. The origin the
// read reports is checked again, so a value from another document is dropped.
func TestCaptureSecret_RefusesAValueReadFromAnotherOrigin(t *testing.T) {
	st := newCaptureStore()
	ctx, rt := captureRuntime(st, true)

	for name, origin := range map[string]string{
		"other host":   "https://evil.example.net",
		"other port":   "https://app.example.com:8443",
		"plain http":   "http://app.example.com",
		"opaque page":  "about:",
		"empty":        "",
		"other scheme": "file://",
	} {
		read, calls := reader(origin, captureValue, true, nil)
		_, err := captureSecret(ctx, rt, "https://app.example.com/login", captureOpts(), read)
		if !errors.Is(err, errCaptureOriginChanged) || *calls != 1 {
			t.Errorf("%s: want errCaptureOriginChanged after one read, got %v (calls=%d)", name, err, *calls)
		}
		noCaptureValue(t, err)
	}
	if len(st.entries) != 0 {
		t.Fatal("a value from another origin must not be stored")
	}
	if !strings.Contains(st.outcomes(), "agent_capture_origin_changed") {
		t.Fatalf("refusal not audited: %s", st.outcomes())
	}
}

// Against a real Chrome: the address was checked on one origin, then the tab
// moved to another origin whose storage holds the key. The real read reports
// the second origin and the capture is refused.
func TestCaptureSecret_RealPageThatNavigatedAwayIsRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a real headless Chrome")
	}
	bin, ok := launcher.LookPath()
	if !ok {
		t.Skip("no Chrome or Chromium installed")
	}
	serve := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, body)
		}))
	}
	checked := serve(`<!doctype html><title>checked</title><script>localStorage.setItem("access_token", "` + captureValue + `")</script>`)
	defer checked.Close()
	other := serve(`<!doctype html><title>other</title><script>localStorage.setItem("access_token", "` + captureValue + `")</script>`)
	defer other.Close()

	// Without the leakless helper, like the manager's fallback: endpoint
	// security may block that unsigned exe.
	l := launcher.New().Bin(bin).Headless(true).Leakless(false)
	u, err := l.Launch()
	if err != nil {
		if dir := l.Get(flags.UserDataDir); dir != "" {
			_ = os.RemoveAll(dir)
		}
		t.Skipf("Chrome cannot be started here: %v", err)
	}
	defer func() { l.Kill(); l.Cleanup() }()
	b := rod.New().ControlURL(u)
	if err := b.Connect(); err != nil {
		t.Skipf("Chrome cannot be reached: %v", err)
	}
	defer b.Close()
	p, err := b.Page(proto.TargetCreateTarget{URL: checked.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WaitLoad(); err != nil {
		t.Fatal(err)
	}
	info, err := p.Info()
	if err != nil {
		t.Fatal(err)
	}
	checkedURL := info.URL
	if err := p.Navigate(other.URL); err != nil {
		t.Fatal(err)
	}
	if err := p.WaitLoad(); err != nil {
		t.Fatal(err)
	}

	st := newCaptureStore()
	ctx, rt := captureRuntime(st, true)
	_, err = captureSecret(ctx, rt, checkedURL, captureOpts(), func() (pageRead, error) {
		return readPageSecret(p, checkedURL, captureOpts())
	})
	if !errors.Is(err, errCaptureOriginChanged) {
		t.Fatalf("want errCaptureOriginChanged, got %v", err)
	}
	noCaptureValue(t, err)
	if len(st.entries) != 0 {
		t.Fatal("nothing may be stored")
	}

	// Control: the address checked is the one read from, so it is stored.
	info, err = p.Info()
	if err != nil {
		t.Fatal(err)
	}
	status, err := captureSecret(ctx, rt, info.URL, captureOpts(), func() (pageRead, error) {
		return readPageSecret(p, info.URL, captureOpts())
	})
	if err != nil || status != secretvault.WritePending || st.values["app-jwt"].Secret != captureValue {
		t.Fatalf("same-origin capture: status=%q err=%v", status, err)
	}
}

// Lite has no surface to approve agent-written secrets, so capture refuses
// there before touching the page.
func TestCaptureSecret_RefusedInTheLiteEdition(t *testing.T) {
	prev := edition.Current()
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(prev) })

	st := newCaptureStore()
	ctx, _ := captureRuntime(st, true)
	if _, err := New().CaptureSecret(ctx, "", captureOpts()); !errors.Is(err, secretvault.ErrWriteUnsupported) {
		t.Fatalf("want ErrWriteUnsupported, got %v", err)
	}
	if len(st.entries) != 0 || !strings.Contains(st.outcomes(), "agent_write_unsupported_edition") {
		t.Fatalf("entries=%d audit=%s", len(st.entries), st.outcomes())
	}
}

func TestCaptureSecret_ToolSchemaListsTheAct(t *testing.T) {
	tool := NewBrowserTool(New())
	if !strings.Contains(tool.Description(), "capture_secret") {
		t.Fatal("description must document capture_secret")
	}
	req := tool.Parameters()["properties"].(map[string]any)["request"].(map[string]any)["properties"].(map[string]any)
	kinds := req["kind"].(map[string]any)["enum"].([]string)
	if !strings.Contains(strings.Join(kinds, ","), "capture_secret") {
		t.Fatalf("act enum %v", kinds)
	}
	for _, p := range []string{"source", "secret_name", "secret_kind"} {
		if _, ok := req[p]; !ok {
			t.Errorf("missing request property %s", p)
		}
	}
}
