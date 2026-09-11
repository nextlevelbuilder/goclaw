package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/mediabudget"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type mockCredentialProvider struct {
	apiKey  string
	apiBase string
}

func (m *mockCredentialProvider) APIKey() string  { return m.apiKey }
func (m *mockCredentialProvider) APIBase() string { return m.apiBase }

// parseTestByteRange resolves a "bytes=A-B" or suffix "bytes=-N" spec against
// a known total, mirroring what a real range-capable file server would do.
func parseTestByteRange(header string, total int64) (start, end int64, ok bool) {
	const prefix = "bytes="
	if !strings.HasPrefix(header, prefix) {
		return 0, 0, false
	}
	spec := strings.TrimPrefix(header, prefix)
	if strings.HasPrefix(spec, "-") {
		n, err := strconv.ParseInt(spec[1:], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, true
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	s, err1 := strconv.ParseInt(parts[0], 10, 64)
	e, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if e >= total {
		e = total - 1
	}
	return s, e, true
}

func TestReadVideo_BothMediaIdAndUrl_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt":   "describe this video",
		"media_id": "video-123",
		"url":      "https://example.com/video.mp4",
	})

	if !res.IsError {
		t.Fatalf("expected error when both media_id and url are provided")
	}

	if !strings.Contains(res.ForLLM, "Both 'media_id' and 'url' parameters cannot be specified") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

func TestReadVideo_PrivateURL_Error(t *testing.T) {
	tool := NewReadVideoTool(nil, nil)

	res := tool.Execute(context.Background(), map[string]any{
		"prompt": "describe this video",
		"url":    "http://127.0.0.1/video.mp4",
	})

	if !res.IsError {
		t.Fatalf("expected error for private video URL")
	}
	if !strings.Contains(res.ForLLM, "Invalid video URL") {
		t.Errorf("unexpected error message: %s", res.ForLLM)
	}
}

// TestReadVideo_GeminiURL_UnmeasurableOriginNeverGetsAStreamingGET locks the
// front gate: every request to this server fails, so both range probes and the
// HEAD learn nothing. Nothing later could price the payload either, so the
// streaming GET to a caller-supplied URL must never be issued.
func TestReadVideo_GeminiURL_UnmeasurableOriginNeverGetsAStreamingGET(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	hits, streamHits := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method == http.MethodGet && r.Header.Get("Range") == "" {
			streamHits++
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}

	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	params := map[string]any{
		"prompt":         "describe this video",
		"url":            ts.URL,
		"_provider_type": "gemini",
	}

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", params)
	if !errors.Is(err, mediabudget.ErrNoProbeableBytes) {
		t.Fatalf("expected the refusal to name the unprobeable source, got %T: %v", err, err)
	}
	if !isBudgetRefusal(err) {
		t.Fatalf("the refusal must be terminal for the fallback chain, got %T: %v", err, err)
	}
	if streamHits != 0 {
		t.Fatalf("the streaming GET reached the origin %d time(s), want 0", streamHits)
	}
	if hits != 3 {
		t.Fatalf("the URL was contacted %d time(s), want 3 (head probe, tail probe, HEAD)", hits)
	}
}

// TestReadVideo_GeminiURL_RejectsMissingContentLength covers the Content-Length
// check in read_video_resolve.go, which was dead code while the fail-closed
// reservation refused every streamed URL before transport.
func TestReadVideo_GeminiURL_RejectsMissingContentLength(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	stubMediaProbes(t, 20, 1)
	const total = int64(215_000)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			// Flushing before the body makes Go answer chunked, which is what
			// an origin with no declared length looks like.
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			w.Write(make([]byte, 16))
			return
		}
		start, end, ok := parseTestByteRange(r.Header.Get("Range"), total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, end-start+1))
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support static streaming") {
		t.Fatalf("expected the Content-Length check to reject, got: %v", err)
	}
}

// TestReadVideo_GeminiURL_RejectsOversizedStream covers the 2 GB stream ceiling.
// The clip measures 20 s, so the budget lets it through and this ceiling is
// what rejects the 3 GB the origin declares.
func TestReadVideo_GeminiURL_RejectsOversizedStream(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)
	stubMediaProbes(t, 20, 1)

	const total = int64(3_221_225_472) // 3 GB, over the 2 GB ceiling

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			return
		}
		start, end, ok := parseTestByteRange(r.Header.Get("Range"), total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, 1024))
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds the maximum limit of 2 GB") {
		t.Fatalf("expected the 2 GB ceiling to reject, got: %v", err)
	}
}

