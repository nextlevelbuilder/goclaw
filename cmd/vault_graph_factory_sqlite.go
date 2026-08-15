//go:build sqlite || sqliteonly

package cmd

import (
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
)

func newVaultGraphStore(db *sql.DB) store.VaultGraphStore { return sqlitestore.NewSQLiteVaultGraphStore(db) }
func newKGGraphStore(db *sql.DB) store.KGGraphStore       { return sqlitestore.NewSQLiteKGGraphStore(db) }
