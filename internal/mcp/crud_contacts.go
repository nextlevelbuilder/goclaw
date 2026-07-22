package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// registerContactsCRUDTools registers the goclaw_contacts_* MCP tools backed
// by store.ContactStore — closes a CLI-vs-MCP coverage gap (the `goclaw
// channels contacts create/list/verify` commands had no MCP equivalent).
// Contacts are auto-collected from channel traffic (see internal/store
// ContactCollector), so there is no create tool here — only inspection and
// identity merge/unmerge, mirroring internal/http/contact_merge_handlers.go.
func registerContactsCRUDTools(srv *mcpserver.MCPServer, contacts store.ContactStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_contacts_list",
		mcpgo.WithDescription("List/search channel contacts (auto-collected user info from channel traffic)."),
		mcpgo.WithString("search", mcpgo.Description("Search text (matches display name, username, sender ID).")),
		mcpgo.WithString("channel_type", mcpgo.Description("Filter by platform (telegram, discord, etc.).")),
		mcpgo.WithString("channel_instance", mcpgo.Description("Filter by channel instance name.")),
		mcpgo.WithString("peer_kind", mcpgo.Description("Filter by \"direct\" or \"group\".")),
		mcpgo.WithString("contact_type", mcpgo.Description("Filter by \"user\" or \"group\".")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum contacts to return; defaults to 50.")),
		mcpgo.WithNumber("offset", mcpgo.Description("Pagination offset.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleContactsList(contacts))

	srv.AddTool(mcpgo.NewTool("goclaw_contacts_get",
		mcpgo.WithDescription("Get a single contact by UUID."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Contact UUID.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleContactsGet(contacts))

	srv.AddTool(mcpgo.NewTool("goclaw_contacts_merge",
		mcpgo.WithDescription("Merge one or more contacts into a single tenant-user identity."),
		mcpgo.WithArray("contact_ids", mcpgo.Required(), mcpgo.Description("Contact UUIDs to merge.")),
		mcpgo.WithString("tenant_user_id", mcpgo.Required(), mcpgo.Description("Tenant-user UUID to merge into.")),
	), handleContactsMerge(contacts))

	srv.AddTool(mcpgo.NewTool("goclaw_contacts_unmerge",
		mcpgo.WithDescription("Unmerge contacts, unlinking them from their tenant-user identity."),
		mcpgo.WithArray("contact_ids", mcpgo.Required(), mcpgo.Description("Contact UUIDs to unmerge.")),
	), handleContactsUnmerge(contacts))
}

func handleContactsList(contacts store.ContactStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		opts := store.ContactListOpts{
			Search:          req.GetString("search", ""),
			ChannelType:     req.GetString("channel_type", ""),
			ChannelInstance: req.GetString("channel_instance", ""),
			PeerKind:        req.GetString("peer_kind", ""),
			ContactType:     req.GetString("contact_type", ""),
			Limit:           intArg(req, "limit", 50),
			Offset:          intArg(req, "offset", 0),
		}
		list, err := contacts.ListContacts(ctx, opts)
		if err != nil {
			return toolError("contacts.list", err)
		}
		total, err := contacts.CountContacts(ctx, opts)
		if err != nil {
			return toolError("contacts.list", err)
		}
		return jsonToolResult(map[string]any{"contacts": list, "total": total})
	}
}

func handleContactsGet(contacts store.ContactStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("contacts.get", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("contacts.get", fmt.Errorf("invalid id: %w", err))
		}
		contact, err := contacts.GetContactByID(ctx, id)
		if err != nil {
			return toolError("contacts.get", err)
		}
		return jsonToolResult(contact)
	}
}

// parseUUIDArray parses a required array argument as a list of UUIDs.
func parseUUIDArray(req mcpgo.CallToolRequest, key string) ([]uuid.UUID, error) {
	raw, ok := req.GetArguments()[key].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("%s is required and must be a non-empty array", key)
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s: all elements must be strings", key)
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid UUID %q: %w", key, s, err)
		}
		out = append(out, id)
	}
	return out, nil
}

func handleContactsMerge(contacts store.ContactStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		contactIDs, err := parseUUIDArray(req, "contact_ids")
		if err != nil {
			return toolError("contacts.merge", err)
		}
		tenantUserIDStr, err := req.RequireString("tenant_user_id")
		if err != nil {
			return toolError("contacts.merge", err)
		}
		tenantUserID, err := uuid.Parse(tenantUserIDStr)
		if err != nil {
			return toolError("contacts.merge", fmt.Errorf("invalid tenant_user_id: %w", err))
		}
		if err := contacts.MergeContacts(ctx, contactIDs, tenantUserID); err != nil {
			return toolError("contacts.merge", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}

func handleContactsUnmerge(contacts store.ContactStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		contactIDs, err := parseUUIDArray(req, "contact_ids")
		if err != nil {
			return toolError("contacts.unmerge", err)
		}
		if err := contacts.UnmergeContacts(ctx, contactIDs); err != nil {
			return toolError("contacts.unmerge", err)
		}
		return jsonToolResult(map[string]string{"ok": "true"})
	}
}
