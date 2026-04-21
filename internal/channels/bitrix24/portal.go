package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// expiryBuffer is the safety margin before a token actually expires.
// Tokens are refreshed as soon as time-to-live drops below this — keeps us
// out of the race where a request starts with a valid token but the upstream
// sees it as expired by the time the TCP handshake completes.
const expiryBuffer = 5 * time.Minute

// refreshBackoffs is the exponential backoff ladder for refresh failures.
// After the last entry, the portal keeps polling at that cadence until a
// reinstall happens (state is surfaced via health in Phase 07).
var refreshBackoffs = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	2 * time.Minute,
	5 * time.Minute,
	10 * time.Minute,
}

// Portal wraps the runtime state of a single Bitrix24 portal.
//
// One Portal backs N channels (bots) on that portal — the channels share
// access tokens and the refresh goroutine. All state mutations go through
// persistState, which serialises JSON + writes via BitrixPortalStore so a
// crash mid-refresh can never produce a half-written row.
type Portal struct {
	tenantID uuid.UUID
	name     string
	domain   string
	store    store.BitrixPortalStore
	encKey   string // reserved for callers that need to re-encrypt side payloads
	creds    store.BitrixPortalCredentials
	client   *Client

	mu    sync.RWMutex
	state store.BitrixPortalState

	sf singleflight.Group

	// refresh loop lifecycle
	stopOnce sync.Once
	stopCh   chan struct{}
	running  atomic.Bool
}

// NewPortal loads the row from the store and returns a ready Portal.
//
// Missing credentials (brand-new row with no client_id/secret) is a fatal
// error — the caller is supposed to seed credentials before goclaw touches
// the portal. Missing state (never installed) is fine; Exchange() fills it.
func NewPortal(
	ctx context.Context,
	tenantID uuid.UUID,
	name string,
	s store.BitrixPortalStore,
	encKey string,
) (*Portal, error) {
	if s == nil {
		return nil, errors.New("bitrix24 portal: nil store")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("bitrix24 portal: tenant_id required")
	}
	if name == "" {
		return nil, errors.New("bitrix24 portal: name required")
	}

	row, err := s.GetByName(ctx, tenantID, name)
	if err != nil {
		return nil, fmt.Errorf("bitrix24 portal %q: load row: %w", name, err)
	}

	var creds store.BitrixPortalCredentials
	if len(row.Credentials) > 0 {
		if err := json.Unmarshal(row.Credentials, &creds); err != nil {
			return nil, fmt.Errorf("bitrix24 portal %q: decode credentials: %w", name, err)
		}
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("bitrix24 portal %q: credentials missing client_id/client_secret", name)
	}

	var st store.BitrixPortalState
	if len(row.State) > 0 {
		if err := json.Unmarshal(row.State, &st); err != nil {
			return nil, fmt.Errorf("bitrix24 portal %q: decode state: %w", name, err)
		}
	}

	client := NewClient(row.Domain, nil)
	p := &Portal{
		tenantID: tenantID,
		name:     name,
		domain:   row.Domain,
		store:    s,
		encKey:   encKey,
		creds:    creds,
		client:   client,
		state:    st,
		stopCh:   make(chan struct{}),
	}
	client.SetPortal(p)
	return p, nil
}

// TenantID returns the tenant scope of this portal.
func (p *Portal) TenantID() uuid.UUID { return p.tenantID }

// Name returns the portal name (unique per tenant).
func (p *Portal) Name() string { return p.name }

// Domain returns the Bitrix24 portal hostname (e.g. "customer.bitrix24.com").
func (p *Portal) Domain() string { return p.domain }

// Client exposes the underlying REST client (for channels to share transport).
func (p *Portal) Client() *Client { return p.client }

// Installed reports whether the portal has ever completed the OAuth exchange.
// False means we still need an admin visit to /bitrix24/install.
func (p *Portal) Installed() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.RefreshToken != ""
}

// MemberID returns the Bitrix-assigned unique portal id (stable even on domain rename).
func (p *Portal) MemberID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.MemberID
}

// AppToken returns auth.application_token from the OAuth response.
// Phase 02 uses this to verify outgoing event webhooks.
func (p *Portal) AppToken() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.AppToken
}

// LookupRegisteredBot returns the bot id previously registered under a code.
// Phase 03 uses it to decide whether imbot.register needs to run at startup.
func (p *Portal) LookupRegisteredBot(code string) (int, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.RegisteredBots == nil {
		return 0, false
	}
	id, ok := p.state.RegisteredBots[code]
	return id, ok
}

