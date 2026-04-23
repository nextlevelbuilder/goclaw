package discord

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// Test_Intents_bitmask_includes_voice_states is a regression guard (plan
// issue 3D). The intents bitmask is a contract with Discord's gateway —
// removing any of the original three kills text handling; failing to
// include the voice-state intents silently breaks voice transcription.
//
// This test asserts the union of intents the feature depends on, without
// opening a real session. If a future refactor reshuffles the bitmask,
// this test forces an explicit decision rather than a silent regression.
func Test_Intents_bitmask_includes_voice_states(t *testing.T) {
	ch, err := New(
		config.DiscordConfig{Enabled: true, Token: "test-token"},
		bus.New(),
		nil, nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got := ch.session.Identify.Intents
	required := []struct {
		name string
		v    discordgo.Intent
	}{
		{"IntentsGuildMessages", discordgo.IntentsGuildMessages},
		{"IntentsDirectMessages", discordgo.IntentsDirectMessages},
		{"IntentsMessageContent", discordgo.IntentsMessageContent},
		{"IntentsGuilds", discordgo.IntentsGuilds},
		{"IntentsGuildVoiceStates", discordgo.IntentsGuildVoiceStates},
	}
	for _, r := range required {
		if got&r.v != r.v {
			t.Errorf("intents bitmask missing %s (0x%x); got 0x%x", r.name, uint32(r.v), uint32(got))
		}
	}
}
