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

// resolveDocumentFile finds the document file path from an explicit workspace
// path or from context MediaRefs.
func (t *ReadDocumentTool) resolveDocumentFile(ctx context.Context, mediaID, docPath string) (path, mime string, err error) {
	if strings.TrimSpace(mediaID) != "" && strings.TrimSpace(docPath) != "" {
		return "", "", fmt.Errorf("specify either media_id or path, not both")
	}
	if docPath != "" {
		p, err := resolveDocumentPathArg(ctx, docPath)
		if err != nil {
			return "", "", err
		}
		return p, mimeFromDocExt(filepath.Ext(p)), nil
	}

	refs := MediaDocRefsFromCtx(ctx)
	if len(refs) == 0 {
		return "", "", fmt.Errorf("no documents available in this conversation. The user may not have sent a document.")
	}

	// Find specific media_id or use most recent document.
	var ref *providers.MediaRef
	if mediaID != "" {
		for i := range refs {
			if refs[i].ID == mediaID {
				ref = &refs[i]
				break
			}
		}
		if ref == nil {
			return "", "", fmt.Errorf("document media_id %q not found in this conversation", mediaID)
		}
	} else {
		// Use the last (most recent) document ref.
		ref = &refs[len(refs)-1]
	}

	// Prefer persisted workspace path; fall back to legacy .media/ lookup.
	p := ref.Path
	loadedLegacy := false
	if p == "" {
		var err error
		if t.mediaLoader == nil {
			return "", "", fmt.Errorf("no media storage configured")
		}
		p, err = t.mediaLoader.LoadPath(ref.ID)
		if err != nil {
			return "", "", fmt.Errorf("document file not found: %v", err)
		}
		loadedLegacy = true
	}

	if loadedLegacy {
		p, err = resolveLoadedMediaRefPath(ctx, t.mediaLoader, p, "document")
	} else {
		p, err = resolveStructuredMediaRefPath(ctx, p, "document")
	}
	if err != nil {
		return "", "", err
	}

	// Determine MIME type: prefer ref's stored MIME, fall back to extension.
	mime = ref.MimeType
	if mime == "" || mime == "application/octet-stream" {
		mime = mimeFromDocExt(filepath.Ext(p))
	}

	return p, mime, nil
}

func resolveDocumentPathArg(ctx context.Context, path string) (string, error) {
	return resolveStructuredMediaPath(ctx, path, "document")
}

func isArchiveDocumentPath(path string) bool {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"),
		strings.HasSuffix(lower, ".tar"),
		strings.HasSuffix(lower, ".tar.gz"),
		strings.HasSuffix(lower, ".tgz"),
		strings.HasSuffix(lower, ".tar.bz2"),
		strings.HasSuffix(lower, ".tbz2"),
		strings.HasSuffix(lower, ".tar.xz"),
		strings.HasSuffix(lower, ".txz"),
		strings.HasSuffix(lower, ".gz"):
		return true
	default:
		return false
	}
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
		chatReq := providers.ChatRequest{
			Messages: []providers.Message{{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime}},
			}},
			Model:   model,
			Options: map[string]any{"max_tokens": 16384},
		}
		reservation, reserveErr := reserveToolLLMUsageWithMedia(ctx, t.usageCaps, t.Name(), providerName, model, chatReq, mime, data)
		if reserveErr != nil {
			return nil, nil, reserveErr
		}
		resp, err := geminiNativeDocumentCall(ctx, cp.APIKey(), model, prompt, data, mime)
		if reservation != nil {
			reservation.Reconcile(ctx, resp, err)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("gemini native call: %w", err)
		}
		return []byte(resp.Content), resp.Usage, nil
	}

	// Other providers: use standard Chat API with document as base64 image_url.
	p, err := t.registry.Get(ctx, providerName)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %q not available: %w", providerName, err)
	}

	slog.Info("read_document: using chat API", "provider", providerName, "model", model, "doc_size", len(data))

	opts := map[string]any{
		"max_tokens":  16384,
		"temperature": 0.2,
	}
	// Scope disable_tools to claude-cli only — it's a CLI-bridge-specific
	// option that skips loading the built-in MCP toolset for one-shot calls.
	// Other providers silently ignore unknown keys today, but leaking
	// provider-specific flags into the shared Options map couples layers.
	if providerName == "claude-cli" {
		opts["disable_tools"] = true
	}

	chatReq := providers.ChatRequest{
		Messages: []providers.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  []providers.ImageContent{{MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}},
			},
		},
		Model:   model,
		Options: opts,
	}
	reservation, reserveErr := reserveToolLLMUsage(ctx, t.usageCaps, t.Name(), providerName, model, chatReq)
	if reserveErr != nil {
		return nil, nil, reserveErr
	}
	resp, err := p.Chat(ctx, chatReq)
	if reservation != nil {
		reservation.Reconcile(ctx, resp, err)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("chat call: %w", err)
	}
	return []byte(resp.Content), resp.Usage, nil
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
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz", ".tgz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}
