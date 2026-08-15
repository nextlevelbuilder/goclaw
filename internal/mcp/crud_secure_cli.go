package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerSecureCLICRUDTools registers the goclaw_secure_cli_binaries_* MCP
// tools backed by store.SecureCLIStore — this is the closest real
// server-side resource to the CLI's local `goclaw credentials` command,
// which is otherwise out of scope for this MCP surface (it manages the
// `goclaw` CLI's own auth profile in ~/.goclaw/config.yaml + OS keychain,
// a client-local concept with no server API to wrap). SecureCLIStore
// instead manages which exec-sandboxed binaries (gh, git, etc.) are
// credential-gated and how — the actual secret values (encrypted_env) are
// never exposed here (SecureCLIBinary.EncryptedEnv/UserEnv are json:"-").
// Per-user/per-agent credential value CRUD (SetUserCredentials et al.,
// internal/http/secure_cli_user_credentials.go /
// secure_cli_agent_credentials.go) needs the same encryption handling the
// HTTP layer owns and is not exposed here.
func registerSecureCLICRUDTools(srv *mcpserver.MCPServer, secureCLI store.SecureCLIStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_secure_cli_binaries_list",
		mcpgo.WithDescription("List registered secure-CLI binary configs (which sandboxed exec binaries are credential-gated)."),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSecureCLIBinariesList(secureCLI))

	srv.AddTool(mcpgo.NewTool("goclaw_secure_cli_binaries_get",
		mcpgo.WithDescription("Get a single secure-CLI binary config by UUID."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Binary config UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleSecureCLIBinariesGet(secureCLI))

	srv.AddTool(mcpgo.NewTool("goclaw_secure_cli_binaries_create",
		mcpgo.WithDescription("Register a new secure-CLI binary config."),
		mcpgo.WithString("binary_name", mcpgo.Required(), mcpgo.Description("Binary name (e.g. \"gh\", \"git\").")),
		mcpgo.WithString("description", mcpgo.Description("Description shown to agents.")),
		mcpgo.WithBoolean("is_global", mcpgo.Description("Whether all agents can use this binary without a grant; defaults to false.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("Enabled state; defaults to true.")),
		mcpgo.WithNumber("timeout_seconds", mcpgo.Description("Exec timeout in seconds.")),
	), handleSecureCLIBinariesCreate(secureCLI))

	srv.AddTool(mcpgo.NewTool("goclaw_secure_cli_binaries_update",
		mcpgo.WithDescription("Apply a partial update to a secure-CLI binary config."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Binary config UUID.")),
		mcpgo.WithString("description", mcpgo.Description("New description.")),
		mcpgo.WithBoolean("is_global", mcpgo.Description("New is_global value.")),
		mcpgo.WithBoolean("enabled", mcpgo.Description("New enabled value.")),
		mcpgo.WithNumber("timeout_seconds", mcpgo.Description("New exec timeout in seconds.")),
	), handleSecureCLIBinariesUpdate(secureCLI))

	srv.AddTool(mcpgo.NewTool("goclaw_secure_cli_binaries_delete",
		mcpgo.WithDescription("Delete a secure-CLI binary config."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Binary config UUID.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleSecureCLIBinariesDelete(secureCLI))
}

func handleSecureCLIBinariesList(secureCLI store.SecureCLIStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		list, err := secureCLI.List(ctx)
		if err != nil {
			return toolError("secure_cli_binaries.list", err)
		}
		return jsonToolResult(list)
	}
}

func handleSecureCLIBinariesGet(secureCLI store.SecureCLIStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("secure_cli_binaries.get", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("secure_cli_binaries.get", fmt.Errorf("invalid id: %w", err))
		}
		b, err := secureCLI.Get(ctx, id)
		if err != nil {
			return toolError("secure_cli_binaries.get", err)
		}
		return jsonToolResult(b)
	}
}

func handleSecureCLIBinariesCreate(secureCLI store.SecureCLIStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		binaryName, err := req.RequireString("binary_name")
		if err != nil {
			return toolError("secure_cli_binaries.create", err)
		}
		b := &store.SecureCLIBinary{
			BinaryName:     binaryName,
			Description:    req.GetString("description", ""),
			IsGlobal:       req.GetBool("is_global", false),
			Enabled:        req.GetBool("enabled", true),
			TimeoutSeconds: intArg(req, "timeout_seconds", 0),
			CreatedBy:      "mcp",
		}
		if err := secureCLI.Create(ctx, b); err != nil {
			return toolError("secure_cli_binaries.create", err)
		}
		return jsonToolResult(b)
	}
}

func handleSecureCLIBinariesUpdate(secureCLI store.SecureCLIStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("secure_cli_binaries.update", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("secure_cli_binaries.update", fmt.Errorf("invalid id: %w", err))
		}

		updates := map[string]any{}
		args := req.GetArguments()
		if v, ok := args["description"]; ok {
			updates["description"] = v
		}
		if v, ok := args["is_global"]; ok {
			updates["is_global"] = v
		}
		if v, ok := args["enabled"]; ok {
			updates["enabled"] = v
		}
		if v, ok := args["timeout_seconds"]; ok {
			updates["timeout_seconds"] = v
		}
		if len(updates) == 0 {
			return mcpgo.NewToolResultError("secure_cli_binaries.update: no fields to update"), nil
		}

		if err := secureCLI.Update(ctx, id, updates); err != nil {
			return toolError("secure_cli_binaries.update", err)
		}
		b, err := secureCLI.Get(ctx, id)
		if err != nil {
			return toolError("secure_cli_binaries.update", err)
		}
		return jsonToolResult(b)
	}
}

func handleSecureCLIBinariesDelete(secureCLI store.SecureCLIStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("secure_cli_binaries.delete", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("secure_cli_binaries.delete", fmt.Errorf("invalid id: %w", err))
		}
		if err := secureCLI.Delete(ctx, id); err != nil {
			return toolError("secure_cli_binaries.delete", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
