package secretvault

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Errors returned to tools. Messages never contain a secret value and do not
// reveal whether a name exists but is not granted.
var (
	ErrNoRuntime           = errors.New("secret vault is not available in this run")
	ErrUnavailable         = errors.New("secret is not available to this agent")
	ErrKindMismatch        = errors.New("secret kind is not allowed for this tool")
	ErrDomainNotAllowed    = errors.New("secret is not allowed for this host")
	ErrInvalidPlaceholder  = errors.New("invalid secret placeholder")
	ErrPlaceholderLocation = errors.New("secret placeholder is not allowed in this field")
	ErrApprovalDenied      = errors.New("the user did not approve using this secret")
)

// Runtime is the per-run vault context: which agent is running, the store to
// resolve from, and the redactor every output sink must pass through.
type Runtime struct {
	Store    store.SecretVaultStore
	AgentID  uuid.UUID
	UserID   string
	Redactor *Redactor

	// Forms brokers Secure Forms and approvals; nil when the run cannot reach
	// a Web UI. SessionKey scopes session-only secrets.
	Forms      *FormManager
	SessionKey string

	// AllowWrite is the agent's vault_agent_write opt-in, read from the agent
	// record by the resolver. Every agent write path checks it via CanWrite.
	AllowWrite bool
	// Agents resolves an agent key to its id in the context tenant, for an
	// agent the context user may access; vault_grant needs it. Nil disables
	// grants.
	Agents AgentLookup
	// TenantRoles returns a user's role in a tenant. A live approval of an
	// agent write counts only from a tenant owner or admin; nil means no live
	// approval counts (entries stay pending, grants are refused).
	TenantRoles TenantRoleLookup

	approvedMu sync.Mutex
	approved   map[string]bool // "name|host" approved earlier in this run
}

// NewRuntime builds a runtime with an empty redactor.
func NewRuntime(s store.SecretVaultStore, agentID uuid.UUID, userID string) *Runtime {
	return &Runtime{Store: s, AgentID: agentID, UserID: userID, Redactor: NewRedactor()}
}

// Preload registers every secret granted to the agent with the redactor, not
// only the ones used so far: an agent can send a secret to an allowed host and
// read it back in a later run when the used-set would be empty.
func (rt *Runtime) Preload(ctx context.Context) error {
	granted, err := rt.Store.ListGrantedSecrets(ctx, rt.AgentID)
	if err != nil {
		return err
	}
	for _, g := range granted {
		rt.Redactor.AddPayload(g.Payload)
	}
	rt.addSessionSecrets(ctx)
	return nil
}

func (rt *Runtime) addSessionSecrets(ctx context.Context) {
	if rt.Forms == nil || rt.SessionKey == "" {
		return
	}
	for _, g := range rt.Forms.Sessions.List(store.TenantIDFromContext(ctx), rt.SessionKey) {
		rt.Redactor.AddPayload(g.Payload)
	}
}

// MarkApproved records that the user already vouched for name in this run
// (for example by typing it into a Secure Form), so it is not asked again.
func (rt *Runtime) MarkApproved(name string) {
	rt.approvedMu.Lock()
	defer rt.approvedMu.Unlock()
	if rt.approved == nil {
		rt.approved = make(map[string]bool)
	}
	rt.approved[name+"|*"] = true
}

func (rt *Runtime) isApproved(name, host string) bool {
	rt.approvedMu.Lock()
	defer rt.approvedMu.Unlock()
	return rt.approved[name+"|*"] || rt.approved[name+"|"+host]
}

func (rt *Runtime) rememberApproval(name, host string) {
	rt.approvedMu.Lock()
	defer rt.approvedMu.Unlock()
	if rt.approved == nil {
		rt.approved = make(map[string]bool)
	}
	rt.approved[name+"|"+host] = true
}

// requireApproval asks the user once per (secret, host) per run. Without a way
// to ask, a secret that needs approval is refused (fail closed).
func (rt *Runtime) requireApproval(ctx context.Context, e *store.SecretVaultEntry, host string) error {
	if rt.isApproved(e.Name, host) {
		return nil
	}
	if rt.Forms == nil {
		rt.audit(ctx, e, store.SecretAuditDenied, host, "approval_unavailable")
		return fmt.Errorf("%w: %q (no Web UI to ask)", ErrApprovalDenied, e.Name)
	}
	ok, err := rt.Forms.RequestApproval(ctx, FormSpec{
		Name: e.Name, Host: host, Action: "use secret " + e.Name + " for " + host,
		AgentID: rt.AgentID, SessionKey: rt.SessionKey,
	})
	if err != nil || !ok {
		rt.audit(ctx, e, store.SecretAuditDenied, host, "approval_denied")
		return fmt.Errorf("%w: %q", ErrApprovalDenied, e.Name)
	}
	if err := rt.appendAudit(ctx, e, store.SecretAuditApproved, host, "ok"); err != nil {
		return fmt.Errorf("secret vault: audit failed for %q", e.Name)
	}
	rt.rememberApproval(e.Name, host)
	return nil
}