// RecordRegisteredBot saves (bot_code → bot_id) into state atomically.
func (p *Portal) RecordRegisteredBot(ctx context.Context, code string, id int) error {
	if code == "" {
		return errors.New("bot code required")
	}
	p.mu.Lock()
	if p.state.RegisteredBots == nil {
		p.state.RegisteredBots = make(map[string]int)
	}
	p.state.RegisteredBots[code] = id
	stateCopy := p.state
	p.mu.Unlock()
	return p.writeState(ctx, stateCopy)
}

// LookupMediaFolder returns the cached disk folder id for a bot_code.
// Empty string means “no folder cached yet”.
func (p *Portal) LookupMediaFolder(code string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.state.MediaFolders == nil {
		return ""
	}
	return p.state.MediaFolders[code]
}

// SaveMediaFolder persists the disk folder id for a bot_code.
func (p *Portal) SaveMediaFolder(ctx context.Context, code, folderID string) error {
	if code == "" {
		return errors.New("bot code required")
	}
	p.mu.Lock()
	if p.state.MediaFolders == nil {
		p.state.MediaFolders = make(map[string]string)
	}
	p.state.MediaFolders[code] = folderID
	stateCopy := p.state
	p.mu.Unlock()
	return p.writeState(ctx, stateCopy)
}

// InstallFromTokens persists tokens handed over directly by Bitrix24 without
// running the OAuth authorization_code exchange. This is the "Local application"
// install flow: Bitrix24 POSTs `AUTH_ID` / `REFRESH_ID` / `AUTH_EXPIRES` /
// `application_token` / `member_id` / `DOMAIN` into the handler path and there
// is no `code` to exchange — the tokens are already minted.
//
// Callers build a TokenResponse from the form body so we can reuse the same
// applyTokenResponse + persistState path that Exchange uses on OAuth2 apps.
// Any missing critical field (access_token OR refresh_token) is rejected:
// persisting a half-install would leave the portal permanently wedged
// until a full reinstall.
func (p *Portal) InstallFromTokens(ctx context.Context, tr *TokenResponse) error {
	if tr == nil {
		return errors.New("bitrix24 install: nil token response")
	}
	if tr.AccessToken == "" || tr.RefreshToken == "" {
		return errors.New("bitrix24 install: AUTH_ID and REFRESH_ID required")
	}
	if tr.Domain != "" && !strings.EqualFold(tr.Domain, p.domain) {
		slog.Warn("bitrix24 install: domain mismatch",
			"portal", p.name, "expected", p.domain, "received", tr.Domain)
	}
	p.applyTokenResponse(tr)
	// Detach context for the same reason Exchange does: once Bitrix has handed
	// us tokens we MUST get them to disk, even if the install-callback
	// goroutine's context is about to be canceled.
	return p.persistState(context.WithoutCancel(ctx))
}

// Exchange runs on the OAuth install callback: trade `code` for tokens,
// persist them, and prime the refresh loop.
func (p *Portal) Exchange(ctx context.Context, code string) error {
	if code == "" {
		return errors.New("bitrix24 exchange: code required")
	}
	tr, err := p.client.ExchangeAuthCode(ctx, p.creds.ClientID, p.creds.ClientSecret, code)
	if err != nil {
		return fmt.Errorf("bitrix24 exchange: %w", err)
	}
	if tr.Domain != "" && !strings.EqualFold(tr.Domain, p.domain) {
		slog.Warn("bitrix24 exchange: domain mismatch",
			"portal", p.name, "expected", p.domain, "received", tr.Domain)
	}
	p.applyTokenResponse(tr)
	// Same rationale as refreshLocked: once Bitrix has minted tokens for us,
	// a canceled install-callback context must not prevent persistence.
	return p.persistState(context.WithoutCancel(ctx))
}

// AccessToken returns a valid access token, refreshing synchronously if we're
// inside the expiry buffer. Concurrent callers coalesce through singleflight
// so a thundering-herd only triggers one refresh regardless of goroutine count.
func (p *Portal) AccessToken(ctx context.Context) (string, error) {
	p.mu.RLock()
	tok := p.state.AccessToken
	expiry := p.state.ExpiresAt
	installed := p.state.RefreshToken != ""
	p.mu.RUnlock()

	if !installed {
		return "", errors.New("bitrix24 portal: not installed — run /bitrix24/install first")
	}

	// Token still safely inside its window.
	if tok != "" && time.Until(expiry) > expiryBuffer {
		return tok, nil
	}

	// Near or past expiry → refresh (singleflighted).
	if _, err, _ := p.sf.Do("refresh", func() (any, error) {
		return nil, p.refreshLocked(ctx)
	}); err != nil {
		return "", err
	}

	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state.AccessToken, nil
}

