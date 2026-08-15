package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpbridge "github.com/nextlevelbuilder/goclaw/internal/mcp"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type recordingMCPServerStore struct {
	*mockMCPServerForOAuth
	created []*store.MCPServerData
}

func newRecordingMCPServerStore() *recordingMCPServerStore {
	return &recordingMCPServerStore{mockMCPServerForOAuth: newMockMCPServerForOAuth()}
}

func (s *recordingMCPServerStore) CreateServer(_ context.Context, server *store.MCPServerData) error {
	s.created = append(s.created, server)
	return nil
}

type mcpTestResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error"`
	ToolCount int    `json:"tool_count"`
}

func runMCPTestConnection(t *testing.T, h *MCPHandler, rawURL string) (int, mcpTestResponse) {
	t.Helper()

	body := `{"transport":"streamable-http","url":` + mustJSON(t, rawURL) + `}`
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/servers/test", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleTestConnection(rec, req)

	var response mcpTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode test response: %v; body: %s", err, rec.Body.String())
	}
	return rec.Code, response
}

func runMCPCreate(t *testing.T, h *MCPHandler, rawURL string) (int, string) {
	t.Helper()

	body := `{"name":"ssrf-parity","transport":"streamable-http","url":` + mustJSON(t, rawURL) + `}`
	req := httptest.NewRequest(http.MethodPost, "/v1/mcp/servers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleCreateServer(rec, req)

	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode create response: %v; body: %s", err, rec.Body.String())
	}
	return rec.Code, response.Error
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func newMCPSSRFParityHandler(store *recordingMCPServerStore, discoveryCalls *int) *MCPHandler {
	h := NewMCPHandler(store, nil, nil)
	h.discoverTools = func(context.Context, string, string, []string, map[string]string, string, map[string]string) ([]mcpbridge.ToolInfo, error) {
		*discoveryCalls++
		return []mcpbridge.ToolInfo{{Name: "one"}, {Name: "two"}}, nil
	}
	return h
}

func resetMCPSSRFPolicy(t *testing.T) {
	t.Helper()
	security.SetAllowLoopbackForTest(false)
	mcpbridge.SetAllowedHosts(nil)
	t.Cleanup(func() {
		security.SetAllowLoopbackForTest(false)
		mcpbridge.SetAllowedHosts(nil)
	})
}

func TestMCPTestAndSaveRejectPrivateURLsConsistently(t *testing.T) {
	resetMCPSSRFPolicy(t)

	for _, rawURL := range []string{
		"http://localhost:8765/mcp",
		"http://127.0.0.1:8765/mcp",
		"http://10.18.231.2:8765/mcp",
		"http://169.254.169.254/latest/meta-data",
	} {
		t.Run(rawURL, func(t *testing.T) {
			store := newRecordingMCPServerStore()
			discoveryCalls := 0
			h := newMCPSSRFParityHandler(store, &discoveryCalls)

			testStatus, testResponse := runMCPTestConnection(t, h, rawURL)
			createStatus, createError := runMCPCreate(t, h, rawURL)

			if testStatus != http.StatusOK || testResponse.Success {
				t.Fatalf("test status/success = %d/%v, want 200/false", testStatus, testResponse.Success)
			}
			if createStatus != http.StatusBadRequest {
				t.Fatalf("create status = %d, want 400", createStatus)
			}
			if testResponse.Error == "" || testResponse.Error != createError {
				t.Fatalf("test/save errors differ: test=%q save=%q", testResponse.Error, createError)
			}
			if discoveryCalls != 0 {
				t.Fatalf("blocked URL reached discovery %d times", discoveryCalls)
			}
			if len(store.created) != 0 {
				t.Fatalf("blocked URL created %d servers", len(store.created))
			}
		})
	}
}

func TestMCPTestAndSaveAllowExplicitPrivateHost(t *testing.T) {
	resetMCPSSRFPolicy(t)
	mcpbridge.SetAllowedHosts([]string{"127.0.0.1"})

	store := newRecordingMCPServerStore()
	discoveryCalls := 0
	h := newMCPSSRFParityHandler(store, &discoveryCalls)
	rawURL := "http://127.0.0.1:8765/mcp"

	testStatus, testResponse := runMCPTestConnection(t, h, rawURL)
	createStatus, createError := runMCPCreate(t, h, rawURL)

	if testStatus != http.StatusOK || !testResponse.Success || testResponse.ToolCount != 2 {
		t.Fatalf("test response = status %d, %+v; want success with 2 tools", testStatus, testResponse)
	}
	if createStatus != http.StatusCreated || createError != "" {
		t.Fatalf("create response = status %d, error %q; want 201", createStatus, createError)
	}
	if discoveryCalls != 1 || len(store.created) != 1 {
		t.Fatalf("discovery/creates = %d/%d, want 1/1", discoveryCalls, len(store.created))
	}
}

func TestMCPAllowlistDoesNotPermitMetadata(t *testing.T) {
	resetMCPSSRFPolicy(t)
	mcpbridge.SetAllowedHosts([]string{"169.254.169.254"})

	store := newRecordingMCPServerStore()
	discoveryCalls := 0
	h := newMCPSSRFParityHandler(store, &discoveryCalls)
	rawURL := "http://169.254.169.254/latest/meta-data"

	_, testResponse := runMCPTestConnection(t, h, rawURL)
	createStatus, createError := runMCPCreate(t, h, rawURL)

	if testResponse.Success || testResponse.Error == "" || testResponse.Error != createError {
		t.Fatalf("metadata test/save errors differ: test=%q save=%q", testResponse.Error, createError)
	}
	if createStatus != http.StatusBadRequest || discoveryCalls != 0 || len(store.created) != 0 {
		t.Fatalf("metadata status/discovery/creates = %d/%d/%d, want 400/0/0", createStatus, discoveryCalls, len(store.created))
	}
}

func TestMCPTestAndSaveAcceptPublicURL(t *testing.T) {
	resetMCPSSRFPolicy(t)

	store := newRecordingMCPServerStore()
	discoveryCalls := 0
	h := newMCPSSRFParityHandler(store, &discoveryCalls)
	rawURL := "https://8.8.8.8/mcp"

	testStatus, testResponse := runMCPTestConnection(t, h, rawURL)
	createStatus, createError := runMCPCreate(t, h, rawURL)

	if testStatus != http.StatusOK || !testResponse.Success {
		t.Fatalf("test response = status %d, %+v; want success", testStatus, testResponse)
	}
	if createStatus != http.StatusCreated || createError != "" {
		t.Fatalf("create response = status %d, error %q; want 201", createStatus, createError)
	}
	if discoveryCalls != 1 || len(store.created) != 1 {
		t.Fatalf("discovery/creates = %d/%d, want 1/1", discoveryCalls, len(store.created))
	}
}
