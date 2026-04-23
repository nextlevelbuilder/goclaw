package discord

import (
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// (chunkForDiscord test removed — trySendViaInteraction now delegates to
// channels.ChunkMarkdown which has its own coverage in internal/channels/chunking_test.go.)

// TestEphemeralSuppressesFallback exercises the security-critical guard that
// prevents a /ask private:true reply from leaking into a public guild channel
// when the interaction-token reply path fails or expires. The matrix must be
// exact: any drift here turns "the private reply was lost" into "the private
// reply was posted publicly."
func TestEphemeralSuppressesFallback(t *testing.T) {
	tests := []struct {
		name string
		echo *interactionEcho
		want bool
	}{
		{"nil echo → no suppression", nil, false},
		{"public in guild → fall through OK", &interactionEcho{Ephemeral: false, GuildID: "g1"}, false},
		{"public in DM → fall through OK", &interactionEcho{Ephemeral: false, GuildID: ""}, false},
		// Ephemeral in DM: the DM channel is already 1:1 private, so posting
		// there doesn't leak. Allow fallback.
		{"ephemeral in DM → fall through OK", &interactionEcho{Ephemeral: true, GuildID: ""}, false},
		// Ephemeral in guild: this is the leak we're guarding against.
		{"ephemeral in guild → suppress", &interactionEcho{Ephemeral: true, GuildID: "g1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ephemeralSuppressesFallback(tt.echo); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSweepInteractionTokens verifies the background cleanup drops expired
// entries and leaves fresh ones intact. Without this sweeper the map grows
// monotonically on every slash invocation whose agent run never triggers Send.
func TestSweepInteractionTokens(t *testing.T) {
	c := &Channel{}

	fresh := &interactionEcho{CreatedAt: time.Now()}
	old := &interactionEcho{CreatedAt: time.Now().Add(-discordInteractionTTL - time.Minute)}
	c.interactionTokens.Store("fresh", fresh)
	c.interactionTokens.Store("old", old)
	c.interactionTokens.Store("bogus", "not-an-echo") // hostile entry shouldn't crash the sweep

	c.sweepInteractionTokens()

	if _, ok := c.interactionTokens.Load("fresh"); !ok {
		t.Error("fresh entry should survive the sweep")
	}
	if _, ok := c.interactionTokens.Load("old"); ok {
		t.Error("expired entry should be dropped")
	}
	// Non-*interactionEcho values are ignored (not dropped, not panicking).
	// The sweep is a safety net, not a full garbage collector.
	if _, ok := c.interactionTokens.Load("bogus"); !ok {
		t.Error("non-echo entry should be ignored, not dropped")
	}
}

func TestInteractionEchoExpiry(t *testing.T) {
	fresh := &interactionEcho{CreatedAt: time.Now()}
	if fresh.expired() {
		t.Error("fresh echo should not be expired")
	}

	old := &interactionEcho{CreatedAt: time.Now().Add(-15 * time.Minute)}
	if !old.expired() {
		t.Error("15-minute-old echo should be expired")
	}

	// 14 min + safety margin — should count as expired per the 14-min cap
	// (1-min safety below the 15-min Discord limit).
	edge := &interactionEcho{CreatedAt: time.Now().Add(-14*time.Minute - time.Second)}
	if !edge.expired() {
		t.Error("14m+ echo should be expired (we keep a 1-min safety margin)")
	}
}

func TestAgentBackedCommands(t *testing.T) {
	// Guard the set — any future reshuffle of meta vs agent-backed should be
	// deliberate. Changing this test forces the author to think about which
	// commands defer their ACK (agent-backed) vs respond inline (meta/direct).
	want := map[SlashCommandName]bool{
		SlashCommandAsk:       true,
		SlashCommandRecall:    true,
		SlashCommandSummarize: true,
	}
	for k, v := range want {
		if agentBackedCommands[k] != v {
			t.Errorf("agentBackedCommands[%s] = %v, want %v", k, agentBackedCommands[k], v)
		}
	}
	for k, v := range agentBackedCommands {
		if !want[k] && v {
			t.Errorf("agentBackedCommands[%s] = true but not in expected set", k)
		}
	}
}

func TestOptionString(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "prompt", Value: "hello"},
			{Name: "private", Value: true},
		},
	}
	if got := optionString(data, "prompt"); got != "hello" {
		t.Errorf("prompt = %q, want hello", got)
	}
	if got := optionString(data, "missing"); got != "" {
		t.Errorf("missing = %q, want empty", got)
	}
	// Wrong type should fall back to empty, not panic.
	if got := optionString(data, "private"); got != "" {
		t.Errorf("bool option read as string = %q, want empty", got)
	}
}

func TestOptionBool(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "private", Value: true},
		},
	}
	if !optionBool(data, "private") {
		t.Error("private option should be true")
	}
	if optionBool(data, "missing") {
		t.Error("missing option should default to false")
	}
}

