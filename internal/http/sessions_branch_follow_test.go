package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type branchSessionStore struct {
	recordingSessionStore
	data map[string]*store.SessionData
}

func (s *branchSessionStore) Get(_ context.Context, key string) *store.SessionData {
	return s.data[key]
}

func (s *branchSessionStore) GetHistory(_ context.Context, key string) []providers.Message {
	if sess := s.data[key]; sess != nil {
		return append([]providers.Message(nil), sess.Messages...)
	}
	return nil
}

func (s *branchSessionStore) BranchSession(_ context.Context, sourceKey string, opts store.SessionBranchOpts) (*store.SessionData, int, error) {
	source := s.data[sourceKey]
	if source == nil {
		return nil, 0, store.ErrSessionNotFound
	}
	if _, exists := s.data[opts.NewKey]; exists {
		return nil, 0, store.ErrSessionAlreadyExists
	}
	if opts.UpToIndex < 0 || opts.UpToIndex > len(source.Messages) {
		return nil, 0, store.ErrInvalidSessionBranch
	}
	msgs := append([]providers.Message(nil), source.Messages[:opts.UpToIndex]...)
	branch := &store.SessionData{
		Key:      opts.NewKey,
		Messages: msgs,
		UserID:   source.UserID,
		Channel:  "branch",
		Label:    opts.Label,
		Updated:  time.Now().UTC(),
	}
	s.data[opts.NewKey] = branch
	return branch, len(msgs), nil
}

func TestSessionsBranchCreatesGeneratedBranchAtIndex(t *testing.T) {
	token := "session-read-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	sessions := &branchSessionStore{data: map[string]*store.SessionData{
		"agent:default:ws:direct:abc": {
			Key:     "agent:default:ws:direct:abc",
			UserID:  "caller",
			Updated: time.Now().UTC(),
			Messages: []providers.Message{
				{Role: "user", Content: "one"},
				{Role: "assistant", Content: "two"},
				{Role: "user", Content: "three"},
			},
		},
	}}
	mux := http.NewServeMux()
	NewSessionsHandler(sessions, nil).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/sessions/agent:default:ws:direct:abc/branch", strings.NewReader(`{"up_to_index":2,"label":"branch"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		SessionKey     string `json:"session_key"`
		CopiedMessages int    `json:"copied_messages"`
		TotalMessages  int    `json:"total_messages"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(body.SessionKey, "agent:default:branch:direct:") {
		t.Fatalf("session_key = %q", body.SessionKey)
	}
	if body.CopiedMessages != 2 || body.TotalMessages != 3 {
		t.Fatalf("body = %+v", body)
	}
}

func TestSessionsHistoryFollowCursorReset(t *testing.T) {
	token := "session-follow-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})
	sessions := &branchSessionStore{data: map[string]*store.SessionData{
		"agent:default:ws:direct:abc": {
			Key:     "agent:default:ws:direct:abc",
			UserID:  "caller",
			Updated: time.Now().UTC(),
			Messages: []providers.Message{
				{Role: "user", Content: "one"},
			},
		},
	}}
	mux := http.NewServeMux()
	NewSessionsHandler(sessions, nil).RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/sessions/agent:default:ws:direct:abc/history/follow?cursor=9", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Reset      bool `json:"reset"`
		NextCursor int  `json:"next_cursor"`
		Total      int  `json:"total"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Reset || body.NextCursor != 1 || body.Total != 1 {
		t.Fatalf("body = %+v", body)
	}
}
