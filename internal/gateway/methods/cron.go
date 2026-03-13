package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

var cronSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// CronMethods handles cron.list, cron.create, cron.update, cron.delete, cron.toggle.
type CronMethods struct {
	service  store.CronStore
	eventBus bus.EventPublisher
}

func NewCronMethods(service store.CronStore, eventBus bus.EventPublisher) *CronMethods {
	return &CronMethods{service: service, eventBus: eventBus}
}

func (m *CronMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodCronList, m.handleList)
	router.Register(protocol.MethodCronCreate, m.handleCreate)
	router.Register(protocol.MethodCronUpdate, m.handleUpdate)
	router.Register(protocol.MethodCronDelete, m.handleDelete)
	router.Register(protocol.MethodCronToggle, m.handleToggle)
	router.Register(protocol.MethodCronStatus, m.handleStatus)
	router.Register(protocol.MethodCronRun, m.handleRun)
	router.Register(protocol.MethodCronRuns, m.handleRuns)
}

func (m *CronMethods) handleList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		IncludeDisabled bool `json:"includeDisabled"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	jobs := m.service.ListJobs(params.IncludeDisabled, "", "")

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"jobs":   jobs,
		"status": m.service.Status(),
	}))
}

func (m *CronMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Name     string             `json:"name"`
		Schedule store.CronSchedule `json:"schedule"`
		Message  string             `json:"message"`
		Deliver  bool               `json:"deliver"`
		Channel  string             `json:"channel"`
		To       string             `json:"to"`
		AgentID  string             `json:"agentId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if !cronSlugRe.MatchString(params.Name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidSlug, "name")))
		return
	}
	if params.Message == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgMsgRequired)))
		return
	}

	job, err := m.service.AddJob(params.Name, params.Schedule, params.Message, params.Deliver, params.Channel, params.To, params.AgentID, client.UserID())
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"job": job,
	}))
	emitAudit(m.eventBus, client, "cron.created", "cron", job.ID)
}

func (m *CronMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		JobID string `json:"jobId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.JobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "jobId")))
		return
	}

	if err := m.service.RemoveJob(params.JobID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"deleted": true,
	}))
	emitAudit(m.eventBus, client, "cron.deleted", "cron", params.JobID)
}

func (m *CronMethods) handleToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		JobID   string `json:"jobId"`
		Enabled bool   `json:"enabled"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.JobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "jobId")))
		return
	}

	if err := m.service.EnableJob(params.JobID, params.Enabled); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"jobId":   params.JobID,
		"enabled": params.Enabled,
	}))
	emitAudit(m.eventBus, client, "cron.toggled", "cron", params.JobID)
}

func (m *CronMethods) handleStatus(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	client.SendResponse(protocol.NewOKResponse(req.ID, m.service.Status()))
}

func (m *CronMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		JobID string             `json:"jobId"`
		ID    string             `json:"id"` // alias (matching TS)
		Patch store.CronJobPatch `json:"patch"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	jobID := params.JobID
	if jobID == "" {
		jobID = params.ID
	}
	if jobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "jobId")))
		return
	}

	job, err := m.service.UpdateJob(jobID, params.Patch)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"job": job,
	}))
	emitAudit(m.eventBus, client, "cron.updated", "cron", jobID)
}

func (m *CronMethods) handleRun(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		JobID string `json:"jobId"`
		ID    string `json:"id"`
		Mode  string `json:"mode"` // "force" or "due" (default)
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	jobID := params.JobID
	if jobID == "" {
		jobID = params.ID
	}
	if jobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "jobId")))
		return
	}

	force := params.Mode == "force"

	// Validate job exists before responding
	_, ok := m.service.GetJob(jobID)
	if !ok {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgJobNotFound)))
		return
	}

	// Respond immediately — job execution happens in background
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":  true,
		"ran": true,
	}))
	emitAudit(m.eventBus, client, "cron.run", "cron", jobID)

	go func() {
		if _, _, err := m.service.RunJob(jobID, force); err != nil {
			slog.Warn("cron.run background error", "jobId", jobID, "error", err)
		}
	}()
}

func (m *CronMethods) handleRuns(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		JobID  string `json:"jobId"`
		ID     string `json:"id"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	jobID := params.JobID
	if jobID == "" {
		jobID = params.ID
	}

	entries, total := m.service.GetRunLog(jobID, params.Limit, params.Offset)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"entries": entries,
		"total":   total,
	}))
}
