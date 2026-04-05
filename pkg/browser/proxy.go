package browser

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxProfilesPerProxy is the maximum number of profiles that can share a single proxy
// before the sticky assignment logic picks a different one.
const maxProfilesPerProxy = 5

// ProxyManager manages the proxy pool: assignment, rotation, health checks.
type ProxyManager struct {
	store       store.BrowserProxyStore
	assignStore store.BrowserProxyAssignmentStore
	encryptKey  string
	logger      *slog.Logger
	mu          sync.Mutex
	rrIndex     map[string]int // tenantID+geo → round-robin index
}

// NewProxyManager creates a ProxyManager.
func NewProxyManager(s store.BrowserProxyStore, encryptKey string, l *slog.Logger) *ProxyManager {
	if l == nil {
		l = slog.Default()
	}
	return &ProxyManager{
		store:      s,
		encryptKey: encryptKey,
		logger:     l,
		rrIndex:    make(map[string]int),
	}
}

// SetAssignmentStore wires the sticky assignment store (called after stores init).
func (pm *ProxyManager) SetAssignmentStore(as store.BrowserProxyAssignmentStore) {
	pm.assignStore = as
}

// Assign picks a healthy+enabled proxy matching geo hint using round-robin.
func (pm *ProxyManager) Assign(ctx context.Context, tenantID, geo string) (*store.BrowserProxy, error) {
	proxies, err := pm.store.ListHealthy(ctx, tenantID, geo)
	if err != nil {
		return nil, fmt.Errorf("list healthy proxies: %w", err)
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no healthy proxies available (tenant=%s, geo=%s)", tenantID, geo)
	}

	pm.mu.Lock()
	key := tenantID + ":" + geo
	idx := pm.rrIndex[key] % len(proxies)
	pm.rrIndex[key] = idx + 1
	pm.mu.Unlock()

	return proxies[idx], nil
}

// AssignForProfile returns a sticky proxy for the given profile.
// If an existing assignment exists and the proxy is still healthy+enabled (and not overloaded),
// it reuses the same proxy. Otherwise picks a new one via round-robin.
func (pm *ProxyManager) AssignForProfile(ctx context.Context, tenantID, profileDir, geo string) (*store.BrowserProxy, error) {
	if pm.assignStore != nil {
		existing, err := pm.assignStore.GetByProfile(ctx, tenantID, profileDir)
		if err == nil && existing != nil {
			// Check if assigned proxy is still usable
			proxy, getErr := pm.store.Get(ctx, existing.ProxyID)
			if getErr == nil && proxy != nil && proxy.IsHealthy && proxy.IsEnabled {
				count, cErr := pm.assignStore.CountByProxy(ctx, proxy.ID)
				if cErr == nil && count < maxProfilesPerProxy {
					// Reuse — update last_used_at
					_ = pm.assignStore.Upsert(ctx, &store.ProxyProfileAssignment{
						TenantID:   tenantID,
						ProxyID:    proxy.ID,
						ProfileDir: profileDir,
					})
					pm.logger.Debug("sticky proxy reused", "profile", profileDir, "proxy", proxy.Name)
					return proxy, nil
				}
			}
			// Proxy gone/unhealthy/disabled/overloaded — fall through to re-assign
		} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
			pm.logger.Warn("lookup sticky assignment failed", "error", err)
		}
	}

	// Pick new proxy via round-robin
	proxy, err := pm.Assign(ctx, tenantID, geo)
	if err != nil {
		return nil, err
	}

	// Save sticky assignment
	if pm.assignStore != nil {
		if uErr := pm.assignStore.Upsert(ctx, &store.ProxyProfileAssignment{
			TenantID:   tenantID,
			ProxyID:    proxy.ID,
			ProfileDir: profileDir,
		}); uErr != nil {
			pm.logger.Warn("save sticky assignment failed", "error", uErr)
		}
	}

	pm.logger.Debug("proxy assigned to profile", "profile", profileDir, "proxy", proxy.Name)
	return proxy, nil
}

