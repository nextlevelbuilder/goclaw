package browser

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/go-rod/rod"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func normalizeBrowserHostname(hostname string) string {
	return strings.ToLower(strings.TrimSpace(hostname))
}

func matchesBrowserHostnameAllowlist(hostname string, patterns []string) bool {
	normalizedHostname := normalizeBrowserHostname(hostname)
	for _, pattern := range patterns {
		normalizedPattern := normalizeBrowserHostname(pattern)
		if normalizedPattern == "" {
			continue
		}
		if normalizedPattern == normalizedHostname {
			return true
		}
		if strings.HasPrefix(normalizedPattern, "*.") {
			suffix := strings.TrimPrefix(normalizedPattern, "*")
			if strings.HasSuffix(normalizedHostname, suffix) && normalizedHostname != strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}

func isBrowserHostnameExplicitlyAllowed(hostname string, policy SSRFPolicy) bool {
	normalizedHostname := normalizeBrowserHostname(hostname)
	if normalizedHostname == "" {
		return false
	}
	for _, allowed := range policy.AllowedHostnames {
		if normalizeBrowserHostname(allowed) == normalizedHostname {
			return true
		}
	}
	return matchesBrowserHostnameAllowlist(normalizedHostname, policy.HostnameAllowlist)
}

// validateBrowserTargetURL enforces the browser navigation policy for explicit
// open/navigate requests.
func validateBrowserTargetURL(rawURL string, policy SSRFPolicy) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	switch parsed.Scheme {
	case "http", "https":
		if parsed.Host == "" {
			return fmt.Errorf("missing hostname in URL")
		}
		if isBrowserHostnameExplicitlyAllowed(parsed.Hostname(), policy) {
			return nil
		}
		if policy.AllowPrivateNetwork {
			return nil
		}
		if err := tools.CheckSSRF(rawURL); err != nil {
			return fmt.Errorf("SSRF protection: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("only http and https URLs are supported")
	}
}

// validateBrowserObservedURL validates a page URL after navigation or an
// interaction-triggered redirect. It only enforces SSRF checks on network URLs.
func validateBrowserObservedURL(rawURL string, policy SSRFPolicy) error {
	if rawURL == "" {
		return nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}

	switch parsed.Scheme {
	case "http", "https":
		return validateBrowserTargetURL(rawURL, policy)
	case "file":
		return fmt.Errorf("file URLs are not allowed")
	default:
		return nil
	}
}

// pageCurrentURL reads the current URL from a Rod page.
func pageCurrentURL(page *rod.Page) (string, error) {
	info, err := page.Info()
	if err != nil {
		return "", fmt.Errorf("read page info: %w", err)
	}
	if info == nil {
		return "", fmt.Errorf("page info unavailable")
	}
	return info.URL, nil
}

// pageTargetID resolves the target ID for a page, falling back to the page's
// Rod target ID when the caller did not specify one.
func pageTargetID(targetID string, page *rod.Page) string {
	if targetID != "" {
		return targetID
	}
	if page == nil {
		return ""
	}
	return string(page.TargetID)
}

// removePageLocked removes a page and all cached bookkeeping. Must be called
// with m.mu held.
func (m *Manager) removePageLocked(targetID string, page *rod.Page) {
	if page != nil {
		_ = page.Close()
	}
	delete(m.pages, targetID)
	delete(m.console, targetID)
	delete(m.pageTenants, targetID)
	delete(m.pageLastUsed, targetID)
	m.refs.Remove(targetID)
}

// guardPageURLLocked validates an already-read page URL. Must be called with
// m.mu held.
func (m *Manager) guardPageURLLocked(targetID, currentURL string, page *rod.Page) error {
	if err := validateBrowserObservedURL(currentURL, m.ssrfPolicy); err != nil {
		m.removePageLocked(targetID, page)
		return fmt.Errorf("blocked browser navigation to %q: %w", currentURL, err)
	}
	return nil
}

// ensurePageURLAllowed reads the current page URL and applies the browser URL
// policy. It cleans up the page if validation fails.
func (m *Manager) ensurePageURLAllowed(targetID string, page *rod.Page) error {
	currentURL, err := pageCurrentURL(page)
	if err != nil {
		m.mu.Lock()
		m.removePageLocked(targetID, page)
		m.mu.Unlock()
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.guardPageURLLocked(targetID, currentURL, page)
}
