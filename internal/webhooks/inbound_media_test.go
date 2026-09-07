package webhooks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	media "github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

// ---- helpers ----
//
// Tests that point at an httptest.Server hit 127.0.0.1, which the SSRF policy
// blocks. allowLoopback flips the process-global bypass for the duration of one
// test. That global is why none of these tests may call t.Parallel(): under
// -race a parallel test flipping it reports a data race that reads like a bug
// in the feature.
func allowLoopback(t *testing.T) {
	t.Helper()
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })
}

// isolateTempDir points os.CreateTemp at this test's own directory.
//
// Both this package and internal/http glob os.TempDir() for goclaw_webhook_*
// to prove a failed request left nothing behind, and `go test ./...` runs
// packages in parallel — without isolation each would see the other's files and
// the before/after deltas would flake. TMPDIR covers unix, TMP/TEMP cover
// Windows, so the isolation holds however the suite is run.
func isolateTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
}

// tinyPNG returns a valid 1x1 PNG.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// tinyJPEG returns a valid 1x1 JPEG.
func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// bombPNG returns a structurally valid PNG whose IHDR *declares* w x h while
// carrying the pixel data of a 1x1 image. DecodeConfig reads only the header,
// so this is exactly the shape of a decompression bomb: a few hundred bytes on
// the wire that would allocate w*h*4 bytes if fully decoded.
func bombPNG(t *testing.T, w, h uint32) []byte {
	t.Helper()
	b := tinyPNG(t)
	// Layout: 8-byte signature | 4-byte length | "IHDR" (12:16) | 13-byte data
	// (16:29) | 4-byte CRC (29:33). Width and height are the first 8 data bytes.
	binary.BigEndian.PutUint32(b[16:20], w)
	binary.BigEndian.PutUint32(b[20:24], h)
	// CRC covers chunk type + data.
	binary.BigEndian.PutUint32(b[29:33], crc32.ChecksumIEEE(b[12:29]))
	return b
}

// countTempFiles reports how many fetcher temp files are on disk, so a test can
// prove that a failed request left nothing behind.
func countTempFiles(t *testing.T) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "goclaw_webhook_*"))
	if err != nil {
		t.Fatalf("glob temp: %v", err)
	}
	return len(matches)
}

// serve starts a test server returning body under contentType.
func serve(t *testing.T, contentType string, body []byte) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

// ---- fetch tests ----

func TestFetchInboundMedia_HappyPath(t *testing.T) {
	allowLoopback(t)
	body := tinyJPEG(t)
	srv := serve(t, "image/jpeg", body)

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/photo.jpg", Filename: "photo.jpg"},
	})
	defer CleanupInboundMedia(res.Files)

	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failures)
	}
	if len(res.Files) != 1 || len(res.Infos) != 1 {
		t.Fatalf("Files=%d Infos=%d, want 1 each", len(res.Files), len(res.Infos))
	}
	f := res.Files[0]
	if f.MimeType != "image/jpeg" {
		t.Errorf("MimeType = %q, want image/jpeg", f.MimeType)
	}
	if f.Filename != "photo.jpg" {
		t.Errorf("Filename = %q, want photo.jpg", f.Filename)
	}
	st, err := os.Stat(f.Path)
	if err != nil {
		t.Fatalf("temp file not on disk: %v", err)
	}
	if st.Size() != int64(len(body)) {
		t.Errorf("size = %d, want %d", st.Size(), len(body))
	}
	info := res.Infos[0]
	if info.Type != "image" {
		t.Errorf("Info.Type = %q, want image", info.Type)
	}
	// SourceURL is what makes BuildMediaTags emit <media:image url="...">.
	if info.SourceURL == "" {
		t.Error("Info.SourceURL empty; the media tag would lose the URL")
	}
}

func TestFetchInboundMedia_FollowsRedirect(t *testing.T) {
	allowLoopback(t)
	body := tinyJPEG(t)
	dest := serve(t, "image/jpeg", body)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL+"/real.jpg", http.StatusFound)
	}))
	defer origin.Close()

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: origin.URL + "/signed", Filename: "real.jpg"},
	})
	defer CleanupInboundMedia(res.Files)

	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failures)
	}
	if len(res.Files) != 1 {
		t.Fatalf("Files = %d, want 1", len(res.Files))
	}
	// Assert on SIZE, not just on the absence of an error. security.NewSafeClient
	// refuses redirects via http.ErrUseLastResponse, which produces a
	// successful-looking response with an EMPTY body — no error, a 0-byte temp
	// file, and an agent that silently sees nothing. Only a size assertion
	// catches that regression.
	st, err := os.Stat(res.Files[0].Path)
	if err != nil {
		t.Fatalf("stat temp: %v", err)
	}
	if st.Size() == 0 {
		t.Fatal("downloaded 0 bytes through a redirect — wrong HTTP client")
	}
	if st.Size() != int64(len(body)) {
		t.Errorf("size = %d, want %d", st.Size(), len(body))
	}
}

