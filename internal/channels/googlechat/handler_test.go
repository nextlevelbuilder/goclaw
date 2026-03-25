package googlechat

import "testing"

func TestIsBotMentioned_WithBotMention(t *testing.T) {
	event := &SpaceEvent{
		Message: &GCMessage{
			Annotations: []GCAnnotation{
				{
					Type: "USER_MENTION",
					UserMention: &GCUserMention{
						User: &GCUser{Type: "BOT"},
					},
				},
			},
		},
	}
	if !isBotMentioned(event) {
		t.Fatal("expected bot to be mentioned")
	}
}

func TestIsBotMentioned_WithHumanMention(t *testing.T) {
	event := &SpaceEvent{
		Message: &GCMessage{
			Annotations: []GCAnnotation{
				{
					Type: "USER_MENTION",
					UserMention: &GCUserMention{
						User: &GCUser{Type: "HUMAN"},
					},
				},
			},
		},
	}
	if isBotMentioned(event) {
		t.Fatal("expected bot NOT to be mentioned")
	}
}

func TestIsBotMentioned_NilMessage(t *testing.T) {
	event := &SpaceEvent{}
	if isBotMentioned(event) {
		t.Fatal("expected false for nil message")
	}
}

func TestIsBotMentioned_NoAnnotations(t *testing.T) {
	event := &SpaceEvent{Message: &GCMessage{}}
	if isBotMentioned(event) {
		t.Fatal("expected false for no annotations")
	}
}

func TestExtractSenderInfo_FromMessageSender(t *testing.T) {
	event := &SpaceEvent{
		User: &GCUser{Name: "users/111", DisplayName: "EventUser"},
		Message: &GCMessage{
			Sender: &GCUser{Name: "users/222", DisplayName: "MsgSender"},
		},
	}
	id, name := extractSenderInfo(event)
	if id != "users/222" {
		t.Fatalf("unexpected senderID: %s", id)
	}
	if name != "MsgSender" {
		t.Fatalf("unexpected name: %s", name)
	}
}

func TestExtractSenderInfo_FallbackToEventUser(t *testing.T) {
	event := &SpaceEvent{
		User:    &GCUser{Name: "users/111", DisplayName: "EventUser"},
		Message: &GCMessage{},
	}
	id, name := extractSenderInfo(event)
	if id != "users/111" {
		t.Fatalf("unexpected senderID: %s", id)
	}
	if name != "EventUser" {
		t.Fatalf("unexpected name: %s", name)
	}
}

func TestExtractSenderInfo_NilUser(t *testing.T) {
	event := &SpaceEvent{}
	id, name := extractSenderInfo(event)
	if id != "" || name != "" {
		t.Fatalf("expected empty strings, got id=%q name=%q", id, name)
	}
}

func TestExtractSenderInfo_NoDisplayName(t *testing.T) {
	event := &SpaceEvent{
		User: &GCUser{Name: "users/111"},
	}
	id, name := extractSenderInfo(event)
	// When no display name, senderID is just the resource name
	if id != "users/111" {
		t.Fatalf("unexpected senderID: %s", id)
	}
	if name != "" {
		t.Fatalf("expected empty name, got %q", name)
	}
}
