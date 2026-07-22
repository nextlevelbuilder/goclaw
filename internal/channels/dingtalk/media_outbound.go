package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// Legacy upload endpoints. Media upload has no v1.0 equivalent, so it is the
// only reason the channel holds an oapi token at all.
const (
	pathMediaUpload    = "/media/upload"
	pathChunkEnable    = "/file/upload/transaction/enable"
	pathChunkUpload    = "/file/upload/chunk"
	pathChunkSubmit    = "/file/upload/transaction/submit"
	multipartMediaFile = "media"
)

// DingTalk's per-type upload caps. Exceeding them yields an opaque rejection, so
// they are checked before the bytes go out.
// Video and file have no fixed cap here: above chunkThreshold they take the
// chunked transaction instead of being rejected.
const (
	maxImageBytes = 10 * 1024 * 1024
	maxVoiceBytes = 2 * 1024 * 1024
)

// Files above chunkThreshold must go through the three-step chunk transaction.
const (
	chunkThreshold  = 20 * 1024 * 1024
	chunkSizeSmall  = 5 * 1024 * 1024 // default
	chunkSizeMedium = 6 * 1024 * 1024 // files over 50MB
	chunkSizeLarge  = 8 * 1024 * 1024 // files over 100MB (max)
	chunkTier50MB   = 50 * 1024 * 1024
	chunkTier100MB  = 100 * 1024 * 1024
)

// uploadKind is DingTalk's `type` query parameter on /media/upload.
type uploadKind string

const (
	uploadImage uploadKind = "image"
	uploadVoice uploadKind = "voice"
	uploadVideo uploadKind = "video"
	uploadFile  uploadKind = "file"
)

