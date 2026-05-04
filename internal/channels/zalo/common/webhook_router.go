package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Router dispatches webhook POSTs to registered Zalo channel instances by
// path-suffix slug. Channels register at Start() and unregister at Stop();
// the process-global router (shared.go) is mounted once on the mux via
// MountRoute() at the WebhookPathPrefix prefix.
type Router struct {
	mu              sync.RWMutex
	instances       map[uuid.UUID]*registeredInstance
	slugToInstance  map[string]uuid.UUID
	instanceToSlug  map[uuid.UUID]string
	dedup           *Dedup
	rateLimiter     *channels.WebhookRateLimiter
	maxBodySize     int64

	routeMu      sync.Mutex
	routeHandled bool
}

// MountRoute returns (WebhookPathPrefix, r) on the first call across the shared
// router and ("", nil) afterwards. Sticky across instance_loader.Reload
// because http.ServeMux would panic on re-mount.
func (r *Router) MountRoute() (string, http.Handler) {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	if !r.routeHandled {
		r.routeHandled = true
		return WebhookPathPrefix, r
	}
	return "", nil
}

// emptyIDStreakWarnThreshold catches schema drift where the extractor
// silently disables dedup by always returning empty.
const emptyIDStreakWarnThreshold = 10

type registeredInstance struct {
	handler  WebhookHandler
	tenantID uuid.UUID

	ctx    context.Context
	cancel context.CancelFunc

	dispatchWG sync.WaitGroup

	// emptyIDStreak counts consecutive empty extractor returns; resets on
	// any non-empty extraction.
	emptyIDStreak atomic.Int64
}

// WebhookHandler is the per-instance contract the router invokes after
// rate limit / signature / dedup checks pass.
type WebhookHandler interface {
	HandleWebhookEvent(ctx context.Context, raw json.RawMessage) error
	SignatureVerifier() SignatureVerifier
	MessageIDExtractor() MessageIDExtractor
}

// SignatureVerifier validates per-request authenticity.
type SignatureVerifier interface {
	Verify(headers http.Header, body []byte) error
}

// MessageIDExtractor pulls the dedup id; "" disables dedup for the event.
type MessageIDExtractor interface {
	ExtractMessageID(raw json.RawMessage) string
}

// ErrSignatureMismatch is the canonical signature-mismatch error; the
// router maps it to 401.
var ErrSignatureMismatch = errors.New("zalo_common: webhook signature mismatch")

// ErrSlugCollision is returned by RegisterInstance when two channels claim
// the same slug. Caller should MarkFailed with kind=config.
var ErrSlugCollision = errors.New("zalo_common: webhook slug already in use")

const (
	defaultDedupTTL     = 5 * time.Minute
	defaultDedupMax     = 1000
	defaultMaxBodyBytes = 1 * 1024 * 1024
)

// NewRouter returns a router with default dedup and rate-limit params.
func NewRouter() *Router {
	return &Router{
		instances:      make(map[uuid.UUID]*registeredInstance),
		slugToInstance: make(map[string]uuid.UUID),
		instanceToSlug: make(map[uuid.UUID]string),
		dedup:          NewDedup(defaultDedupTTL, defaultDedupMax),
		rateLimiter:    channels.NewWebhookRateLimiter(),
		maxBodySize:    defaultMaxBodyBytes,
	}
}

// RegisterInstance enrolls a channel for routing under the given slug.
// Returns ErrSlugInvalid for malformed slugs, ErrSlugCollision when
// another channel already owns the slug. The per-instance ctx is cancelled
// by UnregisterInstance so dispatch goroutines bail promptly.
func (r *Router) RegisterInstance(id uuid.UUID, h WebhookHandler, tenantID uuid.UUID, slug string) error {
	if id == uuid.Nil {
		return fmt.Errorf("zalo_common: register requires non-nil instance id")
	}
	if err := ValidateSlug(slug); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	if tenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, tenantID)
	}
	inst := &registeredInstance{
		handler:  h,
		tenantID: tenantID,
		ctx:      ctx,
		cancel:   cancel,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.slugToInstance[slug]; ok && existing != id {
		cancel()
		return fmt.Errorf("%w: slug %q already registered", ErrSlugCollision, slug)
	}
	// Re-register under same id: clear old slug mapping if it changed.
	if oldSlug, ok := r.instanceToSlug[id]; ok && oldSlug != slug {
		delete(r.slugToInstance, oldSlug)
	}
	r.instances[id] = inst
	r.slugToInstance[slug] = id
	r.instanceToSlug[id] = slug
	return nil
}

// unregisterDrainTimeout bounds Stop()/Reload() so a slow handler can't hang shutdown.
const unregisterDrainTimeout = 5 * time.Second

