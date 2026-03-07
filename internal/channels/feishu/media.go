package feishu

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// downloadMessageResource downloads a message attachment (image, file, audio, video, sticker).
// Uses the im.messageResource.get API — the primary API for inbound media.
func (c *Channel) downloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string) ([]byte, string, error) {
	return c.client.DownloadMessageResource(ctx, messageID, fileKey, resourceType)
}

// --- Image upload ---

// uploadImage uploads an image and returns the image_key for use in messages.
func (c *Channel) uploadImage(ctx context.Context, data io.Reader) (string, error) {
	return c.client.UploadImage(ctx, data)
}

// --- File upload ---

// uploadFile uploads a file and returns the file_key.
func (c *Channel) uploadFile(ctx context.Context, data io.Reader, fileName, fileType string, durationMs int) (string, error) {
	return c.client.UploadFile(ctx, data, fileName, fileType, durationMs)
}

// --- Send media ---

// sendImage sends an image message using an image_key.
func (c *Channel) sendImage(ctx context.Context, chatID, receiveIDType, imageKey string) error {
	content := fmt.Sprintf(`{"image_key":"%s"}`, imageKey)
	_, err := c.client.SendMessage(ctx, receiveIDType, chatID, "image", content)
	if err != nil {
		return fmt.Errorf("feishu send image: %w", err)
	}
	return nil
}

// sendFile sends a file message using a file_key.
// msgType: "file" for documents, "media" for audio/video.
func (c *Channel) sendFile(ctx context.Context, chatID, receiveIDType, fileKey, msgType string) error {
	if msgType == "" {
		msgType = "file"
	}
	content := fmt.Sprintf(`{"file_key":"%s"}`, fileKey)
	_, err := c.client.SendMessage(ctx, receiveIDType, chatID, msgType, content)
	if err != nil {
		return fmt.Errorf("feishu send file: %w", err)
	}
	return nil
}

// --- Outbound media ---

// sendMediaAttachment uploads and sends a media attachment (image or file).
func (c *Channel) sendMediaAttachment(ctx context.Context, chatID, receiveIDType string, media bus.MediaAttachment) error {
	filePath := media.URL
	if filePath == "" {
		return nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open media file %s: %w", filePath, err)
	}
	defer f.Close()

	ct := media.ContentType
	if isImageContentType(ct) {
		imageKey, err := c.uploadImage(ctx, f)
		if err != nil {
			return fmt.Errorf("upload image: %w", err)
		}
		return c.sendImage(ctx, chatID, receiveIDType, imageKey)
	}

	// Everything else: upload as file
	fileName := filepath.Base(filePath)
	fileType := detectFileType(fileName)
	fileKey, err := c.uploadFile(ctx, f, fileName, fileType, 0)
	if err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	return c.sendFile(ctx, chatID, receiveIDType, fileKey, "file")
}

func isImageContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") || ct == "image"
}

// --- Media helpers ---

// saveMediaToTemp writes media bytes to a temp file and returns the path.
func saveMediaToTemp(data []byte, prefix, ext string) (string, error) {
	if ext == "" {
		ext = ".bin"
	}
	fileName := fmt.Sprintf("feishu_%s_%d%s", prefix, time.Now().UnixMilli(), ext)
	path := filepath.Join(os.TempDir(), fileName)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
}

// detectFileType maps file extension to Feishu file_type.
// Matching TS media.ts detectFileType.
func detectFileType(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".opus", ".ogg":
		return "opus"
	case ".mp4", ".mov", ".avi", ".wmv", ".mkv":
		return "mp4"
	case ".pdf":
		return "pdf"
	case ".doc", ".docx":
		return "doc"
	case ".xls", ".xlsx":
		return "xls"
	case ".ppt", ".pptx":
		return "ppt"
	default:
		return "stream"
	}
}

// resolveMediaFromMessage extracts and downloads media from a Feishu message.
// Returns list of local file paths for any media found.
func (c *Channel) resolveMediaFromMessage(ctx context.Context, messageID, messageType, rawContent string) []string {
	maxBytes := int64(c.cfg.MediaMaxMB) * 1024 * 1024
	if maxBytes <= 0 {
		maxBytes = int64(defaultMediaMaxMB) * 1024 * 1024
	}

	var paths []string

	switch messageType {
	case "image":
		imageKey := extractJSONField(rawContent, "image_key")
		if imageKey == "" {
			return nil
		}
		data, _, err := c.downloadMessageResource(ctx, messageID, imageKey, "image")
		if err != nil {
			slog.Debug("feishu download image failed", "message_id", messageID, "error", err)
			return nil
		}
		if int64(len(data)) > maxBytes {
			slog.Debug("feishu image too large", "size", len(data), "max", maxBytes)
			return nil
		}
		path, err := saveMediaToTemp(data, "img", ".png")
		if err != nil {
			slog.Debug("feishu save image failed", "error", err)
			return nil
		}
		paths = append(paths, path)

	case "file":
		fileKey := extractJSONField(rawContent, "file_key")
		if fileKey == "" {
			return nil
		}
		data, fileName, err := c.downloadMessageResource(ctx, messageID, fileKey, "file")
		if err != nil {
			slog.Debug("feishu download file failed", "message_id", messageID, "error", err)
			return nil
		}
		if int64(len(data)) > maxBytes {
			slog.Debug("feishu file too large", "size", len(data), "max", maxBytes)
			return nil
		}
		ext := filepath.Ext(fileName)
		if ext == "" {
			ext = ".bin"
		}
		path, err := saveMediaToTemp(data, "file", ext)
		if err != nil {
			slog.Debug("feishu save file failed", "error", err)
			return nil
		}
		paths = append(paths, path)

	case "audio":
		fileKey := extractJSONField(rawContent, "file_key")
		if fileKey == "" {
			return nil
		}
		data, _, err := c.downloadMessageResource(ctx, messageID, fileKey, "file")
		if err != nil {
			slog.Debug("feishu download audio failed", "error", err)
			return nil
		}
		if int64(len(data)) > maxBytes {
			return nil
		}
		path, err := saveMediaToTemp(data, "audio", ".opus")
		if err != nil {
			return nil
		}
		paths = append(paths, path)

	case "video":
		fileKey := extractJSONField(rawContent, "file_key")
		if fileKey == "" {
			return nil
		}
		data, _, err := c.downloadMessageResource(ctx, messageID, fileKey, "file")
		if err != nil {
			slog.Debug("feishu download video failed", "error", err)
			return nil
		}
		if int64(len(data)) > maxBytes {
			return nil
		}
		path, err := saveMediaToTemp(data, "video", ".mp4")
		if err != nil {
			return nil
		}
		paths = append(paths, path)

	case "sticker":
		fileKey := extractJSONField(rawContent, "file_key")
		if fileKey == "" {
			return nil
		}
		data, _, err := c.downloadMessageResource(ctx, messageID, fileKey, "image")
		if err != nil {
			return nil
		}
		if int64(len(data)) > maxBytes {
			return nil
		}
		path, err := saveMediaToTemp(data, "sticker", ".png")
		if err != nil {
			return nil
		}
		paths = append(paths, path)
	}

	return paths
}

// extractJSONField is a simple helper to extract a string field from JSON content.
// Used for parsing media keys from message content without full struct parsing.
func extractJSONField(jsonStr, field string) string {
	key := `"` + field + `":"`
	idx := strings.Index(jsonStr, key)
	if idx < 0 {
		return ""
	}
	start := idx + len(key)
	end := strings.Index(jsonStr[start:], `"`)
	if end < 0 {
		return ""
	}
	return jsonStr[start : start+end]
}
