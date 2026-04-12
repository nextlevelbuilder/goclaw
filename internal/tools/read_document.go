package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// textReadableMIMEs are MIME types whose content can be returned directly without LLM analysis.
var textReadableMIMEs = map[string]bool{
	"application/json":       true,
	"text/csv":               true,
	"text/plain":             true,
	"text/html":              true,
	"text/xml":               true,
	"application/xml":        true,
	"text/markdown":          true,
	"application/javascript": true,
	"text/css":               true,
	"application/yaml":       true,
	"text/yaml":              true,
}

// documentMaxTextBytes is the max size for direct text return (500KB).
const documentMaxTextBytes = 500 * 1024

// --- Context helpers for media documents ---

const ctxMediaDocRefs toolContextKey = "tool_media_doc_refs"

// WithMediaDocRefs stores document MediaRefs in context for read_document tool access.
func WithMediaDocRefs(ctx context.Context, refs []providers.MediaRef) context.Context {
	return context.WithValue(ctx, ctxMediaDocRefs, refs)
}

// MediaDocRefsFromCtx retrieves stored document MediaRefs from context.
func MediaDocRefsFromCtx(ctx context.Context) []providers.MediaRef {
	v, _ := ctx.Value(ctxMediaDocRefs).([]providers.MediaRef)
	return v
}

// --- ReadDocumentTool ---

// documentMaxBytes is the max file size for document analysis (20MB).
const documentMaxBytes = 20 * 1024 * 1024

// documentProviderPriority is the order in which providers are tried for document analysis.
// Gemini has best native PDF support (50MB, 258 tokens/page). claude-cli is
// included so installations with only Claude CLI configured can still analyze
// PDFs via the CLI bridge (document content block in stream-json).
var documentProviderPriority = []string{"gemini", "anthropic", "claude-cli", "openrouter", "dashscope"}

// documentModelDefaults maps provider names to preferred document-capable models.
// Empty string lets the provider pick its own default model.
var documentModelDefaults = map[string]string{
	"gemini":     "gemini-2.5-flash",
	"openrouter": "google/gemini-2.5-flash",
	"claude-cli": "",
	"dashscope":  "qwen-vl-max",
}

// ReadDocumentTool uses a document-capable provider to analyze files
// attached to the current conversation. Follows same pattern as ReadImageTool.
type ReadDocumentTool struct {
	registry    *providers.Registry
	mediaLoader MediaPathLoader
}

func NewReadDocumentTool(registry *providers.Registry, mediaLoader MediaPathLoader) *ReadDocumentTool {
	return &ReadDocumentTool{registry: registry, mediaLoader: mediaLoader}
}

func (t *ReadDocumentTool) Name() string { return "read_document" }

func (t *ReadDocumentTool) Description() string {
	return "Analyze documents (PDF, DOCX, images of documents, etc.) attached to the conversation. " +
		"Use when you see <media:document> tags and need to extract or analyze document content. " +
		"Specify what you want to extract or analyze."
}

func (t *ReadDocumentTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "What to analyze. E.g. 'Extract all tables', 'Summarize key findings', 'What does page 3 say?'",
			},
			"media_id": map[string]any{
				"type":        "string",
				"description": "Optional: specific media_id from <media:document> tag. If omitted, uses most recent document.",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *ReadDocumentTool) Execute(ctx context.Context, args map[string]any) *Result {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		prompt = "Analyze this document and describe its contents."
	}
	mediaID, _ := args["media_id"].(string)

	// Resolve document file path from MediaRefs in context.
	docPath, docMime, err := t.resolveDocumentFile(ctx, mediaID)
	if err != nil {
		return ErrorResult(err.Error())
	}

	slog.Info("read_document: resolved file", "path", docPath, "mime", docMime, "media_id", mediaID)

	// Read document file.
	data, err := os.ReadFile(docPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Failed to read document file: %v", err))
	}
	slog.Info("read_document: file loaded", "size_bytes", len(data))
	if len(data) > documentMaxBytes {
		return ErrorResult(fmt.Sprintf("Document too large: %d bytes (max %d)", len(data), documentMaxBytes))
	}

	// Fast path: text-readable files — return content directly without LLM.
	if textReadableMIMEs[docMime] || strings.HasPrefix(docMime, "text/") {
		content := string(data)
		if len(data) > documentMaxTextBytes {
			content = content[:documentMaxTextBytes] + "\n\n[... truncated at 500KB ...]"
		}
		slog.Info("read_document: returning text content directly", "mime", docMime, "size", len(data))
		return NewResult(content)
	}

	chain := ResolveMediaProviderChain(ctx, "read_document", "", "",
		documentProviderPriority, documentModelDefaults, t.registry)

	// Inject prompt, data, and mime into each chain entry's params
	for i := range chain {
		if chain[i].Params == nil {
			chain[i].Params = make(map[string]any)
		}
		chain[i].Params["prompt"] = prompt
		chain[i].Params["data"] = data
		chain[i].Params["mime"] = docMime
	}

	chainResult, err := ExecuteWithChain(ctx, chain, t.registry, t.callProvider)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Document analysis failed: %v", err))
	}

	result := NewResult(string(chainResult.Data))
	result.Usage = chainResult.Usage
	result.Provider = chainResult.Provider
	result.Model = chainResult.Model
	return result
}
