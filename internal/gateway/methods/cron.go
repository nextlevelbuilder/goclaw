package methods

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

var cronSlugRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// CronMethods handles cron.list, cron.create, cron.update, cron.delete, cron.toggle.
type CronMethods struct {
	service store.CronStore
}

func NewCronMethods(service store.CronStore) *CronMethods {
	return &CronMethods{service: service}
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

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"jobs":   jobs,
		"status": m.service.Status(),
	}))
}

func (m *CronMethods) handleCreate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name is required"))
		return
	}
	if !cronSlugRe.MatchString(params.Name) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "name must be a valid slug (lowercase letters, numbers, hyphens only)"))
		return
	}
	if params.Message == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "message is required"))
		return
	}

	job, err := m.service.AddJob(params.Name, params.Schedule, params.Message, params.Deliver, params.Channel, params.To, params.AgentID, "")
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"job": job,
	}))
}

func (m *CronMethods) handleDelete(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		JobID string `json:"jobId"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.JobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "jobId is required"))
		return
	}

	if err := m.service.RemoveJob(params.JobID); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"deleted": true,
	}))
}

func (m *CronMethods) handleToggle(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		JobID   string `json:"jobId"`
		Enabled bool   `json:"enabled"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.JobID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "jobId is required"))
		return
	}

	if err := m.service.EnableJob(params.JobID, params.Enabled); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"jobId":   params.JobID,
		"enabled": params.Enabled,
	}))
}

func (m *CronMethods) handleStatus(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	client.SendResponse(protocol.NewOKResponse(req.ID, m.service.Status()))
}

func (m *CronMethods) handleUpdate(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "jobId is required"))
		return
	}

	job, err := m.service.UpdateJob(jobID, params.Patch)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"job": job,
	}))
}

func (m *CronMethods) handleRun(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "jobId is required"))
		return
	}

	force := params.Mode == "force"
	ran, reason, err := m.service.RunJob(jobID, force)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	resp := map[string]interface{}{
		"ok":  true,
		"ran": ran,
	}
	if !ran && reason != "" {
		resp["reason"] = reason
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, resp))
}

func (m *CronMethods) handleRuns(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		JobID string `json:"jobId"`
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	jobID := params.JobID
	if jobID == "" {
		jobID = params.ID
	}

	entries := m.service.GetRunLog(jobID, params.Limit)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]interface{}{
		"entries": entries,
	}))
}
