//go:build integration

package integration

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreContact_UpsertAndList(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGContactStore(db)

	senderID := "sender-" + uuid.New().String()[:8]

	// Upsert new contact.
	if err := s.UpsertContact(ctx, "telegram", "bot-123", senderID, "user-42", "Alice", "alice_tg", "private", "user", "", ""); err != nil {
		t.Fatalf("UpsertContact: %v", err)
	}

	// ListContacts — should find the upserted contact.
	contacts, err := s.ListContacts(ctx, store.ContactListOpts{Limit: 20})
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	found := false
	for _, c := range contacts {
		if c.SenderID == senderID {
			found = true
			if c.DisplayName == nil || *c.DisplayName != "Alice" {
				t.Errorf("expected DisplayName='Alice', got %v", c.DisplayName)
			}
			break
		}
	}
	if !found {
		t.Errorf("upserted contact senderID=%q not found in ListContacts (got %d)", senderID, len(contacts))
	}

	// Upsert same senderID again — should update display_name.
	if err := s.UpsertContact(ctx, "telegram", "bot-123", senderID, "user-42", "Alice Updated", "alice_tg", "private", "user", "", ""); err != nil {
		t.Fatalf("UpsertContact update: %v", err)
	}

	contacts2, err := s.ListContacts(ctx, store.ContactListOpts{Limit: 20})
	if err != nil {
		t.Fatalf("ListContacts after update: %v", err)
	}
	for _, c := range contacts2 {
		if c.SenderID == senderID {
			if c.DisplayName == nil || *c.DisplayName != "Alice Updated" {
				t.Errorf("expected DisplayName='Alice Updated', got %v", c.DisplayName)
			}
			break
		}
	}
}

func TestStoreContact_GetByID(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := pg.NewPGContactStore(db)

	senderID := fmt.Sprintf("sender-%s", uuid.New().String()[:8])

	if err := s.UpsertContact(ctx, "telegram", "bot-456", senderID, "", "Bob", "bob_tg", "private", "user", "", ""); err != nil {
		t.Fatalf("UpsertContact: %v", err)
	}

	// Find the contact ID via list.
	contacts, err := s.ListContacts(ctx, store.ContactListOpts{Limit: 50})
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	var contactID uuid.UUID
	for _, c := range contacts {
		if c.SenderID == senderID {
			contactID = c.ID
			break
		}
	}
	if contactID == uuid.Nil {
		t.Fatal("could not find contact ID after upsert")
	}

	// GetContactByID.
	got, err := s.GetContactByID(ctx, contactID)
	if err != nil {
		t.Fatalf("GetContactByID: %v", err)
	}
	if got.SenderID != senderID {
		t.Errorf("SenderID mismatch: got %q, want %q", got.SenderID, senderID)
	}
	if got.ChannelType != "telegram" {
		t.Errorf("ChannelType mismatch: got %q, want 'telegram'", got.ChannelType)
	}
}

func TestStoreContact_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, tenantB, _, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	s := pg.NewPGContactStore(db)

	senderID := "iso-sender-" + uuid.New().String()[:8]

	// Upsert contact in tenant A.
	if err := s.UpsertContact(ctxA, "telegram", "bot-789", senderID, "", "Carol", "carol_tg", "private", "user", "", ""); err != nil {
		t.Fatalf("UpsertContact tenantA: %v", err)
	}

	// Tenant B list — should not include tenant A's contact.
	contacts, err := s.ListContacts(ctxB, store.ContactListOpts{Limit: 50})
	if err != nil {
		t.Fatalf("ListContacts tenantB: %v", err)
	}
	for _, c := range contacts {
		if c.SenderID == senderID {
			t.Errorf("tenant B should not see tenant A's contact senderID=%q", senderID)
		}
	}
}
