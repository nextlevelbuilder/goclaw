package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mymmrac/telego"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	// defaultMediaMaxBytes is the default max download size for the official Bot API (20 MB).
	defaultMediaMaxBytes int64 = 20 * 1024 * 1024

	// localAPIDefaultMaxBytes is the default max download size when a local Bot API server
	// is configured. The local server supports up to 2 GB; we default to 200 MB and let
	// downstream providers enforce their own limits.
	localAPIDefaultMaxBytes int64 = 200 * 1024 * 1024

	// downloadMaxRetries is the number of download retry attempts.
	downloadMaxRetries = 3

	// stallTimeout is how long a download can receive zero bytes before being aborted.
	stallTimeout = 60 * time.Second
)

// errMediaTooLarge indicates a file exceeded the configured max download size.
var errMediaTooLarge = errors.New("file exceeds max size")

// MediaError records a media download failure with enough context for user/model feedback.
type MediaError struct {
	Type     string // "image", "video", "audio", "voice", "document", "animation"
	Reason   string // human-readable reason
	MaxBytes int64  // configured limit (0 if not a size error)
}

// MediaInfo is an alias for the shared media.MediaInfo type.
type MediaInfo = media.MediaInfo

// resolveMedia extracts and downloads media from a Telegram message.
// Returns successfully downloaded media and any download errors for feedback.
func (c *Channel) resolveMedia(ctx context.Context, msg *telego.Message) ([]MediaInfo, []MediaError) {
	var results []MediaInfo
	var mediaErrors []MediaError

	maxBytes := c.config.MediaMaxBytes
	if maxBytes == 0 {
		if c.config.APIServer != "" {
			maxBytes = localAPIDefaultMaxBytes
		} else {
			maxBytes = defaultMediaMaxBytes
		}
	}

	// Photo: take highest resolution (last element)
	if msg.Photo != nil && len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		filePath, err := c.downloadMedia(ctx, photo.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download photo", "file_id", photo.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("image", err, maxBytes))
		} else {
			// Pass raw file to agent loop — sanitization now happens at loop level.
			results = append(results, MediaInfo{
				Type:        "image",
				FilePath:    filePath,
				FileID:      photo.FileID,
				ContentType: "image/jpeg",
				FileSize:    int64(photo.FileSize),
			})
		}
	}

	// Video
	if msg.Video != nil {
		filePath, err := c.downloadMedia(ctx, msg.Video.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download video", "file_id", msg.Video.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("video", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "video",
				FilePath:    filePath,
				FileID:      msg.Video.FileID,
				ContentType: msg.Video.MimeType,
				FileName:    msg.Video.FileName,
				FileSize:    int64(msg.Video.FileSize),
			})
		}
	}

	// Video Note (round video)
	if msg.VideoNote != nil {
		filePath, err := c.downloadMedia(ctx, msg.VideoNote.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download video note", "file_id", msg.VideoNote.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("video", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "video",
				FilePath:    filePath,
				FileID:      msg.VideoNote.FileID,
				ContentType: "video/mp4",
				FileSize:    int64(msg.VideoNote.FileSize),
			})
		}
	}

	// Animation (GIF)
	if msg.Animation != nil {
		filePath, err := c.downloadMedia(ctx, msg.Animation.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download animation", "file_id", msg.Animation.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("animation", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "animation",
				FilePath:    filePath,
				FileID:      msg.Animation.FileID,
				ContentType: msg.Animation.MimeType,
				FileName:    msg.Animation.FileName,
				FileSize:    int64(msg.Animation.FileSize),
			})
		}
	}

	// Audio
	if msg.Audio != nil {
		filePath, err := c.downloadMedia(ctx, msg.Audio.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download audio", "file_id", msg.Audio.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("audio", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "audio",
				FilePath:    filePath,
				FileID:      msg.Audio.FileID,
				ContentType: msg.Audio.MimeType,
				FileName:    msg.Audio.FileName,
				FileSize:    int64(msg.Audio.FileSize),
			})
		}
	}

	// Voice
	if msg.Voice != nil {
		filePath, err := c.downloadMedia(ctx, msg.Voice.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download voice", "file_id", msg.Voice.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("voice", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "voice",
				FilePath:    filePath,
				FileID:      msg.Voice.FileID,
				ContentType: msg.Voice.MimeType,
				FileSize:    int64(msg.Voice.FileSize),
			})
		}
	}

	// Document
	if msg.Document != nil {
		filePath, err := c.downloadMedia(ctx, msg.Document.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download document", "file_id", msg.Document.FileID, "error", err)
			mediaErrors = append(mediaErrors, newMediaError("document", err, maxBytes))
		} else {
			results = append(results, MediaInfo{
				Type:        "document",
				FilePath:    filePath,
				FileID:      msg.Document.FileID,
				ContentType: msg.Document.MimeType,
				FileName:    msg.Document.FileName,
				FileSize:    int64(msg.Document.FileSize),
			})
		}
	}

	return results, mediaErrors
}

