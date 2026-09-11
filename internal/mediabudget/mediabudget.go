// Package mediabudget estimates the token cost of media payloads that are
// uploaded out-of-band to a model provider (read_video, read_audio,
// read_document), so they never appear as bytes in the request the token
// counter otherwise sees.
//
// Only two kinds of number are used here: published per-unit rates, and
// published provider limits. Nothing is derived from a payload's byte size
// except the one case that genuinely is bytes, a non-PDF document, and even
// there the result is capped by a provider limit. A byte size carries no
// information about a duration: 120 seconds of static image fitted in 20,627
// bytes on the machine this policy was measured on, so any bytes-per-second
// figure is a guess that can be off by orders of magnitude in either
// direction. Where a real measurement is unavailable the payload is either
// charged a proven provider ceiling or refused outright.
package mediabudget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	videoTokensPerSecond = 263
	audioTokensPerSecond = 32
	pdfTokensPerPage     = 258

	// maxPDFPages is Gemini's published PDF limit. It bounds what this package
	// CHARGES for a document, not what a provider bills for one.
	maxPDFPages            = 1000
	maxPDFTokensPerPayload = maxPDFPages * pdfTokensPerPage

	// pdfSniffWindow: readers accept the %PDF- header anywhere in the first 1024 bytes.
	pdfSniffWindow = 1024

	// maxPageScanBytes bounds the page-object scan so one charge cannot become a long read.
	maxPageScanBytes = 32 << 20

	// documentBytesPerToken is the standard rough text ratio, an approximation
	// rather than a bound. The proven half of the non-PDF document charge is
	// maxPDFTokensPerPayload above it.
	documentBytesPerToken = 4

	probeTimeout = 5 * time.Second

	// maxProbeSeconds: ffprobe parses "inf" as +Inf, so reject any duration past this before it reaches token arithmetic.
	maxProbeSeconds = 1e7 // about 115 days

	// maxTokenEstimate: clamp so overflowed or implausible arithmetic never surfaces as a negative charge (read as free by a budget gate).
	maxTokenEstimate = 1_000_000_000
)

// HeadBytes and TailBytes are the number of leading and trailing bytes a
// caller should read from a remote payload before building a Payload, so
// Tokens has enough of the container to probe.
const (
	HeadBytes = 524288
	TailBytes = 524288
)

// ErrDurationUnmeasurable is the parent of every reason a video or audio
// payload could not be measured, so a caller matches the class with errors.Is
// while the operator still reads the specific remedy below.
//
// Refusing rather than charging a ceiling preserves a guarantee that predates
// this package: unverifiable native media was already refused wholesale, for
// every URL. This narrows that refusal to what genuinely cannot be measured
// instead of removing it. A PDF is charged a ceiling instead because its page
// count is cheaply measurable and that ceiling fits an ordinary window.
var ErrDurationUnmeasurable = errors.New("media duration could not be determined")

// ErrProbeUnavailable means this host has no probe binary to measure with.
var ErrProbeUnavailable = fmt.Errorf("%w: ffprobe (from the ffmpeg package) is not installed, so this tool cannot enforce its token budget", ErrDurationUnmeasurable)

// ErrNoProbeableBytes means the probe exists but the source yielded nothing to measure.
var ErrNoProbeableBytes = fmt.Errorf("%w: the source provided no bytes that could be probed, so its duration is unknowable here", ErrDurationUnmeasurable)

// Kind names the media class a payload belongs to. The tool that reads the
// payload knows this; deriving it from a caller-supplied MIME would let a
// mislabelled video be charged on the far cheaper document tier. The zero
// value is the strictest tier, so a Kind left unset cannot under-charge.
type Kind int

const (
	KindVideo Kind = iota
	KindAudio
	KindDocument
)

func (k Kind) String() string {
	switch k {
	case KindAudio:
		return "audio"
	case KindDocument:
		return "document"
	default:
		return "video"
	}
}

// Payload describes one out-of-band media payload whose cost must be estimated.
type Payload struct {
	Kind Kind   // media class; the zero value is video
	MIME string // e.g. "video/mp4"; for a document it is the fallback when no bytes can be sniffed
	Size int64  // total payload size in bytes, always known
	Path string // path to a complete local file, or "" when the payload is remote
	Head []byte // leading bytes of a remote payload, the whole file for a document already in memory, or nil
	Tail []byte // trailing bytes of a remote payload, or nil
}

