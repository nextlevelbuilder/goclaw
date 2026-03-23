package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// reconnectLocked re-establishes the CDP connection to a remote Chrome.
// Must be called with m.mu held. Only works when remoteURL is set.
func (m *Manager) reconnectLocked() error {
	m.closeTenantContextsLocked()
	m.browser = nil
	m.pages = make(map[string]*rod.Page)
	m.console = make(map[string][]ConsoleMessage)
	m.pageTenants = make(map[string]string)
	m.refs = NewRefStore()

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

// getPageForTenant wraps getPage with tenant ownership validation.
// If tenantID is set and the page belongs to a different tenant, access is denied.
// Must be called with m.mu held.
func (m *Manager) getPageForTenant(targetID, tenantID string) (*rod.Page, error) {
	page, err := m.getPage(targetID)
	if err != nil {
		return nil, err
	}
	// If no tenant context or master tenant, allow access to all pages
	if tenantID == "" || tenantID == MasterTenantID {
		return page, nil
	}
	// Check ownership: page must belong to this tenant
	resolvedTID := targetID
	if targetID == "" {
		resolvedTID = string(page.TargetID)
	}
	if owner, ok := m.pageTenants[resolvedTID]; ok && owner != tenantID {
		return nil, fmt.Errorf("tab not found: %s", targetID)
	}
	return page, nil
}

// setupConsoleListener attaches a console message listener to a page via Rod's EachEvent.
func (m *Manager) setupConsoleListener(page *rod.Page, targetID string) {
	go page.EachEvent(func(e *proto.RuntimeConsoleAPICalled) {
		var text strings.Builder
		for _, arg := range e.Args {
			s := arg.Value.String()
			if s != "" && s != "null" {
				text.WriteString(s + " ")
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
			Text:  text.String(),
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

// getPageAndResolve is a helper that locks, gets page with tenant check, and resolves an element.
func (m *Manager) getPageAndResolve(ctx context.Context, targetID, ref string) (*rod.Page, *rod.Element, error) {
	tenantID := tenantIDFromCtx(ctx)
	m.mu.Lock()
	page, err := m.getPageForTenant(targetID, tenantID)
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
