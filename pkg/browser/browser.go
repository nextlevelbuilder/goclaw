package browser

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// Manager handles the browser lifecycle and page management.
type Manager struct {
	mu            sync.Mutex
	engine        Engine                     // abstracts Chrome/Container/Lightpanda
	newEngine     func() Engine              // factory for engine reset after Stop()
	refs          *RefStore
	pages         map[string]Page            // targetID → page
	console       map[string][]ConsoleMessage // targetID → console messages
	tenantEngines map[string]Engine          // tenantID → incognito engine
	pageTenants   map[string]string          // targetID → tenantID (for filtering)
	pageAgents      map[string]string          // targetID → agentKey (for filtering)
	pageSessionKeys map[string]string          // targetID → sessionKey (for per-session isolation)
	pageLastUsed    map[string]time.Time       // targetID → last access time
	pageProfiles    map[string]string          // targetID → profileDir
	profileAgents   map[string]string          // profileDir → agentKey (inherited by all pages in profile)
	profileSessions map[string]string          // profileDir → sessionKey (inherited by all pages in profile)
	headless      bool
	remoteURL     string        // CDP endpoint for remote Chrome (sidecar)
	binaryPath    string        // custom browser binary (e.g. Brave, Edge, Chromium)
	proxyURL       string        // proxy server URL (http/https/socks5)
	activeProxyURL string        // proxy URL currently applied to running browser
	proxyMgr       *ProxyManager // optional: pool-based proxy for host mode
	workspace     string        // root dir for profiles, screenshots, etc.
	activeProfile string        // current Chrome profile name
	actionTimeout time.Duration // per-action context timeout (default 30s)
	idleTimeout   time.Duration // auto-close pages idle longer than this (default 10m, 0=disabled)
	maxPages       int           // max open pages per tenant (default 5)
	viewportWidth  int           // default viewport width (default 1280)
	viewportHeight int           // default viewport height (default 720)
	stopReaper     chan struct{} // signal to stop the reaper goroutine
	closed         bool          // true after Close()/Stop() — prevents reconnect during shutdown
	logger         *slog.Logger
}

// Option configures a Manager.
type Option func(*Manager)

// WithHeadless sets headless mode (default false).
func WithHeadless(h bool) Option {
	return func(m *Manager) { m.headless = h }
}

// WithRemoteURL sets a remote CDP endpoint (e.g. "ws://chrome:9222").
// When set, Start() connects to the remote Chrome instead of launching locally.
func WithRemoteURL(url string) Option {
	return func(m *Manager) { m.remoteURL = url }
}

// WithBinaryPath sets a custom browser binary path.
// Use this to run Brave, Edge, Chromium, or any Chromium-based browser.
// Example: WithBinaryPath("/Applications/Brave Browser.app/Contents/MacOS/Brave Browser")
func WithBinaryPath(path string) Option {
	return func(m *Manager) { m.binaryPath = path }
}

