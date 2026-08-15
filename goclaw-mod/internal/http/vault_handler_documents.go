package http

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// writeDocumentContent writes the given content bytes to a tenant-workspace-
// relative path, after validating the path stays inside the workspace and uses
// an allowed extension. Returns the SHA-256 content hash on success.
//
// This is the shared writer used by handleCreateDocument and handleUpdateDocument
// to materialise inline JSON `content` payloads on disk, so the enrichment
// pipeline (which reads files by path) can later compute summaries, embeddings
// and links.
func (h *VaultHandler) writeDocumentContent(workspace, relPath string, content []byte) (string, error) {
	// Ensure the tenant workspace exists before resolving symlinks. Non-master
	// tenants live under workspace/tenants/{slug}/ which handleUpload creates on
	// demand (see vault_handler_upload.go) — without this, the very first JSON
	// content write into a fresh tenant would fail EvalSymlinks with ErrNotExist
	// and 500 the request.
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return "", err
	}

	// Resolve the workspace root through any symlinks so all containment checks
	// compare canonical paths.
	realWS, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", err
	}
	target := filepath.Join(realWS, filepath.Clean(relPath))

	// Lexical containment: blocks "../" and absolute-path escapes.
	if target != realWS && !strings.HasPrefix(target, realWS+string(os.PathSeparator)) {
		slog.Warn("security.vault_symlink_escape",
			"site", "path_traversal", "attempted", target, "workspace", realWS)
		return "", os.ErrInvalid
	}

	// Symlink containment: a lexical prefix check is not enough — a symlink that
	// lives *inside* the workspace but points outside it (e.g. workspace/link ->
	// /etc) would otherwise let the write follow it and clobber a file beyond
	// the workspace. Resolve the deepest already-existing ancestor of the target
	// and confirm it still canonicalises to a path inside the workspace, before
	// any directory is created or byte written.
	for ancestor := filepath.Dir(target); ; {
		resolved, rerr := filepath.EvalSymlinks(ancestor)
		if rerr == nil {
			if resolved != realWS && !strings.HasPrefix(resolved, realWS+string(os.PathSeparator)) {
				slog.Warn("security.vault_symlink_escape",
					"site", "ancestor", "resolved", resolved, "workspace", realWS)
				return "", os.ErrInvalid
			}
			break
		}
		if !os.IsNotExist(rerr) {
			return "", rerr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break // walked to the filesystem root without an existing ancestor
		}
		ancestor = parent
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}

	// Fast-fail when the final component is already a symlink — gives a clean
	// rejection + security log without parsing OS-specific error codes. The
	// O_NOFOLLOW open below closes the TOCTOU window this Lstat leaves open
	// (an attacker could swap the file for a symlink between calls).
	if fi, lerr := os.Lstat(target); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		slog.Warn("security.vault_symlink_escape",
			"site", "final_lstat", "target", target, "workspace", realWS)
		return "", os.ErrInvalid
	}

	// O_NOFOLLOW makes the kernel refuse to follow a symlink at the final
	// component atomically with the open, removing the Lstat→Open race entirely
	// on Unix. On Windows oNoFollow == 0 and the Lstat above is best-effort.
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|oNoFollow, 0o644)
	if err != nil {
		if isSymlinkLoopErr(err) {
			slog.Warn("security.vault_symlink_escape",
				"site", "open_nofollow", "target", target, "workspace", realWS)
			return "", os.ErrInvalid
		}
		return "", err
	}
	if _, werr := f.Write(content); werr != nil {
		_ = f.Close() // best-effort cleanup; the write error is the real one
		return "", werr
	}
	// Explicit Close so a delayed FS error (e.g. disk-full at flush) surfaces
	// instead of being silently swallowed by a deferred close.
	if cerr := f.Close(); cerr != nil {
		return "", cerr
	}
	return vault.ContentHash(content), nil
}

