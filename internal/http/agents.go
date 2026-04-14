package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// AgentsHandler handles agent CRUD and sharing endpoints.
type AgentsHandler struct {
	agents           store.AgentStore
	providers        store.ProviderStore
	providerReg      *providers.Registry
	db               *sql.DB
	tracingStore     store.TracingStore
	memoryStore      store.MemoryStore         // for import (nil = disabled)
	kgStore          store.KnowledgeGraphStore // for import (nil = disabled)
	episodicStore    store.EpisodicStore       // for import (nil in SQLite/lite builds)
	vaultStore       store.VaultStore          // for vault import (nil = disabled)
	toolsReg         ToolPreviewLister          // for system prompt preview tool resolution (nil = fallback)
	skillsLoader     SkillPreviewBuilder        // for system prompt preview pinned skills (nil = skip)
	skillAccessStore store.SkillAccessStore     // for system prompt preview skill filtering (nil = skip)
	teamStore        store.TeamStore           // for system prompt preview team context (nil = skip)
	agentLinkStore   store.AgentLinkStore      // for system prompt preview delegation targets (nil = skip)
	defaultWorkspace string                   // default workspace path template (e.g. "~/.goclaw/workspace")
	dataDir          string                   // resolved data directory (e.g. "~/.goclaw/data") — for team workspace export
	msgBus           *bus.MessageBus          // for cache invalidation events (nil = no events)
	summoner         *AgentSummoner           // LLM-based agent setup (nil = disabled)
	isOwner          func(string) bool        // checks if user ID is a system owner (nil = no owners configured)
}

// NewAgentsHandler creates a handler for agent management endpoints.
// isOwner is a function that checks if a user ID is in GOCLAW_OWNER_IDS (nil = disabled).
func NewAgentsHandler(agents store.AgentStore, providers store.ProviderStore, providerReg *providers.Registry, db *sql.DB, tracing store.TracingStore, defaultWorkspace string, msgBus *bus.MessageBus, summoner *AgentSummoner, isOwner func(string) bool) *AgentsHandler {
	return &AgentsHandler{
		agents:           agents,
		providers:        providers,
		providerReg:      providerReg,
		db:               db,
		tracingStore:     tracing,
		defaultWorkspace: defaultWorkspace,
		msgBus:           msgBus,
		summoner:         summoner,
		isOwner:          isOwner,
	}
}

// SetDataDir sets the resolved data directory used for team workspace paths.
func (h *AgentsHandler) SetDataDir(dataDir string) {
	h.dataDir = dataDir
}

// SetImportStores attaches optional stores needed for agent import.
func (h *AgentsHandler) SetImportStores(mem store.MemoryStore, kg store.KnowledgeGraphStore) {
	h.memoryStore = mem
	h.kgStore = kg
}

// SetEpisodicStore attaches the episodic store for Tier 2 memory import.
// Not available in SQLite/lite builds — nil is safe (episodic import is skipped).
func (h *AgentsHandler) SetEpisodicStore(ep store.EpisodicStore) {
	h.episodicStore = ep
}

// SetVaultStore attaches the vault store for Knowledge Vault import.
// nil is safe — vault import is skipped when not set.
func (h *AgentsHandler) SetVaultStore(vs store.VaultStore) {
	h.vaultStore = vs
}

// ToolPreviewLister is satisfied by tools.Registry for system prompt preview.
type ToolPreviewLister interface {
	List() []string
	Get(name string) (tools.Tool, bool)
	Aliases() map[string]string
}

// SkillPreviewBuilder is satisfied by skills.Loader for system prompt preview.
type SkillPreviewBuilder interface {
	BuildPinnedSummary(ctx context.Context, names []string) string
	BuildSummary(ctx context.Context, allowList []string) string
}

// SetPreviewDeps attaches optional dependencies for system prompt preview.
func (h *AgentsHandler) SetPreviewDeps(tl ToolPreviewLister, sl SkillPreviewBuilder) {
	h.toolsReg = tl
	h.skillsLoader = sl
}

// SetPreviewStores attaches team + agent link stores for system prompt preview.
func (h *AgentsHandler) SetPreviewStores(ts store.TeamStore, als store.AgentLinkStore, sas store.SkillAccessStore) {
	h.teamStore = ts
	h.agentLinkStore = als
	h.skillAccessStore = sas
}