// WithProxy sets a proxy server for the browser.
// Supports http, https, and socks5 protocols.
// Example: WithProxy("socks5://proxy.example.com:1080")
func WithProxy(proxyURL string) Option {
	return func(m *Manager) { m.proxyURL = proxyURL }
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

// WithWorkspace sets the root workspace directory for profile storage.
func WithWorkspace(dir string) Option {
	return func(m *Manager) { m.workspace = dir }
}

// WithViewport sets the default viewport size for screencast.
func WithViewport(w, h int) Option {
	return func(m *Manager) {
		m.viewportWidth = w
		m.viewportHeight = h
	}
}

// WithEngine sets a custom Engine implementation (e.g. ContainerEngine).
// When set, the Manager uses this engine instead of the default ChromeEngine.
// The same engine instance is reused after Stop() (it handles its own cleanup/re-launch).
func WithEngine(e Engine) Option {
	return func(m *Manager) {
		m.engine = e
		m.newEngine = func() Engine { return e }
	}
}

// New creates a Manager with options.
func New(opts ...Option) *Manager {
	m := &Manager{
		refs:          NewRefStore(),
		pages:         make(map[string]Page),
		console:       make(map[string][]ConsoleMessage),
		tenantEngines: make(map[string]Engine),
		pageTenants:   make(map[string]string),
		pageAgents:      make(map[string]string),
		pageSessionKeys: make(map[string]string),
		pageLastUsed:    make(map[string]time.Time),
		pageProfiles:    make(map[string]string),
		profileAgents:   make(map[string]string),
		profileSessions: make(map[string]string),
		actionTimeout:  30 * time.Second,
		idleTimeout:    30 * time.Minute,
		maxPages:       5,
		viewportWidth:  1280,
		viewportHeight: 720,
		logger:         slog.Default(),
	}
	for _, o := range opts {
		o(m)
	}
	// Default engine: Chrome (unless overridden by WithEngine)
	if m.engine == nil {
		m.engine = NewChromeEngine(m.logger)
		m.newEngine = func() Engine { return NewChromeEngine(m.logger) }
	}
	return m
}

// ActionTimeout returns the configured per-action timeout.
func (m *Manager) ActionTimeout() time.Duration {
	return m.actionTimeout
}

// touchPageLocked updates the last-used timestamp for a page. Must be called with mu held.
func (m *Manager) touchPageLocked(targetID string) {
	m.pageLastUsed[targetID] = time.Now()
}

// Start launches a local Chrome browser or connects to a remote one.
// If already connected but the connection is dead, it reconnects automatically.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Prevent reconnect after Stop()/Close() to avoid launching new containers during shutdown
	if m.closed {
		return fmt.Errorf("browser manager is closed")
	}

	profileDir := m.resolveProfileDir(ctx)
	ctx = WithProfileDir(ctx, profileDir)

	// Resolve proxy URL: static config first, then pool-based auto-assign for host mode.
	// Only auto-assign from pool if the agent has opted in via use_proxy context flag.
	proxyURL := m.proxyURL
	if useProxy, ok := useProxyFromCtx(ctx); ok && useProxy && proxyURL == "" && m.proxyMgr != nil {
		tenantID := tenantIDFromCtx(ctx)
		if tenantID == "" {
			tenantID = MasterTenantID
		}
		if proxy, err := m.proxyMgr.AssignForProfile(ctx, tenantID, profileDir, ""); err == nil {
			if fmtURL, fmtErr := m.proxyMgr.FormatURL(proxy); fmtErr == nil {
				proxyURL = fmtURL
				m.logger.Info("host mode: auto-assigned proxy from pool", "proxy", proxy.Name)
			}
		}
	}

	// If engine is already connected, check if we need to restart for proxy change.
	// Chrome --proxy-server is a launch-time flag — can't change on running process.
	if m.engine.IsConnected() {
		if proxyURL != "" && m.activeProxyURL != proxyURL {
			m.logger.Info("host mode: proxy changed, restarting browser",
				"old", m.activeProxyURL, "new", "[REDACTED]")
			m.closeTenantEnginesLocked()
			_ = m.engine.Close()
			m.resetMapsLocked()
			// Fall through to re-launch with new proxy
		} else {
			return nil
		}
	} else if len(m.pages) > 0 {
		// Connection dead — clean up and reconnect
		m.logger.Info("browser connection lost, reconnecting")
		m.closeTenantEnginesLocked()
		m.resetMapsLocked()
	}

	err := m.engine.Launch(LaunchOpts{
		Headless:   m.headless,
		ProfileDir: profileDir,
		RemoteURL:  m.remoteURL,
		BinaryPath: m.binaryPath,
		ProxyURL:   proxyURL,
	})
	if err != nil {
		return err
	}

	m.activeProxyURL = proxyURL

	// Start idle-page reaper if configured
	if m.idleTimeout > 0 && m.stopReaper == nil {
		m.stopReaper = make(chan struct{})
		go m.runReaper()
	}

	return nil
}

// Stop closes the browser (local) or disconnects (remote sidecar).
// The manager can be restarted with Start() after Stop().
func (m *Manager) Stop(ctx context.Context) error {
	return m.shutdown(false)
}

