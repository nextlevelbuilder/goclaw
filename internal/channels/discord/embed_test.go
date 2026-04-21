package discord

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

type fakeEmbedAPI struct {
	called  bool
	gotCID  string
	gotData *discordgo.MessageSend
	result  *discordgo.Message
	err     error
}

func (f *fakeEmbedAPI) channelMessageSendComplex(_ context.Context, channelID string, data *discordgo.MessageSend) (*discordgo.Message, error) {
	f.called = true
	f.gotCID = channelID
	f.gotData = data
	if f.err != nil {
		return nil, f.err
	}
	if f.result == nil {
		return &discordgo.Message{ID: "m1", ChannelID: channelID}, nil
	}
	return f.result, nil
}

func basicEmbed() channels.DiscordEmbed {
	return channels.DiscordEmbed{Title: "Hello", Description: "world"}
}

func TestSendEmbed_RequiresChannelID(t *testing.T) {
	f := &fakeEmbedAPI{}
	_, err := sendEmbed(context.Background(), f, channels.DiscordSendEmbedParams{
		Embeds: []channels.DiscordEmbed{basicEmbed()},
	})
	if err == nil || !strings.Contains(err.Error(), "channel_id") {
		t.Fatalf("expected channel_id error, got %v", err)
	}
	if f.called {
		t.Fatal("API should not be called when channel_id missing")
	}
}

func TestSendEmbed_RequiresAtLeastOneEmbed(t *testing.T) {
	f := &fakeEmbedAPI{}
	_, err := sendEmbed(context.Background(), f, channels.DiscordSendEmbedParams{ChannelID: "c1"})
	if err == nil || !strings.Contains(err.Error(), "at least one embed") {
		t.Fatalf("expected 'at least one embed' error, got %v", err)
	}
}

func TestSendEmbed_TooManyEmbeds(t *testing.T) {
	embeds := make([]channels.DiscordEmbed, embedMaxPerMessage+1)
	for i := range embeds {
		embeds[i] = basicEmbed()
	}
	_, err := sendEmbed(context.Background(), &fakeEmbedAPI{}, channels.DiscordSendEmbedParams{
		ChannelID: "c1",
		Embeds:    embeds,
	})
	if err == nil || !strings.Contains(err.Error(), "too many embeds") {
		t.Fatalf("expected too-many-embeds error, got %v", err)
	}
}

func TestSendEmbed_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		embed channels.DiscordEmbed
		want  string
	}{
		{"title too long", channels.DiscordEmbed{Title: strings.Repeat("a", embedTitleMax+1)}, "title exceeds"},
		{"description too long", channels.DiscordEmbed{Description: strings.Repeat("a", embedDescriptionMax+1)}, "description exceeds"},
		{"footer without text", channels.DiscordEmbed{Title: "t", Footer: &channels.DiscordEmbedFooter{IconURL: "https://x"}}, "footer.text is required"},
		{"image without url", channels.DiscordEmbed{Title: "t", Image: &channels.DiscordEmbedMedia{}}, "image.url is required"},
		{"thumbnail without url", channels.DiscordEmbed{Title: "t", Thumbnail: &channels.DiscordEmbedMedia{}}, "thumbnail.url is required"},
		{"author without name", channels.DiscordEmbed{Title: "t", Author: &channels.DiscordEmbedAuthor{URL: "https://x"}}, "author.name is required"},
		{"bad timestamp", channels.DiscordEmbed{Title: "t", Timestamp: "not-a-time"}, "timestamp must be ISO 8601"},
		{"too many fields", channels.DiscordEmbed{Title: "t", Fields: makeFields(embedMaxFields + 1)}, "too many fields"},
		{"field missing name", channels.DiscordEmbed{Title: "t", Fields: []channels.DiscordEmbedField{{Value: "v"}}}, "fields[0].name is required"},
		{"field missing value", channels.DiscordEmbed{Title: "t", Fields: []channels.DiscordEmbedField{{Name: "n"}}}, "fields[0].value is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeEmbedAPI{}
			_, err := sendEmbed(context.Background(), f, channels.DiscordSendEmbedParams{
				ChannelID: "c1",
				Embeds:    []channels.DiscordEmbed{tc.embed},
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if f.called {
				t.Fatal("API should not be called when validation fails")
			}
		})
	}
}

