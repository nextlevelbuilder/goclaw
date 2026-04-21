package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// interactionEcho holds the Discord interaction context needed to reply to an
// agent-backed slash command. The token is valid for 15 minutes after the
// interaction; past that, Send() must fall back to a regular channel post
// because InteractionResponseEdit / FollowupMessageCreate will be rejected.
type interactionEcho struct {
	AppID      string
	Token      string
	Ephemeral  bool
	CreatedAt  time.Time
	FirstChunk sync.Once // guards the initial InteractionResponseEdit (replaces the deferred ACK)
}

// expired reports whether the interaction token is past its 15-minute lifetime.
// Called by Send() to decide between the interaction path and a channel post.
func (e *interactionEcho) expired() bool {
	return time.Since(e.CreatedAt) > 14*time.Minute // 1-min safety margin
}

// agentBackedCommands lists the slash commands that route into the agent
// pipeline (i.e. each produces a canned prompt and replies via interaction
// token). The handler switches on this set to decide whether to defer the
// ACK or respond inline.
var agentBackedCommands = map[SlashCommandName]bool{
	SlashCommandAsk:       true,
	SlashCommandRecall:    true,
	SlashCommandSummarize: true,
}

// handleInteraction is the discordgo callback for InteractionCreate events.
// Discord requires a response within 3 seconds; for any agent-backed command
// we send a deferred ACK immediately and let the agent pipeline reply via
// InteractionResponseEdit / FollowupMessageCreate once it finishes.
func (c *Channel) handleInteraction(_ *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Type != discordgo.InteractionApplicationCommand {
		return // ignore button clicks, modal submits, autocomplete — out of scope for this PR
	}

	data := i.ApplicationCommandData()
	name := SlashCommandName(data.Name)
	invoker, invokerTag := resolveInteractionUser(i)
	channelID := i.ChannelID
	isDM := i.GuildID == ""
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	// Tenant context for the policy check.
	ctx := store.WithTenantID(context.Background(), c.TenantID())

	// Allowlist / DM-policy / group-policy check mirrors handleMessage above.
	// For slash commands we respond ephemerally with a short refusal rather
	// than silently dropping, so users know why nothing happened.
	if isDM {
		if !c.checkDMPolicy(ctx, invoker, channelID) {
			c.respondEphemeral(i, "Access not configured. Ask the bot owner to pair your user ID.")
			return
		}
	} else {
		if !c.checkGroupPolicy(ctx, invoker, channelID) {
			c.respondEphemeral(i, "This channel isn't authorized for the bot.")
			return
		}
	}
	if !c.IsAllowed(invoker) {
		c.respondEphemeral(i, "Your user isn't on the allowlist for this bot.")
		return
	}

	// State commands — respond inline, no agent loop.
	switch name {
	case SlashCommandReset:
		c.handleResetCommand(ctx, i, invoker, invokerTag, channelID, peerKind)
		return
	case SlashCommandStatus:
		c.handleStatusCommand(i, channelID)
		return
	case SlashCommandStop:
		c.handleStopCommand(i, channelID)
		return
	case SlashCommandHelp:
		c.handleHelpCommand(i)
		return
	}

	// Agent-backed commands — defer the ACK and route as a canned prompt.
	if !agentBackedCommands[name] {
		c.respondEphemeral(i, fmt.Sprintf("Unknown command: /%s", data.Name))
		return
	}

	prompt, ephemeral, ok := c.buildAgentPrompt(name, data, i)
	if !ok {
		return // buildAgentPrompt already responded with the error
	}

	// Deferred ACK: tells Discord "reply is coming, show a thinking state".
	// Must happen inside 3s of the interaction — do it before the inbound
	// publish so a slow bus subscriber can't starve us into a timeout.
	ackFlags := discordgo.MessageFlags(0)
	if ephemeral {
		ackFlags = discordgo.MessageFlagsEphemeral
	}
	if err := c.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: ackFlags},
	}); err != nil {
		slog.Warn("discord: deferred ACK failed", "command", name, "error", err)
		// If the ACK failed the interaction token is likely unusable, but
		// we still want the agent to run — fall through, Send() will route
		// via channel post instead when the echo expires / is missing.
	}

	// Audio routing (same policy as plain-message handler).
	targetAgentID := c.AgentID()

	// Stash the interaction echo keyed by a synthetic message ID so the
	// outbound dispatcher can find it via discord_interaction_token metadata.
	// We key by interaction ID (unique per invocation); Send() reads it back.
	echo := &interactionEcho{
		AppID:     c.applicationID,
		Token:     i.Token,
		Ephemeral: ephemeral,
		CreatedAt: time.Now(),
	}
	c.interactionTokens.Store(i.ID, echo)

	metadata := map[string]string{
		"message_id":   i.ID,
		"user_id":      invoker,
		"username":     invokerTag,
		"display_name": channels.SanitizeDisplayName(invokerTag),
		"guild_id":     i.GuildID,
		"channel_id":   channelID,
		"is_dm":        fmt.Sprintf("%t", isDM),
		// Interaction reply path (consumed by Send + routingMetaKeys):
		"discord_interaction_token": i.Token,
		"discord_interaction_id":    i.ID,
		"discord_interaction_appid": c.applicationID,
	}
	if ephemeral {
		metadata["discord_interaction_flags"] = "ephemeral"
	}

	// Collect contact so /ask from an unpaired user still shows up in the
	// contacts dashboard.
	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), invoker, invoker, invokerTag, invokerTag, peerKind, "user", "", "")
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: invoker,
		ChatID:   channelID,
		Content:  prompt,
		PeerKind: peerKind,
		UserID:   invoker,
		AgentID:  targetAgentID,
		TenantID: c.TenantID(),
		Metadata: metadata,
	})
}

