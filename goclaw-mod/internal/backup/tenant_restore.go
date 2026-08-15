package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
)

// TenantRestoreOptions configures a tenant-scoped restore run.
type TenantRestoreOptions struct {
	DB            *sql.DB
	ArchivePath   string
	TenantID      uuid.UUID // target tenant; zero = create new (mode "new")
	TenantSlug    string    // target tenant slug; required for mode "new"
	DataDir       string
	WorkspacePath string
	// Mode selects restore strategy:
	//   - "upsert"  (default): INSERT ... ON CONFLICT DO NOTHING. Non-destructive.
	//                          Tenant metadata (name/status/settings) is NOT updated
	//                          if the tenants row already exists.
	//   - "replace": DELETE all tenant-scoped data except the tenants row (FK-safe
	//                with respect to excluded diagnostic tables), then INSERT from
	//                archive. Metadata on the tenants row is preserved in place.
	//   - "new":     Create a new tenant from archive metadata (name/status/settings)
	//                with the provided TenantSlug. All tenant-scoped rows are cloned
	//                with tenant_id remapped to the new tenant. This includes
	//                tenant_users, api_keys, llm_providers, config_secrets, etc. —
	//                effectively a tenant clone under a new slug.
	Mode       string
	Force      bool
	DryRun     bool
	ProgressFn func(phase, detail string)
}

// TenantRestoreResult describes the outcome of a tenant restore.
type TenantRestoreResult struct {
	TenantID       string         `json:"tenant_id"`
	TablesRestored map[string]int `json:"tables_restored"`
	FilesExtracted int            `json:"files_extracted"`
	Warnings       []string       `json:"warnings,omitempty"`
}

// TenantRestore reads a tenant archive and restores DB rows + filesystem.
// Modes:
//   - "upsert"  (default): INSERT … ON CONFLICT DO NOTHING — non-destructive
//   - "replace": DELETE existing tenant data (reverse tier order), then INSERT
//   - "new":     Create a new tenant record, remap tenant_id in all rows before INSERT
func TenantRestore(ctx context.Context, opts TenantRestoreOptions) (*TenantRestoreResult, error) {
	progress := func(phase, detail string) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(phase, detail)
		}
	}

	mode := opts.Mode
	if mode == "" {
		mode = "upsert"
	}

	progress("init", "opening archive")

	tableData, wsEntries, dataEntries, manifest, err := readTenantArchive(opts.ArchivePath)
	if err != nil {
		return nil, err
	}

	result := &TenantRestoreResult{
		TenantID:       opts.TenantID.String(),
		TablesRestored: make(map[string]int),
	}

	// Schema version check.
	currentSchema := int(upgrade.RequiredSchemaVersion)
	if manifest.SchemaVersion > currentSchema {
		return nil, fmt.Errorf("backup schema version %d is newer than current %d; upgrade GoClaw first",
			manifest.SchemaVersion, currentSchema)
	}
	if manifest.SchemaVersion < currentSchema {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("backup schema version %d is older than current %d; run 'goclaw migrate up' after restore",
				manifest.SchemaVersion, currentSchema))
	}

	tables := TenantTables()
	targetTenantID := opts.TenantID
	var sourceTenant *tenantRestoreRow

	if mode == "new" && strings.TrimSpace(opts.TenantSlug) == "" {
		return nil, fmt.Errorf("target tenant slug is required for new restore")
	}

	if mode == "new" {
		tenantData, ok := tableData["tenants"]
		if !ok || len(tenantData) == 0 {
			return nil, fmt.Errorf("archive missing tenants table")
		}

		tenantRow, err := loadTenantRestoreRow(tenantData)
		if err != nil {
			return nil, fmt.Errorf("load tenants row: %w", err)
		}
		sourceTenant = tenantRow
	}

	if mode == "new" && opts.DryRun {
		if err := ensureTenantSlugAvailable(ctx, opts.TenantSlug, func(ctx context.Context, slug string) (uuid.UUID, error) {
			return lookupTenantSlugID(ctx, opts.DB, slug)
		}); err != nil {
			return nil, fmt.Errorf("validate target tenant slug: %w", err)
		}
	}

	if opts.DryRun {
		progress("dry-run", fmt.Sprintf("manifest ok: schema=%d, tables=%d", manifest.SchemaVersion, len(tableData)))
		return result, nil
	}

	switch mode {
	case "new":
		newID, err := createNewTenant(ctx, opts.DB, sourceTenant, opts.TenantSlug)
		if err != nil {
			return nil, fmt.Errorf("create new tenant: %w", err)
		}
		targetTenantID = newID
		result.TenantID = newID.String()
		result.TablesRestored["tenants"] = 1
		progress("database", fmt.Sprintf("created new tenant %s", newID))

	case "replace":
		if err := deleteTenantData(ctx, opts.DB, opts.TenantID, tables); err != nil {
			return nil, fmt.Errorf("delete existing tenant data: %w", err)
		}
		progress("database", "existing tenant data deleted")
	}

	// Restore DB tables in forward tier order.
	for _, table := range tables {
		if !shouldRestoreTable(mode, table) {
			continue
		}
		data, ok := tableData[table.Name]
		if !ok || len(data) == 0 {
			continue
		}
		progress("database", fmt.Sprintf("restoring %s", table.Name))

		importData := data
		if mode == "new" && table.HasTenantID {
			rewritten, err := rewriteTenantIDInJSONL(data, targetTenantID)
			if err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("rewrite tenant_id in %s: %v", table.Name, err))
			} else {
				importData = rewritten
			}
		}

		count, err := ImportTableRows(ctx, opts.DB, table.Name, bytes.NewReader(importData))
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("import %s: %v", table.Name, err))
			continue
		}
		result.TablesRestored[table.Name] = count
		progress("database", fmt.Sprintf("%s: %d rows", table.Name, count))
	}

	// Restore filesystem entries.
	if opts.WorkspacePath != "" && len(wsEntries) > 0 {
		progress("filesystem", "extracting workspace")
		n, _, err := extractEntries(wsEntries, opts.WorkspacePath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("extract workspace: %v", err))
		} else {
			result.FilesExtracted += n
			progress("filesystem", fmt.Sprintf("workspace done (%d files)", n))
		}
	}
	if opts.DataDir != "" && len(dataEntries) > 0 {
		progress("filesystem", "extracting data dir")
		n, _, err := extractEntries(dataEntries, opts.DataDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("extract data dir: %v", err))
		} else {
			result.FilesExtracted += n
			progress("filesystem", fmt.Sprintf("data dir done (%d files)", n))
		}
	}

	progress("done", fmt.Sprintf("tenant restore complete: tables=%d, files=%d",
		len(result.TablesRestored), result.FilesExtracted))
	return result, nil
}

