package discord

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type contactRefreshStore struct {
	contacts map[string]string
	metadata map[string]map[string]string
	existing []store.ChannelContact
	listErr  error
}

type pendingGroupsStore struct {
	groups []store.PendingMessageGroup
}

func (s *pendingGroupsStore) AppendBatch(context.Context, []store.PendingMessage) error { return nil }
func (s *pendingGroupsStore) ListByKey(context.Context, string, string) ([]store.PendingMessage, error) {
	return nil, nil
}
func (s *pendingGroupsStore) DeleteByKey(context.Context, string, string) error { return nil }
func (s *pendingGroupsStore) Compact(context.Context, []uuid.UUID, *store.PendingMessage) error {
	return nil
}
func (s *pendingGroupsStore) DeleteStale(context.Context, time.Duration) (int64, error) {
	return 0, nil
}
func (s *pendingGroupsStore) ListGroups(context.Context) ([]store.PendingMessageGroup, error) {
	return s.groups, nil
}
func (s *pendingGroupsStore) CountAll(context.Context) (int64, error) { return 0, nil }
func (s *pendingGroupsStore) CountByKey(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *pendingGroupsStore) ResolveGroupTitles(context.Context, []store.PendingMessageGroup) (map[string]string, error) {
	return nil, nil
}

func (s *contactRefreshStore) UpsertContactWithMetadata(ctx context.Context, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType string, metadata map[string]string) error {
	if err := s.UpsertContact(ctx, channelType, channelInstance, senderID, userID, displayName, username, peerKind, contactType, threadID, threadType); err != nil {
		return err
	}
	if len(metadata) > 0 {
		if s.metadata == nil {
			s.metadata = make(map[string]map[string]string)
		}
		s.metadata[channelInstance+":"+contactType+":"+senderID] = metadata
	}
	return nil
}

func (s *contactRefreshStore) UpsertContact(_ context.Context, _ string, channelInstance string, senderID string, _ string, displayName string, _ string, _ string, contactType string, _ string, _ string) error {
	if s.contacts == nil {
		s.contacts = make(map[string]string)
	}
	s.contacts[channelInstance+":"+contactType+":"+senderID] = displayName
	return nil
}

func (s *contactRefreshStore) ListContacts(_ context.Context, opts store.ContactListOpts) ([]store.ChannelContact, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var filtered []store.ChannelContact
	for _, contact := range s.existing {
		if opts.ChannelType != "" && contact.ChannelType != opts.ChannelType {
			continue
		}
		if opts.ChannelInstance != "" && (contact.ChannelInstance == nil || *contact.ChannelInstance != opts.ChannelInstance) {
			continue
		}
		if opts.ContactType != "" && contact.ContactType != opts.ContactType {
			continue
		}
		filtered = append(filtered, contact)
	}
	if opts.Offset >= len(filtered) {
		return nil, nil
	}
	contacts := filtered[opts.Offset:]
	if opts.Limit > 0 && len(contacts) > opts.Limit {
		contacts = contacts[:opts.Limit]
	}
	return contacts, nil
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
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread-1","name":"launch-thread","parent_id":"parent-1","type":11}],"members":[],"has_more":false}`))
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
	if got := contactStore.metadata["discord-main:group:thread-1"][contactDisplayTitleMetadataKey]; got != "launch-thread / product-planning" {
		t.Fatalf("thread display metadata = %q, want qualified title", got)
	}
}

