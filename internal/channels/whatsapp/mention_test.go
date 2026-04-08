package whatsapp

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestIsMentioned(t *testing.T) {
	// Helper to build an events.Message with mentioned JIDs in extended text.
	makeEvt := func(mentionedJIDs []string) *events.Message {
		return &events.Message{
			Message: &waE2E.Message{
				ExtendedTextMessage: &waE2E.ExtendedTextMessage{
					Text: proto.String("hello @bot"),
					ContextInfo: &waE2E.ContextInfo{
						MentionedJID: mentionedJIDs,
					},
				},
			},
		}
	}

	tests := []struct {
		name    string
		myJID   string // bot's phone JID
		myLID   string // bot's LID
		mentions []string
		want    bool
	}{
		{
			name:     "mentioned by phone JID",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "mentioned by LID",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210@lid"},
			want:     true,
		},
		{
			name:     "mentioned by JID with device suffix",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"1234567890:42@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "mentioned by LID with device suffix",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210:5@lid"},
			want:     true,
		},
		{
			name:     "dual identity — mentioned via LID when JID also set",
			myJID:    "1234567890@s.whatsapp.net",
			myLID:    "9876543210@lid",
			mentions: []string{"9876543210@lid"},
			want:     true,
		},
		{
			name:     "dual identity — mentioned via JID when LID also set",
			myJID:    "1234567890@s.whatsapp.net",
			myLID:    "9876543210@lid",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     true,
		},
		{
			name:     "not mentioned — different user",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{"9999999999@s.whatsapp.net"},
			want:     false,
		},
		{
			name:     "not mentioned — empty mentions",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: []string{},
			want:     false,
		},
		{
			name:     "unknown identity — fail closed",
			myJID:    "",
			myLID:    "",
			mentions: []string{"1234567890@s.whatsapp.net"},
			want:     false,
		},
		{
			name:     "no extended text message",
			myJID:    "1234567890@s.whatsapp.net",
			mentions: nil, // will use plain conversation message
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &Channel{}

			// Set bot identity.
			if tt.myJID != "" {
				jid, _ := types.ParseJID(tt.myJID)
				ch.myJID = jid
			}
			if tt.myLID != "" {
				lid, _ := types.ParseJID(tt.myLID)
				ch.myLID = lid
			}

			var evt *events.Message
			if tt.mentions == nil {
				// Plain conversation message — no extended text.
				evt = &events.Message{
					Message: &waE2E.Message{
						Conversation: proto.String("hello"),
					},
				}
			} else {
				evt = makeEvt(tt.mentions)
			}

			got := ch.isMentioned(evt)
			if got != tt.want {
				t.Errorf("isMentioned() = %v, want %v", got, tt.want)
			}
		})
	}
}
