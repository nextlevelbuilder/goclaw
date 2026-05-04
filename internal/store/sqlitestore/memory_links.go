//go:build sqlite || sqliteonly

// SQLite mirror of the PG memory_links + frontmatter metadata logic.
// SQLite is dev/test only; production uses Postgres. We keep the
// schemas + behavior in lockstep so the same store interface works
// identically across both backends.
package sqlitestore

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

func (s *SQLiteMemoryStore) upsertMemoryMetadata(ctx context.Context, agentID, userID, path string, meta memory.Metadata) error {
	blob, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if userID == "" {
		_, err = s.db.ExecContext(ctx,
			`UPDATE memory_documents SET metadata = ?
			 WHERE agent_id = ? AND path = ? AND user_id IS NULL`,
			string(blob), agentID, path)
	} else {
		_, err = s.db.ExecContext(ctx,
			`UPDATE memory_documents SET metadata = ?
			 WHERE agent_id = ? AND path = ? AND user_id = ?`,
			string(blob), agentID, path, userID)
	}
	return err
}

func (s *SQLiteMemoryStore) rewriteMemoryLinks(ctx context.Context, agentID, userID, path, body string) error {
	links := memory.ExtractLinks(body)

	uid := userID
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM memory_links WHERE agent_id = ? AND from_path = ? AND user_id = ?`,
		agentID, path, uid)
	if err != nil {
		return fmt.Errorf("delete prior links: %w", err)
	}

	if len(links) == 0 {
		return nil
	}

	docs, err := s.ListDocuments(ctx, agentID, userID)
	if err != nil {
		return fmt.Errorf("list docs for resolution: %w", err)
	}
	candidates := make([]string, 0, len(docs))
	for _, d := range docs {
		if strings.HasSuffix(d.Path, ".md") {
			candidates = append(candidates, d.Path)
		}
	}

	tid := tenantIDForInsert(ctx).String()

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

		var toPath any
		if resolved, ok := memory.ResolveWikilink(l.Target, candidates); ok {
			toPath = resolved
		} else {
			toPath = nil
		}
		var section any = nil
		if l.Section != "" {
			section = l.Section
		}
		var blockID any = nil
		if l.BlockID != "" {
			blockID = l.BlockID
		}
		var display any = nil
		if l.Display != "" {
			display = l.Display
		}

		_, err := s.db.ExecContext(ctx,
			`INSERT INTO memory_links
				(id, tenant_id, agent_id, user_id, from_path, to_path, to_basename, link_type, section, block_id, display)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(agent_id, user_id, from_path, to_basename, link_type, COALESCE(block_id, ''))
			 DO UPDATE SET to_path = excluded.to_path, section = excluded.section,
			               display = excluded.display`,
			uuid.Must(uuid.NewV7()).String(), tid, agentID, uid,
			path, toPath, basename, string(l.Kind), section, blockID, display,
		)
		if err != nil {
			return fmt.Errorf("insert memory_link: %w", err)
		}
	}
	return nil
}

func (s *SQLiteMemoryStore) GetBacklinks(ctx context.Context, agentID, userID, targetPath string) ([]store.BacklinkInfo, error) {
	basename := linkBasename(targetPath)
	rows, err := s.db.QueryContext(ctx,
		`SELECT from_path, link_type, COALESCE(section, ''), COALESCE(block_id, ''),
		        COALESCE(display, ''), user_id
		 FROM memory_links
		 WHERE agent_id = ?
		   AND (user_id = '' OR user_id = ?)
		   AND (to_path = ? OR (to_path IS NULL AND to_basename = ?))`,
		agentID, userID, targetPath, basename)
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

func linkBasename(target string) string {
	base := filepath.Base(target)
	return strings.TrimSuffix(base, ".md")
}
