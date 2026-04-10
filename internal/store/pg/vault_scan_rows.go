package pg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// vaultDocRow is an sqlx scan struct for vault_documents SELECT queries.
// IDs are scanned as uuid.UUID then converted to string for VaultDocument.
// Metadata is scanned as raw JSON then unmarshalled post-scan.
type vaultDocRow struct {
	ID          uuid.UUID  `db:"id"`
	TenantID    uuid.UUID  `db:"tenant_id"`
	AgentID     *uuid.UUID `db:"agent_id"`
	TeamID      *uuid.UUID `db:"team_id"`
	Scope       string     `db:"scope"`
	CustomScope *string    `db:"custom_scope"`
	Path        string     `db:"path"`
	Title       string     `db:"title"`
	DocType     string     `db:"doc_type"`
	ContentHash string     `db:"content_hash"`
	Summary     string     `db:"summary"`
	MetaJSON    []byte     `db:"metadata"`
	CreatedAt   time.Time  `db:"created_at"`
	UpdatedAt   time.Time  `db:"updated_at"`
}

// toVaultDocument converts a vaultDocRow to store.VaultDocument.
func (r *vaultDocRow) toVaultDocument() store.VaultDocument {
	doc := store.VaultDocument{
		ID:          r.ID.String(),
		TenantID:    r.TenantID.String(),
		Scope:       r.Scope,
		CustomScope: r.CustomScope,
		Path:        r.Path,
		Title:       r.Title,
		DocType:     r.DocType,
		ContentHash: r.ContentHash,
		Summary:     r.Summary,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
	if r.AgentID != nil {
		s := r.AgentID.String()
		doc.AgentID = &s
	}
	if r.TeamID != nil {
		s := r.TeamID.String()
		doc.TeamID = &s
	}
	if len(r.MetaJSON) > 0 {
		json.Unmarshal(r.MetaJSON, &doc.Metadata) //nolint:errcheck
	}
	return doc
}

// vaultSearchRow extends vaultDocRow with a computed score column for search queries.
type vaultSearchRow struct {
	vaultDocRow
	Score float64 `db:"score"`
}

// toVaultSearchResult converts a vaultSearchRow to store.VaultSearchResult.
func (r *vaultSearchRow) toVaultSearchResult(source string) store.VaultSearchResult {
	return store.VaultSearchResult{
		Document: r.vaultDocRow.toVaultDocument(),
		Score:    r.Score,
		Source:   source,
	}
}
