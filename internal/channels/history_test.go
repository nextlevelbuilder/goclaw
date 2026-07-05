package channels

import "testing"

func TestSetNameSyncsPersistentHistoryChannelName(t *testing.T) {
	base := NewBaseChannel(TypeDiscord, nil, nil)
	history := &PendingHistory{channelName: TypeDiscord}
	base.SetGroupHistory(history)

	base.SetName("co-assistant-2-0")

	if history.channelName != "co-assistant-2-0" {
		t.Fatalf("history channelName = %q, want %q", history.channelName, "co-assistant-2-0")
	}
}

func TestSetGroupHistorySyncsExistingChannelName(t *testing.T) {
	base := NewBaseChannel(TypeDiscord, nil, nil)
	base.SetName("co-assistant-2-0")
	history := &PendingHistory{channelName: TypeDiscord}

	base.SetGroupHistory(history)

	if history.channelName != "co-assistant-2-0" {
		t.Fatalf("history channelName = %q, want %q", history.channelName, "co-assistant-2-0")
	}
}
