package max

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultWebhookPath is used when cfg.WebhookURL has no path component.
// Channels are namespaced by the URL path component the operator chooses,
// allowing multiple Max bots in one goclaw deployment without colliding
// routes — the recommended pattern is to embed a hard-to-guess token
// (e.g. UUID) in the path.
const defaultWebhookPath = "/max/webhook"

// maxWebhookBodyBytes caps the size of webhook POST bodies. Max API updates
// are typically <10 KB; this cushion guards against accidental or malicious
// oversized bodies while leaving plenty of room for messages with attachments.
const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// webhookDispatchTimeout bounds the lifetime of an async dispatch goroutine
// spawned per webhook delivery. Long enough for the agent to produce a full
// reply (including streaming and media uploads); short enough that wedged
// dispatches don't accumulate goroutines.
//
// 5 minutes matches the upper bound of typical agent run lengths in
// production (most under 30s, p99 under 2m).
const webhookDispatchTimeout = 5 * time.Minute

// WebhookHandler returns the HTTP handler and mount path for this Max channel.
// Implements channels.WebhookChannel.
//
// Returns ("", nil) if the channel is not in webhook mode — the gateway will
// then skip mounting and use the polling lifecycle started in Start().
//
// The path is derived from cfg.WebhookURL, which the operator configures to
// match a publicly-reachable HTTPS endpoint that proxies to this gateway.
//
// Authentication: Max does not send the bot token with webhook updates; the
// only authentication is URL secrecy. Operators MUST configure WebhookURL
// with a hard-to-guess path component (e.g. a UUID).
func (c *Channel) WebhookHandler() (string, http.Handler) {
	if c.cfg.Mode != "webhook" {
		return "", nil
	}
	if c.cfg.WebhookURL == "" {
		return "", nil
	}

	path, err := webhookPathFromURL(c.cfg.WebhookURL)
	if err != nil {
		slog.Warn("max: invalid webhook_url, skipping webhook mount",
			"channel", c.Name(), "error", err)
		return "", nil
	}

	return path, http.HandlerFunc(c.serveWebhook)
}

// webhookPathFromURL extracts the path portion of a webhook URL.
// Returns an error for malformed input or non-HTTPS schemes.
// Empty path defaults to defaultWebhookPath.
func webhookPathFromURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook_url: %w", err)
	}
	if u.Scheme != "https" {
		return "", errors.New("webhook_url must use https://")
	}
	path := u.Path
	if path == "" || path == "/" {
		return defaultWebhookPath, nil
	}
	return strings.TrimRight(path, "/"), nil
}

// serveWebhook is the HTTP handler invoked by the gateway mux for incoming
// Max webhook POSTs.
//
// Contract with Max API:
//   - Max retries non-2xx responses, so we 200 even on internal dispatch
//     errors (logged separately) to avoid retry storms from transient bugs.
//   - We respond promptly and dispatch the update on the channel's long-lived
//     run context so handler goroutines can outlive the HTTP request.
//
// Method handling:
//   - POST  → process update
//   - other → 405 Method Not Allowed
//
// Body limits:
//   - bodies >maxWebhookBodyBytes → 413 Payload Too Large
//
// Decode errors return 400 with a logged preview to aid debugging.
func (c *Channel) serveWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			slog.Warn("max: webhook body too large",
				"channel", c.Name(), "limit", maxWebhookBodyBytes)
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		slog.Warn("max: webhook read body failed",
			"channel", c.Name(), "error", err)
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}

	var update Update
	if err := json.Unmarshal(body, &update); err != nil {
		slog.Warn("max: webhook json decode failed",
			"channel", c.Name(), "error", err,
			"body_preview", truncateForLog(body, 200))
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Dispatch on the channel's run context so handler goroutines can outlive
	// this HTTP request. Falls back to context.Background() if Start() hasn't
	// been called yet (should not happen — handler isn't mounted until Start).
	// Dispatch on a fresh context independent of the channel's polling
	// lifecycle. Webhook deliveries are individual requests from Max API:
	// they must not be cancelled by goclaw rolling restarts or Stop. Once
	// the HTTP request has been parsed and we're going to 200-OK Max, we
	// own the message — we cannot lose it because Stop happened to fire.
	//
	// Bounded timeout (webhookDispatchTimeout) prevents goroutine leaks
	// if dispatch wedges (e.g. agent loop hangs).
	dispatchCtx, dispatchCancel := context.WithTimeout(context.Background(), webhookDispatchTimeout)
	go func() {
		defer dispatchCancel()
		c.handleUpdate(dispatchCtx, update)
	}()

	w.WriteHeader(http.StatusOK)
}

// (runContext was removed — webhook now uses a fresh context per delivery.
// The previous implementation read c.pollRunCtx, which created a
// cancellation race during Stop: a webhook arriving mid-Stop could see a
// just-cancelled context and drop the message after we'd already 200-OK'd
// Max. See Day 5b commit message.)

// pollContext returns the long-lived context spawned by Start, or
// context.Background() if Start has not yet executed. Used by per-chat
// reaction refreshers, which must stop when the channel stops — this is
// the opposite of webhook handlers, which must complete even after Stop.
//
// Do not use for inbound message dispatch.
func (c *Channel) pollContext() context.Context {
	c.runCtxMu.RLock()
	defer c.runCtxMu.RUnlock()
	if c.pollRunCtx != nil {
		return c.pollRunCtx
	}
	return context.Background()
}
