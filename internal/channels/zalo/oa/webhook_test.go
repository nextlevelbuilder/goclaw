package oa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// newWebhookChannel builds an OA channel ready for webhook tests with a
// known app/secret/oa-id and the given sig mode + replay window.
func newWebhookChannel(t *testing.T, secret, mode string, replaySecs int) (*Channel, *bus.MessageBus) {
	t.Helper()
	creds := &ChannelCreds{
		AppID:            "app-1",
		SecretKey:        "oauth-key", // distinct from webhook secret (S7)
		OAID:             "oa-1",
		WebhookSecretKey: secret,
	}
	cfg := config.ZaloOAConfig{
		Transport:                  "webhook",
		WebhookSignatureMode:       mode,
		WebhookReplayWindowSeconds: replaySecs,
	}
	mb := bus.New()
	c, err := New("webhook_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	return c, mb
}

// signedPayload builds a body whose top-level timestamp + signature header
// are computed against (appID, body, ts, secret) per the OA scheme.
// Uses Header.Set so the canonical key matches verifier's Get lookup.
func signedPayload(t *testing.T, appID, secret string, ts int64, body string) (http.Header, []byte) {
	t.Helper()
	full := fmt.Sprintf(`{"timestamp":%d,%s}`, ts, body)
	tsStr := fmt.Sprintf("%d", ts)
	sig := computeOASignature(appID, full, tsStr, secret)
	h := http.Header{}
	h.Set(zaloOASignatureHeader, sig)
	return h, []byte(full)
}

// nowMs is the canonical millisecond timestamp used by Zalo OA payloads.
func nowMs() int64 { return time.Now().UnixMilli() }

// ---------- signature scheme + verifier ----------

func TestComputeOASignature_FixedFixture(t *testing.T) {
	t.Parallel()
	// Fixed input → known output. Verify with:
	//   echo -n 'XBODY1234567890Y' | shasum -a 256
	sig := computeOASignature("X", "BODY", "1234567890", "Y")
	const want = "2f1ef5aabe67e8396a459ca89562e108ad541f82ba5022c85f645bd6b7220cb9"
	if sig != want {
		t.Fatalf("sig = %q, want %q", sig, want)
	}
}

func TestNormalizeMode(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":         "disabled",
		"strict":   "strict",
		"log_only": "log_only",
		"disabled": "disabled",
		"weird":    "disabled",
	}
	for in, want := range cases {
		if got := normalizeMode(in); got != want {
			t.Errorf("normalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampReplayWindowSeconds(t *testing.T) {
	t.Parallel()
	cases := map[int]time.Duration{
		0:     5 * time.Minute,    // unset → default
		-5:    5 * time.Minute,    // negative → default
		30:    60 * time.Second,   // below floor
		120:   120 * time.Second,  // in range
		3600:  3600 * time.Second, // at ceiling
		10000: 3600 * time.Second, // above ceiling
	}
	for in, want := range cases {
		if got := clampReplayWindowSeconds(in); got != want {
			t.Errorf("clampReplayWindowSeconds(%d) = %v, want %v", in, got, want)
		}
	}
}

func TestVerifier_AcceptsValidSignature(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	hdr, body := signedPayload(t, "app-1", "secret", nowMs(), `"event_name":"x"`)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifier_RejectsMissingHeader(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := fmt.Appendf(nil, `{"timestamp":%d}`, nowMs())
	if err := v.Verify(http.Header{}, body); err == nil || !strings.Contains(err.Error(), "missing X-ZEvent-Signature") {
		t.Errorf("Verify(no header) err = %v, want missing-header", err)
	}
}

func TestVerifier_RejectsLengthMismatch(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := fmt.Appendf(nil, `{"timestamp":%d}`, nowMs())
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, "deadbeef") // shorter than 64-char hex
	err := v.Verify(hdr, body)
	if !errors.Is(err, common.ErrSignatureMismatch) {
		t.Errorf("Verify(short sig) err = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifier_RejectsWrongSignature(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := fmt.Appendf(nil, `{"timestamp":%d}`, nowMs())
	wrong := strings.Repeat("a", 64) // valid hex length, wrong value
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, wrong)
	err := v.Verify(hdr, body)
	if !errors.Is(err, common.ErrSignatureMismatch) {
		t.Errorf("Verify(wrong sig) err = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifier_RejectsEmptySecretInStrict(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "", "strict", time.Hour)
	body := fmt.Appendf(nil, `{"timestamp":%d}`, nowMs())
	if err := v.Verify(http.Header{}, body); err == nil || !strings.Contains(err.Error(), "secret unset") {
		t.Errorf("Verify err = %v, want secret-unset", err)
	}
}

// B5: log_only mode swallows mismatches but still accepts (return nil).
func TestVerifier_LogOnlyAcceptsMismatch(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "log_only", time.Hour)
	body := fmt.Appendf(nil, `{"timestamp":%d}`, nowMs())
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, strings.Repeat("a", 64))
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("log_only Verify(wrong sig) err = %v, want nil", err)
	}
}

// B5/N6: disabled mode skips verification entirely (still warns once).
func TestVerifier_DisabledAcceptsAnything(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "", "disabled", time.Hour)
	if err := v.Verify(http.Header{}, []byte(`{"x":1}`)); err != nil {
		t.Errorf("disabled Verify err = %v, want nil", err)
	}
}

// B7: replay window in strict mode rejects out-of-window timestamps.
func TestVerifier_RejectsReplay(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", 5*time.Minute)
	old := nowMs() - int64((10 * time.Minute).Milliseconds())
	hdr, body := signedPayload(t, "app-1", "secret", old, `"event_name":"x"`)
	err := v.Verify(hdr, body)
	if err == nil || !strings.Contains(err.Error(), "replay window") {
		t.Errorf("Verify(replay) err = %v, want replay-window error", err)
	}
}

func TestVerifier_AcceptsWithinReplayWindow(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", 5*time.Minute)
	recent := nowMs() - int64((1 * time.Minute).Milliseconds())
	hdr, body := signedPayload(t, "app-1", "secret", recent, `"event_name":"x"`)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify(within window) err = %v, want nil", err)
	}
}

// S4: timestamp parsed via json.Number → strconv.FormatInt produces the
// canonical decimal Zalo signs against. The verifier hashes the
// canonical form, not the raw JSON bytes.
func TestVerifier_TimestampCanonicalizedViaInt64(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	tsInt := nowMs()
	body := fmt.Appendf(nil, `{"timestamp":%d,"event_name":"x"}`, tsInt)
	tsStr := fmt.Sprintf("%d", tsInt)
	sig := computeOASignature("app-1", string(body), tsStr, "secret")
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, sig)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify(canonical ts) err = %v", err)
	}

	// Also verify extractTimestamp handles json.Number happily (covers the
	// internal canonicalization path even if the body is well-formed int).
	got, err := extractTimestamp(body)
	if err != nil {
		t.Fatalf("extractTimestamp: %v", err)
	}
	if got != tsInt {
		t.Errorf("extractTimestamp = %d, want %d", got, tsInt)
	}
}

