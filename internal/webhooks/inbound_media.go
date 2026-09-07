package webhooks

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "image/gif"  // register GIF decoder for the dimension gate
	_ "image/jpeg" // register JPEG decoder for the dimension gate
	_ "image/png"  // register PNG decoder for the dimension gate

	_ "golang.org/x/image/webp" // register WebP decoder for the dimension gate

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	media "github.com/nextlevelbuilder/goclaw/internal/channels/media"
	mediastore "github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/security"
)

const (
	// MaxInboundMediaItems caps fan-out of HTTP fetches per request. Mirrors
	// bitrix24's maxInboundFiles.
	MaxInboundMediaItems = 10

	// inboundMediaMaxBytes matches the outbound /message cap so both directions
	// of the webhook API agree on one number.
	inboundMediaMaxBytes = 25 * 1024 * 1024

	// inboundDownloadTimeout bounds one fetch, covering all redirect hops.
	inboundDownloadTimeout = 5 * time.Minute

	// maxInboundRedirects caps redirect hops. Each hop's resolved IP is
	// re-validated at dial time by the safe client.
	maxInboundRedirects = 5

	// inboundMediaSniffLen is how many leading bytes are buffered for
	// http.DetectContentType, which never looks past 512.
	inboundMediaSniffLen = 512

	// inboundMediaMaxPixels rejects a declared image whose header dimensions
	// would blow up on decode. The agent's SanitizeImage calls imaging.Open —
	// a full decode to RGBA at 4 bytes per pixel — before it checks any size,
	// so a header claiming 30000x30000 costs 3.6 GB per file. At this cap the
	// worst case is ~200 MB per image, and inboundMediaByteBudget bounds how
	// many requests can be in flight holding files at once.
	inboundMediaMaxPixels = 50_000_000

	// inboundMediaByteBudget bounds total on-disk bytes held by in-flight
	// webhook media across the whole process.
	//
	// The per-webhook rate limiter does NOT bound this: allow() returns true
	// immediately when rpm <= 0 (webhooks_ratelimit.go), and WebhookData
	// .RateLimitPerMin has no non-zero default. Unbounded, the worst case is
	// the tenant tier's 600 rpm x 10 items x 25 MB = 150 GB. 512 MB admits
	// two concurrent worst-case requests; past that, callers get 503.
	inboundMediaByteBudget = 512 * 1024 * 1024

	// requestLevelFailureIndex marks a failure that belongs to the request as a
	// whole rather than to one item — currently only the item-count cap.
	requestLevelFailureIndex = -1
)

// AllowedMediaMIMETypes is the single source of truth for which Content-Type
// values the webhook API accepts, inbound and outbound. internal/http reads it
// directly; do not add a second copy there.
var AllowedMediaMIMETypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/webp":      true,
	"video/mp4":       true,
	"audio/mpeg":      true,
	"audio/ogg":       true,
	"application/pdf": true,
}

// InboundMediaItem is one caller-supplied attachment on POST /v1/webhooks/llm.
//
// There is deliberately no Caption field: MediaInfo has nowhere to put one and
// BuildMediaTags has no branch that would emit it, so a caption would be
// accepted, plumbed, and never read. Callers already have `input`.
type InboundMediaItem struct {
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
}

// InboundMediaFailureCode is a CLOSED enum. It is the only failure detail that
// may cross the API boundary or enter an LLM prompt.
type InboundMediaFailureCode string

const (
	FailSSRF            InboundMediaFailureCode = "ssrf"
	FailTooLarge        InboundMediaFailureCode = "too_large"
	FailMIMEDenied      InboundMediaFailureCode = "mime_denied"
	FailDownloadFailed  InboundMediaFailureCode = "download_failed"
	FailBudgetExhausted InboundMediaFailureCode = "budget_exhausted"
)

// InboundMediaFailure records one item that could not be fetched.
//
// There is deliberately NO free-text reason field. SSRF errors embed the
// hostname and the resolved internal IP, and url.Error embeds the full caller
// URL plus "connection refused" vs "i/o timeout". Any of that reaching a prompt
// or a response body turns this endpoint into an internal-network and
// port-scanning oracle. Detail goes to slog only.
//
// Index is the caller's array position, or requestLevelFailureIndex (-1) when
// the failure is about the request rather than one item.
type InboundMediaFailure struct {
	Index int
	Code  InboundMediaFailureCode
}

// InboundMediaResult carries everything the callers need.
//
// Files and Infos are always both empty or both populated with matching order.
// They are nil whenever Failures is non-empty — see FetchInboundMedia.
type InboundMediaResult struct {
	Files    []bus.MediaFile   // -> agent.RunRequest.Media
	Infos    []media.MediaInfo // -> media.BuildMediaTags
	Failures []InboundMediaFailure
}

