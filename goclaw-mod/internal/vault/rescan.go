package vault

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// RescanParams holds input for tenant-wide workspace rescan.
// AgentMap and TeamSet are pre-loaded by the caller to avoid per-file DB lookups.
type RescanParams struct {
	TenantID  string
	Workspace string            // absolute path to tenant's workspace root
	AgentMap  map[string]string // agent_key → agent UUID
	TeamSet   map[string]bool   // team UUID → exists (for validation)
}

// RescanResult holds the outcome of a workspace rescan.
type RescanResult struct {
	Scanned    int  `json:"scanned"`
	New        int  `json:"new"`
	Updated    int  `json:"updated"`
	Unchanged  int  `json:"unchanged"`
	Reenqueued int  `json:"reenqueued"` // docs re-enqueued for failed enrichment retry
	Skipped    int  `json:"skipped"`
	Errors     int  `json:"errors"`
	Truncated  bool `json:"truncated"`

	// PendingEvents holds enrichment events collected during scan.
	// Caller must publish these AFTER calling progress.Start(total)
	// to avoid race between event workers and progress tracking.
	PendingEvents []eventbus.DomainEvent `json:"-"`
}

// RescanWorkspace walks the tenant workspace and registers missing or changed
// files in vault_documents. Ownership (agent/team/scope) is inferred from path.
// Publishes EventVaultDocUpserted for each new or updated file so the
// enrichment worker can process them asynchronously.
func RescanWorkspace(ctx context.Context, params RescanParams, vs store.VaultStore, bus eventbus.DomainEventBus) (*RescanResult, error) {
	entries, walkStats, err := SafeWalkWorkspace(ctx, params.Workspace, DefaultWalkOptions())
	if err != nil {
		return nil, err
	}

	result := &RescanResult{
		Scanned:   walkStats.Eligible,
		Skipped:   walkStats.SkippedExcluded + walkStats.SkippedSymlinks + walkStats.SkippedTooLarge,
		Truncated: walkStats.Truncated,
	}

	for _, entry := range entries {
		agentID, teamID, chatID, scope, strippedPath := inferOwnerFromPath(entry.RelPath, params.AgentMap, params.TeamSet)
		if scope == "" {
			// Unknown agent key or invalid team UUID — skip.
			result.Skipped++
			continue
		}

		hash, hashErr := ContentHashFile(entry.AbsPath)
		if hashErr != nil {
			result.Errors++
			continue
		}

		// Resolve the agent ID string for store lookup (empty string = no agent filter).
		agentIDStr := ""
		if agentID != nil {
			agentIDStr = *agentID
		}

		// Use full workspace-relative path for DB storage and enrichment.
		// This preserves the real filesystem path (e.g. "teams/{uuid}/system/file.md")
		// so enrichment worker can locate files via filepath.Join(workspace, path).
		relPath := entry.RelPath

		// Check if document already exists with same hash.
		// Fallback: also check old stripped path (pre-fix records stored without prefix).
		existing, _ := vs.GetDocument(ctx, params.TenantID, agentIDStr, relPath)
		if existing == nil && strippedPath != relPath {
			existing, _ = vs.GetDocument(ctx, params.TenantID, agentIDStr, strippedPath)
		}
		if existing != nil && existing.ContentHash == hash {
			result.Unchanged++
			continue
		}

		doc := &store.VaultDocument{
			TenantID:    params.TenantID,
			AgentID:     agentID,
			TeamID:      teamID,
			ChatID:      chatID,
			Scope:       scope,
			Path:        relPath,
			Title:       InferTitle(relPath),
			DocType:     InferDocType(relPath),
			ContentHash: hash,
		}

		if err := vs.UpsertDocument(ctx, doc); err != nil {
			slog.Warn("vault.rescan: upsert", "path", relPath, "err", err)
			result.Errors++
			continue
		}

		if existing != nil {
			result.Updated++
		} else {
			result.New++
		}

		// Collect enrichment events — published AFTER the loop so the caller
		// can call progress.Start(total) before workers receive events.
		// Skip auto-generated media files (goclaw_gen_*) — they create excessive
		// noise links and shouldn't be counted in progress tracking.
		if bus != nil && !shouldSkipEnrichment(filepath.Base(relPath)) {
			result.PendingEvents = append(result.PendingEvents, eventbus.DomainEvent{
				ID:        uuid.Must(uuid.NewV7()).String(),
				Type:      eventbus.EventVaultDocUpserted,
				SourceID:  doc.ID + ":" + hash,
				TenantID:  params.TenantID,
				AgentID:   agentIDStr,
				Timestamp: time.Now(),
				Payload: eventbus.VaultDocUpsertedPayload{
					DocID:       doc.ID,
					TenantID:    params.TenantID,
					AgentID:     agentIDStr,
					Path:        relPath,
					ContentHash: hash,
					Workspace:   params.Workspace,
				},
			})
		}
	}

	slog.Info("vault.rescan",
		"tenant", params.TenantID,
		"scanned", result.Scanned, "new", result.New,
		"updated", result.Updated, "unchanged", result.Unchanged,
		"skipped", result.Skipped, "errors", result.Errors,
		"truncated", result.Truncated)

	return result, nil
}

