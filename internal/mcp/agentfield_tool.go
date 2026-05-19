package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

const (
	defaultAgentFieldBaseURL = "http://127.0.0.1:18088"
	defaultAgentFieldTimeout = 90
)

// AgentFieldExecuteTool bridges GoClaw's MCP runtime to an AgentField control plane.
//
// This intentionally calls AgentField's /api/v1/execute/{node.reasoner} endpoint
// instead of calling an agent node directly, so the execution goes through
// AgentField registration, routing, execution records, and logs.
type AgentFieldExecuteTool struct {
	httpClient *http.Client
}

func NewAgentFieldExecuteTool() *AgentFieldExecuteTool {
	return &AgentFieldExecuteTool{
		httpClient: &http.Client{Timeout: 10 * time.Minute},
	}
}

func (t *AgentFieldExecuteTool) Name() string { return "agentfield_execute" }

func (t *AgentFieldExecuteTool) Description() string {
	return "Execute a registered AgentField reasoner through the AgentField control plane. Minimal POC target: meeting-agent.prepare."
}

func (t *AgentFieldExecuteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target": map[string]any{
				"type":        "string",
				"description": "AgentField target in node.reasoner format, for example meeting-agent.prepare.",
			},
			"input": map[string]any{
				"type":        "object",
				"description": "Input object passed as the AgentField execute request's input field.",
			},
			"context": map[string]any{
				"type":        "object",
				"description": "Optional AgentField execution context object.",
			},
			"timeout_sec": map[string]any{
				"type":        "integer",
				"description": "Request timeout in seconds. Defaults to 90.",
			},
			"base_url": map[string]any{
				"type":        "string",
				"description": "Optional AgentField control plane base URL. Defaults to AGENTFIELD_BASE_URL or http://127.0.0.1:18088.",
			},
		},
		"required": []string{"target"},
	}
}

func (t *AgentFieldExecuteTool) Execute(ctx context.Context, args map[string]any) *tools.Result {
	target := strings.TrimSpace(asString(args["target"]))
	if err := validateAgentFieldTarget(target); err != nil {
		return tools.ErrorResult("agentfield_execute: " + err.Error())
	}

	input := map[string]any{}
	if rawInput, ok := args["input"]; ok && rawInput != nil {
		parsedInput, ok := rawInput.(map[string]any)
		if !ok {
			return tools.ErrorResult("agentfield_execute: input must be an object")
		}
		input = parsedInput
	}

	body := map[string]any{"input": input}
	if rawContext, ok := args["context"]; ok && rawContext != nil {
		parsedContext, ok := rawContext.(map[string]any)
		if !ok {
			return tools.ErrorResult("agentfield_execute: context must be an object")
		}
		body["context"] = parsedContext
	}

	baseURL := strings.TrimRight(firstNonEmpty(
		asString(args["base_url"]),
		os.Getenv("AGENTFIELD_BASE_URL"),
		defaultAgentFieldBaseURL,
	), "/")
	timeoutSec := intArg(args["timeout_sec"], defaultAgentFieldTimeout)
	if timeoutSec <= 0 {
		timeoutSec = defaultAgentFieldTimeout
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	start := time.Now()
	slog.Info("mcp.agentfield.execute.start",
		"target", target,
		"base_url", baseURL,
		"timeout_sec", timeoutSec,
	)

	raw, statusCode, err := t.postExecute(callCtx, baseURL, target, body)
	duration := time.Since(start)
	if err != nil {
		slog.Error("mcp.agentfield.execute.failed",
			"target", target,
			"base_url", baseURL,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return tools.ErrorResult("agentfield_execute: " + err.Error())
	}
	if statusCode >= http.StatusBadRequest {
		slog.Error("mcp.agentfield.execute.bad_status",
			"target", target,
			"base_url", baseURL,
			"status", statusCode,
			"duration_ms", duration.Milliseconds(),
			"body", truncate(string(raw), 1000),
		)
		return tools.ErrorResult(fmt.Sprintf("agentfield_execute: POST %s status %d: %s", baseURL, statusCode, truncate(string(raw), 1000)))
	}

	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return tools.NewResult(string(raw))
	}

	result := map[string]any{
		"agentfield_ok": true,
		"target":        target,
		"duration_ms":   duration.Milliseconds(),
		"status":        parsed["status"],
		"execution_id":  parsed["execution_id"],
		"run_id":        parsed["run_id"],
		"result":        parsed["result"],
	}
	slog.Info("mcp.agentfield.execute.completed",
		"target", target,
		"status", parsed["status"],
		"execution_id", parsed["execution_id"],
		"duration_ms", duration.Milliseconds(),
	)
	return tools.NewResult(compactJSON(result))
}

func (t *AgentFieldExecuteTool) postExecute(ctx context.Context, baseURL, target string, body map[string]any) ([]byte, int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("encode request: %w", err)
	}

	endpoint := baseURL + "/api/v1/execute/" + url.PathEscape(target)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := strings.TrimSpace(os.Getenv("AGENTFIELD_API_KEY")); apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	return raw, resp.StatusCode, nil
}

func validateAgentFieldTarget(target string) error {
	if target == "" {
		return fmt.Errorf("target is required")
	}
	if strings.Count(target, ".") != 1 {
		return fmt.Errorf("target must be in node.reasoner format")
	}
	if strings.ContainsAny(target, "/?#") {
		return fmt.Errorf("target contains invalid URL path characters")
	}
	return nil
}
