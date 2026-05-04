// PG implementation of Obsidian frontmatter metadata + wikilinks /
// backlinks index. The public IndexDocument call site (memory_docs.go)
// invokes upsertMemoryMetadata + rewriteMemoryLinks for .md files.
//
// Resolution strategy: at link extraction we attempt to resolve each
// wikilink to an actual on-vault path using ResolveWikilink against
// the current set of memory_documents.path entries for the same agent
// + user scope. Unresolved targets persist with to_path = NULL +
// to_basename = the raw target — the next time the missing target
// gets indexed, GetBacklinks falls back to a basename match so it
// still reports the inbound reference.
package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/memory"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// upsertMemoryMetadata writes the parsed frontmatter to
// memory_documents.metadata as JSONB. No-ops when meta has no content.
func (s *PGMemoryStore) upsertMemoryMetadata(ctx context.Context, agentID uuid.UUID, userID, path string, meta memory.Metadata) error {
	blob, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if userID == "" {
		_, err = s.db.ExecContext(ctx,
			`UPDATE memory_documents SET metadata = $1::jsonb
			 WHERE agent_id = $2 AND path = $3 AND user_id IS NULL`,
			string(blob), agentID, path)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE memory_documents SET metadata = $1::jsonb
			 WHERE agent_id = $2 AND path = $3 AND user_id = $4`,
			string(blob), agentID, path, userID)
	}
	return err
}

// rewriteMemoryLinks deletes all existing memory_links rows whose
// from_path matches `path` for the agent+user scope, then inserts a
// fresh row per extracted wikilink. Resolution against the current
// vault uses ListDocuments (paths only) so we don't need to load
// every doc body.
func (s *PGMemoryStore) rewriteMemoryLinks(ctx context.Context, agentID uuid.UUID, userID, path, body string) error {
	links := memory.ExtractLinks(body)

	// Delete first so a doc with zero links also clears its prior rows.
	if userID == "" {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM memory_links WHERE agent_id = $1 AND from_path = $2 AND user_id = ''`,
			agentID, path)
		if err != nil {
			return fmt.Errorf("delete prior links: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM memory_links WHERE agent_id = $1 AND from_path = $2 AND user_id = $3`,
			agentID, path, userID)
		if err != nil {
			return fmt.Errorf("delete prior links: %w", err)
		}
	}

	if len(links) == 0 {
		return nil
	}

	// Build resolution candidate set once: every doc path the same
	// agent+user can see (their own + global).
	docs, err := s.ListDocuments(ctx, agentID.String(), userID)
	if err != nil {
		return fmt.Errorf("list docs for resolution: %w", err)
	}
	candidates := make([]string, 0, len(docs))
	for _, d := range docs {
		if strings.HasSuffix(d.Path, ".md") {
			candidates = append(candidates, d.Path)
		}
	}

	tid := tenantIDForInsert(ctx)

	// Dedup by composite key matching the unique index on the table.
	type linkKey struct {
		basename, linkType, blockID string
	}
	seen := map[linkKey]bool{}
	for _, l := range links {
		basename := linkBasename(l.Target)
		key := linkKey{basename: basename, linkType: string(l.Kind), blockID: l.BlockID}
		if seen[key] {
			continue
		}
		seen[key] = true

		var toPath *string
		if resolved, ok := memory.ResolveWikilink(l.Target, candidates); ok {
			toPath = &resolved
		}
		var section *string
		if l.Section != "" {
			section = &l.Section
		}
		var blockID *string
		if l.BlockID != "" {
			blockID = &l.BlockID
		}
		var display *string
		if l.Display != "" {
			display = &l.Display
		}

		_, err := s.db.ExecContext(ctx,
			`INSERT INTO memory_links
				(id, tenant_id, agent_id, user_id, from_path, to_path, to_basename, link_type, section, block_id, display)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 ON CONFLICT (agent_id, COALESCE(user_id, ''), from_path, to_basename, link_type, COALESCE(block_id, ''))
			 DO UPDATE SET to_path = EXCLUDED.to_path, section = EXCLUDED.section,
			               display = EXCLUDED.display, created_at = NOW()`,
			uuid.Must(uuid.NewV7()), tid, agentID, userID,
			path, toPath, basename, string(l.Kind), section, blockID, display,
		)
		if err != nil {
			return fmt.Errorf("insert memory_link: %w", err)
		}
	}
	return nil
}

// GetBacklinks returns every link whose resolved to_path equals the
// target, OR whose unresolved to_basename matches the target's basename.
// The latter clause covers links that pointed at a doc not yet in the
// vault when the source was indexed — once the doc lands and a sweep
// runs, those rows get to_path filled in, but we want backlinks to
// surface immediately for the consumer.
func (s *PGMemoryStore) GetBacklinks(ctx context.Context, agentID, userID, targetPath string) ([]store.BacklinkInfo, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("memory backlinks: %w", err)
	}
	basename := linkBasename(targetPath)

	rows, err := s.db.QueryContext(ctx,
		`SELECT from_path, link_type, COALESCE(section, ''), COALESCE(block_id, ''),
		        COALESCE(display, ''), user_id
		 FROM memory_links
		 WHERE agent_id = $1
		   AND (user_id = '' OR user_id = $2)
		   AND (to_path = $3 OR (to_path IS NULL AND to_basename = $4))`,
		aid, userID, targetPath, basename)
	if err != nil {
		return nil, fmt.Errorf("query backlinks: %w", err)
	}
	defer rows.Close()

	var out []store.BacklinkInfo
	for rows.Next() {
		var b store.BacklinkInfo
		if err := rows.Scan(&b.FromPath, &b.LinkType, &b.Section, &b.BlockID, &b.Display, &b.UserID); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// linkBasename returns the comparison basename for wikilink dedup +
// resolution: the path's filename without the .md extension. For raw
// link targets like "Folder/Note", it strips the folder path; for
// targets like "Note", returns "Note".
func linkBasename(target string) string {
	base := filepath.Base(target)
	return strings.TrimSuffix(base, ".md")
}
