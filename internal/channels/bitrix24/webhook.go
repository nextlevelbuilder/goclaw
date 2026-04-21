package bitrix24

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxInstallBodyBytes caps the /bitrix24/install body. Real install callbacks
// are a few hundred bytes; the cap is only defense-in-depth against a public
// endpoint being abused to buffer huge bodies pre-auth.
const maxInstallBodyBytes = 64 << 10 // 64 KiB

// handleInstall serves /bitrix24/install.
//
// Bitrix24 redirects admins here after the OAuth consent step with:
//
//	GET /bitrix24/install?code=<authcode>&domain=<portal>&state=<tenant_uuid>:<portal_name>&member_id=<mem>
//
// Success response is a small auto-close HTML page so the install popup
// doesn't leave an orphan tab; errors are plain text with short messages
// (detail goes to slog, never to the admin's screen).
func (r *Router) handleInstall(w http.ResponseWriter, req *http.Request) {
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
	code := strings.TrimSpace(req.Form.Get("code"))
	stateParam := strings.TrimSpace(req.Form.Get("state"))
	domain := strings.TrimSpace(req.Form.Get("domain"))

	if code == "" || stateParam == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
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

	// Refresh domain index in case the first Exchange arrived before the
	// initial RegisterPortal was able to read a stored domain.
	r.mu.Lock()
	if d := strings.ToLower(strings.TrimSpace(portal.Domain())); d != "" {
		r.domains[d] = portalKey(tid, name)
	}
	r.mu.Unlock()

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

	evt, err := ParseEvent(req)
	if err != nil {
		slog.Warn("bitrix24 event: parse failed", "err", err)
		http.Error(w, "parse failed", http.StatusBadRequest)
		return
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
		// Portal not yet installed — reject rather than accept unsigned events.
		slog.Warn("security.bitrix24_apptoken_missing",
			"tenant", portal.TenantID(), "portal", portal.Name(), "domain", evt.Auth.Domain)
		writeJSONError(w, http.StatusUnauthorized, "portal not installed")
		return
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
	// internal queue in Phase 03); we still wrap in a goroutine to isolate
	// any panic and keep this handler's latency <50ms.
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
