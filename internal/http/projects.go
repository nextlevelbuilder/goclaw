package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ProjectHandler handles project CRUD and MCP override HTTP endpoints.
type ProjectHandler struct {
	store store.ProjectStore
	token string
}

// NewProjectHandler creates a handler for project management endpoints.
func NewProjectHandler(store store.ProjectStore, token string) *ProjectHandler {
	return &ProjectHandler{store: store, token: token}
}

// RegisterRoutes registers all project routes on the given mux.
func (h *ProjectHandler) RegisterRoutes(mux *http.ServeMux) {
	// Project CRUD
	mux.HandleFunc("GET /v1/projects", h.auth(h.handleListProjects))
	mux.HandleFunc("POST /v1/projects", h.auth(h.handleCreateProject))
	mux.HandleFunc("GET /v1/projects/by-chat", h.auth(h.handleGetByChat))
	mux.HandleFunc("GET /v1/projects/{id}", h.auth(h.handleGetProject))
	mux.HandleFunc("PUT /v1/projects/{id}", h.auth(h.handleUpdateProject))
	mux.HandleFunc("DELETE /v1/projects/{id}", h.auth(h.handleDeleteProject))

	// MCP overrides
	mux.HandleFunc("GET /v1/projects/{id}/mcp", h.auth(h.handleListMCPOverrides))
	mux.HandleFunc("PUT /v1/projects/{id}/mcp/{serverName}", h.auth(h.handleSetMCPOverride))
	mux.HandleFunc("DELETE /v1/projects/{id}/mcp/{serverName}", h.auth(h.handleRemoveMCPOverride))
}

func (h *ProjectHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token != "" {
			if extractBearerToken(r) != h.token {
				locale := extractLocale(r)
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": i18n.T(locale, i18n.MsgUnauthorized)})
				return
			}
		}
		userID := extractUserID(r)
		ctx := store.WithLocale(r.Context(), extractLocale(r))
		if userID != "" {
			ctx = store.WithUserID(ctx, userID)
		}
		r = r.WithContext(ctx)
		next(w, r)
	}
}

// --- Project CRUD ---

func (h *ProjectHandler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects(r.Context())
	if err != nil {
		slog.Error("projects.list", "error", err)
		locale := store.LocaleFromContext(r.Context())
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "projects")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"projects": projects})
}

func (h *ProjectHandler) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())

	var payload struct {
		Name        string     `json:"name"`
		Slug        string     `json:"slug"`
		ChannelType *string    `json:"channel_type"`
		ChatID      *string    `json:"chat_id"`
		TeamID      *uuid.UUID `json:"team_id"`
		Description *string    `json:"description"`
		Status      string     `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if payload.Name == "" || payload.Slug == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "name and slug")})
		return
	}
	if !isValidSlug(payload.Slug) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
		return
	}

	if payload.Status == "" {
		payload.Status = "active"
	}

	project := store.Project{
		Name:        payload.Name,
		Slug:        payload.Slug,
		ChannelType: payload.ChannelType,
		ChatID:      payload.ChatID,
		TeamID:      payload.TeamID,
		Description: payload.Description,
		Status:      payload.Status,
		CreatedBy:   store.UserIDFromContext(r.Context()),
	}

	if err := h.store.CreateProject(r.Context(), &project); err != nil {
		slog.Error("projects.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, project)
}

func (h *ProjectHandler) handleGetProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	project, err := h.store.GetProject(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "project", id.String())})
		return
	}

	writeJSON(w, http.StatusOK, project)
}

func (h *ProjectHandler) handleGetByChat(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	channelType := r.URL.Query().Get("channel_type")
	chatID := r.URL.Query().Get("chat_id")

	if channelType == "" || chatID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "channel_type and chat_id")})
		return
	}

	project, err := h.store.GetProjectByChatID(r.Context(), channelType, chatID)
	if err != nil || project == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found for this chat"})
		return
	}

	writeJSON(w, http.StatusOK, project)
}

func (h *ProjectHandler) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	var updates map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&updates); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if slug, ok := updates["slug"]; ok {
		if s, _ := slug.(string); !isValidSlug(s) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidSlug, "slug")})
			return
		}
	}

	if err := h.store.UpdateProject(r.Context(), id, updates); err != nil {
		slog.Error("projects.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ProjectHandler) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	if err := h.store.DeleteProject(r.Context(), id); err != nil {
		slog.Error("projects.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- MCP Overrides ---

func (h *ProjectHandler) handleListMCPOverrides(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	overrides, err := h.store.GetMCPOverrides(r.Context(), id)
	if err != nil {
		slog.Error("projects.list_mcp_overrides", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "MCP overrides")})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"overrides": overrides})
}

func (h *ProjectHandler) handleSetMCPOverride(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	serverName := r.PathValue("serverName")
	if serverName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "serverName")})
		return
	}

	var envOverrides map[string]string
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&envOverrides); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := h.store.SetMCPOverride(r.Context(), id, serverName, envOverrides); err != nil {
		slog.Error("projects.set_mcp_override", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *ProjectHandler) handleRemoveMCPOverride(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidID, "project")})
		return
	}

	serverName := r.PathValue("serverName")
	if serverName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "serverName")})
		return
	}

	if err := h.store.RemoveMCPOverride(r.Context(), id, serverName); err != nil {
		slog.Error("projects.remove_mcp_override", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
