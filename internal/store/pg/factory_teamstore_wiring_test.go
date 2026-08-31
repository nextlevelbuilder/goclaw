package pg

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestNewPGTeamStoreIsNonNilConcreteAuditStore locks the wiring-boundary
// guarantee that production never hands the audit gate a TYPED-NIL
// TeamWorkClassificationAuditStore.
//
// The gate guards `auditStore == nil`, which catches a LITERAL NIL INTERFACE
// (store never wired) but NOT a TYPED NIL POINTER ((*PGTeamStore)(nil) boxed in
// the interface — non-nil interface, nil receiver, would panic on method call).
// pg/factory.go assigns Stores.Teams = NewPGTeamStore(db) — the only assignment
// in production — so this asserts the constructor yields a non-nil CONCRETE
// pointer regardless of DB connectivity. No live DB needed (so it never skips);
// no reflection, a plain type assertion.
func TestNewPGTeamStoreIsNonNilConcreteAuditStore(t *testing.T) {
	// The constructor does not touch the DB handle, so nil is fine here: we are
	// proving the returned pointer is non-nil, not exercising queries.
	ts := NewPGTeamStore(nil)
	if ts == nil {
		t.Fatal("NewPGTeamStore returned a nil *PGTeamStore")
	}

	// Assign through the interface exactly as the factory does, then prove it is
	// neither a literal nil interface nor a typed-nil pointer.
	var audit store.TeamWorkClassificationAuditStore = ts
	if audit == nil {
		t.Fatal("audit store interface is nil after assignment from the constructor")
	}
	concrete, ok := audit.(*PGTeamStore)
	if !ok {
		t.Fatalf("audit store is not a *PGTeamStore, got %T", audit)
	}
	if concrete == nil {
		t.Fatal("audit store holds a TYPED-NIL *PGTeamStore; the gate's == nil guard would not catch it")
	}
}