// UnregisterInstance removes the channel, cancels its dispatch ctx, and
// drains in-flight dispatch goroutines (bounded). Idempotent.
func (r *Router) UnregisterInstance(id uuid.UUID) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	delete(r.instances, id)
	if slug, hasSlug := r.instanceToSlug[id]; hasSlug {
		delete(r.slugToInstance, slug)
		delete(r.instanceToSlug, id)
	}
	r.mu.Unlock()
	if !ok {
		return
	}
	if inst.cancel != nil {
		inst.cancel()
	}
	done := make(chan struct{})
	go func() {
		inst.dispatchWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(unregisterDrainTimeout):
		slog.Warn("zalo_webhook.unregister_drain_timeout",
			"instance_id", id, "timeout", unregisterDrainTimeout)
	}
}

func (r *Router) lookupBySlug(slug string) (uuid.UUID, *registeredInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.slugToInstance[slug]
	if !ok {
		return uuid.Nil, nil, false
	}
	inst, ok := r.instances[id]
	return id, inst, ok
}

// reserveDispatchSlot does lookup + dispatchWG.Add(1) atomically under RLock.
// UnregisterInstance takes the write lock before Wait, so this prevents the
// "WaitGroup reused before previous Wait returned" race during reload.
func (r *Router) reserveDispatchSlot(slug string) (uuid.UUID, *registeredInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.slugToInstance[slug]
	if !ok {
		return uuid.Nil, nil, false
	}
	inst, ok := r.instances[id]
	if !ok {
		return uuid.Nil, nil, false
	}
	inst.dispatchWG.Add(1)
	return id, inst, true
}

// ServeHTTP returns 200 once dispatch reaches the handler — Zalo retries
// hard on non-2xx, so handler errors are logged, not surfaced. Pre-dispatch
// failures (auth, rate limit, parse) return 4xx for operator visibility.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(req.URL.Path, WebhookPathPrefix)
	// Reject empty suffix and any nested path / traversal attempt.
	if suffix == "" || strings.Contains(suffix, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := ValidateSlug(suffix); err != nil {
		// Path doesn't conform to slug grammar — treat as not found.
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	instanceID, inst, ok := r.lookupBySlug(suffix)
	if !ok {
		http.Error(w, "unknown instance", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, r.maxBodySize))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if err := inst.handler.SignatureVerifier().Verify(req.Header, body); err != nil {
		slog.Warn("security.zalo_webhook_signature_mismatch",
			"instance_id", instanceID,
			"slug", suffix,
			"remote", req.RemoteAddr,
			"err", err)
		http.Error(w, "signature mismatch", http.StatusUnauthorized)
		return
	}

	// Limit AFTER signature verify so a guessed slug can't burn the bucket
	// for legitimate Zalo deliveries. HMAC verify is cheap (~µs).
	if !r.rateLimiter.Allow(instanceID.String()) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	mid := inst.handler.MessageIDExtractor().ExtractMessageID(body)
	if mid == "" {
		// Warn-and-reset at threshold so silent schema drift doesn't go
		// unnoticed; throttles to one warn per threshold-event window.
		n := inst.emptyIDStreak.Add(1)
		if n >= emptyIDStreakWarnThreshold {
			inst.emptyIDStreak.Store(0)
			slog.Warn("zalo_webhook.empty_message_id_streak",
				"count", n,
				"instance_id", instanceID,
				"hint", "extractor may need update for schema drift")
		}
	} else {
		inst.emptyIDStreak.Store(0)
	}

	resolvedID, resolvedInst, ok := r.reserveDispatchSlot(suffix)
	if !ok || resolvedID != instanceID || resolvedInst != inst {
		// Reload swapped the registration between Verify and reserveDispatchSlot.
		slog.Debug("zalo_webhook.reload_race_dropped",
			"slug", suffix,
			"verified_instance_id", instanceID,
			"resolved_instance_id", resolvedID,
			"resolved_ok", ok)
		w.WriteHeader(http.StatusOK)
		return
	}
	// Admit to dedup AFTER the dispatch slot is reserved so reload-dropped
	// requests don't waste TTL slots keyed by a stale instanceID.
	if mid != "" && r.dedup.SeenOrAdd(instanceID, mid) {
		inst.dispatchWG.Done()
		w.WriteHeader(http.StatusOK)
		return
	}
	go r.dispatch(instanceID, inst, body)
	w.WriteHeader(http.StatusOK)
}

// dispatch runs the handler in a goroutine so the HTTP ack isn't blocked
// (Zalo expects ack within ~2s). Panics are recovered and logged.
func (r *Router) dispatch(instanceID uuid.UUID, inst *registeredInstance, body []byte) {
	defer inst.dispatchWG.Done()
	defer safego.Recover(nil, "instance_id", instanceID, "tenant_id", inst.tenantID)
	if err := inst.handler.HandleWebhookEvent(inst.ctx, body); err != nil {
		slog.Error("zalo_webhook.handler_error",
			"instance_id", instanceID,
			"tenant_id", inst.tenantID,
			"err", err)
	}
}
