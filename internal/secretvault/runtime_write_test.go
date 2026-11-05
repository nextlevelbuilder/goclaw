package secretvault

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const writeSecret = "agent-written-token-5c1e8b2d"

// writeStore adds the agent-write behaviour of the real stores to formStore:
// agent-created entries are pending, the pending cap applies, Approve
// activates.
type writeStore struct {
	*formStore
	approved map[uuid.UUID][]string
	limit    int
}

func newWriteStore() *writeStore {
	return &writeStore{formStore: newFormStore(), approved: map[uuid.UUID][]string{}, limit: store.SecretVaultMaxPendingPerAgent}
}

func (w *writeStore) Create(ctx context.Context, e *store.SecretVaultEntry, p store.SecretVaultPayload) error {
	if e.CreatedByAgent != nil {
		w.mu.Lock()
		pending := 0
		for _, x := range w.entries {
			if x.Status == store.SecretVaultStatusPending && x.CreatedByAgent != nil && *x.CreatedByAgent == *e.CreatedByAgent {
				pending++
			}
		}
		w.mu.Unlock()
		if pending >= w.limit {
			return store.ErrSecretVaultPendingLimit
		}
		e.Status, e.CreatedVia = store.SecretVaultStatusPending, store.SecretVaultCreatedViaAgent
	}
	return w.formStore.Create(ctx, e, p)
}

func (w *writeStore) Approve(_ context.Context, id uuid.UUID, domains []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, e := range w.entries {
		if e.ID == id {
			e.Status = store.SecretVaultStatusActive
			w.approved[id] = domains
			return nil
		}
	}
	return store.ErrSecretVaultNotFound
}

// ResolveForUse never returns agent-written entries here: pending ones are not
// resolvable and these tests only resolve session secrets.
func (w *writeStore) ResolveForUse(context.Context, uuid.UUID, string) (*store.SecretVaultEntry, store.SecretVaultPayload, error) {
	return nil, store.SecretVaultPayload{}, store.ErrSecretVaultNotFound
}

func (w *writeStore) entry(name string) *store.SecretVaultEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.entries[name]
}

func (w *writeStore) auditOutcomes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, a := range w.audit {
		out = append(out, a.Action+":"+a.Outcome)
	}
	return out
}

func writeRuntime(h *formHarness, st store.SecretVaultStore, allow bool) *Runtime {
	rt := NewRuntime(st, uuid.New(), h.user)
	rt.AllowWrite = allow
	rt.SessionKey = "sess1"
	return rt
}

func apiSecret(name string) NewSecret {
	return NewSecret{Name: name, Kind: store.SecretKindAPIKey, Domains: []string{"app.example.com"}}
}

func noValueIn(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), writeSecret) {
		t.Fatalf("error leaks the value: %v", err)
	}
}

// answerApproval waits for the approval event and answers it.
func answerApproval(t *testing.T, h *formHarness, approve bool) map[string]any {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-h.events:
			if ev.name != protocol.EventSecretVaultFormRequested {
				continue
			}
			if ev.payload["kind"] != KindApproval {
				t.Fatalf("want an approval, got %v", ev.payload["kind"])
			}
			if err := h.fm.Respond(h.tenant, h.user, ev.payload["id"].(string), approve); err != nil {
				t.Fatal(err)
			}
			return ev.payload
		case <-deadline:
			t.Fatal("no approval event")
		}
	}
}

func TestWrite_DisabledAgentIsRefusedAndAudited(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, false)
	rt.Forms = h.fm

	_, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret})
	if !errors.Is(err, ErrWriteDisabled) {
		t.Fatalf("create: %v", err)
	}
	if err := rt.SetSessionSecret(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); !errors.Is(err, ErrWriteDisabled) {
		t.Fatalf("session: %v", err)
	}
	if err := rt.GrantToAgent(h.ctx, "tok", "other"); !errors.Is(err, ErrWriteDisabled) {
		t.Fatalf("grant: %v", err)
	}
	if st.entry("tok") != nil {
		t.Fatal("nothing may be stored")
	}
	if got := st.auditOutcomes(); len(got) != 3 || !strings.HasPrefix(got[0], "denied:") {
		t.Fatalf("refusals must be audited, got %v", got)
	}
	// The value is still hidden from any later output of the run.
	if strings.Contains(rt.Redactor.Redact("x "+writeSecret), writeSecret) {
		t.Fatal("refused value must still be redacted")
	}
}

func TestCreatePending_WithoutUIStaysPendingAndIsGrantedToTheAgent(t *testing.T) {
	h := newHarness(t, false)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	status, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret})
	if err != nil || status != WritePending {
		t.Fatalf("status=%q err=%v", status, err)
	}
	e := st.entry("tok")
	if e == nil || e.Status != store.SecretVaultStatusPending || e.CreatedByAgent == nil || *e.CreatedByAgent != rt.AgentID {
		t.Fatalf("entry %+v", e)
	}
	if grants, _ := st.ListGrants(h.ctx, e.ID); !slices.Equal(grants, []uuid.UUID{rt.AgentID}) {
		t.Fatalf("grants %v", grants)
	}
	if strings.Contains(rt.Redactor.Redact(writeSecret), writeSecret) {
		t.Fatal("redactor must know the value at once")
	}
	if len(st.approved) != 0 {
		t.Fatal("must not be approved without the user")
	}
}

