package cmd

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// builtinToolSeedData returns the canonical list of built-in tools to seed into the database.
// Seed preserves user-customized enabled/settings values across upgrades.
func builtinToolSeedData() []store.BuiltinToolDef {
	return []store.BuiltinToolDef{
		// filesystem
		{Name: "read_file", DisplayName: "Read File", Description: "Read the contents of a file from the agent's workspace by path", Category: "filesystem", Enabled: true},
		{Name: "write_file", DisplayName: "Write File", Description: "Write content to a file in the workspace, creating directories as needed", Category: "filesystem", Enabled: true},
		{Name: "list_files", DisplayName: "List Files", Description: "List files and directories in a given path within the workspace", Category: "filesystem", Enabled: true},
		{Name: "edit", DisplayName: "Edit File", Description: "Apply targeted search-and-replace edits to existing files without rewriting the entire file", Category: "filesystem", Enabled: true},

		// runtime
		{Name: "exec", DisplayName: "Execute Command", Description: "Execute a shell command in the workspace and return stdout/stderr", Category: "runtime", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Exec Approval"}`),
		},

		// web
		{Name: "web_search", DisplayName: "Web Search", Description: "Search the web for information using a search engine (Brave or DuckDuckGo)", Category: "web", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Web Search"}`),
		},
		{Name: "web_fetch", DisplayName: "Web Fetch", Description: "Fetch a web page or API endpoint and extract its text content", Category: "web", Enabled: true},

		// memory
		{Name: "memory_search", DisplayName: "Memory Search", Description: "Search through the agent's long-term memory using semantic similarity", Category: "memory", Enabled: true,
			Requires: []string{"memory"},
		},
		{Name: "memory_get", DisplayName: "Memory Get", Description: "Retrieve a specific memory document by its file path", Category: "memory", Enabled: true,
			Requires: []string{"memory"},
		},
		{Name: "knowledge_graph_search", DisplayName: "Knowledge Graph Search", Description: "Search entities, relationships, and observations in the agent's knowledge graph", Category: "memory", Enabled: true,
			Settings: json.RawMessage(`{"extract_on_memory_write":false,"extraction_provider":"","extraction_model":"","min_confidence":0.75}`),
			Requires: []string{"knowledge_graph"},
		},

		// media — seed uses chain format: {"providers":[...]}
		{Name: "read_image", DisplayName: "Read Image", Description: "Analyze images using a vision-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"openrouter","model":"google/gemini-2.5-flash-image","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"vision_provider"},
		},
		{Name: "read_document", DisplayName: "Read Document", Description: "Analyze documents (PDF, Word, Excel, PowerPoint, CSV, etc.) using a document-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"gemini","model":"gemini-2.5-flash","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"document_provider"},
		},
		{Name: "create_image", DisplayName: "Create Image", Description: "Generate images from text prompts using an image generation provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"openrouter","model":"google/gemini-2.5-flash-image","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"image_gen_provider"},
		},
		{Name: "read_audio", DisplayName: "Read Audio", Description: "Analyze audio files (speech, music, sounds) using an audio-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"gemini","model":"gemini-2.5-flash","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"audio_provider"},
		},
		{Name: "read_video", DisplayName: "Read Video", Description: "Analyze video files using a video-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"gemini","model":"gemini-2.5-flash","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"video_provider"},
		},
		{Name: "create_video", DisplayName: "Create Video", Description: "Generate videos from text descriptions using AI", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"gemini","model":"veo-3.0-generate-preview","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"video_gen_provider"},
		},
		{Name: "create_audio", DisplayName: "Create Audio", Description: "Generate music or sound effects from text descriptions using AI", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"providers":[{"provider":"minimax","model":"music-2.5+","enabled":true,"timeout":120,"max_retries":2}]}`),
			Requires: []string{"audio_gen_provider"},
		},
		{Name: "tts", DisplayName: "Text to Speech", Description: "Convert text to natural-sounding speech audio", Category: "media", Enabled: true,
			Requires: []string{"tts_provider"},
			Metadata: json.RawMessage(`{"config_hint":"Config → TTS"}`),
		},

		// browser
		{Name: "browser", DisplayName: "Browser", Description: "Automate browser interactions: navigate pages, click elements, fill forms, take screenshots", Category: "browser", Enabled: true,
			Requires: []string{"browser"},
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Browser"}`),
		},

		// sessions
		{Name: "sessions_list", DisplayName: "List Sessions", Description: "List active chat sessions across all channels", Category: "sessions", Enabled: true},
		{Name: "session_status", DisplayName: "Session Status", Description: "Get the current status and metadata of a specific chat session", Category: "sessions", Enabled: true},
		{Name: "sessions_history", DisplayName: "Session History", Description: "Retrieve the message history of a specific chat session", Category: "sessions", Enabled: true},
		{Name: "sessions_send", DisplayName: "Send to Session", Description: "Send a message to an active chat session on behalf of the agent", Category: "sessions", Enabled: true},

		// messaging
		{Name: "message", DisplayName: "Message", Description: "Send a proactive message to a user on a connected channel (Telegram, Discord, etc.)", Category: "messaging", Enabled: true},

		// scheduling
		{Name: "cron", DisplayName: "Cron Scheduler", Description: "Schedule or manage recurring tasks using cron expressions, at-times, or intervals", Category: "scheduling", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Cron"}`),
		},

		// subagents & delegation (unified spawn tool)
		{Name: "spawn", DisplayName: "Spawn / Delegate", Description: "Spawn a subagent for background work or delegate a task to a linked agent", Category: "subagents", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Agents Defaults"}`),
		},

		// skills
		{Name: "skill_search", DisplayName: "Skill Search", Description: "Search for available skills by keyword or description to find relevant capabilities", Category: "skills", Enabled: true},
		{Name: "use_skill", DisplayName: "Use Skill", Description: "Activate a skill to use its specialized capabilities (tracing marker)", Category: "skills", Enabled: true},
		{Name: "publish_skill", DisplayName: "Publish Skill", Description: "Register a skill directory (created via skill-creator) in the system database, making it discoverable and grantable to agents", Category: "skills", Enabled: true},

		// delegation
		{Name: "delegate_search", DisplayName: "Delegate Search", Description: "Search for available delegation targets by keyword when there are too many linked agents to list", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},
		{Name: "evaluate_loop", DisplayName: "Evaluate Loop", Description: "Run a generate→evaluate→revise loop between two agents for quality-critical output", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},
		{Name: "handoff", DisplayName: "Handoff", Description: "Transfer the conversation to another agent — the user will talk directly to that agent", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},

		// teams
		{Name: "team_tasks", DisplayName: "Team Tasks", Description: "View, create, update, and complete tasks on the team task board", Category: "teams", Enabled: true,
			Requires: []string{"managed_mode", "teams"},
		},
		{Name: "team_message", DisplayName: "Team Message", Description: "Send a direct message or broadcast to teammates in the agent team", Category: "teams", Enabled: true,
			Requires: []string{"managed_mode", "teams"},
		},
	}
}