// TestReadVideo_ProbeVideoURL_RequestsHeadAndTailRanges proves the probe
// issues both windows against a range-capable server and uses a suffix spec
// for the tail, matching what the mediabudget estimator needs.
func TestReadVideo_ProbeVideoURL_RequestsHeadAndTailRanges(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	const total = int64(2_000_000)

	var ranges []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		ranges = append(ranges, rangeHeader)

		start, end, ok := parseTestByteRange(rangeHeader, total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, end-start+1))
	}))
	defer ts.Close()

	_, pinnedIP, err := security.Validate(ts.URL)
	if err != nil {
		t.Fatalf("validate test server URL: %v", err)
	}

	payload, ok := probeVideoURL(context.Background(), pinnedIP, ts.URL, "video/mp4")
	if !ok {
		t.Fatal("expected probing to succeed against a range-capable server")
	}
	if payload.Size != total {
		t.Fatalf("payload size = %d, want %d", payload.Size, total)
	}
	if len(ranges) != 2 {
		t.Fatalf("got %d range requests, want 2: %v", len(ranges), ranges)
	}
	if ranges[0] != "bytes=0-524287" {
		t.Errorf("head range = %q, want %q", ranges[0], "bytes=0-524287")
	}
	if ranges[1] != "bytes=-524288" {
		t.Errorf("tail range = %q, want %q", ranges[1], "bytes=-524288")
	}
}

// TestReadVideo_ProbeVideoRange_BoundsReadOnUnsupportedRanges covers a server
// that answers 200 to a ranged request instead of 206: the probe must cap its
// read at the head window and close immediately rather than pull the whole
// declared body, even though the requested range (here, a suffix spec far
// bigger than the head window) would otherwise permit reading more.
func TestReadVideo_ProbeVideoRange_BoundsReadOnUnsupportedRanges(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	const declared = int64(8 * mediabudget.HeadBytes)
	big := make([]byte, declared)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt(declared, 10))
		w.WriteHeader(http.StatusOK) // ignores Range: no range support
		w.Write(big)
	}))
	defer ts.Close()

	_, pinnedIP, err := security.Validate(ts.URL)
	if err != nil {
		t.Fatalf("validate test server URL: %v", err)
	}

	rangeHeader := fmt.Sprintf("bytes=-%d", mediabudget.TailBytes)
	data, total, isHead := probeVideoRange(context.Background(), pinnedIP, ts.URL, rangeHeader, declared)
	if total != declared {
		t.Fatalf("total = %d, want %d", total, declared)
	}
	if len(data) != mediabudget.HeadBytes {
		t.Fatalf("read %d bytes, want exactly the head window (%d)", len(data), mediabudget.HeadBytes)
	}
	if !isHead {
		t.Fatal("a 200 to a suffix range returned the start of the file and must be reported as head bytes")
	}
}

// TestReadVideo_GeminiURL_RejectsHugeMediaBeforeStreaming covers the front
// gate: a video the probe measures as an hour long, too much for the agent's
// window, is refused by the reservation itself, before the real streaming GET
// (the request with no Range header) is ever issued.
func TestReadVideo_GeminiURL_RejectsHugeMediaBeforeStreaming(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)
	stubMediaProbes(t, 3600, 1)

	const total = int64(3_221_225_472) // 3 GB

	streamHits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			streamHits++
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			return
		}
		start, end, ok := parseTestByteRange(rangeHeader, total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(make([]byte, end-start+1))
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if err == nil {
		t.Fatal("expected the reservation to reject a video this large under a small window")
	}
	if !strings.Contains(err.Error(), "context window exceeded") {
		t.Fatalf("expected a context window rejection, got: %v", err)
	}
	if streamHits != 0 {
		t.Fatalf("the streaming GET reached the server %d time(s), want 0", streamHits)
	}
}

// stubGeminiVideoStream replaces the upload so a gate-passing URL can be
// followed all the way to transport without contacting a provider.
func stubGeminiVideoStream(tool *ReadVideoTool, record func(contentLength int64, mime string)) {
	tool.setStreamToGeminiForTest(func(_ context.Context, _, _, _ string, body io.Reader, contentLength int64, mime string, _ time.Duration) (*providers.ChatResponse, error) {
		io.Copy(io.Discard, body)
		record(contentLength, mime)
		return &providers.ChatResponse{Content: "a short clip"}, nil
	})
}

