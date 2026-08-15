package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const apiKeyRawBytes = 32

// registerAPIKeyCRUDTools registers the goclaw_api_keys_* MCP tools backed by
// store.APIKeyStore.
func registerAPIKeyCRUDTools(srv *mcpserver.MCPServer, apiKeys store.APIKeyStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_api_keys_list",
		mcpgo.WithDescription("List API keys visible to the caller."),
		mcpgo.WithString("owner_id", mcpgo.Description("Filter by owner user ID; empty lists all keys.")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleAPIKeysList(apiKeys))

	srv.AddTool(mcpgo.NewTool("goclaw_api_keys_create",
		mcpgo.WithDescription("Create a new API key. The raw key value is only returned once."),
		mcpgo.WithString("name", mcpgo.Required(), mcpgo.Description("Descriptive name for the key.")),
		mcpgo.WithArray("scopes", mcpgo.Required(), mcpgo.Description("Scopes granted to this key (e.g. [\"operator.admin\"]).")),
		mcpgo.WithNumber("expires_in", mcpgo.Description("Expiry in seconds from now; omit for a non-expiring key.")),
		mcpgo.WithString("owner_id", mcpgo.Description("User ID this key is bound to.")),
	), handleAPIKeysCreate(apiKeys))

	srv.AddTool(mcpgo.NewTool("goclaw_api_keys_revoke",
		mcpgo.WithDescription("Revoke an API key."),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("API key UUID.")),
		mcpgo.WithString("owner_id", mcpgo.Description("If set, also enforces owner_id match before revoking.")),
		mcpgo.WithDestructiveHintAnnotation(true),
	), handleAPIKeysRevoke(apiKeys))
}

func handleAPIKeysList(apiKeys store.APIKeyStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		ownerID := req.GetString("owner_id", "")
		list, err := apiKeys.List(ctx, ownerID)
		if err != nil {
			return toolError("api_keys.list", err)
		}
		return jsonToolResult(list)
	}
}

func handleAPIKeysCreate(apiKeys store.APIKeyStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		name, err := req.RequireString("name")
		if err != nil {
			return toolError("api_keys.create", err)
		}
		scopesRaw, err := req.RequireStringSlice("scopes")
		if err != nil {
			return toolError("api_keys.create", err)
		}

		rawKey := make([]byte, apiKeyRawBytes)
		if _, err := rand.Read(rawKey); err != nil {
			return toolError("api_keys.create", fmt.Errorf("generate key: %w", err))
		}
		rawKeyHex := hex.EncodeToString(rawKey)
		hash := sha256.Sum256([]byte(rawKeyHex))
		keyHash := hex.EncodeToString(hash[:])

		var expiresAt *time.Time
		if expiresIn := req.GetFloat("expires_in", 0); expiresIn > 0 {
			t := time.Now().Add(time.Duration(expiresIn) * time.Second)
			expiresAt = &t
		}

		data := &store.APIKeyData{
			ID:        store.GenNewID(),
			Name:      name,
			Prefix:    rawKeyHex[:apiKeyPrefixLen],
			KeyHash:   keyHash,
			Scopes:    scopesRaw,
			OwnerID:   req.GetString("owner_id", ""),
			ExpiresAt: expiresAt,
		}
		if err := apiKeys.Create(ctx, data); err != nil {
			return toolError("api_keys.create", err)
		}
		return jsonToolResult(map[string]any{
			"id":         data.ID,
			"name":       data.Name,
			"prefix":     data.Prefix,
			"key":        rawKeyHex,
			"scopes":     data.Scopes,
			"expires_at": data.ExpiresAt,
			"created_at": data.CreatedAt,
		})
	}
}

const apiKeyPrefixLen = 8

func handleAPIKeysRevoke(apiKeys store.APIKeyStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		idStr, err := req.RequireString("id")
		if err != nil {
			return toolError("api_keys.revoke", err)
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return toolError("api_keys.revoke", fmt.Errorf("invalid id: %w", err))
		}
		ownerID := req.GetString("owner_id", "")
		if err := apiKeys.Revoke(ctx, id, ownerID); err != nil {
			return toolError("api_keys.revoke", err)
		}
		return jsonToolResult(map[string]string{"status": "revoked"})
	}
}
