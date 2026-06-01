package http

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type browserCookieSyncRequest struct {
	AgentID string                     `json:"agent_id"`
	Agent   string                     `json:"agent,omitempty"`
	Source  string                     `json:"source,omitempty"`
	Cookies []browserCookieSyncPayload `json:"cookies"`
}

type browserCookieSyncPayload struct {
	Domain              string     `json:"domain"`
	Name                string     `json:"name"`
	Path                string     `json:"path"`
	Value               string     `json:"value"`
	URL                 string     `json:"url,omitempty"`
	Secure              bool       `json:"secure"`
	HTTPOnly            bool       `json:"httpOnly"`
	HTTPOnlySnake       bool       `json:"http_only"`
	SameSite            string     `json:"sameSite"`
	SameSiteSnake       string     `json:"same_site"`
	ExpiresAt           *time.Time `json:"expiresAt"`
	ExpiresAtSnake      *time.Time `json:"expires_at"`
	ExpirationDate      *float64   `json:"expirationDate"`
	ExpirationDateSnake *float64   `json:"expiration_date"`
}

type browserCookieMetadata struct {
	Domain    string     `json:"domain"`
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	Secure    bool       `json:"secure"`
	HTTPOnly  bool       `json:"httpOnly"`
	SameSite  string     `json:"sameSite,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Source    string     `json:"source,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (p browserCookieSyncPayload) toStoreCookie(source string) (store.BrowserCookie, error) {
	domain := strings.TrimSpace(p.Domain)
	if domain == "" && p.URL != "" {
		u, err := url.Parse(p.URL)
		if err != nil {
			return store.BrowserCookie{}, errBrowserCookieInvalidURL
		}
		domain = u.Hostname()
	}
	if len(p.Value) > maxBrowserCookieValueBytes {
		return store.BrowserCookie{}, errBrowserCookieValueTooLarge
	}
	c := store.NormalizeBrowserCookie(store.BrowserCookie{
		Domain:    domain,
		Name:      p.Name,
		Path:      p.Path,
		Value:     p.Value,
		Secure:    p.Secure,
		HTTPOnly:  p.HTTPOnly || p.HTTPOnlySnake,
		SameSite:  firstBrowserCookieNonEmpty(p.SameSite, p.SameSiteSnake),
		ExpiresAt: firstTime(p.ExpiresAt, p.ExpiresAtSnake, timeFromUnixSeconds(p.ExpirationDate), timeFromUnixSeconds(p.ExpirationDateSnake)),
		Source:    source,
	})
	if err := store.ValidateBrowserCookie(c); err != nil {
		return store.BrowserCookie{}, err
	}
	return c, nil
}

func browserCookieFilterFromQuery(r *http.Request) store.BrowserCookieFilter {
	q := r.URL.Query()
	return store.BrowserCookieFilter{
		Domain: q.Get("domain"),
		Name:   q.Get("name"),
		Path:   q.Get("path"),
	}
}

func firstBrowserCookieNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func firstTime(values ...*time.Time) *time.Time {
	for _, v := range values {
		if v != nil {
			t := v.UTC()
			return &t
		}
	}
	return nil
}

func timeFromUnixSeconds(v *float64) *time.Time {
	if v == nil {
		return nil
	}
	sec := int64(*v)
	nsec := int64((*v - float64(sec)) * 1e9)
	t := time.Unix(sec, nsec).UTC()
	return &t
}
