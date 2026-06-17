package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type stubRunTimelineStore struct {
	opts  []store.RunTimelineListOpts
	items []store.RunTimelineItem
}

func (s *stubRunTimelineStore) AppendRunTimelineItem(context.Context, *store.RunTimelineItem) error {
	return nil
}

func (s *stubRunTimelineStore) ListRunTimelineItems(_ context.Context, opts store.RunTimelineListOpts) ([]store.RunTimelineItem, error) {
	s.opts = append(s.opts, opts)
	return s.items, nil
}

func TestRunTimelineHTTPScopesViewerByUser(t *testing.T) {
	token := setupTraceReadToken(t, "caller")
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{
			{RunID: "run-1", UserID: "caller", Seq: 1, Preview: "visible"},
			{RunID: "run-1", UserID: "other", Seq: 2, Preview: "hidden"},
		},
	}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/timeline?session_key=session-1&limit=20", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []store.RunTimelineItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].UserID != "caller" {
		t.Fatalf("items = %+v", body.Items)
	}
	if len(timeline.opts) != 1 {
		t.Fatalf("List calls = %d, want 1", len(timeline.opts))
	}
	if timeline.opts[0].RunID != "run-1" || timeline.opts[0].SessionKey != "session-1" {
		t.Fatalf("opts = %+v", timeline.opts[0])
	}
}

func TestRunTimelineHTTPAdminSeesTenantItems(t *testing.T) {
	token := "timeline-admin-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {ID: uuid.New(), Scopes: []string{"operator.admin"}, OwnerID: "admin"},
	})
	timeline := &stubRunTimelineStore{
		items: []store.RunTimelineItem{{RunID: "run-1", UserID: "other", Seq: 1}},
	}
	mux := http.NewServeMux()
	NewTracesHandler(&mockTracingStore{}, timeline).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/runs/run-1/timeline", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}
