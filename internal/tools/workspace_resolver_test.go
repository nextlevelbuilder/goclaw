package tools

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// masterTenantID mirrors store.MasterTenantID for tests.
var masterTenantID = uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")

func TestResolveWorkspace_EmptyLayers(t *testing.T) {
	got := ResolveWorkspace("/data")
	if got != "/data" {
		t.Errorf("expected /data, got %s", got)
	}
}

func TestResolveWorkspace_TenantOnly(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	got := ResolveWorkspace("/data", TenantLayer(tid, "acme"))
	want := filepath.Join("/data", "tenants", "acme")
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_TenantMaster(t *testing.T) {
	got := ResolveWorkspace("/data", TenantLayer(masterTenantID, "master"))
	if got != "/data" {
		t.Errorf("master tenant should be no-op, got %s", got)
	}
}

func TestResolveWorkspace_TenantTeam(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	got := ResolveWorkspace("/data",
		TenantLayer(tid, "acme"),
		TeamLayer(teamID),
	)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String())
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_TenantTeamShared(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	got := ResolveWorkspace("/data",
		TenantLayer(tid, "acme"),
		TeamLayer(teamID),
		UserChatLayer("", false), // shared → empty segment
	)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String())
	if got != want {
		t.Errorf("shared team should have no chat segment, want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_TenantTeamIsolated(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	chatID := "chat-abc-123"
	got := ResolveWorkspace("/data",
		TenantLayer(tid, "acme"),
		TeamLayer(teamID),
		UserChatLayer(chatID, false),
	)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String(), chatID)
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_TenantTeamProject(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	projectID := uuid.MustParse("0193d000-0000-7000-8000-000000000004")
	got := ResolveWorkspace("/data",
		TenantLayer(tid, "acme"),
		TeamLayer(teamID),
		ProjectLayer(&projectID),
	)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String(), "projects", projectID.String())
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_FullStack(t *testing.T) {
	tid := uuid.MustParse("0193b000-0000-7000-8000-000000000002")
	teamID := uuid.MustParse("0193c000-0000-7000-8000-000000000003")
	projectID := uuid.MustParse("0193d000-0000-7000-8000-000000000004")
	chatID := "chat-xyz"
	got := ResolveWorkspace("/data",
		TenantLayer(tid, "acme"),
		TeamLayer(teamID),
		ProjectLayer(&projectID),
		UserChatLayer(chatID, false),
	)
	want := filepath.Join("/data", "tenants", "acme", "teams", teamID.String(), "projects", projectID.String(), chatID)
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_SoloAgent(t *testing.T) {
	userID := SanitizePathSegment("user:telegram:12345")
	got := ResolveWorkspace("/ws",
		UserChatLayer(userID, false),
	)
	want := filepath.Join("/ws", "user_telegram_12345")
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_SoloAgentShared(t *testing.T) {
	got := ResolveWorkspace("/ws",
		UserChatLayer("user123", true),
	)
	if got != "/ws" {
		t.Errorf("shared should be no-op, got %s", got)
	}
}

func TestResolveWorkspace_SoloAgentProject(t *testing.T) {
	projectID := uuid.MustParse("0193d000-0000-7000-8000-000000000004")
	userID := SanitizePathSegment("user:slack:u1")
	got := ResolveWorkspace("/ws",
		ProjectLayer(&projectID),
		UserChatLayer(userID, false),
	)
	want := filepath.Join("/ws", "projects", projectID.String(), "user_slack_u1")
	if got != want {
		t.Errorf("want %s, got %s", want, got)
	}
}

func TestResolveWorkspace_NilProject(t *testing.T) {
	got := ResolveWorkspace("/data",
		ProjectLayer(nil),
	)
	if got != "/data" {
		t.Errorf("nil project should be no-op, got %s", got)
	}
}

func TestResolveWorkspace_NilTeam(t *testing.T) {
	got := ResolveWorkspace("/data",
		TeamLayer(uuid.Nil),
	)
	if got != "/data" {
		t.Errorf("nil team should be no-op, got %s", got)
	}
}

func TestResolveWorkspace_ZeroProject(t *testing.T) {
	nilID := uuid.Nil
	got := ResolveWorkspace("/data",
		ProjectLayer(&nilID),
	)
	if got != "/data" {
		t.Errorf("zero project should be no-op, got %s", got)
	}
}

func TestResolveWorkspace_SharedTrue(t *testing.T) {
	got := ResolveWorkspace("/data",
		UserChatLayer("chat-123", true),
	)
	if got != "/data" {
		t.Errorf("shared=true should skip segment, got %s", got)
	}
}

func TestSanitizePathSegment(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"user:telegram:123", "user_telegram_123"},
		{"user@email.com", "user_email_com"},
		{"hello world", "hello_world"},
		{"a-b_c", "a-b_c"},
		{"", ""},
		{"café", "caf_"},
		{"../etc/passwd", "___etc_passwd"},
	}
	for _, tt := range tests {
		got := SanitizePathSegment(tt.input)
		if got != tt.want {
			t.Errorf("SanitizePathSegment(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