// Tokens charges the tokens a provider will bill for one payload, never less
// than 1 for a non-empty one.
//
// Video and audio are charged from a measured duration or not at all. A PDF is
// charged from a measured page count, or from the 1000-page document maximum
// the provider itself enforces. Any other document is charged as text and
// capped by that same maximum.
func Tokens(ctx context.Context, p Payload) (int, error) {
	if p.Size <= 0 {
		return 0, nil
	}

	if p.Kind == KindDocument {
		return documentTokens(ctx, p), nil
	}

	rate := int64(videoTokensPerSecond)
	if p.Kind == KindAudio {
		rate = audioTokensPerSecond
	}

	d, err := probeSeconds(ctx, p)
	if err != nil {
		return 0, fmt.Errorf("%s budget: %w", p.Kind, err)
	}
	seconds, ok := sanitizeSeconds(d.Seconds())
	if !ok {
		return 0, fmt.Errorf("%s budget: %w", p.Kind, ErrNoProbeableBytes)
	}
	return clampTokens(int64(math.Ceil(seconds)) * rate), nil
}

// documentTokens never fails: unlike a duration, a document has a ceiling the
// provider publishes and enforces, so an unmeasurable one can be charged that
// ceiling instead of being refused. At 258,000 tokens it still fits the kind of
// window an agent that reads documents is normally given.
func documentTokens(ctx context.Context, p Payload) int {
	if !isPDFPayload(p, documentSegments(p, pdfSniffWindow)) {
		// docx, pptx, xlsx and friends carry neither a duration nor a page
		// count, so they are charged as the text they mostly are. The cap is
		// what this package will charge, not a promise about what a provider
		// bills for them.
		return clampTokens(min(ceilDiv(p.Size, documentBytesPerToken), maxPDFTokensPerPayload))
	}

	pages := 0
	if probed, ok := probePages(ctx, p); ok {
		pages = probed
	}
	// pdfinfo trusts the page tree root's /Count, which a crafted file sets to
	// 1 while carrying a thousand real pages. Both halves count something real,
	// so taking the larger can only raise the charge toward what the provider
	// will bill.
	if scanned := countPageObjects(documentSegments(p, maxPageScanBytes)); scanned > pages {
		pages = scanned
	}
	if pages <= 0 {
		return clampTokens(maxPDFTokensPerPayload)
	}
	return clampTokens(int64(min(pages, maxPDFPages)) * pdfTokensPerPage)
}

// isPDFPayload sniffs the %PDF- magic rather than trusting p.MIME, which is
// caller-supplied and decides which pricing tier applies: the same file
// labelled application/octet-stream would otherwise be charged as loose text.
// The label is consulted only when no bytes are available to sniff.
func isPDFPayload(p Payload, segments [][]byte) bool {
	if len(segments) == 0 || len(segments[0]) == 0 {
		return isPDF(p.MIME)
	}
	head := segments[0]
	if int64(len(head)) > pdfSniffWindow {
		head = head[:pdfSniffWindow]
	}
	return bytes.Contains(head, []byte("%PDF-"))
}

func isPDF(mime string) bool {
	return mime == "application/pdf"
}

// documentSegments returns the payload bytes available for inspection here,
// without any network trip: a remote payload carries samples in Head/Tail, a
// local one is read from disk up to limit.
func documentSegments(p Payload, limit int64) [][]byte {
	if len(p.Head) > 0 || len(p.Tail) > 0 {
		head := capBytes(p.Head, limit)
		if int64(len(p.Head)) >= p.Size {
			// Head already holds the whole payload; adding Tail would count
			// the same page objects a second time.
			return [][]byte{head}
		}
		return [][]byte{head, capBytes(p.Tail, limit)}
	}
	if p.Path == "" {
		return nil
	}
	f, err := os.Open(p.Path)
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil || len(data) == 0 {
		return nil
	}
	return [][]byte{data}
}

func capBytes(b []byte, limit int64) []byte {
	if int64(len(b)) > limit {
		return b[:limit]
	}
	return b
}

// pdfPageObjectPattern matches the /Type /Page entry of a page object. PDF
// allows any whitespace, or none, between the two names.
var pdfPageObjectPattern = regexp.MustCompile(`/Type[\x00\t\n\f\r ]*/Page`)

// countPageObjects counts the page objects visible in the bytes at hand. It is
// a floor and never a bound: pages living in a compressed object stream are
// invisible to it, which is why it only ever raises a probed count.
func countPageObjects(segments [][]byte) int {
	total := 0
	for _, seg := range segments {
		for _, loc := range pdfPageObjectPattern.FindAllIndex(seg, -1) {
			// A name token runs on until a delimiter, so "/Pages" (the page
			// tree node, not a page) and any other longer name must not count.
			if loc[1] < len(seg) && isPDFNameChar(seg[loc[1]]) {
				continue
			}
			total++
		}
	}
	return total
}

