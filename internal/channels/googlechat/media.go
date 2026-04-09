package googlechat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// downloadAttachment downloads a Chat API attachment to a temp file.
func (c *Channel) downloadAttachment(ctx context.Context, att chatAttachment) (string, error) {
	if att.ResourceName == "" {
		return "", fmt.Errorf("attachment has no resourceName")
	}

	token, err := c.auth.Token(ctx)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/media/%s?alt=media", chatAPIBase, att.ResourceName)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download attachment %d: %s", resp.StatusCode, string(body))
	}

	// Check size limit.
	if c.mediaMaxBytes > 0 && resp.ContentLength > c.mediaMaxBytes {
		return "", fmt.Errorf("attachment too large: %d bytes (max %d)", resp.ContentLength, c.mediaMaxBytes)
	}

	// Determine extension from content type.
	ext := extensionFromMIME(att.ContentType)
	tmpPath := filepath.Join(os.TempDir(), uuid.New().String()+ext)

	f, err := os.Create(tmpPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Limit read to mediaMaxBytes.
	reader := io.Reader(resp.Body)
	if c.mediaMaxBytes > 0 {
		reader = io.LimitReader(resp.Body, c.mediaMaxBytes+1)
	}
	n, err := io.Copy(f, reader)
	if err != nil {
		os.Remove(tmpPath)
		return "", err
	}
	if c.mediaMaxBytes > 0 && n > c.mediaMaxBytes {
		os.Remove(tmpPath)
		return "", fmt.Errorf("attachment exceeded max size during download")
	}

	slog.Debug("googlechat: attachment downloaded", "path", tmpPath, "size", n, "type", att.ContentType)
	return tmpPath, nil
}

// driveFileRecord tracks uploaded Drive files for retention cleanup.
type driveFileRecord struct {
	FileID    string
	CreatedAt time.Time
}

// startDriveCleanupLoop periodically deletes expired Drive files.
func (c *Channel) startDriveCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpiredDriveFiles(ctx)
		}
	}
}

// cleanupExpiredDriveFiles deletes Drive files older than fileRetentionDays.
func (c *Channel) cleanupExpiredDriveFiles(ctx context.Context) {
	c.driveFilesMu.Lock()
	defer c.driveFilesMu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -c.fileRetentionDays)
	var remaining []driveFileRecord
	for _, f := range c.driveFiles {
		if f.CreatedAt.Before(cutoff) {
			if err := c.deleteDriveFile(ctx, f.FileID); err != nil {
				slog.Warn("googlechat: failed to delete expired drive file", "file_id", f.FileID, "error", err)
				remaining = append(remaining, f) // retry next cycle
			} else {
				slog.Debug("googlechat: deleted expired drive file", "file_id", f.FileID)
			}
		} else {
			remaining = append(remaining, f)
		}
	}
	c.driveFiles = remaining
}

// deleteDriveFile deletes a file from Google Drive.
func (c *Channel) deleteDriveFile(ctx context.Context, fileID string) error {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/files/%s", driveAPIBase, fileID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete drive file %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// extensionFromMIME returns a file extension for common MIME types.
func extensionFromMIME(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "application/pdf"):
		return ".pdf"
	case strings.HasPrefix(mime, "text/plain"):
		return ".txt"
	case strings.HasPrefix(mime, "text/markdown"):
		return ".md"
	default:
		return ""
	}
}

// uploadToDrive uploads a file to Google Drive and returns the file ID and web link.
func (c *Channel) uploadToDrive(ctx context.Context, localPath string, fileName string, mimeType string) (fileID string, webLink string, err error) {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return "", "", err
	}

	f, err := os.Open(localPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	// Build multipart upload body.
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer writer.Close()

		// Part 1: metadata
		metaHeader := make(textproto.MIMEHeader)
		metaHeader.Set("Content-Type", "application/json; charset=UTF-8")
		metaPart, _ := writer.CreatePart(metaHeader)
		json.NewEncoder(metaPart).Encode(map[string]string{
			"name":     fileName,
			"mimeType": mimeType,
		})

		// Part 2: file content
		fileHeader := make(textproto.MIMEHeader)
		fileHeader.Set("Content-Type", mimeType)
		filePart, _ := writer.CreatePart(fileHeader)
		io.Copy(filePart, f)
	}()

	url := driveUploadBase + "/files?uploadType=multipart&fields=id,webViewLink"
	req, err := http.NewRequestWithContext(ctx, "POST", url, pr)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "multipart/related; boundary="+writer.Boundary())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("drive upload: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("drive upload %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID          string `json:"id"`
		WebViewLink string `json:"webViewLink"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", "", fmt.Errorf("parse drive response: %w", err)
	}

	// Set permissions.
	if err := c.setDrivePermission(ctx, result.ID); err != nil {
		slog.Warn("googlechat: failed to set drive permission", "file_id", result.ID, "error", err)
	}

	// Track for retention cleanup.
	if c.fileRetentionDays > 0 {
		c.driveFilesMu.Lock()
		c.driveFiles = append(c.driveFiles, driveFileRecord{FileID: result.ID, CreatedAt: time.Now()})
		c.driveFilesMu.Unlock()
	}

	return result.ID, result.WebViewLink, nil
}

// setDrivePermission sets the sharing permission on a Drive file.
func (c *Channel) setDrivePermission(ctx context.Context, fileID string) error {
	token, err := c.auth.Token(ctx)
	if err != nil {
		return err
	}

	var perm map[string]string
	switch c.drivePermission {
	case "anyone":
		perm = map[string]string{"type": "anyone", "role": "reader"}
	default: // "domain"
		perm = map[string]string{"type": "domain", "role": "reader", "domain": c.driveDomain}
	}

	body, _ := json.Marshal(perm)
	url := fmt.Sprintf("%s/files/%s/permissions", driveAPIBase, fileID)

	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set permission %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
