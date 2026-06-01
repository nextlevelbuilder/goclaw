package bitrix24

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Route paths mounted by the Router on the main gateway mux.
//
// Bitrix24 calls /bitrix24/install once during OAuth install; /bitrix24/events
// is hit continuously for every outbound imbot event. Both are public (no
// gateway auth) so Router.ServeHTTP is responsible for all auth/origin checks.
const (
	WebhookPathPrefix = "/bitrix24/"
	installPath       = "/bitrix24/install"
	eventsPath        = "/bitrix24/events"
	// handlerPath is the "Application URL" / "Application settings handler"
	// registered with partners.bitrix24.com. Bitrix24 GET-pings it during
	// app registration to verify reachability (must return 2xx) and later
	// iframe-loads it (with POST tokens) when a user opens the app inside
	// their portal. See handleAppPage for behavior.
	handlerPath = "/bitrix24/handler"
)

const ambiguousDomainKey = "\x00ambiguous-domain"

// BotDispatcher is the contract Phase 03 Channel implements so Router can
// deliver a verified event to the right bot without importing the Channel
// package. Phase 02 tests use an in-memory fake.
//
// DispatchEvent MUST return quickly (non-blocking) — Router already runs it
// in its own goroutine but Bitrix24 retries on timeout, so implementations
// should push onto a bounded buffer and return immediately.
type BotDispatcher interface {
	BotID() int
	TenantID() uuid.UUID
	PortalName() string
	DispatchEvent(ctx context.Context, evt *Event)
}

// Router multiplexes all Bitrix24 webhooks for every portal on a gateway
// instance. One Router is shared across all bitrix24 channel instances —
// Phase 03 injects the singleton into each Channel via the factory.
//
// State:
//   - portals: (tenant_id + ":" + portal_name) → *Portal
//   - domains: bitrix portal domain → tenantKey (or ambiguousDomainKey to fail closed)
//   - byBotID: bot_id (int, set on imbot.register) → BotDispatcher
//   - dedup:   per-portal MESSAGE_ID LRU (bounded + TTL)
type Router struct {
	portalStore store.BitrixPortalStore
	encKey      string

	mu      sync.RWMutex
	portals map[string]*Portal    // tenantKey → *Portal
	domains map[string]string     // domain (normalized lowercase) → tenantKey
	byBotID map[int]BotDispatcher // bot_id → dispatcher
	dedup   *dedupCache

	// running tracks portals whose refresh loop has already been kicked off
	// so EnsurePortalRunning is idempotent across multiple Channel.Start calls
	// sharing the same portal.
	running sync.Map // tenantKey → struct{}

	// routeTaken guards WebhookRoute(): only the first caller gets the path
	// + handler; subsequent callers return ("", nil). Matches Facebook's
	// WebhookChannel convention — see internal/channels/facebook/webhook_router.go.
	routeTaken atomic.Bool

	// errorLog is overridable for tests.
	errorLog func(msg string, args ...any)
}

// RouterConfig holds the tunable knobs. Zero values use sensible defaults.
type RouterConfig struct {
	DedupMaxSize     int
	DedupTTL         time.Duration
	DedupSweepPeriod time.Duration
}

// NewRouter builds a detached Router. Call RegisterPortal to wire portals;
// the Router starts without a mounted route until the first Channel calls
// ClaimWebhookRoute().
func NewRouter(s store.BitrixPortalStore, encKey string, cfg RouterConfig) *Router {
	if cfg.DedupMaxSize <= 0 {
		cfg.DedupMaxSize = 10_000
	}
	if cfg.DedupTTL <= 0 {
		cfg.DedupTTL = 5 * time.Minute
	}
	if cfg.DedupSweepPeriod <= 0 {
		cfg.DedupSweepPeriod = 1 * time.Minute
	}

	r := &Router{
		portalStore: s,
		encKey:      encKey,
		portals:     make(map[string]*Portal),
		domains:     make(map[string]string),
		byBotID:     make(map[int]BotDispatcher),
		dedup:       newDedupCache(cfg.DedupMaxSize, cfg.DedupTTL),
	}
	r.dedup.StartSweeper(cfg.DedupSweepPeriod)
	return r
}

// Stop halts background work (the dedup sweeper). Idempotent.
func (r *Router) Stop() {
	r.dedup.Stop()
}