func TestRefreshContactCacheBackfillsStoredArchivedGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/guilds/guild-1/channels":
			_, _ = w.Write([]byte(`[]`))
		case "/guilds/guild-1/threads/active":
			_, _ = w.Write([]byte(`{"threads":[],"members":[],"has_more":false}`))
		case "/channels/archived-thread":
			_, _ = w.Write([]byte(`{"id":"archived-thread","name":"release-notes","parent_id":"parent-1","type":11}`))
		case "/channels/parent-1":
			_, _ = w.Write([]byte(`{"id":"parent-1","name":"announcements","type":0}`))
		default:
			t.Fatalf("unexpected Discord API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevGuildChannels := discordgo.EndpointGuildChannels
	prevGuildActiveThreads := discordgo.EndpointGuildActiveThreads
	prevChannel := discordgo.EndpointChannel
	discordgo.EndpointGuildChannels = func(gID string) string { return server.URL + "/guilds/" + gID + "/channels" }
	discordgo.EndpointGuildActiveThreads = func(gID string) string { return server.URL + "/guilds/" + gID + "/threads/active" }
	discordgo.EndpointChannel = func(cID string) string { return server.URL + "/channels/" + cID }
	t.Cleanup(func() {
		discordgo.EndpointGuildChannels = prevGuildChannels
		discordgo.EndpointGuildActiveThreads = prevGuildActiveThreads
		discordgo.EndpointChannel = prevChannel
	})

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	instance := "discord-main"
	contactStore := &contactRefreshStore{existing: []store.ChannelContact{{
		ChannelType:     channels.TypeDiscord,
		ChannelInstance: &instance,
		SenderID:        "archived-thread",
		ContactType:     "group",
	}}}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil), session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	ch.refreshContactCache(context.Background())

	if got := contactStore.contacts["discord-main:group:archived-thread"]; got != "release-notes" {
		t.Fatalf("archived group title = %q, want release-notes", got)
	}
	if got := contactStore.metadata["discord-main:group:archived-thread"][contactDisplayTitleMetadataKey]; got != "release-notes / announcements" {
		t.Fatalf("archived group display metadata = %q, want qualified title", got)
	}
}

