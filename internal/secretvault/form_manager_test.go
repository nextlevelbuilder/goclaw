package secretvault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const formSecret = "sk-form-secret-9876"

// formStore is a writable in-memory vault store for the save scope.
type formStore struct {
	store.SecretVaultStore
	mu      sync.Mutex
	entries map[string]*store.SecretVaultEntry
	values  map[uuid.UUID]store.SecretVaultPayload
	grants  map[uuid.UUID][]uuid.UUID
	audit   []store.SecretVaultAuditEntry
}

func newFormStore() *formStore {
	return &formStore{
		entries: map[string]*store.SecretVaultEntry{},
		values:  map[uuid.UUID]store.SecretVaultPayload{},
		grants:  map[uuid.UUID][]uuid.UUID{},
	}
}

func (f *formStore) Create(_ context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.entries[e.Name]; ok {
		return store.ErrSecretVaultNameTaken
	}
	e.ID = uuid.New()
	cp := *e
	f.entries[e.Name] = &cp
	f.values[e.ID] = p
	return nil
}

func (f *formStore) ReplaceValue(_ context.Context, id uuid.UUID, p store.SecretVaultPayload, _ int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[id] = p
	return 2, nil
}

func (f *formStore) List(context.Context) ([]store.SecretVaultEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.SecretVaultEntry
	for _, e := range f.entries {
		out = append(out, *e)
	}
	return out, nil
}

func (f *formStore) ListGrants(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.grants[id]...), nil
}

func (f *formStore) SetGrants(_ context.Context, id uuid.UUID, agents []uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[id] = agents
	return nil
}

func (f *formStore) AddGrant(_ context.Context, id, agent uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.grants[id], agent) {
		f.grants[id] = append(f.grants[id], agent)
	}
	return nil
}

func (f *formStore) AppendAudit(_ context.Context, a *store.SecretVaultAuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.audit = append(f.audit, *a)
	return nil
}

type published struct {
	name    string
	tenant  uuid.UUID
	payload map[string]any
}

type formHarness struct {
	fm     *FormManager
	events chan published
	tenant uuid.UUID
	user   string
	ctx    context.Context
}

func newHarness(t *testing.T, connected bool) *formHarness {
	t.Helper()
	h := &formHarness{fm: NewFormManager(), events: make(chan published, 16), tenant: uuid.New(), user: "user1"}
	h.fm.Configure(func(name string, tenant uuid.UUID, payload map[string]any) {
		h.events <- published{name, tenant, payload}
	}, func(tenant uuid.UUID, user string) bool { return connected && tenant == h.tenant && user == h.user })
	h.ctx = store.WithUserID(store.WithTenantID(context.Background(), h.tenant), h.user)
	return h
}

// ask runs RequestForm in a goroutine and returns its pending id once the
// request event is published.
func (h *formHarness) ask(t *testing.T, spec FormSpec) (id string, result <-chan FormResult) {
	t.Helper()
	out := make(chan FormResult, 1)
	go func() {
		res, err := h.fm.RequestForm(h.ctx, spec)
		if err != nil {
			res = FormResult{Status: "error:" + err.Error()}
		}
		out <- res
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev.name == protocol.EventSecretVaultFormRequested {
				return ev.payload["id"].(string), out
			}
		case <-deadline:
			t.Fatal("no request event")
		}
	}
}

func (h *formHarness) wait(t *testing.T, ch <-chan FormResult) FormResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("request did not finish")
	}
	return FormResult{}
}

func apiSpec(st store.SecretVaultStore, agent uuid.UUID, onSecret func(store.SecretVaultPayload)) FormSpec {
	return FormSpec{
		Kind: store.SecretKindAPIKey, Name: "gh", Domains: []string{"api.github.com"}, Reason: "deploy",
		AgentID: agent, AgentKey: "bot", SessionKey: "sess1", Store: st, OnSecret: onSecret,
	}
}

