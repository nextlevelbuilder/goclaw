package backup

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// deleteTenantData removes all tenant rows in reverse tier order (children before parents).
// Tables without direct tenant_id (e.g. vault_links) are skipped — handled by FK cascade.
func deleteTenantData(ctx context.Context, db *sql.DB, tenantID uuid.UUID, tables []TableDef) error {
	for i := len(tables) - 1; i >= 0; i-- {
		t := tables[i]
		if !t.HasTenantID {
			continue
		}
		q := fmt.Sprintf("DELETE FROM %s WHERE tenant_id = $1", t.Name)
		if _, err := db.ExecContext(ctx, q, tenantID); err != nil {
			return fmt.Errorf("delete %s: %w", t.Name, err)
		}
	}
	return nil
}

// createNewTenant inserts a minimal tenant row and returns its new UUID.
// targetSlug overrides sourceSlug when non-empty.
func createNewTenant(ctx context.Context, db *sql.DB, sourceSlug, targetSlug string) (uuid.UUID, error) {
	slug := sourceSlug
	if targetSlug != "" {
		slug = targetSlug
	}

	// Fail explicitly if slug exists — prevent silent NOOP that would orphan imported data
	var existingID uuid.UUID
	err := db.QueryRowContext(ctx, `SELECT id FROM tenants WHERE slug = $1`, slug).Scan(&existingID)
	if err == nil {
		return uuid.Nil, fmt.Errorf("tenant slug %q already exists (id=%s); use upsert mode or choose a different slug", slug, existingID)
	}

	newID := uuid.New()
	_, err = db.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
		newID, slug, slug,
	)
	if err != nil {
		return uuid.Nil, fmt.Errorf("insert tenant: %w", err)
	}
	return newID, nil
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
