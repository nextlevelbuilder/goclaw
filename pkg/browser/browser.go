package browser

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Manager handles the Chrome browser lifecycle and page management.
type Manager struct {
	mu          sync.Mutex
	browser     *rod.Browser
	launcher    *launcher.Launcher // retained for PID-based cleanup on crash
	refs        *RefStore
	pages       map[string]*rod.Page        // targetID → page
	console     map[string][]ConsoleMessage // targetID → console messages
	tenantCtxs  map[string]*rod.Browser     // tenantID → incognito browser context
	pageTenants map[string]string           // targetID → tenantID (for filtering)
	pageLastUsed map[string]time.Time       // targetID → last access time
	headless      bool
	leakless      bool          // use leakless wrapper to prevent zombie processes (default true)
	remoteURL     string        // CDP endpoint for remote Chrome (sidecar); skips local launcher
	actionTimeout time.Duration // per-action context timeout (default 30s)
	idleTimeout   time.Duration // auto-close pages idle longer than this (default 10m, 0=disabled)
	maxPages      int           // max open pages per tenant (default 5)
	stopReaper    chan struct{} // signal to stop the reaper goroutine
	logger        *slog.Logger
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

// WithLeakless sets whether to use the leakless wrapper to prevent zombie processes.
// Default is true. Set to false on Windows if blocked by antivirus.
func WithLeakless(enabled bool) Option {
	return func(m *Manager) { m.leakless = enabled }
}

// New creates a Manager with options.
func New(opts ...Option) *Manager {
	m := &Manager{
		refs:          NewRefStore(),
		pages:         make(map[string]*rod.Page),
		console:       make(map[string][]ConsoleMessage),
		tenantCtxs:    make(map[string]*rod.Browser),
		pageTenants:   make(map[string]string),
		pageLastUsed:  make(map[string]time.Time),
		actionTimeout: 30 * time.Second,
		idleTimeout:   10 * time.Minute,
		maxPages:      5,
		leakless:      true, // default true, set false on Windows if blocked by antivirus
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

// touchPageLocked updates the last-used timestamp for a page. Must be called with mu held.
func (m *Manager) touchPageLocked(targetID string) {
	m.pageLastUsed[targetID] = time.Now()
}

// Start launches a local Chrome browser or connects to a remote one.
// If already connected but the connection is dead, it reconnects automatically.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

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
		// Remote Chrome sidecar — query /json/version and fix host for Docker networking
		u, err := resolveRemoteCDP(m.remoteURL)
		if err != nil {
			return fmt.Errorf("resolve remote Chrome at %s: %w", m.remoteURL, err)
		}
		controlURL = u
		m.logger.Info("connecting to remote Chrome", "cdp", controlURL, "remote", m.remoteURL)
	} else {
		// Local browser — launch via rod launcher with stability flags
		launchCtx, launchCancel := context.WithTimeout(ctx, 30*time.Second)
		defer launchCancel()

		// Use isolated user data directory to avoid conflicts with user's browser
		userDataDir := filepath.Join(os.TempDir(), "goclaw-browser-profile")

		l := launcher.New().
			Context(launchCtx).
			Leakless(m.leakless).
			Headless(m.headless).
			UserDataDir(userDataDir).
			Set("no-sandbox"). // Required on Windows to prevent browser crash
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

		// On Windows, prefer Edge over Chrome (more stable with CDP)
		browserName := "Chrome"
		if edgePath := findEdgePath(); edgePath != "" {
			l = l.Bin(edgePath)
			browserName = "Edge"
		}

		u, err := l.Launch()
		if err != nil {
			return fmt.Errorf("launch %s: %w", browserName, err)
		}
		controlURL = u
		m.launcher = l
		m.logger.Info("browser launched", "browser", browserName, "cdp", controlURL, "headless", m.headless, "pid", l.PID())
	}

	// Connect with timeout, but keep browser alive with Background context after connect.
	// We use a channel to implement connect timeout without binding context to browser.
	b := rod.New().ControlURL(controlURL)

	connectDone := make(chan error, 1)
	go func() {
		connectDone <- b.Connect()
	}()

	select {
	case err := <-connectDone:
		if err != nil {
			if m.launcher != nil {
				m.launcher.Kill()
				m.launcher.Cleanup()
				m.launcher = nil
			}
			return fmt.Errorf("connect to browser: %w", err)
		}
	case <-time.After(15 * time.Second):
		if m.launcher != nil {
			m.launcher.Kill()
			m.launcher.Cleanup()
			m.launcher = nil
		}
		return fmt.Errorf("connect to browser: timeout after 15s")
	case <-ctx.Done():
		if m.launcher != nil {
			m.launcher.Kill()
			m.launcher.Cleanup()
			m.launcher = nil
		}
		return fmt.Errorf("connect to browser: %w", ctx.Err())
	}

	// NOTE: Do NOT use b.Timeout() here - it causes context conflicts with per-operation timeouts.
	// Each operation (OpenTab, Navigate, etc.) applies its own context timeout from the tool call.
	m.browser = b
	m.logger.Info("browser connected")

	// Start idle-page reaper if configured
	if m.idleTimeout > 0 && m.stopReaper == nil {
		m.stopReaper = make(chan struct{})
		go m.runReaper()
	}

	return nil
}

// Stop closes the Chrome browser (local) or disconnects (remote sidecar).
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

	if m.browser == nil {
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
	m.pages = make(map[string]*rod.Page)
	m.console = make(map[string][]ConsoleMessage)
	m.pageTenants = make(map[string]string)
	m.pageLastUsed = make(map[string]time.Time)
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
	m.browser = nil
	m.pages = make(map[string]*rod.Page)
	m.console = make(map[string][]ConsoleMessage)
	m.pageTenants = make(map[string]string)
	m.pageLastUsed = make(map[string]time.Time)
	m.refs = NewRefStore()
}

// MasterTenantID is the well-known master tenant UUID string.
// Pages opened without a tenant context or by the master tenant use the main browser directly.
const MasterTenantID = "0193a5b0-7000-7000-8000-000000000001"

// tenantBrowserLocked returns an isolated incognito browser context for the given tenant.
// Master tenant and empty string use the main browser (no isolation needed).
// Must be called with mu held.
func (m *Manager) tenantBrowserLocked(tenantID string) (*rod.Browser, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}
	// Master tenant or no tenant: use main browser
	if tenantID == "" || tenantID == MasterTenantID {
		return m.browser, nil
	}
	// Return existing incognito context
	if ctx, ok := m.tenantCtxs[tenantID]; ok {
		return ctx, nil
	}
	// Create new incognito context for this tenant
	incognito, err := m.browser.Incognito()
	if err != nil {
		return nil, fmt.Errorf("create incognito context for tenant %s: %w", tenantID, err)
	}
	m.tenantCtxs[tenantID] = incognito
	m.logger.Info("created incognito browser context", "tenant", tenantID)
	return incognito, nil
}

// Status returns current browser status.
func (m *Manager) Status() *StatusInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return &StatusInfo{Running: false}
	}

	pages, _ := m.browser.Pages()
	info := &StatusInfo{
		Running: true,
		Tabs:    len(pages),
	}
	if len(pages) > 0 {
		if pageInfo, err := pages[0].Info(); err == nil {
			info.URL = pageInfo.URL
		}
	}
	return info
}

// findEdgePath returns the path to Microsoft Edge executable on Windows, or empty string if not found.
func findEdgePath() string {
	paths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
