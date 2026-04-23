package pancake

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- ReplyComment ---

func TestReplyComment_MatchesOfficialContract(t *testing.T) {
	transport := &captureTransport{}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	if err := client.ReplyComment(context.Background(), "conv-123", "msg-456", "thank you"); err != nil {
		t.Fatalf("ReplyComment returned error: %v", err)
	}

	if transport.req == nil {
		t.Fatal("expected outbound request to be captured")
	}
	wantPath := "/api/public_api/v1/pages/page-123/conversations/conv-123/messages"
	if got := transport.req.URL.Path; got != wantPath {
		t.Fatalf("request path = %q, want %q", got, wantPath)
	}
	if got := transport.req.URL.Query().Get("page_access_token"); got != "page-token" {
		t.Fatalf("page_access_token = %q, want %q", got, "page-token")
	}

	var payload map[string]any
	if err := json.Unmarshal(transport.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["action"], "reply_comment"; got != want {
		t.Fatalf("payload.action = %#v, want %#v", got, want)
	}
	if got, want := payload["message"], "thank you"; got != want {
		t.Fatalf("payload.message = %#v, want %#v", got, want)
	}
	if got, want := payload["message_id"], "msg-456"; got != want {
		t.Fatalf("payload.message_id = %#v, want %#v", got, want)
	}
}

func TestReplyComment_ReturnsError(t *testing.T) {
	transport := &captureTransport{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":400,"message":"bad request"}`)),
		},
	}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	if err := client.ReplyComment(context.Background(), "conv-123", "msg-456", "thank you"); err == nil {
		t.Fatal("expected ReplyComment to return error on HTTP 400")
	}
}

// --- PrivateReply ---

func TestPrivateReply_MatchesOfficialContract(t *testing.T) {
	transport := &captureTransport{}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	if err := client.PrivateReply(context.Background(), "conv-123", "thanks for commenting!"); err != nil {
		t.Fatalf("PrivateReply returned error: %v", err)
	}

	if transport.req == nil {
		t.Fatal("expected outbound request to be captured")
	}
	wantPath := "/api/public_api/v1/pages/page-123/conversations/conv-123/messages"
	if got := transport.req.URL.Path; got != wantPath {
		t.Fatalf("request path = %q, want %q", got, wantPath)
	}

	var payload map[string]any
	if err := json.Unmarshal(transport.body, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got, want := payload["action"], "private_reply"; got != want {
		t.Fatalf("payload.action = %#v, want %#v", got, want)
	}
	if got, want := payload["message"], "thanks for commenting!"; got != want {
		t.Fatalf("payload.message = %#v, want %#v", got, want)
	}
}

func TestPrivateReply_ReturnsError(t *testing.T) {
	transport := &captureTransport{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":400,"message":"conversation not found"}`)),
		},
	}
	client := NewAPIClient("user-token", "page-token", "page-123")
	client.httpClient = &http.Client{Transport: transport}

	if err := client.PrivateReply(context.Background(), "conv-123", "hi"); err == nil {
		t.Fatal("expected PrivateReply to return error on HTTP 400")
	}
}

// --- GetPosts ---

func TestGetPosts_ParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"data":[{"id":"post-1","message":"Hello world","created_at":"2024-01-01"}]}`))
	}))
	defer srv.Close()

	client := NewAPIClient("user-token", "page-token", "page-123")
	client.pageV2BaseURL = srv.URL
	client.httpClient = srv.Client()

	posts, err := client.GetPosts(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetPosts returned error: %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].ID != "post-1" {
		t.Errorf("posts[0].ID = %q, want %q", posts[0].ID, "post-1")
	}
	if posts[0].Message != "Hello world" {
		t.Errorf("posts[0].Message = %q, want %q", posts[0].Message, "Hello world")
	}
}

func TestGetPosts_EmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":true,"data":[]}`))
	}))
	defer srv.Close()

	client := NewAPIClient("user-token", "page-token", "page-123")
	client.pageV2BaseURL = srv.URL
	client.httpClient = srv.Client()

	posts, err := client.GetPosts(context.Background(), 10)
	if err != nil {
		t.Fatalf("GetPosts returned error: %v", err)
	}
	if len(posts) != 0 {
		t.Fatalf("expected 0 posts, got %d", len(posts))
	}
}

func TestGetPosts_ErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`internal error`))
	}))
	defer srv.Close()

	client := NewAPIClient("user-token", "page-token", "page-123")
	client.pageV2BaseURL = srv.URL
	client.httpClient = srv.Client()

	_, err := client.GetPosts(context.Background(), 10)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

// --- Config Parsing ---

func TestConfigParsing_CommentReplyOptions(t *testing.T) {
	raw := `{
		"page_id": "123",
		"features": {"comment_reply": true, "first_inbox": true},
		"comment_reply_options": {"filter": "keyword", "keywords": ["price", "buy"]},
		"first_inbox_message": "Thanks!",
		"post_context_cache_ttl": "30m"
	}`

	var cfg pancakeInstanceConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !cfg.Features.FirstInbox {
		t.Error("Features.FirstInbox should be true")
	}
	if cfg.CommentReplyOptions.Filter != "keyword" {
		t.Errorf("Filter = %q, want %q", cfg.CommentReplyOptions.Filter, "keyword")
	}
	if len(cfg.CommentReplyOptions.Keywords) != 2 ||
		cfg.CommentReplyOptions.Keywords[0] != "price" ||
		cfg.CommentReplyOptions.Keywords[1] != "buy" {
		t.Errorf("Keywords = %v, want [price buy]", cfg.CommentReplyOptions.Keywords)
	}
	if cfg.FirstInboxMessage != "Thanks!" {
		t.Errorf("FirstInboxMessage = %q, want %q", cfg.FirstInboxMessage, "Thanks!")
	}
	if cfg.PostContextCacheTTL != "30m" {
		t.Errorf("PostContextCacheTTL = %q, want %q", cfg.PostContextCacheTTL, "30m")
	}
}

