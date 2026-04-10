package pg

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// skillInfoRow is an sqlx scan struct for skills SELECT queries that return SkillInfo-compatible columns.
// Handles: tags (pq.Array), deps JSONB → MissingDeps, frontmatter JSONB → Author, computed Path/BaseDir.
// Used by ListSkills (includes frontmatter) and scanSkillInfoList (no frontmatter).
type skillInfoRow struct {
	ID         uuid.UUID      `db:"id"`
	Name       string         `db:"name"`
	Slug       string         `db:"slug"`
	Desc       *string        `db:"description"`
	Visibility string         `db:"visibility"`
	Tags       pq.StringArray `db:"tags"`
	Version    int            `db:"version"`
	IsSystem   bool           `db:"is_system"`
	Status     string         `db:"status"`
	Enabled    bool           `db:"enabled"`
	DepsRaw    []byte         `db:"deps"`
	FilePath   *string        `db:"file_path"`
}

// skillInfoRowWithFrontmatter extends skillInfoRow with the frontmatter column.
type skillInfoRowWithFrontmatter struct {
	skillInfoRow
	FmRaw []byte `db:"frontmatter"`
}

// toSkillInfo converts a skillInfoRow to store.SkillInfo, resolving computed fields from baseDir.
func (r *skillInfoRow) toSkillInfo(baseDir string) store.SkillInfo {
	info := buildSkillInfo(r.ID.String(), r.Name, r.Slug, r.Desc, r.Version, baseDir, r.FilePath)
	info.Visibility = r.Visibility
	info.Tags = []string(r.Tags)
	info.IsSystem = r.IsSystem
	info.Status = r.Status
	info.Enabled = r.Enabled
	info.MissingDeps = parseDepsColumn(r.DepsRaw)
	return info
}

// toSkillInfoWithFrontmatter converts a skillInfoRowWithFrontmatter to store.SkillInfo including Author.
func (r *skillInfoRowWithFrontmatter) toSkillInfo(baseDir string) store.SkillInfo {
	info := r.skillInfoRow.toSkillInfo(baseDir)
	info.Author = parseFrontmatterAuthor(r.FmRaw)
	return info
}

// skillBackfillRow is an sqlx scan struct for embedding backfill queries.
type skillBackfillRow struct {
	ID   uuid.UUID `db:"id"`
	Name string    `db:"name"`
	Desc string    `db:"description"`
}

// skillEmbeddingSearchRow is an sqlx scan struct for SearchByEmbedding queries.
// Path is computed post-scan from FilePath + baseDir, not a DB column.
type skillEmbeddingSearchRow struct {
	Name     string  `db:"name"`
	Slug     string  `db:"slug"`
	Desc     string  `db:"description"`
	Version  int     `db:"version"`
	FilePath *string `db:"file_path"`
	Score    float64 `db:"score"`
}

// customSkillExportRow is an sqlx scan struct for ExportCustomSkills query.
// Tags uses pq.StringArray to handle PostgreSQL text[]; ID is uuid.UUID for conversion.
type customSkillExportRow struct {
	ID          uuid.UUID      `db:"id"`
	Name        string         `db:"name"`
	Slug        string         `db:"slug"`
	Description *string        `db:"description"`
	Visibility  string         `db:"visibility"`
	Version     int            `db:"version"`
	FmRaw       []byte         `db:"frontmatter"`
	Tags        pq.StringArray `db:"tags"`
	DepsRaw     []byte         `db:"deps"`
	FilePath    *string        `db:"file_path"`
}

// toCustomSkillExport converts a customSkillExportRow to CustomSkillExport.
func (r *customSkillExportRow) toCustomSkillExport() CustomSkillExport {
	sk := CustomSkillExport{
		ID:          r.ID.String(),
		Name:        r.Name,
		Slug:        r.Slug,
		Description: r.Description,
		Visibility:  r.Visibility,
		Version:     r.Version,
		Tags:        []string(r.Tags),
	}
	if len(r.FmRaw) > 0 {
		sk.Frontmatter = json.RawMessage(r.FmRaw)
	}
	if len(r.DepsRaw) > 0 {
		sk.Deps = json.RawMessage(r.DepsRaw)
	}
	if r.FilePath != nil {
		sk.FilePath = *r.FilePath
	}
	return sk
}
