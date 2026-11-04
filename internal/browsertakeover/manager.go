// Package browsertakeover brokers requests where an agent hands a browser tab
// to the user (SSO, CAPTCHA, QR, OTP) and waits until the user is done.
//
// A request carries only metadata: nothing the user types or sees is returned
// to the agent. Requests have an unguessable id and are bound to tenant, user
// and tab, so another user (an admin included) cannot answer them.
package browsertakeover

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Statuses reported to the waiting agent.
const (
	StatusDone      = "done"
	StatusCancelled = "cancelled"
	StatusTimeout   = "timeout"
)

// DefaultTimeout bounds how long an agent run waits for the user.
const DefaultTimeout = 10 * time.Minute

var (
	// ErrNoUI means no Web UI session of the user is connected, so the user
	// cannot take over. Callers fail fast instead of waiting.
	ErrNoUI = errors.New("browser take over needs the Web UI, but no Web UI session of this user is connected")
	// ErrBusy means the tab already has a take over request pending.
	ErrBusy = errors.New("this tab already has a take over request pending")
	// ErrRequestNotFound covers unknown, expired, answered and foreign
	// requests alike, so ids cannot be probed.
	ErrRequestNotFound = errors.New("take over request not found or already resolved")
)

// Publisher delivers an event to the Web UI clients of one tenant. The gateway
// filters browser.takeover.* events down to the owning user.
type Publisher func(event string, tenant uuid.UUID, payload map[string]any)

// Spec describes a request shown to the user.
type Spec struct {
	Tenant     uuid.UUID
	User       string
	TargetID   string // browser tab; one pending request per tab
	Host       string // host of the page the user will work on
	Reason     string // already redacted by the caller
	AgentKey   string
	SessionKey string
	Timeout    time.Duration // 0 selects DefaultTimeout
}

// Result is all the requesting tool learns.
type Result struct {
	Status string
}

type pending struct {
	id      string
	spec    Spec
	expires time.Time
	done    chan Result
}

// Manager tracks pending take over requests.
type Manager struct {
	mu        sync.Mutex
	pending   map[string]*pending
	byTab     map[string]string // tenant|target -> request id
	publish   Publisher
	hasClient func(tenant uuid.UUID, user string) bool
}

// New builds a manager. Until Configure is called every request fails with ErrNoUI.
func New() *Manager {
	return &Manager{pending: make(map[string]*pending), byTab: make(map[string]string)}
}

var (
	defaultOnce sync.Once
	defaultMgr  *Manager
)

// Default is the process-wide manager shared by the browser tool (which blocks
// on it) and the WS handlers (which answer it).
func Default() *Manager {
	defaultOnce.Do(func() { defaultMgr = New() })
	return defaultMgr
}

// Configure wires event delivery and the connected-client check.
func (m *Manager) Configure(publish Publisher, hasClient func(tenant uuid.UUID, user string) bool) {
	m.mu.Lock()
	m.publish, m.hasClient = publish, hasClient
	m.mu.Unlock()
}

func tabKey(tenant uuid.UUID, target string) string { return tenant.String() + "|" + target }

// Request asks the user to take over a tab and blocks until they finish,
// cancel, the timeout elapses, or ctx ends.
func (m *Manager) Request(ctx context.Context, spec Spec) (Result, error) {
	m.mu.Lock()
	publish, hasClient := m.publish, m.hasClient
	m.mu.Unlock()
	if publish == nil || hasClient == nil || spec.User == "" || !hasClient(spec.Tenant, spec.User) {
		return Result{}, ErrNoUI
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	p := &pending{
		id:      uuid.NewString(),
		spec:    spec,
		expires: time.Now().Add(timeout),
		done:    make(chan Result, 1),
	}
	key := tabKey(spec.Tenant, spec.TargetID)
	m.mu.Lock()
	if _, busy := m.byTab[key]; busy {
		m.mu.Unlock()
		return Result{}, ErrBusy
	}
	m.pending[p.id] = p
	m.byTab[key] = p.id
	m.mu.Unlock()

	publish(protocol.EventBrowserTakeoverRequested, spec.Tenant, map[string]any{
		"id":         p.id,
		"targetId":   spec.TargetID,
		"host":       spec.Host,
		"reason":     spec.Reason,
		"agentKey":   spec.AgentKey,
		"sessionKey": spec.SessionKey,
		"userId":     spec.User,
		"expiresAt":  p.expires.UnixMilli(),
	})
	slog.Info("browser.takeover.request", "host", spec.Host)

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var res Result
	select {
	case res = <-p.done:
	case <-timer.C:
		res = Result{Status: StatusTimeout}
	case <-ctx.Done():
		res = Result{Status: StatusCancelled}
	}

	m.mu.Lock()
	delete(m.pending, p.id)
	if m.byTab[key] == p.id {
		delete(m.byTab, key)
	}
	m.mu.Unlock()
	publish(protocol.EventBrowserTakeoverResolved, spec.Tenant, map[string]any{
		"id": p.id, "status": res.Status, "userId": spec.User,
	})
	slog.Info("browser.takeover.done", "status", res.Status)
	return res, nil
}

// claim removes a pending request owned by tenant and user. Mismatches look
// like "not found".
func (m *Manager) claim(id string, tenant uuid.UUID, user string) (*pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pending[id]
	if !ok || p.spec.Tenant != tenant || p.spec.User != user || time.Now().After(p.expires) {
		return nil, ErrRequestNotFound
	}
	delete(m.pending, id)
	return p, nil
}

// Done tells the waiting agent the user finished their part.
func (m *Manager) Done(tenant uuid.UUID, user, id string) error {
	p, err := m.claim(id, tenant, user)
	if err != nil {
		return err
	}
	p.done <- Result{Status: StatusDone}
	return nil
}

// Cancel dismisses the request without the user having done anything.
func (m *Manager) Cancel(tenant uuid.UUID, user, id string) error {
	p, err := m.claim(id, tenant, user)
	if err != nil {
		return err
	}
	p.done <- Result{Status: StatusCancelled}
	return nil
}
