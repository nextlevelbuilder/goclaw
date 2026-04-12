//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// BatchGetTaskSiblingsByBasenames — SQLite mirror of the PG implementation.
// Uses dynamic `?,?,?` placeholders (no array type in sqlite) and the same
// PARTITION BY (source_base, task_id) cap semantics.
func (s *SQLiteTeamStore) BatchGetTaskSiblingsByBasenames(
	ctx context.Context,
	tenantID uuid.UUID,
	basenames []string,
	limit int,
) (map[string][]store.TaskSibling, error) {
	if len(basenames) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 9
	}

	// Lowercase + dedup defensively.
	seen := make(map[string]bool, len(basenames))
	clean := make([]string, 0, len(basenames))
	for _, b := range basenames {
		lb := strings.ToLower(b)
		if lb == "" || seen[lb] {
			continue
		}
		seen[lb] = true
		clean = append(clean, lb)
	}

	out := make(map[string][]store.TaskSibling, len(clean))
	const chunkSize = 500

	for start := 0; start < len(clean); start += chunkSize {
		end := start + chunkSize
		if end > len(clean) {
			end = len(clean)
		}
		chunk := clean[start:end]

		ph := make([]string, len(chunk))
		args := []any{tenantID.String()}
		for i, b := range chunk {
			ph[i] = "?"
			args = append(args, b)
		}
		args = append(args, limit)

		q := fmt.Sprintf(`
WITH target_tasks AS (
  SELECT DISTINCT tta.base_name AS source_base, tta.task_id
  FROM team_task_attachments tta
  WHERE tta.tenant_id = ? AND tta.base_name IN (%s)
),
ranked AS (
  SELECT
    tt.source_base,
    tta2.task_id,
    vd.id AS doc_id,
    tta2.base_name,
    tta2.created_at,
    ROW_NUMBER() OVER (
      PARTITION BY tt.source_base, tta2.task_id
      ORDER BY tta2.created_at DESC, vd.id DESC
    ) AS rn
  FROM target_tasks tt
  JOIN team_task_attachments tta2 ON tta2.task_id = tt.task_id
  JOIN vault_documents vd
    ON vd.tenant_id = tta2.tenant_id
   AND vd.team_id = tta2.team_id
   AND vd.path_basename = tta2.base_name
  WHERE tta2.base_name != tt.source_base
)
SELECT source_base, task_id, doc_id, base_name, created_at
FROM ranked
WHERE rn <= ?
ORDER BY source_base, task_id, created_at DESC
`, strings.Join(ph, ","))

		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return nil, fmt.Errorf("batch task siblings: %w", err)
		}
		for rows.Next() {
			var sourceBase string
			var taskIDStr, docIDStr string
			var sib store.TaskSibling
			ca := &sqliteTime{}
			if err := rows.Scan(&sourceBase, &taskIDStr, &docIDStr, &sib.BaseName, ca); err != nil {
				rows.Close()
				return nil, err
			}
			if tid, perr := uuid.Parse(taskIDStr); perr == nil {
				sib.TaskID = tid
			}
			if did, perr := uuid.Parse(docIDStr); perr == nil {
				sib.DocID = did
			}
			sib.AttachmentTime = ca.Time
			out[sourceBase] = append(out[sourceBase], sib)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
