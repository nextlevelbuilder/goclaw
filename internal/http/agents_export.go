package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// maxImportBodySize limits the import request body to 10 MB.
const maxImportBodySize = 10 << 20

// AgentExport is the portable JSON format for agent export/import.
type AgentExport struct {
	Version        int                 `json:"version"`
	ExportedAt     string              `json:"exported_at"`
	Agent          *AgentExportData    `json:"agent,omitempty"`
	ContextFiles   []ContextFileExport `json:"context_files,omitempty"`
	Memories       []MemoryExport      `json:"memories,omitempty"`
	KnowledgeGraph *KGExport           `json:"knowledge_graph,omitempty"`
}

// AgentExportData contains the agent configuration (no IDs, no tenant info).
type AgentExportData struct {
	DisplayName        string          `json:"display_name"`
	AgentKey           string          `json:"agent_key"`
	Frontmatter        string          `json:"frontmatter,omitempty"`
	AgentType          string          `json:"agent_type"`
	Provider           string          `json:"provider,omitempty"`
	Model              string          `json:"model,omitempty"`
	ContextWindow      int             `json:"context_window,omitempty"`
	MaxToolIterations  int             `json:"max_tool_iterations,omitempty"`
	ToolsConfig        json.RawMessage `json:"tools_config,omitempty"`
	SandboxConfig      json.RawMessage `json:"sandbox_config,omitempty"`
	SubagentsConfig    json.RawMessage `json:"subagents_config,omitempty"`
	MemoryConfig       json.RawMessage `json:"memory_config,omitempty"`
	CompactionConfig   json.RawMessage `json:"compaction_config,omitempty"`
	ContextPruning     json.RawMessage `json:"context_pruning,omitempty"`
	OtherConfig        json.RawMessage `json:"other_config,omitempty"`
	BudgetMonthlyCents *int            `json:"budget_monthly_cents,omitempty"`
}

// ContextFileExport holds one context file for export.
type ContextFileExport struct {
	FileName string `json:"file_name"`
	Content  string `json:"content"`
}

// MemoryExport holds one memory document for export (global only, no per-user data).
type MemoryExport struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// KGExport holds knowledge graph entities and relations for export.
type KGExport struct {
	Entities  []KGEntityExport   `json:"entities,omitempty"`
	Relations []KGRelationExport `json:"relations,omitempty"`
}

// KGEntityExport is a portable entity (uses external_id as reference, no internal UUIDs).
type KGEntityExport struct {
	ExternalID  string            `json:"external_id"`
	UserID      string            `json:"user_id,omitempty"`
	Name        string            `json:"name"`
	EntityType  string            `json:"entity_type"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Confidence  float64           `json:"confidence"`
}

// KGRelationExport is a portable relation (references entities by external_id).
type KGRelationExport struct {
	SourceExternalID string            `json:"source_external_id"`
	TargetExternalID string            `json:"target_external_id"`
	RelationType     string            `json:"relation_type"`
	Confidence       float64           `json:"confidence"`
	Properties       map[string]string `json:"properties,omitempty"`
}

// Export query params:
//
//	?include=context_files,memory,knowledge_graph  (comma-separated, default: context_files)
func (h *AgentsHandler) handleExport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		ag, err2 := h.agents.GetByKey(r.Context(), r.PathValue("id"))
		if err2 != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", r.PathValue("id"))})
			return
		}
		id = ag.ID
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}

	// Block export of incomplete agents
	if ag.Status == store.AgentStatusSummoning {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "agent is still being summoned"})
		return
	}

	// Access check: owner or system owner or shared user
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		if ok, _, _ := h.agents.CanAccess(r.Context(), id, userID); !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgNoAccess, "agent")})
			return
		}
	}
	isOwner := ag.OwnerID == userID || h.isOwnerUser(userID)

	// Parse include options
	includeParam := r.URL.Query().Get("include")
	includes := map[string]bool{"context_files": true} // default
	if includeParam != "" {
		includes = map[string]bool{}
		for _, part := range strings.Split(includeParam, ",") {
			includes[strings.TrimSpace(part)] = true
		}
	}

	export := AgentExport{
		Version:    1,
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Only include agent config when context_files is selected (full agent export for creating new agents).
	// Data-only exports (memory, KG) don't need agent metadata.
	if includes["context_files"] {
		export.Agent = &AgentExportData{
			DisplayName:        ag.DisplayName,
			AgentKey:           ag.AgentKey,
			Frontmatter:        ag.Frontmatter,
			AgentType:          ag.AgentType,
			Provider:           ag.Provider,
			Model:              ag.Model,
			ContextWindow:      ag.ContextWindow,
			MaxToolIterations:  ag.MaxToolIterations,
			ToolsConfig:        ag.ToolsConfig,
			SandboxConfig:      ag.SandboxConfig,
			SubagentsConfig:    ag.SubagentsConfig,
			MemoryConfig:       ag.MemoryConfig,
			CompactionConfig:   ag.CompactionConfig,
			ContextPruning:     ag.ContextPruning,
			OtherConfig:        ag.OtherConfig,
			BudgetMonthlyCents: ag.BudgetMonthlyCents,
		}
	}

	// Context files
	if includes["context_files"] {
		files, fErr := h.agents.GetAgentContextFiles(r.Context(), id)
		if fErr == nil {
			for _, f := range files {
				export.ContextFiles = append(export.ContextFiles, ContextFileExport{
					FileName: f.FileName,
					Content:  f.Content,
				})
			}
		}
	}

	// Memory documents — owner-only, global memories only (no per-user data leak)
	if includes["memory"] && h.memoryStore != nil && isOwner {
		docs, mErr := h.memoryStore.ListAllDocuments(r.Context(), id.String())
		if mErr == nil {
			for _, doc := range docs {
				// Only export global (agent-level) memories, skip per-user
				if doc.UserID != "" {
					continue
				}
				detail, dErr := h.memoryStore.GetDocumentDetail(r.Context(), id.String(), "", doc.Path)
				if dErr != nil {
					continue
				}
				export.Memories = append(export.Memories, MemoryExport{
					Path:    detail.Path,
					Content: detail.Content,
				})
			}
		} else {
			slog.Warn("export: failed to list memories", "agent", ag.AgentKey, "error", mErr)
		}
	}

	// Knowledge graph — owner-only, all scopes (user_id preserved per entity)
	if includes["knowledge_graph"] && h.kgStore != nil && isOwner {
		h.exportKG(r, &export, id.String())
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.agent.json"`, ag.AgentKey))
	json.NewEncoder(w).Encode(export)
}

