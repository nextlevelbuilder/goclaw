package bitrix24

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// bitrix24LogRawEvent is an opt-in debug switch. When set (env
// BITRIX24_LOG_RAW_EVENT=1 at process start), handleEvent dumps the
// full parsed form body of every inbound event, with OAuth credentials
// redacted. Leave OFF in steady-state: the dump leaks message text to
// logs and is noisy — intended for one-shot capture during debugging.
var bitrix24LogRawEvent = strings.TrimSpace(os.Getenv("BITRIX24_LOG_RAW_EVENT")) == "1"

// isRedactedEventKey returns true for form keys whose values carry OAuth
// credentials that must never appear verbatim in logs. Bitrix24 duplicates
// the same tokens under multiple paths — top-level `auth[access_token]` AND
// `data[BOT][<id>][access_token]` AND `data[BOT][<id>][AUTH][access_token]`
// all carry the SAME secret. Earlier version only guarded the top-level
// path and leaked tokens through the nested duplicates. This version
// match-by-suffix on the leaf key name, which catches all three locations
// plus any new `data[...]` nesting Bitrix24 adds in future releases.
//
// Leaf keys considered sensitive:
//   - access_token        (1h OAuth bearer)
//   - refresh_token       (long-lived; can mint new access_token)
//   - application_token   (stable per-install webhook secret)
//   - client_id + client_secret (app identity; client_id is not secret
//     per OAuth spec but pairs with client_secret in app registration
//     so we redact both to avoid admin confusion)
//   - AUTH_ID / REFRESH_ID (install POST variants of the above)
func isRedactedEventKey(k string) bool {
	// Strip bracket path, keep the trailing leaf name.
	// "data[BOT][924][AUTH][access_token]" -> "access_token"
	leaf := k
	if i := strings.LastIndex(k, "["); i >= 0 && strings.HasSuffix(k, "]") {
		leaf = k[i+1 : len(k)-1]
	}
	switch strings.ToLower(leaf) {
	case "access_token",
		"refresh_token",
		"application_token",
		"client_secret",
		"client_id",
		"auth_id",
		"refresh_id":
		return true
	}
	return false
}

// dumpRawEvent logs the parsed form body of a Bitrix24 event with
// credentials redacted. Invoked only when bitrix24LogRawEvent is true —
// the cost of key sort + string build is intentional (one-shot debug
// capture, not a hot path). Output is a sorted multi-line dump so
// successive events are diffable in log archives.
func dumpRawEvent(evt *Event) {
	if evt == nil || evt.Raw == nil {
		// parseJSONEvent doesn't populate Raw. Log a marker so operators
		// realise the JSON variant bypasses the dump.
		if evt != nil {
			slog.Info("bitrix24 event: raw dump (json variant — no raw form)",
				"event_type", evt.Type, "domain", evt.Auth.Domain)
		}
		return
	}
	keys := make([]string, 0, len(evt.Raw))
	for k := range evt.Raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		for _, v := range evt.Raw[k] {
			b.WriteString(k)
			b.WriteByte('=')
			if isRedactedEventKey(k) {
				b.WriteString("<redacted len=")
				b.WriteString(strconv.Itoa(len(v)))
				b.WriteByte('>')
			} else {
				b.WriteString(v)
			}
			b.WriteByte('\n')
		}
	}
	slog.Info("bitrix24 event: raw dump (debug)",
		"event_type", evt.Type,
		"domain", evt.Auth.Domain,
		"body", b.String())
}

// maxInstallBodyBytes caps the /bitrix24/install body. Real install callbacks
// are a few hundred bytes; the cap is only defense-in-depth against a public
// endpoint being abused to buffer huge bodies pre-auth.
const maxInstallBodyBytes = 64 << 10 // 64 KiB

