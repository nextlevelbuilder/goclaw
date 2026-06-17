package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
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
	usageCaps   *usagecaps.Service
	localParser DocumentParser
}

func NewReadDocumentTool(registry *providers.Registry, mediaLoader MediaPathLoader) *ReadDocumentTool {
	return &ReadDocumentTool{registry: registry, mediaLoader: mediaLoader}
}

func (t *ReadDocumentTool) SetUsageCapService(svc *usagecaps.Service) {
	t.usageCaps = svc
}

// SetLocalParser injects the local-first document extractor. When nil (default)
// or disabled, read_document behaves exactly as before — straight to the chain.
func (t *ReadDocumentTool) SetLocalParser(p DocumentParser) {
	t.localParser = p
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
			"path": map[string]any{
				"type":        "string",
				"description": "Optional file path from a <media:document path=\"...\"> tag. Use this when the tag provides a path or the file is an archive that should be inspected with exec.",
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
	docPathArg, _ := args["path"].(string)

	// Resolve document file path from MediaRefs in context.
	docPath, docMime, err := t.resolveDocumentFile(ctx, mediaID, docPathArg)
	if err != nil {
		return ErrorResult(err.Error())
	}

	slog.Info("read_document: resolved file", "path", docPath, "mime", docMime, "media_id", mediaID)

	if isArchiveDocumentPath(docPath) {
		return NewResult(fmt.Sprintf(
			"Archive file available at %s. read_document does not analyze archive containers directly. Use exec to inspect or extract it, for example: unzip -l %q or unzip -q %q -d <output-dir>, then use list_files/read_file on extracted files.",
			docPath, docPath, docPath,
		))
	}

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
			content = content[:documentMaxTextBytes] + docTruncationMarker
		}
		slog.Info("read_document: returning text content directly", "mime", docMime, "size", len(data))
		return NewResult(content)
	}

	// Local-first extraction (opt-in): for PDF/DOCX with an available local
	// binary, extract text without any cloud LLM call. Any miss — disabled,
	// unsupported mime, missing binary, empty/poor output, or exec error —
	// falls through unchanged to the vision chain below. The file was already
	// read + 20MB-checked above; the extractor opens that same path itself.
	if t.localParser != nil && t.localParser.Supports(docMime) {
		// Defense-in-depth: confirm the path is workspace-confined before
		// handing it to a subprocess. A rejection routes to vision rather than
		// erroring, preserving today's behavior for MediaRef-derived paths.
		if safePath, verr := t.validateExecPath(ctx, docPath); verr != nil {
			slog.Warn("security.read_document_local_path_rejected", "path", docPath, "reason", verr.Error())
		} else if text, err := t.localParser.Extract(ctx, safePath, docMime); err == nil {
			slog.Info("read_document: local extraction hit", "mime", docMime, "bytes", len(text))
			return NewResult(text) // no Provider/Model/Usage => no LLM spend
		} else {
			slog.Info("read_document: local extraction miss, falling back", "mime", docMime, "reason", err.Error())
		}
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

// validateExecPath confirms a resolved document path is workspace-confined
// before it is passed to a local extractor subprocess. It reuses the same
// allow/deny resolution as the explicit-path argument branch so MediaRef-derived
// paths get the same boundary check at the exec boundary. Callers fall back to
// the vision chain on error rather than surfacing it to the user.
func (t *ReadDocumentTool) validateExecPath(ctx context.Context, path string) (string, error) {
	workspace := ToolWorkspaceFromCtx(ctx)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, true), allowedWithTeamWorkspace(ctx, nil))
	if err != nil {
		return "", err
	}
	if err := checkDeniedPath(resolved, workspace, nil); err != nil {
		return "", err
	}
	return resolved, nil
}
