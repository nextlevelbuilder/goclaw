package bitrix24

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestSanitizeQuotedText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n\t ", ""},
		{"plain", "báo giá cho anh nhé", "báo giá cho anh nhé"},
		{"user mention", "[USER=62]Đặng Văn Tình[/USER] xong chưa", "@Đặng Văn Tình (ID:62) xong chưa"},
		{"strip formatting", "[b]quan trọng[/b] [i]lắm[/i]", "quan trọng lắm"},
		{"strip quote+context", "[quote][CONTEXT=chat123]cũ[/quote] mới", "cũ mới"},
		{"collapse whitespace", "dòng 1\n\n  dòng 2", "dòng 1 dòng 2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeQuotedText(tc.in); got != tc.want {
				t.Errorf("sanitizeQuotedText(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeQuotedText_Truncates(t *testing.T) {
	long := strings.Repeat("ă", quotedTextMaxRunes+50) // multibyte on purpose
	got := sanitizeQuotedText(long)
	gotRunes := []rune(got)
	// quotedTextMaxRunes content runes + 1 ellipsis rune.
	if len(gotRunes) != quotedTextMaxRunes+1 {
		t.Fatalf("truncated len = %d runes; want %d", len(gotRunes), quotedTextMaxRunes+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated text should end with ellipsis, got %q", got[len(got)-6:])
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abc", 5); got != "abc" {
		t.Errorf("no truncation expected, got %q", got)
	}
	if got := truncateRunes("abcdef", 3); got != "abc…" {
		t.Errorf("truncateRunes = %q; want %q", got, "abc…")
	}
}

func TestQuotedKindVN(t *testing.T) {
	cases := map[string]string{
		"image": "hình ảnh",
		"audio": "tin nhắn thoại",
		"video": "video",
		"file":  "tệp",
		"":      "tệp",
		"WEIRD": "tệp",
	}
	for in, want := range cases {
		if got := quotedKindVN(in); got != want {
			t.Errorf("quotedKindVN(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestQuotedMediaNote(t *testing.T) {
	single := quotedMediaNote([]EventFile{{Name: "voice.ogg", Type: "audio"}}, nil, "Tình")
	if want := "[Đang trả lời một tin nhắn thoại \"voice.ogg\" của Tình]\n"; single != want {
		t.Errorf("single = %q; want %q", single, want)
	}
	noName := quotedMediaNote([]EventFile{{Type: "image"}}, nil, "Tình")
	if want := "[Đang trả lời một hình ảnh của Tình]\n"; noName != want {
		t.Errorf("noName = %q; want %q", noName, want)
	}
	multi := quotedMediaNote([]EventFile{{Name: "a"}, {Name: "b"}}, nil, "Tình")
	if want := "[Đang trả lời 2 tệp đính kèm của Tình]\n"; multi != want {
		t.Errorf("multi = %q; want %q", multi, want)
	}
}

// Regression: im.dialog.messages.get returns blank type/name for voice notes,
// so the note must derive the kind (and filename) from the downloaded MIME
// instead of falling back to the generic "tệp".
func TestQuotedMediaNote_PrefersDownloadedMIME(t *testing.T) {
	files := []EventFile{{ID: "32728"}} // blank Type + Name (as messages.get returned)
	downloaded := []bus.MediaFile{{MimeType: "audio/x-m4a", Filename: "voice.m4a"}}
	got := quotedMediaNote(files, downloaded, "Đình Chinh")
	if want := "[Đang trả lời một tin nhắn thoại \"voice.m4a\" của Đình Chinh]\n"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}

	// Blank downloaded filename → kind still upgraded, no filename shown.
	got2 := quotedMediaNote([]EventFile{{ID: "1"}}, []bus.MediaFile{{MimeType: "image/png"}}, "Tình")
	if want := "[Đang trả lời một hình ảnh của Tình]\n"; got2 != want {
		t.Errorf("got2 %q; want %q", got2, want)
	}
}

func TestMediaKindVN(t *testing.T) {
	cases := map[string]string{
		"audio/x-m4a":     "tin nhắn thoại",
		"image/png":       "hình ảnh",
		"video/mp4":       "video",
		"application/pdf": "tệp",
		"":                "tệp",
	}
	for mime, want := range cases {
		if got := mediaKindVN(mime); got != want {
			t.Errorf("mediaKindVN(%q) = %q; want %q", mime, got, want)
		}
	}
}

func TestExtractFileIDs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"array", `{"FILE_ID":[32728,40]}`, []string{"32728", "40"}},
		{"scalar", `{"FILE_ID":32728}`, []string{"32728"}},
		{"string ids", `{"FILE_ID":["a","b"]}`, []string{"a", "b"}},
		{"filters zero", `{"FILE_ID":[0,32728]}`, []string{"32728"}},
		{"absent", `{"TEXT":"hi"}`, nil},
		{"empty params array", `[]`, nil},
		{"empty raw", ``, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFileIDs(json.RawMessage(tc.in))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("extractFileIDs(%s) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseReplyFiles(t *testing.T) {
	// Realistic im.dialog.messages.get result: numeric ids, files keyed by id.
	result := `{
		"messages": [
			{"id": 315968, "author_id": 62, "params": {"FILE_ID": [32728]}, "text": ""},
			{"id": 315969, "author_id": 62, "text": "câu hỏi"}
		],
		"files": {
			"32728": {"id": 32728, "type": "audio", "name": "voice.ogg", "size": 6789}
		}
	}`
	files, err := parseReplyFiles(json.RawMessage(result), "315968")
	if err != nil {
		t.Fatalf("parseReplyFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("want 1 file, got %d: %+v", len(files), files)
	}
	if files[0].ID != "32728" || files[0].Name != "voice.ogg" || files[0].Type != "audio" || files[0].Size != 6789 {
		t.Errorf("file mismatch: %+v", files[0])
	}
}

func TestParseReplyFiles_NoMatchingMessage(t *testing.T) {
	result := `{"messages":[{"id":999,"params":{"FILE_ID":[1]}}],"files":{"1":{"name":"x"}}}`
	files, err := parseReplyFiles(json.RawMessage(result), "315968")
	if err != nil {
		t.Fatalf("parseReplyFiles: %v", err)
	}
	if files != nil {
		t.Errorf("want nil (no message matched id), got %+v", files)
	}
}

func TestParseReplyFiles_MissingFileMetadata_FallsBackToBareID(t *testing.T) {
	// FILE_ID present on the message but no files map entry → still return the id
	// so downloadEventFiles can fetch by id (imbot.v2.File.download needs only id).
	result := `{"messages":[{"id":315968,"params":{"FILE_ID":[32728]}}],"files":[]}`
	files, err := parseReplyFiles(json.RawMessage(result), "315968")
	if err != nil {
		t.Fatalf("parseReplyFiles: %v", err)
	}
	if len(files) != 1 || files[0].ID != "32728" || files[0].Name != "" {
		t.Errorf("want bare-id file, got %+v", files)
	}
}

func TestParseReplyFiles_EmptyResult(t *testing.T) {
	if _, err := parseReplyFiles(json.RawMessage(``), "1"); err == nil {
		t.Error("want error on empty result")
	}
}

// replyMessagesHandler stubs im.dialog.messages.get, recording FIRST_ID and
// returning the supplied result object.
func replyMessagesHandler(t *testing.T, calls *atomic.Int32, gotFirstID *string, result any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/rest/im.dialog.messages.get.json") {
			t.Errorf("unexpected REST path: %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls.Add(1)
		_ = r.ParseForm()
		if gotFirstID != nil {
			*gotFirstID = r.Form.Get("FIRST_ID")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result})
	}
}

func TestFetchReplyMessageFiles_HappyPath(t *testing.T) {
	var calls atomic.Int32
	var firstID string
	result := map[string]any{
		"messages": []map[string]any{
			{"id": 315968, "params": map[string]any{"FILE_ID": []int{32728}}},
		},
		"files": map[string]any{
			"32728": map[string]any{"id": 32728, "type": "audio", "name": "voice.ogg", "size": 6789},
		},
	}
	srv := httptest.NewServer(replyMessagesHandler(t, &calls, &firstID, result))
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)

	files, err := bc.fetchReplyMessageFiles(context.Background(), "chat1234", "315968")
	if err != nil {
		t.Fatalf("fetchReplyMessageFiles: %v", err)
	}
	// FIRST_ID must be msgID-1 so the quoted message itself is returned.
	if firstID != "315967" {
		t.Errorf("FIRST_ID = %q; want 315967", firstID)
	}
	if len(files) != 1 || files[0].Name != "voice.ogg" || files[0].Type != "audio" {
		t.Fatalf("files mismatch: %+v", files)
	}
}

func TestFetchReplyMessageFiles_NilClient(t *testing.T) {
	bc := &Channel{}
	if _, err := bc.fetchReplyMessageFiles(context.Background(), "chat1", "1"); err == nil {
		t.Error("want error when no REST client bound")
	}
}

func TestFetchReplyMessageFiles_EmptyArgs(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(replyMessagesHandler(t, &calls, nil, map[string]any{}))
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)

	if _, err := bc.fetchReplyMessageFiles(context.Background(), "", "1"); err == nil {
		t.Error("want error on empty dialog id")
	}
	if _, err := bc.fetchReplyMessageFiles(context.Background(), "chat1", ""); err == nil {
		t.Error("want error on empty msg id")
	}
	if calls.Load() != 0 {
		t.Errorf("should not call REST for empty args, calls=%d", calls.Load())
	}
}

func TestResolveReplyContext_NotAReply(t *testing.T) {
	bc := &Channel{}
	evt := &Event{Params: EventParams{ReplyMessage: nil}}
	prefix, media := bc.resolveReplyContext(context.Background(), evt)
	if prefix != "" || media != nil {
		t.Errorf("non-reply should be no-op, got prefix=%q media=%v", prefix, media)
	}
}

func TestResolveReplyContext_TextBranch(t *testing.T) {
	var calls atomic.Int32
	// Author id 62 resolves via user.get to a display name for the note.
	srv := httptest.NewServer(userGetHandler(t, &calls, map[string]userGetRaw{
		"62": {ID: "62", Name: "Đặng", LastName: "Tình"},
	}))
	defer srv.Close()
	bc := newChannelWithBoundPortal(t, srv)

	evt := &Event{Params: EventParams{
		DialogID:     "chat1234",
		ReplyMessage: &EventReplyMessage{ID: "42", AuthorID: "62", Text: "[b]báo giá[/b] chưa em"},
	}}
	prefix, media := bc.resolveReplyContext(context.Background(), evt)
	if media != nil {
		t.Errorf("text branch should not fetch media, got %v", media)
	}
	if !strings.Contains(prefix, "báo giá chưa em") {
		t.Errorf("prefix missing sanitized quote: %q", prefix)
	}
	if !strings.HasPrefix(prefix, "[Đang trả lời tin của ") {
		t.Errorf("prefix format wrong: %q", prefix)
	}
	if !strings.Contains(prefix, "Đặng Tình") {
		t.Errorf("prefix should carry resolved author name: %q", prefix)
	}
}
