package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
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

// ListTabs returns all open tabs.
func (m *Manager) ListTabs(ctx context.Context) ([]TabInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}

	pages, err := m.browser.Pages()
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}

	tabs := make([]TabInfo, 0, len(pages))
	for _, p := range pages {
		info, err := p.Info()
		if err != nil || info == nil {
			continue
		}
		tid := string(p.TargetID)
		m.pages[tid] = p
		tabs = append(tabs, TabInfo{
			TargetID: tid,
			URL:      info.URL,
			Title:    info.Title,
		})
	}
	return tabs, nil
}

// OpenTab opens a new tab with the given URL.
func (m *Manager) OpenTab(ctx context.Context, url string) (*TabInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}

	page, err := m.browser.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("open tab: %w", err)
	}

	if err := page.WaitStable(300 * time.Millisecond); err != nil {
		return nil, fmt.Errorf("wait stable: %w", err)
	}
	info, _ := page.Info()
	tid := string(page.TargetID)
	m.pages[tid] = page

	// Set up console listener
	m.setupConsoleListener(page, tid)

	tab := &TabInfo{TargetID: tid, URL: url}
	if info != nil {
		tab.URL = info.URL
		tab.Title = info.Title
	}
	return tab, nil
}

// FocusTab activates a tab.
func (m *Manager) FocusTab(ctx context.Context, targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.getPage(targetID)
	if err != nil {
		return err
	}

	_, err = page.Activate()
	return err
}

// CloseTab closes a tab.
func (m *Manager) CloseTab(ctx context.Context, targetID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	page, err := m.getPage(targetID)
	if err != nil {
		return err
	}

	delete(m.pages, targetID)
	delete(m.console, targetID)
	return page.Close()
}

// ConsoleMessages returns captured console messages for a tab.
func (m *Manager) ConsoleMessages(targetID string) []ConsoleMessage {
	m.mu.Lock()
	defer m.mu.Unlock()

	msgs := m.console[targetID]
	if msgs == nil {
		return []ConsoleMessage{}
	}

	// Return copy and clear
	result := make([]ConsoleMessage, len(msgs))
	copy(result, msgs)
	m.console[targetID] = nil
	return result
}

// Snapshot takes an accessibility snapshot of a page.
func (m *Manager) Snapshot(ctx context.Context, targetID string, opts SnapshotOptions) (*SnapshotResult, error) {
	m.mu.Lock()
	page, err := m.getPage(targetID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	result, err := proto.AccessibilityGetFullAXTree{}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("get AX tree: %w", err)
	}

	snap := FormatSnapshot(result.Nodes, opts)
	info, _ := page.Info()
	snap.TargetID = targetID
	if info != nil {
		snap.URL = info.URL
		snap.Title = info.Title
	}

	// Cache refs
	m.refs.Store(targetID, snap.Refs)

	return snap, nil
}

// Screenshot captures a page screenshot as PNG bytes.
func (m *Manager) Screenshot(ctx context.Context, targetID string, fullPage bool) ([]byte, error) {
	m.mu.Lock()
	page, err := m.getPage(targetID)
	m.mu.Unlock()

	if err != nil {
		return nil, err
	}

	if fullPage {
		return page.Screenshot(fullPage, &proto.PageCaptureScreenshot{
			Format: proto.PageCaptureScreenshotFormatPng,
		})
	}
	return page.Screenshot(false, nil)
}

// Navigate navigates a page to a URL.
func (m *Manager) Navigate(ctx context.Context, targetID, url string) error {
	m.mu.Lock()
	page, err := m.getPage(targetID)
	m.mu.Unlock()

	if err != nil {
		return err
	}

	if err := page.Navigate(url); err != nil {
		return fmt.Errorf("navigate: %w", err)
	}
	if err := page.WaitStable(300 * time.Millisecond); err != nil {
		return fmt.Errorf("wait stable after navigate: %w", err)
	}
	return nil
}

