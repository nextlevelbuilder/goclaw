package tools

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── extractGroupChatID ──────────────────────────────────────────────────────

func TestExtractGroupChatID_ValidGroupZalo(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		wantID   string
	}{
		{
			name:     "group Zalo format",
			userID:   "group:tuelinhzalo:7296946457790431889",
			wantID:   "7296946457790431889",
		},
		{
			name:     "group Telegram format",
			userID:   "group:telegram:123456",
			wantID:   "123456",
		},
		{
			name:     "group with numeric chatID",
			userID:   "group:channel:999",
			wantID:   "999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroupChatID(tt.userID)
			if got != tt.wantID {
				t.Errorf("extractGroupChatID(%q) = %q, want %q", tt.userID, got, tt.wantID)
			}
		})
	}
}

func TestExtractGroupChatID_NonGroupUsers(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{
			name:   "guild format (not group)",
			userID: "guild:abc:user:456",
		},
		{
			name:   "DM user (no group prefix)",
			userID: "123456789",
		},
		{
			name:   "empty string",
			userID: "",
		},
		{
			name:   "malformed group (missing parts)",
			userID: "group:channel",
		},
		{
			name:   "malformed group (only prefix)",
			userID: "group:",
		},
		{
			name:   "just 'group'",
			userID: "group",
		},
		{
			name:   "guild with many parts",
			userID: "guild:discord:server:channel:msg:123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroupChatID(tt.userID)
			if got != "" {
				t.Errorf("extractGroupChatID(%q) = %q, want empty string", tt.userID, got)
			}
		})
	}
}

func TestExtractGroupChatID_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		wantID   string
	}{
		{
			name:     "chatID containing colon (edge case, unusual but possible)",
			userID:   "group:channel:abc:def",
			wantID:   "abc:def", // SplitN with n=3 takes everything after second colon
		},
		{
			name:     "numeric-only chatID",
			userID:   "group:telegram:9876543210",
			wantID:   "9876543210",
		},
		{
			name:     "alphanumeric chatID",
			userID:   "group:slack:C1234567890",
			wantID:   "C1234567890",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractGroupChatID(tt.userID)
			if got != tt.wantID {
				t.Errorf("extractGroupChatID(%q) = %q, want %q", tt.userID, got, tt.wantID)
			}
		})
	}
}

// ─── isSessionInScope ────────────────────────────────────────────────────────

func TestIsSessionInScope_SharedSessionsEnabled(t *testing.T) {
	ctx := store.WithSharedSessions(context.Background())
	ctx = store.WithUserID(ctx, "group:telegram:111")

	// With shared sessions enabled, all targets should be in scope
	tests := []string{
		"agent:x:zalo:group:222",
		"agent:x:cron:job-abc",
		"direct-dm-session",
		"any-arbitrary-key",
	}

	for _, targetKey := range tests {
		if !isSessionInScope(ctx, targetKey, "current:session") {
			t.Errorf("shared sessions enabled: %q should be in scope", targetKey)
		}
	}
}

func TestIsSessionInScope_AllowOwnSession(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "user:123")

	// Own session should always be allowed, regardless of group scoping
	currentKey := "agent:x:zalo:group:111"
	if !isSessionInScope(ctx, currentKey, currentKey) {
		t.Error("own session should always be in scope")
	}
}

func TestIsSessionInScope_GroupScopedUser_SameChatID(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:zalo:111")

	tests := []struct {
		name      string
		targetKey string
		wantScope bool
	}{
		{
			name:      "basic group match",
			targetKey: "agent:x:zalo:group:111",
			wantScope: true,
		},
		{
			name:      "group with topic",
			targetKey: "agent:x:tg:group:111:topic:5",
			wantScope: true,
		},
		{
			name:      "group at end of key",
			targetKey: "prefix:group:111",
			wantScope: true,
		},
		{
			name:      "exact suffix match with colon",
			targetKey: "agent:111",
			wantScope: true, // HasSuffix(":111") matches, "agent:111".HasSuffix(":111") is true
		},
		{
			name:      "contains chatID in middle with colon after",
			targetKey: "something:111:other",
			wantScope: true, // Contains `:111:`
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionInScope(ctx, tt.targetKey, "current:key")
			if got != tt.wantScope {
				t.Errorf("isSessionInScope(..., %q, ...) = %v, want %v", tt.targetKey, got, tt.wantScope)
			}
		})
	}
}

