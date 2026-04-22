package discord

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// discordInteractionTTL is the Discord interaction-token lifetime. Tokens
	// become invalid 15 min after the interaction fires.
	discordInteractionTTL = 15 * time.Minute
	// interactionExpirySafety is the safety margin we keep below the TTL so a
	// token we decide to use doesn't expire mid-flight due to clock skew or
	// in-flight latency. 1 min covers typical request durations.
	interactionExpirySafety = 1 * time.Minute
	// interactionSweepInterval is how often the background goroutine walks
	// interactionTokens to drop expired entries (see startInteractionSweeper).
	// 5 min is a compromise between timely cleanup and wasted scheduler work.
	interactionSweepInterval = 5 * time.Minute
)

// interactionEcho holds the Discord interaction context needed to reply to an
// agent-backed slash command. The token is valid for 15 minutes after the
// interaction; past that, the interaction path is abandoned (see expired + the
// fallback branch in Send()) because InteractionResponseEdit /
// FollowupMessageCreate will be rejected.
type interactionEcho struct {
	AppID     string
	Token     string
	Ephemeral bool
	GuildID   string // empty = DM; used by the fallback path to avoid leaking ephemeral replies into a guild channel
	CreatedAt time.Time
}

// expired reports whether the interaction token is past its safe lifetime.
// Called by Send() to decide between the interaction path and a channel post.
func (e *interactionEcho) expired() bool {
	return time.Since(e.CreatedAt) > discordInteractionTTL-interactionExpirySafety
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
	if i == nil {
		return
	}
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		c.handleSlashCommand(i)
	case discordgo.InteractionMessageComponent:
		c.handleComponentInteraction(i)
	default:
		// Autocomplete + modal submissions: not yet supported.
	}
}

// handleSlashCommand is the original slash-command dispatch path. Split out of
// handleInteraction so the latter can fan out by interaction type. Logic is
// unchanged from the previous single-purpose handler.
func (c *Channel) handleSlashCommand(i *discordgo.InteractionCreate) {
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
	case SlashCommandStatus:
		c.handleStatusCommand(i, channelID)
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

	// Stash the interaction echo keyed by the Discord interaction ID so the
	// outbound dispatcher can find it via discord_interaction_token metadata.
	echo := &interactionEcho{
		AppID:     c.applicationID,
		Token:     i.Token,
		Ephemeral: ephemeral,
		GuildID:   i.GuildID,
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
//   - handled=false: the interaction echo is missing or expired AND falling
//     through to a regular channel post is safe (not ephemeral, or a DM).
//     Send() should fall through to the regular channel-post flow.
//
// First chunk edits the deferred response (promoting it from "thinking..."
// to the real reply). Subsequent chunks are posted as interaction followups
// via channels.ChunkMarkdown so the same markdown-aware chunking applied to
// regular Discord posts is used here too (avoids splitting code fences).
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
		slog.Warn("discord: interaction token expired",
			"interaction_id", interactionID, "age", time.Since(echo.CreatedAt))
		return ephemeralSuppressesFallback(echo), nil
	}

	content := msg.Content
	if content == "" {
		// Agent suppressed reply — edit the deferred ACK to a terse note so
		// the user doesn't see a permanent "thinking..." state.
		_, editErr := c.session.InteractionResponseEdit(
			&discordgo.Interaction{AppID: echo.AppID, Token: echo.Token},
			&discordgo.WebhookEdit{Content: pointerTo("(no reply)")},
		)
		c.interactionTokens.Delete(interactionID)
		if editErr != nil {
			slog.Warn("discord: no-reply interaction edit failed", "error", editErr)
			return true, editErr
		}
		return true, nil
	}

	chunks := channels.ChunkMarkdown(content, 2000)
	if len(chunks) == 0 {
		return true, nil
	}

	// First chunk: edit the deferred response to surface the reply.
	_, editErr := c.session.InteractionResponseEdit(
		&discordgo.Interaction{AppID: echo.AppID, Token: echo.Token},
		&discordgo.WebhookEdit{Content: pointerTo(chunks[0])},
	)
	if editErr != nil {
		// Edit failed — token may have been revoked. Drop the echo and decide
		// whether the caller may fall through. For ephemeral replies in a
		// guild channel, falling through would post the private content
		// publicly — we must not do that (see ephemeralSuppressesFallback).
		c.interactionTokens.Delete(interactionID)
		slog.Warn("discord: interaction edit failed",
			"interaction_id", interactionID, "error", editErr)
		if suppress := ephemeralSuppressesFallback(echo); suppress {
			// Swallow the reply — safer than leaking. User sees the stuck
			// "thinking..." state until Discord expires the interaction.
			return true, nil
		}
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

// ephemeralSuppressesFallback reports whether the fallback-to-public-channel-post
// path in Send() must be suppressed to avoid leaking a reply the invoker asked
// to be private. True when the reply was ephemeral AND the interaction happened
// in a guild (DMs are already private; an ephemeral fallback in a DM is fine).
func ephemeralSuppressesFallback(echo *interactionEcho) bool {
	return echo != nil && echo.Ephemeral && echo.GuildID != ""
}

// startInteractionSweeper periodically walks interactionTokens and drops
// expired entries. Without it, any slash invocation whose agent run never
// fires Send() with matching metadata (panic, pipeline drop, backpressure)
// leaks forever. Stops when ctx is cancelled — Channel.Stop passes the
// poll context through.
func (c *Channel) startInteractionSweeper(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(interactionSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.sweepInteractionTokens()
			}
		}
	}()
}