// TestReadVideo_GeminiURL_MeasuredPayloadReachesTransportInBudget pins the
// rule that a measurement, once taken, is the charge: a 20-second clip costs
// 5,260 tokens and must reach transport under a 200k window, with the re-gate
// after the streaming response forbidden to re-derive anything from its
// Content-Length and overrule that.
func TestReadVideo_GeminiURL_MeasuredPayloadReachesTransportInBudget(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)
	stubMediaProbes(t, 20, 1)

	const total = int64(215_000)
	body := make([]byte, total)

	var ranges []string
	streamHits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			streamHits++
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			w.Write(body)
			return
		}
		ranges = append(ranges, rangeHeader)
		start, end, ok := parseTestByteRange(rangeHeader, total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[start : end+1])
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	var uploadedLength int64
	var uploadedMIME string
	stubGeminiVideoStream(tool, func(contentLength int64, mime string) {
		uploadedLength, uploadedMIME = contentLength, mime
	})

	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	out, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if err != nil {
		t.Fatalf("a measured 20s clip must reach transport under a 200k window, got: %v", err)
	}
	if string(out) != "a short clip" {
		t.Fatalf("provider output = %q, want the stubbed upload's answer", out)
	}
	if len(ranges) != 2 || ranges[0] != "bytes=0-524287" || ranges[1] != "bytes=-524288" {
		t.Fatalf("unexpected range requests, want head then tail: %v", ranges)
	}
	if streamHits != 1 {
		t.Fatalf("streaming GET reached %d time(s), want 1", streamHits)
	}
	if uploadedLength != total || uploadedMIME != "video/mp4" {
		t.Fatalf("upload got %d bytes of %q, want %d of video/mp4", uploadedLength, uploadedMIME, total)
	}
}

// TestReadVideo_GeminiURL_RangeRefusingOriginBlockedBeforeStreaming is the
// regression test for the cap bypass. A WAF or CDN that answers 403 to any
// ranged read leaves both probes empty-handed, so nothing about this 1.9 GB
// payload can be measured and the streaming GET must never be issued at all.
func TestReadVideo_GeminiURL_RangeRefusingOriginBlockedBeforeStreaming(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	const total = int64(1_900_000_000)

	headHits, streamHits := 0, 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodHead {
			headHits++
			w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
			w.WriteHeader(http.StatusOK)
			return
		}
		streamHits++
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if !errors.Is(err, mediabudget.ErrDurationUnmeasurable) {
		t.Fatalf("expected ErrDurationUnmeasurable for an unmeasurable 1.9 GB payload, got %T: %v", err, err)
	}
	if headHits != 1 {
		t.Fatalf("HEAD reached the origin %d time(s), want 1", headHits)
	}
	if streamHits != 0 {
		t.Fatalf("the streaming request reached the origin %d time(s), want 0", streamHits)
	}
}

// TestReadVideo_GeminiURL_RefusesBeforeFetchingAnUnmeasurableOrigin covers an
// origin that reveals nothing before the stream: no ranged read, no HEAD. A
// payload with neither size nor bytes can never be priced, so the refusal
// belongs at the front gate, before a GET is made to an attacker-chosen URL.
func TestReadVideo_GeminiURL_RefusesBeforeFetchingAnUnmeasurableOrigin(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)

	const total = int64(1_900_000_000)

	streamHits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" || r.Method == http.MethodHead {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		streamHits++
		w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 4096))
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	uploads := 0
	stubGeminiVideoStream(tool, func(int64, string) { uploads++ })

	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if !errors.Is(err, mediabudget.ErrNoProbeableBytes) {
		t.Fatalf("expected the front gate to refuse an unpriceable payload, got %T: %v", err, err)
	}
	if streamHits != 0 {
		t.Fatalf("the origin was fetched %d time(s) for a payload nothing could price, want 0", streamHits)
	}
	if uploads != 0 {
		t.Fatalf("the body was forwarded to the upload %d time(s), want 0", uploads)
	}
}

// TestReadVideo_GeminiURL_StreamingGetSurfacesOriginStatus covers the
// streaming GET's non-2xx branch (read_video_resolve.go:274-280): an origin
// that answers both ranged probes with real, measurable bytes is priced and
// let through the front gate, and when the streaming GET itself then fails,
// the error must name the origin's status code.
func TestReadVideo_GeminiURL_StreamingGetSurfacesOriginStatus(t *testing.T) {
	security.SetAllowLoopbackForTest(true)
	defer security.SetAllowLoopbackForTest(false)
	stubMediaProbes(t, 20, 1)

	const total = int64(215_000)
	body := make([]byte, total)

	streamHits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			streamHits++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		start, end, ok := parseTestByteRange(rangeHeader, total)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(body[start : end+1])
	}))
	defer ts.Close()

	tool := NewReadVideoTool(nil, nil)
	cp := &mockCredentialProvider{apiKey: "test-key"}
	ctx := store.WithAgentContextWindow(context.Background(), 200_000)
	ctx = store.WithAgentMaxTokens(ctx, 32_000)

	_, _, err := tool.callProvider(ctx, cp, "gemini", "gemini-2.5-flash", map[string]any{
		"prompt": "describe this video", "url": ts.URL, "_provider_type": "gemini",
	})
	if err == nil || !strings.Contains(err.Error(), "status code 500") {
		t.Fatalf("expected the streaming GET's status check to reject, got: %v", err)
	}
	if streamHits != 1 {
		t.Fatalf("streaming GET reached %d time(s), want 1", streamHits)
	}
}