func TestIsSessionInScope_GroupScopedUser_DifferentChatID(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:zalo:111") // User in group 111

	tests := []struct {
		name      string
		targetKey string
		wantScope bool
	}{
		{
			name:      "different group",
			targetKey: "agent:x:zalo:group:222",
			wantScope: false,
		},
		{
			name:      "different group with topic",
			targetKey: "agent:x:tg:group:222:topic:5",
			wantScope: false,
		},
		{
			name:      "111 appears only as key suffix (HasSuffix matches)",
			targetKey: "agent:x:channel:111",
			wantScope: true, // HasSuffix(":111") is true, so this passes
		},
		{
			name:      "222 as suffix (not the user's group)",
			targetKey: "agent:x:channel:222",
			wantScope: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionInScope(ctx, tt.targetKey, "current:key")
			if got != tt.wantScope {
				t.Errorf("isSessionInScope(..., %q, ...) = %v, want %v", tt.targetKey, got, tt.wantScope)
			}
		})
	}
}

func TestIsSessionInScope_DMUser_NoRestriction(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "dm:user:12345") // Non-group user

	// DM users have no group restriction — all sessions are accessible
	tests := []string{
		"agent:x:zalo:group:111",
		"agent:x:zalo:group:222",
		"agent:x:cron:job-abc",
		"direct-session",
	}

	for _, targetKey := range tests {
		if !isSessionInScope(ctx, targetKey, "current:key") {
			t.Errorf("DM user: %q should be in scope", targetKey)
		}
	}
}

func TestIsSessionInScope_CronSessionWithGroupUser(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:telegram:111") // Group-scoped user

	// Cron sessions (no group chatID) should not be accessible to group-scoped users
	cronKey := "agent:x:cron:job-abc"
	if isSessionInScope(ctx, cronKey, "current:key") {
		t.Error("cron session should not be in scope for group-scoped user")
	}
}

func TestIsSessionInScope_EmptyUserID(t *testing.T) {
	ctx := context.Background()
	// No user ID set

	// Empty user (non-group) should have no restriction
	if !isSessionInScope(ctx, "agent:x:zalo:group:111", "current:key") {
		t.Error("empty user ID should allow all sessions")
	}
}

func TestIsSessionInScope_MarkerBoundaryExactness(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:zalo:111")

	tests := []struct {
		name      string
		targetKey string
		wantScope bool
	}{
		{
			name:      "exact suffix match with colon",
			targetKey: "prefix:111",
			wantScope: true,
		},
		{
			name:      "suffix with content after",
			targetKey: "prefix:111:topic",
			wantScope: true,
		},
		{
			name:      "no leading colon (numeric collision)",
			targetKey: "group111", // No colon before 111
			wantScope: false,
		},
		{
			name:      "111 in middle without proper bounds",
			targetKey: "a111b", // 111 without colons
			wantScope: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionInScope(ctx, tt.targetKey, "current:key")
			if got != tt.wantScope {
				t.Errorf("isSessionInScope(..., %q, ...) = %v, want %v", tt.targetKey, got, tt.wantScope)
			}
		})
	}
}

func TestIsSessionInScope_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name              string
		userID            string
		sharedSessions    bool
		targetKey         string
		currentKey        string
		wantInScope       bool
	}{
		{
			name:           "shared sessions overrides group restriction",
			userID:         "group:zalo:111",
			sharedSessions: true,
			targetKey:      "agent:x:zalo:group:222",
			currentKey:     "agent:x:zalo:group:111",
			wantInScope:    true,
		},
		{
			name:           "own session always allowed despite group mismatch",
			userID:         "group:zalo:111",
			sharedSessions: false,
			targetKey:      "agent:x:zalo:group:222",
			currentKey:     "agent:x:zalo:group:222",
			wantInScope:    true,
		},
		{
			name:           "group user, target from same group, with forum topic",
			userID:         "group:telegram:999",
			sharedSessions: false,
			targetKey:      "agent:abc:telegram:group:999:topic:42",
			currentKey:     "current:key",
			wantInScope:    true,
		},
		{
			name:           "group user, cron job not accessible",
			userID:         "group:discord:555",
			sharedSessions: false,
			targetKey:      "agent:xyz:cron:backup-job",
			currentKey:     "current:key",
			wantInScope:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.sharedSessions {
				ctx = store.WithSharedSessions(ctx)
			}
			ctx = store.WithUserID(ctx, tt.userID)

			got := isSessionInScope(ctx, tt.targetKey, tt.currentKey)
			if got != tt.wantInScope {
				t.Errorf("isSessionInScope = %v, want %v", got, tt.wantInScope)
			}
		})
	}
}

// ─── Guild user through isSessionInScope ─────────────────────────────────────