// downloadMedia downloads a file from Telegram by file_id with retry logic.
// Returns the local file path.
//
// When a local Bot API server is configured (api_server), the download URL
// points to that server instead of the official api.telegram.org, removing the
// standard 20 MB file size limit. Downstream providers enforce their own limits.
func (c *Channel) downloadMedia(ctx context.Context, fileID string, maxBytes int64) (string, error) {
	var file *telego.File
	var err error

	// Retry up to downloadMaxRetries times with exponential backoff
	for attempt := 1; attempt <= downloadMaxRetries; attempt++ {
		file, err = c.bot.GetFile(ctx, &telego.GetFileParams{FileID: fileID})
		if err == nil {
			break
		}
		if attempt < downloadMaxRetries {
			slog.Debug("retrying file download", "file_id", fileID, "attempt", attempt, "error", err)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
	}
	if err != nil {
		return "", fmt.Errorf("get file info after %d attempts: %w", downloadMaxRetries, err)
	}

	if file.FilePath == "" {
		return "", fmt.Errorf("empty file path for file_id %s", fileID)
	}

	// Check file size before downloading (FileSize may be 0 for large files on local Bot API).
	if file.FileSize > 0 && int64(file.FileSize) > maxBytes {
		return "", fmt.Errorf("%w: %d bytes (limit %d)", errMediaTooLarge, file.FileSize, maxBytes)
	}

	// Local Bot API (--local mode) returns absolute filesystem paths and does NOT
	// serve files over HTTP (/file/ endpoint returns 501). When the path is absolute,
	// copy directly from the filesystem (requires the data dir to be mounted).
	if c.config.APIServer != "" && filepath.IsAbs(file.FilePath) {
		if _, statErr := os.Stat(file.FilePath); statErr == nil {
			slog.Debug("telegram media: copying from local filesystem",
				"file_id", fileID, "path", file.FilePath, "size", file.FileSize)
			return copyLocalFile(file.FilePath, maxBytes)
		}
		return "", fmt.Errorf("local bot api file not accessible (mount the data dir into the container): %s", file.FilePath)
	}

	// Download over HTTP: use custom API server if configured (non-local mode),
	// otherwise the official Telegram API.
	var downloadURL string
	if c.config.APIServer != "" {
		downloadURL = fmt.Sprintf("%s/file/bot%s/%s",
			strings.TrimRight(c.config.APIServer, "/"), c.config.Token, file.FilePath)
	} else {
		downloadURL = fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.config.Token, file.FilePath)
	}

	// SSRF Protection: check the resolved URL before connecting.
	// We skip the check IF the host is our explicitly configured (trusted) API server.
	isTrusted := c.config.APIServer != "" && strings.HasPrefix(downloadURL, c.config.APIServer)
	if !isTrusted {
		if err := tools.CheckSSRF(downloadURL); err != nil {
			return "", fmt.Errorf("SSRF protection: %w", err)
		}
	}

	// Use a generous timeout for media downloads (large files via local Bot API
	// can be up to 200 MB). The shared httpClient has a 30s timeout suited for
	// API calls, so we override per-request with a dedicated context.
	dlCtx, dlCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer dlCancel()

	req, err := http.NewRequestWithContext(dlCtx, "GET", downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("create download request: %w", err)
	}

	// Clone the shared client without the 30s Timeout so the per-request
	// context (5 min) governs the download duration instead.
	dlClient := *c.httpClient
	dlClient.Timeout = 0

	resp, err := dlClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Determine extension from file path
	ext := filepath.Ext(file.FilePath)
	if ext == "" {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_media_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	// Wrap the response body with stall detection: abort if no data received for 60s.
	progressBody := newProgressReader(resp.Body, dlCancel, stallTimeout)
	defer progressBody.Stop()

	// Copy with size limit
	written, err := io.Copy(tmpFile, io.LimitReader(progressBody, maxBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save file: %w", err)
	}
	if written > maxBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("%w: %d bytes (limit %d)", errMediaTooLarge, written, maxBytes)
	}

	return tmpFile.Name(), nil
}

// copyLocalFile copies a file from the local Bot API data directory to a temp file.
func copyLocalFile(srcPath string, maxBytes int64) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", fmt.Errorf("open local file: %w", err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return "", fmt.Errorf("stat local file: %w", err)
	}
	if info.Size() > maxBytes {
		return "", fmt.Errorf("%w: %d bytes (limit %d)", errMediaTooLarge, info.Size(), maxBytes)
	}

	ext := filepath.Ext(srcPath)
	if ext == "" {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_media_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, src); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("copy local file: %w", err)
	}

	return tmpFile.Name(), nil
}

