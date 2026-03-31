package discord

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/bwmarrin/discordgo"

	sharedmedia "github.com/nextlevelbuilder/goclaw/internal/channels/media"
)

func TestResolveMediaPreservesSourceURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()

	att := &discordgo.MessageAttachment{
		URL:         server.URL + "/photo.jpg?token=abc",
		Filename:    "photo.jpg",
		ContentType: "image/jpeg",
		Size:        len("image-bytes"),
	}

	items := resolveMedia([]*discordgo.MessageAttachment{att}, int64(att.Size)+1)
	if len(items) != 1 {
		t.Fatalf("resolveMedia() returned %d items, want 1", len(items))
	}
	t.Cleanup(func() {
		_ = os.Remove(items[0].FilePath)
	})

	if items[0].Type != sharedmedia.TypeImage {
		t.Fatalf("resolveMedia() type = %q, want %q", items[0].Type, sharedmedia.TypeImage)
	}
	if items[0].SourceURL != att.URL {
		t.Fatalf("resolveMedia() source url = %q, want %q", items[0].SourceURL, att.URL)
	}
}