// refreshLocked performs one refresh round-trip and persists the result.
// Must be called via singleflight to guarantee one concurrent refresh.
func (p *Portal) refreshLocked(ctx context.Context) error {
	p.mu.RLock()
	refresh := p.state.RefreshToken
	p.mu.RUnlock()
	if refresh == "" {
		return errors.New("bitrix24 refresh: no refresh_token — reinstall required")
	}

	tr, err := p.client.RefreshToken(ctx, p.creds.ClientID, p.creds.ClientSecret, refresh)
	if err != nil {
		p.mu.Lock()
		p.state.LastRefreshAt = time.Now().UTC()
		p.state.LastRefreshError = truncateErr(err)
		p.state.ConsecutiveFail++
		stateCopy := p.state
		p.mu.Unlock()
		_ = p.writeState(context.Background(), stateCopy)
		return err
	}

	p.applyTokenResponse(tr)
	// Decouple the persist from the caller's context. The refresh itself uses
	// ctx (so a shutdown can abort the HTTP round-trip), but once Bitrix has
	// rotated the refresh_token we MUST get it to the store — if we drop it
	// because an HTTP handler canceled we're stuck with an expired access
	// token and no way to recover without a reinstall.
	return p.persistState(context.WithoutCancel(ctx))
}

// defaultTokenTTL is the fallback window when a Bitrix24 token response
// returns a zero or negative expires_in. Bitrix tokens normally live one
// hour — if the server lies about the TTL we'd rather schedule one safe
// refresh than spin on an "already expired" access token.
const defaultTokenTTL = 1 * time.Hour

// applyTokenResponse writes the OAuth response into state under the write lock.
// Every successful refresh resets the failure counter.
func (p *Portal) applyTokenResponse(tr *TokenResponse) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if tr.AccessToken != "" {
		p.state.AccessToken = tr.AccessToken
	}
	if tr.RefreshToken != "" {
		p.state.RefreshToken = tr.RefreshToken
	}
	// Clamp a missing or bogus expires_in. Without this, ExpiresAt keeps the
	// stale value from the previous token — AccessToken() then sees the fresh
	// token as already-expired and immediately refreshes again, locking us
	// into an infinite refresh loop.
	ttl := time.Duration(tr.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	if tr.AccessToken != "" {
		p.state.ExpiresAt = time.Now().UTC().Add(ttl)
	}
	if tr.MemberID != "" {
		p.state.MemberID = tr.MemberID
	}
	if tr.ApplicationToken != "" {
		p.state.AppToken = tr.ApplicationToken
	}
	if tr.Scope != "" {
		p.state.Scope = tr.Scope
	}
	if tr.ClientEndpoint != "" {
		p.state.ClientEndpoint = tr.ClientEndpoint
	}
	p.state.LastRefreshAt = time.Now().UTC()
	p.state.LastRefreshError = ""
	p.state.ConsecutiveFail = 0
}

// persistState serialises the current state under a read lock and writes it
// to the store. Separate from applyTokenResponse so the lock is released
// before the network-bound store call.
func (p *Portal) persistState(ctx context.Context) error {
	p.mu.RLock()
	stateCopy := p.state
	p.mu.RUnlock()
	return p.writeState(ctx, stateCopy)
}

// writeState encodes and writes a given state snapshot.
func (p *Portal) writeState(ctx context.Context, state store.BitrixPortalState) error {
	b, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode portal state: %w", err)
	}
	if err := p.store.UpdateState(ctx, p.tenantID, p.name, b); err != nil {
		return fmt.Errorf("persist portal state: %w", err)
	}
	return nil
}

// StartRefreshLoop kicks off a background goroutine that refreshes the token
// slightly before expiry. Safe to call multiple times — only the first call
// spawns a goroutine. Call Stop() to release it.
//
// ctx is detached via context.WithoutCancel: the refresh loop's lifetime is
// bound to p.stopCh, NOT to the caller's ctx. If we inherited a request-scoped
// ctx (e.g. someone passed req.Context() instead of context.Background()), the
// loop would silently die when the request ended, and Router.running +
// p.running atomics would keep us from ever restarting it — tokens would
// silently expire. Detaching costs nothing and removes the foot-gun.
func (p *Portal) StartRefreshLoop(ctx context.Context) {
	if !p.running.CompareAndSwap(false, true) {
		return
	}
	go p.refreshLoop(context.WithoutCancel(ctx))
}

