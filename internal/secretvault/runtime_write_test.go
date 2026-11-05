package secretvault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
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
	// approvedElsewhere makes Approve report that an admin already
	// activated the entry.
	approvedElsewhere bool
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
	if w.approvedElsewhere {
		return store.ErrSecretVaultNotPending
	}
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

// writeRuntime builds a runtime whose user is an admin of the harness tenant.
func writeRuntime(h *formHarness, st store.SecretVaultStore, allow bool) *Runtime {
	rt := NewRuntime(st, uuid.New(), h.user)
	rt.AllowWrite = allow
	rt.SessionKey = "sess1"
	rt.TenantRoles = tenantRoles(h, store.TenantRoleAdmin)
	return rt
}

// tenantRoles reports role for the harness user in the harness tenant only.
func tenantRoles(h *formHarness, role string) TenantRoleLookup {
	return func(_ context.Context, tenant uuid.UUID, user string) (string, error) {
		if tenant == h.tenant && user == h.user {
			return role, nil
		}
		return "", nil
	}
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

// answerApproval waits for the approval event and answers it from an admin
// client.
func answerApproval(t *testing.T, h *formHarness, approve bool) map[string]any {
	t.Helper()
	return answerApprovalAs(t, h, approve, permissions.RoleAdmin, nil)
}

// answerApprovalAs answers the next approval from a client with role; before
// runs between the request and the answer.
func answerApprovalAs(t *testing.T, h *formHarness, approve bool, role permissions.Role, before func()) map[string]any {
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
			if before != nil {
				before()
			}
			if err := h.fm.Respond(h.tenant, h.user, ev.payload["id"].(string), approve, role); err != nil {
				t.Fatal(err)
			}
			return ev.payload
		case <-deadline:
			t.Fatal("no approval event")
		}
	}
}

// noApprovalAsked fails when an approval request was published.
func noApprovalAsked(t *testing.T, h *formHarness) {
	t.Helper()
	for {
		select {
		case ev := <-h.events:
			if ev.name == protocol.EventSecretVaultFormRequested {
				t.Fatal("the user must not be asked")
			}
		default:
			return
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
	for _, o := range []string{"denied:agent_name_taken_pending", "denied:agent_value_too_large", "denied:agent_invalid", "denied:agent_pending_limit"} {
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

// A live yes activates an agent-written entry only with the rule of the REST
// approve route: the user is a tenant owner or admin and answers from a
// client with at least the admin role.
func TestCreatePending_LiveApprovalCountsOnlyFromATenantAdmin(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	// A tenant member is not even asked: the entry waits in the vault page.
	rt.TenantRoles = tenantRoles(h, store.TenantRoleMember)
	if s, err := rt.CreatePending(h.ctx, apiSecret("member"), store.SecretVaultPayload{Secret: writeSecret}); err != nil || s != WritePending {
		t.Fatalf("member: status=%q err=%v", s, err)
	}
	noApprovalAsked(t, h)

	// Without a way to read the tenant role nothing counts (fail closed).
	rt.TenantRoles = nil
	if s, err := rt.CreatePending(h.ctx, apiSecret("unknown"), store.SecretVaultPayload{Secret: writeSecret + "u"}); err != nil || s != WritePending {
		t.Fatalf("no role lookup: status=%q err=%v", s, err)
	}
	noApprovalAsked(t, h)

	// A tenant admin answering from a client below admin does not count.
	rt.TenantRoles = tenantRoles(h, store.TenantRoleAdmin)
	done := make(chan string, 1)
	go func() {
		s, _ := rt.CreatePending(h.ctx, apiSecret("operator"), store.SecretVaultPayload{Secret: writeSecret + "o"})
		done <- s
	}()
	answerApprovalAs(t, h, true, permissions.RoleOperator, nil)
	if s := <-done; s != WritePending {
		t.Fatalf("operator client: status %q", s)
	}
	if len(st.approved) != 0 {
		t.Fatalf("nothing may be approved, got %v", st.approved)
	}
	if !slices.Contains(st.auditOutcomes(), "denied:agent_create_approver_not_admin") {
		t.Fatalf("refused approval not audited: %v", st.auditOutcomes())
	}

	// A tenant owner answering from an owner client counts.
	rt.TenantRoles = tenantRoles(h, store.TenantRoleOwner)
	go func() {
		s, _ := rt.CreatePending(h.ctx, apiSecret("owner"), store.SecretVaultPayload{Secret: writeSecret + "w"})
		done <- s
	}()
	answerApprovalAs(t, h, true, permissions.RoleOwner, nil)
	if s := <-done; s != WriteActive {
		t.Fatalf("owner: status %q", s)
	}
}

// When an admin approves the entry in the vault page while the live question
// is open, the live yes finds it active and reports it so.
func TestCreatePending_ApprovedMeanwhileReportsActive(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	st.approvedElsewhere = true
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	done := make(chan string, 1)
	go func() {
		s, _ := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret})
		done <- s
	}()
	answerApproval(t, h, true)
	if s := <-done; s != WriteActive {
		t.Fatalf("status %q, want active", s)
	}
}

func TestGrantToAgent_NeedsATenantAdminWhoCanReachTheTarget(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	other := uuid.New()
	rt.Agents = func(_ context.Context, key string) (uuid.UUID, error) {
		if key == "helper" {
			return other, nil
		}
		// What the agent loop's lookup answers for an agent the user cannot access.
		return uuid.Nil, ErrAgentNotFound
	}
	if _, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); err != nil {
		t.Fatal(err)
	}
	id := st.entry("tok").ID
	rt.Forms = h.fm

	if err := rt.GrantToAgent(h.ctx, "tok", "not-mine"); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("inaccessible agent: %v", err)
	}
	rt.TenantRoles = tenantRoles(h, store.TenantRoleOperator)
	if err := rt.GrantToAgent(h.ctx, "tok", "helper"); !errors.Is(err, ErrGrantNotAdmin) {
		t.Fatalf("operator user: %v", err)
	}
	noApprovalAsked(t, h)

	rt.TenantRoles = tenantRoles(h, store.TenantRoleAdmin)
	errc := make(chan error, 1)
	go func() { errc <- rt.GrantToAgent(h.ctx, "tok", "helper") }()
	answerApprovalAs(t, h, true, permissions.RoleViewer, nil)
	if err := <-errc; !errors.Is(err, ErrGrantNotAdmin) {
		t.Fatalf("viewer client: %v", err)
	}
	if grants, _ := st.ListGrants(h.ctx, id); slices.Contains(grants, other) {
		t.Fatal("a grant without an admin answer was applied")
	}
	for _, o := range []string{"denied:agent_grant_user_not_tenant_admin", "denied:agent_grant_approver_not_admin"} {
		if !slices.Contains(st.auditOutcomes(), o) {
			t.Errorf("missing audit %s in %v", o, st.auditOutcomes())
		}
	}
}