// publishDocUpserted enqueues the standard EventVaultDocUpserted so the
// enrichment worker re-runs summary/embedding/link extraction for this doc.
// No-op when the event bus is not wired (some test setups).
func (h *VaultHandler) publishDocUpserted(tenantID uuid.UUID, agentID, docID, relPath, hash, workspace string) {
	if h.eventBus == nil {
		return
	}
	tenantIDStr := tenantID.String()
	// Start enrichment progress before publishing so the worker pool can't drain
	// the event and finish before the progress tracker registers it — same
	// ordering handleUpload relies on to avoid that race.
	if h.enrichProgress != nil {
		h.enrichProgress.Start(1, tenantID)
	}
	h.eventBus.Publish(eventbus.DomainEvent{
		ID:        uuid.Must(uuid.NewV7()).String(),
		Type:      eventbus.EventVaultDocUpserted,
		SourceID:  docID + ":" + hash,
		TenantID:  tenantIDStr,
		AgentID:   agentID,
		Timestamp: time.Now(),
		Payload: eventbus.VaultDocUpsertedPayload{
			DocID:       docID,
			TenantID:    tenantIDStr,
			AgentID:     agentID,
			Path:        relPath,
			ContentHash: hash,
			Workspace:   workspace,
		},
	})
}

// handleListAllDocuments lists vault documents across all agents in tenant.
// Optional query param agent_id to filter by specific agent.
func (h *VaultHandler) handleListAllDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.URL.Query().Get("agent_id")
	opts := h.parseListOpts(r)

	// Validate team membership if specific team requested.
	if opts.TeamID != nil && *opts.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, *opts.TeamID) {
			return
		}
	}
	// Non-owner without team_id filter: show personal + user's teams.
	if opts.TeamID == nil && !store.IsOwnerRole(r.Context()) {
		h.applyNonOwnerTeamScope(r.Context(), &opts)
	}

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list_all failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	total, cntErr := h.store.CountDocuments(r.Context(), tenantID.String(), agentID, opts)
	if cntErr != nil {
		slog.Warn("vault.count failed", "error", cntErr)
	}
	writeJSON(w, http.StatusOK, vaultDocListResponse{Documents: docs, Total: total})
}

// handleListDocuments lists vault documents for a specific agent.
func (h *VaultHandler) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	opts := h.parseListOpts(r)

	if opts.TeamID != nil && *opts.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, *opts.TeamID) {
			return
		}
	}
	if opts.TeamID == nil && !store.IsOwnerRole(r.Context()) {
		h.applyNonOwnerTeamScope(r.Context(), &opts)
	}

	docs, err := h.store.ListDocuments(r.Context(), tenantID.String(), agentID, opts)
	if err != nil {
		slog.Warn("vault.list failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if docs == nil {
		docs = []store.VaultDocument{}
	}
	total, cntErr := h.store.CountDocuments(r.Context(), tenantID.String(), agentID, opts)
	if cntErr != nil {
		slog.Warn("vault.count failed", "error", cntErr)
	}
	writeJSON(w, http.StatusOK, vaultDocListResponse{Documents: docs, Total: total})
}

// handleGetDocument returns a single vault document by ID, scoped to the agent.
func (h *VaultHandler) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	doc, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil {
		slog.Warn("vault.get failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if doc == nil || (agentID != "" && doc.AgentID != nil && *doc.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	// Verify team boundary — non-owner must be team member to view team docs.
	if doc.TeamID != nil && *doc.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *doc.TeamID) {
			return
		}
	}
	writeJSON(w, http.StatusOK, doc)
}

