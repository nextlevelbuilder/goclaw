package methods

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// Workflow DTOs are deliberately separate from the store rows. In particular,
// they cannot serialize canonical plans/hashes, origin routing, internal audit
// IDs, metadata, claims, tokens, or leases.
type workflowDetailDTO struct {
	ID                     string     `json:"id"`
	TeamID                 string     `json:"team_id"`
	Status                 string     `json:"status"`
	PlanRevision           int        `json:"plan_revision"`
	CoordinatorAgentKey    string     `json:"coordinator_agent_key"`
	CoordinatorDisplayName string     `json:"coordinator_display_name,omitempty"`
	FailureSummary         string     `json:"failure_summary,omitempty"`
	ResultSummary          string     `json:"result_summary,omitempty"`
	CancelReason           string     `json:"cancel_reason,omitempty"`
	DeliveryStatus         string     `json:"delivery_status"`
	ExpansionAttemptCount  int        `json:"expansion_attempt_count"`
	DeliveryAttemptCount   int        `json:"delivery_attempt_count"`
	LastExpansionError     string     `json:"last_expansion_error,omitempty"`
	LastDeliveryError      string     `json:"last_delivery_error,omitempty"`
	NextExpansionAt        *time.Time `json:"next_expansion_at,omitempty"`
	NextDeliveryAt         *time.Time `json:"next_delivery_at,omitempty"`
	FinalizedAt            *time.Time `json:"finalized_at,omitempty"`
	DeliveredAt            *time.Time `json:"delivered_at,omitempty"`
	CancelledAt            *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

type workflowTaskDTO struct {
	ID               string    `json:"id"`
	TaskNumber       int       `json:"task_number,omitempty"`
	Subject          string    `json:"subject"`
	Description      string    `json:"description,omitempty"`
	Status           string    `json:"status"`
	WorkflowStepID   string    `json:"workflow_step_id"`
	WorkflowKind     string    `json:"workflow_kind"`
	WorkflowTerminal bool      `json:"workflow_terminal"`
	PlanRevision     int       `json:"plan_revision"`
	OwnerAgentKey    string    `json:"owner_agent_key,omitempty"`
	BlockerReason    string    `json:"blocker_reason,omitempty"`
	RecoveryCount    int       `json:"recovery_count"`
	DispatchCount    int       `json:"dispatch_count"`
	ProgressPercent  int       `json:"progress_percent"`
	ProgressStep     string    `json:"progress_step,omitempty"`
	Result           *string   `json:"result,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type workflowDetailResponse struct {
	Workflow       workflowDetailDTO      `json:"workflow"`
	Tasks          []workflowTaskDTO      `json:"tasks"`
	AllowedActions []store.WorkflowAction `json:"allowed_actions"`
}

type workflowActionResponse struct {
	Action         store.WorkflowAction   `json:"action"`
	Outcome        string                 `json:"outcome"`
	Workflow       workflowDetailDTO      `json:"workflow"`
	Tasks          []workflowTaskDTO      `json:"tasks"`
	AllowedActions []store.WorkflowAction `json:"allowed_actions"`
}

type workflowGetParams struct {
	TeamID     string `json:"teamId"`
	WorkflowID string `json:"workflowId"`
}

type workflowActionParams struct {
	TeamID               string               `json:"teamId"`
	WorkflowID           string               `json:"workflowId"`
	Action               store.WorkflowAction `json:"action"`
	ExpectedStatus       string               `json:"expectedStatus"`
	ExpectedPlanRevision int                  `json:"expectedPlanRevision"`
	TaskID               *string              `json:"taskId,omitempty"`
	ExpectedTaskStatus   string               `json:"expectedTaskStatus,omitempty"`
	Reason               string               `json:"reason"`
}

func (m *TeamsMethods) SetWorkflowActionService(service *workflowactions.Service) {
	m.workflowActions = service
}

func (m *TeamsMethods) RegisterWorkflows(router *gateway.MethodRouter) {
	router.Register(protocol.MethodTeamsWorkflowGet, m.handleWorkflowGet)
	router.Register(protocol.MethodTeamsWorkflowAction, m.handleWorkflowAction)
}

func decodeStrictWorkflowParams(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func parseWorkflowIDs(teamIDRaw, workflowIDRaw string) (uuid.UUID, uuid.UUID, error) {
	teamID, err := uuid.Parse(strings.TrimSpace(teamIDRaw))
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("teamId")
	}
	workflowID, err := uuid.Parse(strings.TrimSpace(workflowIDRaw))
	if err != nil {
		return uuid.Nil, uuid.Nil, errors.New("workflowId")
	}
	return teamID, workflowID, nil
}

func (m *TeamsMethods) handleWorkflowGet(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if m.workflowActions == nil || m.teamStore == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}
	var params workflowGetParams
	if err := decodeStrictWorkflowParams(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	teamID, workflowID, err := parseWorkflowIDs(params.TeamID, params.WorkflowID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, err.Error())))
		return
	}
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		allowed, accessErr := m.teamStore.HasTeamAccess(ctx, teamID, client.UserID())
		if accessErr != nil || !allowed {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "workflow", "")))
			return
		}
	}
	workflow, tasks, err := m.workflowActions.Get(ctx, teamID, workflowID)
	if err != nil {
		m.sendWorkflowError(locale, client, req.ID, workflowID, err)
		return
	}
	response, err := m.workflowDetail(ctx, workflow, tasks)
	if err != nil {
		slog.Warn("teams.workflows.get DTO failed", "workflow_id", workflowID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, response))
}

func (m *TeamsMethods) handleWorkflowAction(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	// Defense in depth: router policy classifies this method admin-only, and the
	// handler independently rejects direct invocation by any lower role.
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgWorkflowAuthorizationDenied)))
		return
	}
	if m.workflowActions == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgTeamsNotConfigured)))
		return
	}
	var params workflowActionParams
	if err := decodeStrictWorkflowParams(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	teamID, workflowID, err := parseWorkflowIDs(params.TeamID, params.WorkflowID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, err.Error())))
		return
	}
	if strings.TrimSpace(params.ExpectedStatus) == "" || params.ExpectedPlanRevision <= 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWorkflowExpectedGuardsRequired)))
		return
	}
	var taskID *uuid.UUID
	if params.TaskID != nil {
		parsed, parseErr := uuid.Parse(strings.TrimSpace(*params.TaskID))
		if parseErr != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWorkflowInvalidTaskID)))
			return
		}
		taskID = &parsed
	}
	guard := store.WorkflowActionGuard{
		Action:               params.Action,
		TeamID:               teamID,
		WorkflowID:           workflowID,
		ExpectedStatus:       params.ExpectedStatus,
		ExpectedPlanRevision: params.ExpectedPlanRevision,
		TaskID:               taskID,
		ExpectedTaskStatus:   params.ExpectedTaskStatus,
		Reason:               params.Reason,
		Actor: store.WorkflowActionActor{
			Kind:   store.WorkflowActorAdmin,
			UserID: client.UserID(),
		},
	}
	if err := guard.Validate(); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWorkflowActionInvalid)))
		return
	}
	result, err := m.workflowActions.Apply(ctx, guard)
	if err != nil {
		m.sendWorkflowError(locale, client, req.ID, workflowID, err)
		return
	}
	if result.Workflow == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	detail, err := m.workflowDetail(ctx, result.Workflow, result.Tasks)
	if err != nil {
		slog.Warn("teams.workflows.action DTO failed", "workflow_id", workflowID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, "")))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, workflowActionResponse{
		Action: result.Action, Outcome: result.Outcome.String(), Workflow: detail.Workflow,
		Tasks: detail.Tasks, AllowedActions: detail.AllowedActions,
	}))
}

func (m *TeamsMethods) sendWorkflowError(locale string, client *gateway.Client, requestID string, workflowID uuid.UUID, err error) {
	if errors.Is(err, store.ErrTaskNotFound) {
		client.SendResponse(protocol.NewErrorResponse(requestID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgWorkflowNotFound)))
		return
	}
	if errors.Is(err, store.ErrWorkflowActionInvalid) {
		client.SendResponse(protocol.NewErrorResponse(requestID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgWorkflowActionInvalid)))
		return
	}
	slog.Warn("teams workflow RPC failed", "workflow_id", workflowID, "error", err)
	client.SendResponse(protocol.NewErrorResponse(requestID, protocol.ErrInternal, i18n.T(locale, i18n.MsgWorkflowActionFailed)))
}

func (m *TeamsMethods) workflowDetail(ctx context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData) (workflowDetailResponse, error) {
	if workflow == nil {
		return workflowDetailResponse{}, fmt.Errorf("workflow is nil")
	}
	coordinatorDisplay := ""
	if m.agentStore != nil {
		agent, err := m.agentStore.GetByID(ctx, workflow.CoordinatorAgentID)
		if err != nil {
			return workflowDetailResponse{}, fmt.Errorf("load workflow coordinator: %w", err)
		}
		if agent != nil {
			coordinatorDisplay = agent.DisplayName
		}
	}
	publicTasks := make([]workflowTaskDTO, 0, len(tasks))
	for i := range tasks {
		publicTasks = append(publicTasks, workflowTaskDTO{
			ID: tasks[i].ID.String(), TaskNumber: tasks[i].TaskNumber,
			Subject: tasks[i].Subject, Description: tasks[i].Description, Status: tasks[i].Status,
			WorkflowStepID: tasks[i].WorkflowStepID, WorkflowKind: tasks[i].WorkflowKind,
			WorkflowTerminal: tasks[i].WorkflowTerminal, PlanRevision: tasks[i].PlanRevision,
			OwnerAgentKey: tasks[i].OwnerAgentKey, BlockerReason: tasks[i].BlockerReason,
			RecoveryCount: tasks[i].RecoveryCount, DispatchCount: tasks[i].DispatchCount,
			ProgressPercent: tasks[i].ProgressPercent, ProgressStep: tasks[i].ProgressStep,
			Result: tasks[i].Result, CreatedAt: tasks[i].CreatedAt, UpdatedAt: tasks[i].UpdatedAt,
		})
	}
	return workflowDetailResponse{
		Workflow: workflowDetailDTO{
			ID: workflow.ID.String(), TeamID: workflow.TeamID.String(), Status: workflow.Status,
			PlanRevision: workflow.PlanRevision, CoordinatorAgentKey: workflow.CoordinatorAgentKey,
			CoordinatorDisplayName: coordinatorDisplay, FailureSummary: workflow.FailureSummary,
			ResultSummary: workflow.ResultSummary, CancelReason: workflow.CancelReason,
			DeliveryStatus: workflow.DeliveryStatus, ExpansionAttemptCount: workflow.ExpansionAttemptCount,
			DeliveryAttemptCount: workflow.DeliveryAttemptCount, LastExpansionError: workflow.LastExpansionError,
			LastDeliveryError: workflow.LastDeliveryError, NextExpansionAt: workflow.NextExpansionAt,
			NextDeliveryAt: workflow.NextDeliveryAt, FinalizedAt: workflow.FinalizedAt,
			DeliveredAt: workflow.DeliveredAt, CancelledAt: workflow.CancelledAt,
			CreatedAt: workflow.CreatedAt, UpdatedAt: workflow.UpdatedAt,
		},
		Tasks: publicTasks, AllowedActions: m.workflowActions.AllowedActions(workflow, tasks),
	}, nil
}
