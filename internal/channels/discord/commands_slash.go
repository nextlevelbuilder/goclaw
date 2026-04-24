package discord

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/cartridge-gg/discordgo"
)

// SlashCommandName is a canonical identifier for each command we register.
// Exported so handler_interaction.go can switch on them without stringly-typed
// duplication and tests can reference them directly.
type SlashCommandName string

const (
	SlashCommandAsk       SlashCommandName = "ask"
	SlashCommandStatus    SlashCommandName = "status"
	SlashCommandHelp      SlashCommandName = "help"
	SlashCommandRecall    SlashCommandName = "recall"
	SlashCommandSummarize SlashCommandName = "summarize"
	// SlashCommandReset, SlashCommandStop, and SlashCommandThread are
	// intentionally not registered in this PR:
	//   - /reset needs a SessionClearer dependency injected from the gateway
	//     to actually clear the agent's view of the conversation. Shipping
	//     the command as a stub that says "not wired yet" would be a worse
	//     UX than not showing it at all.
	//   - /stop needs a scheduler-cancel hook on channels.Manager to cancel
	//     the in-flight run. Clearing the typing indicator alone leaves the
	//     agent running, so a naive stub misleads the user.
	//   - /thread wraps the create_discord_thread tool, which lives in a
	//     separate upstream PR. We keep this PR self-contained against
	//     upstream/dev.
	// Each lands in its own follow-up once the backing dependency is
	// available. Until then they stay as constants only — not registered,
	// not routed.
	SlashCommandReset  SlashCommandName = "reset"
	SlashCommandStop   SlashCommandName = "stop"
	SlashCommandThread SlashCommandName = "thread"
)

// DefaultSlashCommands returns the command set we register on every goclaw
// Discord bot. Edit this list when adding or removing commands — Start() calls
// ApplicationCommandsBulkOverwrite with the result, so anything not in this
// list is automatically removed from Discord on the next sidecar boot.
//
// Command categories (see handler_interaction.go for routing):
//   - meta: ask (routes to agent), status, help
//   - agent-backed tools: recall (memory_search), summarize (sessions_history)
//
// All fields are plain types so Discord's slash-command param UI can render
// them; commands that conceptually take richer structured input (e.g.
// send_discord_embed) are intentionally NOT surfaced as slash commands — they
// stay agent-only.
func DefaultSlashCommands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{
		{
			Name:        string(SlashCommandAsk),
			Description: "Ask the agent anything. Routes your prompt through the full agent pipeline.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "prompt",
					Description: "Your question or request",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Name:        "private",
					Description: "Reply only visible to you (default: false — reply visible to the channel)",
					Required:    false,
				},
			},
		},
		{
			Name:        string(SlashCommandStatus),
			Description: "Show what the agent is doing right now in this channel (idle, thinking, running a tool).",
		},
		{
			Name:        string(SlashCommandHelp),
			Description: "List the commands this bot supports and what each one does.",
		},
		{
			Name:        string(SlashCommandRecall),
			Description: "Search the agent's memory for relevant prior context and summarize the hits.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "query",
					Description: "What to search memory for",
					Required:    true,
				},
			},
		},
		{
			Name:        string(SlashCommandSummarize),
			Description: "Summarize recent messages in this channel or thread.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionInteger,
					Name:        "count",
					Description: "How many recent messages to summarize (default 20, max 200)",
					Required:    false,
					MinValue:    pointerTo(1.0),
					MaxValue:    200,
				},
			},
		},
	}
}

// pointerTo is a tiny helper so the slash-command literals above stay terse.
// discordgo's MinValue / MinLength are pointer types to distinguish "unset"
// from "zero"; without this helper every use site needs a named variable.
func pointerTo[T any](v T) *T { return &v }

// slashCommandAPI abstracts the discordgo surface SyncSlashCommands uses so
// tests can stub the overwrite call. "" guildID means a global registration
// (all servers the bot is in + DMs; ~1hr propagation delay); a non-empty
// guildID registers per-guild which is instant but only visible in that guild.
type slashCommandAPI interface {
	applicationCommandsBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand) ([]*discordgo.ApplicationCommand, error)
}

type sessionSlashCommandAPI struct{ s *discordgo.Session }

func (a sessionSlashCommandAPI) applicationCommandsBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand) ([]*discordgo.ApplicationCommand, error) {
	return a.s.ApplicationCommandBulkOverwrite(appID, guildID, commands)
}

// SyncSlashCommands registers DefaultSlashCommands() with Discord via
// ApplicationCommandBulkOverwrite. The bulk-overwrite call replaces the
// entire command list on the application atomically — stale commands from a
// previous backend are automatically removed because they aren't in the new
// list.
//
// guildID "" performs a global registration (visible everywhere, ~1hr
// propagation). A non-empty guildID registers per-guild (instant, for dev).
func (c *Channel) SyncSlashCommands(ctx context.Context) error {
	if c.applicationID == "" {
		return fmt.Errorf("slash commands: application ID not yet resolved (Start must run first)")
	}
	return syncSlashCommands(ctx, sessionSlashCommandAPI{s: c.session}, c.applicationID, c.testGuildID, DefaultSlashCommands())
}

func syncSlashCommands(_ context.Context, api slashCommandAPI, appID, guildID string, commands []*discordgo.ApplicationCommand) error {
	if appID == "" {
		return fmt.Errorf("slash commands: application ID is required")
	}
	registered, err := api.applicationCommandsBulkOverwrite(appID, guildID, commands)
	if err != nil {
		return fmt.Errorf("slash commands: bulk overwrite: %w", err)
	}
	scope := "global"
	if guildID != "" {
		scope = "guild=" + guildID
	}
	slog.Info("discord: slash commands synced",
		"scope", scope,
		"count", len(registered),
		"wanted", len(commands),
	)
	return nil
}

// startSlashCommandSync launches SyncSlashCommands on a goroutine with a
// bounded retry loop. Mirrors Telegram's SyncMenuCommands retry pattern
// (internal/channels/telegram/channel.go:223-248) — 3 attempts, linear
// backoff, so a transient Discord 5xx doesn't break channel startup.
func (c *Channel) startSlashCommandSync(ctx context.Context) {
	go func() {
		const maxAttempts = 3
		syncCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if err := c.SyncSlashCommands(syncCtx); err != nil {
				lastErr = err
				slog.Warn("discord: failed to sync slash commands",
					"error", err, "attempt", attempt,
				)
				if attempt < maxAttempts {
					select {
					case <-syncCtx.Done():
						return
					case <-time.After(time.Duration(attempt*5) * time.Second):
					}
				}
				continue
			}
			return
		}
		if lastErr != nil {
			slog.Warn("discord: slash commands remain unsynced after retries",
				"error", lastErr,
			)
		}
	}()
}