// exportKG appends knowledge graph entities and relations to the export.
// Exports all entities across all user_id scopes.
func (h *AgentsHandler) exportKG(r *http.Request, export *AgentExport, agentID string) {
	entities, eErr := h.kgStore.ListEntities(r.Context(), agentID, "", store.EntityListOptions{Limit: 10000})
	if eErr != nil {
		slog.Warn("export: failed to list KG entities", "agent", agentID, "error", eErr)
		return
	}
	if len(entities) == 0 {
		return
	}

	kg := &KGExport{}
	entityIDSet := map[string]bool{}
	for _, e := range entities {
		kg.Entities = append(kg.Entities, KGEntityExport{
			ExternalID:  e.ExternalID,
			UserID:      e.UserID,
			Name:        e.Name,
			EntityType:  e.EntityType,
			Description: e.Description,
			Properties:  e.Properties,
			Confidence:  e.Confidence,
		})
		entityIDSet[e.ID] = true
	}

	relations, rErr := h.kgStore.ListAllRelations(r.Context(), agentID, "", 50000)
	if rErr == nil {
		// Build internal ID → external_id map
		idToExternal := make(map[string]string, len(entities))
		for _, e := range entities {
			idToExternal[e.ID] = e.ExternalID
		}
		for _, rel := range relations {
			srcExt := idToExternal[rel.SourceEntityID]
			tgtExt := idToExternal[rel.TargetEntityID]
			if srcExt == "" || tgtExt == "" {
				continue // skip orphan relations
			}
			kg.Relations = append(kg.Relations, KGRelationExport{
				SourceExternalID: srcExt,
				TargetExternalID: tgtExt,
				RelationType:     rel.RelationType,
				Confidence:       rel.Confidence,
				Properties:       rel.Properties,
			})
		}
	} else {
		slog.Warn("export: failed to list KG relations", "agent", agentID, "error", rErr)
	}

	export.KnowledgeGraph = kg
}

