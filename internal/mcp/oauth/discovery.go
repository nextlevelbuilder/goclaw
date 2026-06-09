// Package mcpoauth implements OAuth 2.1 client support for MCP servers.
// Discovery follows RFC 9728 (Protected Resource Metadata) then RFC 8414
// (Authorization Server Metadata) with OIDC fallback.
package mcpoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const discoveryCacheTTL = 5 * time.Minute

// DiscoveryResult holds the Authorization Server metadata for an MCP server.
type DiscoveryResult struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RegistrationEndpoint  string // DCR — optional
	ResourceURI           string // RFC 8707 resource indicator
	ScopesSupported       []string
}

type cachedDiscovery struct {
	result    *DiscoveryResult
	fetchedAt time.Time
}

// Discoverer caches and performs OAuth AS discovery for MCP server URLs.
type Discoverer struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]*cachedDiscovery
}

func NewDiscoverer(client *http.Client) *Discoverer {
	return &Discoverer{
		client: client,
		cache:  make(map[string]*cachedDiscovery),
	}
}

// Discover finds the OAuth Authorization Server metadata for an MCP server URL.
//
// Priority (per plan spec):
//
//	Phase 1 — RFC 9728: fetch /.well-known/oauth-protected-resource[/{path}]
//	           to resolve the Authorization Server base URL.
//	Phase 2 — RFC 8414 + OIDC fallback on the resolved AS base URL:
//	  1. {AS}/.well-known/oauth-authorization-server
//	  2. {AS}/.well-known/oauth-authorization-server/{path}
//	  3. {AS}/.well-known/openid-configuration
//	  4. {AS}/{path}/.well-known/openid-configuration
func (d *Discoverer) Discover(ctx context.Context, mcpServerURL string) (*DiscoveryResult, error) {
	d.mu.Lock()
	if c, ok := d.cache[mcpServerURL]; ok && time.Since(c.fetchedAt) < discoveryCacheTTL {
		d.mu.Unlock()
		return c.result, nil
	}
	d.mu.Unlock()

	result, err := d.discover(ctx, mcpServerURL)
	if err != nil {
		return nil, err
	}

	d.mu.Lock()
	d.cache[mcpServerURL] = &cachedDiscovery{result: result, fetchedAt: time.Now()}
	d.mu.Unlock()
	return result, nil
}

// InvalidateCache removes the cached discovery for the given MCP server URL.
func (d *Discoverer) InvalidateCache(mcpServerURL string) {
	d.mu.Lock()
	delete(d.cache, mcpServerURL)
	d.mu.Unlock()
}

func (d *Discoverer) discover(ctx context.Context, mcpServerURL string) (*DiscoveryResult, error) {
	parsed, err := url.Parse(mcpServerURL)
	if err != nil {
		return nil, fmt.Errorf("mcpoauth: invalid MCP server URL %q: %w", mcpServerURL, err)
	}
	base := parsed.Scheme + "://" + parsed.Host
	path := strings.TrimPrefix(parsed.Path, "/")

	// Phase 1 — RFC 9728: try to discover Authorization Server via Protected Resource Metadata.
	// When RFC 9728 succeeds, the returned AS base URL is authoritative — no path suffix needed.
	// When RFC 9728 fails, fall back to using the MCP server base with its path.
	asBase := base
	asPath := path
	if prm, err2 := d.fetchProtectedResourceMeta(ctx, base, path); err2 == nil && len(prm.AuthorizationServers) > 0 {
		asBase = strings.TrimRight(prm.AuthorizationServers[0], "/")
		asPath = "" // AS URL from RFC 9728 is complete — no path suffix
	}

	// Phase 2 — RFC 8414 + OIDC fallback on AS base.
	return d.discoverASMeta(ctx, asBase, asPath, mcpServerURL)
}

type protectedResourceMeta struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

func (d *Discoverer) fetchProtectedResourceMeta(ctx context.Context, base, path string) (*protectedResourceMeta, error) {
	candidates := []string{
		base + "/.well-known/oauth-protected-resource",
	}
	if path != "" {
		candidates = append(candidates, base+"/.well-known/oauth-protected-resource/"+path)
	}
	for _, u := range candidates {
		var meta protectedResourceMeta
		if err := d.getJSON(ctx, u, &meta); err == nil && len(meta.AuthorizationServers) > 0 {
			return &meta, nil
		}
	}
	return nil, fmt.Errorf("mcpoauth: RFC 9728 discovery failed")
}

type asMeta struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
}

func (d *Discoverer) discoverASMeta(ctx context.Context, asBase, path, mcpServerURL string) (*DiscoveryResult, error) {
	candidates := []string{
		asBase + "/.well-known/oauth-authorization-server",
	}
	if path != "" {
		candidates = append(candidates,
			asBase+"/.well-known/oauth-authorization-server/"+path,
		)
	}
	candidates = append(candidates, asBase+"/.well-known/openid-configuration")
	if path != "" {
		candidates = append(candidates, asBase+"/"+path+"/.well-known/openid-configuration")
	}

	for _, u := range candidates {
		var meta asMeta
		if err := d.getJSON(ctx, u, &meta); err != nil {
			continue
		}
		if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
			continue
		}
		// RFC 8414 §3.3: the issuer in the metadata must identify the same
		// Authorization Server the metadata was retrieved from. Requiring the
		// issuer origin to match the fetch origin blocks an AS mix-up where a
		// compromised resource server points discovery at an issuer running on
		// a host it does not control. Path differences are tolerated (issuers
		// like Azure AD carry a tenant path), and an absent issuer is allowed
		// since some OIDC fallbacks omit it.
		if meta.Issuer != "" && !sameOrigin(meta.Issuer, u) {
			slog.Warn("mcpoauth.issuer_mismatch",
				"metadata_url", u, "advertised_issuer", meta.Issuer)
			continue
		}
		return &DiscoveryResult{
			Issuer:                meta.Issuer,
			AuthorizationEndpoint: meta.AuthorizationEndpoint,
			TokenEndpoint:         meta.TokenEndpoint,
			RegistrationEndpoint:  meta.RegistrationEndpoint,
			ResourceURI:           mcpServerURL,
			ScopesSupported:       meta.ScopesSupported,
		}, nil
	}

	return nil, fmt.Errorf("mcpoauth: no OAuth Authorization Server metadata found for %s", mcpServerURL)
}

// sameOrigin reports whether two URLs share the same scheme and host
// (case-insensitive). Path, query, and fragment are ignored.
func sameOrigin(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}

func (d *Discoverer) getJSON(ctx context.Context, rawURL string, dst any) error {
	// Validate URL and pin the resolved IP into ctx so the safe HTTP client
	// can dial without re-resolving (prevents DNS rebinding / SSRF).
	_, ip, err := security.Validate(rawURL)
	if err != nil {
		return fmt.Errorf("mcpoauth: SSRF validation failed for %s: %w", rawURL, err)
	}
	ctx = security.WithPinnedIP(ctx, ip)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mcpoauth: HTTP %d from %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}