// Secret is a resolved entry. Payload holds the value: never log or return it.
type Secret struct {
	Entry   store.SecretVaultEntry
	Payload store.SecretVaultPayload
}

// Use resolves a granted secret for a tool, enforcing kind and host binding and
// writing an audit row. host is the request hostname (no port); pass "" only
// for uses that have no network destination. Success is audited before the
// value is returned and a failed audit write fails the use (fail closed).
func (rt *Runtime) Use(ctx context.Context, name, kind, host string) (*Secret, error) {
	if rt == nil || rt.Store == nil {
		return nil, ErrNoRuntime
	}
	entry, payload, err := rt.Store.ResolveForUse(ctx, rt.AgentID, name)
	switch {
	case errors.Is(err, store.ErrSecretVaultNotFound), errors.Is(err, store.ErrSecretVaultNotGranted):
		se, sp, ok := rt.sessionSecret(ctx, name)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnavailable, name)
		}
		entry, payload = se, sp
	case err != nil:
		return nil, fmt.Errorf("secret vault: resolve %q failed", name)
	}
	rt.Redactor.AddPayload(payload)

	if kind != "" && entry.Kind != kind {
		rt.audit(ctx, entry, store.SecretAuditDenied, host, "kind_mismatch")
		return nil, fmt.Errorf("%w: %q", ErrKindMismatch, name)
	}
	if host != "" && !HostAllowed(host, entry.AllowedDomains) {
		rt.audit(ctx, entry, store.SecretAuditDenied, host, "domain_not_allowed")
		return nil, fmt.Errorf("%w: %q", ErrDomainNotAllowed, name)
	}
	if entry.RequireApproval {
		if err := rt.requireApproval(ctx, entry, host); err != nil {
			return nil, err
		}
	}
	if err := rt.appendAudit(ctx, entry, store.SecretAuditUsed, host, "ok"); err != nil {
		return nil, fmt.Errorf("secret vault: audit failed for %q", name)
	}
	return &Secret{Entry: *entry, Payload: payload}, nil
}

// sessionSecret looks up a secret the user provided for this session only.
func (rt *Runtime) sessionSecret(ctx context.Context, name string) (*store.SecretVaultEntry, store.SecretVaultPayload, bool) {
	if rt.Forms == nil || rt.SessionKey == "" {
		return nil, store.SecretVaultPayload{}, false
	}
	e, p, ok := rt.Forms.Sessions.Get(store.TenantIDFromContext(ctx), rt.SessionKey, name)
	if !ok {
		return nil, store.SecretVaultPayload{}, false
	}
	return &e, p, true
}

func (rt *Runtime) audit(ctx context.Context, e *store.SecretVaultEntry, action, host, outcome string) {
	_ = rt.appendAudit(ctx, e, action, host, outcome) // denial audit is best effort
}

func (rt *Runtime) appendAudit(ctx context.Context, e *store.SecretVaultEntry, action, host, outcome string) error {
	agent := rt.AgentID
	return rt.Store.AppendAudit(ctx, &store.SecretVaultAuditEntry{
		EntryID: e.ID, EntryName: e.Name, AgentID: &agent, UserID: rt.UserID,
		Action: action, TargetHost: host, Outcome: outcome,
	})
}

// HostAllowed reports whether host matches an allowed-domain list. "*.x"
// matches any subdomain of x but not x itself. host must be a bare hostname.
func HostAllowed(host string, allowed []string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	for _, a := range allowed {
		if rest, ok := strings.CutPrefix(a, "*."); ok {
			if strings.HasSuffix(host, "."+rest) && len(host) > len(rest)+1 {
				return true
			}
			continue
		}
		if host == a {
			return true
		}
	}
	return false
}

// MaxWriteValueBytes caps the size of a secret an agent may write.
const MaxWriteValueBytes = 8 * 1024