func TestRefreshContactCacheBackfillsPendingOnlyGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/channels/pending-only" {
			t.Fatalf("unexpected Discord API path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pending-only","name":"history-only-thread","type":11}`))
	}))
	defer server.Close()

	prevChannel := discordgo.EndpointChannel
	discordgo.EndpointChannel = func(cID string) string { return server.URL + "/channels/" + cID }
	t.Cleanup(func() { discordgo.EndpointChannel = prevChannel })

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	instance := "discord-main"
	contactStore := &contactRefreshStore{}
	pendingStore := &pendingGroupsStore{groups: []store.PendingMessageGroup{{
		ChannelName: instance,
		HistoryKey:  "pending-only",
	}}}
	base := channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil)
	base.SetGroupHistory(channels.MakeHistory(instance, pendingStore, uuid.Nil))
	ch := &Channel{BaseChannel: base, session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	report, err := ch.RefreshContactCache(context.Background())
	if err != nil {
		t.Fatalf("RefreshContactCache() error = %v", err)
	}
	if !report.OK || report.PendingMessageTargets != 1 || report.DirectLookupResolved != 1 {
		t.Fatalf("report = %+v, want resolved pending-only target", report)
	}
	if got := contactStore.contacts[instance+":group:pending-only"]; got != "history-only-thread" {
		t.Fatalf("pending-only group title = %q, want history-only-thread", got)
	}
}

func TestRefreshContactCacheBackfillsEveryStoredGroupPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/guilds/guild-1/channels":
			_, _ = w.Write([]byte(`[]`))
		case "/guilds/guild-1/threads/active":
			_, _ = w.Write([]byte(`{"threads":[],"members":[],"has_more":false}`))
		default:
			if !strings.HasPrefix(r.URL.Path, "/channels/archived-") {
				t.Fatalf("unexpected Discord API path: %s", r.URL.Path)
			}
			id := strings.TrimPrefix(r.URL.Path, "/channels/")
			_, _ = fmt.Fprintf(w, `{"id":%q,"name":%q,"type":0}`, id, "title-"+id)
		}
	}))
	defer server.Close()

	prevGuildChannels := discordgo.EndpointGuildChannels
	prevGuildActiveThreads := discordgo.EndpointGuildActiveThreads
	prevChannel := discordgo.EndpointChannel
	discordgo.EndpointGuildChannels = func(gID string) string { return server.URL + "/guilds/" + gID + "/channels" }
	discordgo.EndpointGuildActiveThreads = func(gID string) string { return server.URL + "/guilds/" + gID + "/threads/active" }
	discordgo.EndpointChannel = func(cID string) string { return server.URL + "/channels/" + cID }
	t.Cleanup(func() {
		discordgo.EndpointGuildChannels = prevGuildChannels
		discordgo.EndpointGuildActiveThreads = prevGuildActiveThreads
		discordgo.EndpointChannel = prevChannel
	})

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatalf("GuildAdd: %v", err)
	}

	instance := "discord-main"
	contactStore := &contactRefreshStore{}
	for i := 0; i <= contactRefreshPageSize; i++ {
		id := fmt.Sprintf("archived-%03d", i)
		contactStore.existing = append(contactStore.existing, store.ChannelContact{
			ChannelType:     channels.TypeDiscord,
			ChannelInstance: &instance,
			SenderID:        id,
			ContactType:     "group",
		})
	}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil), session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	if _, err := ch.RefreshContactCache(context.Background()); err != nil {
		t.Fatalf("RefreshContactCache() error = %v", err)
	}
	for i := 0; i <= contactRefreshPageSize; i++ {
		id := fmt.Sprintf("archived-%03d", i)
		if got := contactStore.contacts[instance+":group:"+id]; got != "title-"+id {
			t.Fatalf("stored group %s title = %q, want %q", id, got, "title-"+id)
		}
	}
}

func TestRefreshContactCacheReportsUnreadableStoredGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing access", http.StatusForbidden)
	}))
	defer server.Close()

	prevChannel := discordgo.EndpointChannel
	discordgo.EndpointChannel = func(cID string) string { return server.URL + "/channels/" + cID }
	t.Cleanup(func() { discordgo.EndpointChannel = prevChannel })

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	instance := "discord-main"
	contactStore := &contactRefreshStore{existing: []store.ChannelContact{{
		ChannelType:     channels.TypeDiscord,
		ChannelInstance: &instance,
		SenderID:        "unreadable-group",
		ContactType:     "group",
	}}}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil), session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	report, err := ch.RefreshContactCache(context.Background())
	if err != nil {
		t.Fatalf("RefreshContactCache() error = %v", err)
	}
	if report.OK || len(report.Failures) != 1 || report.Failures[0].ChannelID != "unreadable-group" {
		t.Fatalf("report = %+v, want unreadable stored group failure", report)
	}
}

func TestRefreshContactCacheReportsStoredGroupListFailure(t *testing.T) {
	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	instance := "discord-main"
	contactStore := &contactRefreshStore{listErr: fmt.Errorf("database unavailable")}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil), session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	report, err := ch.RefreshContactCache(context.Background())
	if err != nil {
		t.Fatalf("RefreshContactCache() error = %v", err)
	}
	if report.OK || len(report.Errors) != 1 {
		t.Fatalf("report = %+v, want stored group list error", report)
	}
}

func TestRefreshContactCacheReportsMissingStoredThreadParent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/channels/thread-1":
			_, _ = w.Write([]byte(`{"id":"thread-1","name":"release-notes","parent_id":"parent-1","type":11}`))
		case "/channels/parent-1":
			http.Error(w, "missing access", http.StatusForbidden)
		default:
			t.Fatalf("unexpected Discord API path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	prevChannel := discordgo.EndpointChannel
	discordgo.EndpointChannel = func(cID string) string { return server.URL + "/channels/" + cID }
	t.Cleanup(func() { discordgo.EndpointChannel = prevChannel })

	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	session.Client = server.Client()
	instance := "discord-main"
	contactStore := &contactRefreshStore{existing: []store.ChannelContact{{
		ChannelType:     channels.TypeDiscord,
		ChannelInstance: &instance,
		SenderID:        "thread-1",
		ContactType:     "group",
	}}}
	ch := &Channel{BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil), session: session}
	ch.SetName(instance)
	ch.SetType(channels.TypeDiscord)
	ch.SetContactCollector(store.NewContactCollector(contactStore, cache.NewInMemoryCache[bool]()))

	report, err := ch.RefreshContactCache(context.Background())
	if err != nil {
		t.Fatalf("RefreshContactCache() error = %v", err)
	}
	if report.OK || len(report.Failures) != 1 || report.Failures[0].ChannelID != "thread-1" {
		t.Fatalf("report = %+v, want missing thread parent failure", report)
	}
}

func TestSetContactCollectorStartsRefreshAfterChannelStartup(t *testing.T) {
	session, err := discordgo.New("Bot test-token")
	if err != nil {
		t.Fatalf("discordgo.New: %v", err)
	}
	ch := &Channel{
		BaseChannel: channels.NewBaseChannel(channels.TypeDiscord, bus.New(), nil),
		session:     session,
	}
	ch.SetRunning(true)
	ch.SetContactCollector(store.NewContactCollector(&contactRefreshStore{}, cache.NewInMemoryCache[bool]()))
	if ch.contactRefreshCancel == nil {
		t.Fatal("contact refresh loop was not started after collector wiring")
	}
	ch.contactRefreshCancel()
}