// The grant set is read before the approval wait. An admin revoking a grant
// meanwhile must not be undone by writing that old set back.
func TestGrantToAgent_KeepsARevokeMadeWhileWaiting(t *testing.T) {
	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	other := uuid.New()
	rt.Agents = func(context.Context, string) (uuid.UUID, error) { return other, nil }
	if _, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); err != nil {
		t.Fatal(err)
	}
	id := st.entry("tok").ID
	rt.Forms = h.fm

	errc := make(chan error, 1)
	go func() { errc <- rt.GrantToAgent(h.ctx, "tok", "helper") }()
	answerApprovalAs(t, h, true, permissions.RoleAdmin, func() {
		// The admin revokes the creating agent's own grant in the vault page.
		if err := st.SetGrants(h.ctx, id, nil); err != nil {
			t.Error(err)
		}
	})
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if grants, _ := st.ListGrants(h.ctx, id); !slices.Equal(grants, []uuid.UUID{other}) {
		t.Fatalf("grants %v, want only the new one", grants)
	}
}

// Every unusable name gets the same answer, so names cannot be probed; the
// audit row keeps the reason.
func TestWrite_TakenNamesShareOneAnswer(t *testing.T) {
	h := newHarness(t, false)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm
	otherAgent := uuid.New()
	for name, e := range map[string]*store.SecretVaultEntry{
		"user-entry":  {Status: store.SecretVaultStatusActive},
		"other-agent": {Status: store.SecretVaultStatusActive, CreatedByAgent: &otherAgent},
		"other-wait":  {Status: store.SecretVaultStatusPending, CreatedByAgent: &otherAgent},
	} {
		e.Name, e.Kind, e.AllowedDomains = name, store.SecretKindAPIKey, []string{"a.example.com"}
		if err := st.formStore.Create(h.ctx, e, store.SecretVaultPayload{Secret: "x-" + name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := rt.SetSessionSecret(h.ctx, apiSecret("in-session"), store.SecretVaultPayload{Secret: writeSecret}); err != nil {
		t.Fatal(err)
	}

	var msgs []string
	for _, name := range []string{"user-entry", "other-agent", "other-wait"} {
		_, err := rt.CreatePending(h.ctx, apiSecret(name), store.SecretVaultPayload{Secret: writeSecret})
		if !errors.Is(err, ErrNameTaken) {
			t.Fatalf("vault %s: %v", name, err)
		}
		msgs = append(msgs, err.Error())
		err = rt.SetSessionSecret(h.ctx, apiSecret(name), store.SecretVaultPayload{Secret: writeSecret})
		if !errors.Is(err, ErrNameTaken) {
			t.Fatalf("session %s: %v", name, err)
		}
		msgs = append(msgs, err.Error())
	}
	err := rt.SetSessionSecret(h.ctx, apiSecret("in-session"), store.SecretVaultPayload{Secret: writeSecret})
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("session name: %v", err)
	}
	msgs = append(msgs, err.Error())
	for _, m := range msgs {
		if m != ErrNameTaken.Error() {
			t.Fatalf("answers differ: %q vs %q", m, ErrNameTaken.Error())
		}
	}
	for _, o := range []string{
		"denied:agent_name_taken_user_entry", "denied:agent_name_taken_other_agent",
		"denied:agent_name_taken_pending", "denied:agent_name_taken_session",
	} {
		if !slices.Contains(st.auditOutcomes(), o) {
			t.Errorf("missing audit %s in %v", o, st.auditOutcomes())
		}
	}
}

func TestSetSessionSecret_CapsSecretsPerSession(t *testing.T) {
	h := newHarness(t, false)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm

	for i := range MaxSessionSecrets {
		if err := rt.SetSessionSecret(h.ctx, apiSecret(fmt.Sprintf("tok-%d", i)), store.SecretVaultPayload{Secret: fmt.Sprintf("%s-%d", writeSecret, i)}); err != nil {
			t.Fatalf("secret %d: %v", i, err)
		}
	}
	err := rt.SetSessionSecret(h.ctx, apiSecret("one-more"), store.SecretVaultPayload{Secret: writeSecret + "-x"})
	if !errors.Is(err, ErrSessionLimit) {
		t.Fatalf("over the cap: %v", err)
	}
	noValueIn(t, err)
	if _, _, ok := h.fm.Sessions.Get(h.tenant, rt.SessionKey, "one-more"); ok {
		t.Fatal("a secret over the cap was kept")
	}
	if !slices.Contains(st.auditOutcomes(), "denied:agent_session_limit") {
		t.Fatalf("refusal not audited: %v", st.auditOutcomes())
	}
	// Another session has its own budget.
	rt.SessionKey = "sess2"
	if err := rt.SetSessionSecret(h.ctx, apiSecret("one-more"), store.SecretVaultPayload{Secret: writeSecret + "-y"}); err != nil {
		t.Fatalf("other session: %v", err)
	}
}

// Lite has no surface to approve agent-written entries, so every agent write
// path refuses there, even for an opted-in agent.
func TestWrite_RefusedInTheLiteEdition(t *testing.T) {
	prev := edition.Current()
	edition.SetCurrent(edition.Lite)
	t.Cleanup(func() { edition.SetCurrent(prev) })

	h := newHarness(t, true)
	st := newWriteStore()
	rt := writeRuntime(h, st, true)
	rt.Forms = h.fm
	rt.Agents = func(context.Context, string) (uuid.UUID, error) { return uuid.New(), nil }

	if AgentWriteSupported() || rt.CanWrite() {
		t.Fatal("agent writes must be unsupported in Lite")
	}
	if err := rt.CheckWrite(h.ctx, "tok"); !errors.Is(err, ErrWriteUnsupported) {
		t.Fatalf("check: %v", err)
	}
	if _, err := rt.CreatePending(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); !errors.Is(err, ErrWriteUnsupported) {
		t.Fatalf("create: %v", err)
	}
	if err := rt.SetSessionSecret(h.ctx, apiSecret("tok"), store.SecretVaultPayload{Secret: writeSecret}); !errors.Is(err, ErrWriteUnsupported) {
		t.Fatalf("session: %v", err)
	}
	if err := rt.GrantToAgent(h.ctx, "tok", "helper"); !errors.Is(err, ErrWriteUnsupported) {
		t.Fatalf("grant: %v", err)
	}
	if st.entry("tok") != nil {
		t.Fatal("nothing may be stored")
	}
	noApprovalAsked(t, h)
	if got := st.auditOutcomes(); len(got) != 4 || got[0] != "denied:agent_write_unsupported_edition" {
		t.Fatalf("refusals must be audited, got %v", got)
	}
}