func TestFetchInboundMedia_MimeFromResponseHeader(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "image/png", tinyPNG(t))

	// No extension anywhere in the URL or the filename: without the
	// response-header rule this lands as octet-stream, is typed as a document,
	// and vision skips it.
	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/download?id=1"},
	})
	defer CleanupInboundMedia(res.Files)

	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures: %+v", res.Failures)
	}
	if got := res.Files[0].MimeType; got != "image/png" {
		t.Errorf("MimeType = %q, want image/png", got)
	}
	if got := res.Files[0].Path; !strings.HasSuffix(got, ".png") {
		t.Errorf("temp path = %q, want a .png suffix", got)
	}
}

func TestFetchInboundMedia_TooLarge(t *testing.T) {
	isolateTempDir(t)
	allowLoopback(t)
	before := countTempFiles(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		io.CopyN(w, zeroReader{}, inboundMediaMaxBytes+1024)
	}))
	defer srv.Close()

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/big.jpg", Filename: "big.jpg"},
	})

	if len(res.Files) != 0 {
		t.Fatalf("Files = %d, want 0", len(res.Files))
	}
	if len(res.Failures) != 1 || res.Failures[0].Code != FailTooLarge {
		t.Fatalf("failures = %+v, want one too_large", res.Failures)
	}
	if after := countTempFiles(t); after != before {
		t.Errorf("temp files leaked: before=%d after=%d", before, after)
	}
}

func TestFetchInboundMedia_MimeDenied(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "text/html", []byte("<html><body>nope</body></html>"))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/page.html", Filename: "page.html"},
	})

	if len(res.Failures) != 1 || res.Failures[0].Code != FailMIMEDenied {
		t.Fatalf("failures = %+v, want one mime_denied", res.Failures)
	}
}

func TestFetchInboundMedia_SSRFBlocked(t *testing.T) {
	// Deliberately NO loopback bypass: this test asserts that a loopback URL is
	// refused. Setting and resetting the flag per test (never at TestMain level)
	// is what keeps this test honest.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
	}))
	defer srv.Close()

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/x.png"},
	})

	if len(res.Files) != 0 {
		t.Fatalf("Files = %d, want 0", len(res.Files))
	}
	if len(res.Failures) != 1 || res.Failures[0].Code != FailSSRF {
		t.Fatalf("failures = %+v, want one ssrf", res.Failures)
	}
}

func TestFetchInboundMedia_AnyFailureFailsAll(t *testing.T) {
	isolateTempDir(t)
	allowLoopback(t)
	before := countTempFiles(t)

	ok := serve(t, "image/png", tinyPNG(t))
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer missing.Close()

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: ok.URL + "/a.png", Filename: "a.png"},
		{URL: missing.URL + "/b.png", Filename: "b.png"},
		{URL: ok.URL + "/c.png", Filename: "c.png"},
	})

	if res.Files != nil || res.Infos != nil {
		t.Fatalf("all-or-nothing violated: Files=%v Infos=%v", res.Files, res.Infos)
	}
	if len(res.Failures) != 1 {
		t.Fatalf("failures = %+v, want exactly one", res.Failures)
	}
	if res.Failures[0].Index != 1 || res.Failures[0].Code != FailDownloadFailed {
		t.Errorf("failure = %+v, want {Index:1 Code:download_failed}", res.Failures[0])
	}
	if after := countTempFiles(t); after != before {
		t.Errorf("temp files leaked from the two successful items: before=%d after=%d", before, after)
	}
}

