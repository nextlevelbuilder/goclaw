package oa

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// refreshMargin: refresh when the access token expires within this window.
const refreshMargin = 5 * time.Minute

// refreshHTTPTimeout caps the refresh HTTP roundtrip independent of the
// caller ctx so a misconfigured caller can't park the singleflighted
// refresh indefinitely. Shorter than the 15s defaultClientTimeout.
const refreshHTTPTimeout = 12 * time.Second

// tokenSource lazily refreshes the access token. singleflight serializes
// concurrent refresh attempts (Zalo refresh tokens are single-use) without
// holding a lock across the HTTP call, so concurrent readers see the new
// token as soon as it's stored. Reads of creds go through the atomic
// pointer; callers must treat the returned struct as read-only.
type tokenSource struct {
	client     *Client
	creds      atomic.Pointer[ChannelCreds]
	store      store.ChannelInstanceStore
	instanceID uuid.UUID

	refreshSF singleflight.Group
}

// Snapshot returns a read-only pointer to the current creds.
func (ts *tokenSource) Snapshot() *ChannelCreds {
	if p := ts.creds.Load(); p != nil {
		return p
	}
	return &ChannelCreds{}
}

// ForceRefresh marks the cached token stale so the next Access() refreshes.
func (ts *tokenSource) ForceRefresh() {
	for {
		cur := ts.creds.Load()
		if cur == nil {
			return
		}
		next := *cur
		next.ExpiresAt = time.Time{}
		next.AccessToken = ""
		if ts.creds.CompareAndSwap(cur, &next) {
			return
		}
	}
}

// Access returns a valid access token, refreshing if within refreshMargin.
// Uses singleflight so concurrent callers share one HTTP refresh.
func (ts *tokenSource) Access(ctx context.Context) (string, error) {
	if cur := ts.Snapshot(); cur.AccessToken != "" && time.Until(cur.ExpiresAt) > refreshMargin {
		return cur.AccessToken, nil
	}

	_, err, _ := ts.refreshSF.Do("refresh", func() (any, error) {
		// Re-check inside singleflight: a sibling caller may have just
		// finished a refresh while we waited.
		if cur := ts.Snapshot(); cur.AccessToken != "" && time.Until(cur.ExpiresAt) > refreshMargin {
			return nil, nil
		}
		return nil, ts.doRefresh(ctx)
	})
	if err != nil {
		return "", err
	}
	return ts.Snapshot().AccessToken, nil
}

// doRefresh performs the HTTP refresh + persistence. Called under
// singleflight so at most one refresh is in flight per tokenSource.
// Persist-before-commit: if Persist fails after a successful refresh we
// keep the new tokens in memory (the old refresh token is already burned)
// but DB has stale tokens — next process restart will fail to invalid_grant
// and surface re-auth, which is the safe failure mode.
func (ts *tokenSource) doRefresh(ctx context.Context) error {
	cur := ts.Snapshot()
	if cur.RefreshToken == "" {
		// Pre-authorization: distinct from a burned refresh token; do NOT
		// escalate to Failed. Log so ops can distinguish "never consented"
		// (OAID empty) from "consent dropped mid-flow" (OAID set).
		slog.Info("zalo_oa.pre_authorization",
			"instance_id", ts.instanceID,
			"has_oa_id", cur.OAID != "")
		return ErrNotAuthorized
	}

	refreshCtx, cancel := context.WithTimeout(ctx, refreshHTTPTimeout)
	defer cancel()
	tok, rawErr := ts.client.RefreshToken(refreshCtx, cur.AppID, cur.SecretKey, cur.RefreshToken)
	if rawErr != nil {
		err := classifyRefreshError(rawErr)
		if errors.Is(err, ErrAuthExpired) {
			slog.Warn("zalo_oa.reauth_required", "instance_id", ts.instanceID, "oa_id", cur.OAID)
			return err
		}
		slog.Warn("zalo_oa.refresh_failed", "instance_id", ts.instanceID, "oa_id", cur.OAID, "error", err)
		return err
	}

	snapshot := *cur
	snapshot.WithTokens(tok)
	if err := Persist(ctx, ts.store, ts.instanceID, &snapshot); err != nil {
		slog.Error("zalo_oa.persist_failed", "instance_id", ts.instanceID, "oa_id", cur.OAID, "error", err)
		// Commit in memory: the new pair is the only valid one until restart.
		ts.creds.Store(&snapshot)
		return err
	}
	ts.creds.Store(&snapshot)
	slog.Info("zalo_oa.token_refreshed",
		"instance_id", ts.instanceID,
		"oa_id", snapshot.OAID,
		"new_expires_at", snapshot.ExpiresAt,
		"refresh_expires_at", snapshot.RefreshTokenExpiresAt,
	)
	return nil
}