func TestOptionInt(t *testing.T) {
	data := discordgo.ApplicationCommandInteractionData{
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{Name: "count", Value: float64(42)}, // discordgo decodes JSON numbers to float64
			{Name: "explicit_int", Value: int(7)},
		},
	}
	if got := optionInt(data, "count"); got != 42 {
		t.Errorf("count = %d, want 42", got)
	}
	if got := optionInt(data, "explicit_int"); got != 7 {
		t.Errorf("explicit_int = %d, want 7", got)
	}
	if got := optionInt(data, "missing"); got != 0 {
		t.Errorf("missing = %d, want 0", got)
	}
}

func TestBuildAgentPrompt(t *testing.T) {
	c := &Channel{}

	t.Run("ask with prompt", func(t *testing.T) {
		data := discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "prompt", Value: "what time is it"},
			},
		}
		prompt, ephemeral, ok := c.buildAgentPrompt(SlashCommandAsk, data, nil)
		if !ok || prompt != "what time is it" || ephemeral {
			t.Errorf("ask: got (%q, %v, %v), want (\"what time is it\", false, true)", prompt, ephemeral, ok)
		}
	})

	t.Run("ask with private flag", func(t *testing.T) {
		data := discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "prompt", Value: "secret"},
				{Name: "private", Value: true},
			},
		}
		prompt, ephemeral, ok := c.buildAgentPrompt(SlashCommandAsk, data, nil)
		if !ok || !ephemeral || prompt != "secret" {
			t.Errorf("ask ephemeral: got (%q, %v, %v)", prompt, ephemeral, ok)
		}
	})

	t.Run("recall", func(t *testing.T) {
		data := discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "query", Value: "vacation plans"},
			},
		}
		prompt, ephemeral, ok := c.buildAgentPrompt(SlashCommandRecall, data, nil)
		if !ok || ephemeral {
			t.Errorf("recall: ok=%v ephemeral=%v", ok, ephemeral)
		}
		if !strings.Contains(prompt, "vacation plans") {
			t.Errorf("recall prompt missing query: %q", prompt)
		}
	})

	t.Run("summarize default count", func(t *testing.T) {
		data := discordgo.ApplicationCommandInteractionData{Options: nil}
		prompt, _, ok := c.buildAgentPrompt(SlashCommandSummarize, data, nil)
		if !ok || !strings.Contains(prompt, "last 20 messages") {
			t.Errorf("summarize default = %q", prompt)
		}
	})

	t.Run("summarize caps at 200", func(t *testing.T) {
		data := discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "count", Value: float64(999)},
			},
		}
		prompt, _, ok := c.buildAgentPrompt(SlashCommandSummarize, data, nil)
		if !ok || !strings.Contains(prompt, "last 200 messages") {
			t.Errorf("summarize cap = %q", prompt)
		}
	})
}

// TestRoutingMetaKeysIncludeInteractionToken ensures the interaction-reply
// metadata keys survive the copyRoutingMeta hop. Without this, outbound
// dispatch strips the token and /ask replies fall back to channel posts —
// breaking the inline-reply UX silently.
func TestRoutingMetaKeysIncludeInteractionToken(t *testing.T) {
	src := map[string]string{
		"discord_interaction_token": "abc",
		"discord_interaction_id":    "123",
		"discord_interaction_appid": "app",
		"discord_interaction_flags": "ephemeral",
	}
	got := channels.CopyFinalRoutingMeta(src)
	for k, v := range src {
		if got[k] != v {
			t.Errorf("key %q not preserved: got %q, want %q", k, got[k], v)
		}
	}
}
