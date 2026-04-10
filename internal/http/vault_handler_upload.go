package http

import (
	"io"
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

// allowedUploadExts is the whitelist of text-based file extensions accepted by vault upload.
var allowedUploadExts = map[string]bool{
	".md": true, ".txt": true, ".json": true, ".yaml": true, ".yml": true,
	".csv": true, ".toml": true, ".xml": true, ".html": true, ".htm": true,
	".go": true, ".py": true, ".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".rs": true, ".java": true, ".rb": true, ".sh": true, ".sql": true,
	".swift": true, ".kt": true, ".c": true, ".cpp": true, ".h": true,
}

// handleUpload accepts multipart file uploads and registers them in the vault.
// Files are written to the tenant workspace in the appropriate subfolder based on agent/team selection,
// then UpsertDocument + EventVaultDocUpserted triggers the existing enrichment pipeline.
func (h *VaultHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())
	tenantIDStr := tenantID.String()

	// 32 MB in-memory; remainder spills to disk.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}
	defer r.MultipartForm.RemoveAll()

	agentIDStr := r.FormValue("agent_id")
	teamIDStr := r.FormValue("team_id")

	// Validate team membership if provided.
	if teamIDStr != "" {
		if !h.validateTeamMembership(r.Context(), w, teamIDStr) {
			return
		}
	}

	// Resolve agent UUID → agent_key for folder placement.
	var agentKey string
	if agentIDStr != "" {
		if h.agents != nil {
			agents, err := h.agents.List(r.Context(), "")
			if err == nil {
				for _, a := range agents {
					if a.ID.String() == agentIDStr {
						agentKey = a.AgentKey
						break
					}
				}
			}
		}
		if agentKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent not found"})
			return
		}
	}

	// Determine target subfolder and scope.
	var subDir, scope string
	switch {
	case agentIDStr != "":
		subDir = filepath.Join("agents", agentKey)
		scope = "personal"
	case teamIDStr != "":
		subDir = filepath.Join("teams", teamIDStr)
		scope = "team"
	default:
		scope = "shared"
	}

	wsPath := h.resolveTenantWorkspace(r.Context())
	if wsPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace not available"})
		return
	}
	targetDir := filepath.Join(wsPath, subDir)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		slog.Warn("vault.upload: mkdir failed", "dir", targetDir, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create target directory"})
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files provided"})
		return
	}
	if len(files) > 50 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "too many files (max 50)"})
		return
	}

	type uploadResult struct {
		Doc   *store.VaultDocument `json:"document"`
		Error string               `json:"error,omitempty"`
	}

	var results []uploadResult
	var created int

	for _, fh := range files {
		// Sanitize filename — basename only, no path traversal.
		name := filepath.Base(fh.Filename)
		if name == "." || name == ".." || name == "" {
			results = append(results, uploadResult{Error: "invalid filename: " + fh.Filename})
			continue
		}

		// Validate extension.
		ext := strings.ToLower(filepath.Ext(name))
		if !allowedUploadExts[ext] {
			results = append(results, uploadResult{Error: "unsupported file type: " + name})
			continue
		}

		// Enforce per-file size limit (50 MB).
		if fh.Size > 50<<20 {
			results = append(results, uploadResult{Error: "file too large: " + name})
			continue
		}

		// Open uploaded file.
		src, err := fh.Open()
		if err != nil {
			results = append(results, uploadResult{Error: "failed to read: " + name})
			continue
		}

		// Write to workspace.
		dstPath := filepath.Join(targetDir, name)
		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			results = append(results, uploadResult{Error: "failed to write: " + name})
			continue
		}
		_, copyErr := io.Copy(dst, src)
		dst.Close()
		src.Close()
		if copyErr != nil {
			results = append(results, uploadResult{Error: "failed to write: " + name})
			continue
		}

		// Hash the written file.
		hash, err := vault.ContentHashFile(dstPath)
		if err != nil {
			results = append(results, uploadResult{Error: "failed to hash: " + name})
			continue
		}

		// Build workspace-relative path for DB storage.
		relPath := name
		if subDir != "" {
			relPath = subDir + "/" + name
		}

		doc := &store.VaultDocument{
			TenantID:    tenantIDStr,
			Path:        relPath,
			Title:       vault.InferTitle(relPath),
			DocType:     vault.InferDocType(relPath),
			ContentHash: hash,
			Scope:       scope,
		}
		if agentIDStr != "" {
			doc.AgentID = &agentIDStr
		}
		if teamIDStr != "" {
			doc.TeamID = &teamIDStr
		}

		if err := h.store.UpsertDocument(r.Context(), doc); err != nil {
			slog.Warn("vault.upload: upsert failed", "path", relPath, "error", err)
			results = append(results, uploadResult{Error: "failed to register: " + name})
			continue
		}

		// Publish enrichment event (same pattern as rescan.go).
		if h.eventBus != nil {
			agentForEvent := ""
			if agentIDStr != "" {
				agentForEvent = agentIDStr
			}
			h.eventBus.Publish(eventbus.DomainEvent{
				ID:        uuid.Must(uuid.NewV7()).String(),
				Type:      eventbus.EventVaultDocUpserted,
				SourceID:  doc.ID + ":" + hash,
				TenantID:  tenantIDStr,
				AgentID:   agentForEvent,
				Timestamp: time.Now(),
				Payload: eventbus.VaultDocUpsertedPayload{
					DocID:       doc.ID,
					TenantID:    tenantIDStr,
					AgentID:     agentForEvent,
					Path:        relPath,
					ContentHash: hash,
					Workspace:   wsPath,
				},
			})
		}

		results = append(results, uploadResult{Doc: doc})
		created++
	}

	// Signal enrichment progress for WS subscribers.
	if h.enrichProgress != nil && created > 0 {
		h.enrichProgress.Start(created, tenantID)
	}

	slog.Info("vault.upload", "tenant", tenantIDStr, "uploaded", created, "errors", len(files)-created)

	writeJSON(w, http.StatusOK, map[string]any{
		"documents": results,
		"count":     created,
	})
}