func (h *AgentsHandler) handleImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	// Limit request body size to prevent DoS
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)

	var export AgentExport
	if err := json.NewDecoder(r.Body).Decode(&export); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	if export.Version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported export version"})
		return
	}

	if export.Agent == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing agent config — this file is data-only (memory/KG), use merge import instead"})
		return
	}
	ag := export.Agent
	if ag.DisplayName == "" && ag.AgentKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "display_name or agent_key")})
		return
	}

	// Validate required fields for a functional agent
	if ag.Provider == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "provider")})
		return
	}
	if ag.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "model")})
		return
	}
	if ag.AgentType != "" && ag.AgentType != store.AgentTypeOpen && ag.AgentType != store.AgentTypePredefined {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid agent_type: must be 'open' or 'predefined'"})
		return
	}

	// Allow override via query params
	if qKey := r.URL.Query().Get("agent_key"); qKey != "" {
		ag.AgentKey = qKey
	}
	if qName := r.URL.Query().Get("display_name"); qName != "" {
		ag.DisplayName = qName
	}

	if ag.AgentKey == "" {
		ag.AgentKey = config.NormalizeAgentID(ag.DisplayName)
	}
	if !isValidSlug(ag.AgentKey) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "agent_key")})
		return
	}

	// Deduplicate: if agent_key exists, append suffix
	originalKey := ag.AgentKey
	for attempt := 0; attempt < 10; attempt++ {
		key := originalKey
		if attempt > 0 {
			key = fmt.Sprintf("%s-%d", originalKey, attempt)
		}
		if existing, _ := h.agents.GetByKey(r.Context(), key); existing == nil {
			ag.AgentKey = key
			break
		}
		if attempt == 9 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "agent", originalKey)})
			return
		}
	}

	tenantID := store.TenantIDFromContext(r.Context())

	if ag.AgentType == "" {
		ag.AgentType = store.AgentTypeOpen
	}
	if ag.ContextWindow <= 0 {
		ag.ContextWindow = config.DefaultContextWindow
	}
	if ag.MaxToolIterations <= 0 {
		ag.MaxToolIterations = config.DefaultMaxIterations
	}

	// Apply same defaults as handleCreate
	if len(ag.CompactionConfig) == 0 {
		ag.CompactionConfig = json.RawMessage(`{}`)
	}
	if len(ag.MemoryConfig) == 0 {
		ag.MemoryConfig = json.RawMessage(`{"enabled":true}`)
	}

	agentData := &store.AgentData{
		AgentKey:            ag.AgentKey,
		DisplayName:         ag.DisplayName,
		Frontmatter:         ag.Frontmatter,
		OwnerID:             userID,
		TenantID:            tenantID,
		AgentType:           ag.AgentType,
		Provider:            ag.Provider,
		Model:               ag.Model,
		ContextWindow:       ag.ContextWindow,
		MaxToolIterations:   ag.MaxToolIterations,
		Workspace:           fmt.Sprintf("%s/%s", h.defaultWorkspace, ag.AgentKey),
		RestrictToWorkspace: true,
		Status:              store.AgentStatusActive,
		ToolsConfig:         ag.ToolsConfig,
		SandboxConfig:       ag.SandboxConfig,
		SubagentsConfig:     ag.SubagentsConfig,
		MemoryConfig:        ag.MemoryConfig,
		CompactionConfig:    ag.CompactionConfig,
		ContextPruning:      ag.ContextPruning,
		OtherConfig:         ag.OtherConfig,
		BudgetMonthlyCents:  ag.BudgetMonthlyCents,
	}

	if err := h.agents.Create(r.Context(), agentData); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "agent", ag.AgentKey)})
		} else {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	// Import context files: for predefined agents, store as agent-level files.
	// For open agents, context files are per-user and seeded on first chat,
	// so we still store them as agent-level templates that SeedUserFiles can pick up.
	if len(export.ContextFiles) > 0 {
		for _, f := range export.ContextFiles {
			if err := h.agents.SetAgentContextFile(r.Context(), agentData.ID, f.FileName, f.Content); err != nil {
				slog.Warn("import: failed to set context file", "agent", ag.AgentKey, "file", f.FileName, "error", err)
			}
		}
	} else {
		// No files in export — seed from templates (no-op for open agents)
		if _, err := bootstrap.SeedToStore(r.Context(), h.agents, agentData.ID, agentData.AgentType); err != nil {
			slog.Warn("import: failed to seed context files", "agent", ag.AgentKey, "error", err)
		}
	}

	agentIDStr := agentData.ID.String()

	// Import memory documents (global only) + index for searchability
	if len(export.Memories) > 0 && h.memoryStore != nil {
		h.importMemories(r, agentIDStr, ag.AgentKey, export.Memories)
	}

	// Import knowledge graph — user_id preserved from export data per entity
	if export.KnowledgeGraph != nil && h.kgStore != nil {
		h.importKG(r, agentIDStr, ag.AgentKey, "", export.KnowledgeGraph)
	}

	emitAudit(h.msgBus, r, "agent.imported", "agent", agentIDStr)
	writeJSON(w, http.StatusCreated, agentData)
}

