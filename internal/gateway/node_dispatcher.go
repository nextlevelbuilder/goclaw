package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// DefaultNodeTimeout is the default timeout for node invocations.
const DefaultNodeTimeout = 120 * time.Second

// NodeDispatcher sends commands to connected nodes and correlates results.
type NodeDispatcher struct {
	registry *NodeRegistry
	pending  sync.Map // map[requestID] chan *nodeInvokeResponse
}

// NodeInvokeResponse is the result received from a node.
type NodeInvokeResponse struct {
	Ok          bool            `json:"ok"`
	PayloadJSON string          `json:"payloadJSON,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Error       *struct {
		Code    string `json:"code,omitempty"`
		Message string `json:"message,omitempty"`
	} `json:"error,omitempty"`
}

// NewNodeDispatcher creates a dispatcher backed by the given registry.
func NewNodeDispatcher(registry *NodeRegistry) *NodeDispatcher {
	return &NodeDispatcher{registry: registry}
}

// Registry returns the underlying node registry.
func (d *NodeDispatcher) Registry() *NodeRegistry { return d.registry }

// Dispatch sends a command to a connected node and waits for the result.
// Returns the raw JSON payload on success, or an error on failure/timeout.
func (d *NodeDispatcher) Dispatch(ctx context.Context, command string, paramsJSON string, timeoutMs int) (json.RawMessage, error) {
	node := d.registry.Pick(command)
	if node == nil {
		return nil, fmt.Errorf("no node available for command %q", command)
	}

	requestID := uuid.NewString()

	// Create response channel and register it.
	ch := make(chan *NodeInvokeResponse, 1)
	d.pending.Store(requestID, ch)
	defer d.pending.Delete(requestID)

	// Send the invoke request event to the node.
	node.Client.SendEvent(*protocol.NewEvent("node.invoke.request", map[string]any{
		"id":         requestID,
		"nodeId":     node.NodeID,
		"command":    command,
		"paramsJSON": paramsJSON,
	}))

	slog.Debug("node dispatch sent", "command", command, "nodeId", node.NodeID, "requestId", requestID)

	// Wait for result with timeout.
	timeout := DefaultNodeTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs)*time.Millisecond + 5*time.Second // add buffer beyond command timeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("node returned nil response")
		}
		if !resp.Ok {
			errMsg := "node invocation failed"
			if resp.Error != nil {
				errMsg = resp.Error.Message
			}
			return nil, fmt.Errorf("%s", errMsg)
		}
		// Return payloadJSON if present, else payload.
		if resp.PayloadJSON != "" {
			return json.RawMessage(resp.PayloadJSON), nil
		}
		return resp.Payload, nil

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-timer.C:
		return nil, fmt.Errorf("node invocation timed out after %s", timeout)
	}
}

// HandleResult delivers a node's invoke result to the waiting dispatch call.
// Called by the node.invoke.result method handler.
func (d *NodeDispatcher) HandleResult(requestID string, result *NodeInvokeResponse) {
	val, ok := d.pending.Load(requestID)
	if !ok {
		slog.Warn("node result for unknown request", "requestId", requestID)
		return
	}
	ch := val.(chan *NodeInvokeResponse)
	select {
	case ch <- result:
	default:
		slog.Warn("node result channel full, dropping", "requestId", requestID)
	}
}

// HasNodes reports whether any node is connected.
func (d *NodeDispatcher) HasNodes() bool {
	return d.registry.Count() > 0
}

