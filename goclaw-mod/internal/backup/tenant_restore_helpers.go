package backup

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type tenantRestoreRow struct {
	Name     string
	Slug     string
	Status   string
	Settings string
}

func loadTenantRestoreRow(data []byte) (*tenantRestoreRow, error) {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 10<<20)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("unmarshal tenants row: %w", err)
		}

		return &tenantRestoreRow{
			Name:     backupValueString(row["name"]),
			Slug:     backupValueString(row["slug"]),
			Status:   backupValueString(row["status"]),
			Settings: backupValueString(row["settings"]),
		}, nil
	}

	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("tenants table is empty")
}

func backupValueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case json.RawMessage:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func lookupTenantSlugID(ctx context.Context, db *sql.DB, slug string) (uuid.UUID, error) {
	if db == nil {
		return uuid.Nil, fmt.Errorf("database is required")
	}

	var existingID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, strings.TrimSpace(slug)).Scan(&existingID)
	if err != nil {
		return uuid.Nil, err
	}
	return existingID, nil
}

func ensureTenantSlugAvailable(ctx context.Context, slug string, lookup func(context.Context, string) (uuid.UUID, error)) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("tenant slug required")
	}

	existingID, err := lookup(ctx, slug)
	switch {
	case err == nil:
		return fmt.Errorf("tenant slug %q already exists (id=%s); use upsert mode or choose a different slug", slug, existingID)
	case errors.Is(err, sql.ErrNoRows):
		return nil
	default:
		return fmt.Errorf("check tenant slug %q: %w", slug, err)
	}
}

// DeleteTenantDataForTest exposes deleteTenantData to integration tests in
// external packages. Production callers must use the internal restore path.
func DeleteTenantDataForTest(ctx context.Context, db *sql.DB, tenantID uuid.UUID, tables []TableDef) error {
	return deleteTenantData(ctx, db, tenantID, tables)
}

// deleteTenantData removes all tenant-scoped rows in reverse tier order (children before parents).
// Tables without direct tenant_id (e.g. vault_links) are skipped — handled by FK cascade.
// The tenants row itself is preserved: diagnostic tables (traces, activity_logs, usage_snapshots,
// spans, embedding_cache, pairing_requests, paired_devices, channel_pending_messages, cron_run_logs)
// reference tenants(id) without ON DELETE CASCADE, so removing the tenants row would fail under
// FK restrict. Replace mode keeps tenant metadata in place and only wipes backed-up child data.
func deleteTenantData(ctx context.Context, db *sql.DB, tenantID uuid.UUID, tables []TableDef) error {
	for i := len(tables) - 1; i >= 0; i-- {
		t := tables[i]
		if t.ParentJoin != "" {
			continue
		}
		if t.Name == "tenants" {
			continue
		}
		q, err := t.deleteQuery()
		if err != nil {
			return fmt.Errorf("delete %s: %w", t.Name, err)
		}
		if _, err := db.ExecContext(ctx, q, tenantID); err != nil {
			return fmt.Errorf("delete %s: %w", t.Name, err)
		}
	}
	return nil
}

// createNewTenant inserts a new tenant row from archived metadata and returns its UUID.
// targetSlug overrides the archived slug when non-empty.
func createNewTenant(ctx context.Context, db *sql.DB, source *tenantRestoreRow, targetSlug string) (uuid.UUID, error) {
	if source == nil {
		return uuid.Nil, fmt.Errorf("tenant source row required")
	}

	slug := strings.TrimSpace(targetSlug)
	if slug == "" {
		slug = strings.TrimSpace(source.Slug)
	}
	if slug == "" {
		return uuid.Nil, fmt.Errorf("tenant slug required")
	}

	if err := ensureTenantSlugAvailable(ctx, slug, func(ctx context.Context, slug string) (uuid.UUID, error) {
		return lookupTenantSlugID(ctx, db, slug)
	}); err != nil {
		return uuid.Nil, err
	}

	name := strings.TrimSpace(source.Name)
	if name == "" {
		name = slug
	}
	status := strings.TrimSpace(source.Status)
	if status == "" {
		status = "active"
	}
	settings := strings.TrimSpace(source.Settings)
	if settings == "" {
		settings = "{}"
	}

	newID := uuid.New()
	_, err := db.ExecContext(ctx,
		`INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW(), NOW())`,
		newID, name, slug, status, settings,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert tenant: %w", err)
	}
	return newID, nil
}

// shouldRestoreTable reports whether a table from the archive should be restored for the given mode.
//   - mode=new:     tenants row is created fresh from archive metadata via createNewTenant,
//                   so the archived tenants copy is skipped to avoid duplicate INSERT.
//   - mode=replace: tenants row is preserved in place (deleteTenantData skips it to respect
//                   FK from excluded diagnostic tables), so the archived copy is NOT re-applied
//                   either — existing tenant metadata (name/status/settings) remains untouched.
//   - mode=upsert:  all tables including tenants are restored via ON CONFLICT DO NOTHING
//                   (NO-OP if the row already exists).
func shouldRestoreTable(mode string, table TableDef) bool {
	if table.Name == "tenants" && (mode == "new" || mode == "replace") {
		return false
	}
	return true
}

// rewriteTenantIDInJSONL parses each JSONL line and replaces the tenant_id field.
// Returns the rewritten JSONL bytes. Uses bufio.Scanner with a 10 MB per-line buffer.
func rewriteTenantIDInJSONL(data []byte, newTenantID uuid.UUID) ([]byte, error) {
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 10<<20)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("unmarshal row: %w", err)
		}
		RemapTenantID(row, newTenantID)
		b, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("marshal row: %w", err)
		}
		out.Write(b)
		out.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
