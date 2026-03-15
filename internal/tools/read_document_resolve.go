package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// resolveDocumentFile finds the document file path from context MediaRefs.
func (t *ReadDocumentTool) resolveDocumentFile(ctx context.Context, mediaID string) (path, mime string, err error) {
	if t.mediaLoader == nil {
		return "", "", fmt.Errorf("no media storage configured — cannot access document files")
	}

	refs := MediaDocRefsFromCtx(ctx)
	if len(refs) == 0 {
		return "", "", fmt.Errorf("no documents available in this conversation. The user may not have sent a document.")
	}

	// Sanitize media_id: LLM may pass the literal tag string instead of a UUID.
	if strings.Contains(mediaID, "<") || strings.Contains(mediaID, "media:") {
		slog.Debug("read_document: sanitizing tag-like media_id", "raw", mediaID)
		mediaID = ""
	}

	// Find specific media_id or use most recent document.
	var ref *providers.MediaRef
	if mediaID != "" {
		// 1. Try exact UUID match.
		for i := range refs {
			if refs[i].ID == mediaID {
				ref = &refs[i]
				break
			}
		}

		// 2. LLM may pass a filename (e.g. "pokeapi-analysis.md") instead of UUID
		//    when <media:document> tags lack an id attribute (historical messages or
		//    pre-fix deployments). Try to match by filename in stored paths.
		if ref == nil {
			ref = t.matchByFilename(refs, mediaID)
			if ref != nil {
				slog.Info("read_document: matched by filename fallback", "media_id", mediaID, "matched_ref", ref.ID)
			}
		}

		// 3. If still not found, fall back to most recent document instead of hard error.
		//    This matches read_audio behavior and handles LLM hallucinations gracefully.
		if ref == nil {
			slog.Warn("read_document: media_id not found, falling back to most recent", "media_id", mediaID, "num_refs", len(refs))
			ref = &refs[len(refs)-1]
		}
	} else {
		// Use the last (most recent) document ref.
		ref = &refs[len(refs)-1]
	}

	p, err := t.mediaLoader.LoadPath(ref.ID)
	if err != nil {
		return "", "", fmt.Errorf("document file not found: %v", err)
	}

	// Determine MIME type: prefer ref's stored MIME, fall back to extension.
	mime = ref.MimeType
	if mime == "" || mime == "application/octet-stream" {
		mime = mimeFromDocExt(filepath.Ext(p))
	}

	return p, mime, nil
}

// callProvider dispatches document analysis to the appropriate provider API.
// For Gemini: uses native generateContent API (supports PDF natively).
// For others: uses standard Chat API with base64 document.
func (t *ReadDocumentTool) callProvider(ctx context.Context, cp credentialProvider, providerName, model string, params map[string]any) ([]byte, *providers.Usage, error) {
	prompt := GetParamString(params, "prompt", "Analyze this document and describe its contents.")
	data, _ := params["data"].([]byte)
	mime := GetParamString(params, "mime", "application/octet-stream")

	// Gemini: use native API (requires credentials; OpenAI-compat endpoint doesn't support non-image MIME types).
	ptype := GetParamString(params, "_provider_type", providerTypeFromName(providerName))
	if cp != nil && ptype == "gemini" {
		slog.Info("read_document: using gemini native API",
			"provider", providerName, "model", model,
			"doc_size", len(data), "mime", mime)
		resp, err := geminiNativeDocumentCall(ctx, cp.APIKey(), model, prompt, data, mime)
		if err != nil {
			return nil, nil, fmt.Errorf("gemini native call: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Other providers: use standard Chat API with document as base64 image_url.
	p, err := t.registry.Get(providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("read_document: using chat API", "provider", providerName, "model", model, "doc_size", len(data))
	resp, err := p.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":  16384,
			"temperature": 0.2,
		},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("chat call: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
}

// matchByFilename tries to find a MediaRef whose stored file path contains the given
// filename. This handles the case where the LLM passes a filename (from <media:document name="...">)
// instead of a UUID (when the id attribute is missing on the tag).
func (t *ReadDocumentTool) matchByFilename(refs []providers.MediaRef, filename string) *providers.MediaRef {
	if t.mediaLoader == nil || filename == "" {
		return nil
	}
	// Normalize: strip leading/trailing whitespace, lowercase for comparison.
	needle := strings.ToLower(strings.TrimSpace(filename))

	// Search from most recent to oldest (refs are ordered historical-first, current-last).
	for i := len(refs) - 1; i >= 0; i-- {
		p, err := t.mediaLoader.LoadPath(refs[i].ID)
		if err != nil {
			continue
		}
		// Check if stored path's basename or the original filename in the path matches.
		base := strings.ToLower(filepath.Base(p))
		if strings.Contains(base, needle) || strings.Contains(needle, base) {
			return &refs[i]
		}
	}
	return nil
}

// mimeFromDocExt returns MIME type for document file extensions.
func mimeFromDocExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".xls", ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".ppt", ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".csv":
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}
