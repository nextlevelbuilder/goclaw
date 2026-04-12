package facebook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// swapGraphBase points graphAPIBase at a test server for the duration of t.
func swapGraphBase(t *testing.T, url string) {
	t.Helper()
	orig := graphAPIBase
	graphAPIBase = url
	t.Cleanup(func() { graphAPIBase = orig })
}

// newFakeGraph spins up a test server and returns (client pointed at it, server).
// Backoff base is reduced to 1ms so retry tests don't burn ~6s on real
// exponential waits (1s, 2s, 4s). Production behavior is unchanged.
func newFakeGraph(t *testing.T, handler http.Handler) *GraphClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	swapGraphBase(t, srv.URL)
	savedBackoff := graphBackoffBase
	graphBackoffBase = time.Millisecond
	t.Cleanup(func() { graphBackoffBase = savedBackoff })
	return NewGraphClient("fake-token", "111222333")
}

// TestParseRetryAfterNegativeDefaults extends the existing TestParseRetryAfter
// in facebook_test.go with the negative-value branch (defaults to 5s).
func TestParseRetryAfterNegativeDefaults(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "-5")
	got := parseRetryAfter(&http.Response{Header: h})
	if got != 5*time.Second {
		t.Errorf("got %v, want 5s (negative Retry-After should default)", got)
	}
}

// --- NewGraphClient ---

func TestNewGraphClient_ConstructsWithTokenAndPage(t *testing.T) {
	g := NewGraphClient("tok", "111")
	if g.pageAccessToken != "tok" {
		t.Errorf("pageAccessToken = %q, want tok", g.pageAccessToken)
	}
	if g.pageID != "111" {
		t.Errorf("pageID = %q, want 111", g.pageID)
	}
	if g.httpClient == nil {
		t.Error("httpClient is nil")
	}
	if g.httpClient.Timeout != 15*time.Second {
		t.Errorf("httpClient timeout = %v, want 15s", g.httpClient.Timeout)
	}
}

// --- VerifyToken ---

func TestVerifyToken_Success(t *testing.T) {
	var gotPath, gotAuth string
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"id":"111","name":"TestPage"}`))
	}))
	if err := g.VerifyToken(context.Background()); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if !strings.Contains(gotPath, "/me") {
		t.Errorf("path = %q, want contains /me", gotPath)
	}
	if gotAuth != "Bearer fake-token" {
		t.Errorf("auth = %q, want Bearer fake-token", gotAuth)
	}
}

func TestVerifyToken_HTTPError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":190,"message":"token expired"}}`))
	}))
	err := g.VerifyToken(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsAuthError(err) {
		t.Errorf("expected IsAuthError(err) true, got err=%v", err)
	}
}

func TestVerifyToken_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	if err := g.VerifyToken(context.Background()); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// --- SubscribeApp ---

func TestSubscribeApp_Success(t *testing.T) {
	var gotPath, gotMethod string
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	if err := g.SubscribeApp(context.Background()); err != nil {
		t.Fatalf("SubscribeApp: %v", err)
	}
	if !strings.Contains(gotPath, "/subscribed_apps") {
		t.Errorf("path = %q, want subscribed_apps", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
}

func TestSubscribeApp_InvalidPageID(t *testing.T) {
	// Don't need a server — validateFBID will reject before doRequest runs.
	g := &GraphClient{
		httpClient:      &http.Client{},
		pageAccessToken: "tok",
		pageID:          "bad id", // space not allowed
	}
	err := g.SubscribeApp(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid pageID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid facebook ID") {
		t.Errorf("err = %v, want ID validation error", err)
	}
}

// --- GetPost / GetComment / GetCommentThread ---

func TestGetPost_Success(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"777","message":"hello","created_time":"2026-04-11T00:00:00Z"}`))
	}))
	post, err := g.GetPost(context.Background(), "777")
	if err != nil {
		t.Fatalf("GetPost: %v", err)
	}
	if post.ID != "777" || post.Message != "hello" {
		t.Errorf("post = %+v", post)
	}
}

func TestGetPost_RejectsBadID(t *testing.T) {
	g := NewGraphClient("tok", "111")
	if _, err := g.GetPost(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty id")
	}
}

func TestGetPost_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":777}`)) // wrong type — ID should be string
	}))
	// This actually unmarshals fine (ID: "" because of type mismatch skipped), let's
	// force a real parse error with outright invalid JSON.
	_ = g
	g2 := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	if _, err := g2.GetPost(context.Background(), "777"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestGetComment_Success(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"111222","message":"hi","from":{"id":"u1","name":"Alice"}}`))
	}))
	c, err := g.GetComment(context.Background(), "111222")
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if c.ID != "111222" || c.From.Name != "Alice" {
		t.Errorf("comment = %+v", c)
	}
}

func TestGetComment_InvalidID(t *testing.T) {
	g := NewGraphClient("tok", "111")
	if _, err := g.GetComment(context.Background(), "invalid id"); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetComment_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	if _, err := g.GetComment(context.Background(), "123"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestGetCommentThread_Success(t *testing.T) {
	var gotQuery string
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[{"id":"111222","message":"a","from":{"id":"u1"}},{"id":"c2","message":"b","from":{"id":"u2"}}]}`))
	}))
	got, err := g.GetCommentThread(context.Background(), "999888", 5)
	if err != nil {
		t.Fatalf("GetCommentThread: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !strings.Contains(gotQuery, "limit=5") {
		t.Errorf("query = %q, want limit=5", gotQuery)
	}
}

