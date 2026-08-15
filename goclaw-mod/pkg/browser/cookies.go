package browser

import (
	"context"
	"net/url"

	"github.com/go-rod/rod/lib/proto"
)

// CookieProvider returns selected cookies for one isolated browser scope and URL.
type CookieProvider interface {
	CookiesForURL(ctx context.Context, scope BrowserScope, targetURL string) ([]*proto.NetworkCookieParam, error)
}

func browserURLSupportsCookies(targetURL string) bool {
	u, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

func (m *Manager) cookiesForURL(ctx context.Context, scope BrowserScope, targetURL string) ([]*proto.NetworkCookieParam, error) {
	if m.cookieProvider == nil || !browserURLSupportsCookies(targetURL) {
		return nil, nil
	}
	return m.cookieProvider.CookiesForURL(ctx, scope, targetURL)
}
