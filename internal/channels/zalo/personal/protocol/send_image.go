package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ImageUploadResult holds the response from uploading an image to Zalo's file service.
type ImageUploadResult struct {
	NormalURL    string      `json:"normalUrl"`
	HDUrl        string      `json:"hdUrl"`
	ThumbURL     string      `json:"thumbUrl"`
	PhotoID      json.Number `json:"photoId"`      // Zalo may return string or number
	ClientFileID json.Number `json:"clientFileId"`  // Zalo may return string or number
	ChunkID      int         `json:"chunkId"`
	Finished     FlexBool    `json:"finished"`      // Zalo returns bool or int depending on endpoint

	// Set by caller (not from API response).
	Width     int `json:"-"`
	Height    int `json:"-"`
	TotalSize int `json:"-"`
}

// uploadChunkBytes is the max bytes per chunk for Zalo file uploads.
// Zalo rejects chunks > 512KB with inner error 201 ("Dung lượng chunk
// upload không được vượt quá 512K"). Use 500KB to leave headroom for
// multipart framing overhead.
const uploadChunkBytes = 500 * 1024

// UploadImage uploads an image file to Zalo's file service. Files larger
// than uploadChunkBytes are split into multiple chunks sharing a single
// clientId so the server reassembles them. The final chunk response
// carries the photoId / URLs needed for SendImage.
func UploadImage(ctx context.Context, sess *Session, threadID string, threadType ThreadType, filePath string) (*ImageUploadResult, error) {
	fileURL := getServiceURL(sess, "file")
	if fileURL == "" {
		return nil, fmt.Errorf("zalo_personal: no file service URL")
	}

	if err := checkFileSize(filePath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: read image: %w", err)
	}

	fileName := filepath.Base(filePath)
	totalSize := len(data)
	width, height := imageDimensions(data)

	pathPrefix := "/api/message/"
	typeParam := "2"
	if threadType == ThreadTypeGroup {
		pathPrefix = "/api/group/"
		typeParam = "11"
	}

	totalChunks := (totalSize + uploadChunkBytes - 1) / uploadChunkBytes
	if totalChunks == 0 {
		totalChunks = 1
	}
	clientID := time.Now().UnixMilli()

	var result ImageUploadResult
	for i := range totalChunks {
		start := i * uploadChunkBytes
		end := start + uploadChunkBytes
		if end > totalSize {
			end = totalSize
		}
		chunk := data[start:end]

		uploadParams := map[string]any{
			"totalChunk": totalChunks,
			"fileName":   fileName,
			"clientId":   clientID,
			"totalSize":  totalSize,
			"imei":       sess.IMEI,
			"isE2EE":     0,
			"jxl":        0,
			"chunkId":    i + 1,
		}
		if threadType == ThreadTypeGroup {
			uploadParams["grid"] = threadID
		} else {
			uploadParams["toid"] = threadID
		}

		encParams, encErr := encryptPayload(sess, uploadParams)
		if encErr != nil {
			return nil, fmt.Errorf("zalo_personal: encrypt upload params: %w", encErr)
		}

		uploadURL := makeURL(sess, fileURL+pathPrefix+"photo_original/upload", map[string]any{
			"type":   typeParam,
			"params": encParams,
		}, true)

		body, contentType, mpErr := buildMultipartBody("chunkContent", fileName, chunk)
		if mpErr != nil {
			return nil, fmt.Errorf("zalo_personal: build multipart chunk %d: %w", i+1, mpErr)
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, body)
		if reqErr != nil {
			return nil, reqErr
		}
		setDefaultHeaders(req, sess)
		req.Header.Set("Content-Type", contentType)

		resp, doErr := sess.Client.Do(req)
		if doErr != nil {
			return nil, fmt.Errorf("zalo_personal: upload image chunk %d: %w", i+1, doErr)
		}

		var envelope Response[*string]
		if jsonErr := readJSON(resp, &envelope); jsonErr != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("zalo_personal: parse upload response: %w", jsonErr)
		}
		resp.Body.Close()
		if envelope.ErrorCode != 0 {
			return nil, fmt.Errorf("zalo_personal: upload error code %d (chunk %d/%d)", envelope.ErrorCode, i+1, totalChunks)
		}
		if envelope.Data == nil {
			return nil, fmt.Errorf("zalo_personal: empty upload response (chunk %d/%d)", i+1, totalChunks)
		}

		plain, decErr := decryptDataField(sess, *envelope.Data)
		if decErr != nil {
			return nil, fmt.Errorf("zalo_personal: decrypt upload response: %w", decErr)
		}

		// Intermediate chunks return empty {} or partial state; the final
		// chunk carries the photoId / URLs. Parse every response — the last
		// one with non-zero PhotoID wins.
		var chunkResult ImageUploadResult
		if jsonErr := json.Unmarshal(plain, &chunkResult); jsonErr != nil {
			return nil, fmt.Errorf("zalo_personal: parse upload result (chunk %d): %w", i+1, jsonErr)
		}
		if chunkResult.PhotoID.String() != "" && chunkResult.PhotoID.String() != "0" {
			result = chunkResult
		}
	}

	if result.PhotoID.String() == "" || result.PhotoID.String() == "0" {
		return nil, fmt.Errorf("zalo_personal: upload finished without photoId (%d chunks)", totalChunks)
	}
	result.Width = width
	result.Height = height
	result.TotalSize = totalSize
	return &result, nil
}

