package nodehost

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"strings"
)

// NodeInvokeRequestPayload is an incoming RPC invocation from the gateway.
type NodeInvokeRequestPayload struct {
	ID             string          `json:"id"`
	NodeID         string          `json:"nodeId"`
	Command        string          `json:"command"`
	ParamsJSON     *string         `json:"paramsJSON,omitempty"`
	TimeoutMs      *int            `json:"timeoutMs,omitempty"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
}

// NodeInvokeResultParams is the result sent back to the gateway.
type NodeInvokeResultParams struct {
	ID          string                  `json:"id"`
	NodeID      string                  `json:"nodeId"`
	Ok          bool                    `json:"ok"`
	PayloadJSON string                  `json:"payloadJSON,omitempty"`
	Payload     json.RawMessage         `json:"payload,omitempty"`
	Error       *NodeInvokeResultError  `json:"error,omitempty"`
}

// NodeInvokeResultError describes an invocation error.
type NodeInvokeResultError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// GatewayRequester abstracts the gateway client for sending requests/events.
type GatewayRequester interface {
	Request(ctx context.Context, method string, params any) error
}

// CoerceNodeInvokePayload validates and extracts an invoke payload from raw JSON.
func CoerceNodeInvokePayload(raw json.RawMessage) *NodeInvokeRequestPayload {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	id := coerceString(obj["id"])
	nodeID := coerceString(obj["nodeId"])
	command := coerceString(obj["command"])
	if id == "" || nodeID == "" || command == "" {
		return nil
	}

	var paramsJSON *string
	if v, ok := obj["paramsJSON"]; ok {
		s := coerceString(v)
		if s != "" {
			paramsJSON = &s
		}
	} else if v, ok := obj["params"]; ok {
		s := string(v)
		paramsJSON = &s
	}

	result := &NodeInvokeRequestPayload{
		ID:      id,
		NodeID:  nodeID,
		Command: command,
	}
	if paramsJSON != nil {
		result.ParamsJSON = paramsJSON
	}
	return result
}

func coerceString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// HandleInvoke routes an incoming RPC command to the appropriate handler.
func HandleInvoke(ctx context.Context, frame NodeInvokeRequestPayload, client GatewayRequester, skillBins *SkillBinsCache) {
	command := strings.TrimSpace(frame.Command)
	slog.Debug("nodehost invoke", "command", command, "id", frame.ID)

	switch command {
	case "system.run":
		handleSystemRunCommand(ctx, frame, client, skillBins)
	case "system.run.prepare":
		handleSystemRunPrepare(ctx, frame, client)
	case "system.which":
		handleSystemWhich(ctx, frame, client)
	default:
		sendErrorResult(ctx, client, frame, "UNKNOWN_COMMAND", "unknown command: "+command)
	}
}

func handleSystemRunCommand(ctx context.Context, frame NodeInvokeRequestPayload, client GatewayRequester, skillBins *SkillBinsCache) {
	params := decodeParamsAs[SystemRunParams](frame.ParamsJSON)

	deps := SystemRunDeps{
		SendDeniedEvent: func(payload ExecEventPayload) {
			sendNodeEvent(ctx, client, "exec.denied", payload)
		},
		SendFinishedEvent: func(p ExecFinishedEventParams) {
			sendNodeEvent(ctx, client, "exec.finished", p)
		},
		SendResult: func(r SystemRunInvokeResult) {
			if r.Ok {
				sendPayloadJSONResult(ctx, client, frame, r.PayloadJSON)
			} else {
				sendErrorResult(ctx, client, frame, r.ErrorCode, r.ErrorMsg)
			}
		},
	}
	HandleSystemRunInvoke(ctx, params, deps)
}

func handleSystemRunPrepare(_ context.Context, frame NodeInvokeRequestPayload, client GatewayRequester) {
	type prepareParams struct {
		Command    []string `json:"command"`
		RawCommand *string  `json:"rawCommand,omitempty"`
		Cwd        *string  `json:"cwd,omitempty"`
		AgentID    *string  `json:"agentId,omitempty"`
		SessionKey *string  `json:"sessionKey,omitempty"`
	}
	params := decodeParamsAs[prepareParams](frame.ParamsJSON)
	result := BuildSystemRunApprovalPlan(params.Command, params.RawCommand, params.Cwd, params.AgentID, params.SessionKey)
	if !result.Ok {
		sendErrorResult(context.Background(), client, frame, "INVALID_REQUEST", result.Message)
		return
	}
	sendPayloadResult(context.Background(), client, frame, result.Plan)
}

func handleSystemWhich(_ context.Context, frame NodeInvokeRequestPayload, client GatewayRequester) {
	type whichParams struct {
		Bins []string `json:"bins"`
	}
	params := decodeParamsAs[whichParams](frame.ParamsJSON)
	type whichEntry struct {
		Bin  string `json:"bin"`
		Path string `json:"path"`
	}
	var results []whichEntry
	for _, bin := range params.Bins {
		name := strings.TrimSpace(bin)
		if name == "" {
			continue
		}
		resolved, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		results = append(results, whichEntry{Bin: name, Path: resolved})
	}
	sendPayloadResult(context.Background(), client, frame, map[string]any{"bins": results})
}

// --- Helper: decode params ---

func decodeParamsAs[T any](paramsJSON *string) T {
	var result T
	if paramsJSON != nil {
		json.Unmarshal([]byte(*paramsJSON), &result)
	}
	return result
}

// --- Helper: send results ---

func sendPayloadResult(ctx context.Context, client GatewayRequester, frame NodeInvokeRequestPayload, payload any) {
	data, _ := json.Marshal(payload)
	sendPayloadJSONResult(ctx, client, frame, string(data))
}

func sendPayloadJSONResult(ctx context.Context, client GatewayRequester, frame NodeInvokeRequestPayload, payloadJSON string) {
	client.Request(ctx, "node.invoke.result", NodeInvokeResultParams{
		ID:          frame.ID,
		NodeID:      frame.NodeID,
		Ok:          true,
		PayloadJSON: payloadJSON,
	})
}

func sendErrorResult(ctx context.Context, client GatewayRequester, frame NodeInvokeRequestPayload, code, message string) {
	client.Request(ctx, "node.invoke.result", NodeInvokeResultParams{
		ID:     frame.ID,
		NodeID: frame.NodeID,
		Ok:     false,
		Error:  &NodeInvokeResultError{Code: code, Message: message},
	})
}

func sendNodeEvent(ctx context.Context, client GatewayRequester, event string, payload any) {
	data, _ := json.Marshal(payload)
	client.Request(ctx, "node.event", map[string]any{
		"event":       event,
		"payloadJSON": string(data),
	})
}
