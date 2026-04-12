//go:build integration

package integration

import (
	"errors"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func newLinkStore(db *sql.DB) *pg.PGAgentLinkStore {
	return pg.NewPGAgentLinkStore(db)
}

func makeLink(srcID, tgtID uuid.UUID, direction, status string) *store.AgentLinkData {
	return &store.AgentLinkData{
		SourceAgentID: srcID,
		TargetAgentID: tgtID,
		Direction:     direction,
		Status:        status,
		MaxConcurrent: 1,
		CreatedBy:     "test",
	}
}

func TestStoreAgentLink_CreateAndGet(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	link := makeLink(agentA, agentB, store.LinkDirectionOutbound, store.LinkStatusActive)
	if err := ls.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_links WHERE id = $1", link.ID)
	})

	if link.ID == uuid.Nil {
		t.Fatal("expected link.ID to be set after CreateLink")
	}

	got, err := ls.GetLink(ctx, link.ID)
	if err != nil {
		t.Fatalf("GetLink: %v", err)
	}
	if got.SourceAgentID != agentA {
		t.Errorf("SourceAgentID = %v, want %v", got.SourceAgentID, agentA)
	}
	if got.TargetAgentID != agentB {
		t.Errorf("TargetAgentID = %v, want %v", got.TargetAgentID, agentB)
	}
	if got.Direction != store.LinkDirectionOutbound {
		t.Errorf("Direction = %q, want %q", got.Direction, store.LinkDirectionOutbound)
	}
	if got.Status != store.LinkStatusActive {
		t.Errorf("Status = %q, want %q", got.Status, store.LinkStatusActive)
	}
}

func TestStoreAgentLink_Delete(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	link := makeLink(agentA, agentB, store.LinkDirectionBidirectional, store.LinkStatusActive)
	if err := ls.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}

	if err := ls.DeleteLink(ctx, link.ID); err != nil {
		t.Fatalf("DeleteLink: %v", err)
	}

	_, err := ls.GetLink(ctx, link.ID)
	if err == nil {
		t.Error("expected error after DeleteLink, got nil")
	}
}

func TestStoreAgentLink_Update(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	link := makeLink(agentA, agentB, store.LinkDirectionOutbound, store.LinkStatusActive)
	if err := ls.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_links WHERE id = $1", link.ID)
	})

	if err := ls.UpdateLink(ctx, link.ID, map[string]any{"status": store.LinkStatusDisabled}); err != nil {
		t.Fatalf("UpdateLink: %v", err)
	}

	got, err := ls.GetLink(ctx, link.ID)
	if err != nil {
		t.Fatalf("GetLink after update: %v", err)
	}
	if got.Status != store.LinkStatusDisabled {
		t.Errorf("Status after update = %q, want %q", got.Status, store.LinkStatusDisabled)
	}
}

func TestStoreAgentLink_ListLinksFrom(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	_, agentC := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	// agentA → agentB, agentA → agentC
	linkAB := makeLink(agentA, agentB, store.LinkDirectionOutbound, store.LinkStatusActive)
	linkAC := makeLink(agentA, agentC, store.LinkDirectionOutbound, store.LinkStatusActive)
	for _, l := range []*store.AgentLinkData{linkAB, linkAC} {
		if err := ls.CreateLink(ctx, l); err != nil {
			t.Fatalf("CreateLink: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_links WHERE id = $1", linkAB.ID)
		db.Exec("DELETE FROM agent_links WHERE id = $1", linkAC.ID)
	})

	links, err := ls.ListLinksFrom(ctx, agentA)
	if err != nil {
		t.Fatalf("ListLinksFrom: %v", err)
	}

	found := map[uuid.UUID]bool{}
	for _, l := range links {
		found[l.ID] = true
	}
	if !found[linkAB.ID] {
		t.Error("ListLinksFrom: linkAB not found")
	}
	if !found[linkAC.ID] {
		t.Error("ListLinksFrom: linkAC not found")
	}
}

func TestStoreAgentLink_ListLinksTo(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	_, agentC := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	// agentA → agentC, agentB → agentC
	linkAC := makeLink(agentA, agentC, store.LinkDirectionOutbound, store.LinkStatusActive)
	linkBC := makeLink(agentB, agentC, store.LinkDirectionOutbound, store.LinkStatusActive)
	for _, l := range []*store.AgentLinkData{linkAC, linkBC} {
		if err := ls.CreateLink(ctx, l); err != nil {
			t.Fatalf("CreateLink: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_links WHERE id = $1", linkAC.ID)
		db.Exec("DELETE FROM agent_links WHERE id = $1", linkBC.ID)
	})

	links, err := ls.ListLinksTo(ctx, agentC)
	if err != nil {
		t.Fatalf("ListLinksTo: %v", err)
	}
	found := map[uuid.UUID]bool{}
	for _, l := range links {
		found[l.ID] = true
	}
	if !found[linkAC.ID] {
		t.Error("ListLinksTo: linkAC not found")
	}
	if !found[linkBC.ID] {
		t.Error("ListLinksTo: linkBC not found")
	}
}

