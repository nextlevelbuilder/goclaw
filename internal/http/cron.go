package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const maxCronRequestBody = 1 << 20

// CronHandler exposes cron job management over HTTP for declarative operators.
type CronHandler struct {
	store store.CronStore
}

// NewCronHandler creates a cron HTTP handler.
func NewCronHandler(s store.CronStore) *CronHandler {
	return &CronHandler{store: s}
}

// RegisterRoutes registers cron management routes.
func (h *CronHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/cron", h.auth(h.handleList))
	mux.HandleFunc("POST /v1/cron", h.auth(h.handleCreate))
	mux.HandleFunc("PATCH /v1/cron/{id}", h.auth(h.handlePatch))
	mux.HandleFunc("DELETE /v1/cron/{id}", h.auth(h.handleDelete))
	mux.HandleFunc("POST /v1/cron/{id}/toggle", h.auth(h.handleToggle))
	mux.HandleFunc("POST /v1/cron/{id}/run", h.auth(h.handleRun))
	mux.HandleFunc("GET /v1/cron/{id}/runs", h.auth(h.handleRuns))
	mux.HandleFunc("GET /v1/cron/status", h.auth(h.handleStatus))
}

func (h *CronHandler) auth(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(permissions.RoleOperator, next)
}

type cronCreateRequest struct {
	Name           string             `json:"name"`
	AgentID        string             `json:"agentId,omitempty"`
	UserID         string             `json:"userId,omitempty"`
	Enabled        *bool              `json:"enabled,omitempty"`
	Schedule       store.CronSchedule `json:"schedule"`
	Message        string             `json:"message"`
	Deliver        bool               `json:"deliver,omitempty"`
	DeliverChannel string             `json:"deliverChannel,omitempty"`
	DeliverTo      string             `json:"deliverTo,omitempty"`
	DeleteAfterRun *bool              `json:"deleteAfterRun,omitempty"`
	Stateless      *bool              `json:"stateless,omitempty"`
	WakeHeartbeat  *bool              `json:"wakeHeartbeat,omitempty"`
	Managed        *store.CronManaged `json:"managed,omitempty"`
	Provider       string             `json:"provider,omitempty"`
	Model          string             `json:"model,omitempty"`
}

func (h *CronHandler) handleList(w http.ResponseWriter, r *http.Request) {
	includeDisabled := parseBoolQuery(r, "includeDisabled")
	agentID := r.URL.Query().Get("agentId")
	userID := r.URL.Query().Get("userId")
	managedBy := r.URL.Query().Get("managedBy")
	managedKey := r.URL.Query().Get("managedKey")

	if !cronCallerCanSeeAll(r.Context()) {
		userID = store.UserIDFromContext(r.Context())
		if userID == "" {
			writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, "cron job access denied")
			return
		}
	}

	jobs := h.store.ListJobs(r.Context(), includeDisabled, agentID, userID)
	if managedBy != "" || managedKey != "" {
		filtered := jobs[:0]
		for _, job := range jobs {
			if managedBy != "" && job.Managed.By != managedBy {
				continue
			}
			if managedKey != "" && job.Managed.Key != managedKey {
				continue
			}
			filtered = append(filtered, job)
		}
		jobs = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *CronHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxCronRequestBody)

	var req cronCreateRequest
	if !bindJSON(w, r, locale, &req) {
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "name is required")
		return
	}
	if req.Message == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "message is required")
		return
	}
	if err := store.ValidateCronSchedule(&req.Schedule); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, err.Error())
		return
	}

	userID := req.UserID
	if !cronCallerCanSeeAll(r.Context()) {
		userID = store.UserIDFromContext(r.Context())
		if userID == "" {
			writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, "cron job access denied")
			return
		}
	} else if userID == "" {
		userID = store.UserIDFromContext(r.Context())
	}

	job, err := h.store.AddJob(r.Context(), req.Name, req.Schedule, req.Message, req.Deliver, req.DeliverChannel, req.DeliverTo, req.AgentID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
		return
	}

	patch := store.CronJobPatch{
		Enabled:        req.Enabled,
		Managed:        req.Managed,
		DeleteAfterRun: req.DeleteAfterRun,
		Stateless:      req.Stateless,
		WakeHeartbeat:  req.WakeHeartbeat,
	}
	if req.Provider != "" {
		patch.Provider = &req.Provider
	}
	if req.Model != "" {
		patch.Model = &req.Model
	}
	if req.Enabled != nil || req.Managed != nil || req.DeleteAfterRun != nil || req.Stateless != nil || req.WakeHeartbeat != nil || patch.Provider != nil || patch.Model != nil {
		jobID := job.ID
		job, err = h.store.UpdateJob(r.Context(), jobID, patch)
		if err != nil {
			if rmErr := h.store.RemoveJob(r.Context(), jobID); rmErr != nil {
				slog.Warn("cron.create rollback failed", "job_id", jobID, "error", rmErr)
			}
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"job": job})
}