// handleInstall serves /bitrix24/install.
//
// Supports both Bitrix24 app install mechanisms — they look almost identical
// on the wire but neither shares its critical fields with the other:
//
//  1. OAuth2 Marketplace app:
//     GET /bitrix24/install?code=<authcode>&domain=<portal>&state=<tenant>:<name>
//     Bitrix24 issues an authorization_code that the app exchanges for tokens.
//
//  2. Local application:
//     POST /bitrix24/install
//     body: AUTH_ID, REFRESH_ID, AUTH_EXPIRES, member_id, DOMAIN,
//     application_token, PROTOCOL, LANG, APP_SID, status, PLACEMENT
//     Tokens are already minted — no exchange call — so we skip ExchangeAuthCode
//     and persist the tokens directly.
//
// Flow detection: the two modes are disambiguated by which field the caller
// supplies. `code` + `state` present → OAuth. `AUTH_ID` + `REFRESH_ID` present
// → Local App. Presence of both is treated as Local App (Bitrix24 never sends
// both, but Local App's fields are the richer payload; prefer them).
//
// Portal resolution differs per flow: OAuth has the `state` parameter we put
// into the install URL and it disambiguates tenant + name. Local App has no
// state — Bitrix24 just POSTs the handler URL verbatim — so we resolve by
// `DOMAIN`, which is unique per installed portal.
//
// Success response is a small auto-close HTML page so the install popup
// doesn't leave an orphan tab; errors are plain text with short messages
// (detail goes to slog, never to the admin's screen).
func (r *Router) handleInstall(w http.ResponseWriter, req *http.Request) {
	// Accept HEAD for partners.bitrix24.com URL reachability ping (some
	// validators issue HEAD before GET). HEAD never carries an install
	// payload so respond 200 immediately and skip the body.
	if req.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Cap the body BEFORE ParseForm. The install endpoint is publicly reachable
	// (Bitrix admins hit it during OAuth) and sits in front of all auth checks
	// — without this cap an attacker could POST an unbounded form body and
	// exhaust memory before we even read `state`. A real install callback is
	// a few hundred bytes; 64 KiB is ~100× headroom.
	if req.Body != nil {
		req.Body = http.MaxBytesReader(nil, req.Body, maxInstallBodyBytes)
	}

	// Bitrix will POST with form body on some flows; parse both.
	_ = req.ParseForm()

	// Local App fields (case-sensitive per Bitrix24 convention).
	authID := strings.TrimSpace(req.Form.Get("AUTH_ID"))
	refreshID := strings.TrimSpace(req.Form.Get("REFRESH_ID"))
	if authID != "" && refreshID != "" {
		r.handleInstallLocalApp(w, req)
		return
	}

	// OAuth2 Marketplace fields.
	code := strings.TrimSpace(req.Form.Get("code"))
	stateParam := strings.TrimSpace(req.Form.Get("state"))
	domain := strings.TrimSpace(req.Form.Get("domain"))

	if code == "" || stateParam == "" {
		// Bitrix24 partner registration validates installer URL with a plain
		// GET (no params). Return a 200 placeholder so registration passes
		// without weakening the real-install error path: a POST without
		// proper params is still a bad install attempt and gets 400.
		if req.Method == http.MethodGet {
			renderBitrixPlaceholder(w, "GoClaw — Bitrix24 Install Endpoint",
				"This URL is invoked by Bitrix24 during application installation.")
			return
		}
		http.Error(w, "missing code or state (OAuth) / AUTH_ID+REFRESH_ID (Local App)", http.StatusBadRequest)
		return
	}

	tid, name, ok := parseInstallState(stateParam)
	if !ok {
		http.Error(w, "invalid state format", http.StatusBadRequest)
		return
	}

	portal, exists := r.PortalByKey(tid, name)
	if !exists {
		slog.Warn("bitrix24 install: unknown portal",
			"tenant", tid, "portal", name)
		http.Error(w, "unknown portal", http.StatusNotFound)
		return
	}

	if domain != "" && !strings.EqualFold(domain, portal.Domain()) {
		slog.Warn("bitrix24 install: domain mismatch",
			"tenant", tid, "portal", name,
			"expected", portal.Domain(), "received", domain)
		http.Error(w, "domain mismatch", http.StatusForbidden)
		return
	}

	ctx := store.WithTenantID(req.Context(), tid)
	if err := portal.Exchange(ctx, code); err != nil {
		slog.Warn("bitrix24 install: exchange failed",
			"tenant", tid, "portal", name, "err", err)
		http.Error(w, "exchange failed", http.StatusBadGateway)
		return
	}

	// Best-effort: capture the gateway public URL Bitrix24 just used to reach us.
	// Channel.eventHandlerURL() will read this when registering imbot event
	// callbacks. Failure (private host, missing headers) is non-fatal — install
	// succeeded; eventHandlerURL falls back to legacy config.public_url.
	capturePublicURL(ctx, portal, req, nil)

	// Refresh domain index in case the first Exchange arrived before the
	// initial RegisterPortal was able to read a stored domain.
	r.mu.Lock()
	r.setDomainLocked(portal.Domain(), portalKey(tid, name))
	r.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(installSuccessHTML))
}

