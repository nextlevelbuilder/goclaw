package tools

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// VaultReadTool reads full content of a vault document by doc_id.
// Provides access to shared/personal/team vault docs that read_file cannot
// reach because the path lies outside the agent's canonical workspace.
type VaultReadTool struct {
	vaultStore    store.VaultStore
	kgStore       store.KnowledgeGraphStore
	episodicStore store.EpisodicStore
	workspace     string
}

// Defaults / limits for max_bytes.
const (
	vaultReadDefaultMaxBytes = 500_000
	vaultReadCeilingMaxBytes = 1_048_576
	vaultReadUTF8SniffBytes  = 8192
	vaultReadMaxOutlinks     = 20
)

// outlinkAllowedTypes restricts the footer to knowledge-graph links; operational
// link types (task_attachment, delegation_attachment, ...) are excluded.
var outlinkAllowedTypes = map[string]struct{}{
	"wikilink":  {},
	"reference": {},
}

// nonTextExtensions are file extensions rejected by the text-only gate.
// Lower-case with leading dot. Keep in sync with phase 01 spec.
var nonTextExtensions = map[string]struct{}{
	// images
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {},
	".bmp": {}, ".svg": {}, ".ico": {}, ".heic": {}, ".tiff": {},
	// audio
	".mp3": {}, ".wav": {}, ".ogg": {}, ".flac": {}, ".m4a": {},
	".aac": {}, ".opus": {},
	// video
	".mp4": {}, ".mov": {}, ".avi": {}, ".mkv": {}, ".webm": {}, ".m4v": {},
	// archives
	".zip": {}, ".tar": {}, ".gz": {}, ".bz2": {}, ".xz": {}, ".7z": {}, ".rar": {},
	// binary documents
	".pdf": {}, ".docx": {}, ".xlsx": {}, ".pptx": {},
	".doc": {}, ".xls": {}, ".ppt": {},
	// executables
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".bin": {},
}

// NewVaultReadTool creates a new vault_read tool.
func NewVaultReadTool() *VaultReadTool { return &VaultReadTool{} }

// SetVaultStore injects the VaultStore dependency (wired at boot).
func (t *VaultReadTool) SetVaultStore(vs store.VaultStore) { t.vaultStore = vs }

// SetKGStore injects an optional KnowledgeGraphStore used for namespace-fallback
// lookup when vault lookup misses. nil is safe.
func (t *VaultReadTool) SetKGStore(kg store.KnowledgeGraphStore) { t.kgStore = kg }

// SetEpisodicStore injects an optional EpisodicStore used for namespace-fallback
// lookup when vault lookup misses. nil is safe.
func (t *VaultReadTool) SetEpisodicStore(es store.EpisodicStore) { t.episodicStore = es }

// SetWorkspace injects the tenant workspace root (wired at boot).
func (t *VaultReadTool) SetWorkspace(ws string) { t.workspace = ws }

func (t *VaultReadTool) Name() string { return "vault_read" }

func (t *VaultReadTool) Description() string {
	return "Read full content of a vault document by doc_id (obtained from vault_search). Use for shared/personal/team vault docs that read_file cannot reach. Text-only — for media use read_image/read_audio/read_video/read_document."
}

func (t *VaultReadTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"doc_id": map[string]any{
				"type":        "string",
				"description": "Vault document UUID (from vault_search result).",
			},
			"max_bytes": map[string]any{
				"type":        "number",
				"description": "Optional byte cap (default 500000, hard max 1048576).",
			},
		},
		"required": []string{"doc_id"},
	}
}

