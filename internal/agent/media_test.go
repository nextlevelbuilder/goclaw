package agent

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestEnrichImageIDs_PreservesExistingTagAttributes(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: `see this <media:image url="https://cdn.discordapp.com/attachments/1/2/photo.jpg">`,
	}}
	refs := []providers.MediaRef{{
		ID:   "image-1",
		Kind: "image",
		Path: "/tmp/photo.jpg",
	}}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	got := messages[0].Content
	if !strings.Contains(got, `url="https://cdn.discordapp.com/attachments/1/2/photo.jpg"`) {
		t.Fatalf("expected url attribute to be preserved, got %q", got)
	}
	if !strings.Contains(got, `id="image-1"`) {
		t.Fatalf("expected id attribute to be added, got %q", got)
	}
	if !strings.Contains(got, `path="/tmp/photo.jpg"`) {
		t.Fatalf("expected path attribute to be added, got %q", got)
	}
}
