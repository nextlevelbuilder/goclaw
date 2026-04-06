package tools

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/vault"
)

// VaultInterceptor registers vault documents on file write/read.
type VaultInterceptor struct {
	vaultStore store.VaultStore
	workspace  string
}

// NewVaultInterceptor creates a new vault interceptor.
func NewVaultInterceptor(vs store.VaultStore, workspace string) *VaultInterceptor {
	return &VaultInterceptor{vaultStore: vs, workspace: workspace}
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
	// uuid.Nil.String() == "00000000-0000-0000-0000-000000000000"
	nilUUID := "00000000-0000-0000-0000-000000000000"
	if tenantID == nilUUID || agentID == nilUUID {
		return
	}

	hash := vault.ContentHash([]byte(content))
	title := inferVaultTitle(relPath)
	docType := inferVaultDocType(relPath)

	doc := &store.VaultDocument{
		TenantID:    tenantID,
		AgentID:     agentID,
		Scope:       "personal",
		Path:        relPath,
		Title:       title,
		DocType:     docType,
		ContentHash: hash,
	}
	if err := v.vaultStore.UpsertDocument(ctx, doc); err != nil {
		slog.Warn("vault.after_write", "path", relPath, "err", err)
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
func inferVaultDocType(relPath string) string {
	lower := strings.ToLower(relPath)
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
