package protocol

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileUploadResult holds the response from uploading a file to Zalo's file service.
type FileUploadResult struct {
	FileID       string      `json:"fileId"`
	FileURL      string      `json:"fileUrl"`      // populated from WS callback
	ClientFileID json.Number `json:"clientFileId"`  // Zalo may return string or number
	ChunkID      int         `json:"chunkId"`
	Finished     int         `json:"finished"`

	// Set by caller.
	TotalSize int    `json:"-"`
	FileName  string `json:"-"`
	Checksum  string `json:"-"` // MD5 hex
}

// UploadFile uploads a non-image file to Zalo's file service.
// The upload response only contains fileId; the fileUrl arrives via WebSocket callback.
// The caller must provide the Listener so we can register a callback for the fileUrl.
func UploadFile(ctx context.Context, sess *Session, ln *Listener, threadID string, threadType ThreadType, filePath string) (*FileUploadResult, error) {
	fileURL := getServiceURL(sess, "file")
	if fileURL == "" {
		return nil, fmt.Errorf("zalo_personal: no file service URL")
	}

	if err := checkFileSize(filePath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: read file: %w", err)
	}

	fileName := filepath.Base(filePath)
	totalSize := len(data)
	clientID := time.Now().UnixMilli()

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

	var result FileUploadResult
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
			return nil, fmt.Errorf("zalo_personal: encrypt file upload params: %w", encErr)
		}

		uploadURL := makeURL(sess, fileURL+pathPrefix+"asyncfile/upload", map[string]any{
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
			return nil, fmt.Errorf("zalo_personal: upload file chunk %d: %w", i+1, doErr)
		}

		var envelope Response[*string]
		if jsonErr := readJSON(resp, &envelope); jsonErr != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("zalo_personal: parse file upload response: %w", jsonErr)
		}
		resp.Body.Close()
		if envelope.ErrorCode != 0 {
			return nil, fmt.Errorf("zalo_personal: file upload error code %d (chunk %d/%d)", envelope.ErrorCode, i+1, totalChunks)
		}
		if envelope.Data == nil {
			return nil, fmt.Errorf("zalo_personal: empty file upload response (chunk %d/%d)", i+1, totalChunks)
		}

		plain, decErr := decryptDataField(sess, *envelope.Data)
		if decErr != nil {
			return nil, fmt.Errorf("zalo_personal: decrypt file upload response: %w", decErr)
		}

		var chunkResult FileUploadResult
		if jsonErr := json.Unmarshal(plain, &chunkResult); jsonErr != nil {
			return nil, fmt.Errorf("zalo_personal: parse file upload result (chunk %d): %w", i+1, jsonErr)
		}
		if chunkResult.FileID != "" && chunkResult.FileID != "0" {
			result = chunkResult
		}
	}

	if result.FileID == "" {
		return nil, fmt.Errorf("zalo_personal: file upload finished without fileId (%d chunks)", totalChunks)
	}

	result.TotalSize = totalSize
	result.FileName = fileName

	// Compute MD5 checksum
	h := md5Hash(data)
	result.Checksum = h

	// Wait for fileUrl from WebSocket callback
	if ln != nil && result.FileID != "" && result.FileID != "-1" {
		urlCh := ln.RegisterUploadCallback(result.FileID)
		select {
		case fileURL := <-urlCh:
			result.FileURL = fileURL
		case <-time.After(30 * time.Second):
			ln.CancelUploadCallback(result.FileID)
			return nil, fmt.Errorf("zalo_personal: timeout waiting for file upload callback (fileId=%s)", result.FileID)
		case <-ctx.Done():
			ln.CancelUploadCallback(result.FileID)
			return nil, ctx.Err()
		}
	}

	return &result, nil
}

// SendFile sends a previously uploaded file as a message.
func SendFile(ctx context.Context, sess *Session, threadID string, threadType ThreadType, upload *FileUploadResult) (string, error) {
	fileURL := getServiceURL(sess, "file")
	if fileURL == "" {
		return "", fmt.Errorf("zalo_personal: no file service URL")
	}

	ext := strings.TrimPrefix(filepath.Ext(upload.FileName), ".")

	params := map[string]any{
		"fileId":      upload.FileID,
		"checksum":    upload.Checksum,
		"checksumSha": "",
		"extention":   ext, // Zalo typo: "extention" not "extension"
		"totalSize":   upload.TotalSize,
		"fileName":    upload.FileName,
		"clientId":    upload.ClientFileID.String(),
		"fType":       1,
		"fileCount":   0,
		"fdata":       "{}",
		"fileUrl":     upload.FileURL,
		"zsource":     -1,
		"ttl":         0,
	}
	if threadType == ThreadTypeGroup {
		params["grid"] = threadID
	} else {
		params["toid"] = threadID
	}

	encData, err := encryptPayload(sess, params)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: encrypt file send params: %w", err)
	}

	pathPrefix := "/api/message/"
	if threadType == ThreadTypeGroup {
		pathPrefix = "/api/group/"
	}

	sendURL := makeURL(sess, fileURL+pathPrefix+"asyncfile/msg", map[string]any{"nretry": 0}, true)
	form := buildFormBody(map[string]string{"params": encData})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, form)
	if err != nil {
		return "", err
	}
	setDefaultHeaders(req, sess)

	resp, err := sess.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: send file: %w", err)
	}
	defer resp.Body.Close()

	var respEnvelope Response[*string]
	if err := readJSON(resp, &respEnvelope); err != nil {
		return "", fmt.Errorf("zalo_personal: parse file send response: %w", err)
	}
	if respEnvelope.ErrorCode != 0 {
		return "", fmt.Errorf("zalo_personal: file send error code %d", respEnvelope.ErrorCode)
	}
	if respEnvelope.Data == nil {
		return "", nil
	}

	plain, err := decryptDataField(sess, *respEnvelope.Data)
	if err != nil {
		return "", fmt.Errorf("zalo_personal: decrypt file send response: %w", err)
	}

	var result struct {
		MsgID json.Number `json:"msgId"`
	}
	if err := json.Unmarshal(plain, &result); err != nil {
		return "", fmt.Errorf("zalo_personal: parse file send result: %w", err)
	}
	return result.MsgID.String(), nil
}

// md5Hash returns the MD5 hex digest. Required by Zalo's file upload API checksum field.
func md5Hash(data []byte) string {
	h := md5.Sum(data)
	return hex.EncodeToString(h[:])
}
