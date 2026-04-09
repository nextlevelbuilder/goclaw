package tools

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// VaultInterceptor registers vault documents on file write/read.
type VaultInterceptor struct {
	vaultStore store.VaultStore
	workspace  string
	eventBus   eventbus.DomainEventBus // nil-safe: enrichment disabled if nil
}

// NewVaultInterceptor creates a new vault interceptor.
func NewVaultInterceptor(vs store.VaultStore, workspace string, bus eventbus.DomainEventBus) *VaultInterceptor {
	return &VaultInterceptor{vaultStore: vs, workspace: workspace, eventBus: bus}
}

// inferScopeFromContext returns scope and team_id based on RunContext.
// TeamID present → scope="team", teamID=&rc.TeamID. Absent → "personal", nil.
func inferScopeFromContext(ctx context.Context) (scope string, teamID *string) {
	rc := store.RunContextFromCtx(ctx)
	if rc != nil && rc.TeamID != "" {
		return "team", &rc.TeamID
	}
	return "personal", nil
}

// AfterWrite registers or updates a vault document after a file write.
// Non-blocking: errors logged but not propagated.
func (v *VaultInterceptor) AfterWrite(ctx context.Context, resolvedPath, content string) {
	if v.vaultStore == nil {
		return
	}

	relPath, err := filepath.Rel(v.workspace, resolvedPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return // outside workspace
	}
	relPath = filepath.ToSlash(relPath)

	tenantID := store.TenantIDFromContext(ctx).String()
	agentID := store.AgentIDFromContext(ctx).String()
	nilUUID := "00000000-0000-0000-0000-000000000000"
	if tenantID == nilUUID || agentID == nilUUID {
		return
	}

	hash := vault.ContentHash([]byte(content))
	title := inferVaultTitle(relPath)
	docType := inferVaultDocType(relPath)
	scope, teamID := inferScopeFromContext(ctx)

	doc := &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentID,
		TeamID:      teamID,
		Scope:       scope,
		Path:        relPath,
		Title:       title,
		DocType:     docType,
		ContentHash: hash,
	}
	if err := v.vaultStore.UpsertDocument(ctx, doc); err != nil {
		slog.Warn("vault.after_write", "path", relPath, "err", err)
		return
	}

	// Publish enrichment event (async summary + embedding + auto-linking).
	if v.eventBus != nil {
		v.eventBus.Publish(eventbus.DomainEvent{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Type:      eventbus.EventVaultDocUpserted,
			SourceID:  doc.ID + ":" + hash, // unique per content version, avoids bus-level dedup suppression
			TenantID:  tenantID,
			AgentID:   agentID,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       doc.ID,
				TenantID:    tenantID,
				AgentID:     agentID,
				Path:        relPath,
				ContentHash: hash,
				Workspace:   v.workspace,
			},
		})
	}
}

// AfterWriteMedia registers a binary media file in the vault.
// Hashes from disk file (not RAM) to avoid holding large binaries in memory.
// Non-blocking: errors logged but not propagated.
func (v *VaultInterceptor) AfterWriteMedia(ctx context.Context, resolvedPath, summary, mimeType string) {
	if v.vaultStore == nil {
		return
	}

	relPath, err := filepath.Rel(v.workspace, resolvedPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return
	}
	relPath = filepath.ToSlash(relPath)

	tenantID := store.TenantIDFromContext(ctx).String()
	agentID := store.AgentIDFromContext(ctx).String()
	nilUUID := "00000000-0000-0000-0000-000000000000"
	if tenantID == nilUUID || agentID == nilUUID {
		return
	}

	hash, err := vault.ContentHashFile(resolvedPath)
	if err != nil {
		slog.Warn("vault.media_hash", "path", relPath, "err", err)
		return
	}

	title := inferVaultTitle(relPath)
	scope, teamID := inferScopeFromContext(ctx)

	doc := &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentID,
		TeamID:      teamID,
		Scope:       scope,
		Path:        relPath,
		Title:       title,
		DocType:     "media",
		ContentHash: hash,
		Summary:     summary,
		Metadata:    map[string]any{"mime_type": mimeType},
	}
	if err := v.vaultStore.UpsertDocument(ctx, doc); err != nil {
		slog.Warn("vault.after_write_media", "path", relPath, "err", err)
		return
	}

	// Publish enrichment event (async embedding + auto-linking; may skip summarize if caption provided).
	if v.eventBus != nil {
		v.eventBus.Publish(eventbus.DomainEvent{
			ID:        uuid.Must(uuid.NewV7()).String(),
			Type:      eventbus.EventVaultDocUpserted,
			SourceID:  doc.ID + ":" + hash,
			TenantID:  tenantID,
			AgentID:   agentID,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       doc.ID,
				TenantID:    tenantID,
				AgentID:     agentID,
				Path:        relPath,
				ContentHash: hash,
				Workspace:   v.workspace,
			},
		})
	}
}

// BeforeRead performs lazy sync: checks if FS hash differs from DB hash and updates if needed.
func (v *VaultInterceptor) BeforeRead(ctx context.Context, resolvedPath string) {
	if v.vaultStore == nil {
		return
	}

	relPath, err := filepath.Rel(v.workspace, resolvedPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return
	}
	relPath = filepath.ToSlash(relPath)

	tenantID := store.TenantIDFromContext(ctx).String()
	agentID := store.AgentIDFromContext(ctx).String()
	nilUUID := "00000000-0000-0000-0000-000000000000"
	if tenantID == nilUUID || agentID == nilUUID {
		return
	}

	doc, err := v.vaultStore.GetDocument(ctx, tenantID, agentID, relPath)
	if err != nil {
		return // not registered yet — skip
	}

	fsHash, err := vault.ContentHashFile(resolvedPath)
	if err != nil {
		return
	}
	if fsHash != doc.ContentHash {
		if err := v.vaultStore.UpdateHash(ctx, tenantID, doc.ID, fsHash); err != nil {
			slog.Warn("vault.lazy_sync", "path", relPath, "err", err)
		}
	}
}

// inferVaultTitle extracts a human-readable title from a file path.
func inferVaultTitle(relPath string) string {
	base := filepath.Base(relPath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

// inferVaultDocType guesses doc_type from path conventions.
// Media extension check runs first — doc_type describes content format, not location.
func inferVaultDocType(relPath string) string {
	lower := strings.ToLower(relPath)
	ext := strings.ToLower(filepath.Ext(relPath))

	// Media types (images, video, audio)
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp",
		".mp4", ".webm", ".mov", ".avi", ".mkv",
		".mp3", ".wav", ".ogg", ".flac", ".aac", ".m4a":
		return "media"
	}

	// Path-based inference
	switch {
	case strings.HasPrefix(lower, "memory/"):
		return "memory"
	case strings.Contains(lower, "soul.md") || strings.Contains(lower, "identity.md") || strings.Contains(lower, "agents.md"):
		return "context"
	case strings.HasPrefix(lower, "skills/") || strings.HasSuffix(lower, "skill.md"):
		return "skill"
	case strings.HasPrefix(lower, "episodic/"):
		return "episodic"
	default:
		return "note"
	}
}
