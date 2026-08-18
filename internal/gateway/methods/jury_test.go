package methods

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// stubContractStore records list calls against the ContractStore interface.
type stubContractStore struct {
	records  []store.ContractRecord
	err      error
	lastOpts store.ContractRecordListOpts
}

func (s *stubContractStore) CreateContractRecord(_ context.Context, _ *store.ContractRecord) error {
	return nil
}

func (s *stubContractStore) GetContractRecord(_ context.Context, _ uuid.UUID) (*store.ContractRecord, error) {
	return nil, nil
}

func (s *stubContractStore) ListContractRecords(_ context.Context, opts store.ContractRecordListOpts) ([]store.ContractRecord, error) {
	s.lastOpts = opts
	return s.records, s.err
}

func (s *stubContractStore) UpdateContractRecordStatus(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}

func multiAgentReq(t *testing.T, method string, params any) *protocol.RequestFrame {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "req-multiagent",
		Method: method,
		Params: raw,
	}
}

func readMultiAgentResponse(t *testing.T, ch <-chan []byte) *protocol.ResponseFrame {
	t.Helper()
	select {
	case raw := <-ch:
		var resp protocol.ResponseFrame
		if err := json.Unmarshal(raw, &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		return &resp
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout: handler did not send response")
		return nil
	}
}

func TestMultiAgentFormation_RoutesToCatalog(t *testing.T) {
	m := NewMultiAgentMethods(nil, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.New(), "caller", 2)
	ctx := context.Background()

	m.handleFormation(ctx, client, multiAgentReq(t, protocol.MethodMultiAgentFormation, map[string]any{
		"task":       "design the auth flow",
		"complexity": "high",
	}))

	resp := readMultiAgentResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload, ok := resp.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T", resp.Payload)
	}
	formation, ok := payload["formation"].(map[string]any)
	if !ok {
		t.Fatalf("formation field missing or wrong type: %#v", payload["formation"])
	}
	if formation["formation"] != "architect-review-team" {
		t.Fatalf("formation = %v, want architect-review-team", formation["formation"])
	}
	agents := formation["agents"].([]any)
	if len(agents) != 2 || agents[0] != "arch" || agents[1] != "review" {
		t.Fatalf("unexpected agents: %v", agents)
	}
	if v, ok := formation["override"]; ok && v != false {
		t.Fatalf("override should be false when not provided, got %v", formation["override"])
	}
}

func TestMultiAgentFormation_OverrideWins(t *testing.T) {
	m := NewMultiAgentMethods(nil, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.New(), "caller", 2)

	m.handleFormation(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentFormation, map[string]any{
		"task":       "anything",
		"complexity": "low",
		"override":   "debugger-panel",
	}))

	resp := readMultiAgentResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	formation := resp.Payload.(map[string]any)["formation"].(map[string]any)
	if formation["formation"] != "debugger-panel" {
		t.Fatalf("override did not win: got %v", formation["formation"])
	}
	if formation["override"] != true {
		t.Fatalf("override should be true when provided")
	}
}

func TestMultiAgentFormation_EmptyTaskErrors(t *testing.T) {
	m := NewMultiAgentMethods(nil, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.New(), "caller", 2)

	m.handleFormation(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentFormation, map[string]any{}))

	resp := readMultiAgentResponse(t, responses)
	if resp.OK {
		t.Fatal("expected error response for empty task")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %+v", resp.Error)
	}
}

func TestMultiAgentFormation_UnknownOverrideErrors(t *testing.T) {
	m := NewMultiAgentMethods(nil, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleOperator, uuid.New(), "caller", 2)

	m.handleFormation(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentFormation, map[string]any{
		"task":     "anything",
		"override": "no-such-formation",
	}))

	resp := readMultiAgentResponse(t, responses)
	if resp.OK {
		t.Fatal("expected error response for unknown override")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST, got %+v", resp.Error)
	}
}

func TestMultiAgentJury_ListRecords(t *testing.T) {
	recs := []store.ContractRecord{
		{ID: uuid.New(), Kind: store.ContractRecordJury, Status: store.ContractRecordClosed},
		{ID: uuid.New(), Kind: store.ContractRecordJury, Status: store.ContractRecordActive},
	}
	cs := &stubContractStore{records: recs}
	m := NewMultiAgentMethods(cs, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, uuid.New(), "caller", 2)

	m.handleJury(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentJury, map[string]any{
		"runId": "run-1",
		"limit": 10,
	}))

	resp := readMultiAgentResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	payload := resp.Payload.(map[string]any)
	if payload["kind"] != store.ContractRecordJury {
		t.Fatalf("kind = %v, want %q", payload["kind"], store.ContractRecordJury)
	}
	if cs.lastOpts.Kind != store.ContractRecordJury {
		t.Fatalf("list filtered by kind = %q, want %q", cs.lastOpts.Kind, store.ContractRecordJury)
	}
	if cs.lastOpts.RunID != "run-1" {
		t.Fatalf("run filter = %q, want run-1", cs.lastOpts.RunID)
	}
}

func TestMultiAgentJury_StoreUnavailable(t *testing.T) {
	m := NewMultiAgentMethods(nil, &stubEventPub{}) // nil contract store
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, uuid.New(), "caller", 2)

	m.handleJury(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentJury, map[string]any{}))

	resp := readMultiAgentResponse(t, responses)
	if resp.OK {
		t.Fatal("expected error when contract store is nil")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("expected UNAVAILABLE, got %+v", resp.Error)
	}
}

func TestMultiAgentNegotiate_ListRecords(t *testing.T) {
	cs := &stubContractStore{records: nil}
	m := NewMultiAgentMethods(cs, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, uuid.New(), "caller", 2)

	m.handleNegotiate(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentNegotiate, map[string]any{}))

	resp := readMultiAgentResponse(t, responses)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if cs.lastOpts.Kind != store.ContractRecordNegotiation {
		t.Fatalf("list filtered by kind = %q, want %q", cs.lastOpts.Kind, store.ContractRecordNegotiation)
	}
}

func TestMultiAgentJury_ListStoreError(t *testing.T) {
	cs := &stubContractStore{err: context.DeadlineExceeded}
	m := NewMultiAgentMethods(cs, &stubEventPub{})
	client, responses := gateway.NewCapturingTestClient(permissions.RoleViewer, uuid.New(), "caller", 2)

	m.handleJury(context.Background(), client, multiAgentReq(t, protocol.MethodMultiAgentJury, map[string]any{}))

	resp := readMultiAgentResponse(t, responses)
	if resp.OK {
		t.Fatal("expected error when store list fails")
	}
	if resp.Error == nil || resp.Error.Code != protocol.ErrInternal {
		t.Fatalf("expected INTERNAL, got %+v", resp.Error)
	}
}