func isPDFNameChar(b byte) bool {
	switch b {
	case 0, '\t', '\n', '\f', '\r', ' ', '(', ')', '<', '>', '[', ']', '{', '}', '/', '%':
		return false
	}
	return true
}

// ceilDiv avoids the n+d-1 overflow that a Size near math.MaxInt64 triggers.
func ceilDiv(n, d int64) int64 {
	q := n / d
	if n%d != 0 {
		q++
	}
	return q
}

// sanitizeSeconds rejects a duration no real media file would have, whether
// from a crafted ffprobe output or a bogus probe implementation.
func sanitizeSeconds(seconds float64) (float64, bool) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 || seconds > maxProbeSeconds {
		return 0, false
	}
	return seconds, true
}

func clampTokens(n int64) int {
	if n < 1 {
		n = 1
	}
	if n > maxTokenEstimate {
		n = maxTokenEstimate
	}
	return int(n)
}

// DurationProbe measures the duration of a local media file.
type DurationProbe func(ctx context.Context, path string) (time.Duration, bool)

// PageProbe counts the pages of a local PDF file.
type PageProbe func(ctx context.Context, path string) (int, bool)

// probeSet is swapped atomically rather than field by field so a test in
// another package cannot race a concurrent Tokens call under -race.
type probeSet struct {
	duration      DurationProbe
	durationReady func() bool
	pages         PageProbe
	pagesReady    func() bool
}

var activeProbes atomic.Pointer[probeSet]

func init() {
	activeProbes.Store(&probeSet{
		duration:      ffprobeDuration,
		durationReady: ffprobeInstalled,
		pages:         pdfinfoPageCount,
		pagesReady:    pdfinfoInstalled,
	})
}

// SetProbesForTest replaces the external-binary probes, so no test anywhere
// needs ffprobe or pdfinfo on PATH. A nil probe simulates a missing binary.
// It returns a function restoring the previous set.
func SetProbesForTest(duration DurationProbe, pages PageProbe) (restore func()) {
	previous := activeProbes.Load()
	next := &probeSet{
		duration:      duration,
		durationReady: func() bool { return duration != nil },
		pages:         pages,
		pagesReady:    func() bool { return pages != nil },
	}
	if duration == nil {
		next.duration = func(context.Context, string) (time.Duration, bool) { return 0, false }
	}
	if pages == nil {
		next.pages = func(context.Context, string) (int, bool) { return 0, false }
	}
	activeProbes.Store(next)
	return func() { activeProbes.Store(previous) }
}

// probeSeconds measures the duration of a payload, naming which of the two
// causes stopped it so the operator is pointed at the remedy that applies.
func probeSeconds(ctx context.Context, p Payload) (time.Duration, error) {
	probes := activeProbes.Load()
	path, cleanup, err := localFile(p, probes.durationReady)
	if err != nil {
		return 0, err
	}
	defer cleanup()
	d, ok := probes.duration(ctx, path)
	if !ok {
		return 0, ErrNoProbeableBytes
	}
	return d, nil
}

// probePages counts the pages of a PDF payload.
func probePages(ctx context.Context, p Payload) (int, bool) {
	probes := activeProbes.Load()
	path, cleanup, err := localFile(p, probes.pagesReady)
	if err != nil {
		return 0, false
	}
	defer cleanup()
	return probes.pages(ctx, path)
}

// localFile hands a probe a path on disk. A local payload is probed in place; a
// remote one is first materialised into a sparse temp file the probe can seek
// into. Both guards precede materialize: a temp file plus a forked process buys
// nothing with no sampled bytes to read, or with no binary to read them.
func localFile(p Payload, ready func() bool) (path string, cleanup func(), err error) {
	noop := func() {}
	if p.Path == "" && len(p.Head) == 0 && len(p.Tail) == 0 {
		return "", noop, ErrNoProbeableBytes
	}
	if !ready() {
		return "", noop, ErrProbeUnavailable
	}
	if p.Path != "" {
		return p.Path, noop, nil
	}

	path, cleanup, materializeErr := materialize(p)
	if materializeErr != nil {
		return "", noop, ErrNoProbeableBytes
	}
	return path, cleanup, nil
}