// SendImage sends a previously uploaded image as a message.
func SendImage(ctx context.Context, sess *Session, threadID string, threadType ThreadType, upload *ImageUploadResult, caption string) (string, error) {
	fileURL := getServiceURL(sess, "file")
	if fileURL == "" {
		return "", fmt.Errorf("zalo_personal: no file service URL")
	}

	params := map[string]any{
		"photoId":  upload.PhotoID,
		"clientId": strconv.FormatInt(time.Now().UnixMilli(), 10),
		"desc":     caption,
		"width":    upload.Width,
		"height":   upload.Height,
		"rawUrl":   upload.NormalURL,
		"hdUrl":    upload.HDUrl,
		"thumbUrl": upload.ThumbURL,
		"hdSize":   strconv.Itoa(upload.TotalSize),
		"zsource":  -1,
		"ttl":      0,
		"jcp":      `{"convertible":"jxl"}`,
	}
	if threadType == ThreadTypeGroup {
		params["grid"] = threadID
		params["oriUrl"] = upload.NormalURL
	} else {
		params["toid"] = threadID
		params["normalUrl"] = upload.NormalURL
	}

	encData, err := encryptPayload(sess, params)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: encrypt image send params: %w", err)
	}

	pathPrefix := "/api/message/"
	if threadType == ThreadTypeGroup {
		pathPrefix = "/api/group/"
	}

	sendURL := makeURL(sess, fileURL+pathPrefix+"photo_original/send", map[string]any{"nretry": 0}, true)
	form := buildFormBody(map[string]string{"params": encData})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, form)
	if err != nil {
		return "", err
	}
	setDefaultHeaders(req, sess)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: send image: %w", err)
	}
	defer resp.Body.Close()

	var envelope Response[*string]
	if err := readJSON(resp, &envelope); err != nil {
		return "", fmt.Errorf("zalo_personal: parse image send response: %w", err)
	}
	if envelope.ErrorCode != 0 {
		return "", fmt.Errorf("zalo_personal: image send error code %d", envelope.ErrorCode)
	}
	if envelope.Data == nil {
		return "", nil
	}

	plain, err := decryptDataField(sess, *envelope.Data)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: decrypt image send response: %w", err)
	}

	var result struct {
		MsgID json.Number `json:"msgId"`
	}
	if err := json.Unmarshal(plain, &result); err != nil {
		return "", fmt.Errorf("zalo_personal: parse image send result: %w", err)
	}
	return result.MsgID.String(), nil
}

// imageDimensions extracts width and height from an image (PNG, JPEG, etc.).
// Returns (0, 0) if the format is unrecognized.
func imageDimensions(data []byte) (int, int) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// IsImageFile returns true if the file extension is a supported image type.
func IsImageFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}
