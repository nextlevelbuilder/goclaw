package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	DefaultUserAgent    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:133.0) Gecko/20100101 Firefox/133.0"
	DefaultLanguage     = "vi"
	DefaultAPIType      = 30
	DefaultAPIVersion   = 671
	DefaultComputerName = "Web"
	DefaultUIDSelf      = "0"
	DefaultEncryptVer   = "v2"
	DefaultZCIDKey      = "3FC4F0D2AB50057BCE0D90D9187A22B1"
	MaxRedirects        = 10
)

// DefaultBaseURL is the Zalo chat base URL.
var DefaultBaseURL = url.URL{Scheme: "https", Host: "chat.zalo.me"}

// ThreadType distinguishes DM vs group.
type ThreadType uint8

const (
	ThreadTypeUser  ThreadType = 0
	ThreadTypeGroup ThreadType = 1
)

// Credentials holds Zalo login data (IMEI + cookies + user agent).
// Serializable for credential persistence.
type Credentials struct {
	IMEI      string       `json:"imei"`
	Cookie    *CookieUnion `json:"cookie"`
	UserAgent string       `json:"userAgent"`
	Language  *string      `json:"language,omitempty"`
}

// IsValid checks if credentials have minimum required fields.
func (c Credentials) IsValid() bool {
	return len(c.IMEI) > 0 && (c.Cookie == nil || c.Cookie.IsValid()) && len(c.UserAgent) > 0
}

// --- Cookie types (ported from zcago, MIT) ---

// SameSite represents cookie SameSite attribute.
type SameSite string

const (
	SameSiteDefault SameSite = ""
	SameSiteLax     SameSite = "lax"
	SameSiteStrict  SameSite = "strict"
	SameSiteNone    SameSite = "none"
)

func (s SameSite) MarshalJSON() ([]byte, error) {
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(s))
}

func (s *SameSite) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	*s = SameSite(str)
	return nil
}