func (h *CronHandler) handlePatch(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "job id is required")
		return
	}
	if !h.authorizeCronJob(w, r.Context(), jobID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCronRequestBody)

	var patch store.CronJobPatch
	if !bindJSON(w, r, locale, &patch) {
		return
	}
	job, err := h.store.UpdateJob(r.Context(), jobID, patch)
	if err != nil {
		writeCronStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *CronHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "job id is required")
		return
	}
	if !h.authorizeCronJob(w, r.Context(), jobID) {
		return
	}
	if err := h.store.RemoveJob(r.Context(), jobID); err != nil {
		writeCronStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *CronHandler) handleToggle(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "job id is required")
		return
	}
	if !h.authorizeCronJob(w, r.Context(), jobID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCronRequestBody)

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !bindJSON(w, r, locale, &req) {
		return
	}
	if err := h.store.EnableJob(r.Context(), jobID, req.Enabled); err != nil {
		writeCronStoreError(w, err)
		return
	}
	job, _ := h.store.GetJob(r.Context(), jobID)
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (h *CronHandler) handleRun(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "job id is required")
		return
	}
	if !h.authorizeCronJob(w, r.Context(), jobID) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCronRequestBody)

	var req struct {
		Force bool   `json:"force"`
		Mode  string `json:"mode,omitempty"`
	}
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "invalid JSON body")
			return
		}
	}
	force := req.Force || req.Mode == "force"
	runCtx := context.WithoutCancel(r.Context())
	go func() {
		if _, _, err := h.store.RunJob(runCtx, jobID, force); err != nil {
			slog.Warn("cron.run background error", "job_id", jobID, "error", err)
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "ran": true})
}

func (h *CronHandler) handleRuns(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, "job id is required")
		return
	}
	if !h.authorizeCronJob(w, r.Context(), jobID) {
		return
	}
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)
	entries, total := h.store.GetRunLog(r.Context(), jobID, limit, offset)
	writeJSON(w, http.StatusOK, map[string]any{"runs": entries, "total": total})
}

func (h *CronHandler) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.Status())
}

func parseBoolQuery(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	if v == "" {
		return false
	}
	ok, err := strconv.ParseBool(v)
	return err == nil && ok
}

func parseIntQuery(r *http.Request, key string, fallback int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func writeCronStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrCronJobNotFound) {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, protocol.ErrInternal, err.Error())
}

func cronCallerCanSeeAll(ctx context.Context) bool {
	return permissions.HasMinRole(permissions.Role(store.RoleFromContext(ctx)), permissions.RoleAdmin)
}

func (h *CronHandler) authorizeCronJob(w http.ResponseWriter, ctx context.Context, jobID string) bool {
	if cronCallerCanSeeAll(ctx) {
		return true
	}
	userID := store.UserIDFromContext(ctx)
	if userID == "" {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, "cron job access denied")
		return false
	}
	job, ok := h.store.GetJob(ctx, jobID)
	if !ok {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, store.ErrCronJobNotFound.Error())
		return false
	}
	if job.UserID != userID {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, "cron job access denied")
		return false
	}
	return true
}
