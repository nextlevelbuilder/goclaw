package personal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

const cookiePersistDebounce = 30 * time.Second

// cookieObserver delegates to the real jar and notifies after SetCookies.
// Wrapping the jar (rather than the transport) catches cookies set by both
// redirects and explicit seeding of service-map hosts.
type cookieObserver struct {
	base  http.CookieJar
	onSet func()
}

func (j *cookieObserver) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.base.SetCookies(u, cookies)
	if len(cookies) > 0 && j.onSet != nil {
		j.onSet()
	}
}

func (j *cookieObserver) Cookies(u *url.URL) []*http.Cookie {
	return j.base.Cookies(u)
}

func installCookieObserver(sess *protocol.Session, onSet func()) {
	if sess == nil {
		return
	}
	base := sess.CookieJar
	if base == nil {
		base, _ = cookiejar.New(nil)
	}
	observer := &cookieObserver{base: base, onSet: onSet}
	sess.CookieJar = observer
	if sess.Client != nil {
		sess.Client.Jar = observer
	}
}

// cookiePersister debounces cookie writes and skips writes when the credential
// snapshot hash has not changed.
type cookiePersister struct {
	mu sync.Mutex

	debounce time.Duration
	snapshot func() *protocol.Credentials
	save     func(*protocol.Credentials) error

	timer    *time.Timer
	dirty    bool
	closed   bool
	lastHash [sha256.Size]byte
	hasHash  bool
}

func newCookiePersister(debounce time.Duration, save func(*protocol.Credentials) error) *cookiePersister {
	if debounce <= 0 {
		debounce = cookiePersistDebounce
	}
	return &cookiePersister{debounce: debounce, save: save}
}

// Bind attaches the live session snapshot and records its current hash so an
// unchanged login is not written back.
func (p *cookiePersister) Bind(snapshot func() *protocol.Credentials) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot = snapshot
	if cred := snapshot(); cred != nil {
		p.lastHash = hashCredentials(cred)
		p.hasHash = true
	}
}

func (p *cookiePersister) MarkDirty() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.dirty = true
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(p.debounce, func() {
		if err := p.Flush(context.Background()); err != nil {
			slog.Warn("zalo_personal: cookie persist failed", "error", err)
		}
	})
}

// Flush writes the current snapshot if it changed. Safe to call from Stop even
// after Close has stopped the debounce timer.
func (p *cookiePersister) Flush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	if !p.dirty || p.snapshot == nil || p.save == nil {
		return nil
	}
	p.dirty = false

	cred := p.snapshot()
	if cred == nil {
		return nil
	}
	hash := hashCredentials(cred)
	if p.hasHash && hash == p.lastHash {
		return nil
	}
	if err := p.save(cred); err != nil {
		p.dirty = true
		return err
	}
	p.lastHash = hash
	p.hasHash = true
	return nil
}

func (p *cookiePersister) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	p.closed = true
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.mu.Unlock()
	return p.Flush(ctx)
}

func hashCredentials(cred *protocol.Credentials) [sha256.Size]byte {
	blob, _ := json.Marshal(cred)
	return sha256.Sum256(blob)
}

// snapshotCredentials flattens cookies from every host that was seeded during
// login. Go's cookiejar does not share cookies across subdomains, so reading
// only DefaultBaseURL would silently drop rotated service-map cookies.
func snapshotCredentials(sess *protocol.Session) *protocol.Credentials {
	if sess == nil || sess.CookieJar == nil {
		return nil
	}

	var cookies []*http.Cookie
	seen := make(map[string]struct{})
	for _, host := range cookieHosts(sess) {
		u := &url.URL{Scheme: "https", Host: host}
		for _, cookie := range sess.CookieJar.Cookies(u) {
			if cookie == nil {
				continue
			}
			if cookie.Domain == "" {
				cookie.Domain = host
			}
			key := cookie.Domain + "\x00" + cookie.Name + "\x00" + cookie.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			cookies = append(cookies, cookie)
		}
	}
	if len(cookies) == 0 {
		return nil
	}

	union := protocol.NewHTTPCookies(cookies)
	lang := sess.Language
	return &protocol.Credentials{
		IMEI:      sess.IMEI,
		Cookie:    &union,
		UserAgent: sess.UserAgent,
		Language:  &lang,
	}
}

func cookieHosts(sess *protocol.Session) []string {
	seen := make(map[string]struct{})
	hosts := make([]string, 0, 8)
	add := func(raw string) {
		if raw == "" {
			return
		}
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" {
			return
		}
		if _, ok := seen[parsed.Host]; ok {
			return
		}
		seen[parsed.Host] = struct{}{}
		hosts = append(hosts, parsed.Host)
	}

	add(protocol.DefaultBaseURL.String())
	add("https://wpa.chat.zalo.me")
	if sess.LoginInfo != nil {
		sm := sess.LoginInfo.ZpwServiceMapV3
		for _, urls := range [][]string{sm.Chat, sm.Group, sm.File, sm.Profile, sm.GroupPoll} {
			for _, raw := range urls {
				add(raw)
			}
		}
	}
	return hosts
}
