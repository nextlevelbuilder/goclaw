package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Backend identifies the CDP browser goclaw is talking to.
// Chrome multiplexes all tabs/contexts over a single WS; Lightpanda requires
// one CDP connection per tab (each connection is its own browser).
type Backend string

const (
	BackendChrome     Backend = "chrome"
	BackendLightpanda Backend = "lightpanda"
)

// Manager handles the browser lifecycle and page management.
//
// Two backends are supported:
//   - Chrome (default): one shared *rod.Browser, tabs are pages within it,
//     tenants are isolated via Incognito browser contexts.
//   - Lightpanda: no shared browser. Each tab mints its own CDP connection
//     (tracked in pageConns). Tenant isolation is implicit — every connection
//     is a fresh browser server-side.
type Manager struct {
	mu             sync.Mutex
	browser        *rod.Browser       // chrome only; nil on lightpanda
	launcher       *launcher.Launcher // retained for PID-based cleanup on crash
	refs           *RefStore
	pages          map[string]*rod.Page        // targetID → page
	pageConns      map[string]*rod.Browser     // lightpanda only: targetID → dedicated CDP conn
	pageInfos      map[string]TabInfo          // lightpanda only: cached URL/Title (page.Info() is unreliable upstream)
	console        map[string][]ConsoleMessage // targetID → console messages
	tenantCtxs     map[string]*rod.Browser     // chrome only: browser scope key → incognito browser context
	pageTenants    map[string]string           // targetID → browser scope key (for filtering)
	pageLastUsed   map[string]time.Time        // targetID → last access time
	backend        Backend                     // "chrome" or "lightpanda"; auto-detected in Start() if empty
	cdpURL         string                      // resolved CDP WS URL for remote sidecar; used to mint new conns on lightpanda
	headless       bool
	remoteURL      string        // CDP endpoint for remote Chrome (sidecar); skips local launcher
	actionTimeout  time.Duration // per-action context timeout (default 30s)
	idleTimeout    time.Duration // auto-close pages idle longer than this (default 10m, 0=disabled)
	maxPages       int           // max open pages per tenant (default 5)
	cookieProvider CookieProvider
	stopReaper     chan struct{} // signal to stop the reaper goroutine
	logger         *slog.Logger
	nextLpTabSeq   uint64 // lightpanda only: monotonic counter for synthetic tab IDs
}

// Option configures a Manager.
type Option func(*Manager)

// WithHeadless sets headless mode (default false).
func WithHeadless(h bool) Option {
	return func(m *Manager) { m.headless = h }
}

// WithRemoteURL sets a remote CDP endpoint (e.g. "ws://chrome:9222").
// When set, Start() connects to the remote sidecar instead of launching locally.
func WithRemoteURL(url string) Option {
	return func(m *Manager) { m.remoteURL = url }
}

// WithBackend sets the browser backend explicitly ("chrome" or "lightpanda").
// If unset and RemoteURL is configured, Start() probes /json/version and
// auto-detects. Local (non-remote) launches are always Chrome.
func WithBackend(b Backend) Option {
	return func(m *Manager) { m.backend = b }
}

// WithLogger sets a custom logger.
func WithLogger(l *slog.Logger) Option {
	return func(m *Manager) { m.logger = l }
}

// WithActionTimeout sets the per-action context timeout.
func WithActionTimeout(d time.Duration) Option {
	return func(m *Manager) { m.actionTimeout = d }
}

// WithIdleTimeout sets the idle page auto-close timeout. 0 disables the reaper.
func WithIdleTimeout(d time.Duration) Option {
	return func(m *Manager) { m.idleTimeout = d }
}

// WithMaxPages sets the max open pages per tenant.
func WithMaxPages(n int) Option {
	return func(m *Manager) { m.maxPages = n }
}

// WithCookieProvider sets the provider for selected cookie sync into new pages.
func WithCookieProvider(p CookieProvider) Option {
	return func(m *Manager) { m.cookieProvider = p }
}

