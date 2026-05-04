package cmd

import (
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

func TestMatchWorkIntakeRoute(t *testing.T) {
	cfg := config.WorkIntakeConfig{
		Enabled: true,
		Routes: []config.WorkIntakeRoute{{
			AgentID: "eng",
			Channel: "discord-eng",
			ChatIDs: []string{"controller-channel"},
			Repos:   []string{"cartridge-gg/controller", "cartridge-gg/controller-rs"},
		}},
	}

	msg := bus.InboundMessage{
		Channel:  "discord-eng",
		ChatID:   "controller-channel",
		PeerKind: "group",
		Metadata: map[string]string{"channel_id": "controller-channel"},
	}
	route, ok := matchWorkIntakeRoute(cfg, msg, "eng", "group")
	if !ok {
		t.Fatal("expected route match")
	}
	if got := strings.Join(route.Repos, ","); got != "cartridge-gg/controller,cartridge-gg/controller-rs" {
		t.Fatalf("repos = %q", got)
	}

	msg.Metadata["is_thread"] = "true"
	if _, ok := matchWorkIntakeRoute(cfg, msg, "eng", "group"); ok {
		t.Fatal("thread messages must not start a new intake")
	}
}

func TestLooksLikeWorkIntake(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "plan request",
			text: "[From: Tarrence]\n@Gillen Create a plan to upgrade controller-rs for Starknet privacy features.",
			want: true,
		},
		{
			name: "implementation request",
			text: "@Gillen fix the session expiry bug",
			want: true,
		},
		{
			name: "read only question",
			text: "@Gillen how does controller-rs sign sessions?",
			want: false,
		},
		{
			name: "read only do we question",
			text: "Do we have a timeout for jobs?",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeWorkIntake(tt.text); got != tt.want {
				t.Fatalf("looksLikeWorkIntake() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInferWorkIntakeRepo(t *testing.T) {
	repos := []string{"cartridge-gg/controller", "cartridge-gg/controller-rs"}
	repo, ok := inferWorkIntakeRepo(repos, "Create a plan to upgrade controller-rs.")
	if !ok || repo != "cartridge-gg/controller-rs" {
		t.Fatalf("repo = %q, %v", repo, ok)
	}
	if repo, ok := inferWorkIntakeRepo(repos, "Fix controller auth."); !ok || repo != "cartridge-gg/controller" {
		t.Fatalf("repo = %q, %v", repo, ok)
	}
	if _, ok := inferWorkIntakeRepo(repos, "Fix auth."); ok {
		t.Fatal("ambiguous request should not pick a repo")
	}
}

func TestBuildWorkIntakeNames(t *testing.T) {
	name := buildWorkIntakeThreadName("cartridge-gg/controller-rs", "Create a plan to upgrade controller-rs for Starknet privacy features with a very long tail that should truncate cleanly")
	if !strings.HasPrefix(name, "controller-rs / Create a plan to upgrade controller-rs") {
		t.Fatalf("thread name = %q", name)
	}
	if len([]rune(name)) > 100 {
		t.Fatalf("thread name too long: %d", len([]rune(name)))
	}

	route := config.WorkIntakeRoute{WorkspaceRoot: "/data/workspace-eng"}
	worktree := buildWorkIntakeWorktreePath(route, "cartridge-gg/controller-rs", "Create a plan to upgrade controller-rs for Starknet privacy features")
	if !strings.HasPrefix(worktree, "/data/workspace-eng/worktrees/controller-rs-create-a-plan-to-upgrade-controlle-") {
		t.Fatalf("worktree = %q", worktree)
	}
}