// Cookie represents a Zalo authentication cookie.
type Cookie struct {
	Domain         string   `json:"domain"`
	ExpirationDate float64  `json:"expirationDate"`
	HostOnly       bool     `json:"hostOnly"`
	HTTPOnly       bool     `json:"httpOnly"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	SameSite       SameSite `json:"sameSite"`
	Secure         bool     `json:"secure"`
	Session        bool     `json:"session"`
	StoreID        *string  `json:"storeId,omitempty"`
	Value          string   `json:"value"`
}

// ToHTTPCookie converts to standard http.Cookie.
func (c Cookie) ToHTTPCookie() *http.Cookie {
	hc := &http.Cookie{
		Domain:   c.Domain,
		Name:     c.Name,
		Value:    c.Value,
		Path:     c.Path,
		HttpOnly: c.HTTPOnly,
		Secure:   c.Secure,
	}
	switch c.SameSite {
	case SameSiteStrict:
		hc.SameSite = http.SameSiteStrictMode
	case SameSiteLax:
		hc.SameSite = http.SameSiteLaxMode
	case SameSiteNone:
		hc.SameSite = http.SameSiteNoneMode
	default:
		hc.SameSite = http.SameSiteDefaultMode
	}
	if !c.Session && c.ExpirationDate > 0 {
		sec := int64(c.ExpirationDate)
		nsec := int64((c.ExpirationDate - float64(sec)) * 1e9)
		hc.Expires = time.Unix(sec, nsec)
	}
	return hc
}

// FromHTTPCookie populates Cookie from a standard http.Cookie.
func (c *Cookie) FromHTTPCookie(hc *http.Cookie) {
	c.Domain = hc.Domain
	c.Name = hc.Name
	c.Value = hc.Value
	c.Path = hc.Path
	c.HTTPOnly = hc.HttpOnly
	c.Secure = hc.Secure
	c.HostOnly = false
	c.StoreID = nil
	switch hc.SameSite {
	case http.SameSiteStrictMode:
		c.SameSite = SameSiteStrict
	case http.SameSiteLaxMode:
		c.SameSite = SameSiteLax
	case http.SameSiteNoneMode:
		c.SameSite = SameSiteNone
	default:
		c.SameSite = SameSiteDefault
	}
	switch {
	case hc.MaxAge > 0:
		exp := time.Now().Add(time.Duration(hc.MaxAge) * time.Second)
		c.Session = false
		c.ExpirationDate = float64(exp.UnixNano()) / 1e9
	case hc.MaxAge == 0 && !hc.Expires.IsZero():
		c.Session = false
		c.ExpirationDate = float64(hc.Expires.UnixNano()) / 1e9
	default:
		c.Session = true
		c.ExpirationDate = 0
	}
}

// J2Cookie represents cookies in the J2Cookie format.
type J2Cookie struct {
	URL     string   `json:"url"`
	Cookies []Cookie `json:"cookies"`
}

// CookieUnion represents cookies in multiple formats (array or J2Cookie).
type CookieUnion struct {
	cookies  []Cookie
	j2cookie *J2Cookie
}

// NewHTTPCookies creates a CookieUnion from standard http.Cookies.
func NewHTTPCookies(hc []*http.Cookie) CookieUnion {
	if hc == nil {
		return CookieUnion{}
	}
	cookies := make([]Cookie, len(hc))
	for i, c := range hc {
		cookies[i].FromHTTPCookie(c)
	}
	return CookieUnion{cookies: cookies}
}

func (cu *CookieUnion) IsValid() bool    { return cu.cookies != nil || cu.j2cookie != nil }
func (cu *CookieUnion) GetCookies() []Cookie {
	if cu.cookies != nil {
		return cu.cookies
	}
	if cu.j2cookie != nil {
		return cu.j2cookie.Cookies
	}
	return nil
}

// BuildCookieJar populates a cookie jar from the stored cookies.
// Strips leading "." from domains (Zalo protocol requirement).
// Sets cookies for both the base URL and wpa.chat.zalo.me (used by login API).
func (cu *CookieUnion) BuildCookieJar(u *url.URL, jar http.CookieJar) {
	src := cu.GetCookies()
	cookieArr := make([]Cookie, len(src))
	copy(cookieArr, src)
	for i := range cookieArr {
		if len(cookieArr[i].Domain) > 0 && cookieArr[i].Domain[0] == '.' {
			cookieArr[i].Domain = cookieArr[i].Domain[1:]
		}
	}
	if jar == nil {
		jar, _ = cookiejar.New(nil)
	}
	cookies := make([]*http.Cookie, len(cookieArr))
	for i, c := range cookieArr {
		cookies[i] = c.ToHTTPCookie()
	}
	jar.SetCookies(u, cookies)

	// Also set cookies for wpa.chat.zalo.me — the login API uses this host.
	wpaURL := &url.URL{Scheme: "https", Host: "wpa.chat.zalo.me"}
	jar.SetCookies(wpaURL, cookies)
}

func (cu CookieUnion) MarshalJSON() ([]byte, error) {
	switch {
	case cu.cookies != nil && cu.j2cookie != nil:
		return nil, errors.New("invariant: both cookies and j2cookie should not be set")
	case cu.cookies != nil:
		return json.Marshal(cu.cookies)
	case cu.j2cookie != nil:
		return json.Marshal(cu.j2cookie)
	default:
		return []byte("null"), nil
	}
}

func (cu *CookieUnion) UnmarshalJSON(b []byte) error {
	trim := bytes.TrimSpace(b)
	if len(trim) == 0 || bytes.Equal(trim, []byte("null")) {
		*cu = CookieUnion{}
		return nil
	}
	if trim[0] == '[' {
		var arr []Cookie
		if err := json.Unmarshal(trim, &arr); err != nil {
			return err
		}
		*cu = CookieUnion{cookies: arr}
		return nil
	}
	var j J2Cookie
	if err := json.Unmarshal(trim, &j); err != nil {
		return err
	}
	*cu = CookieUnion{j2cookie: &j}
	return nil
}
