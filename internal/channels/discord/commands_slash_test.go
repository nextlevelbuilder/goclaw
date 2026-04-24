package discord

import (
	"context"
	"errors"
	"testing"

	"github.com/cartridge-gg/discordgo"
)

type fakeSlashAPI struct {
	gotAppID   string
	gotGuildID string
	gotCmds    []*discordgo.ApplicationCommand
	returnCmds []*discordgo.ApplicationCommand
	err        error
}

func (f *fakeSlashAPI) applicationCommandsBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand) ([]*discordgo.ApplicationCommand, error) {
	f.gotAppID = appID
	f.gotGuildID = guildID
	f.gotCmds = commands
	if f.err != nil {
		return nil, f.err
	}
	if f.returnCmds != nil {
		return f.returnCmds, nil
	}
	return commands, nil
}

// TestDefaultSlashCommands is a characterization test that pins the command
// set. Deliberately checks BOTH the full list of names AND a couple of
// deep-structural expectations (required options, choice values) so schema
// drift shows up as a test failure, not a silent Discord UX regression.
func TestDefaultSlashCommands(t *testing.T) {
	cmds := DefaultSlashCommands()

	// The set is intentionally minimal: /reset, /stop, and /thread are
	// registered only once their backing dependencies land (see comments on
	// the SlashCommandName constants). Adding a command here without wiring
	// the handler in handler_interaction.go is a UX regression — every user
	// who clicks the slash gets "Unknown command" — so the test guards it.
	want := []SlashCommandName{
		SlashCommandAsk,
		SlashCommandStatus,
		SlashCommandHelp,
		SlashCommandRecall,
		SlashCommandSummarize,
	}
	if len(cmds) != len(want) {
		t.Fatalf("command count = %d, want %d", len(cmds), len(want))
	}
	for i, w := range want {
		if SlashCommandName(cmds[i].Name) != w {
			t.Errorf("cmds[%d].Name = %q, want %q", i, cmds[i].Name, w)
		}
		if cmds[i].Description == "" {
			t.Errorf("cmds[%d] (%s) has empty description", i, cmds[i].Name)
		}
	}

	// /ask must require a prompt and accept an optional private flag.
	ask := cmds[0]
	if len(ask.Options) != 2 {
		t.Fatalf("/ask option count = %d, want 2", len(ask.Options))
	}
	if ask.Options[0].Name != "prompt" || !ask.Options[0].Required {
		t.Errorf("/ask first option must be required 'prompt', got %+v", ask.Options[0])
	}
	if ask.Options[1].Name != "private" || ask.Options[1].Required {
		t.Errorf("/ask second option must be optional 'private', got %+v", ask.Options[1])
	}

	// /summarize must expose a count option with a max value so the LLM can't
	// trivially blow past Discord's context budget by passing a huge number.
	var summarize *discordgo.ApplicationCommand
	for _, c := range cmds {
		if c.Name == string(SlashCommandSummarize) {
			summarize = c
			break
		}
	}
	if summarize == nil {
		t.Fatal("/summarize command missing")
	}
	if len(summarize.Options) != 1 || summarize.Options[0].Name != "count" {
		t.Fatalf("/summarize option not 'count': %+v", summarize.Options)
	}
	if summarize.Options[0].MaxValue != 200 {
		t.Errorf("/summarize count MaxValue = %v, want 200", summarize.Options[0].MaxValue)
	}
}

func TestSyncSlashCommands_BulkOverwriteCalled(t *testing.T) {
	f := &fakeSlashAPI{}
	cmds := DefaultSlashCommands()
	if err := syncSlashCommands(context.Background(), f, "app-1", "", cmds); err != nil {
		t.Fatalf("syncSlashCommands: %v", err)
	}
	if f.gotAppID != "app-1" {
		t.Errorf("app id = %q, want app-1", f.gotAppID)
	}
	if f.gotGuildID != "" {
		t.Errorf("guild id should be empty for global registration, got %q", f.gotGuildID)
	}
	if len(f.gotCmds) != len(cmds) {
		t.Errorf("passed %d commands, want %d", len(f.gotCmds), len(cmds))
	}
}

func TestSyncSlashCommands_GuildScoped(t *testing.T) {
	f := &fakeSlashAPI{}
	if err := syncSlashCommands(context.Background(), f, "app-1", "guild-xyz", DefaultSlashCommands()); err != nil {
		t.Fatalf("syncSlashCommands: %v", err)
	}
	if f.gotGuildID != "guild-xyz" {
		t.Errorf("guild id = %q, want guild-xyz", f.gotGuildID)
	}
}

func TestSyncSlashCommands_MissingAppID(t *testing.T) {
	err := syncSlashCommands(context.Background(), &fakeSlashAPI{}, "", "", DefaultSlashCommands())
	if err == nil {
		t.Fatal("expected error when app id missing")
	}
}

func TestSyncSlashCommands_APIError(t *testing.T) {
	f := &fakeSlashAPI{err: errors.New("HTTP 401")}
	err := syncSlashCommands(context.Background(), f, "app-1", "", DefaultSlashCommands())
	if err == nil {
		t.Fatal("expected error propagation from API")
	}
}

func TestSyncSlashCommands_BulkOverwriteClearsStale(t *testing.T) {
	// The bulk-overwrite semantics are what give us "automatic cleanup of
	// stale commands from the old backend." Verify the API receives the
	// exact list we pass — whatever was previously registered gets replaced.
	f := &fakeSlashAPI{}
	cmds := []*discordgo.ApplicationCommand{{Name: "only", Description: "desc"}}
	if err := syncSlashCommands(context.Background(), f, "app-1", "", cmds); err != nil {
		t.Fatalf("syncSlashCommands: %v", err)
	}
	if len(f.gotCmds) != 1 || f.gotCmds[0].Name != "only" {
		t.Errorf("commands passed to API = %+v, want exactly [only]", f.gotCmds)
	}
}