// Errors of the agent write path. Like the use errors they never carry a value.
var (
	ErrWriteDisabled    = errors.New("writing secrets is not enabled for this agent")
	ErrWriteUnsupported = errors.New("agents cannot save secrets in this edition: it has no place to approve them")
	ErrValueTooLarge    = fmt.Errorf("the secret value exceeds %d bytes", MaxWriteValueBytes)
	// ErrNameTaken is the one answer for every name that cannot be used
	// (another agent's, a pending one, one in this session...), so names
	// cannot be probed. The audit row keeps the reason.
	ErrNameTaken     = errors.New("this secret name cannot be used; choose another name")
	ErrPendingLimit  = fmt.Errorf("too many secrets of this agent are waiting for approval (limit %d); ask the user to review them in the Secret Vault page", store.SecretVaultMaxPendingPerAgent)
	ErrSessionLimit  = fmt.Errorf("this chat session already holds %d session-only secrets; reuse one or save to the vault", MaxSessionSecrets)
	ErrNotOwner      = errors.New("only a secret this agent created can be granted")
	ErrAgentNotFound = errors.New("agent not found")
	ErrGrantDenied   = errors.New("the user did not approve the grant")
	ErrGrantNotAdmin = errors.New("only a tenant admin can approve a grant; ask a tenant admin to grant it in the Secret Vault page")
	ErrNoSession     = errors.New("session-only secrets are not available in this run")
)

// AgentLookup resolves an agent key to its id within the context tenant. It
// must fail for an agent the context user cannot access.
type AgentLookup func(ctx context.Context, agentKey string) (uuid.UUID, error)

// TenantRoleLookup returns a user's role in a tenant ("" when not a member).
type TenantRoleLookup func(ctx context.Context, tenant uuid.UUID, userID string) (string, error)

// AgentWriteSupported reports whether agents may save secrets in the running
// edition. Lite has no surface to approve a pending entry, so an agent-written
// entry would wait forever there.
func AgentWriteSupported() bool {
	return edition.Current().Name != edition.Lite.Name
}

// Outcomes of an agent write.
const (
	WriteActive  = "active"  // the user approved it live; usable now
	WritePending = "pending" // waits for approval in the Secret Vault page
	WriteSession = "session" // kept in memory for this chat session only
)

// NewSecret is the metadata of a secret an agent writes. The value travels
// separately as a store.SecretVaultPayload.
type NewSecret struct {
	Name    string
	Kind    string // default api_key
	Domains []string
	Hints   string
}

// CanWrite reports whether this run's agent may write secrets.
func (rt *Runtime) CanWrite() bool {
	return rt != nil && rt.Store != nil && rt.AllowWrite && AgentWriteSupported()
}

// CheckWrite refuses (and audits) a write when the edition does not support
// agent writes or the agent has not opted in. Callers that must do work
// before they have a value (browser capture) call it first; the write
// methods check it again.
func (rt *Runtime) CheckWrite(ctx context.Context, name string) error {
	return rt.checkWrite(ctx, name, "agent_write_disabled")
}

func (rt *Runtime) checkWrite(ctx context.Context, name, disabledOutcome string) error {
	if rt == nil || rt.Store == nil {
		return ErrNoRuntime
	}
	if !AgentWriteSupported() {
		rt.AuditWriteRefusal(ctx, name, "agent_write_unsupported_edition")
		return ErrWriteUnsupported
	}
	if !rt.AllowWrite {
		rt.AuditWriteRefusal(ctx, name, disabledOutcome)
		return ErrWriteDisabled
	}
	return nil
}

// AuditWriteRefusal records a refused agent write under the requested name.
func (rt *Runtime) AuditWriteRefusal(ctx context.Context, name, outcome string) {
	if rt == nil || rt.Store == nil {
		return
	}
	rt.auditWrite(ctx, uuid.Nil, name, store.SecretAuditDenied, outcome)
}

