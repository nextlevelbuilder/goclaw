package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// ToolsInvokeHandler handles POST /v1/tools/invoke (direct tool invocation).
type ToolsInvokeHandler struct {
	registry   *tools.Registry
	token      string
	agentStore store.AgentStore // nil if not configured
}

// NewToolsInvokeHandler creates a handler for the tools invoke endpoint.
func NewToolsInvokeHandler(registry *tools.Registry, token string, agentStore store.AgentStore) *ToolsInvokeHandler {
	return &ToolsInvokeHandler{
		registry:   registry,
		token:      token,
		agentStore: agentStore,
	}
}

type toolsInvokeRequest struct {
	Tool       string                 `json:"tool"`
	Action     string                 `json:"action,omitempty"`
	Args       map[string]interface{} `json:"args"`
	SessionKey string                 `json:"sessionKey,omitempty"`
	AgentID    string                 `json:"agentId,omitempty"`
	DryRun     bool                   `json:"dryRun,omitempty"`
}

func (h *ToolsInvokeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.token != "" {
		if extractBearerToken(r) != h.token {
			http.Error(w, `{"error":{"code":"UNAUTHORIZED","message":"Invalid token"}}`, http.StatusUnauthorized)
			return
		}
	}

	var req toolsInvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":{"code":"BAD_REQUEST","message":"%s"}}`, err), http.StatusBadRequest)
		return
	}

	if req.Tool == "" {
		http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"tool is required"}}`, http.StatusBadRequest)
		return
	}

	slog.Info("tools invoke request", "tool", req.Tool, "dry_run", req.DryRun)

	if req.DryRun {
		// Just check if tool exists and return its schema
		tool, ok := h.registry.Get(req.Tool)
		if !ok {
			writeToolError(w, http.StatusNotFound, "NOT_FOUND", fmt.Sprintf("Tool '%s' not found", req.Tool))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tool":        req.Tool,
			"description": tool.Description(),
			"parameters":  tool.Parameters(),
			"dryRun":      true,
		})
		return
	}

	// Inject userID and agentID into context for interceptors (bootstrap, memory).
	ctx := r.Context()

	if userID := extractUserID(r); userID != "" {
		ctx = store.WithUserID(ctx, userID)
	}

	agentIDStr := req.AgentID
	if agentIDStr == "" {
		agentIDStr = extractAgentID(r, "")
	}
	if agentIDStr != "" && h.agentStore != nil {
		ag, err := h.agentStore.GetByKey(ctx, agentIDStr)
		if err == nil {
			ctx = store.WithAgentID(ctx, ag.ID)
		}
	}

	// Execute the tool
	args := req.Args
	if args == nil {
		args = make(map[string]interface{})
	}

	// If action is specified, add it to args
	if req.Action != "" {
		args["action"] = req.Action
	}

	result := h.registry.ExecuteWithContext(ctx, req.Tool, args, "http", "api", "direct", "", nil)

	if result.IsError {
		writeToolError(w, http.StatusBadRequest, "TOOL_ERROR", result.ForLLM)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"result": map[string]interface{}{
			"output":   result.ForLLM,
			"forUser":  result.ForUser,
			"metadata": map[string]interface{}{},
		},
	})
}

func writeToolError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
