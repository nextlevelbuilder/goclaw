package upgrade

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRequiredSchemaVersionMatchesLatestMigration(t *testing.T) {
	matches, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no up migrations found")
	}

	var latest uint64
	for _, match := range matches {
		base := filepath.Base(match)
		prefix, _, ok := strings.Cut(base, "_")
		if !ok {
			t.Fatalf("migration %q missing numeric prefix", base)
		}
		version, err := strconv.ParseUint(prefix, 10, 64)
		if err != nil {
			t.Fatalf("parse migration prefix %q: %v", base, err)
		}
		if version > latest {
			latest = version
		}
	}

	if RequiredSchemaVersion != uint(latest) {
		t.Fatalf("RequiredSchemaVersion = %d, latest migration = %d", RequiredSchemaVersion, latest)
	}
}
