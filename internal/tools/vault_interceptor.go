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

// inferScopeFromContext returns scope, team_id, and whether agent_id should be set.
// TeamID present → scope="team", teamID=&rc.TeamID, agentOwned=false.
// Absent → "personal", nil, agentOwned=true.
func inferScopeFromContext(ctx context.Context) (scope string, teamID *string, agentOwned bool) {
	rc := store.RunContextFromCtx(ctx)
	if rc != nil && rc.TeamID != "" {
		return "team", &rc.TeamID, false
	}
	return "personal", nil, true
}

// inferChatIDFromContext returns the chat_id to stamp on a vault doc.
// Non-nil only when team uses isolated workspace scope AND WorkspaceChatID is set.
// Shared/personal scope → nil (team-wide, matches any chat in search).
func inferChatIDFromContext(ctx context.Context) *string {
	rc := store.RunContextFromCtx(ctx)
	if rc == nil || rc.TeamID == "" || !rc.TeamIsolated {
		return nil
	}
	chatID := WorkspaceChatIDFromCtx(ctx)
	if chatID == "" {
		return nil
	}
	return &chatID
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
	title := vault.InferTitle(relPath)
	docType := vault.InferDocType(relPath)
	scope, teamID, agentOwned := inferScopeFromContext(ctx)

	// Team-scoped files belong to the team, not the creating agent.
	var agentIDPtr *string
	eventAgentID := ""
	if agentOwned {
		agentIDPtr = &agentID
		eventAgentID = agentID
	}

	doc := &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentIDPtr,
		TeamID:      teamID,
		ChatID:      inferChatIDFromContext(ctx),
		Scope:       scope,
		Path:        relPath,
		Title:       title,
		DocType:     docType,
		ContentHash: hash,
	}
	// Phase 05: tag with delegation_id when write happens inside a delegated
	// task so Phase 2.6 auto-linking can sibling-link the docs later.
	if delegID := DelegationIDFromCtx(ctx); delegID != "" {
		if doc.Metadata == nil {
			doc.Metadata = make(map[string]any)
		}
		doc.Metadata["delegation_id"] = delegID
		doc.Metadata["created_in"] = "delegation"
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
			SourceID:  doc.ID + ":" + hash,
			TenantID:  tenantID,
			AgentID:   eventAgentID,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       doc.ID,
				TenantID:    tenantID,
				AgentID:     eventAgentID,
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

	title := vault.InferTitle(relPath)
	scope, teamID, agentOwned := inferScopeFromContext(ctx)

	var agentIDPtr *string
	eventAgentID := ""
	if agentOwned {
		agentIDPtr = &agentID
		eventAgentID = agentID
	}

	// Build metadata carefully: mime_type always set, plus optional
	// delegation_id / created_in when running inside a delegation.
	// Using explicit assignment preserves any future caller-supplied keys
	// added via a metadata-capable variant — red-team concern #18.
	meta := map[string]any{"mime_type": mimeType}
	if delegID := DelegationIDFromCtx(ctx); delegID != "" {
		meta["delegation_id"] = delegID
		meta["created_in"] = "delegation"
	}
	doc := &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentIDPtr,
		TeamID:      teamID,
		ChatID:      inferChatIDFromContext(ctx),
		Scope:       scope,
		Path:        relPath,
		Title:       title,
		DocType:     "media",
		ContentHash: hash,
		Summary:     summary,
		Metadata:    meta,
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
			AgentID:   eventAgentID,
			Timestamp: time.Now(),
			Payload: eventbus.VaultDocUpsertedPayload{
				DocID:       doc.ID,
				TenantID:    tenantID,
				AgentID:     eventAgentID,
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

	// Try agent-scoped first, then tenant-wide (team/shared docs have no agent_id).
	doc, err := v.vaultStore.GetDocument(ctx, tenantID, agentID, relPath)
	if err != nil {
		doc, err = v.vaultStore.GetDocument(ctx, tenantID, "", relPath)
	}
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

