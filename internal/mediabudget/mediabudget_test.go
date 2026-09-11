package mediabudget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubProbes replaces both external binaries for one test, so no test here
// needs ffprobe or pdfinfo on PATH. A nil probe means "binary missing".
func stubProbes(t *testing.T, duration DurationProbe, pages PageProbe) {
	t.Helper()
	t.Cleanup(SetProbesForTest(duration, pages))
}

func constantDuration(d time.Duration) DurationProbe {
	return func(context.Context, string) (time.Duration, bool) { return d, true }
}

func constantPages(n int) PageProbe {
	return func(context.Context, string) (int, bool) { return n, true }
}

func mustTokens(t *testing.T, p Payload) int {
	t.Helper()
	got, err := Tokens(context.Background(), p)
	if err != nil {
		t.Fatalf("Tokens() returned error: %v", err)
	}
	return got
}

func TestTokens_VideoChargesMeasuredDuration(t *testing.T) {
	stubProbes(t, constantDuration(40*time.Second), nil)

	got := mustTokens(t, Payload{Kind: KindVideo, MIME: "video/mp4", Size: 1, Path: "/does/not/matter"})
	if want := 40 * videoTokensPerSecond; got != want {
		t.Fatalf("Tokens() = %d, want %d", got, want)
	}
}

func TestTokens_AudioChargesMeasuredDuration(t *testing.T) {
	stubProbes(t, constantDuration(120*time.Second), nil)

	got := mustTokens(t, Payload{Kind: KindAudio, MIME: "audio/mpeg", Size: 1, Path: "/does/not/matter"})
	if want := 120 * audioTokensPerSecond; got != want {
		t.Fatalf("Tokens() = %d, want %d", got, want)
	}
}

func TestTokens_FractionalSecondsRoundUp(t *testing.T) {
	stubProbes(t, constantDuration(1200*time.Millisecond), nil)

	got := mustTokens(t, Payload{Kind: KindVideo, MIME: "video/mp4", Size: 1, Path: "/does/not/matter"})
	if want := 2 * videoTokensPerSecond; got != want {
		t.Fatalf("Tokens() = %d, want %d (1.2s must round up to 2s, not down to 1s)", got, want)
	}
}

// A byte size says nothing about a duration in either direction, so an
// unmeasurable clip is refused with a fix the operator can act on rather than
// charged some number derived from its size.
func TestTokens_UnmeasurableVideoIsRefused(t *testing.T) {
	stubProbes(t, nil, nil)

	_, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 200_000_000, Head: []byte("bytes")})
	if !errors.Is(err, ErrDurationUnmeasurable) {
		t.Fatalf("Tokens() error = %v, want ErrDurationUnmeasurable", err)
	}
	if !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("error %q does not name the missing probe", err)
	}
	if !strings.Contains(err.Error(), "video") {
		t.Fatalf("error %q does not name the media kind", err)
	}
}

func TestTokens_UnmeasurableAudioIsRefused(t *testing.T) {
	stubProbes(t, nil, nil)

	_, err := Tokens(context.Background(), Payload{Kind: KindAudio, MIME: "audio/mpeg", Size: 20_000, Head: []byte("bytes")})
	if !errors.Is(err, ErrDurationUnmeasurable) {
		t.Fatalf("Tokens() error = %v, want ErrDurationUnmeasurable", err)
	}
	if !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("error %q does not name the missing probe", err)
	}
	if !strings.Contains(err.Error(), "audio") {
		t.Fatalf("error %q does not name the media kind", err)
	}
}

// A tiny clip is refused on exactly the same terms as a large one: the policy
// charges measurements, and a small file is no more measurable than a big one.
func TestTokens_UnmeasurableTinyVideoIsRefusedToo(t *testing.T) {
	stubProbes(t, nil, nil)

	if _, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 20_000, Head: []byte("bytes")}); !errors.Is(err, ErrDurationUnmeasurable) {
		t.Fatalf("Tokens() error = %v, want ErrDurationUnmeasurable", err)
	}
}

