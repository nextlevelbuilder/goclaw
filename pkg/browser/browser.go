package browser

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// Manager handles the Chrome browser lifecycle and page management.
type Manager struct {
	mu        sync.Mutex
	browser   *rod.Browser
	refs      *RefStore
	pages     map[string]*rod.Page        // targetID → page
	console   map[string][]ConsoleMessage // targetID → console messages
	headless  bool
	remoteURL string // CDP endpoint for remote Chrome (sidecar); skips local launcher
	logger    *slog.Logger
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

// New creates a Manager with options.
func New(opts ...Option) *Manager {
	m := &Manager{
		refs:    NewRefStore(),
		pages:   make(map[string]*rod.Page),
		console: make(map[string][]ConsoleMessage),
		logger:  slog.Default(),
	}
	for _, o := range opts {
		o(m)
	}
	return m
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
		m.browser = nil
		m.pages = make(map[string]*rod.Page)
		m.console = make(map[string][]ConsoleMessage)
		m.refs = NewRefStore()
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
		// Local Chrome — launch via rod launcher
		l := launcher.New().
			Headless(m.headless).
			Set("disable-gpu").
			Set("no-first-run").
			Set("no-default-browser-check")

		u, err := l.Launch()
		if err != nil {
			return fmt.Errorf("launch Chrome: %w", err)
		}
		controlURL = u
		m.logger.Info("Chrome launched", "cdp", controlURL, "headless", m.headless)
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("connect to Chrome: %w", err)
	}

	m.browser = b
	return nil
}

// Stop closes the Chrome browser (local) or disconnects (remote sidecar).
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return nil
	}

	var err error
	if m.remoteURL == "" {
		// Local Chrome — close the browser process
		err = m.browser.Close()
	}
	// Remote Chrome — just drop the connection; sidecar stays alive

	m.browser = nil
	m.pages = make(map[string]*rod.Page)
	m.console = make(map[string][]ConsoleMessage)
	return err
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
