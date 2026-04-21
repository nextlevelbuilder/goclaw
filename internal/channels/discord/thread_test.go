package discord

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

type stubAPI struct {
	state *discordgo.Channel
	rest  *discordgo.Channel

	// per-dispatch flags
	messageCalled bool
	threadCalled  bool
	forumCalled   bool

	// captured params
	gotMessageID string
	gotTS        *discordgo.ThreadStart
	gotMessage   *discordgo.MessageSend

	// return values
	created *discordgo.Channel
	err     error
}

func (s *stubAPI) stateChannel(id string) (*discordgo.Channel, error) {
	if s.state == nil {
		return nil, discordgo.ErrStateNotFound
	}
	return s.state, nil
}

func (s *stubAPI) restChannel(ctx context.Context, id string) (*discordgo.Channel, error) {
	if s.rest == nil {
		return nil, errors.New("not found")
	}
	return s.rest, nil
}

func (s *stubAPI) messageThreadStartComplex(ctx context.Context, channelID, messageID string, data *discordgo.ThreadStart) (*discordgo.Channel, error) {
	s.messageCalled = true
	s.gotMessageID = messageID
	s.gotTS = data
	return s.created, s.err
}

func (s *stubAPI) threadStartComplex(ctx context.Context, channelID string, data *discordgo.ThreadStart) (*discordgo.Channel, error) {
	s.threadCalled = true
	s.gotTS = data
	return s.created, s.err
}

func (s *stubAPI) forumThreadStartComplex(ctx context.Context, channelID string, threadData *discordgo.ThreadStart, messageData *discordgo.MessageSend) (*discordgo.Channel, error) {
	s.forumCalled = true
	s.gotTS = threadData
	s.gotMessage = messageData
	return s.created, s.err
}

func guildText(id string) *discordgo.Channel {
	return &discordgo.Channel{ID: id, Type: discordgo.ChannelTypeGuildText}
}

func forumChannel(id string) *discordgo.Channel {
	return &discordgo.Channel{ID: id, Type: discordgo.ChannelTypeGuildForum}
}

func TestCreateThread_MissingChannelID(t *testing.T) {
	api := &stubAPI{state: guildText("c")}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "channel_id") {
		t.Fatalf("expected channel_id error, got %v", err)
	}
}

func TestCreateThread_NameValidation(t *testing.T) {
	api := &stubAPI{state: guildText("c"), created: &discordgo.Channel{ID: "t"}}
	cases := []struct {
		label string
		name  string
	}{
		{"missing", ""},
		{"too-long", strings.Repeat("a", 101)},
	}
	for _, tc := range cases {
		_, err := createThread(context.Background(), api, channels.DiscordThreadParams{ChannelID: "c", Name: tc.name})
		if err == nil {
			t.Fatalf("%s: expected error, got nil", tc.label)
		}
	}
}

func TestCreateThread_DMRejected(t *testing.T) {
	api := &stubAPI{state: &discordgo.Channel{ID: "c", Type: discordgo.ChannelTypeDM}}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{ChannelID: "c", Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "DMs") {
		t.Fatalf("expected DM rejection, got %v", err)
	}
	if api.messageCalled || api.threadCalled || api.forumCalled {
		t.Fatal("no API call should be made for DM parent")
	}
}

func TestCreateThread_GroupDMRejected(t *testing.T) {
	api := &stubAPI{state: &discordgo.Channel{ID: "c", Type: discordgo.ChannelTypeGroupDM}}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{ChannelID: "c", Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "DMs") {
		t.Fatalf("expected DM rejection, got %v", err)
	}
}

func TestCreateThread_ForumRequiresInitialMessage(t *testing.T) {
	api := &stubAPI{state: forumChannel("c")}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{ChannelID: "c", Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "initial_message") {
		t.Fatalf("expected initial_message error, got %v", err)
	}
	if api.forumCalled {
		t.Fatal("forum dispatch should not have been called")
	}
}

func TestCreateThread_ForumHappyPath(t *testing.T) {
	api := &stubAPI{
		state:   forumChannel("c"),
		created: &discordgo.Channel{ID: "t1", Name: "post"},
	}
	res, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID:      "c",
		Name:           "post",
		InitialMessage: "hello",
		AppliedTags:    []string{"tag-a"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !api.forumCalled {
		t.Fatal("expected ForumThreadStartComplex dispatch")
	}
	if api.gotMessage == nil || api.gotMessage.Content != "hello" {
		t.Errorf("initial message not propagated: %+v", api.gotMessage)
	}
	if api.gotTS == nil || len(api.gotTS.AppliedTags) != 1 || api.gotTS.AppliedTags[0] != "tag-a" {
		t.Errorf("applied tags not propagated: %+v", api.gotTS)
	}
	if !res.IsForum {
		t.Errorf("expected IsForum=true")
	}
}

func TestCreateThread_MessageRooted(t *testing.T) {
	api := &stubAPI{
		state:   guildText("c"),
		created: &discordgo.Channel{ID: "t2", Name: "thread"},
	}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID: "c",
		MessageID: "m1",
		Name:      "thread",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !api.messageCalled {
		t.Fatal("expected MessageThreadStartComplex dispatch")
	}
	if api.gotMessageID != "m1" {
		t.Errorf("message_id: got %q want %q", api.gotMessageID, "m1")
	}
}

func TestCreateThread_StandaloneText(t *testing.T) {
	api := &stubAPI{
		state:   guildText("c"),
		created: &discordgo.Channel{ID: "t3", Name: "standalone"},
	}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID:      "c",
		Name:           "standalone",
		InitialMessage: "ignored for text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !api.threadCalled {
		t.Fatal("expected ThreadStartComplex dispatch")
	}
	if api.messageCalled || api.forumCalled {
		t.Fatal("only ThreadStartComplex should be called for standalone text thread")
	}
}

func TestCreateThread_PrivateTranslatesType(t *testing.T) {
	api := &stubAPI{state: guildText("c"), created: &discordgo.Channel{ID: "t"}}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID: "c",
		Name:      "x",
		Private:   true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.gotTS.Type != discordgo.ChannelTypeGuildPrivateThread {
		t.Errorf("expected private thread type (%d), got %d",
			discordgo.ChannelTypeGuildPrivateThread, api.gotTS.Type)
	}
}

func TestCreateThread_PublicDefault(t *testing.T) {
	api := &stubAPI{state: guildText("c"), created: &discordgo.Channel{ID: "t"}}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID: "c",
		Name:      "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.gotTS.Type != discordgo.ChannelTypeGuildPublicThread {
		t.Errorf("expected public thread type, got %d", api.gotTS.Type)
	}
}

func TestCreateThread_StateMissFallsBackToREST(t *testing.T) {
	api := &stubAPI{
		state:   nil, // state miss
		rest:    guildText("c"),
		created: &discordgo.Channel{ID: "t"},
	}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID: "c",
		Name:      "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !api.threadCalled {
		t.Fatal("expected ThreadStartComplex dispatch after REST fallback")
	}
}

func TestCreateThread_AutoArchiveDefault(t *testing.T) {
	api := &stubAPI{state: guildText("c"), created: &discordgo.Channel{ID: "t"}}
	_, err := createThread(context.Background(), api, channels.DiscordThreadParams{
		ChannelID: "c",
		Name:      "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.gotTS.AutoArchiveDuration != 1440 {
		t.Errorf("expected default 1440 minutes, got %d", api.gotTS.AutoArchiveDuration)
	}
}
