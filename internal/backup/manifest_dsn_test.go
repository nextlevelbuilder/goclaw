package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Backup.Run (filesystem-only, no DB) ---

func TestBackupRun_FilesystemOnly(t *testing.T) {
	// Setup source dirs
	ws := t.TempDir()
	dataDir := t.TempDir()
	outDir := t.TempDir()

	os.WriteFile(filepath.Join(ws, "agent.md"), []byte("agent content"), 0644)
	os.WriteFile(filepath.Join(dataDir, "config.json"), []byte(`{"key":"val"}`), 0644)

	outPath := filepath.Join(outDir, "backup.tar.gz")

	opts := Options{
		DataDir:       dataDir,
		WorkspacePath: ws,
		OutputPath:    outPath,
		CreatedBy:     "test",
		GoclawVersion: "test-1.0",
		SchemaVersion: 42,
		ExcludeDB:     true,
	}

	manifest, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify manifest fields
	if manifest.Version != 1 {
		t.Errorf("manifest.Version: got %d", manifest.Version)
	}
	if manifest.Format != "goclaw-system-backup" {
		t.Errorf("manifest.Format: got %q", manifest.Format)
	}
	if manifest.CreatedBy != "test" {
		t.Errorf("manifest.CreatedBy: got %q", manifest.CreatedBy)
	}
	if manifest.SchemaVersion != 42 {
		t.Errorf("manifest.SchemaVersion: got %d", manifest.SchemaVersion)
	}
	if manifest.PgDumpVersion != "excluded" {
		t.Errorf("ExcludeDB=true should set PgDumpVersion=excluded, got %q", manifest.PgDumpVersion)
	}
	if manifest.Stats.FilesystemFiles < 2 {
		t.Errorf("expected ≥2 filesystem files, got %d", manifest.Stats.FilesystemFiles)
	}

	// Verify archive exists and is valid gzip+tar
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var foundManifest, foundWorkspace, foundData bool
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		switch {
		case hdr.Name == "manifest.json":
			foundManifest = true
			data, _ := io.ReadAll(tr)
			var m BackupManifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Errorf("manifest.json parse error: %v", err)
			}
			if m.Format != "goclaw-system-backup" {
				t.Errorf("manifest in archive: wrong format %q", m.Format)
			}
		case strings.HasPrefix(hdr.Name, "workspace/"):
			foundWorkspace = true
		case strings.HasPrefix(hdr.Name, "data/"):
			foundData = true
		}
	}

	if !foundManifest {
		t.Error("archive should contain manifest.json")
	}
	if !foundWorkspace {
		t.Error("archive should contain workspace/ entries")
	}
	if !foundData {
		t.Error("archive should contain data/ entries")
	}
}

func TestBackupRun_ExcludeFiles(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "backup.tar.gz")

	opts := Options{
		OutputPath:    outPath,
		CreatedBy:     "test",
		GoclawVersion: "1.0",
		ExcludeDB:     true,
		ExcludeFiles:  true,
	}

	manifest, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if manifest.Stats.FilesystemFiles != 0 {
		t.Errorf("ExcludeFiles=true: expected 0 filesystem files, got %d", manifest.Stats.FilesystemFiles)
	}
}

func TestBackupRun_ProgressCallback(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "backup.tar.gz")

	var phases []string
	opts := Options{
		OutputPath:   outPath,
		CreatedBy:    "test",
		ExcludeDB:    true,
		ExcludeFiles: true,
		ProgressFn: func(phase, detail string) {
			phases = append(phases, phase)
		},
	}

	_, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Should have at least a "done" phase
	var hasDone bool
	for _, p := range phases {
		if p == "done" {
			hasDone = true
		}
	}
	if !hasDone {
		t.Errorf("expected 'done' phase in progress, got %v", phases)
	}
}

func TestBackupRun_InvalidOutputPath(t *testing.T) {
	opts := Options{
		OutputPath: "/nonexistent/deep/path/backup.tar.gz",
		ExcludeDB:  true,
	}
	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Error("expected error for invalid output path")
	}
}

func TestBackupRun_TotalBytesSum(t *testing.T) {
	ws := t.TempDir()
	outDir := t.TempDir()
	os.WriteFile(filepath.Join(ws, "file.txt"), []byte("hello"), 0644)

	opts := Options{
		WorkspacePath: ws,
		OutputPath:    filepath.Join(outDir, "backup.tar.gz"),
		CreatedBy:     "test",
		ExcludeDB:     true,
	}

	manifest, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if manifest.Stats.TotalBytes != manifest.Stats.DatabaseSizeBytes+manifest.Stats.FilesystemBytes {
		t.Errorf("TotalBytes should equal DB+FS: %d != %d+%d",
			manifest.Stats.TotalBytes, manifest.Stats.DatabaseSizeBytes, manifest.Stats.FilesystemBytes)
	}
}