// buildAgentPrompt converts a slash-command invocation into the canned prompt
// the agent receives. Returns (prompt, ephemeral, ok). When ok is false the
// caller must not proceed — this function has already responded with the
// appropriate error message to Discord.
func (c *Channel) buildAgentPrompt(name SlashCommandName, data discordgo.ApplicationCommandInteractionData, i *discordgo.InteractionCreate) (string, bool, bool) {
	switch name {
	case SlashCommandAsk:
		prompt := strings.TrimSpace(optionString(data, "prompt"))
		if prompt == "" {
			c.respondEphemeral(i, "The `prompt` option is required.")
			return "", false, false
		}
		private := optionBool(data, "private")
		return prompt, private, true

	case SlashCommandRecall:
		query := strings.TrimSpace(optionString(data, "query"))
		if query == "" {
			c.respondEphemeral(i, "The `query` option is required.")
			return "", false, false
		}
		return fmt.Sprintf("Search my memory for: %s\nSummarize the most relevant hits and cite what you found.", query), false, true

	case SlashCommandSummarize:
		count := int(optionInt(data, "count"))
		if count <= 0 {
			count = 20
		}
		if count > 200 {
			count = 200
		}
		return fmt.Sprintf("Summarize the last %d messages in this channel. Keep it concise — bullet points by theme.", count), false, true
	}
	return "", false, false
}

// handleResetCommand clears the session for the invoker in this channel.
// Keyed by the (channel, user) pair to mirror how Discord session keys are
// computed elsewhere in the pipeline.
func (c *Channel) handleResetCommand(_ context.Context, i *discordgo.InteractionCreate, invoker, invokerTag, channelID, peerKind string) {
	// Session clearing is not implemented in the current channel layer —
	// sessions are owned by the pipeline. Best we can do from the channel
	// is publish a synthetic "[reset]" inbound, but that muddies conversation
	// history. For v1 we respond with instructions and leave the actual
	// clearing to a follow-up that wires a SessionClearer dependency.
	_ = invokerTag
	_ = channelID
	_ = peerKind
	_ = invoker
	c.respondEphemeral(i, "Reset is not yet wired through to the session store. Track: a follow-up PR will add a SessionClearer dependency so /reset clears the agent's view of this conversation. Use /stop to cancel an in-flight run today.")
}