func TestFetchInboundMedia_CountCap(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "image/png", tinyPNG(t))

	items := make([]InboundMediaItem, MaxInboundMediaItems+2)
	for i := range items {
		items[i] = InboundMediaItem{URL: srv.URL + "/x.png", Filename: "x.png"}
	}
	res := FetchInboundMedia(context.Background(), items)

	// Rejected outright, never truncated: silent truncation reads as "we handled
	// everything" when we did not.
	if len(res.Files) != 0 {
		t.Fatalf("Files = %d, want 0 — the request must be rejected, not truncated", len(res.Files))
	}
	if len(res.Failures) != 1 || res.Failures[0].Code != FailDownloadFailed {
		t.Fatalf("failures = %+v, want one download_failed", res.Failures)
	}
	if res.Failures[0].Index != requestLevelFailureIndex {
		t.Errorf("Index = %d, want %d (request-level)", res.Failures[0].Index, requestLevelFailureIndex)
	}
}

func TestFetchInboundMedia_SniffMismatchRejected(t *testing.T) {
	allowLoopback(t)
	// A ZIP served as image/png. The declared type is attacker-controlled and
	// persistMedia trusts MimeType verbatim to pick a routing path.
	zip := append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0}, 64)...)
	srv := serve(t, "image/png", zip)

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/evil.png", Filename: "evil.png"},
	})

	if len(res.Failures) != 1 || res.Failures[0].Code != FailMIMEDenied {
		t.Fatalf("failures = %+v, want one mime_denied", res.Failures)
	}
}

func TestFetchInboundMedia_RejectsDecompressionBomb(t *testing.T) {
	isolateTempDir(t)
	allowLoopback(t)
	before := countTempFiles(t)

	// A few hundred bytes on the wire declaring 30000x30000 — 3.6 GB of RGBA if
	// the agent's imaging.Open ever reached it.
	srv := serve(t, "image/png", bombPNG(t, 30000, 30000))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/bomb.png", Filename: "bomb.png"},
	})

	if len(res.Files) != 0 {
		t.Fatalf("Files = %d, want 0", len(res.Files))
	}
	if len(res.Failures) != 1 || res.Failures[0].Code != FailTooLarge {
		t.Fatalf("failures = %+v, want one too_large", res.Failures)
	}
	if after := countTempFiles(t); after != before {
		t.Errorf("temp files leaked: before=%d after=%d", before, after)
	}
}

func TestFetchInboundMedia_RejectsUndecodableImage(t *testing.T) {
	allowLoopback(t)
	// Bytes that sniff as octet-stream — the sniffer has no opinion, so the
	// family cross-check passes. The header gate is what refuses the file, which
	// matters because SanitizeImage fails OPEN: it logs and proceeds with the
	// original bytes under the caller's declared MIME.
	srv := serve(t, "image/png", bytes.Repeat([]byte{0x7f}, 256))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/not-really.png", Filename: "not-really.png"},
	})

	if len(res.Failures) != 1 || res.Failures[0].Code != FailMIMEDenied {
		t.Fatalf("failures = %+v, want one mime_denied", res.Failures)
	}
}

func TestFetchInboundMedia_LargeImageWithinPixelCapAccepted(t *testing.T) {
	allowLoopback(t)
	// 6000x4000 = 24 MP: an ordinary camera photo. The bomb gate must not
	// reject it.
	srv := serve(t, "image/png", bombPNG(t, 6000, 4000))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/photo.png", Filename: "photo.png"},
	})
	defer CleanupInboundMedia(res.Files)

	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures on a 24 MP image: %+v", res.Failures)
	}
}

// TestFetchInboundMedia_SourceURLIsRedacted pins that the signature never
// reaches the media tag. BuildMediaTags renders SourceURL into
// <media:image url="..."> which is prepended to the agent message, so a raw
// value would be sent to the LLM provider and persisted in session history —
// the same credential the audit row deliberately strips.
func TestFetchInboundMedia_SourceURLIsRedacted(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "image/png", tinyPNG(t))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/shot.png?X-Amz-Signature=deadbeefcafe", Filename: "shot.png"},
	})
	defer CleanupInboundMedia(res.Files)

	if len(res.Infos) != 1 {
		t.Fatalf("Infos = %d, want 1: %+v", len(res.Infos), res.Failures)
	}
	got := res.Infos[0].SourceURL
	if strings.Contains(got, "deadbeefcafe") || strings.Contains(got, "?") {
		t.Errorf("SourceURL retains the signature: %q", got)
	}
	if !strings.HasSuffix(got, "/shot.png") {
		t.Errorf("SourceURL = %q, want the path preserved", got)
	}

	tags := media.BuildMediaTags(res.Infos)
	if strings.Contains(tags, "deadbeefcafe") {
		t.Errorf("media tag carries the signature into the prompt: %q", tags)
	}
}