// ---------- HandleWebhookEvent dispatch ----------

func TestHandleWebhookEvent_DispatchesText(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_text","sender":{"id":"alice","display_name":"Alice"},"message":{"message_id":"m1","text":"hello"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("no inbound published")
	}
	if got.Content != "hello" {
		t.Errorf("Content = %q", got.Content)
	}
	if got.SenderID != "alice" || got.ChatID != "alice" {
		t.Errorf("sender/chat = %q/%q, want alice/alice", got.SenderID, got.ChatID)
	}
	if got.Metadata["message_id"] != "m1" {
		t.Errorf("metadata.message_id = %q", got.Metadata["message_id"])
	}
}

// A8: sender == OAID is the bot's own outbound — must drop, not forward.
func TestHandleWebhookEvent_FiltersSelfEcho(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_text","sender":{"id":"oa-1"},"message":{"message_id":"m1","text":"loop"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("self-echo should not have published")
	}
}

// stubDownloader writes a fixture file and bypasses SSRF for hermetic tests.
func stubDownloader(t *testing.T, c *Channel, ext string, body []byte) {
	t.Helper()
	c.downloadMediaFn = func(_ context.Context, _ string) (string, error) {
		f, err := os.CreateTemp(t.TempDir(), "oa_test_*"+ext)
		if err != nil {
			return "", err
		}
		defer f.Close()
		if _, werr := f.Write(body); werr != nil {
			return "", werr
		}
		return f.Name(), nil
	}
}

