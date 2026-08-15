package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/upgrade"
)

// RestoreOptions configures a system restore run.
type RestoreOptions struct {
	ArchivePath   string
	DSN           string
	DataDir       string
	WorkspacePath string
	DryRun        bool
	SkipDB        bool
	SkipFiles     bool
	Force         bool // skip confirmation (for CLI)
	ProgressFn    func(phase, detail string)
}

// RestoreResult describes the outcome of a restore operation.
type RestoreResult struct {
	ManifestVersion  int      `json:"manifest_version"`
	SchemaVersion    int      `json:"schema_version"`
	DatabaseRestored bool     `json:"database_restored"`
	FilesExtracted   int      `json:"files_extracted"`
	BytesExtracted   int64    `json:"bytes_extracted"`
	Warnings         []string `json:"warnings,omitempty"`
}

// Restore reads an archive produced by Run() and restores DB + filesystem.
func Restore(ctx context.Context, opts RestoreOptions) (*RestoreResult, error) {
	progress := func(phase, detail string) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(phase, detail)
		}
	}

	progress("init", "opening archive")

	f, err := os.Open(opts.ArchivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("decompress archive: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)

	// Read all entries into memory maps for multi-pass access.
	var manifestData []byte
	var dbDump []byte
	wsEntries := map[string][]byte{}
	dataEntries := map[string][]byte{}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}

		switch {
		case hdr.Name == "manifest.json":
			manifestData = data
		case hdr.Name == "database/dump.sql":
			dbDump = data
		case strings.HasPrefix(hdr.Name, "workspace/"):
			rel := strings.TrimPrefix(hdr.Name, "workspace/")
			wsEntries[rel] = data
		case strings.HasPrefix(hdr.Name, "data/"):
			rel := strings.TrimPrefix(hdr.Name, "data/")
			dataEntries[rel] = data
		}
	}

	if manifestData == nil {
		return nil, fmt.Errorf("manifest.json not found in archive")
	}

	var manifest BackupManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.Format != "goclaw-system-backup" {
		return nil, fmt.Errorf("unsupported archive format: %q", manifest.Format)
	}

	result := &RestoreResult{
		ManifestVersion: manifest.Version,
		SchemaVersion:   manifest.SchemaVersion,
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

	if opts.DryRun {
		progress("dry-run", fmt.Sprintf("manifest ok: schema=%d, db=%d bytes, files=%d",
			manifest.SchemaVersion, manifest.Stats.DatabaseSizeBytes, manifest.Stats.FilesystemFiles))
		return result, nil
	}

	// -- Database restore -------------------------------------------------------
	if !opts.SkipDB && opts.DSN != "" {
		if dbDump == nil {
			result.Warnings = append(result.Warnings, "no database/dump.sql in archive; database restore skipped")
		} else {
			progress("database", "restoring database")
			if err := RestoreDatabase(ctx, opts.DSN, bytes.NewReader(dbDump)); err != nil {
				return nil, fmt.Errorf("database restore: %w", err)
			}
			result.DatabaseRestored = true
			progress("database", fmt.Sprintf("done (%d bytes)", len(dbDump)))
		}
	}

	// -- Filesystem restore -----------------------------------------------------
	if !opts.SkipFiles {
		if opts.WorkspacePath != "" && len(wsEntries) > 0 {
			progress("filesystem", "extracting workspace")
			n, b, err := extractEntries(wsEntries, opts.WorkspacePath)
			if err != nil {
				return nil, fmt.Errorf("extract workspace: %w", err)
			}
			result.FilesExtracted += n
			result.BytesExtracted += b
			progress("filesystem", fmt.Sprintf("workspace done (%d files)", n))
		}
		if opts.DataDir != "" && len(dataEntries) > 0 {
			progress("filesystem", "extracting data dir")
			n, b, err := extractEntries(dataEntries, opts.DataDir)
			if err != nil {
				return nil, fmt.Errorf("extract data dir: %w", err)
			}
			result.FilesExtracted += n
			result.BytesExtracted += b
			progress("filesystem", fmt.Sprintf("data dir done (%d files)", n))
		}
	}

	progress("done", fmt.Sprintf("restore complete: db=%v, files=%d", result.DatabaseRestored, result.FilesExtracted))
	return result, nil
}

// extractEntries writes in-memory tar entry data to targetDir.
// Enforces path traversal prevention on every entry name.
func extractEntries(entries map[string][]byte, targetDir string) (files int, written int64, err error) {
	targetDir = filepath.Clean(targetDir)

	for name, data := range entries {
		cleanName := filepath.Clean(name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			continue // skip malicious paths
		}
		resolved := filepath.Join(targetDir, cleanName)
		if !strings.HasPrefix(resolved, targetDir+string(filepath.Separator)) &&
			resolved != targetDir {
			continue // escape attempt
		}

		if err := os.MkdirAll(filepath.Dir(resolved), 0750); err != nil {
			return files, written, fmt.Errorf("create dir for %s: %w", cleanName, err)
		}
		if err := os.WriteFile(resolved, data, 0644); err != nil {
			return files, written, fmt.Errorf("write %s: %w", cleanName, err)
		}
		files++
		written += int64(len(data))
	}
	return files, written, nil
}