// buildMediaTags delegates to the shared media package.
func buildMediaTags(mediaList []MediaInfo) string {
	return media.BuildMediaTags(mediaList)
}

// extractDocumentContent delegates to the shared media package.
func extractDocumentContent(filePath, fileName string) (string, error) {
	return media.ExtractDocumentContent(filePath, fileName)
}

// lightweightMediaTags builds descriptive media placeholders from Telegram message metadata
// without downloading any files. Used for pending history recording when bot is not mentioned.
// Uses bracket notation (e.g. "[sent an image]") instead of XML tags to prevent LLMs from
// confusing context-history media with actionable current-message media (<media:image>).
func lightweightMediaTags(msg *telego.Message) string {
	var tags []string
	if msg.Photo != nil && len(msg.Photo) > 0 {
		tags = append(tags, "[sent an image]")
	}
	if msg.Video != nil {
		tags = append(tags, "[sent a video]")
	}
	if msg.VideoNote != nil {
		tags = append(tags, "[sent a video]")
	}
	if msg.Animation != nil {
		tags = append(tags, "[sent a video]")
	}
	if msg.Audio != nil {
		tags = append(tags, "[sent audio]")
	}
	if msg.Voice != nil {
		tags = append(tags, "[sent a voice message]")
	}
	if msg.Document != nil {
		name := msg.Document.FileName
		if name != "" {
			tags = append(tags, fmt.Sprintf("[sent a file: %s]", name))
		} else {
			tags = append(tags, "[sent a file]")
		}
	}
	if len(tags) == 0 {
		return ""
	}
	return strings.Join(tags, "\n")
}

// extractMediaRefs extracts lightweight media references (file_ids + sizes) from a Telegram
// message without downloading any files. Stored in HistoryEntry.MediaRefs for lazy download
// when the bot is later mentioned.
func extractMediaRefs(msg *telego.Message) []channels.MediaRef {
	var refs []channels.MediaRef
	if msg.Photo != nil && len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1] // highest resolution
		refs = append(refs, channels.MediaRef{Type: "image", FileID: photo.FileID, FileSize: int64(photo.FileSize)})
	}
	if msg.Video != nil {
		refs = append(refs, channels.MediaRef{Type: "video", FileID: msg.Video.FileID, FileSize: int64(msg.Video.FileSize)})
	}
	if msg.VideoNote != nil {
		refs = append(refs, channels.MediaRef{Type: "video", FileID: msg.VideoNote.FileID, FileSize: int64(msg.VideoNote.FileSize)})
	}
	if msg.Animation != nil {
		refs = append(refs, channels.MediaRef{Type: "animation", FileID: msg.Animation.FileID, FileSize: int64(msg.Animation.FileSize)})
	}
	if msg.Audio != nil {
		refs = append(refs, channels.MediaRef{Type: "audio", FileID: msg.Audio.FileID, FileSize: int64(msg.Audio.FileSize)})
	}
	if msg.Voice != nil {
		refs = append(refs, channels.MediaRef{Type: "voice", FileID: msg.Voice.FileID, FileSize: int64(msg.Voice.FileSize)})
	}
	if msg.Document != nil {
		refs = append(refs, channels.MediaRef{Type: "document", FileID: msg.Document.FileID, FileSize: int64(msg.Document.FileSize)})
	}
	return refs
}

// historyMediaMaxBytes is the max file size for deferred history media downloads.
// Caps large files (videos, big documents) to prevent slow mention handling.
const historyMediaMaxBytes int64 = 5 * 1024 * 1024 // 5 MB

// maxHistoryMediaRefs is the max number of deferred media refs to resolve per mention.
// Caps total download time — at worst ~2s each = ~30s for 15 files.
const maxHistoryMediaRefs = 15

