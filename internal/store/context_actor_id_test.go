package store

import (
	"context"
	"testing"
)

// In group context, SenderID (the individual sender) is the actor —
// UserID is the shared group namespace, not a user identity.
func TestActorIDFromContext_GroupUsesSender(t *testing.T) {
	ctx := WithUserID(context.Background(), "group:telegram:-100123")
	ctx = WithSenderID(ctx, "42")
	if got := ActorIDFromContext(ctx); got != "42" {
		t.Errorf("group_uses_sender: got %q, want %q", got, "42")
	}
}

// Guild scope mirrors group scope — SenderID wins.
func TestActorIDFromContext_GuildUsesSender(t *testing.T) {
	ctx := WithUserID(context.Background(), "guild:1234:user:5678")
	ctx = WithSenderID(ctx, "5678")
	if got := ActorIDFromContext(ctx); got != "5678" {
		t.Errorf("guild_uses_sender: got %q, want %q", got, "5678")
	}
}

// DM context — UserID is the (possibly tenant-merged) actor identity;
// SenderID is the raw channel-specific sender. Prefer UserID so
// cross-channel merged identities ("viettx") are preserved over raw
// numeric senders ("386246614").
func TestActorIDFromContext_DMPrefersUserIDForTenantMerge(t *testing.T) {
	ctx := WithUserID(context.Background(), "viettx")
	ctx = WithSenderID(ctx, "386246614")
	if got := ActorIDFromContext(ctx); got != "viettx" {
		t.Errorf("dm_merge: got %q, want %q (merged tenant identity should win in DM)", got, "viettx")
	}
}

// DM without merge — UserID == SenderID. Either works.
func TestActorIDFromContext_DMUnmergedReturnsUser(t *testing.T) {
	ctx := WithUserID(context.Background(), "386246614")
	ctx = WithSenderID(ctx, "386246614")
	if got := ActorIDFromContext(ctx); got != "386246614" {
		t.Errorf("dm_unmerged: got %q, want %q", got, "386246614")
	}
}

// Group with no SenderID — fall back to UserID (the group principal).
// Rare but avoids returning empty when only scope is known.
func TestActorIDFromContext_GroupFallbackToUserWhenSenderEmpty(t *testing.T) {
	ctx := WithUserID(context.Background(), "group:telegram:-100123")
	if got := ActorIDFromContext(ctx); got != "group:telegram:-100123" {
		t.Errorf("group_fallback: got %q, want %q", got, "group:telegram:-100123")
	}
}

// HTTP/cron — no SenderID. UserID is the actor.
func TestActorIDFromContext_HTTPReturnsUserID(t *testing.T) {
	ctx := WithUserID(context.Background(), "user-7")
	if got := ActorIDFromContext(ctx); got != "user-7" {
		t.Errorf("http_user_id: got %q, want %q", got, "user-7")
	}
}

// Neither set — empty.
func TestActorIDFromContext_NeitherSetReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	if got := ActorIDFromContext(ctx); got != "" {
		t.Errorf("neither_set: got %q, want %q", got, "")
	}
}

// Only SenderID set (no UserID) — fall back to SenderID.
func TestActorIDFromContext_SenderOnlyFallback(t *testing.T) {
	ctx := WithSenderID(context.Background(), "42")
	if got := ActorIDFromContext(ctx); got != "42" {
		t.Errorf("sender_only: got %q, want %q", got, "42")
	}
}

// Delimited sender ("42|extra") is returned as-is in group context;
// callers that need a numeric ID do the split themselves
// (config_permission_store.go splits on "|" before lookup).
func TestActorIDFromContext_GroupDelimitedSenderAsIs(t *testing.T) {
	ctx := WithUserID(context.Background(), "group:telegram:-100123")
	ctx = WithSenderID(ctx, "42|extra")
	if got := ActorIDFromContext(ctx); got != "42|extra" {
		t.Errorf("delimited_sender_as_is: got %q, want %q", got, "42|extra")
	}
}
