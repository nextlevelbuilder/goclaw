package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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

// sendImage sends an image message using an image_key. When replyTargetID is
// non-empty, the image is posted through the reply endpoint so it lands inside
// the same thread as the triggering message. Falls back to the new-message
// endpoint on reply errors (see deliverMessage).
func (c *Channel) sendImage(ctx context.Context, chatID, receiveIDType, imageKey, replyTargetID string) error {
	contentBytes, err := json.Marshal(map[string]string{"image_key": imageKey})
	if err != nil {
		return fmt.Errorf("marshal image content: %w", err)
	}
	if err := c.deliverMessage(ctx, chatID, receiveIDType, replyTargetID, "image", string(contentBytes)); err != nil {
		return fmt.Errorf("feishu send image: %w", err)
	}
	return nil
}

// sendFile sends a file message using a file_key. When replyTargetID is
// non-empty, the file is posted through the reply endpoint to stay inside the
// thread. msgType: "file" for documents, "media" for audio/video.
func (c *Channel) sendFile(ctx context.Context, chatID, receiveIDType, fileKey, msgType, replyTargetID string) error {
	if msgType == "" {
		msgType = "file"
	}
	contentBytes, err := json.Marshal(map[string]string{"file_key": fileKey})
	if err != nil {
		return fmt.Errorf("marshal file content: %w", err)
	}
	if err := c.deliverMessage(ctx, chatID, receiveIDType, replyTargetID, msgType, string(contentBytes)); err != nil {
		return fmt.Errorf("feishu send file: %w", err)
	}
	return nil
}

// --- Outbound media ---

// sendMediaAttachment uploads and sends a media attachment routed by MIME type.
// Images → image message, audio/video → media message (inline playable), others → file message.
// When replyTargetID is non-empty, the message is posted through the Lark
// reply endpoint so it lands inside the same thread as the triggering message.
func (c *Channel) sendMediaAttachment(ctx context.Context, chatID, receiveIDType string, att bus.MediaAttachment, replyTargetID string) error {
	filePath := att.URL
	if filePath == "" {
		return nil
	}

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open media file %s: %w", filePath, err)
	}
	defer f.Close()

	ct := strings.ToLower(att.ContentType)

	switch {
	case isImageContentType(ct):
		imageKey, err := c.uploadImage(ctx, f)
		if err != nil {
			return fmt.Errorf("upload image: %w", err)
		}
		return c.sendImage(ctx, chatID, receiveIDType, imageKey, replyTargetID)

	case strings.HasPrefix(ct, "video/"), strings.HasPrefix(ct, "audio/"):
		// Lark "media" message type plays audio/video inline.
		fileName := filepath.Base(filePath)
		fileType := detectFileType(fileName)
		fileKey, err := c.uploadFile(ctx, f, fileName, fileType, 0)
		if err != nil {
			return fmt.Errorf("upload media: %w", err)
		}
		return c.sendFile(ctx, chatID, receiveIDType, fileKey, "media", replyTargetID)

	default:
		fileName := filepath.Base(filePath)
		fileType := detectFileType(fileName)
		fileKey, err := c.uploadFile(ctx, f, fileName, fileType, 0)
		if err != nil {
			return fmt.Errorf("upload file: %w", err)
		}
		return c.sendFile(ctx, chatID, receiveIDType, fileKey, "file", replyTargetID)
	}
}

func isImageContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") || ct == "image"
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
