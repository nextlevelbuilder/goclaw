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
		{Name: "read_file", DisplayName: "Read File", Description: "Read file contents from the workspace", Category: "filesystem", Enabled: true},
		{Name: "write_file", DisplayName: "Write File", Description: "Write or create files in the workspace", Category: "filesystem", Enabled: true},
		{Name: "list_files", DisplayName: "List Files", Description: "List files and directories in the workspace", Category: "filesystem", Enabled: true},
		{Name: "edit", DisplayName: "Edit File", Description: "Apply targeted edits to files (search and replace)", Category: "filesystem", Enabled: true},

		// runtime
		{Name: "exec", DisplayName: "Execute Command", Description: "Execute shell commands in the workspace", Category: "runtime", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Exec Approval"}`),
		},

		// web
		{Name: "web_search", DisplayName: "Web Search", Description: "Search the web using Brave or DuckDuckGo", Category: "web", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Web Search"}`),
		},
		{Name: "web_fetch", DisplayName: "Web Fetch", Description: "Fetch and extract content from web URLs", Category: "web", Enabled: true},

		// memory
		{Name: "memory_search", DisplayName: "Memory Search", Description: "Search through stored memory entries", Category: "memory", Enabled: true,
			Requires: []string{"memory"},
		},
		{Name: "memory_get", DisplayName: "Memory Get", Description: "Retrieve a specific memory entry by key", Category: "memory", Enabled: true,
			Requires: []string{"memory"},
		},
		{Name: "knowledge_graph_search", DisplayName: "Knowledge Graph Search", Description: "Search entities and traverse relationships in the knowledge graph", Category: "memory", Enabled: true,
			Settings: json.RawMessage(`{"extract_on_memory_write":false,"extraction_provider":"","extraction_model":"","min_confidence":0.75}`),
			Requires: []string{"knowledge_graph"},
		},

		// media
		{Name: "read_image", DisplayName: "Read Image", Description: "Analyze images using a vision-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"openrouter","model":"google/gemini-2.5-flash-image"}`),
			Requires: []string{"vision_provider"},
		},
		{Name: "read_document", DisplayName: "Read Document", Description: "Analyze documents (PDF, Word, Excel, PowerPoint, CSV, etc.) using a document-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"gemini","model":"gemini-2.5-flash"}`),
			Requires: []string{"document_provider"},
		},
		{Name: "create_image", DisplayName: "Create Image", Description: "Generate images from text prompts using an image generation provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"openrouter","model":"google/gemini-2.5-flash-image"}`),
			Requires: []string{"image_gen_provider"},
		},
		{Name: "read_audio", DisplayName: "Read Audio", Description: "Analyze audio files (speech, music, sounds) using an audio-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"gemini","model":"gemini-2.5-flash"}`),
			Requires: []string{"audio_provider"},
		},
		{Name: "read_video", DisplayName: "Read Video", Description: "Analyze video files using a video-capable LLM provider", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"gemini","model":"gemini-2.5-flash"}`),
			Requires: []string{"video_provider"},
		},
		{Name: "create_video", DisplayName: "Create Video", Description: "Generate videos from text descriptions using AI", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"gemini","model":"veo-3.0-generate-preview"}`),
			Requires: []string{"video_gen_provider"},
		},
		{Name: "create_audio", DisplayName: "Create Audio", Description: "Generate music or sound effects from text descriptions using AI", Category: "media", Enabled: true,
			Settings: json.RawMessage(`{"provider":"minimax","model":"music-2.5+"}`),
			Requires: []string{"audio_gen_provider"},
		},
		{Name: "tts", DisplayName: "Text to Speech", Description: "Convert text to speech audio", Category: "media", Enabled: true,
			Requires: []string{"tts_provider"},
			Metadata: json.RawMessage(`{"config_hint":"Config → TTS"}`),
		},

		// browser
		{Name: "browser", DisplayName: "Browser", Description: "Automate browser interactions (navigate, click, screenshot)", Category: "browser", Enabled: true,
			Requires: []string{"browser"},
			Metadata: json.RawMessage(`{"config_hint":"Config → Tools → Browser"}`),
		},

		// sessions
		{Name: "sessions_list", DisplayName: "List Sessions", Description: "List active chat sessions", Category: "sessions", Enabled: true},
		{Name: "session_status", DisplayName: "Session Status", Description: "Get status of a chat session", Category: "sessions", Enabled: true},
		{Name: "sessions_history", DisplayName: "Session History", Description: "Get message history of a chat session", Category: "sessions", Enabled: true},
		{Name: "sessions_send", DisplayName: "Send to Session", Description: "Send a message to a chat session", Category: "sessions", Enabled: true},

		// messaging
		{Name: "message", DisplayName: "Message", Description: "Send messages to connected channels (Telegram, Discord, etc.)", Category: "messaging", Enabled: true},

		// scheduling
		{Name: "cron", DisplayName: "Cron Scheduler", Description: "Schedule recurring tasks with cron expressions", Category: "scheduling", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Cron"}`),
		},

		// subagents & delegation (unified spawn tool)
		{Name: "spawn", DisplayName: "Spawn / Delegate", Description: "Spawn a subagent or delegate to another agent", Category: "subagents", Enabled: true,
			Metadata: json.RawMessage(`{"config_hint":"Config → Agents Defaults"}`),
		},

		// skills
		{Name: "skill_search", DisplayName: "Skill Search", Description: "Search available skills by keyword or description", Category: "skills", Enabled: true},
		{Name: "use_skill", DisplayName: "Use Skill", Description: "Activate a skill (marker for tracing and observability)", Category: "skills", Enabled: true},

		// delegation
		{Name: "delegate_search", DisplayName: "Delegate Search", Description: "Search for agents to delegate tasks to", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},
		{Name: "evaluate_loop", DisplayName: "Evaluate Loop", Description: "Run an evaluate-optimize loop with delegated agents", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},
		{Name: "handoff", DisplayName: "Handoff", Description: "Transfer conversation to another agent", Category: "delegation", Enabled: true,
			Requires: []string{"managed_mode", "agent_links"},
		},

		// teams
		{Name: "team_tasks", DisplayName: "Team Tasks", Description: "Manage tasks within a team of agents", Category: "teams", Enabled: true,
			Requires: []string{"managed_mode", "teams"},
		},
		{Name: "team_message", DisplayName: "Team Message", Description: "Send messages between team agents", Category: "teams", Enabled: true,
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
