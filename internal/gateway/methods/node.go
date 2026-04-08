package methods

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// NodeMethods handles node.invoke.result, node.event, and node.list.
type NodeMethods struct {
	dispatcher *gateway.NodeDispatcher
	eventBus   bus.EventPublisher
}

// NewNodeMethods creates node method handlers.
func NewNodeMethods(dispatcher *gateway.NodeDispatcher, eventBus bus.EventPublisher) *NodeMethods {
	return &NodeMethods{dispatcher: dispatcher, eventBus: eventBus}
}

// Register registers node-related RPC method handlers.
func (m *NodeMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodNodeInvokeResult, m.handleInvokeResult)
	router.Register(protocol.MethodNodeEvent, m.handleNodeEvent)
	router.Register(protocol.MethodNodeList, m.handleNodeList)
	router.Register(protocol.MethodNodeExec, m.handleNodeExec)
}

// handleInvokeResult receives a node's command execution result and routes it
// to the waiting dispatcher goroutine.
func (m *NodeMethods) handleInvokeResult(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		ID          string          `json:"id"`
		NodeID      string          `json:"nodeId"`
		Ok          bool            `json:"ok"`
		PayloadJSON string          `json:"payloadJSON,omitempty"`
		Payload     json.RawMessage `json:"payload,omitempty"`
		Error       *struct {
			Code    string `json:"code,omitempty"`
			Message string `json:"message,omitempty"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "missing invoke id"))
		return
	}

	slog.Debug("node invoke result received", "invokeId", params.ID, "nodeId", params.NodeID, "ok", params.Ok)

	m.dispatcher.HandleResult(params.ID, &gateway.NodeInvokeResponse{
		Ok:          params.Ok,
		PayloadJSON: params.PayloadJSON,
		Payload:     params.Payload,
		Error:       params.Error,
	})

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"received": true}))
}

// handleNodeEvent receives exec events (exec.denied, exec.finished) from nodes
// and publishes them to the event bus for session-scoped delivery.
func (m *NodeMethods) handleNodeEvent(_ context.Context, _ *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Event       string `json:"event"`
		PayloadJSON string `json:"payloadJSON,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return
	}

	if params.Event == "" {
		return
	}

	// Publish the node event to the bus so session-scoped clients receive it.
	var payload any
	if params.PayloadJSON != "" {
		json.Unmarshal([]byte(params.PayloadJSON), &payload)
	}

	slog.Debug("node event forwarded", "event", params.Event)
	m.eventBus.Broadcast(bus.Event{
		Name:    "node." + params.Event,
		Payload: payload,
	})
}

// handleNodeExec dispatches a system.run command to a connected node and returns the result.
// Admin-only. Used for testing/debugging the gateway→node dispatch pipeline.
func (m *NodeMethods) handleNodeExec(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Command   []string `json:"command"`
		TimeoutMs int      `json:"timeoutMs,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}
	if len(params.Command) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "command required"))
		return
	}

	// Build system.run params JSON.
	runParams, _ := json.Marshal(map[string]any{
		"command":   params.Command,
		"timeoutMs": params.TimeoutMs,
		"approved":  true,
	})

	slog.Info("node.exec dispatching", "command", params.Command)

	result, err := m.dispatcher.Dispatch(ctx, "system.run", string(runParams), params.TimeoutMs)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Parse the result JSON to return structured data.
	var parsed any
	json.Unmarshal(result, &parsed)

	client.SendResponse(protocol.NewOKResponse(req.ID, parsed))
}

// handleNodeList returns the list of connected nodes (admin only via policy engine).
func (m *NodeMethods) handleNodeList(_ context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	nodes := m.dispatcher.Registry().List()
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"nodes": nodes,
		"count": len(nodes),
	}))
}