func TestGetCommentThread_ZeroLimitDefaults(t *testing.T) {
	var gotQuery string
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	if _, err := g.GetCommentThread(context.Background(), "999888", 0); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(gotQuery, "limit=10") {
		t.Errorf("query = %q, want default limit=10", gotQuery)
	}
}

func TestGetCommentThread_InvalidParent(t *testing.T) {
	g := NewGraphClient("tok", "111")
	if _, err := g.GetCommentThread(context.Background(), "bad id", 5); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetCommentThread_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`bad`))
	}))
	if _, err := g.GetCommentThread(context.Background(), "123", 5); err == nil {
		t.Fatal("expected parse error")
	}
}

// --- ReplyToComment / SendMessage / SendTypingOn ---

func TestReplyToComment_Success(t *testing.T) {
	var gotBody map[string]any
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"id":"new-reply-1"}`))
	}))
	id, err := g.ReplyToComment(context.Background(), "111222", "thanks!")
	if err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}
	if id != "new-reply-1" {
		t.Errorf("id = %q, want new-reply-1", id)
	}
	if gotBody["message"] != "thanks!" {
		t.Errorf("body message = %v", gotBody["message"])
	}
}

func TestReplyToComment_InvalidID(t *testing.T) {
	g := NewGraphClient("tok", "111")
	if _, err := g.ReplyToComment(context.Background(), "", "msg"); err == nil {
		t.Fatal("expected error")
	}
}

func TestReplyToComment_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`broken`))
	}))
	if _, err := g.ReplyToComment(context.Background(), "123", "msg"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSendMessage_SuccessAndBodyShape(t *testing.T) {
	var gotBody map[string]any
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"message_id":"mid-42"}`))
	}))
	id, err := g.SendMessage(context.Background(), "user-1", "hi")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if id != "mid-42" {
		t.Errorf("id = %q, want mid-42", id)
	}
	rec, _ := gotBody["recipient"].(map[string]any)
	if rec == nil || rec["id"] != "user-1" {
		t.Errorf("recipient = %v", gotBody["recipient"])
	}
	msg, _ := gotBody["message"].(map[string]any)
	if msg == nil || msg["text"] != "hi" {
		t.Errorf("message = %v", gotBody["message"])
	}
}

func TestSendMessage_ParseError(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`fail`))
	}))
	if _, err := g.SendMessage(context.Background(), "u", "m"); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestSendTypingOn_Success(t *testing.T) {
	var gotBody map[string]any
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{}`))
	}))
	if err := g.SendTypingOn(context.Background(), "user-9"); err != nil {
		t.Fatalf("SendTypingOn: %v", err)
	}
	if gotBody["sender_action"] != "typing_on" {
		t.Errorf("sender_action = %v", gotBody["sender_action"])
	}
}

// --- doRequest retry + error paths ---

func TestDoRequest_500RetriesThenSucceeds(t *testing.T) {
	var attempts int32
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","name":"n"}`))
	}))
	// Use a long context so backoff sleeps don't exceed the deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := g.VerifyToken(ctx); err != nil {
		t.Fatalf("VerifyToken after retries: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

func TestDoRequest_AllowsGraphErrorPropagation(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":100,"message":"param bad"}}`))
	}))
	_, err := g.GetPost(context.Background(), "111")
	if err == nil {
		t.Fatal("expected graph api error")
	}
	var ge *graphAPIError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *graphAPIError", err)
	}
	if ge.code != 100 {
		t.Errorf("code = %d, want 100", ge.code)
	}
}

