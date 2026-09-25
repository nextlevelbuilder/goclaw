package oa

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type tenantCapturingContactStore struct {
	seenTenant uuid.UUID
}

func (s *tenantCapturingContactStore) UpsertContact(ctx context.Context, _, _, _, _, _, _, _, _, _, _ string) error {
	s.seenTenant = store.TenantIDFromContext(ctx)
	return nil
}

func (s *tenantCapturingContactStore) ListContacts(context.Context, store.ContactListOpts) ([]store.ChannelContact, error) {
	return nil, nil
}

func (s *tenantCapturingContactStore) CountContacts(context.Context, store.ContactListOpts) (int, error) {
	return 0, nil
}

func (s *tenantCapturingContactStore) GetContactsBySenderIDs(context.Context, []string) (map[string]store.ChannelContact, error) {
	return nil, nil
}
func (s *tenantCapturingContactStore) GetContactByID(context.Context, uuid.UUID) (*store.ChannelContact, error) {
	return nil, nil
}
func (s *tenantCapturingContactStore) GetSenderIDsByContactIDs(context.Context, []uuid.UUID) ([]string, error) {
	return nil, nil
}
func (s *tenantCapturingContactStore) MergeContacts(context.Context, []uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *tenantCapturingContactStore) UnmergeContacts(context.Context, []uuid.UUID) error {
	return nil
}
func (s *tenantCapturingContactStore) GetContactsByMergedID(context.Context, uuid.UUID) ([]store.ChannelContact, error) {
	return nil, nil
}
func (s *tenantCapturingContactStore) ResolveTenantUserID(context.Context, string, string) (string, error) {
	return "", nil
}

var _ store.ContactStore = (*tenantCapturingContactStore)(nil)

// TestHandleUserFollow_TenantScopedContact proves the follower contact is
// recorded under the channel's tenant, not MasterTenantID (review P1 fix).
func TestHandleUserFollow_TenantScopedContact(t *testing.T) {
	ch := newTestOAChannel(t)
	tenant := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	ch.SetTenantID(tenant)

	cs := &tenantCapturingContactStore{}
	ch.SetContactCollector(store.NewContactCollector(cs, cache.NewInMemoryCache[bool]()))

	ch.handleUserFollow(&oaInboundEvent{
		Sender: struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name,omitempty"`
		}{ID: "u1", DisplayName: "N"},
	})

	if cs.seenTenant != tenant {
		t.Errorf("contact recorded under tenant %v, want %v (MasterTenantID leak)", cs.seenTenant, tenant)
	}
}

// TestFollow_EmptySenderNoContact ensures an empty sender never records.
func TestFollow_EmptySenderNoContact(t *testing.T) {
	ch := newTestOAChannel(t)
	ch.SetTenantID(uuid.MustParse("33333333-3333-4333-8333-333333333333"))
	cs := &tenantCapturingContactStore{}
	ch.SetContactCollector(store.NewContactCollector(cs, cache.NewInMemoryCache[bool]()))

	ch.handleUserFollow(&oaInboundEvent{})
	if cs.seenTenant != uuid.Nil {
		t.Errorf("empty sender must not record a contact, got tenant %v", cs.seenTenant)
	}
}
