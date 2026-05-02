package upgrade

import (
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestRequiredSchemaVersionMatchesLatestMigration(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("expected migration files")
	}

	re := regexp.MustCompile(`^(\d+)_`)
	var latest uint
	for _, path := range matches {
		base := filepath.Base(path)
		parts := re.FindStringSubmatch(base)
		if len(parts) != 2 {
			t.Fatalf("migration file %q does not start with numeric version", base)
		}
		version, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			t.Fatalf("parse migration version from %q: %v", base, err)
		}
		if uint(version) > latest {
			latest = uint(version)
		}
	}

	if RequiredSchemaVersion != latest {
		t.Fatalf("RequiredSchemaVersion = %d, latest migration = %d", RequiredSchemaVersion, latest)
	}
}