// inferOwnerFromPath parses a tenant-relative path to determine ownership.
// Returns: agentID (*string), teamID (*string), chatID (*string), scope, strippedPath.
//
// Path patterns (checked in order):
//
//	teams/{team_uuid}/{chat}/...     → teamID=uuid, chatID=chat, scope="team"
//	teams/{team_uuid}/file.md        → teamID=uuid, chatID=nil (team-wide), scope="team"
//	agents/{agent_key}/...           → agentID=lookup(key), scope="personal"  (legacy prefix)
//	{agent_key}/...                  → agentID=lookup(key), scope="personal"  (workspace layout)
//
// Chat segments starting with "." (e.g. ".goclaw") are config dirs, not real chats — chatID stays nil.
// The full relPath is preserved in strippedPath for DB storage so enrichment workers
// can locate files via filepath.Join(workspace, path).
// Returns scope="" to signal the file should be skipped (unknown agent or invalid team).
func inferOwnerFromPath(relPath string, agentMap map[string]string, teamSet map[string]bool) (agentID *string, teamID *string, chatID *string, scope string, strippedPath string) {
	// Team paths: teams/{uuid}/[chat/]...
	if strings.HasPrefix(relPath, "teams/") {
		rest := relPath[len("teams/"):]
		id, remainder, hasSlash := strings.Cut(rest, "/")
		if !hasSlash || id == "" || strings.Contains(remainder, "..") {
			return nil, nil, nil, "", relPath
		}
		if _, parseErr := uuid.Parse(id); parseErr != nil {
			return nil, nil, nil, "", relPath
		}
		if !teamSet[id] {
			return nil, nil, nil, "", relPath
		}
		// Extract chat segment (second path component after team uuid) if present
		// and not a config/hidden dir. Paths without a chat segment stay team-wide.
		if chatSeg, _, hasChat := strings.Cut(remainder, "/"); hasChat && chatSeg != "" && !strings.HasPrefix(chatSeg, ".") {
			cid := chatSeg
			return nil, &id, &cid, "team", relPath
		}
		return nil, &id, nil, "team", relPath
	}

	// Agent paths: agents/{key}/... (legacy prefix) or {key}/... (actual workspace layout)
	if strings.HasPrefix(relPath, "agents/") {
		rest := relPath[len("agents/"):]
		key, _, hasSlash := strings.Cut(rest, "/")
		if hasSlash && key != "" && !strings.Contains(relPath, "..") {
			if agentUUID, ok := agentMap[key]; ok {
				return &agentUUID, nil, nil, "personal", relPath
			}
		}
		return nil, nil, nil, "", relPath
	}

	// Root-level agent_key match: {agent_key}/...
	// Workspace resolver uses agent_key as folder name directly.
	firstSeg, _, hasSlash := strings.Cut(relPath, "/")
	if hasSlash && firstSeg != "" {
		if agentUUID, ok := agentMap[firstSeg]; ok {
			return &agentUUID, nil, nil, "personal", relPath
		}
	}

	// Everything else is shared (root-level files, unknown folders)
	return nil, nil, nil, "shared", relPath
}

// InferDocType guesses doc_type from path conventions.
// Exported so both rescan and vault interceptor share the same logic.
//
// Path-prefix rules (memory/, skills/, episodic/, SOUL/IDENTITY/AGENTS)
// take precedence over extension classification — e.g. `memory/foo.md`
// classifies as `memory` even though `.md` is a whitelisted note extension.
// Phase 01: extension fallback uses the shared `extensionDocType` whitelist
// so PDFs/office files resolve to `document` instead of the prior `note`.
func InferDocType(relPath string) string {
	lower := strings.ToLower(relPath)
	ext := strings.ToLower(filepath.Ext(relPath))

	// Path-based overrides first (memory/ beats .md → note).
	switch {
	case strings.HasPrefix(lower, "memory/"):
		return "memory"
	case strings.Contains(lower, "soul.md") || strings.Contains(lower, "identity.md") || strings.Contains(lower, "agents.md"):
		return "context"
	case strings.HasPrefix(lower, "skills/") || strings.HasSuffix(lower, "skill.md"):
		return "skill"
	case strings.HasPrefix(lower, "episodic/"):
		return "episodic"
	}

	// Extension whitelist fallback — single source of truth shared with safe_walk.
	if _, dt := isIncludedExtension(ext); dt != "" {
		return dt
	}
	return "note"
}

// InferTitle extracts a human-readable title from a file path.
// Exported so both rescan and vault interceptor share the same logic.
func InferTitle(relPath string) string {
	base := filepath.Base(relPath)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