func (t *VaultReadTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.vaultStore == nil || t.workspace == "" {
		return ErrorResult("vault_read not available")
	}

	rawID, _ := args["doc_id"].(string)
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return ErrorResult("doc_id parameter is required")
	}
	docID, err := uuid.Parse(rawID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("invalid doc_id: %v", err))
	}

	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		return ErrorResult("tenant not set in context")
	}

	doc, err := t.vaultStore.GetDocumentByID(ctx, tenantID.String(), docID.String())
	if err != nil || doc == nil {
		// Namespace fallback: the id may belong to a knowledge-graph entity or
		// an episodic summary. Return a redirect error instead of the generic
		// "document not found" so the caller can pick the right tool.
		if redirect := t.namespaceRedirect(ctx, docID.String()); redirect != "" {
			return ErrorResult(redirect)
		}
		return ErrorResult("document not found")
	}

	if !t.allowed(ctx, doc) {
		return ErrorResult("document not accessible in current scope")
	}

	// Text-only gate, layer 1: DocType.
	if doc.DocType == "media" {
		return ErrorResult("vault_read does not support media documents — use read_image/read_audio/read_video/read_document")
	}
	// Text-only gate, layer 2: extension blocklist.
	ext := strings.ToLower(filepath.Ext(doc.Path))
	if _, blocked := nonTextExtensions[ext]; blocked {
		return ErrorResult(fmt.Sprintf("vault_read does not support %s files — use read_image/read_audio/read_video/read_document", ext))
	}

	// Resolve path under tenant workspace root.
	fullPath, err := t.resolvePath(doc.Path)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Determine read cap.
	maxBytes := vaultReadDefaultMaxBytes
	if v, ok := args["max_bytes"].(float64); ok && v > 0 {
		maxBytes = int(v)
	}
	if maxBytes > vaultReadCeilingMaxBytes {
		maxBytes = vaultReadCeilingMaxBytes
	}
	if maxBytes < 1 {
		maxBytes = vaultReadDefaultMaxBytes
	}

	content, truncated, err := readCapped(fullPath, maxBytes)
	if err != nil {
		return ErrorResult(fmt.Sprintf("read failed: %v", err))
	}

	// Text-only gate, layer 3: UTF-8 sniff on first N bytes of content.
	sniffLen := min(len(content), vaultReadUTF8SniffBytes)
	if !utf8.Valid(content[:sniffLen]) {
		return ErrorResult("file content is not valid UTF-8 text — use read_image/read_audio/read_video/read_document for binary files")
	}

	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(doc.Title)
	sb.WriteString(" (")
	sb.WriteString(doc.Path)
	sb.WriteString(")\n\n")
	sb.Write(content)
	if truncated {
		fmt.Fprintf(&sb, "\n\n…[truncated, content exceeds %d bytes]", maxBytes)
	}
	sb.WriteString(t.buildOutlinksFooter(ctx, tenantID.String(), doc.ID))
	return NewResult(sb.String())
}

// namespaceRedirect probes the KG and episodic stores for the given id. When
// a match is found returns a user-facing message telling the caller which tool
// to use; otherwise returns "" (caller falls back to "document not found").
// Store lookup errors are swallowed — fallback is best-effort and must never
// mask the original not-found signal.
func (t *VaultReadTool) namespaceRedirect(ctx context.Context, id string) string {
	if t.kgStore != nil {
		agentID := store.AgentIDFromContext(ctx)
		kgUserID := store.KGUserID(ctx)
		if ent, err := t.kgStore.GetEntity(ctx, agentID.String(), kgUserID, id); err == nil && ent != nil {
			slog.Warn("vault_read.namespace_mismatch", "doc_id", id, "source", "kg")
			return fmt.Sprintf("id %q is a knowledge_graph entity_id, not a vault doc_id — call knowledge_graph_search(entity_id=%q) instead", id, id)
		}
	}
	if t.episodicStore != nil {
		if ep, err := t.episodicStore.Get(ctx, id); err == nil && ep != nil {
			slog.Warn("vault_read.namespace_mismatch", "doc_id", id, "source", "episodic")
			return fmt.Sprintf("id %q is an episodic_id, not a vault doc_id — call memory_expand(id=%q) instead", id, id)
		}
	}
	return ""
}