// The zero value of Kind must be the strictest tier, so a payload built
// without one cannot slip onto the cheap document charge.
func TestTokens_ZeroValueKindIsChargedAsVideo(t *testing.T) {
	stubProbes(t, constantDuration(10*time.Second), nil)

	got := mustTokens(t, Payload{Size: 1, Path: "/does/not/matter"})
	if want := 10 * videoTokensPerSecond; got != want {
		t.Fatalf("Tokens() with an unset Kind = %d, want %d (the video rate)", got, want)
	}
}

func TestTokens_NonPositiveSizeReturnsZero(t *testing.T) {
	stubProbes(t, nil, nil)

	for _, size := range []int64{0, -1, -100} {
		got, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: size})
		if err != nil {
			t.Fatalf("Tokens() with Size=%d returned error: %v", size, err)
		}
		if got != 0 {
			t.Fatalf("Tokens() with Size=%d = %d, want 0", size, got)
		}
	}
}

func TestTokens_PDFChargesMeasuredPageCount(t *testing.T) {
	stubProbes(t, nil, constantPages(19))

	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: 149_000, Path: "/does/not/matter"})
	if want := 19 * pdfTokensPerPage; got != want {
		t.Fatalf("Tokens() = %d, want %d", got, want)
	}
}

// A measured count past the provider limit still stops at the limit: the
// provider refuses the extra pages, so they can never be billed.
func TestTokens_PDFPagesCappedAtProviderLimit(t *testing.T) {
	stubProbes(t, nil, constantPages(5000))

	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: 5 << 20, Path: "/does/not/matter"})
	if want := maxPDFTokensPerPayload; got != want {
		t.Fatalf("Tokens() with 5000 measured pages = %d, want %d", got, want)
	}
}

// Unlike a duration, a document has a published ceiling, so an unmeasurable
// one is charged that ceiling instead of being refused.
func TestTokens_UnmeasurablePDFChargesProviderMaximum(t *testing.T) {
	stubProbes(t, nil, nil)

	for _, size := range []int64{5 << 20, 500 << 20, math.MaxInt64} {
		got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: size})
		if got != maxPDFTokensPerPayload {
			t.Fatalf("Tokens() for a %d-byte unmeasurable PDF = %d, want %d", size, got, maxPDFTokensPerPayload)
		}
	}
}

// docx, pptx and friends have neither a duration nor pages, so they are priced
// as the text they mostly are and bounded by the document maximum.
func TestTokens_NonPDFDocumentChargedAsText(t *testing.T) {
	stubProbes(t, nil, nil)

	const size = int64(400_000)
	const docxMIME = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

	got := mustTokens(t, Payload{Kind: KindDocument, MIME: docxMIME, Size: size})
	if want := int(size / documentBytesPerToken); got != want {
		t.Fatalf("Tokens() for a 400KB docx = %d, want %d", got, want)
	}
	if got >= maxPDFTokensPerPayload {
		t.Fatalf("Tokens() = %d, want well under the document maximum %d", got, maxPDFTokensPerPayload)
	}
}

func TestTokens_NonPDFDocumentCappedAtDocumentMaximum(t *testing.T) {
	stubProbes(t, nil, nil)

	for _, size := range []int64{100 << 20, math.MaxInt64} {
		got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/octet-stream", Size: size})
		if got != maxPDFTokensPerPayload {
			t.Fatalf("Tokens() for a %d-byte document = %d, want the document maximum %d", size, got, maxPDFTokensPerPayload)
		}
	}
}

// A remote payload that sampled no bytes has nothing for a probe to read, so
// neither probe may build a full-size sparse file and fork for it.
func TestProbes_RemotePayloadWithoutBytesSkipsMaterialize(t *testing.T) {
	durationCalls, pageCalls := 0, 0
	stubProbes(t,
		func(context.Context, string) (time.Duration, bool) { durationCalls++; return 30 * time.Second, true },
		func(context.Context, string) (int, bool) { pageCalls++; return 19, true },
	)

	if _, err := probeSeconds(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 50 << 20}); !errors.Is(err, ErrNoProbeableBytes) {
		t.Fatalf("probeSeconds() for a payload carrying no bytes: err = %v, want ErrNoProbeableBytes", err)
	}
	if _, ok := probePages(context.Background(), Payload{Kind: KindDocument, MIME: "application/pdf", Size: 50 << 20}); ok {
		t.Fatal("probePages() reported a page count for a payload carrying no bytes")
	}
	if durationCalls != 0 || pageCalls != 0 {
		t.Fatalf("probes ran %d/%d times for a payload carrying no bytes, want 0/0", durationCalls, pageCalls)
	}
}

