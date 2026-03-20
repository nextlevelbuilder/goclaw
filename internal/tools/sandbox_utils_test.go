package tools

import (
	"context"
	"testing"
)

func TestSandboxCwd(t *testing.T) {
	tests := []struct {
		name            string
		ctxWorkspace    string // empty = no workspace in context
		globalWorkspace string
		containerBase   string
		want            string
		wantErr         bool
	}{
		{
			name:            "no workspace in context — fallback to container base",
			ctxWorkspace:    "",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			want:            "/workspace",
		},
		{
			name:            "workspace equals global mount",
			ctxWorkspace:    "/app/workspace",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			want:            "/workspace",
		},
		{
			name:            "per-agent workspace",
			ctxWorkspace:    "/app/workspace/agent-a-workspace",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			want:            "/workspace/agent-a-workspace",
		},
		{
			name:            "per-user workspace",
			ctxWorkspace:    "/app/workspace/agent-a/user-123",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			want:            "/workspace/agent-a/user-123",
		},
		{
			name:            "team workspace",
			ctxWorkspace:    "/app/workspace/teams/team-uuid/chat-123",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			want:            "/workspace/teams/team-uuid/chat-123",
		},
		{
			name:            "workspace outside global mount — error",
			ctxWorkspace:    "/other/path/agent-a",
			globalWorkspace: "/app/workspace",
			containerBase:   "/workspace",
			wantErr:         true,
		},
		{
			name:            "custom container base",
			ctxWorkspace:    "/app/workspace/agent-a",
			globalWorkspace: "/app/workspace",
			containerBase:   "/home/sandbox",
			want:            "/home/sandbox/agent-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.ctxWorkspace != "" {
				ctx = WithToolWorkspace(ctx, tt.ctxWorkspace)
			}
			got, err := SandboxCwd(ctx, tt.globalWorkspace, tt.containerBase)
			if tt.wantErr {
				if err == nil {
					t.Errorf("SandboxCwd() = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Errorf("SandboxCwd() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("SandboxCwd() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveSandboxPath(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		containerCwd string
		want         string
	}{
		{
			name:         "relative path joined with cwd",
			path:         "file.txt",
			containerCwd: "/workspace/agent-a",
			want:         "/workspace/agent-a/file.txt",
		},
		{
			name:         "relative subdirectory path",
			path:         "subdir/file.txt",
			containerCwd: "/workspace/agent-a",
			want:         "/workspace/agent-a/subdir/file.txt",
		},
		{
			name:         "absolute path passed through",
			path:         "/workspace/agent-a/file.txt",
			containerCwd: "/workspace/agent-b",
			want:         "/workspace/agent-a/file.txt",
		},
		{
			name:         "dot path",
			path:         ".",
			containerCwd: "/workspace/agent-a",
			want:         "/workspace/agent-a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveSandboxPath(tt.path, tt.containerCwd)
			if got != tt.want {
				t.Errorf("ResolveSandboxPath(%q, %q) = %q, want %q", tt.path, tt.containerCwd, got, tt.want)
			}
		})
	}
}