// handleStatusCommand reports whether there is an active agent run in this
// channel. Uses the channel's local placeholders/typing state as a proxy —
// we don't have direct access to channels.Manager's RunContext from here,
// and stringing that through is its own feature. For v1: report what we can.
func (c *Channel) handleStatusCommand(i *discordgo.InteractionCreate, channelID string) {
	var parts []string
	if _, ok := c.typingCtrls.Load(channelID); ok {
		parts = append(parts, "typing indicator active (agent likely processing)")
	}
	if _, ok := c.placeholders.Load(channelID); ok {
		parts = append(parts, "placeholder message pending final reply")
	}
	if len(parts) == 0 {
		c.respondEphemeral(i, "Idle — no active run detected in this channel.")
		return
	}
	c.respondEphemeral(i, "Active: "+strings.Join(parts, "; "))
}

// handleStopCommand attempts to cancel the current run by stopping the typing
// indicator. A full cancel would need a hook into the scheduler, which is a
// broader refactor — v1 just turns off the visible signal so the user knows
// we've acknowledged the stop request.
func (c *Channel) handleStopCommand(i *discordgo.InteractionCreate, channelID string) {
	stopped := false
	if v, ok := c.typingCtrls.LoadAndDelete(channelID); ok {
		if ctrl, ok := v.(interface{ Stop() }); ok {
			ctrl.Stop()
			stopped = true
		}
	}
	if stopped {
		c.respondEphemeral(i, "Typing indicator cleared. The agent may still finish its current run; a full cancel hook is a follow-up.")
		return
	}
	c.respondEphemeral(i, "Nothing in flight in this channel.")
}

// handleHelpCommand renders a static help message from DefaultSlashCommands.
// Keeping it generated from the same source of truth means new commands
// automatically appear in the help output.
func (c *Channel) handleHelpCommand(i *discordgo.InteractionCreate) {
	var b strings.Builder
	b.WriteString("**Commands supported by this bot:**\n\n")
	for _, cmd := range DefaultSlashCommands() {
		b.WriteString(fmt.Sprintf("• `/%s` — %s\n", cmd.Name, cmd.Description))
	}
	c.respondEphemeral(i, b.String())
}

// trySendViaInteraction attempts to deliver msg via the slash-command
// interaction reply path. Returns (handled, err):
//   - handled=true : this function took ownership of the delivery (whether
//     successful or failed — err reflects that). Send() must NOT continue.
//   - handled=false: the interaction echo is missing or expired. Send()
//     should fall through to the regular channel-post flow.
//
// First chunk edits the deferred response (promoting it from "thinking..."
// to the real reply). Subsequent chunks are posted as interaction followups.
// Content longer than 2000 chars is split at a newline boundary between
// chunks, matching the regular Discord send path.
func (c *Channel) trySendViaInteraction(_ context.Context, msg bus.OutboundMessage, token string) (bool, error) {
	interactionID := msg.Metadata["discord_interaction_id"]
	if interactionID == "" {
		return false, nil // metadata carries the token without an ID — shouldn't happen, skip safely
	}

	val, ok := c.interactionTokens.Load(interactionID)
	if !ok {
		return false, nil
	}
	echo, ok := val.(*interactionEcho)
	if !ok || echo == nil {
		c.interactionTokens.Delete(interactionID)
		return false, nil
	}
	if echo.expired() {
		c.interactionTokens.Delete(interactionID)
		slog.Warn("discord: interaction token expired, falling back to channel post",
			"interaction_id", interactionID, "age", time.Since(echo.CreatedAt))
		return false, nil
	}

	content := msg.Content
	if content == "" {
		// Agent suppressed reply — edit the deferred ACK to a terse note so
		// the user doesn't see a permanent "thinking..." state.
		_, editErr := c.session.InteractionResponseEdit(
			&discordgo.Interaction{AppID: echo.AppID, Token: echo.Token},
			&discordgo.WebhookEdit{Content: stringPtr("(no reply)")},
		)
		c.interactionTokens.Delete(interactionID)
		if editErr != nil {
			slog.Warn("discord: no-reply interaction edit failed", "error", editErr)
			return true, editErr
		}
		return true, nil
	}

	chunks := chunkForDiscord(content, 2000)
	if len(chunks) == 0 {
		return true, nil
	}

	// First chunk: edit the deferred response to surface the reply.
	_, editErr := c.session.InteractionResponseEdit(
		&discordgo.Interaction{AppID: echo.AppID, Token: echo.Token},
		&discordgo.WebhookEdit{Content: stringPtr(chunks[0])},
	)
	if editErr != nil {
		// Edit failed — token may have been revoked. Drop the echo and let
		// the caller fall through. We return false here so Send() can retry
		// via the regular channel path and the user still gets the reply.
		c.interactionTokens.Delete(interactionID)
		slog.Warn("discord: interaction edit failed, falling back to channel post",
			"interaction_id", interactionID, "error", editErr)
		return false, nil
	}

	// Followups: each subsequent chunk becomes its own message under the
	// interaction. They inherit ephemeral flag from the initial deferred ACK.
	for _, chunk := range chunks[1:] {
		params := &discordgo.WebhookParams{Content: chunk}
		if echo.Ephemeral {
			params.Flags = discordgo.MessageFlagsEphemeral
		}
		if _, fErr := c.session.FollowupMessageCreate(
			&discordgo.Interaction{AppID: echo.AppID, Token: echo.Token},
			false,
			params,
		); fErr != nil {
			slog.Warn("discord: interaction followup failed", "error", fErr)
			// Partial delivery is better than no delivery; keep going.
		}
	}

	c.interactionTokens.Delete(interactionID)
	return true, nil
}