// RegisterPortal makes a portal discoverable by (tenant, name) and by domain.
// Overwrites any prior registration with the same key (reload safe).
//
// When replacing an existing entry with a different *Portal pointer we log a
// warning: the old pointer's refresh goroutine is still running (router.running
// keeps its key) but all lookups will route to the new pointer, so anything
// the old refresh goroutine writes to state is effectively orphaned until the
// old portal is Stop()'d. In practice this only happens under racey reloads
// (BootstrapPortals running concurrently with a Channel.Start that already
// hydrated via ResolveOrLoadPortal) and the old goroutine exits cleanly on
// next process restart.
func (r *Router) RegisterPortal(p *Portal) {
	if p == nil {
		return
	}
	key := portalKey(p.TenantID(), p.Name())
	r.mu.Lock()
	if existing, ok := r.portals[key]; ok && existing != p {
		slog.Warn("bitrix24 router: RegisterPortal replacing existing *Portal pointer — old refresh goroutine will be orphaned until process restart",
			"tenant", p.TenantID(), "portal", p.Name())
	}
	r.portals[key] = p
	r.setDomainLocked(p.Domain(), key)
	r.mu.Unlock()
}

func (r *Router) setDomainLocked(domain, key string) {
	d := normalizePortalDomain(domain)
	if d == "" {
		return
	}
	if existingKey, ok := r.domains[d]; ok && existingKey != key {
		r.domains[d] = ambiguousDomainKey
		slog.Warn("security.bitrix24_domain_collision",
			"domain", d,
			"existing_key", existingKey,
			"new_key", key)
		return
	} else if existingKey == ambiguousDomainKey {
		return
	}
	r.domains[d] = key
}

// UnregisterPortal removes a portal from both lookup tables.
//
// Also drops the entry from `running` so a subsequent re-registration +
// EnsurePortalRunning for the same key actually starts a fresh refresh loop
// (LoadOrStore would otherwise keep returning loaded=true forever).
func (r *Router) UnregisterPortal(tenantID uuid.UUID, name string) {
	key := portalKey(tenantID, name)
	r.mu.Lock()
	if p, ok := r.portals[key]; ok {
		delete(r.portals, key)
		if d := normalizePortalDomain(p.Domain()); d != "" {
			// Only clear if the domain still points at this same key —
			// guards against racing re-registration under a new name.
			if r.domains[d] == key {
				delete(r.domains, d)
			}
		}
	}
	r.mu.Unlock()
	r.running.Delete(key)
}

// RegisterBot wires a bot id to a dispatcher. Called by Phase 03 Channel
// after imbot.register confirms the bot id on the portal.
func (r *Router) RegisterBot(botID int, d BotDispatcher) {
	if botID <= 0 || d == nil {
		return
	}
	r.mu.Lock()
	r.byBotID[botID] = d
	r.mu.Unlock()
}

// UnregisterBot removes the dispatcher entry. Called from ONIMBOTDELETE
// handling or on channel shutdown.
func (r *Router) UnregisterBot(botID int) {
	if botID <= 0 {
		return
	}
	r.mu.Lock()
	delete(r.byBotID, botID)
	r.mu.Unlock()
}

// PortalByKey returns the portal registered under (tenant, name), if any.
// Exported for tests and for Phase 03 channel bootstrap.
func (r *Router) PortalByKey(tenantID uuid.UUID, name string) (*Portal, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.portals[portalKey(tenantID, name)]
	return p, ok
}