func TestCreatePending_LiveApprovalActivatesWithTheDomains(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	done := make(chan string, 1)
	go func() {
		s, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret})
		noValueIn(t, err)
		done <- s
	}()
	ev := answerApproval(t, h, true)
	for k, v := range ev {
		if s, ok := v.(string); ok && strings.Contains(s, writeSecret) {
			t.Fatalf("approval event field %s leaks the value", k)
		}
	}
	if s := <-done; s != WriteActive {
		t.Fatalf("status %q", s)
	}
	if got := st.approved[st.entry("tok").ID]; !slices.Equal(got, []string{"app.example.com"}) {
		t.Fatalf("approved domains %v", got)
	}

	// A no leaves the next one pending.
	go func() {
		s, _ := rt.CreatePending(h.ctx, apiSecret("tok2"), store.SecretVaultPayload{Secret: writeSecret + "2"})
		done <- s
	}()
	answerApproval(t, h, false)
	if s := <-done; s != WritePending {
		t.Fatalf("denied: status %q", s)
	}
}

func TestCreatePending_RejectsTakenNameLimitSizeAndBadInput(t *testing.T) {
	h := newHarness(t, false)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	p := store.SecretVaultPayload{Secret: writeSecret}

	if _, err := rt.CreatePending(h.ctx, apiSecret("tok"), p); err != nil {
		t.Fatal(err)
	}
	_, err := rt.CreatePending(h.ctx, apiSecret("tok"), p)
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}
	noValueIn(t, err)

	_, err = rt.CreatePending(h.ctx, apiSecret("big"), store.SecretVaultPayload{Secret: strings.Repeat("a", MaxWriteValueBytes+1)})
	if !errors.Is(err, ErrValueTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	for name, ns := range map[string]NewSecret{
		"bad name":   {Name: "Bad Name!", Domains: []string{"a.example.com"}},
		"no domains": {Name: "ok-name"},
		"bad kind":   {Name: "ok-name", Kind: "cookie", Domains: []string{"a.example.com"}},
	} {
		_, err := rt.CreatePending(h.ctx, ns, p)
		if err == nil {
			t.Errorf("%s accepted", name)
		}
		noValueIn(t, err)
	}

	st.limit = 1
	if _, err := rt.CreatePending(h.ctx, apiSecret("tok3"), p); !errors.Is(err, ErrPendingLimit) {
		t.Fatalf("limit: %v", err)
	}
	for _, o := range []string{"denied:agent_name_taken", "denied:agent_value_too_large", "denied:agent_invalid", "denied:agent_pending_limit"} {
		if !slices.Contains(st.auditOutcomes(), o) {
			t.Errorf("missing audit %s in %v", o, st.auditOutcomes())
		}
	}
}

func TestSetSessionSecret_StaysInMemoryAndIsUsable(t *testing.T) {
	h := newHarness(t, false)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	if err := rt.SetSessionSecret(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); err != nil {
		t.Fatal(err)
	}
	if st.entry("tok") != nil {
		t.Fatal("a session secret must not reach the store")
	}
	sec, err := rt.Use(h.ctx, "tok", store.SecretKindAPIKey, "app.example.com")
	if err != nil || sec.Payload.Secret != writeSecret {
		t.Fatalf("use: %v", err)
	}
	if err := rt.SetSessionSecret(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: "other-value-1234"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("session name reuse: %v", err)
	}
	if _, err := rt.CreatePending(h.ctx, apiSecret("vaulted"), store.SecretVaultPayload{Secret: "v-1234567"}); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetSessionSecret(h.ctx, apiSecret("vaulted"), store.SecretVaultPayload{Secret: "v-7654321"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("vault name reuse: %v", err)
	}

	rt.Forms = nil
	if err := rt.SetSessionSecret(h.ctx, apiSecret("tok9"), store.SecretVaultPayload{Secret: writeSecret}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session: %v", err)
	}
}

func TestGrantToAgent_OwnerOnlyAndAlwaysAsksTheUser(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm
	other := uuid.New()
	rt.Agents = func(_ context.Context, key string) (uuid.UUID, error) {
		if key == "helper" {
			return other, nil
		}
		return uuid.Nil, errors.New("agent not found")
	}
	// Created by the user, not the agent.
	if err := st.Create(h.ctx, &store.SecretVaultEntry{Name: "user-tok", Kind: store.SecretKindAPIKey, AllowedDomains: []string{"a.example.com"}}, store.SecretVaultPayload{Secret: "u-123456"}); err != nil {
		t.Fatal(err)
	}
	if err := rt.GrantToAgent(h.ctx, "user-tok", "helper"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("not owner: %v", err)
	}
	if err := rt.GrantToAgent(h.ctx, "missing", "helper"); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("missing must look like not owner: %v", err)
	}

	rt.Forms = nil // create without asking
	if _, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); err != nil {
		t.Fatal(err)
	}
	id := st.entry("tok").ID
	if err := rt.GrantToAgent(h.ctx, "tok", "nobody"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("unknown agent: %v", err)
	}
	if err := rt.GrantToAgent(h.ctx, "tok", "helper"); !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("no UI must fail closed: %v", err)
	}

	rt.Forms = h.fm
	errc := make(chan error, 1)
	go func() { errc <- rt.GrantToAgent(h.ctx, "tok", "helper") }()
	answerApproval(t, h, false)
	if err := <-errc; !errors.Is(err, ErrGrantDenied) {
		t.Fatalf("denied: %v", err)
	}
	if grants, _ := st.ListGrants(h.ctx, id); slices.Contains(grants, other) {
		t.Fatal("denied grant was applied")
	}

	go func() { errc <- rt.GrantToAgent(h.ctx, "tok", "helper") }()
	answerApproval(t, h, true)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if grants, _ := st.ListGrants(h.ctx, id); !slices.Contains(grants, other) || !slices.Contains(grants, rt.AgentID) {
		t.Fatalf("grants %v", grants)
	}
	if !slices.Contains(st.auditOutcomes(), "approved:granted_to:helper") {
		t.Fatalf("grant not audited: %v", st.auditOutcomes())
	}
}