// isOwnerUser checks if the given user ID is a system owner.
func (h *AgentsHandler) isOwnerUser(userID string) bool {
	return userID != "" && h.isOwner != nil && h.isOwner(userID)
}

// emitCacheInvalidate broadcasts a cache invalidation event if msgBus is set.
func (h *AgentsHandler) emitCacheInvalidate(kind, key string) {
	if h.msgBus == nil {
		return
	}
	h.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: kind, Key: key},
	})
}

// RegisterRoutes registers all agent management routes on the given mux.
func (h *AgentsHandler) RegisterRoutes(mux *http.ServeMux) {
	// Agent CRUD (reads: viewer+, writes: admin+)
	mux.HandleFunc("GET /v1/agents", h.authMiddleware(h.handleList))
	mux.HandleFunc("POST /v1/agents", h.adminMiddleware(h.handleCreate))
	mux.HandleFunc("GET /v1/agents/{id}", h.authMiddleware(h.handleGet))
	mux.HandleFunc("PUT /v1/agents/{id}", h.adminMiddleware(h.handleUpdate))
	mux.HandleFunc("DELETE /v1/agents/{id}", h.adminMiddleware(h.handleDelete))
	// Bulk operations (admin+)
	mux.HandleFunc("POST /v1/agents/sync-workspace", h.adminMiddleware(h.handleSyncWorkspace))
	// Sharing (admin+)
	mux.HandleFunc("GET /v1/agents/{id}/shares", h.authMiddleware(h.handleListShares))
	mux.HandleFunc("POST /v1/agents/{id}/shares", h.adminMiddleware(h.handleShare))
	mux.HandleFunc("DELETE /v1/agents/{id}/shares/{userID}", h.adminMiddleware(h.handleRevokeShare))
	// Agent operations (admin+)
	mux.HandleFunc("POST /v1/agents/{id}/regenerate", h.adminMiddleware(h.handleRegenerate))
	mux.HandleFunc("POST /v1/agents/{id}/resummon", h.adminMiddleware(h.handleResummon))
	// Export (agent owner or system owner)
	mux.HandleFunc("GET /v1/agents/{id}/system-prompt-preview", h.adminMiddleware(h.handleSystemPromptPreview))
	mux.HandleFunc("GET /v1/agents/{id}/export/preview", h.authMiddleware(h.handleExportPreview))
	mux.HandleFunc("GET /v1/agents/{id}/export", h.authMiddleware(h.handleExport))
	mux.HandleFunc("GET /v1/agents/{id}/export/download/{token}", h.authMiddleware(h.handleExportDownload))
	// Shared download route for all export types (skills, MCP, teams use same token map)
	mux.HandleFunc("GET /v1/export/download/{token}", h.authMiddleware(h.handleExportDownload))
	// Import (admin only — system owner or tenant admin)
	mux.HandleFunc("POST /v1/agents/import/preview", h.adminMiddleware(h.handleImportPreview))
	mux.HandleFunc("POST /v1/agents/import", h.adminMiddleware(h.handleImport))
	mux.HandleFunc("POST /v1/agents/{id}/import", h.adminMiddleware(h.handleMergeImport))
	// Team export/import (system owner only)
	mux.HandleFunc("GET /v1/teams/{id}/export/preview", h.adminMiddleware(h.handleTeamExportPreview))
	mux.HandleFunc("GET /v1/teams/{id}/export", h.adminMiddleware(h.handleTeamExport))
	mux.HandleFunc("POST /v1/teams/import", h.adminMiddleware(h.handleTeamImport))
	// Read-only (viewer+)
	mux.HandleFunc("GET /v1/agents/{id}/codex-pool-activity", h.authMiddleware(h.handleCodexPoolActivity))
	mux.HandleFunc("GET /v1/agents/{id}/instances", h.authMiddleware(h.handleListInstances))
	mux.HandleFunc("GET /v1/agents/{id}/instances/{userID}/files", h.authMiddleware(h.handleGetInstanceFiles))
	// Instance writes (admin+)
	mux.HandleFunc("PUT /v1/agents/{id}/instances/{userID}/files/{fileName}", h.adminMiddleware(h.handleSetInstanceFile))
	mux.HandleFunc("PATCH /v1/agents/{id}/instances/{userID}/metadata", h.adminMiddleware(h.handleUpdateInstanceMetadata))
}

