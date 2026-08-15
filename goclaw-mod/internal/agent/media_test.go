package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/media"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
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
	loop.enrichImageIDs(messages, refs, "/tmp")

	got := messages[0].Content
	want := `check <media:image id="img-1" path="a.jpg">`
	if got != want {
		t.Fatalf("bare tag enrichment:\n got %q\nwant %q", got, want)
	}
}

func TestEnrichInputMedia_DelegateRehomesCallerAttachment(t *testing.T) {
	callerWorkspace := t.TempDir()
	delegateWorkspace := t.TempDir()
	source := filepath.Join(callerWorkspace, "reference.jpg")
	if err := os.WriteFile(source, []byte("reference"), 0644); err != nil {
		t.Fatal(err)
	}

	req := &RunRequest{
		SessionKey: "delegate:parent:cppai-media:test",
		RunKind:    "delegate",
		Media: []bus.MediaFile{{
			Path:     source,
			MimeType: "image/jpeg",
			Filename: "reference.jpg",
		}},
	}
	messages := []providers.Message{{
		Role:    "user",
		Content: "Use this exact reference: " + source,
	}}
	ctx := tools.WithToolWorkspace(context.Background(), delegateWorkspace)

	loop := Loop{id: "cppai-media"}
	_, gotMessages, refs := loop.enrichInputMedia(ctx, req, messages)

	if len(refs) != 1 {
		t.Fatalf("refs = %#v, want one delegate-owned attachment", refs)
	}
	if !strings.HasPrefix(refs[0].Path, filepath.Join(delegateWorkspace, ".uploads")+string(filepath.Separator)) {
		t.Fatalf("persisted path = %q, want under delegate workspace %q", refs[0].Path, delegateWorkspace)
	}
	if strings.Contains(gotMessages[0].Content, source) {
		t.Fatalf("delegate message still exposes caller path: %q", gotMessages[0].Content)
	}
	logicalPath := logicalWorkspaceMediaPath(delegateWorkspace, refs[0].Path)
	if logicalPath == "" || !strings.Contains(gotMessages[0].Content, logicalPath) {
		t.Fatalf("delegate message = %q, want logical copied path %q", gotMessages[0].Content, logicalPath)
	}
	if strings.Contains(gotMessages[0].Content, delegateWorkspace) {
		t.Fatalf("delegate message leaked delegate workspace: %q", gotMessages[0].Content)
	}
	if !strings.Contains(gotMessages[0].Content, `<media:image`) {
		t.Fatalf("delegate message = %q, want explicit image attachment tag", gotMessages[0].Content)
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
	loop.enrichImageIDs(messages, refs, "/tmp")

	got := messages[0].Content
	if !strings.Contains(got, `url="https://cdn.discordapp.com/attachments/1/2/photo.jpg"`) {
		t.Fatalf("expected url attribute to be preserved, got %q", got)
	}
	if !strings.Contains(got, `id="image-1"`) {
		t.Fatalf("expected id attribute to be added, got %q", got)
	}
	if !strings.Contains(got, `path="photo.jpg"`) {
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
	loop.enrichImageIDs(messages, refs, "/new")

	if messages[0].Content != original {
		t.Fatalf("already-enriched tag should not be modified:\n got %q\nwant %q", messages[0].Content, original)
	}
}

func TestCollectRefsByKindOrdersOldestToNewestThenCurrent(t *testing.T) {
	messages := []providers.Message{
		{
			Role:      "user",
			Content:   "old",
			MediaRefs: []providers.MediaRef{{ID: "old-doc", Kind: "document"}},
		},
		{
			Role:      "user",
			Content:   "latest-history",
			MediaRefs: []providers.MediaRef{{ID: "latest-history-doc", Kind: "document"}},
		},
	}
	current := []providers.MediaRef{{ID: "current-doc", Kind: "document"}}

	got := collectRefsByKind(messages, current, "document")
	want := []string{"old-doc", "latest-history-doc", "current-doc"}

	if len(got) != len(want) {
		t.Fatalf("got %d refs, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i].ID, want[i])
		}
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

// TestEnrichImagePaths_UpgradesLegacyAbsolutePath verifies that historical
// host paths are replaced by the current logical workspace contract.
func TestEnrichImagePaths_UpgradesLegacyAbsolutePath(t *testing.T) {
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
	loop.enrichImagePaths(messages, "/workspace")

	want := `<media:image url="https://cdn.example.com/photo.jpg" id="img-1" path=".uploads/img-1.jpg">`
	if messages[0].Content != want {
		t.Fatalf("legacy path was not upgraded:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichImagePathsEscapesLogicalPathAttribute(t *testing.T) {
	workspace := t.TempDir()
	mediaPath := filepath.Join(workspace, ".uploads", `photo"quoted.jpg`)
	messages := []providers.Message{{
		Role:    "user",
		Content: `<media:image id="img-1" path="/legacy/photo.jpg">`,
		MediaRefs: []providers.MediaRef{{
			ID:   "img-1",
			Kind: "image",
			Path: mediaPath,
		}},
	}}

	var loop Loop
	loop.enrichImagePaths(messages, workspace)

	want := `<media:image id="img-1" path=".uploads/photo\"quoted.jpg">`
	if messages[0].Content != want {
		t.Fatalf("escaped path = %q, want %q", messages[0].Content, want)
	}
}

func TestLogicalWorkspaceMediaPathRejectsOutsideWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "agent")
	inside := filepath.Join(workspace, ".uploads", "photo.jpg")
	outside := filepath.Join(filepath.Dir(workspace), "other-agent", ".uploads", "photo.jpg")

	if got := logicalWorkspaceMediaPath(workspace, inside); got != ".uploads/photo.jpg" {
		t.Fatalf("inside path = %q, want %q", got, ".uploads/photo.jpg")
	}
	if got := logicalWorkspaceMediaPath(workspace, outside); got != "" {
		t.Fatalf("outside path = %q, want omitted", got)
	}
	if got := logicalWorkspaceMediaPath(workspace, "../escape.jpg"); got != "" {
		t.Fatalf("traversal path = %q, want omitted", got)
	}
}

func TestEnrichImagePathsStripsOutsideWorkspacePath(t *testing.T) {
	messages := []providers.Message{{
		Role:    "user",
		Content: `<media:image id="img-1" path="/workspace/other-agent/.uploads/photo.jpg">`,
		MediaRefs: []providers.MediaRef{{
			ID:   "img-1",
			Kind: "image",
			Path: "/workspace/other-agent/.uploads/photo.jpg",
		}},
	}}

	var loop Loop
	loop.enrichImagePaths(messages, "/workspace/current-agent")

	want := `<media:image id="img-1">`
	if messages[0].Content != want {
		t.Fatalf("outside workspace path was not stripped: got %q, want %q", messages[0].Content, want)
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
	loop.enrichImagePaths(messages, "/workspace")

	got := messages[0].Content
	if !strings.Contains(got, `path=".uploads/img-1.jpg"`) {
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
	loop.enrichImageIDs(messages, refs, "/tmp")

	want := `first <media:image id="img-a" path="a.jpg">` + "\n" + `second <media:image id="img-b" path="b.jpg">`
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
	loop.enrichImagePaths(messages, "/persisted")

	want := `first <media:image id="` + idA + `" path="a.jpg">` +
		"\n" + `second <media:image id="` + idB + `" path="b.jpg">`
	if messages[0].Content != want {
		t.Fatalf("enrichImagePaths() content = %q, want %q", messages[0].Content, want)
	}
}

func TestEnrichDocumentPaths_MultipleRefs(t *testing.T) {
	workspace := t.TempDir()
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:document>\nsecond <media:document>",
	}}
	refs := []providers.MediaRef{
		{ID: "doc-a", Kind: "document", Path: filepath.Join(workspace, ".uploads", "a.pdf")},
		{ID: "doc-b", Kind: "document", Path: filepath.Join(workspace, ".uploads", "b.pdf")},
	}

	var loop Loop
	loop.enrichDocumentPaths(messages, refs, workspace)

	want := `first <media:document id="doc-a" path=".uploads/a.pdf">` + "\n" +
		`second <media:document id="doc-b" path=".uploads/b.pdf">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichDocumentPathsStripsOutsideWorkspacePath(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "current-agent")
	outside := filepath.Join(filepath.Dir(workspace), "other-agent", ".uploads", "secret.pdf")
	messages := []providers.Message{{
		Role:    "user",
		Content: `<media:document name="secret.pdf" path="/legacy/secret.pdf">`,
	}}
	refs := []providers.MediaRef{{
		ID:   "doc-1",
		Kind: "document",
		Path: outside,
	}}

	var loop Loop
	loop.enrichDocumentPaths(messages, refs, workspace)

	want := `<media:document name="secret.pdf" id="doc-1">`
	if messages[0].Content != want {
		t.Fatalf("outside workspace path was not stripped: got %q, want %q", messages[0].Content, want)
	}
}

func TestEnrichDocumentPathsUpgradesHistoricalAbsolutePath(t *testing.T) {
	workspace := t.TempDir()
	docPath := filepath.Join(workspace, ".uploads", "history.pdf")
	messages := []providers.Message{
		{
			Role:    "user",
			Content: `<media:document id="doc-old" path="/legacy/history.pdf">`,
			MediaRefs: []providers.MediaRef{{
				ID:   "doc-old",
				Kind: "document",
				Path: docPath,
			}},
		},
		{Role: "user", Content: "follow up"},
	}

	var loop Loop
	loop.enrichDocumentPaths(messages, nil, workspace)

	want := `<media:document id="doc-old" path=".uploads/history.pdf">`
	if messages[0].Content != want {
		t.Fatalf("historical path was not upgraded: got %q, want %q", messages[0].Content, want)
	}
}

func TestEnrichAudioIDs_MultipleRefs(t *testing.T) {
	workspace := t.TempDir()
	messages := []providers.Message{{
		Role:    "user",
		Content: "first <media:audio>\nsecond <media:audio>",
	}}
	refs := []providers.MediaRef{
		{ID: "aud-a", Kind: "audio", Path: filepath.Join(workspace, ".uploads", "a.mp3")},
		{ID: "aud-b", Kind: "audio", Path: filepath.Join(workspace, ".uploads", "b.mp3")},
	}

	var loop Loop
	loop.enrichAudioIDs(messages, refs, workspace)

	want := `first <media:audio id="aud-a" path=".uploads/a.mp3">` + "\n" +
		`second <media:audio id="aud-b" path=".uploads/b.mp3">`
	if messages[0].Content != want {
		t.Fatalf("multi-ref alignment:\n got %q\nwant %q", messages[0].Content, want)
	}
}

func TestEnrichAudioIDsUpgradesHistoricalLogicalPath(t *testing.T) {
	workspace := t.TempDir()
	audioPath := filepath.Join(workspace, ".uploads", "history.mp3")
	messages := []providers.Message{
		{
			Role:    "user",
			Content: `<media:voice id="aud-old" path="/legacy/history.mp3">`,
			MediaRefs: []providers.MediaRef{{
				ID:   "aud-old",
				Kind: "audio",
				Path: audioPath,
			}},
		},
		{Role: "user", Content: "follow up"},
	}

	var loop Loop
	loop.enrichAudioIDs(messages, nil, workspace)

	want := `<media:voice id="aud-old" path=".uploads/history.mp3">`
	if messages[0].Content != want {
		t.Fatalf("historical audio path was not upgraded: got %q, want %q", messages[0].Content, want)
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
	loop.enrichImageIDs(messages, refs, "/tmp")

	want := `only one <media:image id="img-a" path="a.jpg">`
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
	loop.enrichImageIDs(messages, refs, "/tmp")

	want := `first <media:image id="img-a" path="a.jpg">` + "\n" + `second <media:image>`
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
	loop.enrichAudioIDs(messages, refs, "")

	want := `hear this <media:audio id="aud-1">` + "\n" + `and this <media:voice id="aud-2">`
	if messages[0].Content != want {
		t.Fatalf("mixed audio/voice:\n got %q\nwant %q", messages[0].Content, want)
	}
}
