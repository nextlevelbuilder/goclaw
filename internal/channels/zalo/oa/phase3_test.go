package oa

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBuildTextBody covers the v3.0 message/cs text shape with quote.
func TestBuildTextBody(t *testing.T) {
	body := buildTextBody("u1", "hello", "mid-1")
	want := `{"message":{"quote_message_id":"mid-1","text":"hello"},"recipient":{"user_id":"u1"}}`
	if got := mustJSON(t, body); got != want {
		t.Errorf("buildTextBody = %s, want %s", got, want)
	}

	body = buildTextBody("u1", "hello", "")
	want = `{"message":{"text":"hello"},"recipient":{"user_id":"u1"}}`
	if got := mustJSON(t, body); got != want {
		t.Errorf("buildTextBody no-quote = %s, want %s", got, want)
	}
}

// TestBuildMediaAttachmentBody covers the template/media shape (image+gif).
func TestBuildMediaAttachmentBody(t *testing.T) {
	body := buildMediaAttachmentBody("u1", "image", "att-1")
	want := `{"message":{"attachment":{"payload":{"elements":[{"attachment_id":"att-1","media_type":"image"}],"template_type":"media"},"type":"template"}},"recipient":{"user_id":"u1"}}`
	if got := mustJSON(t, body); got != want {
		t.Errorf("buildMediaAttachmentBody = %s, want %s", got, want)
	}
}

// TestBuildFileAttachmentBody covers the plain type=file shape.
func TestBuildFileAttachmentBody(t *testing.T) {
	body := buildFileAttachmentBody("u1", "att-2")
	want := `{"message":{"attachment":{"payload":{"attachment_id":"att-2"},"type":"file"}},"recipient":{"user_id":"u1"}}`
	if got := mustJSON(t, body); got != want {
		t.Errorf("buildFileAttachmentBody = %s, want %s", got, want)
	}
}

// TestParseUploadAttachmentID covers attachment_id, legacy token, and error.
func TestParseUploadAttachmentID(t *testing.T) {
	id, err := parseUploadAttachmentID([]byte(`{"data":{"attachment_id":"a1"}}`))
	if err != nil || id != "a1" {
		t.Errorf("attachment_id: got %q, %v", id, err)
	}
	id, err = parseUploadAttachmentID([]byte(`{"data":{"token":"t1"}}`))
	if err != nil || id != "t1" {
		t.Errorf("legacy token: got %q, %v", id, err)
	}
	if _, err := parseUploadAttachmentID([]byte(`{"data":{}}`)); err == nil {
		t.Error("empty response must error")
	}
	if _, err := parseUploadAttachmentID([]byte(`not json`)); err == nil {
		t.Error("malformed response must error")
	}
}

// TestSanitizeFilename covers traversal, dot-only, and length caps.
func TestSanitizeFilename(t *testing.T) {
	if got := sanitizeFilename("../../etc/passwd"); got != "passwd" {
		t.Errorf("traversal = %q, want passwd", got)
	}
	if got := sanitizeFilename(".."); got == ".." || got == "" {
		t.Errorf("dot-dot = %q, want safe fallback", got)
	}
	if got := sanitizeFilename(""); got == "" {
		t.Errorf("empty = %q, want safe fallback", got)
	}
	var long strings.Builder
	for range 300 {
		long.WriteString("a")
	}
	long.WriteString(".pdf")
	got := sanitizeFilename(long.String())
	if len(got) > maxFilenameLen {
		t.Errorf("long name len = %d, want <= %d", len(got), maxFilenameLen)
	}
}

// TestIsZaloSupportedFileMIME covers the PDF/DOC/DOCX whitelist.
func TestIsZaloSupportedFileMIME(t *testing.T) {
	for _, m := range []string{"application/pdf", "application/msword", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "APPLICATION/PDF"} {
		if !isZaloSupportedFileMIME(m) {
			t.Errorf("%q must be supported", m)
		}
	}
	for _, m := range []string{"text/plain", "application/zip", "", "image/png"} {
		if isZaloSupportedFileMIME(m) {
			t.Errorf("%q must NOT be supported", m)
		}
	}
}

// TestMergeTrailingText covers caption+content joins.
func TestMergeTrailingText(t *testing.T) {
	if got := mergeTrailingText("cap", "body"); got != "cap\n\nbody" {
		t.Errorf("both = %q", got)
	}
	if got := mergeTrailingText("", "body"); got != "body" {
		t.Errorf("content only = %q", got)
	}
	if got := mergeTrailingText("cap", ""); got != "cap" {
		t.Errorf("caption only = %q", got)
	}
	if got := mergeTrailingText("", ""); got != "" {
		t.Errorf("empty = %q", got)
	}
}

func TestSequentialTrailingText_ContentFollowsLastAttachment(t *testing.T) {
	// Sequential Send: captions ride with their attachment; content trails the last item.
	items := []struct {
		caption string
		last    bool
	}{
		{caption: "one", last: false},
		{caption: "two", last: true},
	}
	content := "hello"
	var trailings []string
	for _, item := range items {
		if item.last {
			trailings = append(trailings, mergeTrailingText(item.caption, content))
			continue
		}
		trailings = append(trailings, item.caption)
	}
	if trailings[0] != "one" {
		t.Errorf("first trailing = %q, want caption only", trailings[0])
	}
	if trailings[1] != "two\n\nhello" {
		t.Errorf("last trailing = %q, want caption+content", trailings[1])
	}
}

// TestParseMessageResponse covers the send envelope.
func TestParseMessageResponse(t *testing.T) {
	mid, err := parseMessageResponse([]byte(`{"error":0,"data":{"message_id":"m1","recipient_id":"r1"}}`))
	if err != nil || mid != "m1" {
		t.Errorf("got %q, %v", mid, err)
	}
	if _, err := parseMessageResponse([]byte(`{"data":{}}`)); err != nil {
		// empty message_id is valid (no error) — parser only fails on bad JSON
		t.Errorf("empty data must not error: %v", err)
	}
	if _, err := parseMessageResponse([]byte(`oops`)); err == nil {
		t.Error("malformed must error")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}