// handleCreateDocument creates a new vault document.
//
// `content` semantics — symmetric with handleUpdateDocument:
//   - field omitted (nil pointer): metadata-only stub, no file write, no event
//     (typically followed by a rescan that materialises the file from disk).
//   - field present and non-empty: bytes materialised at <tenant-workspace>/<path>,
//     SHA-256 hash stored on the row, EventVaultDocUpserted emitted so the
//     enrichment worker computes summary/embeddings/links — same code path the
//     multipart /v1/vault/upload endpoint and the filesystem rescan use.
//   - field present and empty (""): a 0-byte file is written and the event still
//     fires. Same behaviour as PUT — pick the right shape on the client side.
func (h *VaultHandler) handleCreateDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")

	var body struct {
		Path     string         `json:"path"`
		Title    string         `json:"title"`
		Content  *string        `json:"content"` // nil=no write; ""=write empty file; "data"=write bytes (mirrors PUT)
		DocType  string         `json:"doc_type"`
		Scope    string         `json:"scope"`
		TeamID   string         `json:"team_id"`
		Metadata map[string]any `json:"metadata"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}
	if body.Path == "" || body.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path and title are required"})
		return
	}
	if strings.Contains(body.Path, "..") || strings.HasPrefix(body.Path, "/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}
	if body.DocType == "" {
		body.DocType = "note"
	}
	if body.Scope == "" {
		body.Scope = "personal"
	}
	if !validDocType(body.DocType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid doc_type"})
		return
	}
	if !validScope(body.Scope) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid scope"})
		return
	}
	// Validate extension upfront if caller wants to write content — matches the
	// whitelist enforced by /v1/vault/upload so the two endpoints accept the
	// same file types. nil pointer = "no content field", which skips the check.
	if body.Content != nil {
		ext := strings.ToLower(filepath.Ext(body.Path))
		if !allowedUploadExts[ext] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported file type: " + ext})
			return
		}
	}

	doc := &store.VaultDocument{
		TenantID: tenantID.String(),
		Path:     body.Path,
		Title:    body.Title,
		DocType:  body.DocType,
		Scope:    body.Scope,
		Metadata: body.Metadata,
	}
	if agentID != "" {
		doc.AgentID = &agentID
	} else if doc.Scope == "personal" {
		doc.Scope = "shared" // no agent → shared scope
	}
	if body.TeamID != "" {
		if !h.validateTeamMembership(r.Context(), w, body.TeamID) {
			return
		}
		doc.TeamID = &body.TeamID
		if body.Scope == "personal" {
			doc.Scope = "team"
		}
	}

	// Persist file content if provided, before the DB upsert so the
	// content_hash column is set in a single write.
	var (
		wsPath        string
		writtenHash   string
		contentWasSet bool
	)
	if body.Content != nil {
		wsPath = h.resolveTenantWorkspace(r.Context())
		if wsPath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
			return
		}
		hash, werr := h.writeDocumentContent(wsPath, body.Path, []byte(*body.Content))
		if werr != nil {
			slog.Warn("vault.create: write content failed", "path", body.Path, "error", werr)
			if errors.Is(werr, os.ErrInvalid) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path escapes workspace"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write content"})
			return
		}
		doc.ContentHash = hash
		writtenHash = hash
		contentWasSet = true
	}

	if err := h.store.UpsertDocument(r.Context(), doc); err != nil {
		slog.Warn("vault.create failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Fire enrichment after the DB row exists so the worker can locate the doc.
	if contentWasSet {
		evAgent := ""
		if doc.AgentID != nil {
			evAgent = *doc.AgentID
		}
		h.publishDocUpserted(tenantID, evAgent, doc.ID, body.Path, writtenHash, wsPath)
	}

	// Re-fetch by ID (set via RETURNING) — unambiguous even when same path exists across teams.
	created, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), doc.ID)
	if created != nil {
		writeJSON(w, http.StatusCreated, created)
	} else {
		writeJSON(w, http.StatusCreated, doc)
	}
}

// handleUpdateDocument updates an existing vault document.
func (h *VaultHandler) handleUpdateDocument(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	existing, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil || existing == nil || (agentID != "" && existing.AgentID != nil && *existing.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}
	// Verify team boundary before any metadata or content rewrite. PUT can
	// materialize bytes on disk, so it must match GET/DELETE team access rules.
	if existing.TeamID != nil && *existing.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *existing.TeamID) {
			return
		}
	}

	var body struct {
		Title    *string        `json:"title"`
		Content  *string        `json:"content"` // nil=no change; "" clears the file; non-empty rewrites it
		DocType  *string        `json:"doc_type"`
		Scope    *string        `json:"scope"`
		TeamID   *string        `json:"team_id"` // nil=no change, ""=clear, "uuid"=set
		Metadata map[string]any `json:"metadata"`
	}
	if !bindJSON(w, r, locale, &body) {
		return
	}

	if body.Title != nil {
		existing.Title = *body.Title
	}
	if body.DocType != nil {
		existing.DocType = *body.DocType
	}
	if body.Scope != nil {
		existing.Scope = *body.Scope
	}
	if body.TeamID != nil {
		// Only owner/admin can change team assignment.
		if !store.IsOwnerRole(r.Context()) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "only owner can change document team assignment"})
			return
		}
		if *body.TeamID == "" {
			existing.TeamID = nil
			existing.Scope = "personal"
		} else {
			existing.TeamID = body.TeamID
			existing.Scope = "team"
		}
	}
	if body.Metadata != nil {
		existing.Metadata = body.Metadata
	}

	// Rewrite content on disk when caller supplied a `content` field.
	var (
		wsPath        string
		writtenHash   string
		contentWasSet bool
	)
	if body.Content != nil {
		ext := strings.ToLower(filepath.Ext(existing.Path))
		if !allowedUploadExts[ext] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported file type: " + ext})
			return
		}
		wsPath = h.resolveTenantWorkspace(r.Context())
		if wsPath == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
			return
		}
		hash, werr := h.writeDocumentContent(wsPath, existing.Path, []byte(*body.Content))
		if werr != nil {
			slog.Warn("vault.update: write content failed", "path", existing.Path, "error", werr)
			if errors.Is(werr, os.ErrInvalid) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path escapes workspace"})
				return
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write content"})
			return
		}
		existing.ContentHash = hash
		writtenHash = hash
		contentWasSet = true
	}

	if err := h.store.UpsertDocument(r.Context(), existing); err != nil {
		slog.Warn("vault.update failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if contentWasSet {
		evAgent := ""
		if existing.AgentID != nil {
			evAgent = *existing.AgentID
		}
		h.publishDocUpserted(tenantID, evAgent, existing.ID, existing.Path, writtenHash, wsPath)
	}

	updated, _ := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if updated != nil {
		writeJSON(w, http.StatusOK, updated)
	} else {
		writeJSON(w, http.StatusOK, existing)
	}
}

// handleDeleteDocument deletes a vault document by ID.
func (h *VaultHandler) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	agentID := r.PathValue("agentID")
	docID := r.PathValue("docID")

	existing, err := h.store.GetDocumentByID(r.Context(), tenantID.String(), docID)
	if err != nil || existing == nil || (agentID != "" && existing.AgentID != nil && *existing.AgentID != agentID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "document not found"})
		return
	}

	// Verify team boundary before deletion.
	if existing.TeamID != nil && *existing.TeamID != "" && !store.IsOwnerRole(r.Context()) {
		if !h.validateTeamMembership(r.Context(), w, *existing.TeamID) {
			return
		}
	}

	// DeleteDocument without RunContext applies no team_id filter (broad match on tenant+agent+path).
	// This is safe because we pre-validated team membership above and use server-derived existing.Path.
	// Use the doc's actual agent_id (may be empty for team/shared docs).
	deleteAgentID := ""
	if existing.AgentID != nil {
		deleteAgentID = *existing.AgentID
	}
	if err := h.store.DeleteDocument(r.Context(), tenantID.String(), deleteAgentID, existing.Path); err != nil {
		slog.Warn("vault.delete failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
