package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

// TestEnrichImageIDs_BareTag verifies enrichment of a bare <media:image> tag
// (non-Discord channels where SourceURL is empty).
func TestEnrichImageIDs_BareTag(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: `check <media:image>`,
	}}
	refs := []providers.MediaRef{{ID: "img-1", Kind: "image", Path: "/tmp/a.jpg"}}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	got := messages[0].Content
	want := `check <media:image id="img-1" path="/tmp/a.jpg">`
	if got != want {
		t.Fatalf("bare tag enrichment:\n got %q\nwant %q", got, want)
	}
}

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

// TestEnrichImageIDs_SkipsAlreadyEnriched ensures tags with id are not re-enriched
// (historical messages from prior turns should not be double-modified).
func TestEnrichImageIDs_SkipsAlreadyEnriched(t *testing.T) {
	original := `<media:image url="https://cdn.example.com/photo.jpg" id="old-id" path="/old/path.jpg">`
	messages := []providers.Message{{
		Role:    "user",
		Content: original,
	}}
	refs := []providers.MediaRef{{ID: "new-id", Kind: "image", Path: "/new/path.jpg"}}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	if messages[0].Content != original {
		t.Fatalf("already-enriched tag should not be modified:\n got %q\nwant %q", messages[0].Content, original)
	}
}

// testMediaStore creates a temporary media.Store for tests.
func testMediaStore(t *testing.T) *media.Store {
	t.Helper()
	s, err := media.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestEnrichImagePaths_NoDoubleEnrich verifies that historical messages with
// url+id+path are not re-enriched on subsequent turns.
func TestEnrichImagePaths_NoDoubleEnrich(t *testing.T) {
	original := `<media:image url="https://cdn.example.com/photo.jpg" id="img-1" path="/workspace/.uploads/img-1.jpg">`
	messages := []providers.Message{{
		Role:    "user",
		Content: original,
		MediaRefs: []providers.MediaRef{{
			ID:   "img-1",
			Kind: "image",
			Path: "/workspace/.uploads/img-1.jpg",
		}},
	}}

	loop := Loop{mediaStore: testMediaStore(t)}
	loop.enrichImagePaths(messages)

	if messages[0].Content != original {
		t.Fatalf("double-enrichment detected:\n got %q\nwant %q", messages[0].Content, original)
	}
}

// TestEnrichImagePaths_AttributeOrderIndependence verifies that enrichImagePaths
// correctly finds the id attribute regardless of attribute order in the tag.
func TestEnrichImagePaths_AttributeOrderIndependence(t *testing.T) {
	// url comes before id — old code would fail because it only matched <media:image id=... at tag start.
	messages := []providers.Message{{
		Role:    "user",
		Content: `<media:image url="https://cdn.example.com/photo.jpg" id="img-1">`,
		MediaRefs: []providers.MediaRef{{
			ID:   "img-1",
			Kind: "image",
			Path: "/workspace/.uploads/img-1.jpg",
		}},
	}}

	loop := Loop{mediaStore: testMediaStore(t)}
	loop.enrichImagePaths(messages)

	got := messages[0].Content
	if !strings.Contains(got, `path="/workspace/.uploads/img-1.jpg"`) {
		t.Fatalf("expected path to be added regardless of attribute order, got %q", got)
	}
	if !strings.Contains(got, `url="https://cdn.example.com/photo.jpg"`) {
		t.Fatalf("expected url to be preserved, got %q", got)
	}
	if !strings.Contains(got, `id="img-1"`) {
		t.Fatalf("expected id to be preserved, got %q", got)
	}
}

func TestEnrichImageIDs_MultipleRefs(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:image>\nsecond <media:image>",
	}}
	refs := []providers.MediaRef{
		{ID: "img-a", Kind: "image", Path: "/tmp/a.jpg"},
		{ID: "img-b", Kind: "image", Path: "/tmp/b.jpg"},
	}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	want := `first <media:image id="img-a" path="/tmp/a.jpg">` + "\n" + `second <media:image id="img-b" path="/tmp/b.jpg">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichImagePaths_MultipleRefsKeepTagAlignment(t *testing.T) {
	storeDir := t.TempDir()
	mediaStore, err := media.NewStore(storeDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	const (
		sessionKey = "session-1"
		pathA      = "/persisted/a.jpg"
		pathB      = "/persisted/b.jpg"
	)

	srcA := filepath.Join(storeDir, "a.jpg")
	if err := os.WriteFile(srcA, []byte("a"), 0644); err != nil {
		t.Fatalf("WriteFile(a) error = %v", err)
	}
	idA, _, err := mediaStore.SaveFile(sessionKey, srcA, "image/jpeg")
	if err != nil {
		t.Fatalf("SaveFile(a) error = %v", err)
	}

	srcB := filepath.Join(storeDir, "b.jpg")
	if err := os.WriteFile(srcB, []byte("b"), 0644); err != nil {
		t.Fatalf("WriteFile(b) error = %v", err)
	}
	idB, _, err := mediaStore.SaveFile(sessionKey, srcB, "image/jpeg")
	if err != nil {
		t.Fatalf("SaveFile(b) error = %v", err)
	}

	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:image>\nsecond <media:image>",
		MediaRefs: []providers.MediaRef{
			{ID: idA, Kind: "image", Path: pathA},
			{ID: idB, Kind: "image", Path: pathB},
		},
	}}

	var loop Loop
	loop.mediaStore = mediaStore
	loop.enrichImagePaths(messages)

	want := `first <media:image id="` + idA + `" path="` + pathA + `">` +
		"\n" + `second <media:image id="` + idB + `" path="` + pathB + `">`
	if messages[0].Content != want {
		t.Fatalf("enrichImagePaths() content = %q, want %q", messages[0].Content, want)
	}
}

func TestEnrichDocumentPaths_MultipleRefs(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:document>\nsecond <media:document>",
	}}
	refs := []providers.MediaRef{
		{ID: "doc-a", Kind: "document", Path: "/tmp/a.pdf"},
		{ID: "doc-b", Kind: "document", Path: "/tmp/b.pdf"},
	}

	var loop Loop
	loop.enrichDocumentPaths(messages, refs)

	want := `first <media:document path="/tmp/a.pdf">` + "\n" + `second <media:document path="/tmp/b.pdf">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichAudioIDs_MultipleRefs(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:audio>\nsecond <media:audio>",
	}}
	refs := []providers.MediaRef{
		{ID: "aud-a", Kind: "audio"},
		{ID: "aud-b", Kind: "audio"},
	}

	var loop Loop
	loop.enrichAudioIDs(messages, refs)

	want := `first <media:audio id="aud-a">` + "\n" + `second <media:audio id="aud-b">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichVideoIDs_MultipleRefs(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:video>\nsecond <media:video>",
	}}
	refs := []providers.MediaRef{
		{ID: "vid-a", Kind: "video"},
		{ID: "vid-b", Kind: "video"},
	}

	var loop Loop
	loop.enrichVideoIDs(messages, refs)

	want := `first <media:video id="vid-a">` + "\n" + `second <media:video id="vid-b">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

