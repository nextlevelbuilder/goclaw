package methods

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/workflowactions"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

func TestDecodeStrictWorkflowParamsRejectsUnknownAndTrailingValues(t *testing.T) {
	for _, raw := range []string{
		`{"teamId":"a","workflowId":"b","canonicalPlan":{}}`,
		`{"teamId":"a","workflowId":"b"} {"second":true}`,
		`{"teamId":"a","workflowId":"b","deliveryToken":"secret"}`,
	} {
		var params workflowGetParams
		if err := decodeStrictWorkflowParams(json.RawMessage(raw), &params); err == nil {
			t.Fatalf("decodeStrictWorkflowParams(%s) accepted forbidden input", raw)
		}
	}
}

func TestWorkflowPublicDTOsCannotSerializeSensitiveFields(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeOf(workflowDetailDTO{}),
		reflect.TypeOf(workflowTaskDTO{}),
		reflect.TypeOf(workflowDetailResponse{}),
		reflect.TypeOf(workflowActionResponse{}),
	} {
		assertWorkflowDTOFieldsSafe(t, typ)
	}

	response := workflowActionResponse{
		Action:  store.WorkflowActionRetryDelivery,
		Outcome: "conflict",
		Workflow: workflowDetailDTO{
			ID: uuid.NewString(), TeamID: uuid.NewString(), Status: store.TeamWorkflowStatusCompleted,
			PlanRevision: 4, CoordinatorAgentKey: "lead", DeliveryStatus: store.TeamWorkflowDeliveryDead,
		},
		Tasks: []workflowTaskDTO{{
			ID: uuid.NewString(), Subject: "Deliver", Status: store.TeamTaskStatusCompleted,
			WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, PlanRevision: 4,
		}},
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	wire := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"token", "lease", "canonical", "plan_hash", "origin_", "metadata", "audit", "finalize_", "tenant_id",
	} {
		if strings.Contains(wire, forbidden) {
			t.Fatalf("public workflow JSON leaked forbidden field %q: %s", forbidden, wire)
		}
	}
}

func TestWorkflowTaskDTOCarriesVisibleAuthoritativeStateOnly(t *testing.T) {
	result := "done"
	now := time.Now().UTC()
	task := workflowTaskDTO{
		ID: uuid.NewString(), TaskNumber: 3, Subject: "Review", Description: "Independent check",
		Status: store.TeamTaskStatusInReview, WorkflowStepID: "review", WorkflowKind: store.TeamWorkflowTaskKindWork,
		PlanRevision: 2, OwnerAgentKey: "critic", BlockerReason: "", ProgressPercent: 80,
		ProgressStep: "checking", Result: &result, CreatedAt: now, UpdatedAt: now,
	}
	encoded, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(encoded)
	for _, expected := range []string{`"workflow_step_id":"review"`, `"plan_revision":2`, `"owner_agent_key":"critic"`, `"status":"in_review"`} {
		if !strings.Contains(wire, expected) {
			t.Fatalf("workflow task DTO missing %s: %s", expected, wire)
		}
	}
}

type workflowRPCStore struct {
	store.TeamStore
	store.TeamWorkflowStore
	workflow *store.TeamWorkflowData
	tasks    []store.TeamTaskData
	access   bool
	apply    store.WorkflowActionResult
	applies  int
}

func (s *workflowRPCStore) HasTeamAccess(context.Context, uuid.UUID, string) (bool, error) {
	return s.access, nil
}

func (*workflowRPCStore) ListUserTeamIDs(context.Context, string) ([]uuid.UUID, error) {
	return nil, nil
}

func (s *workflowRPCStore) GetWorkflow(context.Context, uuid.UUID) (*store.TeamWorkflowData, error) {
	return s.workflow, nil
}

func (s *workflowRPCStore) ListWorkflowTasks(context.Context, uuid.UUID) ([]store.TeamTaskData, error) {
	return append([]store.TeamTaskData(nil), s.tasks...), nil
}

func (s *workflowRPCStore) ApplyWorkflowAction(_ context.Context, guard store.WorkflowActionGuard) (store.WorkflowActionResult, error) {
	s.applies++
	result := s.apply
	result.Action = guard.Action
	return result, nil
}

func TestWorkflowHandlersEnforceReadAndMutationAuthorization(t *testing.T) {
	tenantID, teamID, workflowID := store.MasterTenantID, uuid.New(), uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 1,
	}
	storeStub := &workflowRPCStore{workflow: workflow, access: true}
	methods, server, client := newWorkflowRPCHarness(t, storeStub, "")

	response := callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowGet, map[string]any{
		"teamId": teamID.String(), "workflowId": workflowID.String(),
	})
	if !response.OK {
		t.Fatalf("authorized workflow get failed: %+v", response.Error)
	}

	storeStub.access = false
	response = callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowGet, map[string]any{
		"teamId": teamID.String(), "workflowId": workflowID.String(),
	})
	if response.OK || response.Error == nil || response.Error.Code != protocol.ErrNotFound {
		t.Fatalf("inaccessible workflow response = %+v, want not found", response)
	}

	response = callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, map[string]any{
		"teamId": teamID.String(), "workflowId": workflowID.String(),
		"action": store.WorkflowActionCancelWorkflow, "expectedStatus": workflow.Status,
		"expectedPlanRevision": workflow.PlanRevision, "reason": "stop",
	})
	if response.OK || response.Error == nil || response.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("non-admin workflow action response = %+v, want unauthorized", response)
	}
	if storeStub.applies != 0 {
		t.Fatalf("non-admin action reached store %d time(s)", storeStub.applies)
	}
	_ = methods
}

