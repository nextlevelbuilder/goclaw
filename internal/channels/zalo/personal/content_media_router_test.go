package personal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
)

func TestContentRouterByMsgType(t *testing.T) {
	tests := []struct {
		name    string
		msgType string
		raw     string
		notify  string
		want    []string
	}{
		{"sticker", "chat.sticker", `{"id":"stk1"}`, "", []string{"[sticker id=stk1]"}},
		{"stickerId", "chat.sticker", `{"stickerId":"stk2"}`, "", []string{"[sticker id=stk2]"}},
		{"voice", "chat.voice", `{"duration":12.5}`, "", []string{"[voice 12.5s]"}},
		{"video", "chat.video", `{"duration":5,"thumb":"https://cdn/t.jpg"}`, "", []string{"[video 5s]", "[thumbnail] https://cdn/t.jpg"}},
		{"link", "share.link", `{"href":"https://example.com/a"}`, "", []string{"[link] https://example.com/a"}},
		{"location", "chat.location.new", `{"lat":10.5,"lng":106.25,"desc":"office"}`, "", []string{"[location 10.5,106.25] office"}},
		{"poll", "chat.poll", `{"question":"lunch?"}`, "", []string{"[zalo:chat.poll]", `"question":"lunch?"`}},
		{"card", "chat.card", ``, "", []string{"[zalo:chat.card]"}},
		{"unknown notify", "chat.mystery", `{"x":1}`, "new mystery message", []string{"new mystery message"}},
		{"unknown raw", "chat.mystery", `{"x":1}`, "", []string{"[unsupported: chat.mystery]", `"x":1`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := protocol.Content{}
			if tt.raw != "" {
				content.Raw = json.RawMessage(tt.raw)
			}
			got, media := extractContentAndMediaWithFetcher(content, tt.msgType, tt.notify, func(context.Context, string) (string, error) {
				t.Fatal("unexpected download")
				return "", nil
			})
			if len(media) != 0 {
				t.Fatalf("media = %v, want none", media)
			}
			if got == "" {
				t.Fatal("router returned empty content")
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("content = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestContentRouterPhotoPrefersOriURL(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(imagePath, []byte{0xFF, 0xD8, 0xFF, 0xE0}, 0644); err != nil {
		t.Fatal(err)
	}
	content := protocol.Content{Raw: json.RawMessage(`{
		"title":"photo caption",
		"href":"https://example.com/landing-page",
		"oriUrl":"https://cdn.example.com/jpg/photo.jpg"
	}`)}
	var fetched string
	got, media := extractContentAndMediaWithFetcher(content, "chat.photo", "", func(_ context.Context, url string) (string, error) {
		fetched = url
		return imagePath, nil
	})
	if fetched != "https://cdn.example.com/jpg/photo.jpg" {
		t.Fatalf("fetched = %q, want oriUrl", fetched)
	}
	if len(media) != 1 || media[0] != imagePath {
		t.Fatalf("media = %v", media)
	}
	if !strings.Contains(got, "<media:image>") || !strings.Contains(got, "photo caption") {
		t.Fatalf("content = %q, want media tag and caption", got)
	}
}

func TestContentRouterPhotoDownloadFailureIsNonEmpty(t *testing.T) {
	content := protocol.Content{Raw: json.RawMessage(`{"title":"brief.png","href":"https://example.com/brief.png"}`)}
	got, media := extractContentAndMediaWithFetcher(content, "chat.photo", "", func(context.Context, string) (string, error) {
		return "", os.ErrNotExist
	})
	if len(media) != 0 {
		t.Fatalf("media = %v, want none", media)
	}
	if !strings.Contains(got, "brief.png") {
		t.Fatalf("content = %q, want title fallback", got)
	}
}
