package mcp

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/nextlevelbuilder/goclaw/internal/audio"
	"github.com/nextlevelbuilder/goclaw/internal/audio/elevenlabs"
	"github.com/nextlevelbuilder/goclaw/internal/audio/minimax"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const voicesRequestTimeoutMS = 15000

// registerVoicesCRUDTools registers goclaw_voices_{list,refresh}, backed by
// the same audio.VoiceCache shared with the gateway's voices.list/refresh WS
// methods (internal/gateway/methods/voices_list.go) and HTTP endpoints
// (internal/http/voices.go). Provider resolution mirrors
// internal/http/voices.go's resolveProvider (duplicated rather than imported:
// internal/http already imports this package for the MCP tool bridge, so
// importing internal/http here would create a cycle) — resolves a per-tenant
// API key from secretStore, defaulting to ElevenLabs.
func registerVoicesCRUDTools(srv *mcpserver.MCPServer, cache *audio.VoiceCache, secretStore store.ConfigSecretsStore) {
	srv.AddTool(mcpgo.NewTool("goclaw_voices_list",
		mcpgo.WithDescription("List available TTS voices for the caller's tenant (cached)."),
		mcpgo.WithString("provider", mcpgo.Description("\"elevenlabs\" (default) or \"minimax\".")),
		mcpgo.WithReadOnlyHintAnnotation(true),
	), handleVoicesList(cache, secretStore))

	srv.AddTool(mcpgo.NewTool("goclaw_voices_refresh",
		mcpgo.WithDescription("Invalidate the voice cache and re-fetch from the TTS provider."),
		mcpgo.WithString("provider", mcpgo.Description("\"elevenlabs\" (default) or \"minimax\".")),
	), handleVoicesRefresh(cache, secretStore))
}

func handleVoicesList(cache *audio.VoiceCache, secretStore store.ConfigSecretsStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if cache == nil {
			return mcpgo.NewToolResultError("voices.list: voice cache not available"), nil
		}
		tenantID := store.TenantIDFromContext(ctx)
		if voices, ok := cache.Get(tenantID); ok {
			return jsonToolResult(map[string]any{"voices": voices})
		}
		p, err := resolveVoiceProvider(ctx, secretStore, tenantID, req.GetString("provider", ""))
		if err != nil {
			return toolError("voices.list", err)
		}
		voices, err := p.ListVoices(ctx)
		if err != nil {
			return toolError("voices.list", err)
		}
		cache.Set(tenantID, voices)
		return jsonToolResult(map[string]any{"voices": voices})
	}
}

func handleVoicesRefresh(cache *audio.VoiceCache, secretStore store.ConfigSecretsStore) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		if cache == nil {
			return mcpgo.NewToolResultError("voices.refresh: voice cache not available"), nil
		}
		tenantID := store.TenantIDFromContext(ctx)
		cache.Invalidate(tenantID)

		p, err := resolveVoiceProvider(ctx, secretStore, tenantID, req.GetString("provider", ""))
		if err != nil {
			return toolError("voices.refresh", err)
		}
		voices, err := p.ListVoices(ctx)
		if err != nil {
			return toolError("voices.refresh", err)
		}
		cache.Set(tenantID, voices)
		return jsonToolResult(map[string]any{"voices": voices})
	}
}

func resolveVoiceProvider(ctx context.Context, secretStore store.ConfigSecretsStore, tenantID uuid.UUID, providerName string) (audio.VoiceListProvider, error) {
	if secretStore == nil {
		return nil, fmt.Errorf("no voice provider configured")
	}
	if providerName == "" {
		providerName = "elevenlabs"
	}

	switch providerName {
	case "minimax":
		apiKey, err := secretStore.Get(ctx, "tts.minimax.api_key")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("MiniMax API key not found for tenant %s", tenantID)
		}
		apiBase, _ := secretStore.Get(ctx, "tts.minimax.api_base")
		return minimax.NewVoiceLister(apiKey, apiBase, voicesRequestTimeoutMS, tenantID), nil
	case "elevenlabs":
		apiKey, err := secretStore.Get(ctx, "tts.elevenlabs.api_key")
		if err != nil || apiKey == "" {
			return nil, fmt.Errorf("ElevenLabs API key not found for tenant %s", tenantID)
		}
		return elevenlabs.NewTTSProvider(elevenlabs.Config{APIKey: apiKey}), nil
	default:
		return nil, fmt.Errorf("unsupported voice provider: %s", providerName)
	}
}
