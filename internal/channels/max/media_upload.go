package max

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Upload size cap for outbound. Conservative — tighten if Max enforces lower
// limits in production. Tested values from API observation suggest ≤10 MiB
// works reliably.
const uploadMaxBytes int64 = 50 << 20 // 50 MiB

// uploadAndAttachMedia processes outbound MediaAttachments by:
//  1. Determining Max upload type from ContentType / filename
//  2. POST /uploads?type=... → temporary upload URL
//  3. multipart POST file bytes to that URL
//  4. Building Attachment entries with the returned token/photo_id
//
// Returns the attachments to include in SendMessageRequest, and a slice of
// errors keyed by attachment index. Errors do NOT abort the whole batch —
// individual upload failures are logged and the message is sent without that
// attachment. The agent's text content is preserved.
//
// Returns nil, nil if input is empty.
func (c *Channel) uploadAndAttachMedia(
	ctx context.Context,
	media []bus.MediaAttachment,
) (atts []Attachment, errs []uploadError) {
	if len(media) == 0 {
		return nil, nil
	}

	for i, m := range media {
		att, err := c.uploadOneMedia(ctx, m)
		if err != nil {
			errs = append(errs, uploadError{Index: i, Err: err})
			slog.Warn("max: outbound media upload failed",
				"channel", c.Name(),
				"index", i,
				"path", m.URL,
				"error", err)
			continue
		}
		atts = append(atts, att)
	}
	return atts, errs
}

// uploadError pairs an upload failure with the original attachment index
// so callers can map errors back to user-facing positions if needed.
type uploadError struct {
	Index int
	Err   error
}

// uploadOneMedia handles a single attachment: open file, classify type,
// request upload URL, push bytes, return a built Attachment.
func (c *Channel) uploadOneMedia(ctx context.Context, m bus.MediaAttachment) (Attachment, error) {
	if m.URL == "" {
		return Attachment{}, errors.New("media URL is empty")
	}

	maxType := classifyUploadType(m.ContentType, m.URL)

	// Open the file. For Day 4, we only support local file paths; remote
	// URLs would require fetching first, then uploading — agent loop
	// produces local paths today.
	file, err := os.Open(m.URL)
	if err != nil {
		return Attachment{}, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return Attachment{}, fmt.Errorf("stat: %w", err)
	}
	if stat.Size() > uploadMaxBytes {
		return Attachment{}, fmt.Errorf("file too large: %d bytes (limit %d)",
			stat.Size(), uploadMaxBytes)
	}
	if stat.Size() == 0 {
		return Attachment{}, errors.New("file is empty")
	}

	// Step 1: request upload URL.
	uploadURL, err := c.client.RequestUploadURL(ctx, maxType)
	if err != nil {
		return Attachment{}, fmt.Errorf("request upload url: %w", err)
	}

	// Step 2: push file bytes.
	uploadResp, err := c.client.UploadFile(ctx, uploadURL, file, filepath.Base(m.URL), m.ContentType)
	if err != nil {
		return Attachment{}, fmt.Errorf("upload file: %w", err)
	}

	// Step 3: build Attachment from server response. The exact response
	// shape depends on upload type:
	//   - image:    {photo_ids: {<id>: <token>}}  OR  {photos: {...}}
	//   - video:    {token: "..."}
	//   - audio:    {token: "..."}
	//   - file:     {token: "..."}
	//
	// We accept any of the documented shapes; if none match, return an
	// error so the caller can decide whether to retry or skip.
	att, err := buildAttachmentFromUploadResponse(maxType, uploadResp)
	if err != nil {
		return Attachment{}, fmt.Errorf("interpret upload response: %w", err)
	}
	return att, nil
}

// classifyUploadType maps a file's MIME type / filename to the Max upload
// type query parameter. Recognized values: image, video, audio, file.
func classifyUploadType(contentType, path string) string {
	ct := strings.ToLower(contentType)

	switch {
	case strings.HasPrefix(ct, "image/"):
		return "image"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	}

	// Fall back to extension-based classification.
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp":
		return "image"
	case ".mp4", ".mov", ".webm", ".mkv", ".avi":
		return "video"
	case ".mp3", ".ogg", ".m4a", ".wav", ".flac", ".aac":
		return "audio"
	}

	return "file"
}