func TestPancakeConfig_AutoReactOptionsRoundtrip(t *testing.T) {
	src := `{
	  "page_id": "123",
	  "platform": "facebook",
	  "features": {"auto_react": true},
	  "auto_react_options": {
	    "allow_post_ids": ["post-1", "post-2"],
	    "deny_post_ids": ["post-3"],
	    "allow_user_ids": ["user-a"],
	    "deny_user_ids": ["user-b", "user-c"]
	  }
	}`
	var cfg pancakeInstanceConfig
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.AutoReactOptions == nil {
		t.Fatal("AutoReactOptions should be non-nil when JSON has auto_react_options")
	}
	if len(cfg.AutoReactOptions.AllowPostIDs) != 2 {
		t.Errorf("AllowPostIDs len = %d, want 2", len(cfg.AutoReactOptions.AllowPostIDs))
	}
	if cfg.AutoReactOptions.DenyPostIDs[0] != "post-3" {
		t.Errorf("DenyPostIDs[0] = %q, want post-3", cfg.AutoReactOptions.DenyPostIDs[0])
	}
	if len(cfg.AutoReactOptions.DenyUserIDs) != 2 {
		t.Errorf("DenyUserIDs len = %d, want 2", len(cfg.AutoReactOptions.DenyUserIDs))
	}
	if cfg.AutoReactOptions.AllowUserIDs[0] != "user-a" {
		t.Errorf("AllowUserIDs[0] = %q, want user-a", cfg.AutoReactOptions.AllowUserIDs[0])
	}
}

func TestPancakeConfig_AutoReactOptionsOmitempty(t *testing.T) {
	var cfg pancakeInstanceConfig
	cfg.PageID = "123"
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "auto_react_options") {
		t.Errorf("empty AutoReactOptions should be omitted, got: %s", b)
	}
}

func TestPancakeConfig_AutoReactOptionsDefaultsEmpty(t *testing.T) {
	src := `{"page_id": "123"}`
	var cfg pancakeInstanceConfig
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.AutoReactOptions != nil {
		t.Errorf("AutoReactOptions should be nil when absent, got %+v", cfg.AutoReactOptions)
	}
}

func TestConfigParsing_Defaults(t *testing.T) {
	raw := `{"page_id":"123","features":{"inbox_reply":true}}`

	var cfg pancakeInstanceConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if cfg.Features.FirstInbox {
		t.Error("Features.FirstInbox should default to false")
	}
	if cfg.CommentReplyOptions.Filter != "" {
		t.Errorf("CommentReplyOptions.Filter should default to empty, got %q", cfg.CommentReplyOptions.Filter)
	}
	if cfg.FirstInboxMessage != "" {
		t.Errorf("FirstInboxMessage should default to empty, got %q", cfg.FirstInboxMessage)
	}
}

// --- ReactComment (Pancake user API) ---

func TestReactComment_Success(t *testing.T) {
	done := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/likes") {
			if got := r.URL.Query().Get("access_token"); got != "user-key" {
				t.Errorf("access_token query = %q, want user-key", got)
			}
			if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "multipart/form-data") {
				t.Errorf("Content-Type = %q, want multipart/form-data", ct)
			}
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("ParseMultipartForm: %v", err)
			}
			if got := r.FormValue("action"); got != "like_toggle" {
				t.Errorf("action = %q, want like_toggle", got)
			}
			if got := r.FormValue("user_likes"); got != "false" {
				t.Errorf("user_likes = %q, want false", got)
			}
			select {
			case done <- r.URL.Path:
			default:
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewAPIClient("user-key", "page-token", "page123")
	client.userBaseURL = srv.URL
	client.httpClient = srv.Client()

	err := client.ReactComment(context.Background(), "conv-1_msg-1", "conv-1_msg-1")
	if err != nil {
		t.Fatalf("ReactComment unexpected error: %v", err)
	}
	select {
	case path := <-done:
		want := "/pages/page123/conversations/conv-1_msg-1/messages/conv-1_msg-1/likes"
		if path != want {
			t.Errorf("path = %q, want %q", path, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST /likes was not called within 2s")
	}
}

func TestReactComment_ErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"success":false,"message":"invalid access_token"}`))
	}))
	defer srv.Close()

	client := NewAPIClient("bad-key", "page-token", "page123")
	client.userBaseURL = srv.URL
	client.httpClient = srv.Client()

	err := client.ReactComment(context.Background(), "conv-1", "msg-1")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "invalid access_token") {
		t.Errorf("expected HTTP status + body in error, got: %v", err)
	}
}

func TestReactComment_RejectsInvalidIDs(t *testing.T) {
	client := NewAPIClient("key", "token", "page123")
	cases := []struct{ conv, msg string }{
		{"", "msg-1"},
		{"conv-1", ""},
		{"conv/1", "msg-1"},
		{"conv-1", "msg?evil=1"},
		{"conv#frag", "msg-1"},
	}
	for _, c := range cases {
		if err := client.ReactComment(context.Background(), c.conv, c.msg); err == nil {
			t.Errorf("expected error for conv=%q msg=%q, got nil", c.conv, c.msg)
		}
	}
}
