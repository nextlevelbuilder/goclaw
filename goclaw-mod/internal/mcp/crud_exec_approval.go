package mcp

import (
	"context"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// registerExecApprovalCRUDTools registers the goclaw_exec_approval_* MCP
// tools backed by *tools.ExecApprovalManager. Mirrors
// internal/gateway/methods/exec_approval.go.
func registerExecApprovalCRUDTools(srv *mcpserver.MCPServer, manager *tools.ExecApprovalManager) {
	srv.AddTool(mcpgo.NewTool("goclaw_exec_approval_list",
		mcpgo.WithDescription("List pending shell exec approvals."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleExecApprovalList(manager))

	srv.AddTool(mcpgo.NewTool("goclaw_exec_approval_approve",
		mcpgo.WithDescription("Approve a pending shell exec approval."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Approval ID.")),
		mcpgo.WithBoolean("always", mcpgo.Description("true = allow-always, false (default) = allow-once.")),
	), handleExecApprovalApprove(manager))

	srv.AddTool(mcpgo.NewTool("goclaw_exec_approval_deny",
		mcpgo.WithDescription("Deny a pending shell exec approval."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Approval ID.")),
	), handleExecApprovalDeny(manager))
}

func handleExecApprovalList(manager *tools.ExecApprovalManager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		pending := manager.ListPending()
		type pendingInfo struct {
			ID        string `json:"id"`
			Command   string `json:"command"`
			AgentID   string `json:"agentId"`
			CreatedAt int64  `json:"createdAt"`
		}
		items := make([]pendingInfo, 0, len(pending))
		for _, pa := range pending {
			items = append(items, pendingInfo{
				ID: pa.ID, Command: pa.Command, AgentID: pa.AgentID, CreatedAt: pa.CreatedAt.UnixMilli(),
			})
		}
		return jsonToolResult(map[string]any{"pending": items})
	}
}

func handleExecApprovalApprove(manager *tools.ExecApprovalManager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolError("exec.approval.approve", err)
		}
		decision := tools.ApprovalAllowOnce
		if req.GetBool("always", false) {
			decision = tools.ApprovalAllowAlways
		}
		if err := manager.Resolve(id, decision); err != nil {
			return toolError("exec.approval.approve", err)
		}
		return jsonToolResult(map[string]any{"resolved": true, "decision": string(decision)})
	}
}

func handleExecApprovalDeny(manager *tools.ExecApprovalManager) mcpserver.ToolHandlerFunc {
	return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		id, err := req.RequireString("id")
		if err != nil {
			return toolError("exec.approval.deny", err)
		}
		if err := manager.Resolve(id, tools.ApprovalDeny); err != nil {
			return toolError("exec.approval.deny", err)
		}
		return jsonToolResult(map[string]any{"resolved": true, "decision": "deny"})
	}
}
