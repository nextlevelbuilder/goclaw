// memory_backlinks tool — given a memory document path, list every
// other doc that links into it via Obsidian wikilinks. Powered by the
// memory_links table populated during IndexDocument (see
// internal/store/pg/memory_links.go).
//
// Project-agnostic: works for any vault that uses Obsidian-style
// wikilinks, regardless of the vault's folder layout.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// MemoryBacklinksTool surfaces inbound wikilinks for a target document.
type MemoryBacklinksTool struct {
	memStore store.MemoryStore
}

func NewMemoryBacklinksTool() *MemoryBacklinksTool { return &MemoryBacklinksTool{} }

// SetMemoryStore enables the tool. Without it Execute returns a
// disabled marker so the agent surfaces "memory backlinks unavailable"
// rather than failing silently.
func (t *MemoryBacklinksTool) SetMemoryStore(ms store.MemoryStore) { t.memStore = ms }

func (t *MemoryBacklinksTool) Name() string { return "memory_backlinks" }

func (t *MemoryBacklinksTool) Description() string {
	return "List every memory document that links into the given path via Obsidian wikilinks ([[Note]] or [[Folder/Note]]). " +
		"Use after memory_search to discover related notes that aren't surface keyword/semantic matches but reference the target. " +
		"Path argument can be either a full vault-relative path (e.g. \"memory/wiki/projects/dojo.md\") or a basename (e.g. \"dojo\") — the tool resolves both. " +
		"Returns up to 50 inbound links sorted by source path. If response has disabled=true, the backlink index is unavailable for this agent."
}

func (t *MemoryBacklinksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Vault-relative path or basename of the target document (e.g. \"memory/wiki/projects/dojo.md\" or \"dojo\").",
			},
		},
		"required": []string{"path"},
	}
}

func (t *MemoryBacklinksTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		return ErrorResult("path parameter is required")
	}
	if t.memStore == nil {
		data, _ := json.Marshal(map[string]any{
			"disabled": true,
			"message":  "memory backlinks unavailable: memory store not configured",
		})
		return NewResult(string(data))
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID == uuid.Nil {
		return ErrorResult("agent_id missing from context — cannot scope backlinks lookup")
	}
	userID := store.MemoryUserID(ctx)

	links, err := t.memStore.GetBacklinks(ctx, agentID.String(), userID, path)
	if err != nil {
		return ErrorResult(fmt.Sprintf("backlinks query failed: %v", err))
	}
	const cap = 50
	if len(links) > cap {
		links = links[:cap]
	}
	out := map[string]any{
		"target": path,
		"count":  len(links),
		"links":  links,
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	return NewResult(string(data))
}
