package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxExportSize = 500 << 20 // 500MB

// exportToken holds a short-lived reference to a completed export temp file.
type exportToken struct {
	agentID   string
	userID    string // creator — verified on download
	filePath  string
	fileName  string
	expiresAt time.Time
}

var (
	exportTokenMu   sync.Mutex
	exportTokens    = map[string]*exportToken{}
	exportSweepOnce sync.Once
)

// storeExportToken creates a short-lived token referencing a temp export file.
// Starts a background sweep goroutine on first call (once per process).
func storeExportToken(entityID, userID, filePath, fileName string) string {
	exportSweepOnce.Do(func() {
		go sweepExportTokens()
	})
	token := uuid.Must(uuid.NewV7()).String()
	entry := &exportToken{
		agentID:   entityID,
		userID:    userID,
		filePath:  filePath,
		fileName:  fileName,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	exportTokenMu.Lock()
	exportTokens[token] = entry
	exportTokenMu.Unlock()
	return token
}

// sweepExportTokens runs every 60s, removes expired tokens and their temp files.
func sweepExportTokens() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		exportTokenMu.Lock()
		for tok, e := range exportTokens {
			if now.After(e.expiresAt) {
				os.Remove(e.filePath) //nolint:errcheck
				delete(exportTokens, tok)
			}
		}
		exportTokenMu.Unlock()
	}
}

// ExportManifest describes the archive contents.
type ExportManifest struct {
	Version    int            `json:"version"`
	Format     string         `json:"format"`
	ExportedAt string         `json:"exported_at"`
	ExportedBy string         `json:"exported_by"`
	AgentKey   string         `json:"agent_key"`
	AgentID    string         `json:"agent_id"`
	Sections   map[string]any `json:"sections"`
}