// shutdown performs the actual stop. If permanent is true, the manager cannot be restarted.
func (m *Manager) shutdown(permanent bool) error {
	// Grab and nil-out stopReaper under the lock, then close outside to avoid
	// deadlock (reaper goroutine also acquires mu).
	m.mu.Lock()
	ch := m.stopReaper
	m.stopReaper = nil
	m.closed = true // prevent Start() from reconnecting while shutdown is in progress
	m.mu.Unlock()
	if ch != nil {
		close(ch)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.closeTenantEnginesLocked()

	err := m.engine.Close()
	m.activeProxyURL = ""

	// Reset engine for next Start()
	if m.newEngine != nil {
		m.engine = m.newEngine()
	} else {
		m.engine = NewChromeEngine(m.logger)
	}
	m.resetMapsLocked()

	// Allow restart unless this is a permanent Close()
	if !permanent {
		m.closed = false
	}
	return err
}

// resetMapsLocked clears all page/session/profile tracking maps. Must be called with mu held.
func (m *Manager) resetMapsLocked() {
	m.pages = make(map[string]Page)
	m.console = make(map[string][]ConsoleMessage)
	m.pageTenants = make(map[string]string)
	m.pageAgents = make(map[string]string)
	m.pageSessionKeys = make(map[string]string)
	m.pageLastUsed = make(map[string]time.Time)
	m.pageProfiles = make(map[string]string)
	m.profileAgents = make(map[string]string)
	m.profileSessions = make(map[string]string)
	m.refs = NewRefStore()
}

// closeTenantEnginesLocked closes all incognito engine contexts. Must be called with mu held.
func (m *Manager) closeTenantEnginesLocked() {
	for tid, eng := range m.tenantEngines {
		if err := eng.Close(); err != nil {
			m.logger.Warn("failed to close tenant engine", "tenant", tid, "error", err)
		}
	}
	m.tenantEngines = make(map[string]Engine)
}

// MasterTenantID is the well-known master tenant UUID string.
// Pages opened without a tenant context or by the master tenant use the main engine directly.
const MasterTenantID = "0193a5b0-7000-7000-8000-000000000001"

// tenantEngineLocked returns an isolated incognito engine for the given tenant.
// Master tenant and empty string use the main engine (no isolation needed).
// Must be called with mu held.
func (m *Manager) tenantEngineLocked(tenantID string) (Engine, error) {
	if !m.engine.IsConnected() {
		return nil, fmt.Errorf("browser not running")
	}
	// Master tenant or no tenant: use main engine
	if tenantID == "" || tenantID == MasterTenantID {
		return m.engine, nil
	}
	// Return existing incognito engine
	if eng, ok := m.tenantEngines[tenantID]; ok {
		return eng, nil
	}
	// Create new incognito context for this tenant
	inc, err := m.engine.Incognito()
	if err != nil {
		return nil, fmt.Errorf("create incognito context for tenant %s: %w", tenantID, err)
	}
	m.tenantEngines[tenantID] = inc
	m.logger.Info("created incognito engine context", "tenant", tenantID)
	return inc, nil
}

// Status returns current browser status.
func (m *Manager) Status() *StatusInfo {
	m.mu.Lock()
	engine := m.engine
	headless := m.headless
	m.mu.Unlock()

	if !engine.IsConnected() {
		return &StatusInfo{Running: false}
	}

	pages, _ := engine.Pages()
	info := &StatusInfo{
		Running:  true,
		Tabs:     len(pages),
		Engine:   engine.Name(),
		Headless: &headless,
	}
	if len(pages) > 0 {
		if pageInfo, err := pages[0].Info(); err == nil && pageInfo != nil {
			info.URL = pageInfo.URL
		}
	}
	return info
}

// resolveProfileDir builds the Chrome profile directory path from workspace + tenant + profile.
// Profile name is read from context first (per-request), then falls back to Manager's activeProfile.
func (m *Manager) resolveProfileDir(ctx context.Context) string {
	if m.workspace == "" {
		return ""
	}
	tenantID := tenantIDFromCtx(ctx)
	if tenantID == "" {
		tenantID = "default"
	}
	// Per-request profile from context takes priority over global activeProfile
	profile := profileNameFromCtx(ctx)
	if profile == "" {
		profile = m.activeProfile
	}
	if profile == "" {
		profile = "default"
	}
	return filepath.Join(m.workspace, "browser", "profiles", tenantID, profile)
}

// Close permanently shuts down the browser. The manager cannot be restarted after Close().
func (m *Manager) Close() error {
	return m.shutdown(true)
}

// PageByTargetID returns the Page for a target ID, or nil if not found.
// Used by LiveView handler to access pages without tenant context.
func (m *Manager) PageByTargetID(targetID string) Page {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pages[targetID]
}

// Refs returns the RefStore for external use (e.g. actions).
func (m *Manager) Refs() *RefStore {
	return m.refs
}

// ViewportSize returns the configured default viewport dimensions.
func (m *Manager) ViewportSize() (int, int) {
	return m.viewportWidth, m.viewportHeight
}

// SetProxyManager wires a ProxyManager for proxy pool rotation.
// For ContainerPoolEngine: enables per-container proxy assignment.
// For host mode (ChromeEngine): enables pool-based proxy at browser start.
func (m *Manager) SetProxyManager(pm *ProxyManager, tenantID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proxyMgr = pm
	if pool, ok := m.engine.(*ContainerPoolEngine); ok {
		pool.mu.Lock()
		pool.proxyMgr = pm
		pool.tenantID = tenantID
		pool.mu.Unlock()
	}
}