// handleInstallLocalApp finishes install for a Bitrix24 Local App. Body already
// parsed by the caller; AUTH_ID + REFRESH_ID presence already checked.
//
// Portal resolution is by DOMAIN. Local Apps don't round-trip through our
// install URL so there's no state param to carry (tenant, name) — the only
// stable identifier in the POST body is DOMAIN, which matches the `domain`
// column on `bitrix_portals`. PortalByDomain enforces this is O(1) via the
// Router's domain index.
func (r *Router) handleInstallLocalApp(w http.ResponseWriter, req *http.Request) {
	authID := strings.TrimSpace(req.Form.Get("AUTH_ID"))
	refreshID := strings.TrimSpace(req.Form.Get("REFRESH_ID"))
	domain := strings.TrimSpace(req.Form.Get("DOMAIN"))
	memberID := strings.TrimSpace(req.Form.Get("member_id"))
	appToken := strings.TrimSpace(req.Form.Get("application_token"))
	expiresStr := strings.TrimSpace(req.Form.Get("AUTH_EXPIRES"))

	if domain == "" {
		http.Error(w, "missing DOMAIN", http.StatusBadRequest)
		return
	}

	portal, ok := r.PortalByDomain(domain)
	if !ok {
		slog.Warn("bitrix24 install (local): unknown portal domain", "domain", domain)
		http.Error(w, "unknown portal", http.StatusNotFound)
		return
	}

	// Parse AUTH_EXPIRES — Bitrix24 sends seconds as a decimal string. A
	// missing/unparseable value falls through to Portal.applyTokenResponse's
	// defaultTokenTTL clamp, so no extra branch here.
	var expiresIn int64
	if expiresStr != "" {
		if v, err := strconv.ParseInt(expiresStr, 10, 64); err == nil && v > 0 {
			expiresIn = v
		}
	}

	tr := &TokenResponse{
		AccessToken:      authID,
		RefreshToken:     refreshID,
		ExpiresIn:        expiresIn,
		Domain:           domain,
		MemberID:         memberID,
		ApplicationToken: appToken,
	}

	ctx := store.WithTenantID(req.Context(), portal.TenantID())
	if err := portal.InstallFromTokens(ctx, tr); err != nil {
		slog.Warn("bitrix24 install (local): persist failed",
			"tenant", portal.TenantID(), "portal", portal.Name(), "err", err)
		http.Error(w, "install failed", http.StatusBadGateway)
		return
	}

	// Best-effort: capture the gateway public URL Bitrix24 just used to reach us.
	// See handleInstall (OAuth path) for rationale.
	capturePublicURL(ctx, portal, req, nil)

	// Refresh domain index in case the first install landed before RegisterPortal
	// could read a stored domain (mirrors OAuth path above).
	r.mu.Lock()
	r.setDomainLocked(domain, portalKey(portal.TenantID(), portal.Name()))
	r.mu.Unlock()

	// Visible signal in logs so operators can confirm a Local App reinstall
	// actually reached the handler. The happy path used to be silent, which
	// made "did the reinstall POST arrive?" unanswerable from logs alone.
	slog.Info("bitrix24 install (local): tokens persisted",
		"tenant", portal.TenantID(), "portal", portal.Name(),
		"domain", domain, "member_id", memberID, "expires_in", expiresIn)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(installSuccessHTML))
}