// Close shuts down the browser if running.
func (m *Manager) Close() error {
	return m.Stop(context.Background())
}

// Refs returns the RefStore for external use (e.g. actions).
func (m *Manager) Refs() *RefStore {
	return m.refs
}

// reconnectLocked re-establishes the CDP connection to a remote Chrome.
// Must be called with m.mu held. Only works when remoteURL is set.
func (m *Manager) reconnectLocked() error {
	m.browser = nil
	m.pages = make(map[string]*rod.Page)
	m.console = make(map[string][]ConsoleMessage)

	controlURL, err := resolveRemoteCDP(m.remoteURL)
	if err != nil {
		return err
	}

	b := rod.New().ControlURL(controlURL)
	if err := b.Connect(); err != nil {
		return err
	}
	m.browser = b
	return nil
}

// getPage looks up a page by targetID. If targetID is empty, returns the first available page.
// Must be called with m.mu held. If the connection is dead and remoteURL is set,
// it attempts one automatic reconnect.
func (m *Manager) getPage(targetID string) (*rod.Page, error) {
	if m.browser == nil {
		return nil, fmt.Errorf("browser not running")
	}

	// If targetID specified, look in cache first
	if targetID != "" {
		if p, ok := m.pages[targetID]; ok {
			return p, nil
		}
	}

	// Refresh page list from browser
	pages, err := m.browser.Pages()
	if err != nil {
		// Connection dead — try auto-reconnect for remote Chrome
		if m.remoteURL != "" {
			if reconnErr := m.reconnectLocked(); reconnErr != nil {
				return nil, fmt.Errorf("list pages: %w (reconnect also failed: %v)", err, reconnErr)
			}
			m.logger.Info("auto-reconnected to remote Chrome")
			pages, err = m.browser.Pages()
			if err != nil {
				return nil, fmt.Errorf("list pages after reconnect: %w", err)
			}
		} else {
			return nil, fmt.Errorf("list pages: %w", err)
		}
	}

	// Update cache
	for _, p := range pages {
		tid := string(p.TargetID)
		m.pages[tid] = p
	}

	if targetID != "" {
		if p, ok := m.pages[targetID]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("tab not found: %s", targetID)
	}

	// No targetID: return first page
	if len(pages) == 0 {
		return nil, fmt.Errorf("no tabs open")
	}
	return pages[0], nil
}

// setupConsoleListener attaches a console message listener to a page via Rod's EachEvent.
func (m *Manager) setupConsoleListener(page *rod.Page, targetID string) {
	go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
		var text string
		for _, arg := range e.Args {
			s := arg.Value.String()
			if s != "" && s != "null" {
				text += s + " "
			}
		}

		level := "log"
		switch e.Type {
		case proto.RuntimeConsoleAPICalledTypeWarning:
			level = "warn"
		case proto.RuntimeConsoleAPICalledTypeError:
			level = "error"
		case proto.RuntimeConsoleAPICalledTypeInfo:
			level = "info"
		}

		m.mu.Lock()
		msgs := m.console[targetID]
		if len(msgs) >= 500 {
			msgs = msgs[1:]
		}
		m.console[targetID] = append(msgs, ConsoleMessage{
			Level: level,
			Text:  text,
		})
		m.mu.Unlock()
	})()
}

// resolveElement converts a RoleRef to a Rod Element via backendNodeID.
func (m *Manager) resolveElement(page *rod.Page, targetID, ref string) (*rod.Element, error) {
	roleRef, ok := m.refs.Resolve(targetID, ref)
	if !ok {
		return nil, fmt.Errorf("unknown ref %q — take a new snapshot first", ref)
	}

	if roleRef.BackendNodeID == 0 {
		return nil, fmt.Errorf("no backendNodeID for ref %q", ref)
	}

	backendID := proto.DOMBackendNodeID(roleRef.BackendNodeID)
	resolved, err := proto.DOMResolveNode{BackendNodeID: backendID}.Call(page)
	if err != nil {
		return nil, fmt.Errorf("resolve DOM node for %q (backendNodeID=%d): %w", ref, roleRef.BackendNodeID, err)
	}

	el, err := page.ElementFromObject(resolved.Object)
	if err != nil {
		return nil, fmt.Errorf("get element from object for %q: %w", ref, err)
	}

	return el, nil
}

