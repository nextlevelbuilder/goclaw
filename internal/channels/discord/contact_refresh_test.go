package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type contactRefreshStore struct {
	contacts map[string]string
}

func (s *contactRefreshStore) UpsertContact(_ context.Context, _ string, channelInstance string, senderID string, _ string, displayName string, _ string, _ string, contactType string, _ string, _ string) error {
	if s.contacts == nil {
		s.contacts = make(map[string]string)
	}
	s.contacts[channelInstance+":"+contactType+":"+senderID] = displayName
	return nil
}

func (s *contactRefreshStore) ListContacts(context.Context, store.ContactListOpts) ([]store.ChannelContact, error) {
	return nil, nil
}
func (s *contactRefreshStore) CountContacts(context.Context, store.ContactListOpts) (int, error) {
	return 0, nil
}
func (s *contactRefreshStore) GetContactsBySenderIDs(context.Context, []string) (map[string]store.ChannelContact, error) {
	return nil, nil
}
func (s *contactRefreshStore) GetContactByID(context.Context, uuid.UUID) (*store.ChannelContact, error) {
	return nil, nil
}
func (s *contactRefreshStore) GetSenderIDsByContactIDs(context.Context, []uuid.UUID) ([]string, error) {
	return nil, nil
}
func (s *contactRefreshStore) MergeContacts(context.Context, []uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *contactRefreshStore) UnmergeContacts(context.Context, []uuid.UUID) error {
	return nil
}
func (s *contactRefreshStore) GetContactsByMergedID(context.Context, uuid.UUID) ([]store.ChannelContact, error) {
	return nil, nil
}
func (s *contactRefreshStore) ResolveTenantUserID(context.Context, string, string) (string, error) {
	return "", nil
}

func TestRefreshContactCacheStoresDiscordChannelTitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/guilds/guild-1/channels":
			_, _ = w.Write([]byte(`[
				{"id":"parent-1","name":"product-planning","type":0},
				{"id":"category-1","name":"operations","type":4}
			]`))
		case "/guilds/guild-1/threads/active":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread-1","name":"launch-thread","type":11}],"members":[],"has_more":false}`))
		default:
			t.Fatalf("unexpected Discord API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevGuildChannels := discordgo.EndpointGuildChannels
	prevGuildActiveThreads := discordgo.EndpointGuildActiveThreads
	discordgo.EndpointGuildChannels = func(gID string) string { return server.URL + "/guilds/" + gID + "/channels" }
	discordgo.EndpointGuildActiveThreads = func(gID string) string { return server.URL + "/guilds/" + gID + "/threads/active" }
	t.Cleanup(func() {
		discordgo.EndpointGuildChannels = prevGuildChannels
		discordgo.EndpointGuildActiveThreads = prevGuildActiveThreads
	})

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		Channels: []*discordgo.Channel{
			{ID: "state-channel", Name: "support", Type: discordgo.ChannelTypeGuildText},
		},
		Members: []*discordgo.Member{
			{Nick: "Casey", User: &discordgo.User{ID: "user-1", Username: "casey.dev", GlobalName: "Casey Dev"}},
		},
	}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	contactStore := &contactRefreshStore{}
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil),
		session:     session,
	}
	ch.SetName("discord-main")
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	ch.refreshContactCache(context.Background())

	want := map[string]string{
		"discord-main:group:state-channel": "support",
		"discord-main:group:parent-1":      "product-planning",
		"discord-main:group:category-1":    "operations",
		"discord-main:group:thread-1":      "launch-thread",
		"discord-main:user:user-1":         "Casey",
	}
	for key, title := range want {
		if contactStore.contacts[key] != title {
			t.Fatalf("contact %s = %q, want %q; all=%#v", key, contactStore.contacts[key], title, contactStore.contacts)
		}
	}
}
