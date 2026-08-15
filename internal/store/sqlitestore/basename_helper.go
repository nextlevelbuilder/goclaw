//go:build sqlite || sqliteonly

package sqlitestore

import (
	"path/filepath"
	"strings"
)

// ComputeAttachmentBaseName derives the canonical basename used for both
// team_task_attachments.base_name and vault_documents.path_basename on SQLite.
//
// PG GENERATES these columns via `lower(regexp_replace(path, '.+/', ''))`.
// modernc.org/sqlite (bundled) ships no regexp_replace, so SQLite callers
// must compute the value app-side at INSERT/UPDATE time. This helper is the
// single source of truth — call it from workspace_interceptor, team_tasks_create,
// vault UpsertDocument, and v15→v16 migration backfill.
//
// Pure string manipulation; safe to invoke from PG code paths too where it
// becomes a harmless no-op (PG's GENERATED column overrides anything the Go
// caller supplies).
func ComputeAttachmentBaseName(path string) string {
	if path == "" {
		return ""
	}
	return strings.ToLower(filepath.Base(path))
}
