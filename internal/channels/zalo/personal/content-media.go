package personal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// extractContentAndMedia returns text content with media tags plus local media paths.
func extractContentAndMedia(content protocol.Content) (string, []string) {
	if text := content.Text(); text != "" {
		return text, nil
	}
	att := content.ParseAttachment()
	if att == nil || att.URL() == "" {
		return "", nil
	}

	filePath, err := downloadFile(context.Background(), att.URL())
	if err != nil {
		slog.Warn("zalo_personal: failed to download attachment", "url", att.URL(), "error", err)
		if text := content.AttachmentText(); text != "" {
			return text, nil
		}
		return "", nil
	}

	mimeType := media.DetectMIMEType(filePath)
	mediaKind := media.MediaKindFromMime(mimeType)
	if mediaKind != media.TypeImage && att.IsImage() {
		mediaKind = media.TypeImage
	}

	tag := media.BuildMediaTags([]media.MediaInfo{{
		Type:        mediaKind,
		FilePath:    filePath,
		ContentType: mimeType,
		FileName:    att.Title,
	}})
	return tag, []string{filePath}
}

func cacheBodyForContent(content protocol.Content) string {
	if text := strings.TrimSpace(content.Text()); text != "" {
		return text
	}
	return content.AttachmentText()
}

func displayNameOrID(displayName, id string) string {
	if strings.TrimSpace(displayName) != "" {
		return displayName
	}
	return id
}

const maxMediaBytes = 20 * 1024 * 1024

// downloadFile validates URL safety and stores the attachment in a bounded temp file.
func downloadFile(ctx context.Context, fileURL string) (string, error) {
	if err := tools.CheckSSRF(fileURL); err != nil {
		return "", fmt.Errorf("ssrf check: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	path := fileURL
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	ext := filepath.Ext(path)
	if ext == "" || len(ext) > 5 {
		ext = ".bin"
	}

	tmpFile, err := os.CreateTemp("", "goclaw_zca_*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer tmpFile.Close()

	written, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save: %w", err)
	}
	if written > maxMediaBytes {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("file too large: %d bytes (max %d)", written, maxMediaBytes)
	}
	return tmpFile.Name(), nil
}
