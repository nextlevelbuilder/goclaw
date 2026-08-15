package http

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// webhookSyncReservationTTL must exceed the longest legitimate sync webhook path.
// Telegram media sends can run for 3 minutes on slow uploads; keep enough margin
// so a duplicate idempotency request cannot mark an active send as expired.
const webhookSyncReservationTTL = 10 * time.Minute

// checkIdempotency inspects the Idempotency-Key header and resolves prior calls.
//
// Returns:
//   - (true, nil)    — no key present; proceed normally.
//   - (true, nil)    — key present, no prior call; caller should record the call
//     after handler success (phases 05/06).
//   - (false, nil)   — key matches prior call with same body → response already
//     written (HTTP 200 replay). Handler must not write again.
//   - (false, error) — 409 Conflict written (body hash mismatch). Handler must
//     not write again.
//
// Body hash is SHA-256 of the raw request body bytes (already buffered by
// readLimitedBody at this point).
func checkIdempotency(
	w http.ResponseWriter,
	r *http.Request,
	body []byte,
	webhookID uuid.UUID,
	calls store.WebhookCallStore,
) (proceed bool, err error) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		return true, nil
	}

	bodyHash := sha256Hex(body)
	ctx := r.Context()
	locale := store.LocaleFromContext(ctx)

	existing, err := calls.GetByIdempotency(ctx, webhookID, key)
	if errors.Is(err, sql.ErrNoRows) {
		// First time this key is seen — caller proceeds; let handler record call.
		return true, nil
	}
	if err != nil {
		// Store error — fail open (don't block on idempotency store errors).
		return true, nil
	}

	// Prior call found — check body hash stored in request_payload JSON.
	// Post-K2 all producers emit {"body_hash":"<64-hex>","meta":{...}}.
	// Fail-closed: empty storedHash (malformed row) is treated as mismatch → 409.
	// This prevents a corrupt or tampered stored row from serving as a replay vehicle
	// for arbitrary request bodies.
	storedHash := extractBodyHash(existing.RequestPayload)
	if storedHash != bodyHash {
		// Same key, different (or unverifiable) body → 409 Conflict.
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": i18n.T(locale, i18n.MsgWebhookIdempotencyConflict),
		})
		return false, errors.New("idempotency conflict")
	}

	expireStaleSyncReservation(ctx, calls, existing, time.Now())

	// Same key + matching body → replay last stored response.
	if len(existing.Response) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Idempotency-Replayed", "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(existing.Response)
		return false, nil
	}

	// Call exists but response not yet written (still queued/running).
	// Return 202 Accepted so the caller knows to poll.
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  existing.Status,
		"call_id": existing.ID.String(),
	})
	return false, nil
}

// sha256Hex returns the lowercase hex SHA-256 digest of b.
func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// extractBodyHash parses the canonical audit payload JSON and returns body_hash.
// Expected shape: {"body_hash": "<sha256-hex-64-chars>", "meta": {...}}.
//
// Fail-closed: returns "" on any parse failure or if body_hash is not exactly
// 64 lowercase hex characters — preventing hash bypass via malformed payloads.
func extractBodyHash(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var p struct {
		BodyHash string `json:"body_hash"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	if len(p.BodyHash) != 64 {
		return ""
	}
	// Validate all characters are lowercase hex — reject any non-hex payload.
	for _, c := range p.BodyHash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ""
		}
	}
	return p.BodyHash
}

func optionalIdempotencyKey(r *http.Request) *string {
	if key := r.Header.Get("Idempotency-Key"); key != "" {
		return &key
	}
	return nil
}

func reserveIdempotentCall(
	w http.ResponseWriter,
	r *http.Request,
	calls store.WebhookCallStore,
	call *store.WebhookCallData,
) (reserved bool, handled bool) {
	if call.IdempotencyKey == nil {
		return false, false
	}
	if err := calls.Create(r.Context(), call); err != nil {
		if errors.Is(err, store.ErrIdempotencyConflict) {
			if replayStoredIdempotencyFromPayload(w, r, calls, call.WebhookID, *call.IdempotencyKey, call.RequestPayload) {
				return false, true
			}
		}
		slog.Error("webhook.idempotency_reserve_failed", "error", err, "call_id", call.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": i18n.T(store.LocaleFromContext(r.Context()), i18n.MsgInternalError, "failed to reserve idempotency key"),
		})
		return false, true
	}
	return true, false
}

func persistWebhookCall(
	ctx context.Context,
	calls store.WebhookCallStore,
	call *store.WebhookCallData,
	reserved bool,
	logName string,
) {
	ctx = context.WithoutCancel(ctx)
	var err error
	if reserved {
		updates := map[string]any{
			"status":       call.Status,
			"attempts":     call.Attempts,
			"response":     call.Response,
			"last_error":   call.LastError,
			"completed_at": call.CompletedAt,
		}
		err = calls.UpdateStatus(ctx, call.ID, updates)
	} else {
		err = calls.Create(ctx, call)
	}
	if err != nil {
		slog.Warn(logName, "error", err, "call_id", call.ID)
	}
}

func replayStoredIdempotencyFromPayload(
	w http.ResponseWriter,
	r *http.Request,
	calls store.WebhookCallStore,
	webhookID uuid.UUID,
	key string,
	requestPayload []byte,
) bool {
	existing, err := calls.GetByIdempotency(r.Context(), webhookID, key)
	if err != nil {
		return false
	}
	locale := store.LocaleFromContext(r.Context())
	if extractBodyHash(existing.RequestPayload) != extractBodyHash(requestPayload) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": i18n.T(locale, i18n.MsgWebhookIdempotencyConflict),
		})
		return true
	}
	expireStaleSyncReservation(r.Context(), calls, existing, time.Now())
	if len(existing.Response) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Idempotency-Replayed", "true")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(existing.Response)
		return true
	}
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  existing.Status,
		"call_id": existing.ID.String(),
	})
	return true
}

func expireStaleSyncReservation(
	ctx context.Context,
	calls store.WebhookCallStore,
	existing *store.WebhookCallData,
	now time.Time,
) bool {
	if !isStaleSyncReservation(existing, now) {
		return false
	}

	reason := "sync idempotency reservation expired"
	resp, err := json.Marshal(map[string]string{
		"call_id": existing.ID.String(),
		"status":  "failed",
		"error":   reason,
	})
	if err != nil {
		slog.Warn("webhook.idempotency_expire_response_failed", "error", err, "call_id", existing.ID)
		return false
	}

	completedAt := now
	attempts := existing.Attempts
	if attempts == 0 {
		attempts = 1
	}
	updates := map[string]any{
		"status":       "failed",
		"attempts":     attempts,
		"response":     resp,
		"last_error":   reason,
		"completed_at": completedAt,
	}
	if err := calls.UpdateStatus(context.WithoutCancel(ctx), existing.ID, updates); err != nil {
		slog.Warn("webhook.idempotency_expire_failed", "error", err, "call_id", existing.ID)
		return false
	}

	existing.Status = "failed"
	existing.Attempts = attempts
	existing.Response = resp
	existing.LastError = &reason
	existing.CompletedAt = &completedAt
	return true
}

func isStaleSyncReservation(existing *store.WebhookCallData, now time.Time) bool {
	if existing == nil || existing.Mode != "sync" || existing.Status != "running" {
		return false
	}
	startedAt := existing.CreatedAt
	if existing.StartedAt != nil {
		startedAt = *existing.StartedAt
	}
	if startedAt.IsZero() {
		return false
	}
	return now.Sub(startedAt) > webhookSyncReservationTTL
}
