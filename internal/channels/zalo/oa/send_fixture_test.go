package oa

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestSend_WireShape_Fixtures locks the exact JSON bytes each Send* function
// sends to /v3.0/oa/message/cs. Guards against accidental byte-drift in the
// outbound wire shape. Runs under plain `go test -race`, no build tag.
//
// On mismatch: either (a) an unintended behavior change — revert it, or
// (b) the wire shape was intentionally changed — regenerate the fixture
// AND land that behavior change as a separate commit with a clear subject.
func TestSend_WireShape_Fixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		call           func(c *Channel) (string, error)
		wantReqFixture string
		uploadFixture  string // empty for text-only
		uploadPath     string // empty for text-only
		wantMID        string
	}{
		{
			name: "SendText",
			call: func(c *Channel) (string, error) {
				return c.SendText(context.Background(), "user-fixture", "hello fixture", "")
			},
			wantReqFixture: "testdata/send_text_request.json",
			wantMID:        "msg-fixture-1",
		},
		{
			name: "SendText_Quote",
			call: func(c *Channel) (string, error) {
				return c.SendText(context.Background(), "186729651760683225", "Chào bạn", "48687128d04c9410cd5f")
			},
			wantReqFixture: "testdata/send_text_quote_request.json",
			wantMID:        "msg-fixture-1",
		},
		{
			name: "SendImage",
			call: func(c *Channel) (string, error) {
				return c.SendImage(context.Background(), "user-fixture", []byte("\x89PNG\r\n\x1a\nfake"), "image/png")
			},
			wantReqFixture: "testdata/send_image_request.json",
			uploadFixture:  "testdata/upload_image_200.json",
			uploadPath:     "/v2.0/oa/upload/image",
			wantMID:        "msg-fixture-1",
		},
		{
			name: "SendGIF",
			call: func(c *Channel) (string, error) {
				return c.SendGIF(context.Background(), "user-fixture", []byte("GIF89a-fake"))
			},
			wantReqFixture: "testdata/send_gif_request.json",
			uploadFixture:  "testdata/upload_gif_200.json",
			uploadPath:     "/v2.0/oa/upload/gif",
			wantMID:        "msg-fixture-1",
		},
		{
			name: "SendFile",
			call: func(c *Channel) (string, error) {
				return c.SendFile(context.Background(), "user-fixture", []byte("%PDF-fake"), "doc.pdf")
			},
			wantReqFixture: "testdata/send_file_request.json",
			uploadFixture:  "testdata/upload_file_200.json",
			uploadPath:     "/v2.0/oa/upload/file",
			wantMID:        "msg-fixture-1",
		},
	}

	sendReply := mustReadFixture(t, "testdata/send_message_200.json")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var sendBody []byte
			var msgCount int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.uploadPath:
					// drain multipart body but don't need it for wire-shape assertions
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(mustReadFixture(t, tc.uploadFixture))
				case "/v3.0/oa/message/cs":
					if atomic.AddInt32(&msgCount, 1) == 1 {
						body, _ := io.ReadAll(r.Body)
						sendBody = body
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(sendReply)
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			t.Cleanup(srv.Close)

			refresh, _ := newRefreshServer(t, "")
			c := newSendChannel(t, srv, refresh, &fakeStore{})

			mid, err := tc.call(c)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if mid != tc.wantMID {
				t.Errorf("message_id = %q, want %q", mid, tc.wantMID)
			}
			if sendBody == nil {
				t.Fatalf("send body not captured")
			}

			want := mustReadFixture(t, tc.wantReqFixture)
			if !jsonCanonicalEqual(t, sendBody, want) {
				t.Errorf("wire-shape drift for %s\n got: %s\nwant: %s",
					tc.name, canonicalize(t, sendBody), canonicalize(t, want))
			}
		})
	}
}

// mustReadFixture reads a testdata file relative to this test package.
func mustReadFixture(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.FromSlash(rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return b
}

// jsonCanonicalEqual compares two JSON byte slices after unmarshal+remarshal
// so field order doesn't matter. Go's json.Marshal sorts map keys, so the
// remarshaled output is deterministic.
func jsonCanonicalEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	return bytes.Equal(canonicalize(t, a), canonicalize(t, b))
}

func canonicalize(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("canonicalize unmarshal: %v\nraw: %s", err, string(raw))
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("canonicalize marshal: %v", err)
	}
	return out
}