// sweepInteractionTokens drops expired entries from the interactionTokens
// sync.Map. Extracted from the sweeper goroutine for testability.
func (c *Channel) sweepInteractionTokens() {
	c.interactionTokens.Range(func(k, v any) bool {
		if echo, ok := v.(*interactionEcho); ok && echo != nil && echo.expired() {
			c.interactionTokens.Delete(k)
		}
		return true
	})
}

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

// handleComponentInteraction routes a message-component click (today: buttons)
// through the same inbound-bus path a text message takes. The button's
// CustomID becomes the inbound prompt so skills can dispatch off a stable
// string (e.g. "triage:suppress:<sig>" -> the error-triage-response skill).
// The original message content is stashed in metadata so skills can parse
// whatever state markers the sender embedded in the message body.
//
// Link-style buttons don't fire interactions — Discord opens the URL without
// calling back — so this path is only hit for primary/secondary/success/
// danger styles.
func (c *Channel) handleComponentInteraction(i *discordgo.InteractionCreate) {
	data := i.MessageComponentData()
	if data.ComponentType != discordgo.ButtonComponent {
		// Select menus / text inputs: not yet routed.
		return
	}
	invoker, invokerTag := resolveInteractionUser(i)
	channelID := i.ChannelID
	isDM := i.GuildID == ""
	peerKind := "group"
	if isDM {
		peerKind = "direct"
	}

	ctx := store.WithTenantID(context.Background(), c.TenantID())

	// Policy checks mirror the slash-command path. We respond ephemerally on
	// refusal so the user sees why nothing happened.
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

	// Button clicks are always non-ephemeral — the click itself is visible to
	// the channel (Discord's own UI shows "user clicked X"), so there's no
	// secrecy gain from an ephemeral ACK and plenty of audit value from a
	// public one.
	if err := c.session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	}); err != nil {
		slog.Warn("discord: deferred ACK failed for component interaction", "custom_id", data.CustomID, "error", err)
		// Keep going — the agent reply will fall back to a regular channel
		// post when Send() can't find a usable interaction token.
	}

	echo := &interactionEcho{
		AppID:     c.applicationID,
		Token:     i.Token,
		Ephemeral: false,
		GuildID:   i.GuildID,
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
		// Interaction reply path (same keys as slash-command dispatch).
		"discord_interaction_token": i.Token,
		"discord_interaction_id":    i.ID,
		"discord_interaction_appid": c.applicationID,
		// Component-specific routing keys. Skills can branch on
		// interaction_kind=component, read button_custom_id as the action,
		// and recover prior state from component_parent_content (the original
		// message body, including any HTML-comment markers the sender
		// embedded for state handoff).
		"interaction_kind": "component",
		"component_type":   "button",
		"button_custom_id": data.CustomID,
	}
	// Parent-message fields only populate if Discord actually delivered the
	// resolved message. discordgo.InteractionCreate.Message is nominally
	// non-nil for button clicks, but guard defensively — a nil dereference
	// here would panic mid-handler and the interaction would stay un-ACKed.
	if i.Message != nil {
		metadata["component_parent_message"] = i.Message.ID
		metadata["component_parent_channel"] = i.Message.ChannelID
		metadata["component_parent_content"] = i.Message.Content
	}

	if cc := c.ContactCollector(); cc != nil {
		cc.EnsureContact(ctx, c.Type(), c.Name(), invoker, invoker, invokerTag, invokerTag, peerKind, "user", "", "")
	}

	c.Bus().PublishInbound(bus.InboundMessage{
		Channel:  c.Name(),
		SenderID: invoker,
		ChatID:   channelID,
		Content:  data.CustomID, // prompt = custom_id; skills route off this
		PeerKind: peerKind,
		UserID:   invoker,
		AgentID:  c.AgentID(),
		TenantID: c.TenantID(),
		Metadata: metadata,
	})
}