// handleEvent serves /bitrix24/events.
//
// Control flow:
//  1. ParseEvent → 400 on parse failure
//  2. Lookup portal by auth.domain → 404 on miss
//  3. Validate application_token against portal.AppToken() → 401 on mismatch
//     and slog.Warn("security.bitrix24_apptoken_mismatch", ...)
//  4. Dedup on (domain + ":" + MESSAGE_ID) → 200 {"duplicate":true} on hit
//     (2xx so Bitrix won't retry; the message was already delivered once)
//  5. Lookup dispatcher by BotID → 404 on miss
//  6. Spawn goroutine: dispatcher.DispatchEvent(ctx, evt)
//  7. 200 {"ok":true} — we ack immediately; Bitrix has a 10s timeout
//
// Steps 1–5 are synchronous and cheap; step 6 is the only async work.
func (r *Router) handleEvent(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Entry trace so "did Bitrix POST anything to our events URL?" is
	// answerable from logs without adding middleware. Keep at INFO for now —
	// can drop to Debug once the wire-up is stable in production.
	slog.Info("bitrix24 event: inbound",
		"remote", req.RemoteAddr,
		"content_length", req.ContentLength,
		"user_agent", req.Header.Get("User-Agent"))

	evt, err := ParseEvent(req)
	if err != nil {
		slog.Warn("bitrix24 event: parse failed", "err", err)
		http.Error(w, "parse failed", http.StatusBadRequest)
		return
	}

	// Opt-in raw-body dump for debugging. Gated by BITRIX24_LOG_RAW_EVENT
	// env at process start — never on in steady-state because the dump
	// leaks user message text to logs.
	if bitrix24LogRawEvent {
		dumpRawEvent(evt)
	}

	if evt.Auth.Domain == "" {
		writeJSONError(w, http.StatusBadRequest, "missing auth.domain")
		return
	}

	portal, ok := r.PortalByDomain(evt.Auth.Domain)
	if !ok {
		slog.Warn("bitrix24 event: unknown portal domain",
			"domain", evt.Auth.Domain, "event", evt.Type)
		writeJSONError(w, http.StatusNotFound, "unknown portal")
		return
	}

	// App-token check. Constant-time compare is overkill for a per-install
	// secret (not a password) but the cost is negligible and it avoids
	// timing-side-channel surprises if this ever grows hot.
	want := portal.AppToken()
	got := evt.Auth.AppToken
	if want == "" {
		// Bootstrap path: Bitrix24 Local App install POST does NOT include
		// application_token (only AUTH_ID / REFRESH_ID / member_id are sent).
		// The token first appears in the event stream. Seed state from this
		// event iff member_id matches what install persisted — see
		// Portal.BootstrapAppToken for the full trust argument.
		if got != "" {
			if err := portal.BootstrapAppToken(req.Context(), evt.Auth.MemberID, got); err != nil {
				slog.Warn("security.bitrix24_apptoken_bootstrap_failed",
					"tenant", portal.TenantID(), "portal", portal.Name(),
					"domain", evt.Auth.Domain, "event", evt.Type, "err", err)
				writeJSONError(w, http.StatusUnauthorized, "app_token bootstrap rejected")
				return
			}
			slog.Info("bitrix24 event: app_token bootstrapped on first event",
				"tenant", portal.TenantID(), "portal", portal.Name(),
				"domain", evt.Auth.Domain, "event", evt.Type,
				"member_id", evt.Auth.MemberID)
			want = got // proceed to secureEqual below — will now match
		} else {
			slog.Warn("security.bitrix24_apptoken_missing",
				"tenant", portal.TenantID(), "portal", portal.Name(), "domain", evt.Auth.Domain)
			writeJSONError(w, http.StatusUnauthorized, "portal not installed")
			return
		}
	}
	if !secureEqual(want, got) {
		slog.Warn("security.bitrix24_apptoken_mismatch",
			"tenant", portal.TenantID(), "portal", portal.Name(),
			"domain", evt.Auth.Domain, "event", evt.Type)
		writeJSONError(w, http.StatusUnauthorized, "invalid application_token")
		return
	}

	// Dedup by (domain, MESSAGE_ID). Events without MESSAGE_ID (e.g. joinChat)
	// bypass dedup since there's nothing to key on — those handlers are
	// idempotent at the agent layer.
	if evt.Params.MessageID != "" {
		key := evt.Auth.Domain + ":" + evt.Type + ":" + evt.Params.MessageID
		if r.dedup.Seen(key) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"duplicate":true}`))
			return
		}
	}

	// Bot lookup. OnInstall events can arrive before the first bot register,
	// so only message/edit/delete events require a dispatcher.
	switch evt.Type {
	case EventAppUninstall:
		// App-level uninstall: drop all bots for this portal and ack.
		r.handleAppUninstall(portal)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
		return
	}

	if evt.Params.BotID == 0 {
		writeJSONError(w, http.StatusBadRequest, "missing BOT_ID")
		return
	}
	r.mu.RLock()
	disp, hasBot := r.byBotID[evt.Params.BotID]
	r.mu.RUnlock()
	if !hasBot {
		slog.Warn("bitrix24 event: unknown bot",
			"bot_id", evt.Params.BotID, "tenant", portal.TenantID(),
			"portal", portal.Name(), "event", evt.Type)
		writeJSONError(w, http.StatusNotFound, "unknown bot")
		return
	}

	// ONIMBOTDELETE terminates the channel side too — unregister and ack.
	if evt.Type == EventBotDelete {
		r.UnregisterBot(evt.Params.BotID)
	}

	// Async dispatch. DispatchEvent is contractually non-blocking (bounded
	// internal queue); we still wrap in a goroutine to isolate any panic and keep
	// this handler's latency <50ms.
	//
	// IMPORTANT: net/http cancels req.Context() as soon as this handler
	// returns. The dispatcher goroutine outlives the handler, so we must
	// detach the context before handing it off — otherwise every downstream
	// DB / pairing / LLM call inside the dispatcher fails with
	// context.Canceled the moment we write "200 OK" below. We still want
	// request-scoped values (trace ids etc.) so we use WithoutCancel rather
	// than context.Background().
	ctx := store.WithTenantID(context.WithoutCancel(req.Context()), portal.TenantID())
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("bitrix24 event: dispatcher panic",
					"bot_id", evt.Params.BotID, "event", evt.Type, "panic", rec)
			}
		}()
		disp.DispatchEvent(ctx, evt)
	}()

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleAppUninstall is called when Bitrix reports the app was removed from
// the portal. We drop all bot entries for that portal so further events
// (retries, stragglers) return 404 instead of hitting a stale dispatcher.
// The portal row in SQLite is NOT deleted — admins may reinstall and we
// want the (client_id, client_secret) to survive.
func (r *Router) handleAppUninstall(p *Portal) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tenantKey := portalKey(p.TenantID(), p.Name())
	// Drop every bot whose dispatcher reports the same tenantKey.
	for botID, disp := range r.byBotID {
		if portalKey(disp.TenantID(), disp.PortalName()) == tenantKey {
			delete(r.byBotID, botID)
		}
	}
}

// writeJSONError is a small helper that writes {"error":"<msg>"} with the
// given HTTP status. Using JSON across the endpoint keeps response shape
// predictable for integration tests and clients.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]string{"error": msg}
	_ = json.NewEncoder(w).Encode(payload)
}

// secureEqual returns a==b in constant-ish time relative to len(a). For
// per-install app tokens this is defensive; the primary check is still
// the domain lookup that narrows the comparison to one known token.
func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// renderBitrixPlaceholder writes a minimal 200 OK HTML page used when
// partners.bitrix24.com validates a registered app's URLs at registration
// time (Application URL / Application installer URL / Application settings
// handler). Bitrix performs a plain GET and rejects any non-2xx response.
//
// Kept intentionally tiny and content-free — the production behavior at
// these URLs (real install POST, app iframe load with AUTH_ID, etc.) is
// handled by the matching handler dispatch; this helper only covers the
// validation ping path.
func renderBitrixPlaceholder(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>` + title + `</title>
<style>body{font:15px/1.5 system-ui,sans-serif;padding:2rem;color:#222}h1{margin:0 0 .5rem}</style>
</head><body><h1>` + title + `</h1><p>` + body + `</p></body></html>`))
}

// handleAppPage serves /bitrix24/handler — the URL Bitrix24 iframe-loads
// when a user opens the GoClaw app inside their portal interface. Used as
// the "Application URL" and "Application settings handler" in the partner
// app registration form.
//
// Currently responds with the 200 placeholder required by Bitrix24 app URL
// validation. A future POST handler can process the opening user's Bitrix24
// tokens and forward them to the MCP onboarding endpoint.
func (r *Router) handleAppPage(w http.ResponseWriter, req *http.Request) {
	// Accept HEAD for Bitrix24 URL reachability ping at registration time.
	if req.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	renderBitrixPlaceholder(w,
		"GoClaw — Bitrix24 Application",
		"This page is loaded inside Bitrix24 when a user opens the GoClaw bot application.")
}
