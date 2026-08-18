// Package artifact defines the first-class persisted objects an agent can
// produce (plan, patch, code, report, review, research, architecture, ADR,
// test report, deployment plan) and the helpers that keep them consistent
// across the store boundary.
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// ArtifactType enumerates the kinds of artifacts an agent can produce.
const (
	TypePlan           = "plan"
	TypePatch          = "patch"
	TypeCode           = "code"
	TypeReport         = "report"
	TypeReview         = "review"
	TypeResearch       = "research"
	TypeArchitecture   = "architecture"
	TypeADR            = "adr"
	TypeTestReport     = "test-report"
	TypeDeploymentPlan = "deployment-plan"
)

// ArtifactStatus enumerates the lifecycle states of an artifact version.
const (
	StatusDraft      = "draft"
	StatusFinal      = "final"
	StatusSuperseded = "superseded"
	StatusArchived   = "archived"
)

// ValidType reports whether t is a known artifact type.
func ValidType(t string) bool {
	switch t {
	case TypePlan, TypePatch, TypeCode, TypeReport, TypeReview,
		TypeResearch, TypeArchitecture, TypeADR, TypeTestReport, TypeDeploymentPlan:
		return true
	}
	return false
}

// ValidStatus reports whether s is a known artifact status.
func ValidStatus(s string) bool {
	switch s {
	case StatusDraft, StatusFinal, StatusSuperseded, StatusArchived:
		return true
	}
	return false
}

// Checksum returns the SHA-256 hex digest of an artifact's content. Two
// artifacts with equal checksums carry identical content.
func Checksum(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Artifact is a first-class persisted object an agent produced. One row per
// version; ParentID links a version to its predecessor so a revision chain can
// be walked. Content is stored inline (no blob store) with a SHA-256 checksum
// for integrity verification.
type Artifact struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	RunID       string     `json:"run_id,omitempty"`
	Version     int        `json:"version"`
	AuthorAgent string     `json:"author_agent,omitempty"`
	Type        string     `json:"type"`
	Status      string     `json:"status,omitempty"`
	Checksum    string     `json:"checksum,omitempty"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	Title       string     `json:"title,omitempty"`
	Content     string     `json:"content,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}