// resolveMediaRefs downloads media from deferred file_id references stored in pending history.
// Used to resolve history media when the bot is mentioned.
// Caps at maxHistoryMediaRefs most-recent refs and skips files exceeding historyMediaMaxBytes.
func (c *Channel) resolveMediaRefs(ctx context.Context, refs []channels.MediaRef) ([]MediaInfo, []MediaError) {
	// Only resolve the most recent refs to avoid blocking mention handling.
	if len(refs) > maxHistoryMediaRefs {
		slog.Debug("telegram: capping history media refs",
			"total", len(refs), "cap", maxHistoryMediaRefs)
		refs = refs[len(refs)-maxHistoryMediaRefs:]
	}

	maxBytes := c.config.MediaMaxBytes
	if maxBytes == 0 {
		if c.config.APIServer != "" {
			maxBytes = localAPIDefaultMaxBytes
		} else {
			maxBytes = defaultMediaMaxBytes
		}
	}
	// Use the stricter of channel config and history cap.
	if historyMediaMaxBytes < maxBytes {
		maxBytes = historyMediaMaxBytes
	}

	// Batch timeout: abort remaining downloads if total time exceeds limit.
	batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var results []MediaInfo
	var errs []MediaError
	for _, ref := range refs {
		// Pre-flight size check — skip without downloading if known to exceed limit.
		if ref.FileSize > 0 && ref.FileSize > maxBytes {
			slog.Debug("telegram: skipping oversized history media ref",
				"type", ref.Type, "size", ref.FileSize, "max", maxBytes)
			errs = append(errs, MediaError{Type: ref.Type, Reason: "file too large for history resolve", MaxBytes: maxBytes})
			continue
		}
		filePath, err := c.downloadMedia(batchCtx, ref.FileID, maxBytes)
		if err != nil {
			// On batch timeout, stop processing remaining refs.
			if batchCtx.Err() != nil {
				slog.Warn("telegram: history media batch timeout, skipping remaining",
					"resolved", len(results), "remaining", len(refs))
				break
			}
			slog.Warn("telegram: history media ref download failed",
				"type", ref.Type, "file_id", ref.FileID, "error", err)
			errs = append(errs, newMediaError(ref.Type, err, maxBytes))
			continue
		}
		results = append(results, MediaInfo{
			Type:     ref.Type,
			FilePath: filePath,
			FileID:   ref.FileID,
		})
	}
	return results, errs
}

// lightweightTagForType returns the single lightweight tag that matches a given media type
// within a Telegram message. Used for targeted replacement when a specific media fails.
func lightweightTagForType(mediaType string, msg *telego.Message) string {
	switch mediaType {
	case "image":
		if msg.Photo != nil && len(msg.Photo) > 0 {
			return "[sent an image]"
		}
	case "video":
		if msg.Video != nil || msg.VideoNote != nil {
			return "[sent a video]"
		}
	case "animation":
		if msg.Animation != nil {
			return "[sent a video]"
		}
	case "audio":
		if msg.Audio != nil {
			return "[sent audio]"
		}
	case "voice":
		if msg.Voice != nil {
			return "[sent a voice message]"
		}
	case "document":
		if msg.Document != nil {
			if msg.Document.FileName != "" {
				return fmt.Sprintf("[sent a file: %s]", msg.Document.FileName)
			}
			return "[sent a file]"
		}
	}
	return ""
}

// newMediaError builds a MediaError from a download error, detecting size-limit failures.
func newMediaError(mediaType string, err error, maxBytes int64) MediaError {
	me := MediaError{Type: mediaType}
	if errors.Is(err, errMediaTooLarge) {
		me.Reason = fmt.Sprintf("exceeds %d MB limit", maxBytes/(1024*1024))
		me.MaxBytes = maxBytes
	} else {
		me.Reason = "download failed"
	}
	return me
}

// progressReader wraps an io.Reader and cancels a context if no data is
// received within a specified timeout. Used to detect mid-stream stalls.
type progressReader struct {
	io.Reader
	cancel context.CancelFunc
	timer  *time.Timer
	d      time.Duration
}

func newProgressReader(r io.Reader, cancel context.CancelFunc, d time.Duration) *progressReader {
	pr := &progressReader{
		Reader: r,
		cancel: cancel,
		d:      d,
	}
	pr.timer = time.AfterFunc(d, func() {
		slog.Warn("telegram media: download stalled, aborting", "timeout", d)
		cancel()
	})
	return pr
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	if n > 0 {
		pr.timer.Reset(pr.d)
	}
	return n, err
}

func (pr *progressReader) Stop() {
	if pr.timer != nil {
		pr.timer.Stop()
	}
}