// buildAttachmentFromUploadResponse constructs an Attachment payload from
// the upload service response. Accepts multiple shapes observed in the
// wild — see uploadResponse type for the union.
func buildAttachmentFromUploadResponse(maxType string, resp uploadResponse) (Attachment, error) {
	switch maxType {
	case "image":
		// Image uploads return photo_ids — a map of photoID → token.
		// We forward all photos. If the map is empty, fall back to "photos"
		// (legacy shape).
		if len(resp.PhotoIDs) > 0 {
			// Pick first; Max accepts {photo_ids:{ID:TOKEN}} for one image.
			for id, tok := range resp.PhotoIDs {
				return Attachment{
					Type: AttachmentTypeImage,
					Payload: AttachmentPayload{
						PhotoID: id,
						Token:   tok,
					},
				}, nil
			}
		}
		if resp.Photos != nil {
			for id, tok := range resp.Photos {
				return Attachment{
					Type: AttachmentTypeImage,
					Payload: AttachmentPayload{
						PhotoID: id,
						Token:   tok,
					},
				}, nil
			}
		}
		return Attachment{}, errors.New("upload response has no photo_ids or photos")

	case "video":
		if resp.Token == "" {
			return Attachment{}, errors.New("video upload response has no token")
		}
		return Attachment{
			Type:    AttachmentTypeVideo,
			Payload: AttachmentPayload{Token: resp.Token},
		}, nil

	case "audio":
		if resp.Token == "" {
			return Attachment{}, errors.New("audio upload response has no token")
		}
		return Attachment{
			Type:    AttachmentTypeAudio,
			Payload: AttachmentPayload{Token: resp.Token},
		}, nil

	case "file":
		if resp.Token == "" {
			return Attachment{}, errors.New("file upload response has no token")
		}
		return Attachment{
			Type:    AttachmentTypeFile,
			Payload: AttachmentPayload{Token: resp.Token},
		}, nil
	}

	return Attachment{}, fmt.Errorf("unknown upload type %q", maxType)
}

// uploadResponse models the body returned by the Max upload service after a
// successful multipart POST. Field set is the union across documented types.
type uploadResponse struct {
	// image: photo_ids is the canonical shape per docs.
	PhotoIDs map[int64]string `json:"photo_ids,omitempty"`

	// image: legacy shape some endpoints return.
	Photos map[int64]string `json:"photos,omitempty"`

	// video / audio / file
	Token string `json:"token,omitempty"`
}

// =====================================================================
// Client methods (added here to keep upload code colocated)
// =====================================================================

// RequestUploadURL calls POST /uploads?type=<kind> on the platform API and
// returns the temporary upload URL where the actual bytes will be pushed.
//
// kind must be one of: "image", "video", "audio", "file".
//
// Note: Max returns a URL that points to a different host (iu.oneme.ru in
// observed responses). The bot token is NOT used to authenticate to this
// host — the URL itself is signed.
func (c *Client) RequestUploadURL(ctx context.Context, kind string) (string, error) {
	if kind == "" {
		return "", errors.New("max client: upload kind is required")
	}

	q := url.Values{}
	q.Set("type", kind)

	var body struct {
		URL string `json:"url"`
	}
	if err := c.do(ctx, http.MethodPost, "/uploads", q, nil, &body); err != nil {
		return "", fmt.Errorf("request upload url: %w", err)
	}
	if body.URL == "" {
		return "", errors.New("upload service returned empty url")
	}
	return body.URL, nil
}

// UploadFile posts a file body as multipart/form-data to the temporary
// upload URL returned by RequestUploadURL. Returns the parsed response
// containing tokens/photo_ids needed to attach the file to a message.
//
// reader supplies the file bytes. filename is used in the multipart
// Content-Disposition. contentType, when non-empty, sets the Content-Type
// of the form file part.
//
// Note: this method does NOT add the bot Authorization header — the upload
// URL is pre-signed and rejects unexpected headers in some cases. Use the
// channel's regular http client; rate limits are enforced by the upload
// service via the URL's signature, not by the platform-api.max.ru limit.
func (c *Client) UploadFile(
	ctx context.Context,
	uploadURL string,
	reader io.Reader,
	filename string,
	contentType string,
) (uploadResponse, error) {
	if uploadURL == "" {
		return uploadResponse{}, errors.New("max client: upload url is required")
	}
	if reader == nil {
		return uploadResponse{}, errors.New("max client: reader is required")
	}

	// Build multipart body in memory. The expected file size is bounded by
	// uploadMaxBytes (~50 MiB), which is well within reasonable RAM.
	// Streaming via io.Pipe is possible but adds complexity for marginal
	// benefit at our size budget.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	header := make(map[string][]string)
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	cd := fmt.Sprintf(`form-data; name="data"; filename=%q`, filename)
	header["Content-Disposition"] = []string{cd}

	part, err := mw.CreatePart(header)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("multipart part: %w", err)
	}
	if _, err := io.Copy(part, reader); err != nil {
		return uploadResponse{}, fmt.Errorf("copy file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return uploadResponse{}, fmt.Errorf("close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &buf)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return uploadResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return uploadResponse{}, fmt.Errorf("upload http %d: %s",
			resp.StatusCode, truncateForLog(respBody, 200))
	}

	var out uploadResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return uploadResponse{}, fmt.Errorf("decode upload response: %w", err)
	}
	return out, nil
}
