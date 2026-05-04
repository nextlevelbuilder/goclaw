package oa

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
)

var legacyTokenWarnOnce sync.Once

const maxFilenameLen = 200

// uploadImage uploads bytes and returns the attachment_id. Filename must
// carry an extension — Zalo validates payload type by extension and
// silently returns empty-data otherwise.
func (c *Channel) uploadImage(ctx context.Context, data []byte, mime string) (string, error) {
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return "", err
	}
	filename := "image.jpg"
	if mime == "image/png" {
		filename = "image.png"
	}
	raw, err := c.client.apiPostMultipart(ctx, pathUploadImage, "file", filename, data, nil, tok)
	if err != nil {
		return "", err
	}
	return parseUploadAttachmentID(raw)
}

// uploadGIF uploads to /upload/gif (5MB cap).
func (c *Channel) uploadGIF(ctx context.Context, data []byte) (string, error) {
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return "", err
	}
	raw, err := c.client.apiPostMultipart(ctx, pathUploadGIF, "file", "image.gif", data, nil, tok)
	if err != nil {
		return "", err
	}
	return parseUploadAttachmentID(raw)
}

// uploadFile uploads a file. filename is sanitized (path traversal,
// dot-only, oversized inputs get a safe fallback).
func (c *Channel) uploadFile(ctx context.Context, data []byte, filename string) (string, error) {
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return "", err
	}
	safe := sanitizeFilename(filename)
	raw, err := c.client.apiPostMultipart(ctx, pathUploadFile, "file", safe,
		data, map[string]string{"filename": safe}, tok)
	if err != nil {
		return "", err
	}
	return parseUploadAttachmentID(raw)
}

// sanitizeFilename strips path components, falls back for dot-only/empty
// inputs, and caps length at maxFilenameLen.
func sanitizeFilename(raw string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	switch name {
	case "", ".", "..", string(filepath.Separator):
		// crypto/rand suffix avoids collisions on coarse-clock platforms
		// where UnixNano() can repeat across tight bursts.
		var b [4]byte
		_, _ = rand.Read(b[:])
		return fmt.Sprintf("file-%s.bin", hex.EncodeToString(b[:]))
	}
	if len(name) > maxFilenameLen {
		name = name[:maxFilenameLen]
	}
	return name
}

// parseUploadAttachmentID reads data.attachment_id from the upload
// response. Falls back to data.token (legacy alias) and warns once if seen.
func parseUploadAttachmentID(raw json.RawMessage) (string, error) {
	var env struct {
		Data struct {
			AttachmentID string `json:"attachment_id"`
			Token        string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("zalo_oa: decode upload response: %w", err)
	}
	id := env.Data.AttachmentID
	if id == "" && env.Data.Token != "" {
		legacyTokenWarnOnce.Do(func() {
			slog.Warn("zalo_oa.upload.legacy_token_field_seen")
		})
		id = env.Data.Token
	}
	if id == "" {
		preview := string(raw)
		if len(preview) > 500 {
			preview = preview[:500] + "…(truncated)"
		}
		return "", fmt.Errorf("zalo_oa: upload response missing data.attachment_id (raw=%s)", preview)
	}
	return id, nil
}
