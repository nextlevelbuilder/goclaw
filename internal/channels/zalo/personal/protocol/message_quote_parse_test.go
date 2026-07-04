package protocol

import (
	"encoding/json"
	"testing"
)

// ParseAttachment must decode the quoted attachment's own JSON payload (not
// just render it as placeholder text) so callers can reach the image URL to
// download it — a reply to a photo needs the actual photo, not "[Quoted image]".
func TestQuoteAttachment_ParseAttachment(t *testing.T) {
	t.Run("image quote decodes to Attachment with URL", func(t *testing.T) {
		var q TQuote
		payload := `{"ownerId":"1","cliMsgId":"2","globalMsgId":"3","msg":"","attach":"{\"type\":\"image\",\"title\":\"photo.jpg\",\"href\":\"https://example.com/photo.jpg\"}","fromD":"Anh"}`
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			t.Fatalf("unmarshal TQuote: %v", err)
		}
		att := q.Attach.ParseAttachment()
		if att == nil {
			t.Fatal("ParseAttachment() = nil, want decoded Attachment")
		}
		if !att.IsImage() {
			t.Errorf("IsImage() = false, want true for %+v", att)
		}
		if att.URL() != "https://example.com/photo.jpg" {
			t.Errorf("URL() = %q, want https://example.com/photo.jpg", att.URL())
		}
	})

	t.Run("no attachment payload returns nil", func(t *testing.T) {
		var a QuoteAttachment
		if got := a.ParseAttachment(); got != nil {
			t.Errorf("ParseAttachment() = %+v, want nil for empty payload", got)
		}
	})

	t.Run("non-object payload (plain quoted text) returns nil", func(t *testing.T) {
		var a QuoteAttachment
		if err := json.Unmarshal([]byte(`"just a string"`), &a); err != nil {
			t.Fatalf("unmarshal QuoteAttachment: %v", err)
		}
		if got := a.ParseAttachment(); got != nil {
			t.Errorf("ParseAttachment() = %+v, want nil for plain-text payload", got)
		}
	})
}
