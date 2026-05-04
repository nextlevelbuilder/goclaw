package oa

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// newReactionAPIServer captures requests to /v2.0/oa/message and replies
// from canned bodies. Distinct from newAPIServer to avoid touching the
// existing /v3.0/oa/message/cs routing the rest of the suite depends on.
func newReactionAPIServer(t *testing.T, replies []string) (*httptest.Server, *[]capturedRequest, *int32) {
	t.Helper()
	var captured []capturedRequest
	var idx int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = append(captured, capturedRequest{
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			accessToken: r.Header.Get("access_token"),
			body:        body,
		})
		if r.URL.Path != pathSendReaction {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		i := atomic.AddInt32(&idx, 1) - 1
		if int(i) >= len(replies) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":-1,"message":"no canned reply"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(replies[i]))
	}))
	t.Cleanup(srv.Close)
	return srv, &captured, &idx
}

func TestBuildReactionBody_Shape(t *testing.T) {
	t.Parallel()
	body := buildReactionBody("user-1", "msg-abc", "/-heart")
	rec, _ := body["recipient"].(map[string]any)
	sa, _ := body["sender_action"].(map[string]any)
	if rec["user_id"] != "user-1" {
		t.Errorf("recipient.user_id = %v", rec["user_id"])
	}
	if sa["react_icon"] != "/-heart" {
		t.Errorf("sender_action.react_icon = %v", sa["react_icon"])
	}
	if sa["react_message_id"] != "msg-abc" {
		t.Errorf("sender_action.react_message_id = %v", sa["react_message_id"])
	}
	// Round-trip JSON to confirm marshalability.
	if _, err := json.Marshal(body); err != nil {
		t.Fatalf("marshal: %v", err)
	}
}

func TestSendReaction_HappyPath(t *testing.T) {
	t.Parallel()
	api, captured, _ := newReactionAPIServer(t,
		[]string{`{"data":{"message_id":"react-mid-1","user_id":"user-1"},"error":0,"message":"Success"}`})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	mid, err := c.SendReaction(context.Background(), "user-1", "src-msg-1", "/-heart")
	if err != nil {
		t.Fatalf("SendReaction: %v", err)
	}
	if mid != "react-mid-1" {
		t.Errorf("mid = %q, want react-mid-1", mid)
	}
	if len(*captured) != 1 {
		t.Fatalf("captured %d, want 1", len(*captured))
	}
	r := (*captured)[0]
	if r.path != "/v2.0/oa/message" {
		t.Errorf("path = %q, want /v2.0/oa/message", r.path)
	}
	if r.accessToken != "AT-current" {
		t.Errorf("access_token = %q", r.accessToken)
	}
	if !strings.HasPrefix(r.contentType, "application/json") {
		t.Errorf("content-type = %q", r.contentType)
	}
	var body map[string]any
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	sa, _ := body["sender_action"].(map[string]any)
	if sa["react_icon"] != "/-heart" || sa["react_message_id"] != "src-msg-1" {
		t.Errorf("sender_action wrong: %v", sa)
	}
}

func TestSendReaction_PayloadFamilyError(t *testing.T) {
	t.Parallel()
	// -201 (params invalid) is FamilyPayload — source message_id might be
	// expired/over-cap. Must surface, must not retry.
	api, captured, _ := newReactionAPIServer(t,
		[]string{`{"error":-201,"message":"params invalid"}`})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	_, err := c.SendReaction(context.Background(), "user-1", "stale-msg", "/-heart")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v, want *APIError", err, err)
	}
	if Classify(apiErr.Code).Family != FamilyPayload {
		t.Errorf("family = %v, want payload", Classify(apiErr.Code).Family)
	}
	if len(*captured) != 1 {
		t.Errorf("captured %d, want 1 (payload errors must not retry)", len(*captured))
	}
}

// TestSendReaction_AuthError_NoRetryNoHealthFlip: phase-2 step 6 — reactions
// bypass c.post, so a 401-class error is returned as-is (one request, no
// ForceRefresh) and channel health is NOT flipped to Failed.
func TestSendReaction_AuthError_NoRetryNoHealthFlip(t *testing.T) {
	t.Parallel()
	api, captured, _ := newReactionAPIServer(t,
		[]string{`{"error":-216,"message":"access_token invalid"}`})
	refresh, refreshHits := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	_, err := c.SendReaction(context.Background(), "user-1", "msg", "/-heart")
	if err == nil {
		t.Fatal("expected auth error")
	}
	if len(*captured) != 1 {
		t.Errorf("captured %d, want 1 (no retry on auth)", len(*captured))
	}
	if n := atomic.LoadInt32(refreshHits); n != 0 {
		t.Errorf("refresh hits = %d, want 0 (reactions must not trigger ForceRefresh)", n)
	}
	if state := c.HealthSnapshot().State; state == channels.ChannelHealthStateFailed {
		t.Errorf("channel state = %v, must not flip to Failed on reaction auth error", state)
	}
}

func TestSendReaction_RejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	api, captured, _ := newReactionAPIServer(t, []string{`{"error":0,"data":{}}`})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	cases := []struct {
		name             string
		userID, mid, ico string
	}{
		{"empty userID", "", "msg", "/-heart"},
		{"empty messageID", "user", "", "/-heart"},
		{"empty icon", "user", "msg", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.SendReaction(context.Background(), tc.userID, tc.mid, tc.ico)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
			}
		})
	}
	if len(*captured) != 0 {
		t.Errorf("captured %d, want 0 (empty args must short-circuit)", len(*captured))
	}
}

// TestClearReactionAPI uses the real /-remove sentinel icon to retract a
// previously dropped reaction. Verifies the wire-level shape.
func TestSendReaction_RemoveSentinel(t *testing.T) {
	t.Parallel()
	api, captured, _ := newReactionAPIServer(t,
		[]string{`{"data":{"message_id":"rem-1","user_id":"u"},"error":0,"message":"Success"}`})
	refresh, _ := newRefreshServer(t, "")
	c := newSendChannel(t, api, refresh, &fakeStore{})

	if _, err := c.SendReaction(context.Background(), "u", "src", reactionIconRemove); err != nil {
		t.Fatalf("SendReaction(remove): %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal((*captured)[0].body, &body)
	sa, _ := body["sender_action"].(map[string]any)
	if sa["react_icon"] != "/-remove" {
		t.Errorf("react_icon = %v, want /-remove", sa["react_icon"])
	}
}
