package dingtalk

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

const pathMessageFilesDownload = "/v1.0/robot/messageFiles/download"

// Downloads run against OSS, not the OpenAPI, so they get their own budgets.
const (
	imageDownloadTimeout = 30 * time.Second
	fileDownloadTimeout  = 60 * time.Second
)

// resolveMedia redeems every downloadCode on a message and writes the bytes to
// temp files.
//
// Failures are logged and skipped rather than aborting the message: a broken
// attachment should not silence the text that came with it.
func (c *Channel) resolveMedia(ctx context.Context, in *inbound) []media.MediaInfo {
	if len(in.Media) == 0 {
		return nil
	}

	out := make([]media.MediaInfo, 0, len(in.Media))
	for _, ref := range in.Media {
		info, err := c.fetchMedia(ctx, ref)
		if err != nil {
			slog.Warn("dingtalk media download failed",
				"channel", c.Name(), "kind", ref.Kind, "error", err)
			continue
		}
		// DingTalk already transcribed voice notes for us.
		if ref.Kind == "audio" && in.Text != "" {
			info.Transcript = in.Text
		}
		out = append(out, *info)
	}
	return out
}

// fetchMedia turns one downloadCode into a local file.
func (c *Channel) fetchMedia(ctx context.Context, ref mediaRef) (*media.MediaInfo, error) {
	url, err := c.resolveDownloadURL(ctx, ref.DownloadCode)
	if err != nil {
		return nil, err
	}

	timeout := fileDownloadTimeout
	if ref.Kind == "picture" {
		timeout = imageDownloadTimeout
	}
	body, err := c.downloadSigned(ctx, url, timeout)
	if err != nil {
		return nil, err
	}

	if max := c.mediaMaxBytes(); int64(len(body)) > max {
		return nil, fmt.Errorf("dingtalk: media is %d bytes, over the %d byte limit", len(body), max)
	}

	fileName := ref.FileName
	if fileName == "" {
		fileName = defaultFileName(ref.Kind)
	}

	path, err := saveTemp(body, fileName)
	if err != nil {
		return nil, err
	}

	return &media.MediaInfo{
		Type:        mediaTypeFor(ref.Kind),
		FilePath:    path,
		FileName:    fileName,
		FileSize:    int64(len(body)),
		ContentType: media.DetectMIMEType(fileName),
	}, nil
}

// resolveDownloadURL exchanges a downloadCode for a short-lived signed URL.
func (c *Channel) resolveDownloadURL(ctx context.Context, downloadCode string) (string, error) {
	body := map[string]string{
		"downloadCode": downloadCode,
		"robotCode":    c.cfg.ClientID,
	}
	var out struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if err := c.client.doAPI(ctx, http.MethodPost, pathMessageFilesDownload, body, &out); err != nil {
		return "", err
	}
	if out.DownloadURL == "" {
		return "", fmt.Errorf("dingtalk: messageFiles/download returned no downloadUrl")
	}
	return out.DownloadURL, nil
}

// downloadSigned fetches an OSS-signed URL.
//
// The request must carry no Content-Type header. OSS computes the signature over
// the request headers, and a Content-Type the signer did not include makes the
// signature mismatch — the download fails with an opaque 403. Go's http.Client
// does not add one to a GET with no body, but an interceptor or a shared
// Transport might, so this is asserted by a test rather than assumed.
func (c *Channel) downloadSigned(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: build media download: %w", err)
	}
	req.Header.Del("Content-Type")

	resp, err := c.client.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("dingtalk: download media http %d: %s", resp.StatusCode, truncate(raw))
	}

	// Read one byte past the cap so an oversized file is rejected without
	// buffering all of it.
	limit := c.mediaMaxBytes()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("dingtalk: read media body: %w", err)
	}
	return body, nil
}

// mediaMaxBytes is the configured inbound cap.
func (c *Channel) mediaMaxBytes() int64 {
	return int64(c.cfg.MediaMaxMB) * 1024 * 1024
}

// mediaTypeFor maps a DingTalk attachment kind onto GoClaw's media taxonomy.
func mediaTypeFor(kind string) string {
	switch kind {
	case "picture":
		return media.TypeImage
	case "audio":
		return media.TypeVoice
	case "video":
		return media.TypeVideo
	default:
		return media.TypeDocument
	}
}

func defaultFileName(kind string) string {
	switch kind {
	case "picture":
		return "image.jpg"
	case "audio":
		return "audio.amr"
	case "video":
		return "video.mp4"
	default:
		return "file.bin"
	}
}

// saveTemp writes bytes to a temp file, preserving the extension so MIME
// sniffing downstream keeps working.
func saveTemp(body []byte, fileName string) (string, error) {
	ext := filepath.Ext(fileName)
	f, err := os.CreateTemp("", "dingtalk-*"+ext)
	if err != nil {
		return "", fmt.Errorf("dingtalk: create temp file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(body); err != nil {
		os.Remove(f.Name())
		return "", fmt.Errorf("dingtalk: write temp file: %w", err)
	}
	return f.Name(), nil
}