func TestStoreAgentLink_CanDelegate(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	_, agentC := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	// outbound A→B (A can delegate to B).
	link := makeLink(agentA, agentB, store.LinkDirectionOutbound, store.LinkStatusActive)
	if err := ls.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_links WHERE id = $1", link.ID) })

	t.Run("can_delegate_outbound", func(t *testing.T) {
		ok, err := ls.CanDelegate(ctx, agentA, agentB)
		if err != nil {
			t.Fatalf("CanDelegate: %v", err)
		}
		if !ok {
			t.Error("expected A can delegate to B (outbound link)")
		}
	})

	t.Run("no_link_returns_false", func(t *testing.T) {
		ok, err := ls.CanDelegate(ctx, agentA, agentC)
		if err != nil {
			t.Fatalf("CanDelegate no-link: %v", err)
		}
		if ok {
			t.Error("expected CanDelegate false for agents with no link")
		}
	})

	t.Run("disabled_link_returns_false", func(t *testing.T) {
		linkDis := makeLink(agentA, agentC, store.LinkDirectionOutbound, store.LinkStatusDisabled)
		if err := ls.CreateLink(ctx, linkDis); err != nil {
			t.Fatalf("CreateLink disabled: %v", err)
		}
		t.Cleanup(func() { db.Exec("DELETE FROM agent_links WHERE id = $1", linkDis.ID) })

		ok, err := ls.CanDelegate(ctx, agentA, agentC)
		if err != nil {
			t.Fatalf("CanDelegate disabled: %v", err)
		}
		if ok {
			t.Error("disabled link should not permit delegation")
		}
	})
}

func TestStoreAgentLink_GetLinkBetween(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	link := makeLink(agentA, agentB, store.LinkDirectionBidirectional, store.LinkStatusActive)
	if err := ls.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_links WHERE id = $1", link.ID) })

	got, err := ls.GetLinkBetween(ctx, agentA, agentB)
	if err != nil {
		t.Fatalf("GetLinkBetween: %v", err)
	}
	if got == nil {
		t.Fatal("GetLinkBetween returned nil")
	}
	if got.ID != link.ID {
		t.Errorf("ID = %v, want %v", got.ID, link.ID)
	}
}

func TestStoreAgentLink_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA1 := seedTenantAgent(t, db)
	tenantB, agentB1 := seedTenantAgent(t, db)
	// Create a second agent in each tenant for valid links.
	_, agentA2 := seedTenantAgent(t, db)
	_, agentB2 := seedTenantAgent(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ls := newLinkStore(db)

	// Link in tenant A.
	linkA := makeLink(agentA1, agentA2, store.LinkDirectionOutbound, store.LinkStatusActive)
	if err := ls.CreateLink(ctxA, linkA); err != nil {
		t.Fatalf("CreateLink tenantA: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_links WHERE id = $1", linkA.ID) })

	// Link in tenant B.
	linkB := makeLink(agentB1, agentB2, store.LinkDirectionOutbound, store.LinkStatusActive)
	if err := ls.CreateLink(ctxB, linkB); err != nil {
		t.Fatalf("CreateLink tenantB: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM agent_links WHERE id = $1", linkB.ID) })

	// Tenant B cannot GetLink for tenant A's link.
	_, err := ls.GetLink(ctxB, linkA.ID)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		// Accept any error (ErrNoRows or wrapped) — what matters is it's not found.
		if err == nil {
			t.Error("tenant B can read tenant A's link — isolation broken")
		}
	}

	// ListLinksFrom for agentA1 under tenantB scope should be empty.
	fromB, err := ls.ListLinksFrom(ctxB, agentA1)
	if err != nil {
		t.Fatalf("ListLinksFrom cross-tenant: %v", err)
	}
	for _, l := range fromB {
		if l.ID == linkA.ID {
			t.Error("tenant B sees tenant A's link via ListLinksFrom")
		}
	}
}

func TestStoreAgentLink_DelegateTargets(t *testing.T) {
	db := testDB(t)
	tenantID, agentA := seedTenantAgent(t, db)
	_, agentB := seedTenantAgent(t, db)
	_, agentC := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ls := newLinkStore(db)

	// Active outbound A→B, active outbound A→C.
	lAB := makeLink(agentA, agentB, store.LinkDirectionOutbound, store.LinkStatusActive)
	lAC := makeLink(agentA, agentC, store.LinkDirectionOutbound, store.LinkStatusActive)
	for _, l := range []*store.AgentLinkData{lAB, lAC} {
		if err := ls.CreateLink(ctx, l); err != nil {
			t.Fatalf("CreateLink: %v", err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_links WHERE id = $1", lAB.ID)
		db.Exec("DELETE FROM agent_links WHERE id = $1", lAC.ID)
	})

	targets, err := ls.DelegateTargets(ctx, agentA)
	if err != nil {
		t.Fatalf("DelegateTargets: %v", err)
	}
	found := map[uuid.UUID]bool{}
	for _, l := range targets {
		found[l.ID] = true
	}
	if !found[lAB.ID] {
		t.Error("DelegateTargets: lAB not found")
	}
	if !found[lAC.ID] {
		t.Error("DelegateTargets: lAC not found")
	}
}
