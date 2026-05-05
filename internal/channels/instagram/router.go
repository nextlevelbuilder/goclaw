package instagram

import (
	"log/slog"
	"net/http"
	"sync"
)

// webhookRouter routes incoming Instagram webhook events to the correct
// channel instance by instagram_user_id (entry.ID). A single HTTP handler
// is shared across all Instagram channel instances on the same gateway.
//
// Multi-Meta-App note: all instances registered here are expected to share
// the same Meta App (and thus the same app_secret). If instances with
// different secrets are registered, ServeHTTP accepts a payload if its
// signature matches any known secret. In the common single-app case there
// is exactly one secret and behavior is unchanged.
type webhookRouter struct {
	mu           sync.RWMutex
	instances    map[string]*Channel // instagram_user_id → channel
	routeHandled bool                // true after first webhookRoute() call
}

var globalRouter = &webhookRouter{
	instances: make(map[string]*Channel),
}

func (r *webhookRouter) register(ch *Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[ch.instagramID] = ch
}

func (r *webhookRouter) unregister(instagramID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, instagramID)
}

// webhookRoute returns the path+handler for the first call; ("", nil) for subsequent calls.
// Only one instance registers the HTTP route with the gateway mux; the shared
// ServeHTTP below dispatches per-entry to the right *Channel.
func (r *webhookRouter) webhookRoute() (string, http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.routeHandled {
		r.routeHandled = true
		return webhookPath, r
	}
	return "", nil
}

// ServeHTTP is the shared handler for all Instagram webhooks.
// Routes each entry to the matching channel instance by instagram_user_id.
//
// Multi-Meta-App support: all registered app secrets are collected and tried
// in order. A payload is accepted if its signature matches any known
// app_secret. In the common case (single Meta App) there is exactly one
// secret and behavior is unchanged.
func (r *webhookRouter) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	var primarySecret, verifyToken string
	var extraSecrets []string
	seenSecrets := make(map[string]bool)
	for _, ch := range r.instances {
		s := ch.webhookH.appSecret
		if primarySecret == "" {
			primarySecret = s
			verifyToken = ch.webhookH.verifyToken
			seenSecrets[s] = true
		} else if !seenSecrets[s] {
			extraSecrets = append(extraSecrets, s)
			seenSecrets[s] = true
		}
	}
	r.mu.RUnlock()

	if primarySecret == "" {
		// No instances registered yet. Always 200 so Meta doesn't retry.
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(extraSecrets) > 0 {
		slog.Warn("security.instagram_multi_meta_app",
			"extra_app_count", len(extraSecrets),
			"note", "multiple Meta App secrets registered; payloads verified against all known secrets")
	}

	routingWH := &WebhookHandler{
		appSecret:    primarySecret,
		verifyToken:  verifyToken,
		extraSecrets: extraSecrets,
	}
	routingWH.onMessage = func(entry WebhookEntry, event MessagingEvent) {
		r.mu.RLock()
		target := r.instances[entry.ID]
		r.mu.RUnlock()
		if target != nil {
			target.handleMessagingEvent(entry, event)
		}
	}
	routingWH.ServeHTTP(w, req)
}