// A missing binary must be detected before a temp file is created, so the
// probe is never reached for a remote payload it could not read anyway.
func TestProbes_MissingBinarySkipsMaterialize(t *testing.T) {
	stubProbes(t, nil, nil)

	if _, err := probeSeconds(context.Background(), Payload{Kind: KindVideo, Size: 1 << 20, Head: []byte("HEAD")}); !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("probeSeconds() with no ffprobe available: err = %v, want ErrProbeUnavailable", err)
	}
	if _, ok := probePages(context.Background(), Payload{Kind: KindDocument, MIME: "application/pdf", Size: 1 << 20, Head: []byte("%PDF")}); ok {
		t.Fatal("probePages() succeeded with no pdfinfo available")
	}
}

func TestTokens_RemotePayloadMaterializesSparseFileAndCleansUp(t *testing.T) {
	const size = int64(1 << 20)
	head := []byte("HEADHEADHEAD")
	tail := []byte("TAILTAILTAIL")

	var inspectedDir string
	stubProbes(t, func(ctx context.Context, path string) (time.Duration, bool) {
		inspectedDir = filepath.Dir(path)

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat temp file: %v", err)
		}
		if info.Size() != size {
			t.Fatalf("temp file size = %d, want %d", info.Size(), size)
		}

		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open temp file: %v", err)
		}
		defer f.Close()

		gotHead := make([]byte, len(head))
		if _, err := f.ReadAt(gotHead, 0); err != nil {
			t.Fatalf("read head: %v", err)
		}
		if string(gotHead) != string(head) {
			t.Fatalf("temp file head = %q, want %q", gotHead, head)
		}

		gotTail := make([]byte, len(tail))
		if _, err := f.ReadAt(gotTail, size-int64(len(tail))); err != nil {
			t.Fatalf("read tail: %v", err)
		}
		if string(gotTail) != string(tail) {
			t.Fatalf("temp file tail = %q, want %q", gotTail, tail)
		}

		return 10 * time.Second, true
	}, nil)

	got := mustTokens(t, Payload{Kind: KindVideo, MIME: "video/mp4", Size: size, Head: head, Tail: tail})
	if want := 10 * videoTokensPerSecond; got != want {
		t.Fatalf("Tokens() = %d, want %d", got, want)
	}

	if inspectedDir == "" {
		t.Fatal("probe stub was never called")
	}
	if _, err := os.Stat(inspectedDir); !os.IsNotExist(err) {
		t.Fatalf("temp dir %q still exists after Tokens returned: err=%v", inspectedDir, err)
	}
}

// A stubbed probe returning +Inf must never turn into a negative token count:
// ffprobe itself can report "inf" for a crafted duration field.
func TestTokens_ProbeReturningInfiniteDurationIsRejected(t *testing.T) {
	stubProbes(t, constantDuration(time.Duration(math.Inf(1))), nil)

	_, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 1, Path: "/does/not/matter"})
	if !errors.Is(err, ErrDurationUnmeasurable) {
		t.Fatalf("Tokens() with an infinite probe duration: err = %v, want ErrDurationUnmeasurable", err)
	}
}

// A stubbed probe claiming 1e15 seconds overflows time.Duration's nanosecond
// range; the result must be a refusal, never a wrapped-around charge.
func TestTokens_ProbeReturningAbsurdDurationIsRejected(t *testing.T) {
	var absurdSeconds float64 = 1e15
	stubProbes(t, constantDuration(time.Duration(absurdSeconds*float64(time.Second))), nil)

	_, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 1, Path: "/does/not/matter"})
	if !errors.Is(err, ErrDurationUnmeasurable) {
		t.Fatalf("Tokens() with a 1e15s probe duration: err = %v, want ErrDurationUnmeasurable", err)
	}
}