func TestSecretForm_SaveEncryptsGrantsAndNeverReturnsValue(t *testing.T) {
	h := newHarness(t, true)
	st, agent := newFormStore(), uuid.New()
	var seen store.SecretVaultPayload
	id, res := h.ask(t, apiSpec(st, agent, func(p store.SecretVaultPayload) { seen = p }))

	err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Secret: formSecret})
	if err != nil {
		t.Fatal(err)
	}
	got := h.wait(t, res)
	if got.Status != StatusSubmitted || got.SecretName != "gh" {
		t.Fatalf("result %+v", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", got), formSecret) {
		t.Fatal("result leaks the value")
	}
	e := st.entries["gh"]
	if e == nil || e.Kind != store.SecretKindAPIKey || e.RequireApproval || e.CreatedBy != h.user {
		t.Fatalf("entry %+v", e)
	}
	if st.values[e.ID].Secret != formSecret || len(st.grants[e.ID]) != 1 || st.grants[e.ID][0] != agent {
		t.Fatal("value or grant not stored")
	}
	if seen.Secret != formSecret {
		t.Fatal("OnSecret must receive the value before the result is delivered")
	}
	if len(st.audit) != 1 || st.audit[0].Action != store.SecretAuditCreated {
		t.Fatalf("audit %+v", st.audit)
	}

	// Events never contain the value; the resolved event carries the owner.
	drain := func() {
		for {
			select {
			case ev := <-h.events:
				if strings.Contains(fmt.Sprint(ev.payload), formSecret) {
					t.Fatalf("event %s leaks the value", ev.name)
				}
				if ev.tenant != h.tenant || ev.payload["userId"] != h.user {
					t.Fatalf("event %s not scoped to tenant/user: %+v", ev.name, ev)
				}
			default:
				return
			}
		}
	}
	drain()
}

func TestSecretForm_LoginSaveRequiresApprovalByDefault(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	spec := apiSpec(st, uuid.New(), nil)
	spec.Kind, spec.Name = store.SecretKindLogin, "bank"
	id, res := h.ask(t, spec)
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Username: "alice", Secret: "pw-12345"}); err != nil {
		t.Fatal(err)
	}
	h.wait(t, res)
	if !st.entries["bank"].RequireApproval {
		t.Fatal("login must default to require_approval")
	}
}

func TestSecretForm_SessionScopeStaysOutOfStore(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	id, res := h.ask(t, apiSpec(st, uuid.New(), nil))
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSession, store.SecretVaultPayload{Secret: formSecret}); err != nil {
		t.Fatal(err)
	}
	h.wait(t, res)
	if len(st.entries) != 0 || len(st.audit) != 0 {
		t.Fatal("session scope must not touch the store")
	}
	_, p, ok := h.fm.Sessions.Get(h.tenant, "sess1", "gh")
	if !ok || p.Secret != formSecret {
		t.Fatal("session secret missing")
	}
	if _, _, ok := h.fm.Sessions.Get(h.tenant, "other-session", "gh"); ok {
		t.Fatal("session secret leaked to another session")
	}
	if _, _, ok := h.fm.Sessions.Get(uuid.New(), "sess1", "gh"); ok {
		t.Fatal("session secret leaked to another tenant")
	}
}

func TestSecretForm_WrongTenantUserSessionOrKindIsNotFoundAndKeepsRequestOpen(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	id, res := h.ask(t, apiSpec(st, uuid.New(), nil))
	pay := store.SecretVaultPayload{Secret: formSecret}

	cases := map[string]error{
		"tenant":   h.fm.Submit(h.ctx, uuid.New(), h.user, id, "sess1", ScopeSave, pay),
		"user":     h.fm.Submit(h.ctx, h.tenant, "mallory", id, "sess1", ScopeSave, pay),
		"session":  h.fm.Submit(h.ctx, h.tenant, h.user, id, "other", ScopeSave, pay),
		"unknown":  h.fm.Submit(h.ctx, h.tenant, h.user, uuid.NewString(), "sess1", ScopeSave, pay),
		"approval": h.fm.Respond(h.tenant, h.user, id, true, permissions.RoleOperator),
		"cancel":   h.fm.Cancel(h.tenant, "mallory", id),
	}
	for name, err := range cases {
		if !errors.Is(err, ErrRequestNotFound) {
			t.Errorf("%s: want not found, got %v", name, err)
		}
	}
	if len(st.entries) != 0 {
		t.Fatal("a rejected submit must not store anything")
	}
	// The legitimate owner can still answer.
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, pay); err != nil {
		t.Fatal(err)
	}
	h.wait(t, res)
}

func TestSecretForm_DoubleSubmitAndBadScope(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	id, res := h.ask(t, apiSpec(st, uuid.New(), nil))
	pay := store.SecretVaultPayload{Secret: formSecret}

	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", "forever", pay); !errors.Is(err, ErrScopeInvalid) {
		t.Fatalf("scope: %v", err)
	}
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, pay); err != nil {
		t.Fatal(err)
	}
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, pay); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("double submit: %v", err)
	}
	h.wait(t, res)
}

