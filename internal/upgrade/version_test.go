package upgrade

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.(up|down)\.sql$`)

func TestMigrationManifestIsContiguousAndPaired(t *testing.T) {
	migrationDir := filepath.Join("..", "..", "migrations")
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}

	type pair struct {
		up   string
		down string
	}
	versions := make(map[int]pair)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" || entry.Name() == "audit_fk_integrity.sql" {
			continue
		}
		match := migrationFilenamePattern.FindStringSubmatch(entry.Name())
		if match == nil {
			t.Fatalf("migration %q does not match NNNNNN_name.{up,down}.sql", entry.Name())
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration version %q: %v", entry.Name(), err)
		}
		p := versions[version]
		switch match[2] {
		case "up":
			if p.up != "" {
				t.Fatalf("migration version %06d has duplicate up files %q and %q", version, p.up, entry.Name())
			}
			p.up = entry.Name()
		case "down":
			if p.down != "" {
				t.Fatalf("migration version %06d has duplicate down files %q and %q", version, p.down, entry.Name())
			}
			p.down = entry.Name()
		}
		versions[version] = p
	}
	if len(versions) == 0 {
		t.Fatal("no migrations found")
	}

	for version := 1; version <= int(RequiredSchemaVersion); version++ {
		p, ok := versions[version]
		if !ok {
			t.Fatalf("migration manifest has gap at version %06d", version)
		}
		if p.up == "" || p.down == "" {
			t.Fatalf("migration version %06d is unpaired: up=%q down=%q", version, p.up, p.down)
		}
	}
	if len(versions) != int(RequiredSchemaVersion) {
		t.Fatalf("migration manifest has %d versions, RequiredSchemaVersion = %d", len(versions), RequiredSchemaVersion)
	}
}
