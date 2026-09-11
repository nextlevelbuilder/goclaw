package tokencount

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestCountMessages_ChargesFlatUnitPerStructuredMedia pins the flat media unit
// that both inline and out-of-band media are charged. Counting media as text
// would scale with payload size and make any real video exceed every window.
func TestCountMessages_ChargesFlatUnitPerStructuredMedia(t *testing.T) {
	counter := NewBudgetCounter()
	text := []providers.Message{{Role: "user", Content: "describe this"}}

	base, err := counter.CountMessages(text)
	if err != nil {
		t.Fatalf("CountMessages(text) returned error: %v", err)
	}

	withVideo := []providers.Message{{
		Role:    "user",
		Content: "describe this",
		Videos: []providers.VideoContent{
			{MimeType: "video/mp4", URL: "https://example.com/clip.mp4"},
		},
	}}
	got, err := counter.CountMessages(withVideo)
	if err != nil {
		t.Fatalf("CountMessages(video) returned error: %v", err)
	}
	if got-base != InlineMediaUnit {
		t.Fatalf("one video item cost %d tokens, want %d", got-base, InlineMediaUnit)
	}

	withTwo := []providers.Message{{
		Role:    "user",
		Content: "describe this",
		Videos: []providers.VideoContent{
			{MimeType: "video/mp4", URL: "https://example.com/a.mp4"},
			{MimeType: "video/mp4", URL: "https://example.com/b.mp4"},
		},
	}}
	gotTwo, err := counter.CountMessages(withTwo)
	if err != nil {
		t.Fatalf("CountMessages(two videos) returned error: %v", err)
	}
	if gotTwo-base != 2*InlineMediaUnit {
		t.Fatalf("two video items cost %d tokens, want %d", gotTwo-base, 2*InlineMediaUnit)
	}
}
