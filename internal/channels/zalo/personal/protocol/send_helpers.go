package protocol

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path/filepath"
)

// maxUploadSize is the maximum file size for Zalo uploads (25 MB).
const maxUploadSize = 25 * 1024 * 1024

// checkFileSize validates that the file exists and is within the upload size limit.
func checkFileSize(filePath string) error {
	fi, err := os.Stat(filePath)
	if err != nil {
		// Log directory contents for diagnostics when file is missing.
		dir := filepath.Dir(filePath)
		if entries, dirErr := os.ReadDir(dir); dirErr == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			slog.Warn("zalo_personal: file stat failed, dir listing",
				"path", filePath, "dir", dir, "contents", names, "error", err)
		}
		return fmt.Errorf("zalo_personal: stat file: %w", err)
	}
	if fi.Size() > maxUploadSize {
		return fmt.Errorf("zalo_personal: file too large: %d bytes (max %d)", fi.Size(), maxUploadSize)
	}
	return nil
}

// FlexBool handles JSON fields that may be bool (true/false) or number (0/1).
type FlexBool bool

func (b *FlexBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", "1":
		*b = true
	default:
		*b = false
	}
	return nil
}

// buildMultipartBody creates a multipart/form-data body with a single file field.
func buildMultipartBody(fieldName, fileName string, data []byte) (io.Reader, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	boundary := "----GoClaw" + randomBoundary()
	if err := w.SetBoundary(boundary); err != nil {
		return nil, "", err
	}

	part, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(data); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}

	return &buf, w.FormDataContentType(), nil
}

func randomBoundary() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