func TestSendEmbed_TotalCharCap(t *testing.T) {
	// Two embeds, each ~3500 chars of description — together they exceed 6000.
	big := strings.Repeat("x", 3500)
	_, err := sendEmbed(context.Background(), &fakeEmbedAPI{}, channels.DiscordSendEmbedParams{
		ChannelID: "c1",
		Embeds: []channels.DiscordEmbed{
			{Description: big},
			{Description: big},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "combined embed text exceeds") {
		t.Fatalf("expected combined-cap error, got %v", err)
	}
}

func TestSendEmbed_ContentTooLong(t *testing.T) {
	_, err := sendEmbed(context.Background(), &fakeEmbedAPI{}, channels.DiscordSendEmbedParams{
		ChannelID: "c1",
		Content:   strings.Repeat("x", messageContentMax+1),
		Embeds:    []channels.DiscordEmbed{basicEmbed()},
	})
	if err == nil || !strings.Contains(err.Error(), "content exceeds") {
		t.Fatalf("expected content-too-long error, got %v", err)
	}
}

func TestSendEmbed_FullConversion(t *testing.T) {
	f := &fakeEmbedAPI{}
	res, err := sendEmbed(context.Background(), f, channels.DiscordSendEmbedParams{
		ChannelID: "c1",
		Content:   "above text",
		ReplyTo:   "m0",
		Embeds: []channels.DiscordEmbed{
			{
				Title:       "Status",
				Description: "All systems go.",
				URL:         "https://example.com",
				Color:       0x2ECC71,
				Timestamp:   "2026-04-21T15:04:05Z",
				Author:      &channels.DiscordEmbedAuthor{Name: "ci", URL: "https://example.com/ci", IconURL: "https://example.com/ci.png"},
				Footer:      &channels.DiscordEmbedFooter{Text: "build #42", IconURL: "https://example.com/logo.png"},
				Image:       &channels.DiscordEmbedMedia{URL: "https://example.com/banner.png"},
				Thumbnail:   &channels.DiscordEmbedMedia{URL: "https://example.com/thumb.png"},
				Fields: []channels.DiscordEmbedField{
					{Name: "env", Value: "prod", Inline: true},
					{Name: "duration", Value: "12s", Inline: true},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.MessageID != "m1" || res.ChannelID != "c1" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if !f.called {
		t.Fatal("API was not called")
	}
	if f.gotCID != "c1" {
		t.Errorf("channel id = %q, want c1", f.gotCID)
	}
	if f.gotData.Content != "above text" {
		t.Errorf("content = %q", f.gotData.Content)
	}
	if f.gotData.Reference == nil || f.gotData.Reference.MessageID != "m0" {
		t.Errorf("reply reference not set correctly: %+v", f.gotData.Reference)
	}
	if len(f.gotData.Embeds) != 1 {
		t.Fatalf("embeds count = %d", len(f.gotData.Embeds))
	}
	got := f.gotData.Embeds[0]
	if got.Title != "Status" || got.Description != "All systems go." || got.Color != 0x2ECC71 {
		t.Errorf("basic fields not copied: %+v", got)
	}
	if got.Author == nil || got.Author.Name != "ci" {
		t.Error("author not copied")
	}
	if got.Footer == nil || got.Footer.Text != "build #42" {
		t.Error("footer not copied")
	}
	if got.Image == nil || got.Image.URL != "https://example.com/banner.png" {
		t.Error("image not copied")
	}
	if got.Thumbnail == nil || got.Thumbnail.URL != "https://example.com/thumb.png" {
		t.Error("thumbnail not copied")
	}
	if len(got.Fields) != 2 || got.Fields[0].Name != "env" || !got.Fields[0].Inline {
		t.Errorf("fields not copied: %+v", got.Fields)
	}
}

func TestSendEmbed_APIError(t *testing.T) {
	f := &fakeEmbedAPI{err: errors.New("HTTP 403: Missing Permissions")}
	_, err := sendEmbed(context.Background(), f, channels.DiscordSendEmbedParams{
		ChannelID: "c1",
		Embeds:    []channels.DiscordEmbed{basicEmbed()},
	})
	if err == nil || !strings.Contains(err.Error(), "Missing Permissions") {
		t.Fatalf("expected wrapped API error, got %v", err)
	}
}

func makeFields(n int) []channels.DiscordEmbedField {
	out := make([]channels.DiscordEmbedField, n)
	for i := range out {
		out[i] = channels.DiscordEmbedField{Name: "n", Value: "v"}
	}
	return out
}