// PortalByDomain resolves a portal by its Bitrix24 domain.
// Used by handleEvent to find the target portal from auth.domain.
func (r *Router) PortalByDomain(domain string) (*Portal, bool) {
	d := normalizePortalDomain(domain)
	if d == "" {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.domains[d]
	if !ok || key == ambiguousDomainKey {
		return nil, false
	}
	p, ok := r.portals[key]
	return p, ok
}

func normalizePortalDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// ClaimWebhookRoute returns the path+handler pair that the first Bitrix24
// Channel reports via WebhookChannel.WebhookHandler(). Subsequent calls
// return ("", nil) — all portals share a single mount point.
//
// Matches the pattern in internal/channels/facebook/webhook_router.go.
func (r *Router) ClaimWebhookRoute() (string, http.Handler) {
	if r.routeTaken.CompareAndSwap(false, true) {
		return WebhookPathPrefix, r
	}
	return "", nil
}

// ResolveOrLoadPortal returns the portal registered under (tenant, name),
// loading it from the store if not yet registered. Concurrency-safe via
// double-checked locking — two goroutines racing to hydrate the same portal
// will observe identical *Portal pointers.
func (r *Router) ResolveOrLoadPortal(ctx context.Context, tenantID uuid.UUID, name string) (*Portal, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("bitrix24 router: tenant_id required")
	}
	if name == "" {
		return nil, errors.New("bitrix24 router: portal name required")
	}

	// Fast path — already loaded.
	if p, ok := r.PortalByKey(tenantID, name); ok {
		return p, nil
	}

	// Slow path — load under write lock so concurrent callers coalesce.
	r.mu.Lock()
	defer r.mu.Unlock()

	key := portalKey(tenantID, name)
	if p, ok := r.portals[key]; ok {
		return p, nil
	}

	p, err := NewPortal(ctx, tenantID, name, r.portalStore, r.encKey)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 router: load portal %q: %w", name, err)
	}
	r.portals[key] = p
	if d := strings.ToLower(strings.TrimSpace(p.Domain())); d != "" {
		r.domains[d] = key
	}
	return p, nil
}

// EnsurePortalRunning kicks off the portal's refresh loop if not already
// running. Idempotent — calls from multiple channels on the same portal
// only start one goroutine.
func (r *Router) EnsurePortalRunning(ctx context.Context, p *Portal) {
	if p == nil {
		return
	}
	key := portalKey(p.TenantID(), p.Name())
	if _, loaded := r.running.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	p.StartRefreshLoop(ctx)
}

// ServeHTTP is the single entrypoint for /bitrix24/install and /bitrix24/events.
// Path routing happens here so the same prefix registration covers both.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.URL.Path {
	case installPath:
		r.handleInstall(w, req)
	case eventsPath:
		r.handleEvent(w, req)
	case handlerPath:
		r.handleAppPage(w, req)
	default:
		http.NotFound(w, req)
	}
}

// portalKey produces the canonical lookup key for a (tenant, name) pair.
// Kept as a package-level func so tests can construct the same keys.
func portalKey(tenantID uuid.UUID, name string) string {
	return tenantID.String() + ":" + name
}

// parseInstallState splits the OAuth state param (<tenant_uuid>:<name>)
// and validates the uuid. Returns uuid.Nil on any failure; callers treat
// an invalid state as 400 Bad Request.
func parseInstallState(state string) (uuid.UUID, string, bool) {
	tenantStr, name, ok := strings.Cut(strings.TrimSpace(state), ":")
	if !ok || tenantStr == "" || name == "" {
		return uuid.Nil, "", false
	}
	tid, err := uuid.Parse(tenantStr)
	if err != nil {
		return uuid.Nil, "", false
	}
	return tid, name, true
}

// defaultRouter is the process-wide singleton used by the Phase 03 channel
// factory. Initialised once via InitWebhookRouter; accessed via WebhookRouter.
var (
	routerOnce sync.Once
	routerInst *Router
	routerErr  error
)

// InitWebhookRouter lazily builds the process-wide Router. Safe to call
// multiple times — only the first invocation wins, subsequent calls are
// no-ops and return the same pointer.
//
// Returns an error if called with a nil store.
func InitWebhookRouter(s store.BitrixPortalStore, encKey string, cfg RouterConfig) (*Router, error) {
	routerOnce.Do(func() {
		if s == nil {
			routerErr = errors.New("bitrix24: nil BitrixPortalStore")
			return
		}
		routerInst = NewRouter(s, encKey, cfg)
	})
	return routerInst, routerErr
}

// WebhookRouter returns the process-wide Router, or nil if InitWebhookRouter
// has not been called yet.
func WebhookRouter() *Router {
	return routerInst
}

// resetWebhookRouterForTest is used only by tests to get a fresh singleton.
// Not exported outside the package. Stops the previous router's background
// work (dedup sweeper goroutine) so a test run doesn't accumulate goroutines
// across subtests.
func resetWebhookRouterForTest() {
	if routerInst != nil {
		routerInst.Stop()
	}
	routerOnce = sync.Once{}
	routerInst = nil
	routerErr = nil
}