// sendMedia uploads each attachment and posts it as its own message.
//
// The session webhook cannot carry media, so this always uses the proactive API.
func (c *Channel) sendMedia(ctx context.Context, msg bus.OutboundMessage, isGroup bool) error {
	for _, att := range msg.Media {
		if err := c.sendOneAttachment(ctx, msg, isGroup, att); err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) sendOneAttachment(ctx context.Context, msg bus.OutboundMessage, isGroup bool, att bus.MediaAttachment) error {
	body, err := os.ReadFile(att.URL)
	if err != nil {
		return fmt.Errorf("dingtalk: read attachment %q: %w", att.URL, err)
	}

	kind := uploadKindFor(att.ContentType, att.URL)
	if err := checkUploadSize(kind, len(body)); err != nil {
		return err
	}

	fileName := filepath.Base(att.URL)
	mediaID, err := c.upload(ctx, kind, fileName, body)
	if err != nil {
		return err
	}

	msgKey, msgParam, err := buildMediaPayload(kind, mediaID, fileName)
	if err != nil {
		return err
	}
	return c.postProactive(ctx, msg, isGroup, msgKey, msgParam)
}

// upload stores bytes with DingTalk and returns the media id.
func (c *Channel) upload(ctx context.Context, kind uploadKind, fileName string, body []byte) (string, error) {
	if len(body) > chunkThreshold {
		return c.uploadChunked(ctx, fileName, body)
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(multipartMediaFile, fileName)
	if err != nil {
		return "", fmt.Errorf("dingtalk: build upload form: %w", err)
	}
	if _, err := part.Write(body); err != nil {
		return "", fmt.Errorf("dingtalk: write upload form: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("dingtalk: close upload form: %w", err)
	}

	var out struct {
		MediaID string `json:"media_id"`
	}
	query := url.Values{"type": {string(kind)}}
	if err := c.client.doOAPI(ctx, http.MethodPost, pathMediaUpload, query, &buf, w.FormDataContentType(), &out); err != nil {
		return "", err
	}
	if out.MediaID == "" {
		return "", fmt.Errorf("dingtalk: media upload returned no media_id")
	}
	// DingTalk prefixes media ids with '@'. Passing it through verbatim makes
	// the subsequent send fail with an unhelpful "invalid media" error.
	return strings.TrimPrefix(out.MediaID, "@"), nil
}

// uploadChunked runs the three-step transaction required above 20MB.
func (c *Channel) uploadChunked(ctx context.Context, fileName string, body []byte) (string, error) {
	size := len(body)

	var enable struct {
		UploadID string `json:"upload_id"`
	}
	enableQuery := url.Values{
		"file_name": {fileName},
		"file_size": {strconv.Itoa(size)},
	}
	if err := c.client.doOAPI(ctx, http.MethodPost, pathChunkEnable, enableQuery, nil, "", &enable); err != nil {
		return "", err
	}
	if enable.UploadID == "" {
		return "", fmt.Errorf("dingtalk: chunk upload enable returned no upload_id")
	}

	chunkSize := chunkSizeFor(size)
	totalChunks := (size + chunkSize - 1) / chunkSize

	for i := range totalChunks {
		start := i * chunkSize
		end := min(start+chunkSize, size)

		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			return "", fmt.Errorf("dingtalk: build chunk form: %w", err)
		}
		if _, err := part.Write(body[start:end]); err != nil {
			return "", fmt.Errorf("dingtalk: write chunk form: %w", err)
		}
		if err := w.Close(); err != nil {
			return "", fmt.Errorf("dingtalk: close chunk form: %w", err)
		}

		// chunk_number is 1-based.
		chunkQuery := url.Values{
			"upload_id":    {enable.UploadID},
			"chunk_number": {strconv.Itoa(i + 1)},
			"total_chunks": {strconv.Itoa(totalChunks)},
		}
		if err := c.client.doOAPI(ctx, http.MethodPost, pathChunkUpload, chunkQuery, &buf, w.FormDataContentType(), nil); err != nil {
			return "", fmt.Errorf("dingtalk: upload chunk %d/%d: %w", i+1, totalChunks, err)
		}
	}

	var submit struct {
		FileID string `json:"file_id"`
	}
	submitQuery := url.Values{
		"upload_id": {enable.UploadID},
		"file_name": {fileName},
	}
	if err := c.client.doOAPI(ctx, http.MethodGet, pathChunkSubmit, submitQuery, nil, "", &submit); err != nil {
		return "", err
	}
	if submit.FileID == "" {
		return "", fmt.Errorf("dingtalk: chunk upload submit returned no file_id")
	}
	return submit.FileID, nil
}

// chunkSizeFor scales the block size with the file, as the connector does.
func chunkSizeFor(size int) int {
	switch {
	case size > chunkTier100MB:
		return chunkSizeLarge
	case size > chunkTier50MB:
		return chunkSizeMedium
	default:
		return chunkSizeSmall
	}
}

// checkUploadSize rejects an oversized attachment before it is sent, so the
// operator sees the real limit instead of DingTalk's opaque failure.
func checkUploadSize(kind uploadKind, size int) error {
	var limit int
	switch kind {
	case uploadImage:
		limit = maxImageBytes
	case uploadVoice:
		limit = maxVoiceBytes
	default:
		// video and file may exceed maxFileBytes via the chunk transaction.
		return nil
	}
	if size > limit {
		return fmt.Errorf("dingtalk: %s attachment is %d bytes, over DingTalk's %d byte limit", kind, size, limit)
	}
	return nil
}

// uploadKindFor classifies an attachment. Content type wins; the extension is a
// fallback for callers that did not set one.
func uploadKindFor(contentType, path string) uploadKind {
	if contentType == "" {
		contentType = mimeByExt(path)
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return uploadImage
	case strings.HasPrefix(contentType, "audio/"):
		return uploadVoice
	case strings.HasPrefix(contentType, "video/"):
		return uploadVideo
	default:
		return uploadFile
	}
}

func mimeByExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".amr":
		return "audio/amr"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	default:
		return ""
	}
}

// buildMediaPayload maps an uploaded attachment to its msgKey/msgParam.
//
// Video is the awkward one: sampleVideo needs a separate cover image
// (picMediaId) that we do not have, so a video is delivered as a file. That is a
// worse preview but a working download, which beats a rejected send.
func buildMediaPayload(kind uploadKind, mediaID, fileName string) (msgKey, msgParam string, err error) {
	var param map[string]string
	switch kind {
	case uploadImage:
		msgKey, param = "sampleImageMsg", map[string]string{"photoURL": mediaID}
	case uploadVoice:
		// duration is required; we do not decode the clip, and DingTalk renders
		// the real length from the file itself.
		msgKey, param = "sampleAudio", map[string]string{"mediaId": mediaID, "duration": "0"}
	default:
		msgKey, param = "sampleFile", map[string]string{
			"mediaId":  mediaID,
			"fileName": fileName,
			"fileType": strings.TrimPrefix(filepath.Ext(fileName), "."),
		}
	}

	encoded, err := json.Marshal(param)
	if err != nil {
		return "", "", fmt.Errorf("dingtalk: encode media msgParam: %w", err)
	}
	return msgKey, string(encoded), nil
}