func TestHandleWebhookEvent_DispatchesImage(t *testing.T) {
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	stubDownloader(t, ch, ".jpg", []byte("\xff\xd8\xff\xe0fake-jpeg"))
	payload := `{"event_name":"user_send_image","sender":{"id":"alice"},"message":{"message_id":"m_img","attachments":[{"type":"image","payload":{"url":"https://cdn.zalo.example/photo.jpg"}}]}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("image event was not dispatched")
	}
	if len(got.Media) != 1 {
		t.Fatalf("Media len = %d, want 1", len(got.Media))
	}
	if !strings.Contains(got.Content, "<media:image") {
		t.Errorf("Content missing <media:image tag: %q", got.Content)
	}
}

func TestHandleWebhookEvent_DispatchesFile(t *testing.T) {
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	stubDownloader(t, ch, ".xlsx", []byte("PK\x03\x04xlsx-bytes"))
	payload := `{"event_name":"user_send_file","sender":{"id":"alice"},"message":{"message_id":"m_file","text":"please summarize","attachments":[{"type":"file","payload":{"url":"https://cdn.zalo.example/report.xlsx","name":"report.xlsx"}}]}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("file event was not dispatched")
	}
	if !strings.Contains(got.Content, "please summarize") {
		t.Errorf("user caption dropped: %q", got.Content)
	}
	if !strings.Contains(got.Content, "<media:document") {
		t.Errorf("xlsx should classify as document, got: %q", got.Content)
	}
}

func TestHandleWebhookEvent_DispatchesLink(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_link","sender":{"id":"alice"},"message":{"message_id":"m_link","text":"check this","attachments":[{"type":"link","payload":{"url":"https://example.com","title":"Example","description":"a sample"}}]}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("link event was not dispatched")
	}
	if len(got.Media) != 0 {
		t.Errorf("link should not download media, got %d files", len(got.Media))
	}
	for _, want := range []string{"check this", "Example", "https://example.com", "a sample"} {
		if !strings.Contains(got.Content, want) {
			t.Errorf("link content missing %q: %q", want, got.Content)
		}
	}
}

func TestHandleWebhookEvent_AttachmentMissingURL(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_image","sender":{"id":"alice"},"message":{"message_id":"m_bad","attachments":[]}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("missing-URL attachment should not dispatch")
	}
}

func TestHandleWebhookEvent_UnknownEventNoError(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"some_future_thing","sender":{"id":"alice"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Errorf("unknown event should not error: %v", err)
	}
}

// Zalo sends `timestamp` as a string in real traffic; decode must not break.
func TestHandleWebhookEvent_AcceptsStringTimestamp(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_text","timestamp":"1714476720000","sender":{"id":"alice"},"message":{"message_id":"m_str","text":"hi"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("string-timestamp payload should decode, got: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("string-timestamp payload was not dispatched")
	}
}

func TestHandleWebhookEvent_BadJSONReturnsError(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "secret", "strict", 0)
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(`not-json`)); err == nil {
		t.Error("bad JSON must return error")
	}
}

func TestMessageIDExtractor(t *testing.T) {
	t.Parallel()
	e := oaMessageIDExtractor{}
	if got := e.ExtractMessageID(json.RawMessage(`{"message":{"message_id":"m1"}}`)); got != "m1" {
		t.Errorf("ExtractMessageID(message_id) = %q", got)
	}
	if got := e.ExtractMessageID(json.RawMessage(`{"message":{"msg_id":"m2"}}`)); got != "m2" {
		t.Errorf("ExtractMessageID(msg_id fallback) = %q", got)
	}
	if e.ExtractMessageID(json.RawMessage(`{}`)) != "" {
		t.Error("missing → empty")
	}
	if e.ExtractMessageID(json.RawMessage(`not-json`)) != "" {
		t.Error("invalid JSON → empty (no panic)")
	}
}

