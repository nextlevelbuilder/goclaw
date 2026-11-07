package secretvault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Request kinds. "login" and "api_key" mirror the store kinds.
const (
	KindApproval = "approval"
)

// Scopes of a submitted form.
const (
	ScopeSave    = "save"
	ScopeSession = "session"
)

// Result statuses reported to the requesting tool. They never carry a value.
const (
	StatusSubmitted = "submitted"
	StatusCancelled = "cancelled"
	StatusTimeout   = "timeout"
	StatusApproved  = "approved"
	StatusDenied    = "denied"
)

var (
	// ErrNoUI means no Web UI session of the user is connected, so a form or
	// approval cannot be shown. Callers fail fast instead of waiting.
	ErrNoUI = errors.New("secure form needs the Web UI, but no Web UI session of this user is connected")
	// ErrRequestNotFound covers unknown, expired, already answered and
	// foreign requests alike, so ids cannot be probed.
	ErrRequestNotFound = errors.New("secret request not found or already resolved")
	ErrScopeInvalid    = errors.New("secret form scope must be save or session")
)

// Publisher delivers an event to the Web UI clients of one tenant. The
// gateway filters secretVault.* events down to the owning user.
type Publisher func(event string, tenant uuid.UUID, payload map[string]any)

// FormSpec describes a request shown to the user. Store and OnSecret are
// server-side only and never appear in an event.
type FormSpec struct {
	Kind       string // store.SecretKindLogin | store.SecretKindAPIKey | KindApproval
	Name       string
	Domains    []string
	Reason     string
	Hints      string
	Host       string // approval: destination host
	Action     string // approval: what the agent is about to do
	AgentID    uuid.UUID
	AgentKey   string
	SessionKey string

	// Store receives scope=save submissions.
	Store store.SecretVaultStore
	// OnSecret is called with the submitted value before the request result is
	// delivered, so the run's redactor knows it before anything can echo it.
	OnSecret func(store.SecretVaultPayload)
}

// FormResult is all the requesting tool learns. It never holds a value.
type FormResult struct {
	Status     string
	SecretName string
	// ApproverRole is the gateway role of the client that answered an
	// approval (server-side only).
	ApproverRole permissions.Role
}

type pendingRequest struct {
	id      string
	spec    FormSpec
	tenant  uuid.UUID
	user    string
	expires time.Time
	done    chan FormResult
}

// FormManager brokers Secure Forms and approvals between a blocked agent run
// and the Web UI. Unlike the exec approval manager every request has an
// unguessable id and is bound to tenant, user and session.
type FormManager struct {
	// Sessions holds scope=session secrets.
	Sessions *SessionStore
	// FormTimeout and ApprovalTimeout bound how long a run waits.
	FormTimeout     time.Duration
	ApprovalTimeout time.Duration

	mu        sync.Mutex
	pending   map[string]*pendingRequest
	publish   Publisher
	hasClient func(tenant uuid.UUID, user string) bool
}

// NewFormManager builds a manager. publish and hasClient may be set later with
// Configure; until then every request fails with ErrNoUI.
func NewFormManager() *FormManager {
	return &FormManager{
		Sessions:        NewSessionStore(0),
		FormTimeout:     5 * time.Minute,
		ApprovalTimeout: 2 * time.Minute,
		pending:         make(map[string]*pendingRequest),
	}
}

// Configure wires event delivery and the connected-client check.
func (m *FormManager) Configure(publish Publisher, hasClient func(tenant uuid.UUID, user string) bool) {
	m.mu.Lock()
	m.publish, m.hasClient = publish, hasClient
	m.mu.Unlock()
}

var (
	defaultFormsOnce sync.Once
	defaultForms     *FormManager
)

// DefaultForms is the process-wide manager shared by the agent runtime (which
// blocks on it) and the WS handlers (which answer it).
func DefaultForms() *FormManager {
	defaultFormsOnce.Do(func() { defaultForms = NewFormManager() })
	return defaultForms
}

// RequestForm asks the user for a secret and blocks until the form is
// submitted, cancelled, times out, or ctx ends.
func (m *FormManager) RequestForm(ctx context.Context, spec FormSpec) (FormResult, error) {
	if spec.Kind != store.SecretKindLogin && spec.Kind != store.SecretKindAPIKey {
		return FormResult{}, fmt.Errorf("%w: unsupported form kind", store.ErrSecretVaultInvalid)
	}
	return m.request(ctx, spec, m.FormTimeout)
}