func TestWorkflowActionHandlerRejectsMalformedGuardShapesBeforeStore(t *testing.T) {
	tenantID, teamID, workflowID, taskID := store.MasterTenantID, uuid.New(), uuid.New(), uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 3,
	}
	storeStub := &workflowRPCStore{workflow: workflow}
	_, server, client := newWorkflowRPCHarness(t, storeStub, "admin-token")

	tests := []struct {
		name   string
		params map[string]any
	}{
		{
			name: "step missing task",
			params: map[string]any{"action": store.WorkflowActionRetryBlocked,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"expectedTaskStatus": store.TeamTaskStatusBlocked, "reason": "retry"},
		},
		{
			name: "step invalid task",
			params: map[string]any{"action": store.WorkflowActionRetryBlocked,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"taskId": "bad", "expectedTaskStatus": store.TeamTaskStatusBlocked, "reason": "retry"},
		},
		{
			name: "step missing task status",
			params: map[string]any{"action": store.WorkflowActionRetryBlocked,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"taskId": taskID.String(), "reason": "retry"},
		},
		{
			name: "workflow carries task",
			params: map[string]any{"action": store.WorkflowActionCancelWorkflow,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"taskId": taskID.String(), "reason": "stop"},
		},
		{
			name: "workflow carries task status",
			params: map[string]any{"action": store.WorkflowActionCancelWorkflow,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"expectedTaskStatus": store.TeamTaskStatusBlocked, "reason": "stop"},
		},
		{
			name: "forbidden action field",
			params: map[string]any{"action": store.WorkflowActionCancelWorkflow,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"reason": "stop", "deliveryToken": uuid.NewString()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := map[string]any{"teamId": teamID.String(), "workflowId": workflowID.String()}
			for key, value := range tt.params {
				params[key] = value
			}
			before := storeStub.applies
			response := callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, params)
			if response.OK || response.Error == nil || response.Error.Code != protocol.ErrInvalidRequest {
				t.Fatalf("response = %+v, want invalid request", response)
			}
			if storeStub.applies != before {
				t.Fatalf("invalid request reached store: before=%d after=%d", before, storeStub.applies)
			}
		})
	}
}

func TestWorkflowHandlersLocalizePhase8ValidationAcrossFiveLocales(t *testing.T) {
	tenantID, teamID, workflowID := store.MasterTenantID, uuid.New(), uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 3,
	}
	tests := []struct {
		locale       string
		invalidIDMsg string
		guardMsg     string
		actionMsg    string
	}{
		{i18n.LocaleEN, "invalid teamId ID", "expectedStatus and expectedPlanRevision are required", "invalid workflow action request"},
		{i18n.LocaleVI, "ID teamId không hợp lệ", "expectedStatus và expectedPlanRevision là bắt buộc", "yêu cầu thao tác workflow không hợp lệ"},
		{i18n.LocaleZH, "无效的 teamId ID", "expectedStatus 和 expectedPlanRevision 为必填项", "无效的工作流操作请求"},
		{i18n.LocaleKO, "잘못된 teamId ID", "expectedStatus and expectedPlanRevision are required", "invalid workflow action request"},
		{i18n.LocaleRU, "неверный ID teamId", "expectedStatus and expectedPlanRevision are required", "invalid workflow action request"},
	}
	for _, tt := range tests {
		t.Run(tt.locale, func(t *testing.T) {
			storeStub := &workflowRPCStore{workflow: workflow}
			_, server, client := newWorkflowRPCHarnessWithLocale(t, storeStub, "admin-token", tt.locale)
			before := storeStub.applies

			response := callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowGet, map[string]any{
				"teamId": "bad", "workflowId": workflowID.String(),
			})
			assertWorkflowRPCError(t, response, protocol.ErrInvalidRequest, tt.invalidIDMsg)

			response = callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, map[string]any{
				"teamId": teamID.String(), "workflowId": workflowID.String(),
				"action": store.WorkflowActionCancelWorkflow, "reason": "stop",
			})
			assertWorkflowRPCError(t, response, protocol.ErrInvalidRequest, tt.guardMsg)

			response = callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, map[string]any{
				"teamId": teamID.String(), "workflowId": workflowID.String(),
				"action":         store.WorkflowActionCancelWorkflow,
				"expectedStatus": workflow.Status, "expectedPlanRevision": workflow.PlanRevision,
				"reason": " ",
			})
			assertWorkflowRPCError(t, response, protocol.ErrInvalidRequest, tt.actionMsg)
			if storeStub.applies != before {
				t.Fatalf("invalid localized requests reached store: before=%d after=%d", before, storeStub.applies)
			}
		})
	}
}