func TestStart_WebhookMissingSecretEntersBootstrap(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: "strict",
	}
	mb := bus.New()
	c, err := New("bootstrap_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.webhookRouter = common.NewRouter()

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	snap := c.HealthSnapshot()
	if string(snap.State) != "degraded" {
		t.Errorf("State = %v, want degraded", snap.State)
	}
	if snap.BootstrapState != channels.ChannelBootstrapAwaitingSecret {
		t.Errorf("BootstrapState = %q, want %q", snap.BootstrapState, channels.ChannelBootstrapAwaitingSecret)
	}
	if c.ResolvedWebhookSlug() == "" {
		t.Error("slug not registered in bootstrap")
	}
	if got := c.BootstrapDroppedForTest(); got != 0 {
		t.Errorf("drop counter = %d, want 0", got)
	}
}

// Bootstrap: HTTP POST to slug → 200, drop counter increments, no bus event.
func TestWebhookBootstrap_AcksAndDrops(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{Transport: "webhook", WebhookSignatureMode: "strict"}
	mb := bus.New()
	c, err := New("bootstrap_drop", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	router := common.NewRouter()
	c.webhookRouter = router

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())

	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	slug := c.ResolvedWebhookSlug()
	body := []byte(`{"event_name":"user_send_text","sender":{"id":"alice"},"message":{"message_id":"m_boot","text":"hi"}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+common.WebhookPathPrefix+slug, bytes.NewReader(body))
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("router post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (bootstrap acks even unsigned ping)", resp.StatusCode)
	}

	// Drop should run on the dispatch goroutine — give it a moment.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if c.BootstrapDroppedForTest() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := c.BootstrapDroppedForTest(); got != 1 {
		t.Errorf("drop counter = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("bootstrap MUST NOT publish events to bus")
	}
}

// After secret arrives + restart, verifier becomes strict: unsigned 401,
// signed 200 + dispatch. Mirrors the prod reload flow (Stop old, Start new
// instance via factory).
func TestWebhookBootstrap_TransitionsToStrictAfterSecret(t *testing.T) {
	t.Parallel()
	router := common.NewRouter()
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Stage 1: bootstrap channel.
	credsBoot := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: "strict",
		WebhookPath:          "fixedslug",
	}
	mb := bus.New()
	cBoot, err := New("transition_test", cfg, credsBoot, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New bootstrap: %v", err)
	}
	cBoot.SetInstanceID(uuid.New())
	cBoot.webhookRouter = router
	if err := cBoot.Start(context.Background()); err != nil {
		t.Fatalf("Start bootstrap: %v", err)
	}
	if string(cBoot.HealthSnapshot().State) != "degraded" {
		t.Fatalf("expected degraded, got %v", cBoot.HealthSnapshot().State)
	}
	_ = cBoot.Stop(context.Background())

	// Stage 2: fresh channel with secret, same slug.
	credsStrict := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1", WebhookSecretKey: "real-secret"}
	cStrict, err := New("transition_test", cfg, credsStrict, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New strict: %v", err)
	}
	cStrict.SetInstanceID(uuid.New())
	cStrict.webhookRouter = router
	if err := cStrict.Start(context.Background()); err != nil {
		t.Fatalf("Start strict: %v", err)
	}
	defer cStrict.Stop(context.Background())
	if string(cStrict.HealthSnapshot().State) != "healthy" {
		t.Errorf("expected healthy after secret, got %v", cStrict.HealthSnapshot().State)
	}

	url := srv.URL + common.WebhookPathPrefix + "fixedslug"

	// Unsigned POST → 401.
	unsigned := []byte(`{"event_name":"user_send_text","sender":{"id":"alice"},"message":{"message_id":"m_u","text":"hi"}}`)
	reqU, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(unsigned))
	respU, err := srv.Client().Do(reqU)
	if err != nil {
		t.Fatalf("unsigned post: %v", err)
	}
	if respU.StatusCode != http.StatusUnauthorized {
		t.Errorf("unsigned status = %d, want 401", respU.StatusCode)
	}

	// Signed POST → 200 + dispatch.
	hdr, body := signedPayload(t, "app-1", "real-secret", nowMs(),
		`"event_name":"user_send_text","sender":{"id":"alice"},"message":{"message_id":"m_s","text":"hi-strict"}`)
	reqS, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	reqS.Header = hdr
	respS, err := srv.Client().Do(reqS)
	if err != nil {
		t.Fatalf("signed post: %v", err)
	}
	if respS.StatusCode != http.StatusOK {
		t.Fatalf("signed status = %d, want 200", respS.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Error("strict mode did not dispatch signed event")
	}
}

// signature_mode=disabled + no secret → still goes Healthy (unchanged
// behavior). Bootstrap only triggers when mode != disabled.
func TestWebhookBootstrap_DisabledModeUnaffected(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: "disabled",
	}
	mb := bus.New()
	c, err := New("disabled_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.webhookRouter = common.NewRouter()
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())
	if string(c.HealthSnapshot().State) != "healthy" {
		t.Errorf("State = %v, want healthy (disabled mode skips bootstrap gate)", c.HealthSnapshot().State)
	}
}

// Start with transport=webhook + secret → registers with router; Stop unregisters.
func TestStart_WebhookRegistersAndStopUnregisters(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1", WebhookSecretKey: "secret"}
	cfg := config.ZaloOAConfig{
		Transport: "webhook",
	}
	mb := bus.New()
	c, err := New("start_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := uuid.New()
	c.SetInstanceID(id)
	router := common.NewRouter()
	c.webhookRouter = router

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.IsRunning() {
		t.Error("channel not Running after Start")
	}
	// Confirm registered: dispatch a request through the router and assert
	// the channel's HandleWebhookEvent runs.
	mux := http.NewServeMux()
	mux.Handle(common.WebhookPathPrefix, router)
	srv := httptest.NewServer(mux)
	defer srv.Close()
	slug := c.ResolvedWebhookSlug()
	if slug == "" {
		t.Fatal("ResolvedWebhookSlug empty after Start")
	}
	hdr, body := signedPayload(t, "app-1", "secret", nowMs(),
		`"event_name":"user_send_text","sender":{"id":"alice"},"message":{"message_id":"m1","text":"hi"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+common.WebhookPathPrefix+slug, bytes.NewReader(body))
	req.Header = hdr
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("router post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("router did not deliver event to channel handler")
	}

	// Stop unregisters → next request must 404.
	_ = c.Stop(context.Background())
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+common.WebhookPathPrefix+slug, bytes.NewReader(body))
	req2.Header = hdr
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatalf("router post 2: %v", err)
	}
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("after Stop: status = %d, want 404", resp2.StatusCode)
	}
}