func TestFetchInboundMedia_EmptyURL(t *testing.T) {
	res := FetchInboundMedia(context.Background(), []InboundMediaItem{{URL: "   "}})

	// Reported as a download failure rather than an SSRF block: nothing was
	// resolved, so calling it an SSRF block would mislead the caller.
	if len(res.Failures) != 1 || res.Failures[0].Code != FailDownloadFailed {
		t.Fatalf("failures = %+v, want one download_failed", res.Failures)
	}
}

// TestBlockedDialIsIdentifiableByErrorsIs proves the sentinel survives the
// net.OpError and url.Error wrappers, which is what lets the fetcher report a
// redirect into a private range as ssrf instead of a generic failure. The old
// substring match on "ssrf:" would break silently if the message were reworded;
// this breaks loudly.
func TestBlockedDialIsIdentifiableByErrorsIs(t *testing.T) {
	// No loopback bypass — it disables every blocked range, including this one.
	hc := security.NewRedirectFollowingSafeClient(5*time.Second, maxInboundRedirects)
	_, err := hc.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected the link-local dial to be refused")
	}
	if !errors.Is(err, security.ErrBlockedDial) {
		t.Fatalf("errors.Is(err, security.ErrBlockedDial) = false for %v", err)
	}
	// And the stripped form must not carry the request URL.
	if stripped := stripURLEnvelope(err); strings.Contains(stripped.Error(), "meta-data") {
		t.Errorf("stripURLEnvelope left the URL in place: %v", stripped)
	}
}

func TestFetchInboundMedia_NoItems(t *testing.T) {
	res := FetchInboundMedia(context.Background(), nil)
	if len(res.Files) != 0 || len(res.Failures) != 0 {
		t.Fatalf("empty input produced %+v", res)
	}
}

// ---- cleanup ----

func TestCleanupInboundMedia_Idempotent(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "image/png", tinyPNG(t))

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/a.png", Filename: "a.png"},
	})
	if len(res.Files) != 1 {
		t.Fatalf("Files = %d, want 1", len(res.Files))
	}
	path := res.Files[0].Path

	CleanupInboundMedia(res.Files)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still present after cleanup: %v", err)
	}
	CleanupInboundMedia(res.Files) // must not panic
	CleanupInboundMedia(nil)       // must not panic
}

// ---- severity ordering ----

func TestWorstFailure_SeverityOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []InboundMediaFailure
		want InboundMediaFailureCode
	}{
		{"ssrf beats mime_denied", []InboundMediaFailure{
			{Index: 0, Code: FailMIMEDenied}, {Index: 1, Code: FailSSRF}}, FailSSRF},
		{"mime_denied beats too_large", []InboundMediaFailure{
			{Index: 0, Code: FailTooLarge}, {Index: 1, Code: FailMIMEDenied}}, FailMIMEDenied},
		{"too_large beats download_failed", []InboundMediaFailure{
			{Index: 0, Code: FailDownloadFailed}, {Index: 1, Code: FailTooLarge}}, FailTooLarge},
		{"download_failed beats budget_exhausted", []InboundMediaFailure{
			{Index: 0, Code: FailBudgetExhausted}, {Index: 1, Code: FailDownloadFailed}}, FailDownloadFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forward := WorstFailure(tc.in)
			reversed := WorstFailure([]InboundMediaFailure{tc.in[1], tc.in[0]})
			if forward.Code != tc.want || reversed.Code != tc.want {
				t.Fatalf("forward=%q reversed=%q, want %q", forward.Code, reversed.Code, tc.want)
			}
		})
	}

	if got := WorstFailure(nil); got.Code != "" {
		t.Errorf("WorstFailure(nil).Code = %q, want empty", got.Code)
	}
}

// TestInboundMediaFailure_HasNoFreeText pins the closed-enum contract.
//
// Someone will eventually want to add a Reason string "just for debugging".
// SSRF errors embed the hostname and the resolved internal IP, and url.Error
// embeds the caller's full URL plus "connection refused" vs "i/o timeout" — any
// of which turns this endpoint into a port-scanning oracle once it reaches a
// prompt or a response body. The detail belongs in slog, which callers cannot
// read.
func TestInboundMediaFailure_HasNoFreeText(t *testing.T) {
	typ := reflect.TypeOf(InboundMediaFailure{})
	if typ.NumField() != 2 {
		t.Fatalf("InboundMediaFailure has %d fields, want exactly 2 (Index, Code)", typ.NumField())
	}
	want := map[string]reflect.Type{
		"Index": reflect.TypeOf(0),
		"Code":  reflect.TypeOf(InboundMediaFailureCode("")),
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		exp, ok := want[f.Name]
		if !ok {
			t.Fatalf("unexpected field %q: failure detail must stay a closed enum", f.Name)
		}
		if f.Type != exp {
			t.Errorf("field %q has type %v, want %v", f.Name, f.Type, exp)
		}
	}
}

