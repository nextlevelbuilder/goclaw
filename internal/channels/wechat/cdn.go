package wechat

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// UploadedFileInfo holds the result of uploading a file to the CDN.
type UploadedFileInfo struct {
	FileKey                     string
	DownloadEncryptedQueryParam string
	AesKeyHex                   string // hex-encoded AES key
	FileSize                    int64  // plaintext size
	FileSizeCiphertext          int64  // ciphertext size
}

// buildCdnDownloadURL constructs a CDN download URL.
func buildCdnDownloadURL(encryptedQueryParam, cdnBaseURL string) string {
	return fmt.Sprintf("%s/download?encrypted_query_param=%s",
		cdnBaseURL, url.QueryEscape(encryptedQueryParam))
}

// buildCdnUploadURL constructs a CDN upload URL.
func buildCdnUploadURL(cdnBaseURL, uploadParam, filekey string) string {
	return fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s",
		cdnBaseURL, url.QueryEscape(uploadParam), url.QueryEscape(filekey))
}

const uploadMaxRetries = 3

// uploadBufferToCdn encrypts and uploads a buffer to the Weixin CDN.
func uploadBufferToCdn(ctx context.Context, plaintext, aesKey []byte, uploadFullURL, uploadParam, filekey, cdnBaseURL, label string) (string, error) {
	ciphertext, err := encryptAesEcb(plaintext, aesKey)
	if err != nil {
		return "", fmt.Errorf("%s: encrypt: %w", label, err)
	}

	var cdnURL string
	trimmedFull := strings.TrimSpace(uploadFullURL)
	if trimmedFull != "" {
		cdnURL = trimmedFull
	} else if uploadParam != "" {
		cdnURL = buildCdnUploadURL(cdnBaseURL, uploadParam, filekey)
	} else {
		return "", fmt.Errorf("%s: CDN upload URL missing", label)
	}

	slog.Debug("wechat cdn upload", "label", label, "size", len(ciphertext))

	var downloadParam string
	var lastErr error
	client := &http.Client{Timeout: 60 * time.Second}

	for attempt := 1; attempt <= uploadMaxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cdnURL, bytes.NewReader(ciphertext))
		if err != nil {
			return "", fmt.Errorf("%s: create request: %w", label, err)
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			if attempt < uploadMaxRetries {
				slog.Error("wechat cdn upload failed, retrying", "label", label, "attempt", attempt, "error", err)
				continue
			}
			break
		}
		resp.Body.Close()

		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			errMsg := resp.Header.Get("x-error-message")
			return "", fmt.Errorf("%s: CDN client error %d: %s", label, resp.StatusCode, errMsg)
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("%s: CDN server error %d", label, resp.StatusCode)
			if attempt < uploadMaxRetries {
				slog.Error("wechat cdn upload failed, retrying", "label", label, "attempt", attempt, "status", resp.StatusCode)
				continue
			}
			break
		}

		downloadParam = resp.Header.Get("x-encrypted-param")
		if downloadParam == "" {
			lastErr = fmt.Errorf("%s: CDN response missing x-encrypted-param header", label)
			if attempt < uploadMaxRetries {
				continue
			}
			break
		}
		return downloadParam, nil
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("%s: CDN upload failed after %d attempts", label, uploadMaxRetries)
}