// SetEnabled toggles a proxy's enabled state (scoped to tenant).
func (pm *ProxyManager) SetEnabled(ctx context.Context, id, tenantID string, enabled bool) error {
	return pm.store.SetEnabled(ctx, id, tenantID, enabled)
}

// FormatURL returns "protocol://user:pass@host:port" with decrypted password.
func (pm *ProxyManager) FormatURL(p *store.BrowserProxy) (string, error) {
	proxyURL, user, pass, err := pm.FormatURLAndCreds(p)
	if err != nil {
		return "", err
	}
	if user == "" {
		return proxyURL, nil
	}
	// Reconstruct full URL with credentials for backward compat callers.
	parsed, parseErr := url.Parse(proxyURL)
	if parseErr != nil {
		return proxyURL, nil
	}
	parsed.User = url.UserPassword(user, pass)
	return parsed.String(), nil
}

// FormatURLAndCreds returns the proxy URL without credentials and the decrypted
// username/password separately. The URL is suitable for Chrome's --proxy-server
// flag (which does not support credentials in the URL).
func (pm *ProxyManager) FormatURLAndCreds(p *store.BrowserProxy) (proxyURL, username, password string, err error) {
	raw := p.URL
	// Auto-prefix http:// if no scheme — prevents url.Parse from misinterpreting
	// "host:port" as "scheme:opaque" (Go treats text before ':' as scheme).
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	parsed, parseErr := url.Parse(raw)
	if parseErr != nil {
		return raw, "", "", nil
	}

	// Strip any userinfo that might be in the URL itself
	parsed.User = nil
	proxyURL = parsed.String()

	if p.Username != "" {
		username = p.Username
		password = p.Password
		if password != "" && pm.encryptKey != "" {
			decrypted, decErr := crypto.Decrypt(password, pm.encryptKey)
			if decErr != nil {
				return "", "", "", fmt.Errorf("decrypt proxy password: %w", decErr)
			}
			password = decrypted
		}
	}

	return proxyURL, username, password, nil
}

// RunHealthCheck pings all proxies for a tenant and updates health status.
func (pm *ProxyManager) RunHealthCheck(ctx context.Context, tenantID string) error {
	proxies, err := pm.store.List(ctx, tenantID)
	if err != nil {
		return err
	}

	for _, p := range proxies {
		healthy := pm.checkProxy(p)
		failCount := 0
		if !healthy {
			failCount = p.FailCount + 1
		}
		// Mark unhealthy after 3 consecutive failures
		isHealthy := failCount < 3
		if healthy {
			isHealthy = true
			failCount = 0
		}

		if err := pm.store.UpdateHealth(ctx, p.ID, isHealthy, failCount); err != nil {
			pm.logger.Warn("failed to update proxy health", "proxy", p.Name, "error", err)
		}
	}
	return nil
}

// checkProxy attempts a TCP connection to the proxy to verify reachability.
func (pm *ProxyManager) checkProxy(p *store.BrowserProxy) bool {
	raw := p.URL
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := parsed.Host
	if host == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// List returns all proxies for a tenant.
func (pm *ProxyManager) List(ctx context.Context, tenantID string) ([]*store.BrowserProxy, error) {
	return pm.store.List(ctx, tenantID)
}

// Add creates a new proxy. Password is encrypted before storage.
func (pm *ProxyManager) Add(ctx context.Context, p *store.BrowserProxy) error {
	if p.Password != "" && pm.encryptKey != "" {
		encrypted, err := crypto.Encrypt(p.Password, pm.encryptKey)
		if err != nil {
			return fmt.Errorf("encrypt proxy password: %w", err)
		}
		p.Password = encrypted
	}
	return pm.store.Create(ctx, p)
}

// Remove deletes a proxy (scoped to tenant).
func (pm *ProxyManager) Remove(ctx context.Context, id, tenantID string) error {
	return pm.store.Delete(ctx, id, tenantID)
}