// ---- byte budget ----

func TestByteBudget_ReserveCommitRelease(t *testing.T) {
	b := &byteBudget{remaining: 100, held: make(map[string]int64)}

	if !b.reserve(60) {
		t.Fatal("first reserve of 60/100 should succeed")
	}
	if b.reserve(60) {
		t.Fatal("second reserve of 60 with 40 left should fail")
	}

	// Commit less than reserved: the remainder goes back to the pool.
	b.commit("/tmp/a", 60, 10)
	if b.remaining != 90 {
		t.Fatalf("remaining = %d, want 90 (100 - 10 held)", b.remaining)
	}

	// Releasing the path frees exactly what was held, and is safe twice — this
	// is what makes CleanupInboundMedia idempotent.
	b.releasePath("/tmp/a")
	if b.remaining != 100 {
		t.Fatalf("remaining = %d, want 100", b.remaining)
	}
	b.releasePath("/tmp/a")
	b.releasePath("/tmp/never-seen")
	if b.remaining != 100 {
		t.Fatalf("remaining = %d after redundant releases, want 100", b.remaining)
	}

	// An uncommitted reservation is handed back whole.
	b.reserve(40)
	b.release(40)
	if b.remaining != 100 {
		t.Fatalf("remaining = %d, want 100", b.remaining)
	}
}

func TestFetchInboundMedia_BudgetExhausted(t *testing.T) {
	allowLoopback(t)
	srv := serve(t, "image/png", tinyPNG(t))

	// Drain the shared budget, then restore it so later tests are unaffected.
	if !inboundBudget.reserve(inboundMediaByteBudget) {
		t.Fatal("budget was not full at test start")
	}
	defer inboundBudget.release(inboundMediaByteBudget)

	res := FetchInboundMedia(context.Background(), []InboundMediaItem{
		{URL: srv.URL + "/a.png", Filename: "a.png"},
	})

	if len(res.Files) != 0 {
		t.Fatalf("Files = %d, want 0", len(res.Files))
	}
	if len(res.Failures) != 1 || res.Failures[0].Code != FailBudgetExhausted {
		t.Fatalf("failures = %+v, want one budget_exhausted", res.Failures)
	}
}

// ---- mime helpers ----

func TestResolveInboundMime(t *testing.T) {
	cases := []struct {
		respCT, filename, want string
	}{
		{"image/png", "x.png", "image/png"},
		{"image/PNG; charset=binary", "x.bin", "image/png"},
		{"", "x.png", "image/png"},
		{"application/octet-stream", "x.jpg", "image/jpeg"},
		{"", "", "application/octet-stream"},
	}
	for _, tc := range cases {
		if got := resolveInboundMime(tc.respCT, tc.filename); got != tc.want {
			t.Errorf("resolveInboundMime(%q, %q) = %q, want %q", tc.respCT, tc.filename, got, tc.want)
		}
	}
}

func TestSniffCompatible(t *testing.T) {
	cases := []struct {
		declared, sniffed string
		want              bool
	}{
		{"image/png", "image/png", true},
		{"image/jpeg", "image/png", true},                // same family; the header gate settles images
		{"image/png", "application/zip", false},          // the case this exists for
		{"application/pdf", "application/zip", false},    // family match proves nothing inside application/*
		{"application/pdf", "application/x-gzip", false}, //
		{"image/png", "application/octet-stream", true},  // sniffer has no opinion
		{"audio/ogg", "application/ogg", true},           // Go names every Ogg container application/ogg
		{"application/pdf", "application/pdf", true},     //
		{"video/mp4", "text/html; charset=utf-8", false}, // never
	}
	for _, tc := range cases {
		if got := sniffCompatible(tc.declared, parseMIMEValue(tc.sniffed)); got != tc.want {
			t.Errorf("sniffCompatible(%q, %q) = %v, want %v", tc.declared, tc.sniffed, got, tc.want)
		}
	}
}

// zeroReader is an endless source of zero bytes for the size-cap test.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
