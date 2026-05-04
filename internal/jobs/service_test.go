package jobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestService_Spawn_HappyPath(t *testing.T) {
	var captured Request
	var capturedSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		capturedSig = r.Header.Get("X-Hub-Signature-256")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		_ = json.NewEncoder(w).Encode(Response{
			JobID:      "abc",
			K8sJobName: "voice-summary-abc",
			K8sJobUID:  "uid-1",
		})
	}))
	defer srv.Close()

	s := NewService(srv.URL, []byte("secret"))
	resp, err := s.Spawn(context.Background(), Request{
		JobID:         "abc",
		Kind:          "voice-summary",
		Command:       "/app/agent/bin/run-voice-summary",
		WorktreePath:  "/data/wt/x",
		Sinks:         []Sink{{Type: "discord", Channel: "c", ThreadID: "t"}},
		Model:         "deepseek/deepseek-v4-pro",
		Provider:      "openrouter",
		ActivateSkill: "voice-session-summarization",
	})
	if err != nil {
		t.Fatalf("spawn err: %v", err)
	}
	if resp.JobID != "abc" || resp.K8sJobName != "voice-summary-abc" {
		t.Errorf("unexpected response: %+v", resp)
	}
	if captured.Model != "deepseek/deepseek-v4-pro" || captured.ActivateSkill != "voice-session-summarization" {
		t.Errorf("override fields not forwarded: %+v", captured)
	}
	if !strings.HasPrefix(capturedSig, "sha256=") {
		t.Errorf("missing/invalid HMAC signature: %q", capturedSig)
	}
}

func TestService_Spawn_RequiresHMACSecret(t *testing.T) {
	s := NewService("http://example.invalid", nil)
	_, err := s.Spawn(context.Background(), Request{Kind: "x"})
	if err == nil || !strings.Contains(err.Error(), "HMAC secret") {
		t.Errorf("expected HMAC secret error, got %v", err)
	}
}

func TestService_Spawn_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewService(srv.URL, []byte("secret"))
	_, err := s.Spawn(context.Background(), Request{Kind: "x"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500-status error, got %v", err)
	}
}
