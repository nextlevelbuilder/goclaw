package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/browser"
)

type storeBrowserCookieProvider struct {
	cookies store.BrowserCookieStore
}

func newStoreBrowserCookieProvider(cookies store.BrowserCookieStore) browser.CookieProvider {
	if cookies == nil {
		return nil
	}
	return &storeBrowserCookieProvider{cookies: cookies}
}

func (p *storeBrowserCookieProvider) CookiesForURL(ctx context.Context, scope browser.BrowserScope, targetURL string) ([]*proto.NetworkCookieParam, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("parse target url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, nil
	}
	storeScope, err := browserScopeToCookieScope(scope)
	if err != nil {
		return nil, err
	}
	cookies, err := p.cookies.List(ctx, storeScope, store.BrowserCookieFilter{})
	if err != nil {
		return nil, err
	}

	host := strings.ToLower(u.Hostname())
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	now := time.Now().UTC()
	params := make([]*proto.NetworkCookieParam, 0, len(cookies))
	for _, c := range cookies {
		if !browserCookieMatchesURL(c, host, path, now) {
			continue
		}
		param := &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			URL:      targetURL,
			Path:     c.Path,
			Secure:   c.Secure,
			HTTPOnly: c.HTTPOnly,
			SameSite: browserCookieSameSite(c.SameSite),
		}
		if strings.HasPrefix(c.Domain, ".") {
			param.Domain = c.Domain
		}
		if c.ExpiresAt != nil {
			param.Expires = proto.TimeSinceEpoch(float64(c.ExpiresAt.Unix()))
		}
		params = append(params, param)
	}
	return params, nil
}

func browserScopeToCookieScope(scope browser.BrowserScope) (store.BrowserCookieScope, error) {
	tenantID := store.MasterTenantID
	if strings.TrimSpace(scope.TenantID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(scope.TenantID))
		if err != nil {
			return store.BrowserCookieScope{}, fmt.Errorf("invalid browser tenant scope: %w", err)
		}
		tenantID = parsed
	}
	cookieScope := store.BrowserCookieScope{
		TenantID: tenantID,
		UserID:   strings.TrimSpace(scope.UserID),
		AgentID:  strings.TrimSpace(scope.AgentID),
	}
	if err := cookieScope.Validate(); err != nil {
		return store.BrowserCookieScope{}, err
	}
	return cookieScope, nil
}

func browserCookieMatchesURL(c store.BrowserCookie, host, requestPath string, now time.Time) bool {
	domain := strings.ToLower(strings.TrimSpace(c.Domain))
	if domain == "" {
		return false
	}
	if c.ExpiresAt != nil && !c.ExpiresAt.After(now) {
		return false
	}
	hostOnly := !strings.HasPrefix(domain, ".")
	matchDomain := strings.TrimPrefix(domain, ".")
	if hostOnly {
		if host != matchDomain {
			return false
		}
	} else if host != matchDomain && !strings.HasSuffix(host, "."+matchDomain) {
		return false
	}
	cookiePath := c.Path
	if cookiePath == "" {
		cookiePath = "/"
	}
	return requestPath == cookiePath || strings.HasPrefix(requestPath, strings.TrimRight(cookiePath, "/")+"/")
}

func browserCookieSameSite(value string) proto.NetworkCookieSameSite {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "strict":
		return proto.NetworkCookieSameSiteStrict
	case "lax":
		return proto.NetworkCookieSameSiteLax
	case "none", "no_restriction", "no-restriction":
		return proto.NetworkCookieSameSiteNone
	default:
		return ""
	}
}