// ValidateNewSecret checks name, kind and domains of a secret an agent is
// about to write and returns the entry to store. It never sees the value.
func ValidateNewSecret(ns NewSecret) (*store.SecretVaultEntry, error) {
	kind := ns.Kind
	if kind == "" {
		kind = store.SecretKindAPIKey
	}
	domains, err := store.NormalizeSecretDomains(ns.Domains)
	if err != nil {
		return nil, err
	}
	entry := &store.SecretVaultEntry{Name: ns.Name, Kind: kind, AllowedDomains: domains, Hints: ns.Hints}
	if err := store.ValidateSecretVaultEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// prepareWrite registers the value with the redactor before anything else (so
// no later error or output can echo it), then checks the opt-in, size and
// shape. Every refusal is audited.
func (rt *Runtime) prepareWrite(ctx context.Context, ns NewSecret, p store.SecretVaultPayload) (*store.SecretVaultEntry, error) {
	if rt == nil || rt.Store == nil {
		return nil, ErrNoRuntime
	}
	rt.Redactor.AddPayload(p)
	if err := rt.CheckWrite(ctx, ns.Name); err != nil {
		return nil, err
	}
	if len(p.Secret)+len(p.Username) > MaxWriteValueBytes {
		rt.AuditWriteRefusal(ctx, ns.Name, "agent_value_too_large")
		return nil, ErrValueTooLarge
	}
	entry, err := ValidateNewSecret(ns)
	if err == nil {
		err = store.ValidateSecretVaultPayload(entry.Kind, p)
	}
	if err != nil {
		rt.AuditWriteRefusal(ctx, ns.Name, "agent_invalid")
		return nil, err
	}
	entry.CreatedBy = rt.UserID
	return entry, nil
}

// CreatePending stores a new vault entry written by this run's agent. The
// store keeps it pending, enforces the per-agent cap and writes the created
// audit row. The entry is granted to the agent so it becomes usable once the
// user approves it. When a Web UI is open the user is asked right away and a
// yes activates the entry; otherwise it waits in the Secret Vault page.
func (rt *Runtime) CreatePending(ctx context.Context, ns NewSecret, p store.SecretVaultPayload) (string, error) {
	entry, err := rt.prepareWrite(ctx, ns, p)
	if err != nil {
		return "", err
	}
	agent := rt.AgentID
	entry.CreatedByAgent = &agent
	err = rt.Store.Create(ctx, entry, p)
	switch {
	case errors.Is(err, store.ErrSecretVaultNameTaken):
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, rt.nameTakenOutcome(ctx, entry.Name))
		return "", ErrNameTaken
	case errors.Is(err, store.ErrSecretVaultPendingLimit):
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, "agent_pending_limit")
		return "", ErrPendingLimit
	case err != nil:
		slog.Warn("secretvault.agent_create_failed", "name", entry.Name, "error", rt.Redactor.Redact(err.Error()))
		return "", fmt.Errorf("secret vault: could not store %q", entry.Name)
	}
	if err := rt.Store.SetGrants(ctx, entry.ID, []uuid.UUID{agent}); err != nil {
		slog.Warn("secretvault.agent_grant_failed", "name", entry.Name, "error", rt.Redactor.Redact(err.Error()))
		return WritePending, fmt.Errorf("secret %q was stored pending approval but could not be granted to this agent; ask the user to grant it in the Secret Vault page", entry.Name)
	}
	return rt.approveNew(ctx, entry), nil
}

// approveNew asks the user to activate a freshly written entry. Only a live
// yes that counts as a tenant admin's (see adminApproval) activates it;
// anything else leaves it pending for the Secret Vault page.
func (rt *Runtime) approveNew(ctx context.Context, e *store.SecretVaultEntry) string {
	if rt.Forms == nil {
		return WritePending
	}
	domains := strings.Join(e.AllowedDomains, ", ")
	ok, err := rt.adminApproval(ctx, FormSpec{
		Name: e.Name, Domains: e.AllowedDomains, Host: domains,
		Action:  "save the new secret " + e.Name + " written by the agent, for use on " + domains,
		AgentID: rt.AgentID, AgentKey: store.AgentKeyFromContext(ctx), SessionKey: rt.SessionKey,
	})
	switch {
	case errors.Is(err, ErrNoUI):
		return WritePending
	case errors.Is(err, errUserNotTenantAdmin):
		// Not asked: the user could not approve it anyway.
		return WritePending
	case errors.Is(err, errApproverNotAdmin):
		rt.audit(ctx, e, store.SecretAuditDenied, "", "agent_create_approver_not_admin")
		return WritePending
	case err != nil || !ok:
		rt.audit(ctx, e, store.SecretAuditDenied, "", "agent_create_not_approved")
		return WritePending
	}
	err = rt.Store.Approve(ctx, e.ID, e.AllowedDomains)
	switch {
	case err == nil, errors.Is(err, store.ErrSecretVaultNotPending):
		// Not pending any more: an admin approved it meanwhile.
		return WriteActive
	default:
		slog.Warn("secretvault.agent_approve_failed", "name", e.Name, "error", err)
		return WritePending
	}
}