func (h *AgentsHandler) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth("", next)
}

func (h *AgentsHandler) adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleAdmin, next)
}

func (h *AgentsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	if userID == "" {
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
		return
	}

	var agents []store.AgentData
	var err error
	if h.isOwnerUser(userID) {
		agents, err = h.agents.List(r.Context(), "") // owners see all agents
	} else {
		agents, err = h.agents.ListAccessible(r.Context(), userID)
	}
	if err != nil {
		slog.Error("agents.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToList, "agents"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (h *AgentsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgUserIDHeader))
		return
	}

	var req store.AgentData
	if !bindJSON(w, r, locale, &req) {
		return
	}

	if !isValidSlug(req.AgentKey) {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidSlug, "agent_key"))
		return
	}

	// Check for duplicate agent_key before creating
	if existing, _ := h.agents.GetByKey(r.Context(), req.AgentKey); existing != nil {
		writeError(w, http.StatusConflict, protocol.ErrAlreadyExists, i18n.T(locale, i18n.MsgAlreadyExists, "agent", req.AgentKey))
		return
	}

	req.OwnerID = userID

	// Resolve tenant_id: explicit body field for cross-tenant; otherwise inherit from auth context.
	if store.IsOwnerRole(r.Context()) {
		if req.TenantID == uuid.Nil {
			req.TenantID = store.TenantIDFromContext(r.Context())
		}
	} else {
		req.TenantID = store.TenantIDFromContext(r.Context())
	}

	if req.AgentType == "" || req.AgentType == store.AgentTypeOpen {
		req.AgentType = store.AgentTypePredefined // v3: open agents deprecated, default to predefined
	}
	if req.ContextWindow <= 0 {
		req.ContextWindow = config.DefaultContextWindow
	}
	if req.MaxToolIterations <= 0 {
		req.MaxToolIterations = config.DefaultMaxIterations
	}
	if req.Workspace == "" {
		req.Workspace = fmt.Sprintf("%s/%s", h.defaultWorkspace, req.AgentKey)
	}
	req.RestrictToWorkspace = true

	// Default: enable compaction and memory for new agents
	if len(req.CompactionConfig) == 0 {
		req.CompactionConfig = json.RawMessage(`{}`)
	}
	if len(req.MemoryConfig) == 0 {
		req.MemoryConfig = json.RawMessage(`{"enabled":true}`)
	}

	// Check if predefined agent has a description for LLM summoning
	description := req.AgentDescription
	if req.AgentType == store.AgentTypePredefined && description != "" && h.summoner != nil {
		req.Status = store.AgentStatusSummoning
	} else if req.Status == "" {
		req.Status = store.AgentStatusActive
	}

	if err := validateChatGPTOAuthAgentRouting(
		r.Context(),
		h.providers,
		req.Provider,
		req.ParseChatGPTOAuthRouting(),
	); err != nil {
		slog.Error("agents.create.validate_routing", "error", err)
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}

	if err := h.agents.Create(r.Context(), &req); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "23505") {
			writeError(w, http.StatusConflict, protocol.ErrAlreadyExists, i18n.T(locale, i18n.MsgAlreadyExists, "agent", req.AgentKey))
		} else {
			slog.Error("agents.create", "agent_key", req.AgentKey, "error", err)
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToCreate, "agent", "internal error"))
		}
		return
	}

	// Seed context files into agent_context_files (skipped for open agents).
	// For summoning agents, templates serve as fallback if LLM fails.
	if _, err := bootstrap.SeedToStore(r.Context(), h.agents, req.ID, req.AgentType); err != nil {
		slog.Warn("failed to seed context files for new agent", "agent", req.AgentKey, "error", err)
	}

	// Start LLM summoning in background if applicable
	if req.Status == store.AgentStatusSummoning {
		go h.summoner.SummonAgent(req.ID, req.TenantID, req.Provider, req.Model, description)
	}

	emitAudit(h.msgBus, r, "agent.created", "agent", req.ID.String())
	writeJSON(w, http.StatusCreated, req)
}

