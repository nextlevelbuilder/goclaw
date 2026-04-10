package facebook

import (
	"log/slog"
	"net/http"
	"sync"
)

// webhookRouter routes incoming Facebook webhook events to the correct channel instance by page_id.
// A single HTTP handler is shared across all facebook channel instances on the same server.
//
// Multi-Meta-App note: all page instances registered here are expected to share the same Meta App
// (and thus the same app_secret). If instances with different secrets are registered, ServeHTTP
// tries all known secrets and accepts the payload if any matches.
type webhookRouter struct {
	mu           sync.RWMutex
	instances    map[string]*Channel // pageID → channel
	routeHandled bool                // true after first webhookRoute() call
}

var globalRouter = &webhookRouter{
	instances: make(map[string]*Channel),
}

func (r *webhookRouter) register(ch *Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.instances[ch.pageID] = ch
}

func (r *webhookRouter) unregister(pageID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.instances, pageID)
}

// webhookRoute returns the path+handler for the first call; ("", nil) for subsequent calls.
func (r *webhookRouter) webhookRoute() (string, http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.routeHandled {
		r.routeHandled = true
		return webhookPath, r
	}
	return "", nil
}

// ServeHTTP is the shared handler for all Facebook page webhooks.
// Routes each entry to the matching channel instance by page_id.
//
// Multi-Meta-App support: all registered page secrets are collected and tried in order.
// A payload is accepted if its signature matches any known app_secret. In the common
// case (single Meta App) there is exactly one secret and behavior is unchanged.
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
		// No instances registered yet.
		w.WriteHeader(http.StatusOK)
		return
	}

	if len(extraSecrets) > 0 {
		slog.Warn("security.facebook_multi_meta_app",
			"extra_app_count", len(extraSecrets),
			"note", "multiple Meta App secrets registered; payloads verified against all known secrets")
	}

	routingWH := &WebhookHandler{
		appSecret:    primarySecret,
		verifyToken:  verifyToken,
		extraSecrets: extraSecrets,
	}
	routingWH.onComment = func(entry WebhookEntry, change ChangeValue) {
		r.mu.RLock()
		target := r.instances[entry.ID]
		r.mu.RUnlock()
		if target != nil {
			target.handleCommentEvent(entry, change)
		}
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
