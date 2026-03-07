package telegram

import (
	"context"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mymmrac/telego"
)

const (
	// defaultMediaMaxBytes is the default max download size (20MB, Telegram Bot API limit).
	defaultMediaMaxBytes int64 = 20 * 1024 * 1024

	// downloadMaxRetries is the number of download retry attempts.
	downloadMaxRetries = 3

	// docMaxChars is the max characters to extract from text documents (matching TS: 200K).
	docMaxChars = 200_000
)

// MediaInfo contains information about a downloaded media file.
type MediaInfo struct {
	Type        string // "image", "video", "audio", "voice", "document", "animation"
	FilePath    string // local file path after download (sanitized for images)
	FileID      string // Telegram file_id
	ContentType string // MIME type
	FileName    string // original filename
	FileSize    int64
	Transcript  string // STT transcript for audio/voice media (empty if not transcribed)
}

// resolveMedia extracts and downloads media from a Telegram message.
// Returns a list of MediaInfo for each media item found.
func (c *Channel) resolveMedia(ctx context.Context, msg *telego.Message) []MediaInfo {
	var results []MediaInfo

	maxBytes := c.config.MediaMaxBytes
	if maxBytes == 0 {
		maxBytes = defaultMediaMaxBytes
	}

	// Photo: take highest resolution (last element)
	if msg.Photo != nil && len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		filePath, err := c.downloadMedia(ctx, photo.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download photo", "file_id", photo.FileID, "error", err)
		} else {
			// Sanitize image for LLM vision
			sanitized, sanitizeErr := sanitizeImage(filePath)
			if sanitizeErr != nil {
				slog.Warn("failed to sanitize image, using original", "error", sanitizeErr)
				sanitized = filePath
			}
			results = append(results, MediaInfo{
				Type:        "image",
				FilePath:    sanitized,
				FileID:      photo.FileID,
				ContentType: "image/jpeg",
				FileSize:    int64(photo.FileSize),
			})
		}
	}

	// Video
	if msg.Video != nil {
		results = append(results, MediaInfo{
			Type:        "video",
			FileID:      msg.Video.FileID,
			ContentType: msg.Video.MimeType,
			FileName:    msg.Video.FileName,
			FileSize:    int64(msg.Video.FileSize),
		})
	}

	// Video Note (round video)
	if msg.VideoNote != nil {
		results = append(results, MediaInfo{
			Type:        "video",
			FileID:      msg.VideoNote.FileID,
			ContentType: "video/mp4",
			FileSize:    int64(msg.VideoNote.FileSize),
		})
	}

	// Animation (GIF)
	if msg.Animation != nil {
		results = append(results, MediaInfo{
			Type:        "animation",
			FileID:      msg.Animation.FileID,
			ContentType: msg.Animation.MimeType,
			FileName:    msg.Animation.FileName,
			FileSize:    int64(msg.Animation.FileSize),
		})
	}

	// Audio
	if msg.Audio != nil {
		filePath, err := c.downloadMedia(ctx, msg.Audio.FileID, maxBytes)
		if err != nil {
			slog.Warn("failed to download audio", "file_id", msg.Audio.FileID, "error", err)
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

	return results
}

// downloadMedia downloads a file from Telegram by file_id with retry logic.
// Returns the local file path.
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

	// Check file size before downloading
	if int64(file.FileSize) > maxBytes {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", file.FileSize, maxBytes)
	}

	// Build download URL
	downloadURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", c.config.Token, file.FilePath)

	resp, err := http.Get(downloadURL)
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

	// Copy with size limit
	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save file: %w", err)
	}
	if written > maxBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file exceeds max size during download: %d bytes", written)
	}

	return tmpFile.Name(), nil
}

// buildMediaTags generates content tags for media items (matching TS media placeholder format).
// For audio/voice items that have been transcribed, the transcript is embedded in a <transcript> block.
func buildMediaTags(mediaList []MediaInfo) string {
	var tags []string
	for _, m := range mediaList {
		switch m.Type {
		case "image":
			tags = append(tags, "<media:image>")
		case "video", "animation":
			tags = append(tags, "<media:video>")
		case "audio":
			if m.Transcript != "" {
				tags = append(tags, fmt.Sprintf("<media:audio>\n<transcript>%s</transcript>", html.EscapeString(m.Transcript)))
			} else {
				tags = append(tags, "<media:audio>")
			}
		case "voice":
			if m.Transcript != "" {
				tags = append(tags, fmt.Sprintf("<media:voice>\n<transcript>%s</transcript>", html.EscapeString(m.Transcript)))
			} else {
				tags = append(tags, "<media:voice>")
			}
		case "document":
			tags = append(tags, "<media:document>")
		}
	}
	return strings.Join(tags, "\n")
}

// --- Document Text Extraction ---

// textExtensions maps file extensions to MIME types for text files we can extract.
var textExtensions = map[string]string{
	".txt":  "text/plain",
	".md":   "text/markdown",
	".csv":  "text/csv",
	".tsv":  "text/tab-separated-values",
	".json": "application/json",
	".yaml": "text/yaml",
	".yml":  "text/yaml",
	".xml":  "text/xml",
	".log":  "text/plain",
	".ini":  "text/plain",
	".cfg":  "text/plain",
	".env":  "text/plain",
	".sh":   "text/x-shellscript",
	".py":   "text/x-python",
	".go":   "text/x-go",
	".js":   "text/javascript",
	".ts":   "text/typescript",
	".html": "text/html",
	".css":  "text/css",
	".sql":  "text/x-sql",
	".rs":   "text/x-rust",
	".java": "text/x-java",
	".c":    "text/x-c",
	".cpp":  "text/x-c++",
	".h":    "text/x-c",
	".rb":   "text/x-ruby",
	".php":  "text/x-php",
	".toml": "text/x-toml",
}

// extractDocumentContent reads a document file and returns its content wrapped in XML tags.
// For text files: extracts content, truncates at docMaxChars, wraps in <file> block.
// For binary files: returns a placeholder message.
// Ref: TS src/media-understanding/apply.ts → extractFileBlocks()
func extractDocumentContent(filePath, fileName string) (string, error) {
	if filePath == "" {
		return fmt.Sprintf("[File: %s — download failed]", fileName), nil
	}

	ext := strings.ToLower(filepath.Ext(fileName))
	mime, isText := textExtensions[ext]
	if !isText {
		return fmt.Sprintf("[File: %s — binary format not supported, only text files can be processed]", fileName), nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", fileName, err)
	}

	content := string(data)

	// Truncate if too long
	if len(content) > docMaxChars {
		content = content[:docMaxChars] + "\n... [truncated]"
	}

	// XML escape content to prevent injection
	escaped := html.EscapeString(content)

	return fmt.Sprintf("<file name=%q mime=%q>\n%s\n</file>", fileName, mime, escaped), nil
}