var (
	errUserNotTenantAdmin = errors.New("the run's user is not a tenant admin")
	errApproverNotAdmin   = errors.New("the approving client is not an admin")
)

// adminApproval asks the run's user (the only one who can answer an
// approval of this run) and reports a yes only when it carries the rule of
// the REST approve route: a system owner, or a client with at least the
// admin role whose user is an owner or admin of the context tenant. When the
// user is no tenant admin it does not ask (errUserNotTenantAdmin); a yes from
// a client below admin is errApproverNotAdmin. Without a way to read the
// tenant role it fails closed.
func (rt *Runtime) adminApproval(ctx context.Context, spec FormSpec) (bool, error) {
	tenantAdmin := rt.userIsTenantAdmin(ctx)
	if !tenantAdmin && !store.IsOwnerRole(ctx) {
		return false, errUserNotTenantAdmin
	}
	res, err := rt.Forms.requestApproval(ctx, spec)
	if err != nil {
		return false, err
	}
	if res.Status != StatusApproved {
		return false, nil
	}
	if res.ApproverRole == permissions.RoleOwner ||
		(tenantAdmin && permissions.HasMinRole(res.ApproverRole, permissions.RoleAdmin)) {
		return true, nil
	}
	return false, errApproverNotAdmin
}

// userIsTenantAdmin reports whether the context user is an owner or admin of
// the context tenant.
func (rt *Runtime) userIsTenantAdmin(ctx context.Context) bool {
	tenant, user := store.TenantIDFromContext(ctx), store.UserIDFromContext(ctx)
	if rt.TenantRoles == nil || tenant == uuid.Nil || user == "" {
		return false
	}
	role, err := rt.TenantRoles(ctx, tenant, user)
	if err != nil {
		slog.Warn("secretvault.tenant_role_lookup_failed", "error", err)
		return false
	}
	return role == store.TenantRoleOwner || role == store.TenantRoleAdmin
}

// nameTakenOutcome is the audit outcome of a refused name. The agent gets the
// generic ErrNameTaken; only the audit row says why.
func (rt *Runtime) nameTakenOutcome(ctx context.Context, name string) string {
	e, err := findByName(ctx, rt.Store, name)
	switch {
	case err != nil:
		return "agent_name_taken"
	case e.Status == store.SecretVaultStatusPending:
		return "agent_name_taken_pending"
	case e.CreatedByAgent == nil:
		return "agent_name_taken_user_entry"
	case *e.CreatedByAgent != rt.AgentID:
		return "agent_name_taken_other_agent"
	default:
		return "agent_name_taken_own_entry"
	}
}

// SetSessionSecret keeps a secret written by the agent in memory for this
// chat session only. It never reaches the database. A name already used in
// the session or in the vault is refused, and a session holds at most
// MaxSessionSecrets secrets.
func (rt *Runtime) SetSessionSecret(ctx context.Context, ns NewSecret, p store.SecretVaultPayload) error {
	entry, err := rt.prepareWrite(ctx, ns, p)
	if err != nil {
		return err
	}
	if rt.Forms == nil || rt.SessionKey == "" {
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, "agent_no_session")
		return ErrNoSession
	}
	_, lookupErr := findByName(ctx, rt.Store, entry.Name)
	switch {
	case lookupErr == nil:
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, rt.nameTakenOutcome(ctx, entry.Name))
		return ErrNameTaken
	case !errors.Is(lookupErr, store.ErrSecretVaultNotFound):
		return fmt.Errorf("secret vault: could not check the name %q", entry.Name)
	}
	entry.Status, entry.CreatedVia = store.SecretVaultStatusActive, store.SecretVaultCreatedViaAgent
	err = rt.Forms.Sessions.PutNew(store.TenantIDFromContext(ctx), rt.SessionKey, *entry, p, MaxSessionSecrets)
	switch {
	case errors.Is(err, errSessionNameTaken):
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, "agent_name_taken_session")
		return ErrNameTaken
	case errors.Is(err, errSessionFull):
		rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditDenied, "agent_session_limit")
		return ErrSessionLimit
	case err != nil:
		return fmt.Errorf("secret vault: could not keep %q for this session", entry.Name)
	}
	rt.auditWrite(ctx, uuid.Nil, entry.Name, store.SecretAuditCreated, "agent_session")
	return nil
}

