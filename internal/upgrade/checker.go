package upgrade

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
)

// SchemaStatus represents the result of a schema compatibility check.
type SchemaStatus struct {
	CurrentVersion  uint
	RequiredVersion uint
	Dirty           bool
	Compatible      bool
	NeedsMigration  bool
}

var (
	ErrSchemaOutdated = errors.New("database schema is outdated")
	ErrSchemaDirty    = errors.New("database schema is dirty (failed migration)")
	ErrSchemaAhead    = errors.New("database schema is newer than this binary")
)

// CheckSchema queries the schema_migrations table and compares
// against RequiredSchemaVersion to determine compatibility.
func CheckSchema(db *sql.DB) (*SchemaStatus, error) {
	var version uint
	var dirty bool

	err := db.QueryRow("SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&version, &dirty)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &SchemaStatus{
				RequiredVersion: RequiredSchemaVersion,
				NeedsMigration:  true,
			}, nil
		}
		// Table might not exist (fresh DB).
		return &SchemaStatus{
			RequiredVersion: RequiredSchemaVersion,
			NeedsMigration:  true,
		}, nil
	}

	s := &SchemaStatus{
		CurrentVersion:  version,
		RequiredVersion: RequiredSchemaVersion,
		Dirty:           dirty,
	}

	if dirty {
		return s, nil
	}

	switch {
	case version == RequiredSchemaVersion:
		s.Compatible = true
	case version < RequiredSchemaVersion:
		s.NeedsMigration = true
	default:
		// Schema is ahead — binary is too old.
	}

	return s, nil
}

// FormatError returns a user-friendly error message for the given status.
func FormatError(s *SchemaStatus) string {
	if s.Dirty {
		return fmt.Sprintf(
			"Database schema is in a dirty state (version %d).\n"+
				"This usually means a migration failed partway.\n\n"+
				"  Fix:  ./goclaw migrate force %d\n"+
				"  Then: ./goclaw upgrade\n",
			s.CurrentVersion, s.CurrentVersion-1,
		)
	}
	if s.CurrentVersion > s.RequiredVersion {
		return fmt.Sprintf(
			"Database schema (v%d) is newer than this binary (requires v%d).\n"+
				"You may be running an older version of goclaw.\n\n"+
				"  Fix: upgrade your goclaw binary to the latest version.\n",
			s.CurrentVersion, s.RequiredVersion,
		)
	}
	return fmt.Sprintf(
		"Database schema is outdated: current v%d, required v%d.\n\n"+
			"  Run:  ./goclaw upgrade\n"+
			"  Or:   ./goclaw migrate up   (SQL-only, no data hooks)\n\n"+
			"  Docker/CI: set GOCLAW_AUTO_UPGRADE=true to upgrade automatically on startup.\n",
		s.CurrentVersion, s.RequiredVersion,
	)
}

// EnsureExtensions checks if pgcrypto and pgvector extensions exist.
// If they don't exist, attempts to create them. If creation fails due to
// permission errors, returns a clear error message for the DBA.
func EnsureExtensions(db *sql.DB) error {
	extensions := []string{"pgcrypto", "vector"}

	for _, ext := range extensions {
		exists, err := extensionExists(db, ext)
		if err != nil {
			return fmt.Errorf("check extension %q: %w", ext, err)
		}

		if !exists {
			if err := createExtension(db, ext); err != nil {
				return fmt.Errorf(
					"extension %q is required but missing.\n"+
						"Attempted to create it, but got permission error: %v\n\n"+
						"Ask your database administrator to run:\n"+
						"  CREATE EXTENSION IF NOT EXISTS \"%s\";\n\n"+
						"The goclaw database role may need explicit CREATE privilege on pgcrypto and pgvector.\n",
					ext, err, ext,
				)
			}
			slog.Info("created missing extension", "extension", ext)
		}
	}

	return nil
}

// extensionExists checks if an extension is installed in the current database.
func extensionExists(db *sql.DB, name string) (bool, error) {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pg_extension WHERE extname = $1
		)
	`, name).Scan(&exists)
	return exists, err
}

// createExtension attempts to create an extension. Returns error if the
// current role lacks CREATE privilege.
func createExtension(db *sql.DB, name string) error {
	_, err := db.Exec(fmt.Sprintf(`CREATE EXTENSION IF NOT EXISTS "%s"`, name))
	return err
}
