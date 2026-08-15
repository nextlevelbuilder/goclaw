package cmd

import (
	"path/filepath"
	"testing"
)

// TestNewMigrationSource_LoadsMigrations guards against the Windows regression
// where golang-migrate's file:// source driver failed to open an absolute
// drive-letter path (e.g. "file:///D:/..." → "open ." error). The iofs source
// over os.DirFS must load the real migrations directory on every platform.
func TestNewMigrationSource_LoadsMigrations(t *testing.T) {
	migrationsDir = filepath.Join("..", "migrations")
	t.Cleanup(func() { migrationsDir = "" })

	src, err := newMigrationSource()
	if err != nil {
		t.Fatalf("newMigrationSource: %v", err)
	}
	defer src.Close()

	first, err := src.First()
	if err != nil {
		t.Fatalf("read first migration: %v", err)
	}
	if first != 1 {
		t.Errorf("first migration version = %d, want 1", first)
	}
}