// buildOutlinksFooter returns a "## Links" section listing scope-accessible
// outlinks (wikilink + reference only), deduped by target, capped at
// vaultReadMaxOutlinks. Returns "" when no accessible links remain or on any
// store error — footer is additive and must not break the read.
func (t *VaultReadTool) buildOutlinksFooter(ctx context.Context, tenantID, docID string) string {
	links, err := t.vaultStore.GetOutLinks(ctx, tenantID, docID)
	if err != nil {
		slog.Warn("vault_read.outlinks.get_failed", "doc_id", docID, "error", err)
		return ""
	}
	if len(links) == 0 {
		return ""
	}

	// Pass 1: type filter + self-link drop + dedup by ToDocID.
	seen := make(map[string]struct{}, len(links))
	filtered := make([]store.VaultLink, 0, len(links))
	for _, l := range links {
		if _, ok := outlinkAllowedTypes[l.LinkType]; !ok {
			continue
		}
		if l.ToDocID == docID {
			continue
		}
		if _, ok := seen[l.ToDocID]; ok {
			continue
		}
		seen[l.ToDocID] = struct{}{}
		filtered = append(filtered, l)
	}
	if len(filtered) == 0 {
		return ""
	}

	// Batch fetch targets.
	ids := make([]string, 0, len(filtered))
	for _, l := range filtered {
		ids = append(ids, l.ToDocID)
	}
	targets, err := t.vaultStore.GetDocumentsByIDs(ctx, tenantID, ids)
	if err != nil {
		slog.Warn("vault_read.outlinks.targets_failed", "doc_id", docID, "error", err)
		return ""
	}
	byID := make(map[string]store.VaultDocument, len(targets))
	for _, d := range targets {
		byID[d.ID] = d
	}

	// Pass 2: missing-target + scope filter, cap.
	type keptLink struct {
		doc      store.VaultDocument
		linkType string
	}
	kept := make([]keptLink, 0, vaultReadMaxOutlinks)
	var overflow int
	for _, l := range filtered {
		target, ok := byID[l.ToDocID]
		if !ok {
			continue
		}
		if !t.allowed(ctx, &target) {
			continue
		}
		if len(kept) >= vaultReadMaxOutlinks {
			overflow++
			continue
		}
		kept = append(kept, keptLink{doc: target, linkType: l.LinkType})
	}
	if len(kept) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n## Links\n")
	for _, k := range kept {
		title := k.doc.Title
		if title == "" {
			title = k.doc.Path
		}
		fmt.Fprintf(&sb, "- %s — id: %s (%s)\n", title, k.doc.ID, k.linkType)
	}
	if overflow > 0 {
		fmt.Fprintf(&sb, "…[%d more links omitted]\n", overflow)
	}
	return sb.String()
}

// allowed enforces the scope matrix:
//   - shared:   allow.
//   - personal: allow iff doc.AgentID == agentID from ctx.
//   - team:     allow iff RunContext.TeamID == *doc.TeamID.
//   - else:     deny (default-deny for unknown scope values).
func (t *VaultReadTool) allowed(ctx context.Context, doc *store.VaultDocument) bool {
	switch doc.Scope {
	case "shared":
		return true
	case "personal":
		if doc.AgentID == nil {
			return false
		}
		aid := store.AgentIDFromContext(ctx)
		if aid == uuid.Nil {
			return false
		}
		return *doc.AgentID == aid.String()
	case "team":
		if doc.TeamID == nil || *doc.TeamID == "" {
			return false
		}
		rc := store.RunContextFromCtx(ctx)
		if rc == nil || rc.TeamID == "" {
			return false
		}
		return rc.TeamID == *doc.TeamID
	default:
		return false
	}
}

// resolvePath joins the tenant workspace with the doc's relative path and
// enforces that the fully-resolved path remains strictly under the workspace.
// Symlinks are resolved via EvalSymlinks for defence-in-depth; if the file is
// missing EvalSymlinks will fail and we fall back to the cleaned join (the
// subsequent os.Open will surface the not-found error naturally).
func (t *VaultReadTool) resolvePath(relPath string) (string, error) {
	wsClean := filepath.Clean(t.workspace)
	joined := filepath.Join(wsClean, filepath.FromSlash(relPath))

	// Resolve symlinks where possible; on error (e.g. file missing) keep the
	// cleaned join — downstream open will surface the true error.
	resolved := joined
	if r, err := filepath.EvalSymlinks(joined); err == nil {
		resolved = r
	}

	// Also resolve the workspace itself so prefix comparison is symlink-safe.
	wsResolved := wsClean
	if r, err := filepath.EvalSymlinks(wsClean); err == nil {
		wsResolved = r
	}

	if !pathUnder(resolved, wsResolved) {
		return "", fmt.Errorf("access denied: document path outside workspace")
	}
	// Defence-in-depth: reject paths whose parent dir is a mutable symlink
	// (TOCTOU rebind risk) and hardlinked targets (nlink > 1).
	// Shared with read_file boundary logic (filesystem_unix.go / _windows.go).
	if hasMutableSymlinkParent(resolved) {
		return "", fmt.Errorf("access denied: document path contains mutable symlink component")
	}
	if err := checkHardlink(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

// pathUnder reports whether child is equal to or strictly under parent.
func pathUnder(child, parent string) bool {
	childClean := filepath.Clean(child)
	parentClean := filepath.Clean(parent)
	if childClean == parentClean {
		return true
	}
	withSep := parentClean + string(os.PathSeparator)
	return strings.HasPrefix(childClean, withSep)
}

// readCapped opens path and reads up to maxBytes; if the file is longer the
// result is truncated and truncated=true. Reads maxBytes+1 via LimitReader to
// detect overflow cheaply without buffering the whole file.
func readCapped(path string, maxBytes int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return nil, false, err
	}
	if len(buf) > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf, false, nil
}