func TestSanitizeSeconds_RejectsInfAndAbsurdSeconds(t *testing.T) {
	cases := []struct {
		name    string
		seconds float64
	}{
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
		{"NaN", math.NaN()},
		{"past maxProbeSeconds", maxProbeSeconds + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := sanitizeSeconds(tc.seconds); ok {
				t.Fatalf("sanitizeSeconds(%v) = ok, want rejected", tc.seconds)
			}
		})
	}
}

// pdfinfo prints several integers before and after the page count, so the
// field has to be matched by name rather than by position.
func TestParsePDFInfoPages_ReadsTheNamedField(t *testing.T) {
	const out = `Title:           shared mime info spec
Creator:         DocBook XSL Stylesheets V1.79.2
Producer:        Apache FOP Version 2.8
CreationDate:    Mon Jun  3 11:22:33 2024 UTC
Custom Metadata: no
Pages:           19
Encrypted:       no
Page size:       595.276 x 841.89 pts (A4)
PDF version:     1.4
`
	pages, ok := parsePDFInfoPages(out)
	if !ok {
		t.Fatal("parsePDFInfoPages() found no page count")
	}
	if pages != 19 {
		t.Fatalf("parsePDFInfoPages() = %d, want 19", pages)
	}
}

func TestParsePDFInfoPages_RejectsOutputWithoutTheField(t *testing.T) {
	for _, out := range []string{"", "Encrypted: no\n", "Pages:\n", "Pages:           0\n"} {
		if pages, ok := parsePDFInfoPages(out); ok {
			t.Fatalf("parsePDFInfoPages(%q) = %d, want rejected", out, pages)
		}
	}
}

func TestMaterialize_ClampsOversizedHeadAndTail(t *testing.T) {
	cases := []struct {
		name string
		size int64
		head []byte
		tail []byte
	}{
		{"head longer than size", 4, []byte("HELLOWORLD"), nil},
		{"tail longer than size", 4, nil, []byte("HELLOWORLD")},
		{"head and tail overlap and together exceed size", 6, []byte("ABCDEFGH"), []byte("WXYZ")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, cleanup, err := materialize(Payload{Size: tc.size, Head: tc.head, Tail: tc.tail})
			if err != nil {
				t.Fatalf("materialize() returned error: %v", err)
			}
			defer cleanup()

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat temp file: %v", err)
			}
			if info.Size() != tc.size {
				t.Fatalf("temp file size = %d, want %d", info.Size(), tc.size)
			}
		})
	}
}

// A URL payload's Size is attacker-supplied and can name a file far larger
// than any that exists, so the sparse temp file must stay bounded.
func TestMaterialize_ClampsHugeDeclaredSize(t *testing.T) {
	const declared = int64(100 << 40) // 100 TB, far beyond maxMaterializeBytes

	path, cleanup, err := materialize(Payload{Size: declared})
	if err != nil {
		t.Fatalf("materialize() returned error: %v", err)
	}
	info, statErr := os.Stat(path)
	cleanup()
	if statErr != nil {
		t.Fatalf("stat temp file: %v", statErr)
	}
	if info.Size() != maxMaterializeBytes {
		t.Fatalf("temp file size = %d, want exactly the clamp %d", info.Size(), maxMaterializeBytes)
	}
}

// craftedPDF builds a PDF whose page tree root understates its own size: the
// /Count says 1 while the file carries pageObjects real page objects. pdfinfo
// reports the /Count, ghostscript reports the objects.
func craftedPDF(pageObjects int) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n1 0 obj\n<< /Type /Pages /Count 1 /Kids [] >>\nendobj\n")
	for i := range pageObjects {
		fmt.Fprintf(&b, "%d 0 obj\n<< /Type /Page /Parent 1 0 R >>\nendobj\n", i+2)
	}
	b.WriteString("trailer\n<< /Root 1 0 R >>\n%%EOF\n")
	return b.Bytes()
}

// A crafted /Count must not buy a thousand pages at the price of one.
func TestTokens_CraftedPageCountIsRaisedByTheObjectScan(t *testing.T) {
	stubProbes(t, nil, constantPages(1))

	data := craftedPDF(1000)
	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: int64(len(data)), Head: data})
	if got != maxPDFTokensPerPayload {
		t.Fatalf("Tokens() for a PDF claiming 1 page and carrying 1000 = %d, want %d", got, maxPDFTokensPerPayload)
	}
}