// RequestApproval asks the user to approve one use of a secret.
func (m *FormManager) RequestApproval(ctx context.Context, spec FormSpec) (bool, error) {
	res, err := m.requestApproval(ctx, spec)
	if err != nil {
		return false, err
	}
	return res.Status == StatusApproved, nil
}

// requestApproval is RequestApproval with the full result, including the
// role of the client that answered.
func (m *FormManager) requestApproval(ctx context.Context, spec FormSpec) (FormResult, error) {
	spec.Kind = KindApproval
	return m.request(ctx, spec, m.ApprovalTimeout)
}

func (m *FormManager) request(ctx context.Context, spec FormSpec, timeout time.Duration) (FormResult, error) {
	tenant, user := store.TenantIDFromContext(ctx), store.UserIDFromContext(ctx)
	m.mu.Lock()
	publish, hasClient := m.publish, m.hasClient
	m.mu.Unlock()
	if tenant == uuid.Nil || user == "" || publish == nil || hasClient == nil || !hasClient(tenant, user) {
		return FormResult{}, ErrNoUI
	}

	p := &pendingRequest{
		id:      uuid.NewString(),
		spec:    spec,
		tenant:  tenant,
		user:    user,
		expires: time.Now().Add(timeout),
		done:    make(chan FormResult, 1),
	}
	m.mu.Lock()
	m.pending[p.id] = p
	m.mu.Unlock()

	publish(protocol.EventSecretVaultFormRequested, tenant, map[string]any{
		"id":         p.id,
		"kind":       spec.Kind,
		"name":       spec.Name,
		"domains":    spec.Domains,
		"reason":     spec.Reason,
		"hints":      spec.Hints,
		"host":       spec.Host,
		"action":     spec.Action,
		"agentKey":   spec.AgentKey,
		"sessionKey": spec.SessionKey,
		"userId":     user,
		"expiresAt":  p.expires.UnixMilli(),
	})
	slog.Info("secretvault.request", "kind", spec.Kind, "name", spec.Name)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var res FormResult
	select {
	case res = <-p.done:
	case <-timer.C:
		res = FormResult{Status: StatusTimeout, SecretName: spec.Name}
	case <-ctx.Done():
		res = FormResult{Status: StatusCancelled, SecretName: spec.Name}
	}

	m.mu.Lock()
	delete(m.pending, p.id)
	m.mu.Unlock()
	publish(protocol.EventSecretVaultFormResolved, tenant, map[string]any{
		"id": p.id, "status": res.Status, "userId": user,
	})
	slog.Info("secretvault.request.done", "kind", spec.Kind, "name", spec.Name, "status", res.Status)
	return res, nil
}

// claim removes a pending request that matches tenant, user, session and kind,
// so a second answer cannot be applied. Mismatches look like "not found".
func (m *FormManager) claim(id string, tenant uuid.UUID, user, session string, approval bool) (*pendingRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[id]
	if !ok || p.tenant != tenant || p.user != user || (session != "" && p.spec.SessionKey != session) ||
		(p.spec.Kind == KindApproval) != approval || time.Now().After(p.expires) {
		return nil, ErrRequestNotFound
	}
	delete(m.pending, id)
	return p, nil
}

func (m *FormManager) restore(p *pendingRequest) {
	m.mu.Lock()
	if time.Now().Before(p.expires) {
		m.pending[p.id] = p
	}
	m.mu.Unlock()
}

// Submit applies a user's form answer. scope=save stores the secret in the
// vault (encrypted) and grants it to the asking agent; scope=session keeps it
// in memory for this session only. Nothing is returned to the agent but a
// status. On a validation or storage error the request stays open.
func (m *FormManager) Submit(ctx context.Context, tenant uuid.UUID, user, id, session, scope string, payload store.SecretVaultPayload) error {
	if scope != ScopeSave && scope != ScopeSession {
		return ErrScopeInvalid
	}
	p, err := m.claim(id, tenant, user, session, false)
	if err != nil {
		return err
	}
	if err := m.apply(store.WithTenantID(ctx, tenant), p, scope, payload); err != nil {
		m.restore(p)
		return err
	}
	p.done <- FormResult{Status: StatusSubmitted, SecretName: p.spec.Name}
	return nil
}

