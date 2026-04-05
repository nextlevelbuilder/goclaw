package browser

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// ValidateProxyURL checks that a proxy URL has a valid scheme and host,
// and does not contain Chrome flag injection attempts.
func ValidateProxyURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("proxy URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid proxy URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks4":
		// OK
	default:
		return fmt.Errorf("unsupported proxy scheme %q (use http, https, socks4, or socks5)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("proxy URL missing host")
	}
	// Block Chrome flag injection (e.g. "http://evil.com --remote-debugging-address=0.0.0.0")
	if strings.Contains(raw, " --") || strings.ContainsAny(raw, "\n\r") {
		return fmt.Errorf("proxy URL contains invalid characters")
	}
	return nil
}

// ValidateCDPURL checks that a CDP WebSocket URL is safe to connect to.
// Blocks private/loopback IPs to prevent SSRF attacks.
func ValidateCDPURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("CDP URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid CDP URL: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("CDP URL must use ws:// or wss:// scheme, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("CDP URL missing host")
	}
	// Block loopback and private IP ranges
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("SSRF blocked: cannot attach to private/loopback address %s", host)
		}
	} else {
		// Hostname — resolve and check
		if host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
			return fmt.Errorf("SSRF blocked: cannot attach to local hostname %q", host)
		}
		ips, resolveErr := net.LookupHost(host)
		if resolveErr == nil {
			for _, resolved := range ips {
				rIP := net.ParseIP(resolved)
				if rIP != nil && (rIP.IsLoopback() || rIP.IsPrivate() || rIP.IsLinkLocalUnicast()) {
					return fmt.Errorf("SSRF blocked: %s resolves to private address %s", host, resolved)
				}
			}
		}
	}
	return nil
}

// ValidateBrowserURL checks that a URL is safe for the browser to navigate to.
// Only allows http:// and https:// schemes (and about:blank).
func ValidateBrowserURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("URL is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		return nil
	case "about":
		if u.Opaque == "blank" || u.Path == "blank" {
			return nil
		}
		return fmt.Errorf("only about:blank is allowed, got about:%s", u.Path)
	default:
		return fmt.Errorf("URL scheme %q not allowed (use http:// or https://)", u.Scheme)
	}
}

// ValidateExtensionPath checks that an extension path is within the allowed workspace.
func ValidateExtensionPath(extensionPath, workspace string) error {
	if extensionPath == "" {
		return fmt.Errorf("extension path is empty")
	}
	cleaned := filepath.Clean(extensionPath)
	if workspace == "" {
		// No workspace — block all absolute paths and path traversal
		if filepath.IsAbs(cleaned) || strings.Contains(cleaned, "..") {
			return fmt.Errorf("extension path must be relative and within workspace")
		}
		return nil
	}
	wsAbs, _ := filepath.Abs(workspace)
	pathAbs, _ := filepath.Abs(cleaned)
	if !strings.HasPrefix(pathAbs, wsAbs+string(filepath.Separator)) && pathAbs != wsAbs {
		return fmt.Errorf("extension path must be within workspace %q", workspace)
	}
	return nil
}