func (p *Portal) refreshLoop(ctx context.Context) {
	backoffIdx := 0
	for {
		p.mu.RLock()
		expiry := p.state.ExpiresAt
		lastErr := p.state.LastRefreshError
		p.mu.RUnlock()

		var wait time.Duration
		switch {
		case lastErr != "":
			// Backoff ladder clamps at the last entry (= 10 min).
			if backoffIdx >= len(refreshBackoffs) {
				backoffIdx = len(refreshBackoffs) - 1
			}
			wait = refreshBackoffs[backoffIdx]
		case expiry.IsZero():
			// Never installed — wait before re-checking the row in case an
			// admin just completed Exchange() from another goroutine.
			wait = 1 * time.Minute
		default:
			wait = time.Until(expiry) - expiryBuffer
			if wait < 30*time.Second {
				wait = 30 * time.Second
			}
		}

		select {
		case <-p.stopCh:
			return
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		// Skip if not installed.
		p.mu.RLock()
		installed := p.state.RefreshToken != ""
		p.mu.RUnlock()
		if !installed {
			continue
		}

		_, err, _ := p.sf.Do("refresh", func() (any, error) {
			return nil, p.refreshLocked(ctx)
		})
		if err != nil {
			slog.Warn("bitrix24 refresh failed", "portal", p.name, "tenant", p.tenantID, "err", err)
			if backoffIdx < len(refreshBackoffs)-1 {
				backoffIdx++
			}
			continue
		}
		backoffIdx = 0
	}
}

// Stop halts the background refresh loop. Idempotent.
func (p *Portal) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	p.running.Store(false)
}

// HandleInstall is the http.HandlerFunc for /bitrix24/install.
//
// URL: GET /bitrix24/install?code=XXX&domain=YYY&state=<tenant_id>:<portal_name>
//
// We validate that (a) the state token matches this portal's (tenant_id, name)
// and (b) the reported domain equals the stored portal.Domain. On success we
// run Exchange() and render a tiny HTML page that auto-closes the install
// popup — matches Bitrix's own UX so admins don't see a blank tab.
//
// This is mounted by the webhook router in Phase 02. Gateway-level rate
// limiting guards against brute-force state guesses.
func (p *Portal) HandleInstall(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := strings.TrimSpace(q.Get("code"))
	stateParam := strings.TrimSpace(q.Get("state"))
	domain := strings.TrimSpace(q.Get("domain"))
	if code == "" || stateParam == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	tenantStr, portalName, ok := strings.Cut(stateParam, ":")
	if !ok {
		http.Error(w, "invalid state format", http.StatusBadRequest)
		return
	}
	tid, err := uuid.Parse(tenantStr)
	if err != nil || tid != p.tenantID {
		http.Error(w, "state tenant mismatch", http.StatusForbidden)
		return
	}
	if portalName != p.name {
		http.Error(w, "state portal mismatch", http.StatusForbidden)
		return
	}
	if domain != "" && !strings.EqualFold(domain, p.domain) {
		http.Error(w, "domain mismatch", http.StatusForbidden)
		return
	}

	if err := p.Exchange(r.Context(), code); err != nil {
		slog.Warn("bitrix24 install: exchange failed", "portal", p.name, "tenant", p.tenantID, "err", err)
		http.Error(w, "exchange failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(installSuccessHTML))
}

// installSuccessHTML is shown in the install popup after a successful OAuth
// exchange. Auto-closes the window after a short delay, falling back to a
// visible success message if the window can't close (cross-origin policies).
const installSuccessHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Bitrix24 installation complete</title>
<style>
  body { font: 15px/1.5 system-ui, sans-serif; padding: 2rem; color: #222; }
  h1 { color: #1db954; margin: 0 0 .5rem 0; }
  p  { margin: 0; }
</style>
</head>
<body>
<h1>Installation successful</h1>
<p>GoClaw is now connected to your Bitrix24 portal. You can close this window.</p>
<script>setTimeout(function(){ try { window.close(); } catch (e) {} }, 800);</script>
</body>
</html>`

// truncateErr bounds an error string for state persistence.
func truncateErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 512 {
		s = s[:512] + "…"
	}
	return s
}
