package methods

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// MultiAgentMethods exposes dynamic team formation routing and read-only
// windows over durable multi-agent records (jury verdict history and
// negotiation contract history). Jury/negotiation execution itself happens in
// the agent loop via the jury/negotiate tools; these RPC methods select the
// formation directive and query the persisted records so the UI can render
// verdicts without re-running agents.
type MultiAgentMethods struct {
	contracts store.ContractStore
	eventBus  bus.EventPublisher
}

// NewMultiAgentMethods creates the multi-agent RPC surface. contracts may be
// nil — the handlers then respond with ErrUnavailable for record reads while
// formation routing (pure, store-free) keeps working.
func NewMultiAgentMethods(contracts store.ContractStore, eventBus bus.EventPublisher) *MultiAgentMethods {
	return &MultiAgentMethods{contracts: contracts, eventBus: eventBus}
}

// Register wires multiagent.* RPC methods onto the gateway router.
func (m *MultiAgentMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodMultiAgentFormation, m.handleFormation)
	router.Register(protocol.MethodMultiAgentJury, m.handleJury)
	router.Register(protocol.MethodMultiAgentNegotiate, m.handleNegotiate)
}

// handleFormation routes a task to a dynamic team formation. It runs no
// agents: the returned formation is a directive the caller (or the agent loop
// via the jury/negotiate tools) turns into real work. A matching
// multiagent.formation_selected event is broadcast for real-time UI updates.
func (m *MultiAgentMethods) handleFormation(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		Task       string `json:"task"`
		Complexity string `json:"complexity"`
		Override   string `json:"override"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
		return
	}
	if strings.TrimSpace(params.Task) == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "task")))
		return
	}

	formation, err := teamworkclassify.SelectFormation(params.Task, params.Complexity, params.Override)
	if err != nil {
		slog.Warn("multiagent.formation.invalid", "task", params.Task, "override", params.Override, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, err.Error())))
		return
	}

	payload := protocol.MultiAgentFormationPayload{
		Task:       params.Task,
		Formation:  formation.Name,
		Agents:     formation.Agents,
		Pipeline:   formation.Pipeline,
		Complexity: formation.Complexity,
		Category:   teamworkclassify.FormationCategory(formation.Name),
		Override:   strings.TrimSpace(params.Override) != "",
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"formation": payload,
	}))
	m.broadcast(protocol.EventMultiAgentFormationSelected, payload, client.TenantID())
}

// handleJury lists persisted jury verdict records (durable multi-agent
// contract history). Execution is delegated to the jury tool in the agent
// loop; this method is a read-only query surface. When the contract store is
// absent it reports ErrUnavailable instead of failing closed silently.
func (m *MultiAgentMethods) handleJury(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	m.handleRecordList(ctx, client, req, store.ContractRecordJury)
}

// handleNegotiate lists persisted negotiation contract records. Like
// handleJury this is a read-only history query; negotiation rounds run via the
// negotiate tool in the agent loop. Use multiagent.negotiate when the client
// wants to inspect prior negotiation state, not to start a new round.
func (m *MultiAgentMethods) handleNegotiate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	m.handleRecordList(ctx, client, req, store.ContractRecordNegotiation)
}

func (m *MultiAgentMethods) handleRecordList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame, kind string) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		RunID  string `json:"runId"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidJSON)))
			return
		}
	}
	if m.contracts == nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnavailable, i18n.T(locale, i18n.MsgMultiAgentStoreUnavailable, kind)))
		return
	}
	opts := store.ContractRecordListOpts{
		RunID:  params.RunID,
		Kind:   kind,
		Limit:  params.Limit,
		Offset: params.Offset,
	}
	records, err := m.contracts.ListContractRecords(ctx, opts)
	if err != nil {
		slog.Warn("multiagent.records.list_failed", "kind", kind, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error())))
		return
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"kind":    kind,
		"records": records,
	}))
}

// broadcast wraps the event publisher (nil-safe) so a disabled event bus never
// panics a request handler.
func (m *MultiAgentMethods) broadcast(name string, payload any, tenantID uuid.UUID) {
	if m.eventBus == nil {
		return
	}
	m.eventBus.Broadcast(bus.Event{Name: name, Payload: payload, TenantID: tenantID})
}