func TestWorkflowActionHandlerRequiresOptimisticGuardsAndReturnsTypedConflict(t *testing.T) {
	tenantID, teamID, workflowID := store.MasterTenantID, uuid.New(), uuid.New()
	workflow := &store.TeamWorkflowData{
		BaseModel: store.BaseModel{ID: workflowID}, TenantID: tenantID, TeamID: teamID,
		Status: store.TeamWorkflowStatusRunning, PlanRevision: 3,
	}
	storeStub := &workflowRPCStore{
		workflow: workflow,
		apply: store.WorkflowActionResult{
			Outcome: store.WorkflowActionConflict, Workflow: workflow,
		},
	}
	_, server, client := newWorkflowRPCHarness(t, storeStub, "admin-token")

	response := callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, map[string]any{
		"teamId": teamID.String(), "workflowId": workflowID.String(),
		"action": store.WorkflowActionCancelWorkflow, "expectedStatus": workflow.Status,
		"reason": "stop",
	})
	if response.OK || response.Error == nil || response.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("missing revision response = %+v, want invalid request", response)
	}
	if storeStub.applies != 0 {
		t.Fatalf("invalid request reached store %d time(s)", storeStub.applies)
	}

	response = callWorkflowRPC(t, server, client, protocol.MethodTeamsWorkflowAction, map[string]any{
		"teamId": teamID.String(), "workflowId": workflowID.String(),
		"action": store.WorkflowActionCancelWorkflow, "expectedStatus": workflow.Status,
		"expectedPlanRevision": workflow.PlanRevision, "reason": "stop",
	})
	if !response.OK || response.Error != nil {
		t.Fatalf("typed conflict returned RPC error: %+v", response.Error)
	}
	encoded, err := json.Marshal(response.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload workflowActionResponse
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Outcome != "conflict" || payload.Action != store.WorkflowActionCancelWorkflow || payload.Workflow.PlanRevision != 3 {
		t.Fatalf("conflict payload = %+v", payload)
	}
}

func newWorkflowRPCHarness(t *testing.T, storeStub *workflowRPCStore, token string) (*TeamsMethods, *gateway.Server, *websocket.Conn) {
	t.Helper()
	return newWorkflowRPCHarnessWithLocale(t, storeStub, token, "")
}

func newWorkflowRPCHarnessWithLocale(t *testing.T, storeStub *workflowRPCStore, token, locale string) (*TeamsMethods, *gateway.Server, *websocket.Conn) {
	t.Helper()
	cfg := config.Default()
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Token = token
	msgBus := bus.New()
	server := gateway.NewServer(cfg, msgBus, nil, nil)
	server.Router().SetTeamAccessStore(storeStub)
	server.SetPolicyEngine(permissions.NewPolicyEngine(nil))
	methods := NewTeamsMethods(storeStub, nil, nil, nil, msgBus, nil, t.TempDir())
	methods.SetWorkflowActionService(workflowactions.New(storeStub, msgBus, nil))
	methods.RegisterWorkflows(server.Router())

	ctx, cancel := context.WithCancel(context.Background())
	addr, start := gateway.StartTestServer(server, ctx)
	go start()
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(); cancel() })
	response := writeWorkflowRPC(t, conn, protocol.MethodConnect, map[string]any{
		"token": token, "user_id": "operator", "locale": locale,
	})
	if !response.OK {
		t.Fatalf("connect failed: %+v", response.Error)
	}
	return methods, server, conn
}

func assertWorkflowRPCError(t *testing.T, response *protocol.ResponseFrame, code, message string) {
	t.Helper()
	if response.OK || response.Error == nil || response.Error.Code != code || response.Error.Message != message {
		t.Fatalf("response = %+v, want %s %q", response, code, message)
	}
}

func callWorkflowRPC(t *testing.T, _ *gateway.Server, conn *websocket.Conn, method string, params any) *protocol.ResponseFrame {
	t.Helper()
	return writeWorkflowRPC(t, conn, method, params)
}

func writeWorkflowRPC(t *testing.T, conn *websocket.Conn, method string, params any) *protocol.ResponseFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(protocol.RequestFrame{
		Type: protocol.FrameTypeRequest, ID: "request", Method: method, Params: raw,
	}); err != nil {
		t.Fatal(err)
	}
	var response protocol.ResponseFrame
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatal(err)
	}
	return &response
}

func assertWorkflowDTOFieldsSafe(t *testing.T, typ reflect.Type) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		lower := strings.ToLower(jsonName)
		if jsonName != "-" {
			for _, forbidden := range []string{"token", "lease", "canonical", "plan_hash", "origin_", "metadata", "audit", "tenant_id"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("%s.%s exposes forbidden JSON field %q", typ.Name(), field.Name, jsonName)
				}
			}
		}
		assertWorkflowDTOFieldsSafe(t, field.Type)
	}
}