// failureSeverity ranks codes so the HTTP status a caller sees never depends on
// which array slot happened to fail first. Higher wins.
//
// budget_exhausted ranks lowest on purpose: it is the one transient,
// server-side, retryable code. If any item is also genuinely bad, the request
// is bad on retry too and must not be reported as a 503.
var failureSeverity = map[InboundMediaFailureCode]int{
	FailSSRF:            4,
	FailMIMEDenied:      3,
	FailTooLarge:        2,
	FailDownloadFailed:  1,
	FailBudgetExhausted: 0,
}

// WorstFailure returns the failure ranking highest in the fixed severity order
// ssrf > mime_denied > too_large > download_failed > budget_exhausted. Returns
// the zero value for an empty slice.
func WorstFailure(fs []InboundMediaFailure) InboundMediaFailure {
	var worst InboundMediaFailure
	best := -1
	for _, f := range fs {
		if rank, ok := failureSeverity[f.Code]; ok && rank > best {
			best, worst = rank, f
		}
	}
	return worst
}

// byteBudget bounds total on-disk bytes held by in-flight inbound media.
//
// A fetch reserves the worst case before downloading, then commits the actual
// bytes written and hands the difference back. The committed amount stays held,
// keyed by temp path, until CleanupInboundMedia removes that file — which is
// what makes this a disk bound rather than a download-concurrency bound: files
// live from fetch time until the agent run finishes, which can be minutes.
type byteBudget struct {
	mu        sync.Mutex
	remaining int64
	held      map[string]int64
}

var inboundBudget = &byteBudget{
	remaining: inboundMediaByteBudget,
	held:      make(map[string]int64),
}

// reserve takes n bytes, reporting false when the budget cannot cover it.
func (b *byteBudget) reserve(n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.remaining < n {
		return false
	}
	b.remaining -= n
	return true
}

// commit converts a reservation into an amount held against path, returning the
// unused remainder to the pool.
func (b *byteBudget) commit(path string, reserved, actual int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remaining += reserved - actual
	b.held[path] += actual
}

// left reports the unreserved bytes, for diagnostics only.
func (b *byteBudget) left() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining
}

// release hands back an uncommitted reservation.
func (b *byteBudget) release(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.remaining += n
}

// releasePath hands back whatever is held against path. Safe on a path that was
// never committed or was already released, which is what makes
// CleanupInboundMedia idempotent.
func (b *byteBudget) releasePath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n, ok := b.held[path]; ok {
		b.remaining += n
		delete(b.held, path)
	}
}

// FetchInboundMedia downloads every item to its own temp file.
//
// ALL-OR-NOTHING: on any failure it removes whatever it already wrote and
// returns Files == nil with the full Failures list. Partial success was
// dropped because this is a machine-to-machine API — the chat-channel
// precedent it was modelled on annotates the message for a human to read, and
// there is no human here.
//
// Every item is attempted even after one fails. Bailing on the first failure
// would make the caller's HTTP status depend on array order.
func FetchInboundMedia(ctx context.Context, items []InboundMediaItem) InboundMediaResult {
	var res InboundMediaResult
	if len(items) == 0 {
		return res
	}
	if len(items) > MaxInboundMediaItems {
		// Reject rather than truncate: silent truncation reads as "we handled
		// everything" when we did not, and all-or-nothing makes rejecting the
		// consistent choice anyway.
		slog.Warn("webhook.media.too_many_items",
			"count", len(items), "cap", MaxInboundMediaItems)
		res.Failures = append(res.Failures, InboundMediaFailure{
			Index: requestLevelFailureIndex,
			Code:  FailDownloadFailed,
		})
		return res
	}

	// One client shared across items. NOT security.NewSafeClient: that one sets
	// CheckRedirect to http.ErrUseLastResponse, so a presigned or CDN URL that
	// 3xx-redirects yields a 0-byte body with no error at all. This client
	// follows redirects and re-validates the resolved destination IP at every
	// hop's dial, which also closes DNS rebinding.
	hc := security.NewRedirectFollowingSafeClient(inboundDownloadTimeout, maxInboundRedirects)

	for i, item := range items {
		file, info, code := fetchOneInboundMedia(ctx, hc, item)
		if code != "" {
			res.Failures = append(res.Failures, InboundMediaFailure{Index: i, Code: code})
			continue
		}
		res.Files = append(res.Files, file)
		res.Infos = append(res.Infos, info)
	}

	if len(res.Failures) > 0 {
		CleanupInboundMedia(res.Files)
		res.Files = nil
		res.Infos = nil
	}
	return res
}