// materialize writes a remote payload's known head and tail bytes into a
// full-size sparse temp file. A plain file holding only a prefix of the
// payload makes ffprobe under-report duration: it clamps to the bytes it can
// see, and a 64 KB prefix of a 30-second WAV measured as 0.74 seconds.
// Sizing the file to the payload's true size with Truncate, and writing the
// head and tail at their correct offsets, restores the correct answer.
func materialize(p Payload) (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "goclaw-mediabudget-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }

	f, err := os.Create(filepath.Join(dir, "payload"))
	if err != nil {
		cleanup()
		return "", nil, err
	}

	if err := writePayload(f, p); err != nil {
		f.Close()
		cleanup()
		return "", nil, err
	}

	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}

	return f.Name(), cleanup, nil
}

// maxMaterializeBytes bounds the sparse file for a remote payload, whose declared Size is attacker-supplied and not guaranteed sparse-cheap on every filesystem or quota scheme. Tokens still charges from the probe result, not from this clamp.
const maxMaterializeBytes = 4 << 30 // 4 GiB, above any payload this system streams

func writePayload(f *os.File, p Payload) error {
	size := p.Size
	if size > maxMaterializeBytes {
		size = maxMaterializeBytes
	}
	if err := f.Truncate(size); err != nil {
		return err
	}

	head := p.Head
	if int64(len(head)) > size {
		head = head[:size]
	}
	if len(head) > 0 {
		if _, err := f.WriteAt(head, 0); err != nil {
			return err
		}
	}

	tail := p.Tail
	if int64(len(tail)) > size {
		tail = tail[int64(len(tail))-size:]
	}
	if len(tail) > 0 {
		if _, err := f.WriteAt(tail, size-int64(len(tail))); err != nil {
			return err
		}
	}

	return nil
}

// lookupOnce caches one exec.LookPath result, so a per-payload probe does not
// walk PATH on every call.
type lookupOnce struct {
	once  sync.Once
	name  string
	found bool
}

func (l *lookupOnce) installed() bool {
	l.once.Do(func() {
		_, err := exec.LookPath(l.name)
		l.found = err == nil
	})
	return l.found
}

var (
	ffprobeLookup = &lookupOnce{name: "ffprobe"}
	pdfinfoLookup = &lookupOnce{name: "pdfinfo"}
)

func ffprobeInstalled() bool { return ffprobeLookup.installed() }

func pdfinfoInstalled() bool { return pdfinfoLookup.installed() }

// ffprobeDuration shells out to ffprobe for the duration of a local file. The
// path must never be a URL: -protocol_whitelist file is defence in depth so
// even a malformed argument cannot make ffprobe open a network connection.
func ffprobeDuration(ctx context.Context, path string) (time.Duration, bool) {
	if !ffprobeInstalled() {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-protocol_whitelist", "file",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		hardenPath(path),
	)
	// Bound Wait() after a context kill even if a child inherits the stdout pipe.
	cmd.WaitDelay = time.Second

	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}

	parsed, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, false
	}
	seconds, ok := sanitizeSeconds(parsed)
	if !ok {
		return 0, false
	}

	return time.Duration(seconds * float64(time.Second)), true
}

// pdfinfoPageCount shells out to poppler's pdfinfo for the page count of a
// local PDF. An external parser is used rather than a byte scan because a page
// tree may live inside compressed object streams: pdfinfo reads 19 pages from
// the shared-mime-info-spec.pdf this was measured against, where a hand-rolled
// /Count scan over the same bytes finds nothing at all. Like ffprobeDuration
// the argument is a local path, never a URL, and it is run through argv rather
// than a shell.
func pdfinfoPageCount(ctx context.Context, path string) (int, bool) {
	if !pdfinfoInstalled() {
		return 0, false
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pdfinfo", hardenPath(path))
	cmd.WaitDelay = time.Second

	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	return parsePDFInfoPages(string(out))
}

// pdfInfoPagesPattern matches pdfinfo's own "Pages:" field. The field is named
// rather than taken as the first integer in the output, which would otherwise
// pick up a page size, a PDF version or a creation date.
var pdfInfoPagesPattern = regexp.MustCompile(`(?m)^Pages:[ \t]+(\d+)[ \t]*$`)

func parsePDFInfoPages(out string) (int, bool) {
	m := pdfInfoPagesPattern.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	pages, err := strconv.Atoi(m[1])
	if err != nil || pages <= 0 {
		return 0, false
	}
	return pages, true
}

// hardenPath stops a path starting with "-" from being parsed as an option by
// the probe binary.
func hardenPath(path string) string {
	if filepath.IsAbs(path) || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return path
	}
	return "./" + path
}
