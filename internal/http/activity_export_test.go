package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// exportFakeStore returns a fixed set of activity rows for List so the export
// endpoints can be asserted against real serialized output.
type exportFakeStore struct {
	rows []store.ActivityLog
}

func (s *exportFakeStore) Log(context.Context, *store.ActivityLog) error { return nil }
func (s *exportFakeStore) List(_ context.Context, opts store.ActivityListOpts) ([]store.ActivityLog, error) {
	if opts.Offset >= len(s.rows) {
		return nil, nil
	}
	end := opts.Offset + opts.Limit
	if end > len(s.rows) {
		end = len(s.rows)
	}
	return s.rows[opts.Offset:end], nil
}
func (s *exportFakeStore) Count(context.Context, store.ActivityListOpts) (int, error) {
	return len(s.rows), nil
}
func (s *exportFakeStore) Prune(context.Context, time.Time) (int64, error) { return 0, nil }
func (s *exportFakeStore) Aggregate(context.Context, store.ActivityAggregateOpts) ([]store.ActivityAggregateBucket, int, error) {
	return nil, 0, nil
}

func exportRows() []store.ActivityLog {
	return []store.ActivityLog{
		{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), ActorType: "user", ActorID: "alice", Action: "auth.login", EntityType: "auth", EntityID: "bearer", IPAddress: "10.0.0.1"},
		{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), ActorType: "user", ActorID: "bob", Action: "agent.create", EntityType: "agent", EntityID: "agent-2", IPAddress: "10.0.0.2"},
	}
}

func TestActivityExport_CSV_AdminGated(t *testing.T) {
	token := "export-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&exportFakeStore{rows: exportRows()}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/export?format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "id,actor_type,actor_id,") {
		t.Errorf("missing CSV header:\n%s", body)
	}
	if !strings.Contains(body, "alice") || !strings.Contains(body, "auth.login") {
		t.Errorf("CSV body missing expected row content:\n%s", body)
	}
	if !strings.Contains(body, "bob") {
		t.Errorf("CSV body missing second row:\n%s", body)
	}
}

func TestActivityExport_JSONL_AdminGated(t *testing.T) {
	token := "export-jsonl-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&exportFakeStore{rows: exportRows()}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/export?format=jsonl", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/x-ndjson") {
		t.Errorf("Content-Type = %q, want application/x-ndjson", ct)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("NDJSON lines = %d, want 2\n%s", len(lines), rec.Body.String())
	}
	if !strings.Contains(lines[0], `"actor_id":"alice"`) {
		t.Errorf("line 1 missing actor alice: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"actor_id":"bob"`) {
		t.Errorf("line 2 missing actor bob: %s", lines[1])
	}
}

func TestActivityExport_RejectsInvalidFormat(t *testing.T) {
	token := "export-invalid-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&exportFakeStore{rows: exportRows()}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/export?format=xml", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestActivityExport_AdminOnly(t *testing.T) {
	token := "export-viewer-key"
	// Viewer role (viewer.read scope) — must be denied.
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"viewer.read"}, OwnerID: "viewer"},
	})
	mux := http.NewServeMux()
	NewActivityHandler(&exportFakeStore{rows: exportRows()}).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/activity/export?format=csv", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}