func TestSecretForm_InvalidPayloadKeepsRequestOpen(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	spec := apiSpec(st, uuid.New(), nil)
	spec.Kind, spec.Name = store.SecretKindLogin, "bank"
	id, res := h.ask(t, spec)

	err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Secret: "only-password"})
	if !errors.Is(err, store.ErrSecretVaultInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Username: "a", Secret: "pw-12345"}); err != nil {
		t.Fatalf("retry after validation error: %v", err)
	}
	h.wait(t, res)
}

func TestSecretForm_ExistingNameReplacesValueAndAddsGrant(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	first, second := uuid.New(), uuid.New()

	id, res := h.ask(t, apiSpec(st, first, nil))
	_ = h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Secret: "old-value-1"})
	h.wait(t, res)

	id, res = h.ask(t, apiSpec(st, second, nil))
	if err := h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSave, store.SecretVaultPayload{Secret: "new-value-2"}); err != nil {
		t.Fatal(err)
	}
	h.wait(t, res)
	e := st.entries["gh"]
	if st.values[e.ID].Secret != "new-value-2" || len(st.grants[e.ID]) != 2 {
		t.Fatalf("value=%q grants=%d", st.values[e.ID].Secret, len(st.grants[e.ID]))
	}
}

func TestSecretForm_CancelTimeoutAndCtx(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()

	id, res := h.ask(t, apiSpec(st, uuid.New(), nil))
	if err := h.fm.Cancel(h.tenant, h.user, id); err != nil {
		t.Fatal(err)
	}
	if got := h.wait(t, res); got.Status != StatusCancelled {
		t.Fatalf("cancel: %+v", got)
	}

	h.fm.FormTimeout = 30 * time.Millisecond
	_, res = h.ask(t, apiSpec(st, uuid.New(), nil))
	if got := h.wait(t, res); got.Status != StatusTimeout {
		t.Fatalf("timeout: %+v", got)
	}
	if len(h.fm.pending) != 0 {
		t.Fatal("pending request must be cleaned up")
	}
}

func TestSecretForm_FailsFastWithoutWebUI(t *testing.T) {
	h := newHarness(t, false)
	start := time.Now()
	_, err := h.fm.RequestForm(h.ctx, apiSpec(newFormStore(), uuid.New(), nil))
	if !errors.Is(err, ErrNoUI) || time.Since(start) > time.Second {
		t.Fatalf("want immediate ErrNoUI, got %v after %s", err, time.Since(start))
	}
	// Unconfigured manager and a context without tenant/user also fail fast.
	if _, err := NewFormManager().RequestForm(h.ctx, apiSpec(nil, uuid.New(), nil)); !errors.Is(err, ErrNoUI) {
		t.Fatalf("unconfigured: %v", err)
	}
	if _, err := newHarness(t, true).fm.RequestForm(context.Background(), apiSpec(nil, uuid.New(), nil)); !errors.Is(err, ErrNoUI) {
		t.Fatalf("no identity: %v", err)
	}
}

func TestSecretApproval_GatesUseAndCachesPerHost(t *testing.T) {
	h := newHarness(t, true)
	f := newFake()
	e := f.entries["gh"]
	e.Entry.RequireApproval = true
	f.entries["gh"] = e

	rt := NewRuntime(f, uuid.New(), h.user)
	rt.Forms, rt.SessionKey = h.fm, "sess1"
	ctx := WithRuntime(h.ctx, rt)

	type outcome struct {
		sec *Secret
		err error
	}
	use := func() chan outcome {
		ch := make(chan outcome, 1)
		go func() { s, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "api.github.com"); ch <- outcome{s, err} }()
		return ch
	}

	ch := use()
	ev := <-h.events
	if ev.payload["kind"] != KindApproval || ev.payload["host"] != "api.github.com" || ev.payload["name"] != "gh" {
		t.Fatalf("approval event %+v", ev.payload)
	}
	if strings.Contains(fmt.Sprint(ev.payload), "ghp_SuperSecretToken") {
		t.Fatal("approval event leaks the value")
	}
	if err := h.fm.Respond(h.tenant, h.user, ev.payload["id"].(string), true, permissions.RoleOperator); err != nil {
		t.Fatal(err)
	}
	if o := <-ch; o.err != nil || o.sec == nil {
		t.Fatalf("approved use failed: %v", o.err)
	}
	<-h.events // resolved

	// Second use for the same host in this run does not ask again.
	if s, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "api.github.com"); err != nil || s == nil {
		t.Fatalf("cached approval: %v", err)
	}
	var actions []string
	for _, a := range f.audit {
		actions = append(actions, a.Action)
	}
	if strings.Join(actions, ",") != "approved,used,used" {
		t.Fatalf("audit %v", actions)
	}
}

