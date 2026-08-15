package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeActivityStore struct {
	opts store.ActivityAggregateOpts
}

func (s *fakeActivityStore) Log(context.Context, *store.ActivityLog) error { return nil }
func (s *fakeActivityStore) List(context.Context, store.ActivityListOpts) ([]store.ActivityLog, error) {
	return nil, nil
}
func (s *fakeActivityStore) Count(context.Context, store.ActivityListOpts) (int, error) {
	return 0, nil
}
func (s *fakeActivityStore) Aggregate(_ context.Context, opts store.ActivityAggregateOpts) ([]store.ActivityAggregateBucket, int, error) {
	s.opts = opts
	return []store.ActivityAggregateBucket{{Key: "session.branch", Count: 2, LastSeen: time.Now().UTC()}}, 2, nil
}

func TestActivityAggregateScopesViewerToCaller(t *testing.T) {
	token := "activity-read-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	activity := &fakeActivityStore{}
	mux := http.NewServeMux()
	NewActivityHandler(activity).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/aggregate?group_by=action&actor_id=other&limit=999", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if activity.opts.ActorID != "caller" {
		t.Fatalf("ActorID = %q, want caller", activity.opts.ActorID)
	}
	if activity.opts.Limit != 200 {
		t.Fatalf("Limit = %d, want 200", activity.opts.Limit)
	}
}

func TestActivityAggregateRejectsActorIDGroupForViewer(t *testing.T) {
	token := "activity-viewer-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&fakeActivityStore{}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/aggregate?group_by=actor_id", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestActivityAggregateRejectsUnboundNonAdminKey(t *testing.T) {
	token := "activity-unbound-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&fakeActivityStore{}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/aggregate?group_by=action", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestActivityAggregateRejectsInvalidRange(t *testing.T) {
	token := "activity-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&fakeActivityStore{}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/aggregate?group_by=action&from=2026-05-23T00:00:00Z&to=2026-05-22T00:00:00Z", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestActivityAggregateResponseShape(t *testing.T) {
	token := "activity-admin-shape-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&fakeActivityStore{}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/aggregate?group_by=action", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var body struct {
		Source  string `json:"source"`
		GroupBy string `json:"group_by"`
		Total   int    `json:"total"`
		Buckets []any  `json:"buckets"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Source != "activity" || body.GroupBy != "action" || body.Total != 2 || len(body.Buckets) != 1 {
		t.Fatalf("body = %+v", body)
	}
}