// New creates a Manager with options.
func New(opts ...Option) *Manager {
	m := &Manager{
		refs:          NewRefStore(),
		pages:         make(map[string]*rod.Page),
		pageConns:     make(map[string]*rod.Browser),
		pageInfos:     make(map[string]TabInfo),
		console:       make(map[string][]ConsoleMessage),
		tenantCtxs:    make(map[string]*rod.Browser),
		pageTenants:   make(map[string]string),
		pageLastUsed:  make(map[string]time.Time),
		actionTimeout: 30 * time.Second,
		idleTimeout:   10 * time.Minute,
		maxPages:      5,
		logger:        slog.Default(),
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

// ActionTimeout returns the configured per-action timeout.
func (m *Manager) ActionTimeout() time.Duration {
	return m.actionTimeout
}

// SetCookieProvider updates the selected-cookie provider after store setup.
func (m *Manager) SetCookieProvider(p CookieProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cookieProvider = p
}

// Backend returns the current backend (resolved after Start()).
func (m *Manager) Backend() Backend {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.backend
}

// isRunningLocked reports whether the manager has an active backend connection.
// Must be called with mu held.
func (m *Manager) isRunningLocked() bool {
	if m.backend == BackendLightpanda {
		return m.cdpURL != ""
	}
	return m.browser != nil
}

// resetPageMapsLocked clears all page-related tracking. Must be called with mu held.
func (m *Manager) resetPageMapsLocked() {
	m.pages = make(map[string]*rod.Page)
	m.pageConns = make(map[string]*rod.Browser)
	m.pageInfos = make(map[string]TabInfo)
	m.console = make(map[string][]ConsoleMessage)
	m.pageTenants = make(map[string]string)
	m.pageLastUsed = make(map[string]time.Time)
}

// probeBackendLocked queries /json/version to detect whether the remote is
// Lightpanda or Chrome. Must be called with mu held. Falls back to Chrome on
// any error or ambiguity — Chrome is the safe default.
func (m *Manager) probeBackendLocked() Backend {
	if m.remoteURL == "" {
		return BackendChrome // local launcher is Chrome
	}
	parsed, err := url.Parse(m.remoteURL)
	if err != nil {
		return BackendChrome
	}
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "9222"
	}
	resp, err := cdpHTTPClient.Get(fmt.Sprintf("http://%s:%s/json/version", host, port)) //nolint:gosec // user-configured remote URL
	if err != nil {
		return BackendChrome
	}
	defer resp.Body.Close()
	var ver struct {
		Browser string `json:"Browser"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return BackendChrome
	}
	if strings.Contains(strings.ToLower(ver.Browser), "lightpanda") {
		return BackendLightpanda
	}
	return BackendChrome
}

// touchPageLocked updates the last-used timestamp for a page. Must be called with mu held.
func (m *Manager) touchPageLocked(targetID string) {
	m.pageLastUsed[targetID] = time.Now()
}

// Start launches a local Chrome browser or connects to a remote one.
// If already connected but the connection is dead, it reconnects automatically.
//
// On Lightpanda there is no persistent shared browser — Start() only resolves
// the CDP URL and (optionally) auto-detects the backend. Each tab will mint
// its own CDP connection when opened.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Lightpanda path: no persistent browser. Resolve CDP URL once, start reaper.
	if m.backend == BackendLightpanda {
		if m.cdpURL == "" {
			u, err := resolveRemoteCDP(m.remoteURL)
			if err != nil {
				return fmt.Errorf("resolve remote CDP at %s: %w", m.remoteURL, err)
			}
			m.cdpURL = u
			m.logger.Info("lightpanda backend ready", "cdp", m.cdpURL)
		}
		if m.idleTimeout > 0 && m.stopReaper == nil {
			m.stopReaper = make(chan struct{})
			go m.runReaper()
		}
		return nil
	}

	// If browser exists, check if connection is still alive
	if m.browser != nil {
		if _, err := m.browser.Pages(); err == nil {
			return nil // already connected and healthy
		}
		// Connection dead — clean up and reconnect
		m.logger.Info("browser connection lost, reconnecting")
		m.cleanupDeadBrowserLocked()
	}

	var controlURL string

	if m.remoteURL != "" {
		// Remote sidecar — query /json/version and fix host for Docker networking
		u, err := resolveRemoteCDP(m.remoteURL)
		if err != nil {
			return fmt.Errorf("resolve remote CDP at %s: %w", m.remoteURL, err)
		}
		controlURL = u
		m.cdpURL = u

		// Auto-detect backend if caller didn't set one explicitly.
		if m.backend == "" {
			m.backend = m.probeBackendLocked()
			m.logger.Info("auto-detected browser backend", "backend", m.backend)
			if m.backend == BackendLightpanda {
				// Switch to lightpanda-style lifecycle: no persistent browser.
				if m.idleTimeout > 0 && m.stopReaper == nil {
					m.stopReaper = make(chan struct{})
					go m.runReaper()
				}
				return nil
			}
		}
		m.logger.Info("connecting to remote Chrome", "cdp", controlURL, "remote", m.remoteURL)
	} else {
		// Local Chrome — launch via rod launcher with stability flags
		launchCtx, launchCancel := context.WithTimeout(ctx, 30*time.Second)
		defer launchCancel()

		l := launcher.New().
			Context(launchCtx).
			Leakless(true).
			Headless(m.headless).
			Set("disable-gpu").
			Set("no-first-run").
			Set("no-default-browser-check").
			Set("disable-dev-shm-usage").
			Set("disable-software-rasterizer").
			Set("disable-extensions").
			Set("disable-background-networking").
			Set("disable-renderer-backgrounding").
			Set("disable-background-timer-throttling").
			Set("disable-backgrounding-occluded-windows")

		u, err := l.Launch()
		if err != nil {
			return fmt.Errorf("launch Chrome: %w", err)
		}
		controlURL = u
		m.launcher = l
		m.backend = BackendChrome // local launcher is always Chrome
		m.logger.Info("Chrome launched", "cdp", controlURL, "headless", m.headless, "pid", l.PID())
	}

	// Connect using a long-lived background context, the same way reconnectLocked
	// does. Binding m.browser to a timeout/request context here would cancel it
	// the moment Start returns (via defer), leaving the shared browser handle with
	// a dead context. Later operations that reuse it — notably m.browser.Incognito()
	// in tenantBrowserLocked — then fail immediately with "context canceled", which
	// is why a successful "start" is followed by a failing "open"/tab creation.
	// resolveRemoteCDP already bounds remote reachability, and the local launcher
	// path is bounded by launchCtx above.
	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		// If local launch succeeded but connect failed, kill the orphan process
		if m.launcher != nil {
			m.launcher.Kill()
			m.launcher.Cleanup()
			m.launcher = nil
		}
		return fmt.Errorf("connect to Chrome: %w", err)
	}

	m.browser = b

	// Start idle-page reaper if configured
	if m.idleTimeout > 0 && m.stopReaper == nil {
		m.stopReaper = make(chan struct{})
		go m.runReaper()
	}

	return nil
}

// Stop closes the browser (local) or disconnects (remote sidecar).
// On Lightpanda, closes every per-tab CDP connection (which tears down each
// browser server-side).
func (m *Manager) Stop(ctx context.Context) error {
	// Grab and nil-out stopReaper under the lock, then close outside to avoid
	// deadlock (reaper goroutine also acquires mu).
	m.mu.Lock()
	ch := m.stopReaper
	m.stopReaper = nil
	m.mu.Unlock()
	if ch != nil {
		close(ch)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunningLocked() {
		return nil
	}

	// Lightpanda: close every per-tab connection. Lightpanda auto-cleans the
	// browser on disconnect, so no page.Close() is required. rod.Browser.Close
	// calls Browser.close which Lightpanda doesn't implement (UnknownMethod);
	// the WS drops regardless, so swallow the error.
	if m.backend == BackendLightpanda {
		for _, conn := range m.pageConns {
			_ = conn.Close()
		}
		m.cdpURL = ""
		m.resetPageMapsLocked()
		return nil
	}

	m.closeTenantContextsLocked()

	var err error
	if m.remoteURL == "" {
		// Local Chrome — close the browser process
		err = m.browser.Close()
		// Force-kill via launcher if retained
		if m.launcher != nil {
			m.launcher.Kill()
			m.launcher.Cleanup()
			m.launcher = nil
		}
	}
	// Remote Chrome — just drop the connection; sidecar stays alive

	m.browser = nil
	m.resetPageMapsLocked()
	return err
}

// closeTenantContextsLocked closes all incognito browser contexts. Must be called with mu held.
func (m *Manager) closeTenantContextsLocked() {
	for tid, ctx := range m.tenantCtxs {
		if err := ctx.Close(); err != nil {
			m.logger.Warn("failed to close tenant browser context", "tenant", tid, "error", err)
		}
	}
	m.tenantCtxs = make(map[string]*rod.Browser)
}

// cleanupDeadBrowserLocked resets all state and kills any orphan Chrome process.
// Must be called with mu held.
func (m *Manager) cleanupDeadBrowserLocked() {
	m.closeTenantContextsLocked()
	if m.launcher != nil {
		m.launcher.Kill()
		m.launcher.Cleanup()
		m.launcher = nil
	}
	// Lightpanda: also drop any dedicated page conns (will be torn down when
	// the underlying WS dies anyway, but keep state consistent).
	for _, conn := range m.pageConns {
		_ = conn.Close()
	}
	m.browser = nil
	m.resetPageMapsLocked()
	m.refs = NewRefStore()
}

// MasterTenantID is the well-known master tenant UUID string.
// Pages opened without a tenant context or by the master tenant use the main browser directly.
const MasterTenantID = "0193a5b0-7000-7000-8000-000000000001"

// tenantBrowserLocked returns an isolated incognito browser context for the given scope.
// Legacy master/empty scopes with no user/agent use the main browser.
// Must be called with mu held.
func (m *Manager) tenantBrowserLocked(scopeKey string) (*rod.Browser, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}
	if scopeKey == "" || scopeKey == MasterTenantID {
		return m.browser, nil
	}
	// Return existing incognito context
	if ctx, ok := m.tenantCtxs[scopeKey]; ok {
		return ctx, nil
	}
	// Create new incognito context for this scope.
	incognito, err := m.browser.Incognito()
	if err != nil {
		return nil, fmt.Errorf("create incognito context for browser scope %s: %w", scopeKey, err)
	}
	m.tenantCtxs[scopeKey] = incognito
	m.logger.Info("created incognito browser context", "scope", scopeKey)
	return incognito, nil
}

// Status returns current browser status.
func (m *Manager) Status() *StatusInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	info := &StatusInfo{
		Running:         m.isRunningLocked(),
		Headless:        m.headless,
		RemoteURL:       m.remoteURL,
		ActionTimeoutMs: int(m.actionTimeout / time.Millisecond),
		IdleTimeoutMs:   int(m.idleTimeout / time.Millisecond),
		MaxPages:        m.maxPages,
		IsolationMode:   "tenant_user_agent",
		CookieSync:      m.cookieProvider != nil,
	}

	if !m.isRunningLocked() {
		return info
	}

	// Lightpanda: no upstream /json/list, report from the local map.
	if m.backend == BackendLightpanda {
		info.Tabs = len(m.pages)
		for _, p := range m.pages {
			if pi, err := p.Info(); err == nil && pi != nil {
				info.URL = pi.URL
				break
			}
		}
		return info
	}

	pages, _ := m.browser.Pages()
	info.Tabs = len(pages)
	if len(pages) > 0 {
		if pageInfo, err := pages[0].Info(); err == nil {
			info.URL = pageInfo.URL
		}
	}
	return info
}
