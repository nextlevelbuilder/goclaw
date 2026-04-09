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
	"time"
)

// Options configures a system backup run.
type Options struct {
	DSN           string
	DataDir       string
	WorkspacePath string
	OutputPath    string // destination .tar.gz file path
	CreatedBy     string // user ID or "cli"
	GoclawVersion string
	SchemaVersion int
	ExcludeDB     bool
	ExcludeFiles  bool
	ProgressFn    func(phase string, detail string)
}

// Run creates a full system backup archive at opts.OutputPath.
// Returns the manifest on success. The archive format is:
//
//	manifest.json
//	database/dump.sql   (unless ExcludeDB)
//	workspace/          (unless ExcludeFiles)
//	data/               (unless ExcludeFiles, tmp dirs skipped)
func Run(ctx context.Context, opts Options) (*BackupManifest, error) {
	progress := func(phase, detail string) {
		if opts.ProgressFn != nil {
			opts.ProgressFn(phase, detail)
		}
	}

	outFile, err := os.Create(opts.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	gw := gzip.NewWriter(outFile)
	tw := tar.NewWriter(gw)

	manifest := &BackupManifest{
		Version:       1,
		Format:        "goclaw-system-backup",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:     opts.CreatedBy,
		GoclawVersion: opts.GoclawVersion,
		SchemaVersion: opts.SchemaVersion,
		DatabaseDSN:   SanitizeDSN(opts.DSN),
		Paths: PathsInfo{
			DataDir:   opts.DataDir,
			Workspace: opts.WorkspacePath,
		},
	}

	// -- Database dump --------------------------------------------------------
	if !opts.ExcludeDB && opts.DSN != "" {
		progress("database", "starting pg_dump")

		pgVer, _ := PgDumpVersion(ctx)
		manifest.PgDumpVersion = pgVer

		var dbBuf bytes.Buffer
		if err := DumpDatabase(ctx, opts.DSN, &dbBuf); err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("database dump: %w", err)
		}

		manifest.Stats.DatabaseSizeBytes = int64(dbBuf.Len())
		if err := addBytesToTar(tw, "database/dump.sql", dbBuf.Bytes()); err != nil {
			tw.Close()
			gw.Close()
			return nil, fmt.Errorf("write database/dump.sql: %w", err)
		}
		progress("database", fmt.Sprintf("done (%d bytes)", dbBuf.Len()))
	} else if opts.ExcludeDB {
		manifest.PgDumpVersion = "excluded"
	}

	// -- Filesystem archive ---------------------------------------------------
	if !opts.ExcludeFiles {
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
	}

	manifest.Stats.TotalBytes = manifest.Stats.DatabaseSizeBytes + manifest.Stats.FilesystemBytes

	// -- Manifest (last, so stats are complete) -------------------------------
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

// addBytesToTar writes a single in-memory file into the tar archive.
func addBytesToTar(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:     name,
		Mode:     0644,
		Size:     int64(len(data)),
		ModTime:  time.Now().UTC(),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := io.Copy(tw, bytes.NewReader(data))
	return err
}