// The scan reads a local file too, where the whole payload is on disk rather
// than sampled into Head.
func TestTokens_CraftedPageCountIsScannedFromALocalFile(t *testing.T) {
	stubProbes(t, nil, constantPages(1))

	path := filepath.Join(t.TempDir(), "crafted.pdf")
	if err := os.WriteFile(path, craftedPDF(400), 0o600); err != nil {
		t.Fatal(err)
	}

	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: 1, Path: path})
	if want := 400 * pdfTokensPerPage; got != want {
		t.Fatalf("Tokens() = %d, want %d (400 page objects on disk)", got, want)
	}
}

// "/Type /Pages" is the page tree node, not a page, and must never be counted.
func TestTokens_PageTreeNodeIsNotCountedAsAPage(t *testing.T) {
	stubProbes(t, nil, constantPages(3))

	data := []byte("%PDF-1.7\n<< /Type /Pages /Count 3 >>\n<< /Type/Pages >>\n")
	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: int64(len(data)), Head: data})
	if want := 3 * pdfTokensPerPage; got != want {
		t.Fatalf("Tokens() = %d, want %d (the probed count, unchanged by page tree nodes)", got, want)
	}
}

// The MIME label is caller-supplied and would otherwise pick the pricing tier,
// so a PDF labelled application/octet-stream is sniffed and charged as a PDF.
func TestTokens_MislabelledPDFIsStillChargedAsAPDF(t *testing.T) {
	stubProbes(t, nil, constantPages(1))

	data := craftedPDF(1000)
	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/octet-stream", Size: int64(len(data)), Head: data})
	if got != maxPDFTokensPerPayload {
		t.Fatalf("Tokens() for a mislabelled 1000-page PDF = %d, want %d", got, maxPDFTokensPerPayload)
	}
}

// The opposite label lies the same way: bytes that are plainly not a PDF are
// charged as the text they are, whatever the caller called them.
func TestTokens_NonPDFBytesLabelledPDFAreChargedAsText(t *testing.T) {
	stubProbes(t, nil, constantPages(1000))

	data := []byte(strings.Repeat("plain text, no header here. ", 64))
	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: int64(len(data)), Head: data})
	if want := ceilDiv(int64(len(data)), documentBytesPerToken); got != int(want) {
		t.Fatalf("Tokens() = %d, want %d (the text tier)", got, want)
	}
}

// With nothing to sniff, the label is all there is, and a PDF label keeps the
// PDF ceiling rather than falling to the cheaper text tier.
func TestTokens_LabelDecidesTheTierWhenNoBytesAreAvailable(t *testing.T) {
	stubProbes(t, nil, nil)

	got := mustTokens(t, Payload{Kind: KindDocument, MIME: "application/pdf", Size: 5 << 20})
	if got != maxPDFTokensPerPayload {
		t.Fatalf("Tokens() = %d, want the PDF ceiling %d", got, maxPDFTokensPerPayload)
	}
}

// The two causes of an unmeasurable duration have different remedies, so the
// error must not send an operator to install a binary that is already there.
func TestTokens_UnmeasurableCausesNameTheirOwnRemedy(t *testing.T) {
	stubProbes(t, nil, nil)
	_, err := Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 20_000, Path: "/does/not/matter"})
	if !errors.Is(err, ErrProbeUnavailable) {
		t.Fatalf("with no probe binary: err = %v, want ErrProbeUnavailable", err)
	}

	stubProbes(t, constantDuration(30*time.Second), nil)
	_, err = Tokens(context.Background(), Payload{Kind: KindVideo, MIME: "video/mp4", Size: 1_900_000_000})
	if !errors.Is(err, ErrNoProbeableBytes) {
		t.Fatalf("with a probe but no bytes: err = %v, want ErrNoProbeableBytes", err)
	}
	if strings.Contains(err.Error(), "not installed") {
		t.Fatalf("error %q blames a missing binary that is present", err)
	}
}