// seedBuiltinTools seeds built-in tool definitions into the database.
// Idempotent: preserves user-customized enabled/settings on conflict.
func seedBuiltinTools(ctx context.Context, bts store.BuiltinToolStore) {
	seeds := builtinToolSeedData()
	if err := bts.Seed(ctx, seeds); err != nil {
		slog.Error("failed to seed builtin tools", "error", err)
		return
	}
	slog.Info("builtin tools seeded", "count", len(seeds))
}

// mediaToolNames lists media tools whose settings should use chain format.
var mediaToolNames = map[string]bool{
	"read_image": true, "read_document": true, "create_image": true,
	"read_audio": true, "read_video": true, "create_video": true, "create_audio": true,
}

// migrateBuiltinToolSettings converts legacy flat settings {"provider":"X","model":"Y"}
// to chain format {"providers":[...]} in the database. Runs once at startup.
func migrateBuiltinToolSettings(ctx context.Context, bts store.BuiltinToolStore) {
	all, err := bts.List(ctx)
	if err != nil {
		slog.Warn("builtin_tools: failed to list for migration", "error", err)
		return
	}

	var migrated int
	for _, t := range all {
		if !mediaToolNames[t.Name] {
			continue
		}
		if len(t.Settings) == 0 || string(t.Settings) == "{}" {
			continue
		}

		// Detect legacy flat format: has "provider" key but no "providers" key
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(t.Settings, &raw); err != nil {
			continue
		}
		if _, hasProviders := raw["providers"]; hasProviders {
			continue // already chain format
		}
		if _, hasProvider := raw["provider"]; !hasProvider {
			continue // neither format, skip
		}

		// Parse legacy flat fields
		var flat struct {
			Provider string `json:"provider"`
			Model    string `json:"model"`
		}
		if err := json.Unmarshal(t.Settings, &flat); err != nil || flat.Provider == "" {
			continue
		}

		// Convert to chain format
		chain := map[string]any{
			"providers": []map[string]any{{
				"provider":    flat.Provider,
				"model":       flat.Model,
				"enabled":     true,
				"timeout":     120,
				"max_retries": 2,
			}},
		}
		newSettings, err := json.Marshal(chain)
		if err != nil {
			continue
		}

		if err := bts.Update(ctx, t.Name, map[string]any{"settings": json.RawMessage(newSettings)}); err != nil {
			slog.Warn("builtin_tools: failed to migrate settings", "tool", t.Name, "error", err)
			continue
		}
		migrated++
	}

	if migrated > 0 {
		slog.Info("builtin_tools: migrated legacy settings to chain format", "count", migrated)
	}
}

// applyBuiltinToolDisables unregisters disabled builtin tools from the registry.
// Called at startup and on cache invalidation.
func applyBuiltinToolDisables(ctx context.Context, bts store.BuiltinToolStore, toolsReg *tools.Registry) {
	all, err := bts.List(ctx)
	if err != nil {
		slog.Warn("failed to list builtin tools for disable check", "error", err)
		return
	}

	var disabled int
	for _, t := range all {
		if !t.Enabled {
			toolsReg.Unregister(t.Name)
			disabled++
		}
	}
	if disabled > 0 {
		slog.Info("builtin tools disabled", "count", disabled)
	}
}