// GrantToAgent lets another agent of the same tenant use a secret this agent
// created. It always needs a live yes from a tenant admin who can access the
// target agent; without a Web UI, or when the run's user is no tenant admin,
// it is refused (fail closed, no pending queue for grants).
func (rt *Runtime) GrantToAgent(ctx context.Context, name, agentKey string) error {
	if err := rt.checkWrite(ctx, name, "agent_grant_write_disabled"); err != nil {
		return err
	}
	entry, err := findByName(ctx, rt.Store, name)
	switch {
	case errors.Is(err, store.ErrSecretVaultNotFound):
		// Same answer as "not yours", so names of other secrets cannot be probed.
		rt.auditWrite(ctx, uuid.Nil, name, store.SecretAuditDenied, "agent_grant_not_owner")
		return fmt.Errorf("%w: %q", ErrNotOwner, name)
	case err != nil:
		return fmt.Errorf("secret vault: lookup %q failed", name)
	}
	if entry.CreatedByAgent == nil || *entry.CreatedByAgent != rt.AgentID {
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_not_owner")
		return fmt.Errorf("%w: %q", ErrNotOwner, name)
	}
	var target uuid.UUID
	if rt.Agents != nil {
		target, err = rt.Agents(ctx, agentKey)
	}
	if rt.Agents == nil || err != nil || target == uuid.Nil {
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_unknown_agent")
		return fmt.Errorf("%w: %q", ErrAgentNotFound, agentKey)
	}
	grants, err := rt.Store.ListGrants(ctx, entry.ID)
	if err != nil {
		return fmt.Errorf("secret vault: grant %q failed", name)
	}
	if slices.Contains(grants, target) {
		return nil
	}
	if rt.Forms == nil {
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_approval_unavailable")
		return fmt.Errorf("%w: no Web UI to ask", ErrGrantDenied)
	}
	ok, err := rt.adminApproval(ctx, FormSpec{
		Name: entry.Name, Domains: entry.AllowedDomains,
		Action:  "let agent " + agentKey + " use the secret " + entry.Name,
		AgentID: rt.AgentID, AgentKey: store.AgentKeyFromContext(ctx), SessionKey: rt.SessionKey,
	})
	switch {
	case errors.Is(err, ErrNoUI):
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_approval_unavailable")
		return fmt.Errorf("%w: no Web UI to ask", ErrGrantDenied)
	case errors.Is(err, errUserNotTenantAdmin):
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_user_not_tenant_admin")
		return ErrGrantNotAdmin
	case errors.Is(err, errApproverNotAdmin):
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_approver_not_admin")
		return ErrGrantNotAdmin
	case err != nil || !ok:
		rt.audit(ctx, entry, store.SecretAuditDenied, "", "agent_grant_denied")
		return ErrGrantDenied
	}
	// Add one grant instead of writing back the set read before the wait, so
	// a revoke an admin made meanwhile is not undone.
	if err := rt.Store.AddGrant(ctx, entry.ID, target); err != nil {
		slog.Warn("secretvault.agent_grant_failed", "name", entry.Name, "error", err)
		return fmt.Errorf("secret vault: grant %q failed", name)
	}
	rt.audit(ctx, entry, store.SecretAuditApproved, "", truncate("granted_to:"+agentKey, 255))
	return nil
}

// auditWrite records a write or refusal that has no stored entry (yet). The
// audit trail keeps the requested name; it never holds a value.
func (rt *Runtime) auditWrite(ctx context.Context, id uuid.UUID, name, action, outcome string) {
	agent := rt.AgentID
	if err := rt.Store.AppendAudit(ctx, &store.SecretVaultAuditEntry{
		EntryID: id, EntryName: truncate(name, 64), AgentID: &agent, UserID: rt.UserID,
		Action: action, Outcome: outcome,
	}); err != nil {
		slog.Warn("secretvault.audit_failed", "action", action, "outcome", outcome)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

type runtimeKey struct{}

// WithRuntime installs rt in ctx. A nil rt explicitly clears any runtime
// inherited from a parent context, so a child run never reuses its parent's
// secrets.
func WithRuntime(ctx context.Context, rt *Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// RuntimeFrom returns the run's runtime, or nil when none (or cleared).
func RuntimeFrom(ctx context.Context) *Runtime {
	rt, _ := ctx.Value(runtimeKey{}).(*Runtime)
	return rt
}