// uploadMediaToCdn reads a file, generates AES key, gets upload URL, and uploads.
func uploadMediaToCdn(ctx context.Context, api *APIClient, filePath, toUserID, cdnBaseURL string, mediaType int, label string) (*UploadedFileInfo, error) {
	plaintext, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("%s: read file: %w", label, err)
	}

	rawSize := int64(len(plaintext))
	hash := md5.Sum(plaintext)
	rawFileMD5 := hex.EncodeToString(hash[:])
	fileSize := aesEcbPaddedSize(len(plaintext))

	fileKeyBytes := make([]byte, 16)
	_, _ = rand.Read(fileKeyBytes)
	fileKey := hex.EncodeToString(fileKeyBytes)

	aesKeyBytes := make([]byte, 16)
	_, _ = rand.Read(aesKeyBytes)
	aesKeyHex := hex.EncodeToString(aesKeyBytes)

	uploadResp, err := api.GetUploadURL(ctx, &GetUploadURLReq{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     rawSize,
		RawFileMD5:  rawFileMD5,
		FileSize:    fileSize,
		NoNeedThumb: true,
		AesKey:      aesKeyHex,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: getUploadUrl: %w", label, err)
	}

	uploadFullURL := strings.TrimSpace(uploadResp.UploadFullURL)
	uploadParam := uploadResp.UploadParam
	if uploadFullURL == "" && uploadParam == "" {
		return nil, fmt.Errorf("%s: getUploadUrl returned no upload URL", label)
	}

	downloadParam, err := uploadBufferToCdn(ctx, plaintext, aesKeyBytes, uploadFullURL, uploadParam, fileKey, cdnBaseURL, label)
	if err != nil {
		return nil, err
	}

	return &UploadedFileInfo{
		FileKey:                     fileKey,
		DownloadEncryptedQueryParam: downloadParam,
		AesKeyHex:                   aesKeyHex,
		FileSize:                    rawSize,
		FileSizeCiphertext:          fileSize,
	}, nil
}

// downloadAndDecrypt downloads and AES-128-ECB decrypts a CDN media file.
func downloadAndDecrypt(ctx context.Context, encryptedQueryParam, aesKeyBase64, cdnBaseURL, label, fullURL string) ([]byte, error) {
	key, err := parseAesKey(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}

	var downloadURL string
	if fullURL != "" {
		downloadURL = fullURL
	} else {
		downloadURL = buildCdnDownloadURL(encryptedQueryParam, cdnBaseURL)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", label, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: fetch: %w", label, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: CDN download %d", label, resp.StatusCode)
	}

	encrypted, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", label, err)
	}

	decrypted, err := decryptAesEcb(encrypted, key)
	if err != nil {
		return nil, fmt.Errorf("%s: decrypt: %w", label, err)
	}
	return decrypted, nil
}

// parseAesKey parses a base64-encoded AES key, handling two encodings.
func parseAesKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := tryDecodeBase64(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode aes_key base64: %w", err)
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 && isHexString(decoded) {
		key, err := hex.DecodeString(string(decoded))
		if err != nil {
			return nil, fmt.Errorf("decode hex aes_key: %w", err)
		}
		return key, nil
	}
	return nil, fmt.Errorf("aes_key must decode to 16 raw bytes or 32-char hex, got %d bytes", len(decoded))
}

func isHexString(b []byte) bool {
	for _, c := range b {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func tryDecodeBase64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

// downloadRemoteToTemp downloads a remote URL to a local temp file.
func downloadRemoteToTemp(ctx context.Context, remoteURL string) (string, error) {
	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, remoteURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("remote media download failed: %d %s", resp.StatusCode, resp.Status)
	}

	// Determine extension from URL or Content-Type
	ext := guessExtFromContentType(resp.Header.Get("Content-Type"), remoteURL)

	// Create temp file using standard Go pattern
	tmpFile, err := os.CreateTemp("", "wechat-remote-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("save file: %w", err)
	}

	return tmpFile.Name(), nil
}

func guessExtFromContentType(contentType, rawURL string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(ct, "image/png"):
		return ".png"
	case strings.Contains(ct, "image/gif"):
		return ".gif"
	case strings.Contains(ct, "image/webp"):
		return ".webp"
	case strings.Contains(ct, "image/jpeg"), strings.Contains(ct, "image/jpg"):
		return ".jpg"
	case strings.Contains(ct, "video/mp4"):
		return ".mp4"
	case strings.Contains(ct, "audio/wav"):
		return ".wav"
	case strings.Contains(ct, "application/pdf"):
		return ".pdf"
	}
	ext := filepath.Ext(rawURL)
	if ext != "" && len(ext) <= 6 {
		return ext
	}
	return ".bin"
}
