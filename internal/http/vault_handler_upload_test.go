package http

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildUploadRequest constructs a multipart POST request with optional form fields
// and one minimal file part. It does NOT attempt to reach storage — the handler's
// boundary UUID validation runs before any store or workspace access.
func buildUploadRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			t.Fatalf("writer.WriteField(%q): %v", k, err)
		}
	}
	// Minimal file part so ParseMultipartForm sees at least one file.
	part, err := writer.CreateFormFile("files", "note.md")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write([]byte("hello")); err != nil {
		t.Fatalf("part.Write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/vault/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

// assertBadRequest decodes a JSON error response and checks the HTTP status
// is 400 and the error message contains the expected substring.
func assertBadRequest(t *testing.T, rr *httptest.ResponseRecorder, wantFragment string) {
	t.Helper()
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (body: %s)", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v (body: %s)", err, rr.Body.String())
	}
	msg, ok := payload["error"]
	if !ok {
		t.Fatalf("response missing `error` field: %s", rr.Body.String())
	}
	if !strings.Contains(msg, wantFragment) {
		t.Errorf("error = %q, want fragment %q", msg, wantFragment)
	}
}

// TestHandleUpload_InvalidAgentIDReturns400 asserts bad form `agent_id` is
// rejected at the HTTP boundary before any store call. Closes the
// owner / lite edition gap where validateTeamMembership would skip the UUID
// check.
func TestHandleUpload_InvalidAgentIDReturns400(t *testing.T) {
	h := &VaultHandler{} // no store wired — boundary check runs first
	req := buildUploadRequest(t, map[string]string{
		"agent_id": "goctech-leader", // agent_key, not UUID
	})
	rr := httptest.NewRecorder()

	h.handleUpload(rr, req)

	assertBadRequest(t, rr, "invalid agent_id")
}

// TestHandleUpload_InvalidTeamIDReturns400 asserts bad form `team_id` is
// rejected at the HTTP boundary. validateTeamMembership short-circuits on
// owner role and on nil teamAccess (lite edition), never parsing the UUID —
// the boundary check closes that hole.
func TestHandleUpload_InvalidTeamIDReturns400(t *testing.T) {
	h := &VaultHandler{} // lite edition: teamAccess is nil
	req := buildUploadRequest(t, map[string]string{
		"team_id": "my-team-key", // team_key, not UUID
	})
	rr := httptest.NewRecorder()

	h.handleUpload(rr, req)

	assertBadRequest(t, rr, "invalid team_id")
}

// TestHandleUpload_InvalidAgentID_OwnerContext asserts the boundary check
// fires regardless of role. The UUID hole existed for every caller — the
// boundary check ignores role entirely.
func TestHandleUpload_InvalidAgentID_OwnerContext(t *testing.T) {
	h := &VaultHandler{}
	req := buildUploadRequest(t, map[string]string{
		"agent_id": "definitely-not-a-uuid",
	})
	// Simulate owner role context — the boundary check ignores role entirely.
	ctx := context.WithValue(req.Context(), ownerContextKey{}, true)
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.handleUpload(rr, req)

	assertBadRequest(t, rr, "invalid agent_id")
}

// TestHandleUpload_EmptyAgentIDAndTeamID_NoBoundaryError asserts the boundary
// check skips empty strings (shared-scope uploads). It should proceed to the
// next validation layer (workspace resolution) — which will fail with a
// different error since our test VaultHandler has no workspace. The important
// thing is that the boundary check itself does NOT return 400 with "invalid
// agent_id" or "invalid team_id" for empty values.
func TestHandleUpload_EmptyAgentIDAndTeamID_BoundaryCheckSkipped(t *testing.T) {
	h := &VaultHandler{}
	req := buildUploadRequest(t, map[string]string{})
	rr := httptest.NewRecorder()

	h.handleUpload(rr, req)

	// Expect a failure AFTER boundary validation (e.g. workspace missing),
	// not the boundary UUID validation errors.
	if rr.Code == http.StatusBadRequest {
		body := rr.Body.String()
		if strings.Contains(body, "invalid agent_id") || strings.Contains(body, "invalid team_id") {
			t.Errorf("boundary check fired on empty input (should skip): body=%s", body)
		}
	}
}

// ownerContextKey is a private test helper for simulating owner role in
// context. The real context key type lives in internal/store; we only need
// to ensure the boundary check fires independent of role-related context.
type ownerContextKey struct{}