// handleMergeImport imports data (context files, memory, KG) into an existing agent.
// POST /v1/agents/{id}/import?include=memory,knowledge_graph — owner-only, does not change agent config.
// ?include controls which sections to import (comma-separated). If omitted, imports all sections present.
func (h *AgentsHandler) handleMergeImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	if userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgUserIDHeader)})
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "agent")})
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "agent", id.String())})
		return
	}

	// Owner-only
	if ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": i18n.T(locale, i18n.MsgOwnerOnly, "import into agent")})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)

	var export AgentExport
	if err := json.NewDecoder(r.Body).Decode(&export); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidRequest, err.Error())})
		return
	}

	if export.Version < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported export version"})
		return
	}

	// Parse include filter — controls which data sections to import.
	// If omitted, import all sections present in the file.
	includeParam := r.URL.Query().Get("include")
	includes := map[string]bool{"context_files": true, "memory": true, "knowledge_graph": true} // default: all
	if includeParam != "" {
		includes = map[string]bool{}
		for _, part := range strings.Split(includeParam, ",") {
			includes[strings.TrimSpace(part)] = true
		}
	}

	agentIDStr := id.String()
	var imported []string

	// Merge context files (upsert — overwrites existing files with same name)
	if includes["context_files"] && len(export.ContextFiles) > 0 {
		for _, f := range export.ContextFiles {
			if err := h.agents.SetAgentContextFile(r.Context(), id, f.FileName, f.Content); err != nil {
				slog.Warn("merge-import: failed to set context file", "agent", ag.AgentKey, "file", f.FileName, "error", err)
			}
		}
		imported = append(imported, fmt.Sprintf("%d context_files", len(export.ContextFiles)))
	}

	// Merge memory documents (upsert by path — overwrites content, re-indexes embeddings)
	if includes["memory"] && len(export.Memories) > 0 && h.memoryStore != nil {
		h.importMemories(r, agentIDStr, ag.AgentKey, export.Memories)
		imported = append(imported, fmt.Sprintf("%d memories", len(export.Memories)))
	}

	// Merge knowledge graph (upsert by external_id — new entities added, existing updated)
	if includes["knowledge_graph"] && export.KnowledgeGraph != nil && h.kgStore != nil {
		kg := export.KnowledgeGraph
		h.importKG(r, agentIDStr, ag.AgentKey, "", kg)
		imported = append(imported, fmt.Sprintf("%d entities, %d relations", len(kg.Entities), len(kg.Relations)))
	}

	if len(imported) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no data to import for selected sections"})
		return
	}

	// Invalidate caches
	h.emitCacheInvalidate("agent", ag.AgentKey)
	h.emitCacheInvalidate("bootstrap", agentIDStr)

	emitAudit(h.msgBus, r, "agent.merge_imported", "agent", agentIDStr)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"agent_id": agentIDStr,
		"imported": imported,
	})
}

// importMemories imports memory documents into an agent and indexes them for search.
func (h *AgentsHandler) importMemories(r *http.Request, agentID, agentKey string, memories []MemoryExport) {
	for _, mem := range memories {
		if err := h.memoryStore.PutDocument(r.Context(), agentID, "", mem.Path, mem.Content); err != nil {
			slog.Warn("import: failed to put memory", "agent", agentKey, "path", mem.Path, "error", err)
			continue
		}
		if err := h.memoryStore.IndexDocument(r.Context(), agentID, "", mem.Path); err != nil {
			slog.Warn("import: failed to index memory", "agent", agentKey, "path", mem.Path, "error", err)
		}
	}
}

// importKG imports knowledge graph entities and relations using IngestExtraction.
// Entities are grouped by user_id from export data and ingested per-scope.
// If userID is non-empty, it overrides all entity user_ids (single scope).
func (h *AgentsHandler) importKG(r *http.Request, agentID, agentKey, userID string, kg *KGExport) {
	if len(kg.Entities) == 0 {
		return
	}

	// Group entities by effective user_id
	type batch struct {
		entities  []store.Entity
		relations []store.Relation
	}
	batches := map[string]*batch{}
	entityUserID := map[string]string{} // external_id → effective user_id (for relation routing)

	for _, e := range kg.Entities {
		uid := e.UserID
		if userID != "" {
			uid = userID
		}
		if batches[uid] == nil {
			batches[uid] = &batch{}
		}
		batches[uid].entities = append(batches[uid].entities, store.Entity{
			ExternalID:  e.ExternalID,
			Name:        e.Name,
			EntityType:  e.EntityType,
			Description: e.Description,
			Properties:  e.Properties,
			Confidence:  e.Confidence,
		})
		entityUserID[e.ExternalID] = uid
	}

	for _, rel := range kg.Relations {
		// Route relation to source entity's scope
		uid := entityUserID[rel.SourceExternalID]
		if batches[uid] == nil {
			batches[uid] = &batch{}
		}
		batches[uid].relations = append(batches[uid].relations, store.Relation{
			SourceEntityID: rel.SourceExternalID,
			TargetEntityID: rel.TargetExternalID,
			RelationType:   rel.RelationType,
			Confidence:     rel.Confidence,
			Properties:     rel.Properties,
		})
	}

	for uid, b := range batches {
		if err := h.kgStore.IngestExtraction(r.Context(), agentID, uid, b.entities, b.relations); err != nil {
			slog.Warn("import: failed to ingest KG", "agent", agentKey, "user_id", uid, "entities", len(b.entities), "relations", len(b.relations), "error", err)
		}
	}
}