func TestDoRequest_24hWindowErrorNotRetried(t *testing.T) {
	var attempts int32
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":551,"message":"24h window"}}`))
	}))
	_, err := g.SendMessage(context.Background(), "u", "m")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 551)", got)
	}
}

func TestDoRequest_SubcodeTriggers24hNotRetried(t *testing.T) {
	var attempts int32
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		// Different top-level code but matching subcode.
		_, _ = w.Write([]byte(`{"error":{"code":200,"error_subcode":2018109,"message":"outside window"}}`))
	}))
	_, err := g.SendMessage(context.Background(), "u", "m")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestDoRequest_429RetriesWithBackoff(t *testing.T) {
	var attempts int32
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":4,"message":"rate limited"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","name":"n"}`))
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := g.VerifyToken(ctx); err != nil {
		t.Fatalf("VerifyToken after 429 retry: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got < 2 {
		t.Errorf("attempts = %d, want ≥2", got)
	}
}

func TestDoRequest_ContextCancelledDuringBackoff(t *testing.T) {
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60") // force a long sleep
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":4,"message":"rate limited"}}`))
	}))
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel shortly after dispatch.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := g.GetPost(ctx, "111")
	if err == nil {
		t.Fatal("expected context cancel error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestDoRequest_TransportErrorThenSuccess(t *testing.T) {
	// Use a client that fails the first request with a transport error.
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			// Hijack and close to trigger a transport error on the client.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("ResponseWriter not a Hijacker")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"id":"ok","name":"n"}`))
	}))
	t.Cleanup(srv.Close)
	swapGraphBase(t, srv.URL)
	g := NewGraphClient("tok", "111")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := g.VerifyToken(ctx); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		t.Errorf("calls = %d, want ≥2 (retry after transport error)", got)
	}
}

func TestDoRequest_401RetriesExhaustedReturnsErr(t *testing.T) {
	var calls int32
	g := newFakeGraph(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":190,"message":"token expired"}}`))
	}))
	_, err := g.GetPost(context.Background(), "111")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsAuthError(err) {
		t.Errorf("err = %v, want auth error", err)
	}
	// Auth errors are non-retryable at the Graph API level (not 5xx, not 429).
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 401 with code 190)", got)
	}
}

// --- logRateLimit ---

func TestLogRateLimit_HighUsageDoesNotPanic(t *testing.T) {
	g := NewGraphClient("tok", "111")
	mk := func(header string) *http.Response {
		h := http.Header{}
		h.Set("X-Business-Use-Case-Usage", header)
		return &http.Response{Header: h}
	}
	// Each call should complete without panic (log warn only).
	g.logRateLimit(mk(""))
	g.logRateLimit(mk(`not-json`))
	g.logRateLimit(mk(fmt.Sprintf(`{"111":[{"call_count":%d}]}`, 50)))
	g.logRateLimit(mk(fmt.Sprintf(`{"111":[{"call_count":%d}]}`, 85)))
	g.logRateLimit(mk(fmt.Sprintf(`{"111":[{"call_count":%d}]}`, 97)))
}

// TestErrorClassifiersExtraCodes covers the additional rate-limit codes
// (17, 32, 613) and the 102 auth alias that aren't in facebook_test.go.
func TestErrorClassifiersExtraCodes(t *testing.T) {
	cases := []struct {
		code int
		auth bool
		perm bool
		rate bool
	}{
		{102, true, false, false},
		{200, false, true, false},
		{17, false, false, true},
		{32, false, false, true},
		{613, false, false, true},
		{1, false, false, false}, // unknown code matches nothing
	}
	for _, tc := range cases {
		err := &graphAPIError{code: tc.code}
		if got := IsAuthError(err); got != tc.auth {
			t.Errorf("code %d IsAuth = %v, want %v", tc.code, got, tc.auth)
		}
		if got := IsPermissionError(err); got != tc.perm {
			t.Errorf("code %d IsPerm = %v, want %v", tc.code, got, tc.perm)
		}
		if got := IsRateLimitError(err); got != tc.rate {
			t.Errorf("code %d IsRate = %v, want %v", tc.code, got, tc.rate)
		}
	}
	// non-graph error → all false
	other := errors.New("plain")
	if IsAuthError(other) || IsPermissionError(other) || IsRateLimitError(other) {
		t.Error("plain error should not match any classifier")
	}
}

// --- graphAPIError Error() ---

func TestGraphAPIErrorMessage(t *testing.T) {
	e := &graphAPIError{code: 17, msg: "user limit"}
	s := e.Error()
	if !strings.Contains(s, "17") || !strings.Contains(s, "user limit") {
		t.Errorf("Error() = %q, want both code and msg", s)
	}
}