func (h *AgentsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	isOwner := h.isOwnerUser(userID)

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// Try by agent_key
		ag, err2 := h.agents.GetByKey(r.Context(), r.PathValue("id"))
		if err2 != nil {
			writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", r.PathValue("id")))
			return
		}
		if userID != "" && !isOwner {
			if ok, _, _ := h.agents.CanAccess(r.Context(), ag.ID, userID); !ok {
				writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
				return
			}
		}
		writeJSON(w, http.StatusOK, ag)
		return
	}

	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", id.String()))
		return
	}

	if userID != "" && !isOwner {
		if ok, _, _ := h.agents.CanAccess(r.Context(), id, userID); !ok {
			writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "agent"))
			return
		}
	}

	writeJSON(w, http.StatusOK, ag)
}

func (h *AgentsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}

	// Tenant admins can update any agent in their tenant (adminMiddleware already
	// verified RoleAdmin). System owners can update any agent across tenants.
	// GetByID respects tenant scoping from context, so if the agent is returned
	// it belongs to the caller's tenant.
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", id.String()))
		return
	}

	var updates map[string]any
	if !bindJSON(w, r, locale, &updates) {
		return
	}

	// Allowlist: only permit known agent columns to be updated.
	// Defense-in-depth against column injection via arbitrary JSON keys.
	allowed := filterAllowedKeys(updates, agentAllowedFields)
	allowed["restrict_to_workspace"] = true

	// If agent_key is being changed, enforce the slug format. The router
	// cache uses `tenantID:agentKey` as its canonical key and splits on the
	// last colon for exact-segment invalidation — a colon inside agent_key
	// would silently break invalidation. Slug regex already rejects colons
	// and any other shell/path-unfriendly characters.
	if newKey, ok := allowed["agent_key"].(string); ok && newKey != "" {
		if !isValidSlug(newKey) {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidSlug, "agent_key"))
			return
		}
	}

	// Validate v3 flag values in other_config (must be boolean).
	if oc, ok := allowed["other_config"]; ok && oc != nil {
		switch v := oc.(type) {
		case map[string]any:
			if err := store.ValidateV3Flags(v); err != nil {
				writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
				return
			}
		}
	}

	validationProvider := ag.Provider
	if providerName, ok := allowed["provider"].(string); ok && providerName != "" {
		validationProvider = providerName
	}
	validationAgent := *ag
	validationAgent.Provider = validationProvider
	if otherConfig, ok := allowed["other_config"]; ok {
		rawOtherConfig, err := marshalJSONRaw(otherConfig)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
			return
		}
		validationAgent.OtherConfig = rawOtherConfig
	}
	if routing, ok := allowed["chatgpt_oauth_routing"]; ok {
		rawRouting, err := marshalJSONRaw(routing)
		if err != nil {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON))
			return
		}
		validationAgent.ChatGPTOAuthRouting = rawRouting
	}

	if err := validateChatGPTOAuthAgentRouting(
		r.Context(),
		h.providers,
		validationAgent.Provider,
		validationAgent.ParseChatGPTOAuthRouting(),
	); err != nil {
		slog.Error("agents.update.validate_routing", "id", id, "error", err)
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error()))
		return
	}

	if err := h.agents.Update(r.Context(), id, allowed); err != nil {
		slog.Error("agents.update", "id", id, "user_id", userID,
			"tenant_id", store.TenantIDFromContext(r.Context()), "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToUpdate, "agent", err.Error()))
		return
	}

	// Sync display_name change into IDENTITY.md so the agent self-reports the new name.
	if newName, ok := allowed["display_name"].(string); ok && newName != "" {
		h.syncIdentityName(r.Context(), ag, newName)
	}

	// Invalidate caches: agent Loop + bootstrap files
	h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
	h.emitCacheInvalidate(bus.CacheKindBootstrap, id.String())

	// Cascade: if status changed, broadcast so channel instances and cron jobs react.
	if newStatus, ok := allowed["status"].(string); ok && newStatus != ag.Status {
		if h.msgBus != nil {
			bus.BroadcastForTenant(h.msgBus, bus.EventAgentStatusChanged,
				store.TenantIDFromContext(r.Context()),
				bus.AgentStatusChangedPayload{
					AgentID:   id.String(),
					OldStatus: ag.Status,
					NewStatus: newStatus,
				})
		}
	}

	emitAudit(h.msgBus, r, "agent.updated", "agent", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// syncIdentityName updates the Name: field in the agent's IDENTITY.md (agent-level and
// all per-user copies for open agents) so the agent self-reports the new display name.
// Errors are logged but do not fail the rename request.
func (h *AgentsHandler) syncIdentityName(ctx context.Context, ag *store.AgentData, newName string) {
	// Read existing agent-level IDENTITY.md.
	existingContent := ""
	if dbFiles, err := h.agents.GetAgentContextFiles(ctx, ag.ID); err == nil {
		for _, f := range dbFiles {
			if f.FileName == bootstrap.IdentityFile {
				existingContent = f.Content
				break
			}
		}
	}

	newContent := bootstrap.UpdateIdentityField(existingContent, "Name", newName)
	if newContent == "" {
		newContent = "# Identity\nName: " + newName + "\n"
	}
	if err := h.agents.SetAgentContextFile(ctx, ag.ID, bootstrap.IdentityFile, newContent); err != nil {
		slog.Warn("agents.update: failed to sync IDENTITY.md name", "agent", ag.AgentKey, "error", err)
	}

	// For open agents, also update per-user IDENTITY.md copies.
	if ag.AgentType == store.AgentTypeOpen {
		if userFiles, err := h.agents.ListUserContextFilesByName(ctx, ag.ID, bootstrap.IdentityFile); err == nil {
			for _, uf := range userFiles {
				updated := bootstrap.UpdateIdentityField(uf.Content, "Name", newName)
				if updated == uf.Content {
					continue
				}
				if err := h.agents.SetUserContextFile(ctx, ag.ID, uf.UserID, bootstrap.IdentityFile, updated); err != nil {
					slog.Warn("agents.update: failed to sync user IDENTITY.md name", "agent", ag.AgentKey, "user", uf.UserID, "error", err)
				}
			}
		}
	}
}

func (h *AgentsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "agent"))
		return
	}

	// Only owner can delete
	ag, err := h.agents.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "agent", id.String()))
		return
	}
	if userID != "" && ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgOwnerOnly, "delete agent"))
		return
	}

	if err := h.agents.Delete(r.Context(), id); err != nil {
		slog.Error("agents.delete", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgFailedToDelete, "agent", "internal error"))
		return
	}

	// Invalidate caches: agent Loop + bootstrap files
	h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
	h.emitCacheInvalidate(bus.CacheKindBootstrap, id.String())

	emitAudit(h.msgBus, r, "agent.deleted", "agent", id.String())
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// handleSyncWorkspace updates all agents to use the new workspace root.
// POST /v1/agents/sync-workspace
// Body: {"workspace": "E:\\project\\workspace"}
// Requires admin role.
func (h *AgentsHandler) handleSyncWorkspace(w http.ResponseWriter, r *http.Request) {
	tenantID := store.TenantIDFromContext(r.Context())

	var req struct {
		Workspace string `json:"workspace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "invalid JSON body")
		return
	}
	if req.Workspace == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "workspace is required")
		return
	}
	// Path sanity check: reject traversal attempts
	if strings.Contains(req.Workspace, "..") {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "workspace path cannot contain '..'")
		return
	}

	// List all agents (empty ownerID = all agents)
	agents, err := h.agents.List(r.Context(), "")
	if err != nil {
		slog.Error("agents.sync_workspace: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "failed to list agents")
		return
	}

	// Update each agent's workspace to use the new root
	newWorkspace := config.ExpandHome(req.Workspace)
	var updated int
	for _, ag := range agents {
		// Skip agents from other tenants
		if ag.TenantID != tenantID {
			continue
		}
		// Build new workspace path: {newWorkspace}/{agentKey}
		newPath := filepath.Join(newWorkspace, ag.AgentKey)
		if ag.Workspace == newPath {
			continue // already using correct path
		}
		// Use Update with map[string]any
		if err := h.agents.Update(r.Context(), ag.ID, map[string]any{"workspace": newPath}); err != nil {
			slog.Warn("agents.sync_workspace: update failed", "agent", ag.AgentKey, "error", err)
			continue
		}
		h.emitCacheInvalidate(bus.CacheKindAgent, ag.AgentKey)
		updated++
	}

	slog.Info("agents.sync_workspace: completed", "updated", updated, "total", len(agents), "workspace", newWorkspace)
	emitAudit(h.msgBus, r, "agents.workspace_synced", "updated", strconv.Itoa(updated))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "updated": updated})
}