// getPageAndResolve is a helper that locks, gets page, and resolves an element.
func (m *Manager) getPageAndResolve(targetID, ref string) (*rod.Page, *rod.Element, error) {
	m.mu.Lock()
	page, err := m.getPage(targetID)
	m.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}

	// Ensure DOM is enabled for node resolution
	_ = proto.DOMEnable{}.Call(page)

	el, err := m.resolveElement(page, targetID, NormalizeRef(ref))
	if err != nil {
		return nil, nil, err
	}

	return page, el, nil
}

// waitStable waits for page to become stable (no network/DOM activity).
func waitStable(page *rod.Page) {
	_ = page.WaitStable(300 * time.Millisecond)
}

// resolveRemoteCDP queries a Chrome endpoint's /json/version to get the CDP
// WebSocket URL, resolving the hostname to an IP address.
//
// Chrome (M113+) rejects HTTP/WebSocket requests where the Host header is a
// hostname (not an IP or "localhost") to prevent DNS rebinding attacks.
// In Docker, the service name "chrome" is a hostname, so we resolve it to an
// IP address and use that for all connections.

// cdpHTTPClient is used for /json/version queries with a reasonable timeout.
var cdpHTTPClient = &http.Client{Timeout: 10 * time.Second}

func resolveRemoteCDP(remoteURL string) (string, error) {
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", fmt.Errorf("parse remote URL: %w", err)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "9222"
	}

	// Resolve hostname to IP — Chrome M113+ requires IP or "localhost" in
	// the Host header to prevent DNS rebinding attacks.
	ip, err := resolveToIPv4(host)
	if err != nil {
		return "", err
	}

	// Query /json/version using the IP (so Host header is an IP).
	versionURL := fmt.Sprintf("http://%s:%s/json/version", ip, port)
	resp, err := cdpHTTPClient.Get(versionURL) //nolint:gosec // resolved from user-configured URL
	if err != nil {
		return "", fmt.Errorf("query /json/version at %s: %w", versionURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/json/version returned HTTP %d", resp.StatusCode)
	}

	var ver struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return "", fmt.Errorf("parse /json/version: %w", err)
	}
	if ver.WebSocketDebuggerURL == "" {
		return "", fmt.Errorf("empty webSocketDebuggerUrl in /json/version response")
	}

	// Replace host in returned URL with the resolved IP.
	// Chrome returns ws://127.0.0.1/... but we need ws://<container-IP>:<port>/...
	wsURL, err := url.Parse(ver.WebSocketDebuggerURL)
	if err != nil {
		return "", fmt.Errorf("parse webSocketDebuggerUrl: %w", err)
	}
	wsURL.Host = net.JoinHostPort(ip, port)
	return wsURL.String(), nil
}

// resolveToIPv4 resolves a hostname to an IPv4 address.
// Chrome typically binds on 0.0.0.0 (IPv4), so we prefer IPv4 to avoid
// connection failures when DNS returns IPv6 addresses first.
// If the host is already an IP, it is returned as-is.
func resolveToIPv4(host string) (string, error) {
	// Already an IP literal — return as-is.
	if net.ParseIP(host) != nil {
		return host, nil
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}

	// Prefer IPv4.
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip, nil
		}
	}

	// Fallback: return first address (could be IPv6).
	if len(ips) > 0 {
		return ips[0], nil
	}
	return "", fmt.Errorf("no addresses found for %s", host)
}
