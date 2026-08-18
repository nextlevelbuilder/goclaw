package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// ArtifactType enumerates the kinds of first-class, persisted objects an agent
// can produce (plan, patch, code, report, review, research, architecture, ADR,
// test report, deployment plan).
const (
	ArtifactTypePlan           = "plan"
	ArtifactTypePatch          = "patch"
	ArtifactTypeCode           = "code"
	ArtifactTypeReport         = "report"
	ArtifactTypeReview         = "review"
	ArtifactTypeResearch       = "research"
	ArtifactTypeArchitecture   = "architecture"
	ArtifactTypeADR            = "adr"
	ArtifactTypeTestReport     = "test-report"
	ArtifactTypeDeploymentPlan = "deployment-plan"
)

// ArtifactStatus enumerates the lifecycle states of an artifact version.
const (
	ArtifactStatusDraft      = "draft"
	ArtifactStatusFinal      = "final"
	ArtifactStatusSuperseded = "superseded"
	ArtifactStatusArchived   = "archived"
)

// ValidArtifactType reports whether t is a known artifact type.
func ValidArtifactType(t string) bool {
	switch t {
	case ArtifactTypePlan, ArtifactTypePatch, ArtifactTypeCode, ArtifactTypeReport,
		ArtifactTypeReview, ArtifactTypeResearch, ArtifactTypeArchitecture,
		ArtifactTypeADR, ArtifactTypeTestReport, ArtifactTypeDeploymentPlan:
		return true
	}
	return false
}

// ValidArtifactStatus reports whether s is a known artifact status.
func ValidArtifactStatus(s string) bool {
	switch s {
	case ArtifactStatusDraft, ArtifactStatusFinal, ArtifactStatusSuperseded, ArtifactStatusArchived:
		return true
	}
	return false
}

// ArtifactChecksum returns the SHA-256 hex digest of an artifact's content.
// The checksum is the integrity anchor for a persisted version — two artifacts
// with equal checksums carry identical content.
func ArtifactChecksum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Artifact is a first-class, persisted object an agent produced (plan, patch,
// code, report, review, research, architecture, ADR, test report, deployment
// plan). One row per version; parent_id links a version to its predecessor so
// a revision chain can be walked. Content is stored inline (no blob store)
// with a SHA-256 checksum for integrity verification.
type Artifact struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TenantID    uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	RunID       string     `json:"run_id,omitempty" db:"run_id"`
	Version     int        `json:"version" db:"version"`
	AuthorAgent string     `json:"author_agent,omitempty" db:"author_agent"`
	Type        string     `json:"type" db:"type"`
	Status      string     `json:"status,omitempty" db:"status"`
	Checksum    string     `json:"checksum,omitempty" db:"checksum"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty" db:"parent_id"`
	Title       string     `json:"title,omitempty" db:"title"`
	Content     string     `json:"content,omitempty" db:"content"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// ArtifactListOpts scopes an artifact list read. Reads are tenant-scoped via
// context; RunID/Type/Status narrow the result set.
type ArtifactListOpts struct {
	RunID  string
	Type   string
	Status string
	Limit  int
	Offset int
}

// ArtifactStore persists agent-produced artifacts with a version graph.
type ArtifactStore interface {
	// CreateArtifact inserts a new artifact version. Root artifacts (ParentID
	// nil) get version 1; children get parent.version + 1 computed inside a
	// transaction. The checksum is computed from Content when left empty.
	CreateArtifact(ctx context.Context, artifact *Artifact) error
	// GetArtifact returns one artifact by id, scoped to the context tenant.
	GetArtifact(ctx context.Context, id uuid.UUID) (*Artifact, error)
	// ListArtifacts returns artifact rows filtered by opts, scoped to the
	// context tenant. Newest first.
	ListArtifacts(ctx context.Context, opts ArtifactListOpts) ([]Artifact, error)
	// GetVersionChain returns the direct children of parentID within tenantID,
	// ordered by version ascending. A nil parentID returns root artifacts
	// (parent_id IS NULL). The tenant is explicit so a caller can walk a chain
	// without fabricating tenant context.
	GetVersionChain(ctx context.Context, tenantID uuid.UUID, parentID *uuid.UUID) ([]Artifact, error)
	// MarkArtifactStatus transitions one artifact's status, scoped to the
	// context tenant.
	MarkArtifactStatus(ctx context.Context, id uuid.UUID, status string) error
}
