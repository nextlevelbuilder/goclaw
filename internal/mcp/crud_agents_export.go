package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// remarshalInto converts a generic decoded-JSON map into a typed struct via
// a JSON round-trip — used to parse the "config" argument (a nested object
// in the MCP tool call) into store.AgentData without hand-mapping every
// field.
func remarshalInto(src map[string]any, dst any) error {
	raw, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// registerAgentExportCRUDTools registers goclaw_agents_export/
// goclaw_agents_import, partially closing the `goclaw export agent`/`import
// agent` CLI-vs-MCP coverage gap. Scoped to agent config + context files
// (SOUL.md, IDENTITY.md, etc.) as a JSON payload — the CLI's full export is
// a multi-section tar archive (config, context files, knowledge graph
// entities, workspace files; see internal/http/agents_export_archive.go)
// built for streaming HTTP download with progress events, a shape that
// doesn't translate to a single MCP tool call. Config + context files is
// the commonly-needed portable subset (an agent's "brain"); KG/workspace
// portability is not covered here.
func registerAgentExportCRUDTools(srv *mcpserver.MCPServer, agents store.AgentStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_agents_export",
		mcpgo.WithDescription("Export an agent's config and context files (SOUL.md, IDENTITY.md, etc.) as a JSON snapshot. Does not include knowledge graph or workspace files — see tool description for the full CLI export's scope."),
		mcpgo.WithString("id", mcpgo.Description("Agent UUID.")),
		mcpgo.WithString("agent_key", mcpgo.Description("Agent key/slug, used when id is not known.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAgentsExport(agents))

	srv.AddTool(mcpgo.NewTool("goclaw_agents_import",
		mcpgo.WithDescription("Create a new agent from a goclaw_agents_export snapshot. Always creates a new agent (never overwrites) — pass a new agent_key to avoid collision, or omit to auto-dedup the exported key."),
		mcpgo.WithObject("config", mcpgo.Required(), mcpgo.Description("The \"config\" object from a goclaw_agents_export snapshot.")),
		mcpgo.WithObject("context_files", mcpgo.Description("The \"context_files\" object from a goclaw_agents_export snapshot (file name -> content).")),
		mcpgo.WithString("agent_key", mcpgo.Description("Override agent_key for the new agent; defaults to the exported agent_key.")),
		mcpgo.WithString("owner_id", mcpgo.Description("Owner user ID; defaults to \"system\".")),
	), handleAgentsImport(agents))
}

func handleAgentsExport(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr := req.GetString("id", "")
		agentKey := req.GetString("agent_key", "")

		var ag *store.AgentData
		var err error
		switch {
		case idStr != "":
			id, parseErr := uuid.Parse(idStr)
			if parseErr != nil {
				return toolError("agents.export", fmt.Errorf("invalid id: %w", parseErr))
			}
			ag, err = agents.GetByID(ctx, id)
		case agentKey != "":
			ag, err = agents.GetByKey(ctx, agentKey)
		default:
			return mcpgo.NewToolResultError("agents.export: one of id or agent_key is required"), nil
		}
		if err != nil {
			return toolError("agents.export", err)
		}

		dbFiles, err := agents.GetAgentContextFiles(ctx, ag.ID)
		if err != nil {
			return toolError("agents.export", err)
		}
		files := make(map[string]string, len(dbFiles))
		for _, f := range dbFiles {
			files[f.FileName] = f.Content
		}

		return jsonToolResult(map[string]any{"config": ag, "context_files": files})
	}
}

func handleAgentsImport(agents store.AgentStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		args := req.GetArguments()
		rawConfig, ok := args["config"].(map[string]any)
		if !ok || len(rawConfig) == 0 {
			return mcpgo.NewToolResultError("agents.import: config is required"), nil
		}

		var ag store.AgentData
		if err := remarshalInto(rawConfig, &ag); err != nil {
			return toolError("agents.import", fmt.Errorf("cannot parse config: %w", err))
		}

		// Always create a new agent — never overwrite an existing one via import.
		ag.ID = store.GenNewID()
		ag.CreatedAt = time.Time{}
		ag.UpdatedAt = time.Time{}
		if agentKey := req.GetString("agent_key", ""); agentKey != "" {
			ag.AgentKey = agentKey
		}
		if ag.AgentKey == "" {
			return mcpgo.NewToolResultError("agents.import: config.agent_key is required (or pass agent_key)"), nil
		}
		ag.OwnerID = req.GetString("owner_id", "system")
		tenantID := store.TenantIDFromContext(ctx)
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}
		ag.TenantID = tenantID

		if err := agents.Create(ctx, &ag); err != nil {
			return toolError("agents.import", err)
		}

		filesWritten := 0
		if rawFiles, ok := args["context_files"].(map[string]any); ok {
			for name, v := range rawFiles {
				content, ok := v.(string)
				if !ok || !isAllowedAgentContextFile(name) {
					continue
				}
				if err := agents.SetAgentContextFile(ctx, ag.ID, name, content); err == nil {
					filesWritten++
				}
			}
		}

		return jsonToolResult(map[string]any{"id": ag.ID.String(), "agent_key": ag.AgentKey, "files_written": filesWritten})
	}
}
