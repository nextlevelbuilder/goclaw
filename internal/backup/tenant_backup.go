package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
)

// TenantBackupOptions configures a tenant-scoped backup run.
type TenantBackupOptions struct {
	DB            *sql.DB
	TenantID      uuid.UUID
	TenantSlug    string
	DataDir       string
	WorkspacePath string
	OutputPath    string
	CreatedBy     string
	SchemaVersion int
	ProgressFn    func(phase, detail string)
}

// TenantBackupManifest describes the contents of a tenant backup archive.
type TenantBackupManifest struct {
	Version       int            `json:"version"`
	Format        string         `json:"format"` // "goclaw-tenant-backup"
	TenantID      string         `json:"tenant_id"`
	TenantSlug    string         `json:"tenant_slug"`
	SchemaVersion int            `json:"schema_version"`
	CreatedAt     string         `json:"created_at"`
	CreatedBy     string         `json:"created_by"`
	TableCounts   map[string]int `json:"table_counts"`
	Stats         BackupStats    `json:"stats"`
}

// TenantBackup creates a tenant-scoped backup archive at opts.OutputPath.
// Archive layout:
//
//	manifest.json
//	tables/{table}.jsonl   — one file per table, JSONL rows filtered by tenant scope
//	workspace/             — TenantWorkspace contents
//	data/                  — TenantDataDir contents
func TenantBackup(ctx context.Context, opts TenantBackupOptions) (*TenantBackupManifest, error) {
	progress := func(phase, detail string) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(phase, detail)
		}
	}

	// Cross-check registry against actual schema — warn about unregistered tables
	if warnings := ValidateTableRegistry(ctx, opts.DB); len(warnings) > 0 {
		for _, w := range warnings {
			progress("validate", w)
		}
	}

	outFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	manifest := &TenantBackupManifest{
		Version:       1,
		Format:        "goclaw-tenant-backup",
		TenantID:      opts.TenantID.String(),
		TenantSlug:    opts.TenantSlug,
		SchemaVersion: opts.SchemaVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:     opts.CreatedBy,
		TableCounts:   make(map[string]int),
	}

	// -- Export DB tables -------------------------------------------------------
	tables := TenantTables()
	progress("database", fmt.Sprintf("exporting %d tables", len(tables)))

	for _, table := range tables {
		progress("database", fmt.Sprintf("exporting %s", table.Name))

		// Buffer the table JSONL in a temp file to get byte count for tar header.
		tmp, err := os.CreateTemp("", "goclaw-tenant-table-*.jsonl")
		if err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("create temp for %s: %w", table.Name, err)
		}
		tmpPath := tmp.Name()

		count, exportErr := ExportTable(ctx, opts.DB, table, opts.TenantID, tmp)
		tmp.Close()

		if exportErr != nil {
			os.Remove(tmpPath)
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("export %s: %w", table.Name, exportErr)
		}

		manifest.TableCounts[table.Name] = count

		// Add JSONL to tar regardless of row count (empty files are valid).
		if err := addFileToTar(tw, tmpPath, "tables/"+table.Name+".jsonl"); err != nil {
			os.Remove(tmpPath)
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("archive %s: %w", table.Name, err)
		}
		os.Remove(tmpPath)

		progress("database", fmt.Sprintf("%s: %d rows", table.Name, count))
	}

	// -- Filesystem archive -----------------------------------------------------
	if opts.WorkspacePath != "" {
		progress("filesystem", "archiving workspace")
		wFiles, wBytes, err := ArchiveDirectory(tw, opts.WorkspacePath, "workspace", nil)
		if err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("archive workspace: %w", err)
		}
		manifest.Stats.FilesystemFiles += wFiles
		manifest.Stats.FilesystemBytes += wBytes
		progress("filesystem", fmt.Sprintf("workspace done (%d files)", wFiles))
	}

	if opts.DataDir != "" {
		progress("filesystem", "archiving data dir")
		dFiles, dBytes, err := ArchiveDirectory(tw, opts.DataDir, "data", nil)
		if err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("archive data dir: %w", err)
		}
		manifest.Stats.FilesystemFiles += dFiles
		manifest.Stats.FilesystemBytes += dBytes
		progress("filesystem", fmt.Sprintf("data dir done (%d files)", dFiles))
	}

	// -- Manifest (last, stats complete) ----------------------------------------
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		tw.Close()
		gw.Close()
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addBytesToTar(tw, "manifest.json", manifestJSON); err != nil {
		tw.Close()
		gw.Close()
		return nil, fmt.Errorf("write manifest.json: %w", err)
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}

	progress("done", opts.OutputPath)
	return manifest, nil
}

// addFileToTar reads a local file and appends it to the tar archive under tarName.
func addFileToTar(tw *tar.Writer, filePath, tarName string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	hdr := &tar.Header{
		Name:     tarName,
		Mode:     0644,
		Size:     info.Size(),
		ModTime:  info.ModTime(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}