// Start polling leaves the webhook router untouched.
func TestStart_PollingTransportIgnoresRouter(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1", AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour)}
	cfg := config.ZaloOAConfig{Transport: "polling"}
	mb := bus.New()
	c, err := New("start_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	router := common.NewRouter()
	c.webhookRouter = router

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())
	if !c.IsRunning() {
		t.Error("polling channel not Running")
	}
}

// S7: SignatureVerifier() must be wired to creds.WebhookSecretKey, NOT
// creds.SecretKey (the OAuth refresh credential). Verifying against the
// OAuth secret would silently reject every legit Zalo webhook delivery.
func TestSignatureVerifier_UsesWebhookSecretNotOAuthSecret(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "WEBHOOK-SECRET", "strict", 0)
	ts := nowMs()
	hdr, body := signedPayload(t, "app-1", "WEBHOOK-SECRET", ts, `"event_name":"user_send_text"`)
	if err := ch.SignatureVerifier().Verify(hdr, body); err != nil {
		t.Errorf("verifier rejected webhook-secret payload: %v (S7: must wire creds.WebhookSecretKey, not creds.SecretKey)", err)
	}
	// Sanity: the OAuth secret should NOT verify.
	hdr2, body2 := signedPayload(t, "app-1", "oauth-key", ts, `"event_name":"user_send_text"`)
	if err := ch.SignatureVerifier().Verify(hdr2, body2); err == nil {
		t.Error("OAuth-secret-signed payload accepted — verifier wired to wrong field")
	}
}
