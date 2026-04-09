package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticGitHubCopilotTokenSource struct {
	token   string
	apiBase string
}

func (s *staticGitHubCopilotTokenSource) Token() (string, error) { return s.token, nil }
func (s *staticGitHubCopilotTokenSource) APIBase() string        { return s.apiBase }

func TestGitHubCopilotProviderAddsDynamicHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer copilot-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Copilot-Integration-Id"); got != "vscode-chat" {
			t.Fatalf("Copilot-Integration-Id = %q", got)
		}
		if got := r.Header.Get("X-Initiator"); got != "user" {
			t.Fatalf("X-Initiator = %q", got)
		}
		if got := r.Header.Get("Openai-Intent"); got != "conversation-edits" {
			t.Fatalf("Openai-Intent = %q", got)
		}
		if got := r.URL.Path; got != "/responses" {
			t.Fatalf("path = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	p := NewGitHubCopilotProvider("github-copilot", &staticGitHubCopilotTokenSource{token: "copilot-token", apiBase: server.URL}, server.URL, "gpt-5.4")
	p.retryConfig.Attempts = 1
	resp, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("Content = %q", resp.Content)
	}
}

func TestBuildGitHubCopilotDynamicHeadersAgent(t *testing.T) {
	body := map[string]any{
		"input": []any{
			map[string]any{"role": "user", "content": "hello"},
			map[string]any{"role": "assistant", "content": []map[string]any{{"type": "output_text", "text": "done"}}},
		},
	}
	headers := buildGitHubCopilotDynamicHeaders(body)
	if headers["X-Initiator"] != "agent" {
		t.Fatalf("X-Initiator = %q", headers["X-Initiator"])
	}
}

func TestGitHubCopilotProviderDedupesRepeatedMessageOutputs(t *testing.T) {
	messageText := "Yo. I'm here, bro 🚀"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		for i := 0; i < 4; i++ {
			fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
				Type:       "response.output_item.done",
				ItemID:     fmt.Sprintf("msg_%d", i),
				OutputIndex: i,
				Item: &codexItem{
					ID:    fmt.Sprintf("msg_%d", i),
					Type:  "message",
					Role:  "assistant",
					Phase: "final_answer",
					Content: []codexContent{{Type: "output_text", Text: messageText}},
				},
			}))
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID:     "resp-repeat",
				Status: "completed",
				Output: []codexItem{
					{ID: "msg_0", Type: "message", Role: "assistant", Phase: "final_answer", Content: []codexContent{{Type: "output_text", Text: messageText}}},
					{ID: "msg_1", Type: "message", Role: "assistant", Phase: "final_answer", Content: []codexContent{{Type: "output_text", Text: messageText}}},
					{ID: "msg_2", Type: "message", Role: "assistant", Phase: "final_answer", Content: []codexContent{{Type: "output_text", Text: messageText}}},
					{ID: "msg_3", Type: "message", Role: "assistant", Phase: "final_answer", Content: []codexContent{{Type: "output_text", Text: messageText}}},
				},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewGitHubCopilotProvider("github-copilot", &staticGitHubCopilotTokenSource{token: "copilot-token", apiBase: server.URL}, server.URL, "gpt-5.4")
	p.retryConfig.Attempts = 1

	var chunks []string
	resp, err := p.ChatStream(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}}, func(chunk StreamChunk) {
		if chunk.Content != "" {
			chunks = append(chunks, chunk.Content)
		}
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if resp.Content != messageText {
		t.Fatalf("Content = %q, want %q", resp.Content, messageText)
	}
	if len(chunks) != 1 || chunks[0] != messageText {
		t.Fatalf("chunks = %v, want single deduped chunk", chunks)
	}
}

func TestGitHubCopilotProviderDedupesRepeatedCompletedParts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", mustJSON(codexSSEEvent{
			Type: "response.completed",
			Response: &codexAPIResponse{
				ID:     "resp-parts",
				Status: "completed",
				Output: []codexItem{{
					ID:    "msg_final",
					Type:  "message",
					Role:  "assistant",
					Phase: "final_answer",
					Content: []codexContent{
						{Type: "output_text", Text: "I meant to send one line. "},
						{Type: "output_text", Text: "You got the same line four times. My bad."},
						{Type: "output_text", Text: "You got the same line four times. My bad."},
						{Type: "output_text", Text: "You got the same line four times. My bad."},
					},
				}},
			},
		}))
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	p := NewGitHubCopilotProvider("github-copilot", &staticGitHubCopilotTokenSource{token: "copilot-token", apiBase: server.URL}, server.URL, "gpt-5.4")
	p.retryConfig.Attempts = 1

	resp, err := p.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	want := "I meant to send one line. You got the same line four times. My bad."
	if resp.Content != want {
		t.Fatalf("Content = %q, want %q", resp.Content, want)
	}
}