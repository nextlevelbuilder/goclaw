package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeChannelContextContactStore struct {
	contacts []store.ChannelContact
	seenOpts []store.ContactListOpts
}

func (s *fakeChannelContextContactStore) UpsertContact(context.Context, string, string, string, string, string, string, string, string, string, string) error {
	return nil
}

func (s *fakeChannelContextContactStore) ListContacts(_ context.Context, opts store.ContactListOpts) ([]store.ChannelContact, error) {
	s.seenOpts = append(s.seenOpts, opts)
	var out []store.ChannelContact
	for _, c := range s.contacts {
		if opts.ChannelType != "" && c.ChannelType != opts.ChannelType {
			continue
		}
		if opts.ChannelInstance != "" && (c.ChannelInstance == nil || *c.ChannelInstance != opts.ChannelInstance) {
			continue
		}
		if opts.ContactType != "" && c.ContactType != opts.ContactType {
			continue
		}
		if opts.PeerKind != "" && (c.PeerKind == nil || *c.PeerKind != opts.PeerKind) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *fakeChannelContextContactStore) CountContacts(context.Context, store.ContactListOpts) (int, error) {
	return len(s.contacts), nil
}

func (s *fakeChannelContextContactStore) GetContactsBySenderIDs(context.Context, []string) (map[string]store.ChannelContact, error) {
	return map[string]store.ChannelContact{}, nil
}

func (s *fakeChannelContextContactStore) GetContactByID(context.Context, uuid.UUID) (*store.ChannelContact, error) {
	return nil, nil
}

func (s *fakeChannelContextContactStore) GetSenderIDsByContactIDs(context.Context, []uuid.UUID) ([]string, error) {
	return nil, nil
}

func (s *fakeChannelContextContactStore) MergeContacts(context.Context, []uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *fakeChannelContextContactStore) UnmergeContacts(context.Context, []uuid.UUID) error {
	return nil
}

func (s *fakeChannelContextContactStore) GetContactsByMergedID(context.Context, uuid.UUID) ([]store.ChannelContact, error) {
	return nil, nil
}

func (s *fakeChannelContextContactStore) ResolveTenantUserID(context.Context, string, string) (string, error) {
	return "", nil
}

func TestChannelContextsListUsesStoredGroupsAndMasksToContextShape(t *testing.T) {
	token := "channel-contexts-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})

	instID := uuid.New()
	channelName := "discord-admin"
	displayName := "Ops Discord"
	peerKind := "group"
	lastSeen := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	contacts := &fakeChannelContextContactStore{contacts: []store.ChannelContact{{
		ID:              uuid.New(),
		ChannelType:     "discord",
		ChannelInstance: &channelName,
		SenderID:        "guild-123/channel-456",
		DisplayName:     &displayName,
		PeerKind:        &peerKind,
		ContactType:     "group",
		LastSeenAt:      lastSeen,
	}}}
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: instID}, Name: channelName, DisplayName: "Discord Admin", ChannelType: "discord", AgentID: uuid.New()}},
		nil, nil, contacts, nil, nil,
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+instID.String()+"/contexts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Contexts []channelContextDTO `json:"contexts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Contexts) != 2 {
		t.Fatalf("contexts len = %d, want 2: %+v", len(body.Contexts), body.Contexts)
	}
	if body.Contexts[0].ScopeType != store.ChannelScopeTypeChannel || body.Contexts[0].ScopeKey != channelName {
		t.Fatalf("base context = %+v", body.Contexts[0])
	}
	gotGroup := body.Contexts[1]
	if gotGroup.ScopeType != store.ChannelScopeTypeGroup || gotGroup.ScopeKey != "guild-123/channel-456" || gotGroup.Source != "stored_contact" {
		t.Fatalf("group context = %+v", gotGroup)
	}
	if len(contacts.seenOpts) == 0 || contacts.seenOpts[0].ChannelInstance != channelName {
		t.Fatalf("expected channel_instance filter %q, got %+v", channelName, contacts.seenOpts)
	}
}

func TestChannelContextMembersDoNotShowChannelWideContactsAsGroupMembers(t *testing.T) {
	token := "channel-context-members-key"
	setupTestCache(t, map[string]*store.APIKeyData{
		crypto.HashAPIKey(token): {Scopes: []string{"operator.write"}, OwnerID: "caller"},
	})

	instID := uuid.New()
	channelName := "telegram-main"
	peerKind := "group"
	displayName := "Duy"
	username := "duy"
	userID := "tenant-user-1"
	contacts := &fakeChannelContextContactStore{contacts: []store.ChannelContact{{
		ID:              uuid.New(),
		ChannelType:     "telegram",
		ChannelInstance: &channelName,
		SenderID:        "386246614",
		UserID:          &userID,
		DisplayName:     &displayName,
		Username:        &username,
		PeerKind:        &peerKind,
		ContactType:     "user",
		LastSeenAt:      time.Date(2026, 5, 29, 10, 5, 0, 0, time.UTC),
	}}}
	handler := NewChannelInstancesHandler(
		&stubChannelInstanceStore{inst: &store.ChannelInstanceData{BaseModel: store.BaseModel{ID: instID}, Name: channelName, ChannelType: "telegram", AgentID: uuid.New()}},
		nil, nil, contacts, nil, nil,
	)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/v1/channels/instances/"+instID.String()+"/contexts/group/group:telegram:-100123/members", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Members              []channelContextMemberDTO `json:"members"`
		LiveMembersSupported bool                      `json:"live_members_supported"`
		Source               string                    `json:"source"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.LiveMembersSupported || body.Source != "stored_contacts" {
		t.Fatalf("expected unsupported stored contact response, got %+v", body)
	}
	if len(body.Members) != 0 {
		t.Fatalf("members = %+v", body.Members)
	}
}