// readTenantArchive opens a tenant backup archive and separates its contents.
func readTenantArchive(archivePath string) (
	tableData map[string][]byte,
	wsEntries map[string][]byte,
	dataEntries map[string][]byte,
	manifest *TenantBackupManifest,
	err error,
) {
	tableData = map[string][]byte{}
	wsEntries = map[string][]byte{}
	dataEntries = map[string][]byte{}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("decompress archive: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var manifestData []byte

	for {
		hdr, nextErr := tr.Next()
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("read tar: %w", nextErr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, readErr := io.ReadAll(tr)
		if readErr != nil {
			return nil, nil, nil, nil, fmt.Errorf("read entry %s: %w", hdr.Name, readErr)
		}
		switch {
		case hdr.Name == "manifest.json":
			manifestData = data
		case strings.HasPrefix(hdr.Name, "tables/") && strings.HasSuffix(hdr.Name, ".jsonl"):
			name := strings.TrimSuffix(strings.TrimPrefix(hdr.Name, "tables/"), ".jsonl")
			tableData[name] = data
		case strings.HasPrefix(hdr.Name, "workspace/"):
			wsEntries[strings.TrimPrefix(hdr.Name, "workspace/")] = data
		case strings.HasPrefix(hdr.Name, "data/"):
			dataEntries[strings.TrimPrefix(hdr.Name, "data/")] = data
		}
	}

	if manifestData == nil {
		return nil, nil, nil, nil, fmt.Errorf("manifest.json not found in archive")
	}
	var m TenantBackupManifest
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Format != "goclaw-tenant-backup" {
		return nil, nil, nil, nil, fmt.Errorf("unsupported archive format: %q", m.Format)
	}
	return tableData, wsEntries, dataEntries, &m, nil
}