// TestEnrichImageIDs_MoreRefsThanTags verifies that extra refs are silently skipped.
func TestEnrichImageIDs_MoreRefsThanTags(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "only one <media:image>",
	}}
	refs := []providers.MediaRef{
		{ID: "img-a", Kind: "image", Path: "/tmp/a.jpg"},
		{ID: "img-b", Kind: "image", Path: "/tmp/b.jpg"},
	}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	want := `only one <media:image id="img-a" path="/tmp/a.jpg">`
	if messages[0].Content != want {
		t.Fatalf("more refs than tags:\n got %q\nwant %q", messages[0].Content, want)
	}
}

// TestEnrichImageIDs_MoreTagsThanRefs verifies that extra tags are left bare.
func TestEnrichImageIDs_MoreTagsThanRefs(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:image>\nsecond <media:image>",
	}}
	refs := []providers.MediaRef{
		{ID: "img-a", Kind: "image", Path: "/tmp/a.jpg"},
	}

	var loop Loop
	loop.enrichImageIDs(messages, refs)

	want := `first <media:image id="img-a" path="/tmp/a.jpg">` + "\n" + `second <media:image>`
	if messages[0].Content != want {
		t.Fatalf("more tags than refs:\n got %q\nwant %q", messages[0].Content, want)
	}
}

// TestEnrichAudioIDs_MixedAudioAndVoice verifies correct pairing when both
// <media:audio> and <media:voice> tags appear in the same message.
func TestEnrichAudioIDs_MixedAudioAndVoice(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: "hear this <media:audio>\nand this <media:voice>",
	}}
	refs := []providers.MediaRef{
		{ID: "aud-1", Kind: "audio"},
		{ID: "aud-2", Kind: "audio"},
	}

	var loop Loop
	loop.enrichAudioIDs(messages, refs)

	want := `hear this <media:audio id="aud-1">` + "\n" + `and this <media:voice id="aud-2">`
	if messages[0].Content != want {
		t.Fatalf("mixed audio/voice:\n got %q\nwant %q", messages[0].Content, want)
	}
}