// fetchOneInboundMedia downloads a single item. It returns an empty code on
// success; on failure it leaves nothing on disk and holds no budget.
func fetchOneInboundMedia(ctx context.Context, hc *http.Client, item InboundMediaItem) (bus.MediaFile, media.MediaInfo, InboundMediaFailureCode) {
	if strings.TrimSpace(item.URL) == "" {
		slog.Warn("webhook.media.empty_url")
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	safeURL := security.RedactURL(item.URL)

	// Cheap pre-flight. The per-hop dial check inside hc is the real guard;
	// this catches the obvious cases without opening a connection.
	if _, _, err := security.Validate(item.URL); err != nil {
		slog.Warn("security.webhook.ssrf_blocked", "url", safeURL, "error", err)
		return bus.MediaFile{}, media.MediaInfo{}, FailSSRF
	}

	if !inboundBudget.reserve(inboundMediaMaxBytes) {
		slog.Warn("webhook.media.budget_exhausted",
			"url", safeURL,
			"budget", int64(inboundMediaByteBudget),
			"remaining", inboundBudget.left(),
			"want", int64(inboundMediaMaxBytes))
		return bus.MediaFile{}, media.MediaInfo{}, FailBudgetExhausted
	}
	committed := false
	var tmpPath string
	defer func() {
		if !committed {
			inboundBudget.release(inboundMediaMaxBytes)
			if tmpPath != "" {
				os.Remove(tmpPath)
			}
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		slog.Warn("webhook.media.request_build_failed", "url", safeURL, "error", err)
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	resp, err := hc.Do(req)
	if err != nil {
		// A destination blocked at dial time surfaces here, from the initial
		// request or from any redirect hop.
		if errors.Is(err, security.ErrBlockedDial) {
			slog.Warn("security.webhook.ssrf_blocked", "url", safeURL, "error", stripURLEnvelope(err))
			return bus.MediaFile{}, media.MediaInfo{}, FailSSRF
		}
		slog.Warn("webhook.media.download_failed", "url", safeURL, "error", stripURLEnvelope(err))
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("webhook.media.download_failed", "url", safeURL, "status", resp.StatusCode)
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}

	mimeType := resolveInboundMime(resp.Header.Get("Content-Type"), item.Filename)

	// Buffer the leading bytes so the declared type can be cross-checked before
	// anything is written. The declared Content-Type is attacker-controlled and
	// persistMedia trusts MimeType verbatim to decide routing.
	head := make([]byte, inboundMediaSniffLen)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		slog.Warn("webhook.media.download_failed", "url", safeURL, "error", stripURLEnvelope(err))
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	head = head[:n]

	sniffed := parseMIMEValue(http.DetectContentType(head))
	if !sniffCompatible(mimeType, sniffed) {
		slog.Warn("webhook.media.mime_sniff_mismatch",
			"url", safeURL, "declared", mimeType, "sniffed", sniffed)
		return bus.MediaFile{}, media.MediaInfo{}, FailMIMEDenied
	}

	if !AllowedMediaMIMETypes[mimeType] {
		slog.Warn("webhook.media.mime_denied", "url", safeURL, "mime", mimeType)
		return bus.MediaFile{}, media.MediaInfo{}, FailMIMEDenied
	}

	ext := mediastore.ExtFromMime(mimeType)
	if ext == "" {
		ext = filepath.Ext(item.Filename)
	}
	tmp, err := os.CreateTemp("", "goclaw_webhook_*"+ext)
	if err != nil {
		slog.Warn("webhook.media.temp_create_failed", "url", safeURL, "error", err)
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	tmpPath = tmp.Name()

	// +1 is how "hit the cap and more was available" is detected.
	body := io.MultiReader(bytes.NewReader(head), resp.Body)
	written, err := io.Copy(tmp, io.LimitReader(body, inboundMediaMaxBytes+1))
	tmp.Close()
	if err != nil {
		slog.Warn("webhook.media.write_failed", "url", safeURL, "error", stripURLEnvelope(err))
		return bus.MediaFile{}, media.MediaInfo{}, FailDownloadFailed
	}
	if written > inboundMediaMaxBytes {
		slog.Warn("webhook.media.too_large", "url", safeURL, "max", int64(inboundMediaMaxBytes))
		return bus.MediaFile{}, media.MediaInfo{}, FailTooLarge
	}

	if strings.HasPrefix(mimeType, "image/") {
		if code := checkImageDimensions(tmpPath, mimeType, safeURL); code != "" {
			return bus.MediaFile{}, media.MediaInfo{}, code
		}
	}

	inboundBudget.commit(tmpPath, inboundMediaMaxBytes, written)
	committed = true

	slog.Info("webhook.media.downloaded",
		"url", safeURL, "bytes", written, "mime", mimeType)

	return bus.MediaFile{
			Path:     tmpPath,
			MimeType: mimeType,
			Filename: item.Filename,
		}, media.MediaInfo{
			Type:     media.MediaKindFromMime(mimeType),
			FilePath: tmpPath,
			// Redacted, not raw. BuildMediaTags renders this into
			// <media:image url="..."> which is prepended to the agent message,
			// so it is sent to the LLM provider and persisted in session
			// history. A presigned URL's signature is a bearer credential; the
			// path is the only part the model can use, and read_image works
			// from the local file, not from this URL.
			SourceURL:   security.RedactURL(item.URL),
			ContentType: mimeType,
			FileName:    item.Filename,
			FileSize:    written,
		}, ""
}

// checkImageDimensions reads only the image header. It rejects a file that does
// not decode as an image at all, and one whose pixel count would make the
// agent's full decode a memory bomb.
//
// It runs after the file is written rather than off the 512-byte sniff buffer
// because a JPEG's SOF marker can sit past a large EXIF block. DecodeConfig
// still reads only the header — it never allocates the pixel buffer, which is
// the allocation this gate exists to prevent.
//
// Rejecting a non-decodable image here is what keeps a bogus file off the agent
// path: SanitizeImage fails OPEN, logging "sanitize image failed, using
// original" and proceeding with the original bytes under the attacker's
// declared MIME. That fall-through is fine for authenticated channels and is
// not fine for a webhook caller, so the boundary refuses the file instead.
func checkImageDimensions(path, mimeType, safeURL string) InboundMediaFailureCode {
	f, err := os.Open(path)
	if err != nil {
		slog.Warn("webhook.media.image_open_failed", "url", safeURL, "error", err)
		return FailDownloadFailed
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		slog.Warn("webhook.media.image_undecodable", "url", safeURL, "mime", mimeType, "error", err)
		return FailMIMEDenied
	}
	if int64(cfg.Width)*int64(cfg.Height) > inboundMediaMaxPixels {
		slog.Warn("webhook.media.image_too_many_pixels",
			"url", safeURL, "width", cfg.Width, "height", cfg.Height, "max_pixels", int64(inboundMediaMaxPixels))
		return FailTooLarge
	}
	return ""
}

// CleanupInboundMedia removes temp files produced by FetchInboundMedia and
// returns their bytes to the budget. Safe on a nil slice and safe to call
// twice.
func CleanupInboundMedia(files []bus.MediaFile) {
	for _, f := range files {
		if f.Path == "" {
			continue
		}
		if err := os.Remove(f.Path); err != nil && !os.IsNotExist(err) {
			slog.Warn("webhook.media.cleanup_failed", "path", f.Path, "error", err)
		}
		inboundBudget.releasePath(f.Path)
	}
}

// resolveInboundMime prefers the response Content-Type over a filename guess.
// Without this, an extension-less CDN URL lands as application/octet-stream,
// gets typed as a document, and is silently skipped by vision.
func resolveInboundMime(respContentType, filename string) string {
	if ct := parseMIMEValue(respContentType); ct != "" && ct != "application/octet-stream" {
		return ct
	}
	if filename != "" {
		if detected := media.DetectMIMEType(filename); detected != "" {
			return detected
		}
	}
	return "application/octet-stream"
}

// parseMIMEValue strips parameters and lowercases a Content-Type value.
func parseMIMEValue(ct string) string {
	if ct == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
}

// sniffCompatible reports whether the sniffed content family can plausibly be
// the declared type.
//
// The comparison is on family, not exact string, because servers legitimately
// serve a JPEG as image/pjpeg and Go sniffs to its own canonical names. An
// empty or octet-stream sniff means the sniffer had no opinion, which is not
// evidence of a lie — for images the DecodeConfig gate settles it instead.
func sniffCompatible(declared, sniffed string) bool {
	if sniffed == "" || sniffed == "application/octet-stream" {
		return true
	}
	if declared == sniffed {
		return true
	}
	// Go reports every Ogg container as application/ogg, including audio ones.
	if declared == "audio/ogg" && sniffed == "application/ogg" {
		return true
	}
	// Inside application/*, a family match proves nothing: ZIP, gzip and wasm
	// all sniff as application/something, and an exact match was already ruled
	// out above. Only application/pdf is allowlisted in that family, so a ZIP
	// declared as a PDF would otherwise sail through the cross-check.
	if mimeFamily(declared) == "application" {
		return false
	}
	return mimeFamily(declared) == mimeFamily(sniffed)
}

func mimeFamily(mimeType string) string {
	if i := strings.IndexByte(mimeType, '/'); i > 0 {
		return mimeType[:i]
	}
	return mimeType
}

// stripURLEnvelope removes the *url.Error wrapper before an error is logged.
//
// url.Error.Error() renders as `%s %q: %s` with the full request URL in the
// middle - query string included. On a presigned URL that text IS the
// credential, so logging the error verbatim would undo the redaction applied to
// the adjacent url field. The wrapped error keeps the useful part (timeout,
// refused, blocked dial) without the URL.
func stripURLEnvelope(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