func TestIsSessionInScope_GuildUser_NoRestriction(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "guild:abc:user:456") // Non-group, guild-scoped

	// Guild users have no group restriction — extractGroupChatID returns ""
	tests := []string{
		"agent:x:discord:group:111",
		"agent:x:discord:group:222",
		"agent:x:cron:job-abc",
		"agent:x:discord:channel:789",
	}

	for _, targetKey := range tests {
		if !isSessionInScope(ctx, targetKey, "current:key") {
			t.Errorf("guild user: %q should be in scope (no group restriction)", targetKey)
		}
	}
}

// ─── Realistic long chatIDs (production Zalo IDs) ────────────────────────────

func TestIsSessionInScope_RealisticLongChatIDs(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:tuelinhzalo:7296946457790431889")

	tests := []struct {
		name      string
		targetKey string
		wantScope bool
	}{
		{
			name:      "same group, realistic key",
			targetKey: "agent:itstuelinh:tuelinhzalo:group:7296946457790431889",
			wantScope: true,
		},
		{
			name:      "same group with topic",
			targetKey: "agent:itstuelinh:tuelinhzalo:group:7296946457790431889:topic:5",
			wantScope: true,
		},
		{
			name:      "different group, realistic key (THE BUG SCENARIO)",
			targetKey: "agent:itstuelinh:tuelinhzalo:group:1541284681557250737",
			wantScope: false,
		},
		{
			name:      "cron session, no chatID match",
			targetKey: "agent:itstuelinh:cron:019d4345-abcd-1234-5678-abcdef123456",
			wantScope: false,
		},
		{
			name:      "partial numeric overlap (different ID sharing digits)",
			targetKey: "agent:itstuelinh:tuelinhzalo:group:72969464577904318891",
			wantScope: false, // longer ID, should not match
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionInScope(ctx, tt.targetKey, "agent:itstuelinh:cron:current-job")
			if got != tt.wantScope {
				t.Errorf("isSessionInScope(..., %q, ...) = %v, want %v", tt.targetKey, got, tt.wantScope)
			}
		})
	}
}

// ─── Cron own session match ──────────────────────────────────────────────────

func TestIsSessionInScope_CronOwnSessionMatch(t *testing.T) {
	ctx := context.Background()
	ctx = store.WithUserID(ctx, "group:telegram:111")

	// Cron session as currentKey — accessing own session should always work
	cronKey := "agent:mybot:cron:019d4345-daily-report"
	if !isSessionInScope(ctx, cronKey, cronKey) {
		t.Error("cron session accessing itself (own session) should always be in scope")
	}
}

// ─── Multi-colon chatID through full flow ────────────────────────────────────

func TestIsSessionInScope_MultiColonChatID(t *testing.T) {
	ctx := context.Background()
	// SplitN("group:channel:abc:def", ":", 3) → chatID = "abc:def"
	ctx = store.WithUserID(ctx, "group:channel:abc:def")

	tests := []struct {
		name      string
		targetKey string
		wantScope bool
	}{
		{
			name:      "target contains multi-colon chatID as suffix",
			targetKey: "agent:x:group:abc:def",
			wantScope: true, // HasSuffix(":abc:def")
		},
		{
			name:      "target contains multi-colon chatID in middle",
			targetKey: "agent:x:group:abc:def:topic:1",
			wantScope: true, // Contains(":abc:def:")
		},
		{
			name:      "target has partial match only",
			targetKey: "agent:x:group:abc",
			wantScope: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSessionInScope(ctx, tt.targetKey, "current:key")
			if got != tt.wantScope {
				t.Errorf("isSessionInScope(..., %q, ...) = %v, want %v", tt.targetKey, got, tt.wantScope)
			}
		})
	}
}

// ─── Malformed group userID → no restriction ─────────────────────────────────

func TestIsSessionInScope_MalformedGroupUserID(t *testing.T) {
	tests := []struct {
		name   string
		userID string
	}{
		{name: "group with empty chatID", userID: "group:channel:"},
		{name: "group missing chatID", userID: "group:channel"},
		{name: "group only prefix", userID: "group:"},
		{name: "just group", userID: "group"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = store.WithUserID(ctx, tt.userID)

			// Malformed group → empty chatID → no restriction → allow all
			target := "agent:x:zalo:group:222"
			if tt.userID == "group:channel:" {
				// "group:channel:" → chatID="" → no restriction
				if !isSessionInScope(ctx, target, "current:key") {
					t.Errorf("malformed group %q should have no restriction", tt.userID)
				}
			} else {
				// These all return "" from extractGroupChatID
				if !isSessionInScope(ctx, target, "current:key") {
					t.Errorf("malformed group %q should have no restriction", tt.userID)
				}
			}
		})
	}
}
