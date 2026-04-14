package whatsapp

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// --- extractTextContent ---

func TestExtractTextContent_Nil(t *testing.T) {
	got := extractTextContent(nil)
	if got != "" {
		t.Errorf("extractTextContent(nil) = %q, want empty", got)
	}
}

func TestExtractTextContent_Conversation(t *testing.T) {
	msg := &waE2E.Message{Conversation: proto.String("hello world")}
	got := extractTextContent(msg)
	if got != "hello world" {
		t.Errorf("extractTextContent(Conversation) = %q, want %q", got, "hello world")
	}
}

func TestExtractTextContent_ExtendedText(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("extended message"),
		},
	}
	got := extractTextContent(msg)
	if got != "extended message" {
		t.Errorf("extractTextContent(ExtendedText) = %q, want %q", got, "extended message")
	}
}

func TestExtractTextContent_ExtendedTextWithQuote(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("my reply"),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{
					Conversation: proto.String("original message"),
				},
			},
		},
	}
	got := extractTextContent(msg)
	if !strings.Contains(got, "[Replying to: original message]") {
		t.Errorf("expected reply context prefix, got: %q", got)
	}
	if !strings.Contains(got, "my reply") {
		t.Errorf("expected reply text in output, got: %q", got)
	}
}

func TestExtractTextContent_QuoteOnlyNoText(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(""),
			ContextInfo: &waE2E.ContextInfo{
				QuotedMessage: &waE2E.Message{
					Conversation: proto.String("quoted"),
				},
			},
		},
	}
	got := extractTextContent(msg)
	if got != "[Replying to: quoted]" {
		t.Errorf("extractTextContent(quote-only) = %q, want %q", got, "[Replying to: quoted]")
	}
}

func TestExtractTextContent_ImageCaption(t *testing.T) {
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Caption: proto.String("look at this photo"),
		},
	}
	got := extractTextContent(msg)
	if got != "look at this photo" {
		t.Errorf("extractTextContent(ImageCaption) = %q, want %q", got, "look at this photo")
	}
}

func TestExtractTextContent_VideoCaption(t *testing.T) {
	msg := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			Caption: proto.String("cool video"),
		},
	}
	got := extractTextContent(msg)
	if got != "cool video" {
		t.Errorf("extractTextContent(VideoCaption) = %q, want %q", got, "cool video")
	}
}

func TestExtractTextContent_DocumentCaption(t *testing.T) {
	msg := &waE2E.Message{
		DocumentMessage: &waE2E.DocumentMessage{
			Caption: proto.String("see this document"),
		},
	}
	got := extractTextContent(msg)
	if got != "see this document" {
		t.Errorf("extractTextContent(DocumentCaption) = %q, want %q", got, "see this document")
	}
}

func TestExtractTextContent_EmptyMessage(t *testing.T) {
	msg := &waE2E.Message{}
	got := extractTextContent(msg)
	if got != "" {
		t.Errorf("extractTextContent(empty) = %q, want empty", got)
	}
}

// --- extractQuotedText ---

func TestExtractQuotedText_Nil(t *testing.T) {
	got := extractQuotedText(nil)
	if got != "" {
		t.Errorf("extractQuotedText(nil) = %q, want empty", got)
	}
}

func TestExtractQuotedText_Conversation(t *testing.T) {
	msg := &waE2E.Message{Conversation: proto.String("quoted text")}
	got := extractQuotedText(msg)
	if got != "quoted text" {
		t.Errorf("extractQuotedText(Conversation) = %q, want %q", got, "quoted text")
	}
}

func TestExtractQuotedText_ExtendedText(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("extended quoted"),
		},
	}
	got := extractQuotedText(msg)
	if got != "extended quoted" {
		t.Errorf("extractQuotedText(ExtendedText) = %q, want %q", got, "extended quoted")
	}
}

func TestExtractQuotedText_ImageCaption(t *testing.T) {
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{Caption: proto.String("img caption")},
	}
	got := extractQuotedText(msg)
	if got != "img caption" {
		t.Errorf("extractQuotedText(ImageCaption) = %q, want %q", got, "img caption")
	}
}

func TestExtractQuotedText_VideoCaption(t *testing.T) {
	msg := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{Caption: proto.String("vid caption")},
	}
	got := extractQuotedText(msg)
	if got != "vid caption" {
		t.Errorf("extractQuotedText(VideoCaption) = %q, want %q", got, "vid caption")
	}
}

func TestExtractQuotedText_EmptyCaption(t *testing.T) {
	// Image with empty caption → falls through to empty.
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{Caption: proto.String("")},
	}
	got := extractQuotedText(msg)
	if got != "" {
		t.Errorf("extractQuotedText(empty caption) = %q, want empty", got)
	}
}

func TestExtractQuotedText_NoMatch(t *testing.T) {
	// Message with no text fields → empty.
	msg := &waE2E.Message{}
	got := extractQuotedText(msg)
	if got != "" {
		t.Errorf("extractQuotedText(no fields) = %q, want empty", got)
	}
}