// chunkForDiscord splits content at the given length, preferring to break on
// a newline within the second half of the window. Matches the policy used by
// sendChunked for regular message posts so the UX is consistent whether the
// reply goes via interaction path or channel post.
func chunkForDiscord(content string, maxLen int) []string {
	if content == "" {
		return nil
	}
	var out []string
	for len(content) > maxLen {
		cutAt := maxLen
		if idx := lastIndexByte(content[:maxLen], '\n'); idx > maxLen/2 {
			cutAt = idx + 1
		}
		out = append(out, content[:cutAt])
		content = content[cutAt:]
	}
	if content != "" {
		out = append(out, content)
	}
	return out
}

func stringPtr(s string) *string { return &s }

// respondEphemeral sends a short ephemeral reply to the invoker. Used for all
// state/direct commands and for validation / policy errors.
func (c *Channel) respondEphemeral(i *discordgo.InteractionCreate, content string) {
	err := c.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		slog.Warn("discord: interaction reply failed", "error", err)
	}
}

// resolveInteractionUser pulls the invoker's ID + username out of the
// interaction payload. In guild channels Member.User is populated; in DMs
// the top-level User field is used instead.
func resolveInteractionUser(i *discordgo.InteractionCreate) (id, tag string) {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID, i.Member.User.Username
	}
	if i.User != nil {
		return i.User.ID, i.User.Username
	}
	return "", ""
}

// optionString reads a named string option from the interaction data.
// Missing / wrong-type options return the empty string (the caller handles
// required-field validation).
func optionString(data discordgo.ApplicationCommandInteractionData, name string) string {
	for _, opt := range data.Options {
		if opt.Name == name {
			if s, ok := opt.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// optionBool reads a named boolean option. Returns false when missing.
func optionBool(data discordgo.ApplicationCommandInteractionData, name string) bool {
	for _, opt := range data.Options {
		if opt.Name == name {
			if b, ok := opt.Value.(bool); ok {
				return b
			}
		}
	}
	return false
}

// optionInt reads a named integer option. discordgo decodes integers into
// float64 (JSON numbers), so convert defensively.
func optionInt(data discordgo.ApplicationCommandInteractionData, name string) int64 {
	for _, opt := range data.Options {
		if opt.Name == name {
			switch v := opt.Value.(type) {
			case float64:
				return int64(v)
			case int64:
				return v
			case int:
				return int64(v)
			}
		}
	}
	return 0
}

// Unused but imported to keep the import block stable if future edits
// reference uuid in the interaction path (e.g. tenant-aware tracing).
var _ = uuid.Nil