// KGEntityExport is a portable KG entity (no internal UUID).
type KGEntityExport struct {
	ExternalID  string            `json:"external_id"`
	UserID      string            `json:"user_id,omitempty"`
	Name        string            `json:"name"`
	EntityType  string            `json:"entity_type"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Confidence  float64           `json:"confidence"`
}

// KGRelationExport is a portable KG relation using external IDs.
type KGRelationExport struct {
	SourceExternalID string            `json:"source_external_id"`
	TargetExternalID string            `json:"target_external_id"`
	UserID           string            `json:"user_id,omitempty"`
	RelationType     string            `json:"relation_type"`
	Confidence       float64           `json:"confidence"`
	Properties       map[string]string `json:"properties,omitempty"`
}

// MemoryExport is a portable memory document.
type MemoryExport struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	UserID  string `json:"user_id,omitempty"`
}

// handleExportPreview returns lightweight counts per exportable section.
func (h *AgentsHandler) handleExportPreview(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	if !h.canExport(ag, userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
		return
	}

	counts, err := pg.ExportPreviewCounts(r.Context(), h.db, ag.ID)
	if err != nil {
		slog.Error("agents.export.preview", "agent_id", ag.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "failed to fetch preview counts"))
		return
	}

	// Count workspace files (filesystem, not DB)
	var workspaceFiles int
	if ag.Workspace != "" {
		wsPath := config.ExpandHome(ag.Workspace)
		if info, statErr := os.Stat(wsPath); statErr == nil && info.IsDir() {
			filepath.WalkDir(wsPath, func(_ string, d fs.DirEntry, _ error) error { //nolint:errcheck
				if d.IsDir() || strings.HasPrefix(d.Name(), ".") || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				if fi, err := d.Info(); err == nil && fi.Size() <= maxWorkspaceFileSize {
					workspaceFiles++
				}
				return nil
			})
		}
	}

	type previewResponse struct {
		*pg.ExportPreview
		WorkspaceFiles int `json:"workspace_files"`
	}
	writeJSON(w, http.StatusOK, previewResponse{ExportPreview: counts, WorkspaceFiles: workspaceFiles})
}

// handleExport dispatches to SSE streaming or direct download based on ?stream= param.
func (h *AgentsHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	if !h.canExport(ag, userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
		return
	}

	sections := parseExportSections(r.URL.Query().Get("sections"))
	stream := r.URL.Query().Get("stream") == "true"

	if stream {
		h.handleExportSSE(w, r, ag, sections)
	} else {
		h.handleExportDirect(w, r, ag, sections)
	}
}

// handleExportDownload serves a previously-prepared export archive by token.
func (h *AgentsHandler) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "token"))
		return
	}

	exportTokenMu.Lock()
	entry, ok := exportTokens[token]
	if ok && time.Now().After(entry.expiresAt) {
		delete(exportTokens, token)
		ok = false
	}
	exportTokenMu.Unlock()

	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "export token", token))
		return
	}

	// Verify token belongs to requesting user (or system owner)
	if entry.userID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "export download"))
		return
	}

	f, err := os.Open(entry.filePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError))
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, entry.fileName))
	io.Copy(w, f) //nolint:errcheck
}

// handleExportSSE streams build progress as SSE events then sends a download token on completion.
func (h *AgentsHandler) handleExportSSE(w http.ResponseWriter, r *http.Request, ag *store.AgentData, sections map[string]bool) {
	flusher := initSSE(w)
	if flusher == nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
		return
	}

	tmpFile, err := os.CreateTemp("", "goclaw-export-*.tar.gz")
	if err != nil {
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
		return
	}
	tmpPath := tmpFile.Name()

	progressFn := func(ev ProgressEvent) {
		sendSSE(w, flusher, "progress", ev)
	}

	buildErr := h.writeExportArchive(r.Context(), tmpFile, ag, sections, progressFn)
	tmpFile.Close()

	if buildErr != nil {
		slog.Error("agents.export.sse", "agent_id", ag.ID, "error", buildErr)
		sendSSE(w, flusher, "error", ProgressEvent{Phase: "archive", Status: "error", Detail: buildErr.Error()})
		os.Remove(tmpPath)
		return
	}

	userID := store.UserIDFromContext(r.Context())
	token := h.generateExportToken(ag.ID.String(), userID, tmpPath, exportFileName(ag.AgentKey))
	sendSSE(w, flusher, "complete", map[string]string{
		"download_url": "/v1/agents/" + ag.ID.String() + "/export/download/" + token,
	})
}

// handleExportDirect streams the tar.gz archive directly to the response body.
func (h *AgentsHandler) handleExportDirect(w http.ResponseWriter, r *http.Request, ag *store.AgentData, sections map[string]bool) {
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, exportFileName(ag.AgentKey)))

	if err := h.writeExportArchive(r.Context(), w, ag, sections, nil); err != nil {
		// Headers already written; log only — cannot send JSON error at this point.
		slog.Error("agents.export.direct", "agent_id", ag.ID, "error", err)
	}
}

// writeExportArchive builds a tar.gz archive into w, calling progressFn after each section.
// progressFn may be nil (direct mode).
func (h *AgentsHandler) writeExportArchive(ctx context.Context, w io.Writer, ag *store.AgentData, sections map[string]bool, progressFn func(ProgressEvent)) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	gw := gzip.NewWriter(lw)
	tw := tar.NewWriter(gw)

	manifest := &ExportManifest{
		Version:    1,
		Format:     "goclaw-agent-export",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		ExportedBy: store.UserIDFromContext(ctx),
		AgentKey:   ag.AgentKey,
		AgentID:    ag.ID.String(),
		Sections:   make(map[string]any),
	}

	// Section: config (always included)
	agentJSON, err := marshalAgentConfig(ag)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal agent config: %w", err)
	}
	if err := addToTar(tw, "agent.json", agentJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write agent.json: %w", err)
	}
	manifest.Sections["config"] = map[string]int{"count": 1}
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "done", Current: 1, Total: 1})
	}

	// Section: context_files (agent-level + per-user)
	if sections["context_files"] {
		files, err := pg.ExportAgentContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export context_files: %w", err)
		}
		for i, f := range files {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "context_files", Status: "running", Current: i + 1, Total: len(files)})
			}
			if err := addToTar(tw, "context_files/"+sanitizeName(f.FileName), []byte(f.Content)); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write context file %s: %w", f.FileName, err)
			}
		}
		manifest.Sections["context_files"] = map[string]int{"count": len(files)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "context_files", Status: "done", Current: len(files), Total: len(files), Detail: fmt.Sprintf("%d files", len(files))})
		}

		userFiles, err := pg.ExportUserContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export user_context_files: %w", err)
		}
		for i, f := range userFiles {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "user_context_files", Status: "running", Current: i + 1, Total: len(userFiles)})
			}
			path := "user_context_files/" + sanitizeName(f.UserID) + "/" + sanitizeName(f.FileName)
			if err := addToTar(tw, path, []byte(f.Content)); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user context file %s: %w", f.FileName, err)
			}
		}
		manifest.Sections["user_context_files"] = map[string]int{"count": len(userFiles)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_context_files", Status: "done", Current: len(userFiles), Total: len(userFiles), Detail: fmt.Sprintf("%d files", len(userFiles))})
		}
	}

	// Section: memory
	if sections["memory"] {
		docs, err := pg.ExportMemoryDocuments(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export memory: %w", err)
		}

		globalDocs := make([]MemoryExport, 0)
		perUser := make(map[string][]MemoryExport)
		for _, d := range docs {
			me := MemoryExport{Path: d.Path, Content: d.Content, UserID: d.UserID}
			if d.UserID == "" {
				globalDocs = append(globalDocs, me)
			} else {
				perUser[d.UserID] = append(perUser[d.UserID], me)
			}
		}

		if len(globalDocs) > 0 {
			data, err := marshalJSONL(globalDocs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal global memory: %w", err)
			}
			if err := addToTar(tw, "memory/global.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write memory/global.jsonl: %w", err)
			}
		}
		for uid, udocs := range perUser {
			data, err := marshalJSONL(udocs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal memory for user %s: %w", uid, err)
			}
			if err := addToTar(tw, "memory/users/"+sanitizeName(uid)+".jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write memory/users/%s.jsonl: %w", uid, err)
			}
		}
		manifest.Sections["memory"] = map[string]int{
			"global":   len(globalDocs),
			"per_user": len(docs) - len(globalDocs),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "memory", Status: "done", Current: len(docs), Total: len(docs), Detail: fmt.Sprintf("%d docs", len(docs))})
		}
	}

	// Section: knowledge_graph
	if sections["knowledge_graph"] {
		entities, err := pg.ExportKGEntities(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export kg entities: %w", err)
		}

		// Build internal-id → external_id map for relation remapping
		idToExternal := make(map[string]string, len(entities))
		exportEntities := make([]KGEntityExport, 0, len(entities))
		for _, e := range entities {
			idToExternal[e.ID] = e.ExternalID
			exportEntities = append(exportEntities, KGEntityExport{
				ExternalID:  e.ExternalID,
				UserID:      e.UserID,
				Name:        e.Name,
				EntityType:  e.EntityType,
				Description: e.Description,
				Properties:  e.Properties,
				Confidence:  e.Confidence,
			})
		}

		if len(exportEntities) > 0 {
			data, err := marshalJSONL(exportEntities)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal kg entities: %w", err)
			}
			if err := addToTar(tw, "knowledge_graph/entities.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write kg entities: %w", err)
			}
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph_entities", Status: "done", Current: len(entities), Total: len(entities), Detail: fmt.Sprintf("%d entities", len(entities))})
		}

		relations, err := pg.ExportKGRelations(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export kg relations: %w", err)
		}

		exportRelations := make([]KGRelationExport, 0, len(relations))
		for _, rel := range relations {
			exportRelations = append(exportRelations, KGRelationExport{
				SourceExternalID: idToExternal[rel.SourceEntityID],
				TargetExternalID: idToExternal[rel.TargetEntityID],
				UserID:           rel.UserID,
				RelationType:     rel.RelationType,
				Confidence:       rel.Confidence,
				Properties:       rel.Properties,
			})
		}

		if len(exportRelations) > 0 {
			data, err := marshalJSONL(exportRelations)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal kg relations: %w", err)
			}
			if err := addToTar(tw, "knowledge_graph/relations.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write kg relations: %w", err)
			}
		}
		manifest.Sections["knowledge_graph"] = map[string]int{
			"entities":  len(entities),
			"relations": len(relations),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph_relations", Status: "done", Current: len(relations), Total: len(relations), Detail: fmt.Sprintf("%d relations", len(relations))})
		}
	}

	// Section: cron
	if sections["cron"] {
		jobs, qErr := pg.ExportCronJobs(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query cron jobs", "agent", ag.AgentKey, "error", qErr)
		}
		if len(jobs) > 0 {
			data, err := marshalJSONL(jobs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal cron jobs: %w", err)
			}
			if err := addToTar(tw, "cron/jobs.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write cron/jobs.jsonl: %w", err)
			}
		}
		manifest.Sections["cron"] = map[string]int{"count": len(jobs)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "cron", Status: "done", Detail: fmt.Sprintf("%d jobs", len(jobs))})
		}
	}

	// Section: user_profiles
	if sections["user_profiles"] {
		profiles, qErr := pg.ExportUserProfiles(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query user profiles", "agent", ag.AgentKey, "error", qErr)
		}
		if len(profiles) > 0 {
			data, err := marshalJSONL(profiles)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal user profiles: %w", err)
			}
			if err := addToTar(tw, "user_profiles.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user_profiles.jsonl: %w", err)
			}
		}
		manifest.Sections["user_profiles"] = map[string]int{"count": len(profiles)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_profiles", Status: "done", Detail: fmt.Sprintf("%d profiles", len(profiles))})
		}
	}

	// Section: user_overrides
	if sections["user_overrides"] {
		overrides, qErr := pg.ExportUserOverrides(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query user overrides", "agent", ag.AgentKey, "error", qErr)
		}
		if len(overrides) > 0 {
			data, err := marshalJSONL(overrides)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal user overrides: %w", err)
			}
			if err := addToTar(tw, "user_overrides.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user_overrides.jsonl: %w", err)
			}
		}
		manifest.Sections["user_overrides"] = map[string]int{"count": len(overrides)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_overrides", Status: "done", Detail: fmt.Sprintf("%d overrides", len(overrides))})
		}
	}

	// Section: workspace files
	if sections["workspace"] && ag.Workspace != "" {
		wsPath := config.ExpandHome(ag.Workspace)
		fileCount, totalBytes, wsErr := h.exportWorkspaceFiles(ctx, tw, wsPath, progressFn)
		if wsErr != nil {
			slog.Warn("export: workspace walk failed", "path", wsPath, "error", wsErr)
		}
		manifest.Sections["workspace"] = map[string]any{"file_count": fileCount, "total_bytes": totalBytes}
	}

	// Manifest last — has accurate final counts
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addToTar(tw, "manifest.json", manifestJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return fmt.Errorf("close tar: %w", err)
	}
	return gw.Close()
}

// canExport checks if userID has permission to export the given agent.
func (h *AgentsHandler) canExport(ag *store.AgentData, userID string) bool {
	if ag.OwnerID == userID {
		return true
	}
	if h.isOwnerUser(userID) {
		return true
	}
	// TODO: tenant admin check
	return false
}

// generateExportToken is an alias for storeExportToken (backward compat within AgentsHandler).
func (h *AgentsHandler) generateExportToken(entityID, userID, filePath, fileName string) string {
	return storeExportToken(entityID, userID, filePath, fileName)
}

// addToTar adds a single file to the tar archive with a standard header.
func addToTar(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// marshalJSONL encodes items as newline-delimited JSON (one object per line).
func marshalJSONL[T any](items []T) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return nil, err
		}
	}
	return []byte(sb.String()), nil
}

// marshalAgentConfig serializes an agent with sensitive fields (tenant_id, owner_id) stripped.
func marshalAgentConfig(ag *store.AgentData) ([]byte, error) {
	type exportableAgent struct {
		AgentKey          string          `json:"agent_key"`
		DisplayName       string          `json:"display_name,omitempty"`
		Frontmatter       string          `json:"frontmatter,omitempty"`
		Provider          string          `json:"provider"`
		Model             string          `json:"model"`
		ContextWindow     int             `json:"context_window"`
		MaxToolIterations int             `json:"max_tool_iterations"`
		AgentType         string          `json:"agent_type"`
		Status            string          `json:"status"`
		ToolsConfig       json.RawMessage `json:"tools_config,omitempty"`
		SandboxConfig     json.RawMessage `json:"sandbox_config,omitempty"`
		SubagentsConfig   json.RawMessage `json:"subagents_config,omitempty"`
		MemoryConfig      json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig  json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning    json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig       json.RawMessage `json:"other_config,omitempty"`
	}
	return json.MarshalIndent(exportableAgent{
		AgentKey:          ag.AgentKey,
		DisplayName:       ag.DisplayName,
		Frontmatter:       ag.Frontmatter,
		Provider:          ag.Provider,
		Model:             ag.Model,
		ContextWindow:     ag.ContextWindow,
		MaxToolIterations: ag.MaxToolIterations,
		AgentType:         ag.AgentType,
		Status:            ag.Status,
		ToolsConfig:       ag.ToolsConfig,
		SandboxConfig:     ag.SandboxConfig,
		SubagentsConfig:   ag.SubagentsConfig,
		MemoryConfig:      ag.MemoryConfig,
		CompactionConfig:  ag.CompactionConfig,
		ContextPruning:    ag.ContextPruning,
		OtherConfig:       ag.OtherConfig,
	}, "", "  ")
}

// parseExportSections parses the ?sections= query param.
// Defaults to config + context_files when empty.
func parseExportSections(raw string) map[string]bool {
	if raw == "" {
		return map[string]bool{"config": true, "context_files": true}
	}
	out := make(map[string]bool)
	for s := range strings.SplitSeq(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// exportFileName builds the tar.gz filename for a given agent key.
// Strips characters unsafe for Content-Disposition headers.
func exportFileName(agentKey string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, agentKey)
	return fmt.Sprintf("agent-%s-%s.tar.gz", safe, time.Now().UTC().Format("20060102"))
}

// jsonIndent marshals v to indented JSON bytes.
func jsonIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// sanitizeName replaces characters that could cause path traversal in tar entries.
// Use for single-segment names (file names, agent keys). NOT for relative paths with slashes.
func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "_", "..", "__", "\\", "_")
	return r.Replace(name)
}

// sanitizeRelPath sanitizes each segment of a relative path while preserving directory structure.
// Removes ".." traversal and backslashes but keeps forward slashes between segments.
func sanitizeRelPath(relPath string) string {
	parts := strings.Split(relPath, "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		clean = append(clean, strings.ReplaceAll(p, "\\", "_"))
	}
	return strings.Join(clean, "/")
}

// limitedWriter wraps an io.Writer and returns an error once limit bytes are exceeded.
type limitedWriter struct {
	w       io.Writer
	written int64
	limit   int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written+int64(len(p)) > lw.limit {
		return 0, errors.New("export size limit exceeded")
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}