func (m *FormManager) apply(ctx context.Context, p *pendingRequest, scope string, payload store.SecretVaultPayload) error {
	kind := p.spec.Kind
	if err := store.ValidateSecretVaultPayload(kind, payload); err != nil {
		return err
	}
	domains, err := store.NormalizeSecretDomains(p.spec.Domains)
	if err != nil {
		return err
	}
	entry := store.SecretVaultEntry{
		TenantID:        p.tenant,
		Name:            p.spec.Name,
		Kind:            kind,
		AllowedDomains:  domains,
		RequireApproval: kind == store.SecretKindLogin && scope == ScopeSave,
		Hints:           p.spec.Hints,
		CreatedBy:       p.user,
	}
	if err := store.ValidateSecretVaultEntry(&entry); err != nil {
		return err
	}

	if scope == ScopeSession {
		m.Sessions.Put(p.tenant, p.spec.SessionKey, entry, payload)
	} else if err := m.save(ctx, p, &entry, payload); err != nil {
		return err
	}
	if p.spec.OnSecret != nil {
		p.spec.OnSecret(payload)
	}
	return nil
}

// save creates the vault entry (or replaces the value of an existing one of the
// same name and kind) and grants it to the asking agent.
func (m *FormManager) save(ctx context.Context, p *pendingRequest, entry *store.SecretVaultEntry, payload store.SecretVaultPayload) error {
	st := p.spec.Store
	if st == nil {
		return store.ErrSecretVaultNoKey
	}
	action := store.SecretAuditCreated
	err := st.Create(ctx, entry, payload)
	if errors.Is(err, store.ErrSecretVaultNameTaken) {
		existing, lerr := findByName(ctx, st, entry.Name)
		if lerr != nil {
			return lerr
		}
		if existing.Kind != entry.Kind {
			return fmt.Errorf("%w: %q exists with a different kind", store.ErrSecretVaultInvalid, entry.Name)
		}
		// Pinned to the revision just read: a concurrent edit makes this fail
		// with a conflict instead of silently overwriting it.
		newRev, rerr := st.ReplaceValue(ctx, existing.ID, payload, existing.Revision)
		if rerr != nil {
			return rerr
		}
		existing.Revision = newRev
		*entry = *existing
		action = store.SecretAuditUpdated
	} else if err != nil {
		return err
	}

	if p.spec.AgentID != uuid.Nil {
		grants, gerr := st.ListGrants(ctx, entry.ID)
		if gerr != nil {
			return gerr
		}
		if !slices.Contains(grants, p.spec.AgentID) {
			if gerr = st.SetGrants(ctx, entry.ID, append(grants, p.spec.AgentID)); gerr != nil {
				return gerr
			}
		}
	}
	agent := p.spec.AgentID
	if aerr := st.AppendAudit(ctx, &store.SecretVaultAuditEntry{
		EntryID: entry.ID, EntryName: entry.Name, AgentID: &agent, UserID: p.user,
		Action: action, Outcome: "secure_form",
	}); aerr != nil {
		slog.Warn("secretvault.audit_failed", "name", entry.Name, "action", action)
	}
	return nil
}

func findByName(ctx context.Context, st store.SecretVaultStore, name string) (*store.SecretVaultEntry, error) {
	list, err := st.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i], nil
		}
	}
	return nil, store.ErrSecretVaultNotFound
}

// Respond answers an approval request. role is the gateway role of the
// answering client; approvals that need a tenant admin check it.
func (m *FormManager) Respond(tenant uuid.UUID, user, id string, approve bool, role permissions.Role) error {
	p, err := m.claim(id, tenant, user, "", true)
	if err != nil {
		return err
	}
	status := StatusDenied
	if approve {
		status = StatusApproved
	}
	p.done <- FormResult{Status: status, SecretName: p.spec.Name, ApproverRole: role}
	return nil
}

// Cancel dismisses a form or approval without a value.
func (m *FormManager) Cancel(tenant uuid.UUID, user, id string) error {
	m.mu.Lock()
	p, ok := m.pending[id]
	if !ok || p.tenant != tenant || p.user != user {
		m.mu.Unlock()
		return ErrRequestNotFound
	}
	delete(m.pending, id)
	m.mu.Unlock()
	status := StatusCancelled
	if p.spec.Kind == KindApproval {
		status = StatusDenied
	}
	p.done <- FormResult{Status: status, SecretName: p.spec.Name}
	return nil
}
