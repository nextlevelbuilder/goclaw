package secretvault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

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
)

// Runtime is the per-run vault context: which agent is running, the store to
// resolve from, and the redactor every output sink must pass through.
type Runtime struct {
	Store    store.SecretVaultStore
	AgentID  uuid.UUID
	UserID   string
	Redactor *Redactor
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
		return nil, fmt.Errorf("%w: %q", ErrUnavailable, name)
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
	if err := rt.appendAudit(ctx, entry, store.SecretAuditUsed, host, "ok"); err != nil {
		return nil, fmt.Errorf("secret vault: audit failed for %q", name)
	}
	return &Secret{Entry: *entry, Payload: payload}, nil
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
