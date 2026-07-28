//go:build sqlite || sqliteonly

package sqlitestore

import (
	"path/filepath"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestNewSQLiteStoresTeamsIsNonNilConcreteAuditStore locks the wiring-boundary
// guarantee behind the audit gate's fail-safe: production never hands the gate a
// TYPED-NIL TeamWorkClassificationAuditStore.
//
// The distinction matters. The gate guards `auditStore == nil`, which catches a
// LITERAL NIL INTERFACE (store never wired) but NOT a TYPED NIL POINTER
// ((*SQLiteTeamStore)(nil) boxed in the interface — non-nil interface, nil
// receiver, would panic on method call). This test proves the factory — the only
// place Stores.Teams is assigned — yields a non-nil CONCRETE pointer, so no
// typed nil can reach the gate. No reflection; a plain type assertion.
func TestNewSQLiteStoresTeamsIsNonNilConcreteAuditStore(t *testing.T) {
	dir := t.TempDir()
	stores, err := NewSQLiteStores(store.StoreConfig{
		SQLitePath:       filepath.Join(dir, "wiring.db"),
		SkillsStorageDir: dir,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStores: %v", err)
	}
	t.Cleanup(func() {
		if stores.DB != nil {
			_ = stores.DB.Close()
		}
	})

	// 1. Not a literal nil interface.
	if stores.Teams == nil {
		t.Fatal("Stores.Teams is a nil interface; the gate would run without an audit store")
	}
	// 2. The composed audit interface is satisfied (the gate takes exactly this).
	auditStore, ok := stores.Teams.(store.TeamWorkClassificationAuditStore)
	if !ok {
		t.Fatal("Stores.Teams does not satisfy TeamWorkClassificationAuditStore")
	}
	if auditStore == nil {
		t.Fatal("audit store view of Stores.Teams is nil")
	}
	// 3. Not a TYPED NIL: the underlying concrete pointer is non-nil, so a method
	//    call cannot panic on a nil receiver.
	concrete, ok := stores.Teams.(*SQLiteTeamStore)
	if !ok {
		t.Fatalf("Stores.Teams is not a *SQLiteTeamStore, got %T", stores.Teams)
	}
	if concrete == nil {
		t.Fatal("Stores.Teams holds a TYPED-NIL *SQLiteTeamStore; the gate's == nil guard would not catch it")
	}
}