func TestSecretApproval_DeniedTimeoutAndNoUIFailClosed(t *testing.T) {
	h := newHarness(t, true)
	f := newFake()
	e := f.entries["gh"]
	e.Entry.RequireApproval = true
	f.entries["gh"] = e
	rt := NewRuntime(f, uuid.New(), h.user)
	rt.Forms, rt.SessionKey = h.fm, "sess1"
	ctx := WithRuntime(h.ctx, rt)

	ch := make(chan error, 1)
	go func() { _, err := rt.Use(ctx, "gh", "", "api.github.com"); ch <- err }()
	ev := <-h.events
	_ = h.fm.Respond(h.tenant, h.user, ev.payload["id"].(string), false, permissions.RoleOperator)
	if err := <-ch; !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("denied: %v", err)
	}

	h.fm.ApprovalTimeout = 30 * time.Millisecond
	if _, err := rt.Use(ctx, "gh", "", "api.github.com"); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("timeout: %v", err)
	}

	noForms := NewRuntime(f, uuid.New(), h.user)
	if _, err := noForms.Use(WithRuntime(h.ctx, noForms), "gh", "", "api.github.com"); !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("no forms: %v", err)
	}
	for _, a := range f.audit {
		if a.Action == store.SecretAuditUsed {
			t.Fatal("a refused approval must never be audited as used")
		}
	}
}

func TestSecretForm_SessionSecretIsUsableAndRedacted(t *testing.T) {
	h := newHarness(t, true)
	st := newFormStore()
	id, res := h.ask(t, apiSpec(st, uuid.New(), nil))
	_ = h.fm.Submit(h.ctx, h.tenant, h.user, id, "sess1", ScopeSession, store.SecretVaultPayload{Secret: formSecret})
	h.wait(t, res)

	f := &fakeStore{entries: map[string]store.SecretVaultResolved{}, granted: map[string]bool{}}
	rt := NewRuntime(f, uuid.New(), h.user)
	rt.Forms, rt.SessionKey = h.fm, "sess1"
	ctx := WithRuntime(h.ctx, rt)
	if err := rt.Preload(ctx); err != nil {
		t.Fatal(err)
	}
	if got := rt.Redactor.Redact("x " + formSecret); strings.Contains(got, formSecret) {
		t.Fatalf("session secret not redacted after Preload: %q", got)
	}
	s, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "api.github.com")
	if err != nil || s.Payload.Secret != formSecret {
		t.Fatalf("use session secret: %v", err)
	}
	if _, err := rt.Use(ctx, "gh", store.SecretKindAPIKey, "evil.test"); !errors.Is(err, ErrDomainNotAllowed) {
		t.Fatalf("host binding must apply to session secrets: %v", err)
	}
	other := NewRuntime(f, uuid.New(), h.user)
	other.Forms, other.SessionKey = h.fm, "another-session"
	if _, err := other.Use(WithRuntime(h.ctx, other), "gh", "", "api.github.com"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("another session must not see it: %v", err)
	}
}

func TestSessionStore_ExpiresDropsAndWipes(t *testing.T) {
	s := NewSessionStore(time.Minute)
	now := time.Now()
	s.now = func() time.Time { return now }
	tenant := uuid.New()
	s.Put(tenant, "s", store.SecretVaultEntry{Name: "a"}, store.SecretVaultPayload{Secret: "value-1"})
	s.Put(tenant, "s", store.SecretVaultEntry{Name: "b"}, store.SecretVaultPayload{Secret: "value-2"})

	raw := s.m[sessionKey{tenant, "s", "a"}]
	buf := raw.secret
	s.Drop(tenant, "s")
	for _, c := range buf {
		if c != 0 {
			t.Fatal("dropped secret was not zeroed")
		}
	}

	s.Put(tenant, "s", store.SecretVaultEntry{Name: "c"}, store.SecretVaultPayload{Secret: "value-3"})
	buf = s.m[sessionKey{tenant, "s", "c"}].secret
	now = now.Add(2 * time.Minute)
	if _, _, ok := s.Get(tenant, "s", "c"); ok {
		t.Fatal("expired secret returned")
	}
	for _, c := range buf {
		if c != 0 {
			t.Fatal("expired secret was not zeroed")
		}
	}
}
