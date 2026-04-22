package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// outsidePath returns an absolute path that is guaranteed to be outside the
// given workspace and temp directories on any OS.  On Windows bare "/etc/..."
// is relative (no drive letter), so we prepend the volume name of the workspace
// to ensure filepath.IsAbs returns true.
func outsidePath(workspace, segments string) string {
	vol := filepath.VolumeName(workspace)
	return filepath.Join(vol+string(filepath.Separator), segments)
}

func TestResolveMediaPath(t *testing.T) {
	tmpDir := os.TempDir()

	// Create a temp workspace with a test file for workspace-relative tests.
	workspace := t.TempDir()
	docsDir := filepath.Join(workspace, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(docsDir, "report.pdf")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Normalize paths to canonical form (resolves macOS /var/folders → /private/var/folders symlink).
	// The resolvePath function uses filepath.EvalSymlinks, so test expectations must too.
	testFileCanonical, _ := filepath.EvalSymlinks(testFile)
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	t.Run("restricted", func(t *testing.T) {
		tool := NewMessageTool(workspaceCanonical, true)
		ctx := context.Background()

		tests := []struct {
			name   string
			input  string
			want   string
			wantOK bool
		}{
			// /tmp/ always allowed
			{"valid temp file", "MEDIA:" + filepath.Join(tmpDir, "test.png"), filepath.Join(tmpDir, "test.png"), true},
			{"valid nested temp", "MEDIA:" + filepath.Join(tmpDir, "sub", "file.txt"), filepath.Join(tmpDir, "sub", "file.txt"), true},

			// Workspace files allowed
			{"workspace absolute", "MEDIA:" + testFileCanonical, testFileCanonical, true},
			{"workspace relative", "MEDIA:docs/report.pdf", testFileCanonical, true},

			// Not a MEDIA: message
			{"no prefix", filepath.Join(tmpDir, "test.png"), "", false},
			{"empty after prefix", "MEDIA:", "", false},
			{"dot path", "MEDIA:.", "", false},
			{"empty string", "", "", false},
			{"just MEDIA", "MEDIA", "", false},

			// Outside workspace + outside /tmp/ → blocked
			{"outside workspace", "MEDIA:" + outsidePath(workspaceCanonical, "etc/passwd"), "", false},
			{"traversal attack", "MEDIA:" + filepath.Join(workspaceCanonical, "..", "etc", "passwd"), "", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := tool.resolveMediaPath(ctx, tt.input)
				if ok != tt.wantOK {
					t.Errorf("resolveMediaPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				}
				if ok && got != tt.want {
					t.Errorf("resolveMediaPath(%q) = %q, want %q", tt.input, got, tt.want)
				}
			})
		}
	})

	// effectiveRestrict() always returns true (multi-tenant security hardening),
	// so even tools created with restrict=false behave as restricted.
	t.Run("unrestricted_tool_still_restricted", func(t *testing.T) {
		tool := NewMessageTool(workspaceCanonical, false)
		ctx := context.Background()

		tests := []struct {
			name   string
			input  string
			wantOK bool
		}{
			// Outside workspace → blocked (effectiveRestrict overrides to true)
			{"absolute outside workspace", "MEDIA:" + outsidePath(workspaceCanonical, "etc/hostname"), false},
			// Workspace-relative → allowed
			{"workspace relative", "MEDIA:docs/report.pdf", true},
			// /tmp/ → allowed (temp dir exception in restricted mode)
			{"temp file", "MEDIA:" + filepath.Join(tmpDir, "test.png"), true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, ok := tool.resolveMediaPath(ctx, tt.input)
				if ok != tt.wantOK {
					t.Errorf("resolveMediaPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
				}
			})
		}
	})

	t.Run("context workspace override", func(t *testing.T) {
		// Tool has no workspace, but context provides one.
		tool := NewMessageTool("", true)
		ctx := WithToolWorkspace(context.Background(), workspaceCanonical)

		got, ok := tool.resolveMediaPath(ctx, "MEDIA:docs/report.pdf")
		if !ok {
			t.Fatal("expected ok=true for workspace-relative path with context workspace")
		}
		if got != testFileCanonical {
			t.Errorf("got %q, want %q", got, testFileCanonical)
		}
	})
}

func TestIsInTempDir(t *testing.T) {
	tmpDir := os.TempDir()
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"in tmp", filepath.Join(tmpDir, "test.png"), true},
		{"nested in tmp", filepath.Join(tmpDir, "sub", "file.txt"), true},
		{"tmp itself", tmpDir, false}, // only files inside, not the dir itself
		{"outside tmp", outsidePath(tmpDir, "etc/passwd"), false},
		{"relative path", "relative/path.txt", false},
		{"traversal", filepath.Join(tmpDir, "..", "etc", "passwd"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInTempDir(tt.path); got != tt.want {
				t.Errorf("isInTempDir(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestExtractEmbeddedMedia(t *testing.T) {
	tmpDir := os.TempDir()

	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	// Create test files in workspace.
	docsDir := filepath.Join(workspaceCanonical, "docs")
	os.MkdirAll(docsDir, 0o755)
	reportFile := filepath.Join(docsDir, "report.docx")
	os.WriteFile(reportFile, []byte("test"), 0o644)
	reportCanonical, _ := filepath.EvalSymlinks(reportFile)

	tool := NewMessageTool(workspaceCanonical, true)
	ctx := context.Background()

	t.Run("no MEDIA: in message", func(t *testing.T) {
		msg := "Hello, here is your report!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)
		if cleaned != msg {
			t.Errorf("expected unchanged message, got %q", cleaned)
		}
		if len(media) != 0 {
			t.Errorf("expected no media, got %d", len(media))
		}
	})

	t.Run("embedded MEDIA: in multi-line message", func(t *testing.T) {
		msg := "Here is the file:\nMEDIA:" + reportCanonical + "\nPlease download!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Here is the file:\nPlease download!" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
		if media[0].URL != reportCanonical {
			t.Errorf("media URL = %q, want %q", media[0].URL, reportCanonical)
		}
		wantMime := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		if media[0].ContentType != wantMime {
			t.Errorf("content type = %q, want %q", media[0].ContentType, wantMime)
		}
	})

	t.Run("MEDIA: mid-sentence keeps surrounding text", func(t *testing.T) {
		msg := "Here is your report MEDIA:" + reportCanonical + " please review"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Here is your report  please review" {
			t.Errorf("surrounding text lost: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("multiple MEDIA: on same line", func(t *testing.T) {
		img := filepath.Join(tmpDir, "photo.png")
		msg := "MEDIA:" + reportCanonical + " MEDIA:" + img
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "" {
			t.Errorf("expected empty cleaned text, got %q", cleaned)
		}
		if len(media) != 2 {
			t.Fatalf("expected 2 media from same line, got %d", len(media))
		}
	})

	t.Run("MEDIA: path outside workspace is stripped but no attachment", func(t *testing.T) {
		msg := "File:\nMEDIA:" + outsidePath(workspaceCanonical, "etc/passwd") + "\nDone"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "File:\nDone" {
			t.Errorf("MEDIA: line not stripped: %q", cleaned)
		}
		if len(media) != 0 {
			t.Errorf("expected no media for outside-workspace path, got %d", len(media))
		}
	})

	t.Run("message with only MEDIA: lines", func(t *testing.T) {
		msg := "MEDIA:" + reportCanonical
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "" {
			t.Errorf("expected empty cleaned text, got %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("audio_as_voice tag stripped", func(t *testing.T) {
		msg := "[[audio_as_voice]]\nMEDIA:" + filepath.Join(tmpDir, "voice.ogg") + "\nExtra text"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Extra text" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 1 {
			t.Fatalf("expected 1 media, got %d", len(media))
		}
	})

	t.Run("multiple MEDIA: paths", func(t *testing.T) {
		img := filepath.Join(tmpDir, "photo.png")
		msg := "Files:\nMEDIA:" + reportCanonical + "\nMEDIA:" + img + "\nEnjoy!"
		cleaned, media := tool.extractEmbeddedMedia(ctx, msg)

		if cleaned != "Files:\nEnjoy!" {
			t.Errorf("unexpected cleaned text: %q", cleaned)
		}
		if len(media) != 2 {
			t.Fatalf("expected 2 media, got %d", len(media))
		}
	})
}

func TestMimeFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/tmp/file.png", "image/png"},
		{"/tmp/file.jpg", "image/jpeg"},
		{"/tmp/file.mp4", "video/mp4"},
		{"/tmp/file.ogg", "audio/ogg"},
		{"/tmp/file.pdf", "application/pdf"},
		{"/tmp/file.doc", "application/msword"},
		{"/tmp/file.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{"/tmp/file.xls", "application/vnd.ms-excel"},
		{"/tmp/file.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{"/tmp/file.unknown", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			if got := mimeFromPath(tt.path); got != tt.want {
				t.Errorf("mimeFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestValidateChannelTenant(t *testing.T) {
	tenantA := uuid.MustParse("0193a5b0-7000-7000-8000-000000000001")
	tenantB := uuid.MustParse("019d1135-7087-7aa9-a2c7-cdaf7af851b1")

	tool := NewMessageTool("", true)

	t.Run("no checker configured allows all", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "telegram", "123"); err != nil {
			t.Errorf("expected nil, got error: %s", err.ForLLM)
		}
	})

	// Wire a mock checker.
	channels := map[string]uuid.UUID{
		"telegram":       tenantA,
		"tenant-b-tg":   tenantB,
	}
	tool.SetChannelTenantChecker(func(name string) (uuid.UUID, bool) {
		tid, ok := channels[name]
		return tid, ok
	})

	t.Run("same tenant allows", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "telegram", "123"); err != nil {
			t.Errorf("expected nil for same tenant, got: %s", err.ForLLM)
		}
	})

	t.Run("cross tenant blocks", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		err := tool.validateChannelTenant(ctx, "tenant-b-tg", "456")
		if err == nil {
			t.Fatal("expected error for cross-tenant send, got nil")
		}
		if !err.IsError {
			t.Error("expected IsError=true")
		}
	})

	t.Run("channel not found blocks", func(t *testing.T) {
		ctx := store.WithTenantID(context.Background(), tenantA)
		err := tool.validateChannelTenant(ctx, "nonexistent", "789")
		if err == nil {
			t.Fatal("expected error for missing channel, got nil")
		}
	})

	t.Run("nil channel tenant allows (legacy)", func(t *testing.T) {
		channels["legacy-ch"] = uuid.Nil
		ctx := store.WithTenantID(context.Background(), tenantA)
		if err := tool.validateChannelTenant(ctx, "legacy-ch", "123"); err != nil {
			t.Errorf("expected nil for legacy channel, got: %s", err.ForLLM)
		}
	})

	t.Run("nil context tenant allows (master/system)", func(t *testing.T) {
		ctx := context.Background() // no tenant in context
		if err := tool.validateChannelTenant(ctx, "tenant-b-tg", "456"); err != nil {
			t.Errorf("expected nil for master context, got: %s", err.ForLLM)
		}
	})
}

func TestSelfSendGuard(t *testing.T) {
	workspace := t.TempDir()
	workspaceCanonical, _ := filepath.EvalSymlinks(workspace)

	// Create a test file for MEDIA: resolution.
	testFile := filepath.Join(workspaceCanonical, "report.csv")
	os.WriteFile(testFile, []byte("data"), 0o644)

	tool := NewMessageTool(workspaceCanonical, true)
	// Wire message bus so MEDIA sends can proceed past self-send guard.
	tool.SetMessageBus(bus.New())

	// Build context with self-send channel/chatID.
	mkCtx := func() context.Context {
		ctx := context.Background()
		ctx = WithToolChannel(ctx, "telegram")
		ctx = WithToolChatID(ctx, "chat-42")
		return ctx
	}

	t.Run("text self-send blocked", func(t *testing.T) {
		result := tool.Execute(mkCtx(), map[string]any{
			"action":  "send",
			"channel": "telegram",
			"target":  "chat-42",
			"message": "Hello, this is a text message",
		})
		if !result.IsError {
			t.Fatal("expected text self-send to be blocked")
		}
	})

	t.Run("text to different chat allowed", func(t *testing.T) {
		// Cross-target guard also kicks in on unbound sessions — supply
		// forward=true so this test isolates the self-send guard.
		result := tool.Execute(mkCtx(), map[string]any{
			"action":         "send",
			"channel":        "telegram",
			"target":         "chat-99",
			"message":        "Hello, other chat",
			"forward":        true,
			"forward_reason": "test cross-chat",
		})
		if result.IsError {
			t.Fatalf("expected cross-chat send to succeed, got: %s", result.ForLLM)
		}
	})

	t.Run("MEDIA self-send allowed when not delivered", func(t *testing.T) {
		// No delivered media tracker — MEDIA self-send should be allowed.
		result := tool.Execute(mkCtx(), map[string]any{
			"action":  "send",
			"channel": "telegram",
			"target":  "chat-42",
			"message": "MEDIA:" + testFile,
		})
		if result.IsError {
			t.Fatalf("expected MEDIA self-send to be allowed, got: %s", result.ForLLM)
		}
	})

	t.Run("MEDIA self-send blocked when already delivered", func(t *testing.T) {
		ctx := mkCtx()
		dm := NewDeliveredMedia()
		dm.Mark(testFile)
		ctx = WithDeliveredMedia(ctx, dm)

		result := tool.Execute(ctx, map[string]any{
			"action":  "send",
			"channel": "telegram",
			"target":  "chat-42",
			"message": "MEDIA:" + testFile,
		})
		if !result.IsError {
			t.Fatal("expected MEDIA self-send to be blocked when file already delivered")
		}
	})

	t.Run("MEDIA self-send allowed for undelivered file with tracker", func(t *testing.T) {
		ctx := mkCtx()
		dm := NewDeliveredMedia()
		dm.Mark("/some/other/file.pdf") // different file marked
		ctx = WithDeliveredMedia(ctx, dm)

		result := tool.Execute(ctx, map[string]any{
			"action":  "send",
			"channel": "telegram",
			"target":  "chat-42",
			"message": "MEDIA:" + testFile,
		})
		if result.IsError {
			t.Fatalf("expected MEDIA self-send for undelivered file to be allowed, got: %s", result.ForLLM)
		}
	})

	t.Run("embedded MEDIA in text self-send blocked", func(t *testing.T) {
		ctx := mkCtx()
		dm := NewDeliveredMedia()
		dm.Mark(testFile)
		ctx = WithDeliveredMedia(ctx, dm)

		result := tool.Execute(ctx, map[string]any{
			"action":  "send",
			"channel": "telegram",
			"target":  "chat-42",
			"message": "Here is the file\nMEDIA:" + testFile,
		})
		// Contains MEDIA: pattern → passes text guard → but file is delivered → blocked
		if !result.IsError {
			t.Fatal("expected embedded MEDIA self-send to be blocked when file already delivered")
		}
	})
}

func TestMessageToolNumericTargetUsesSendPath(t *testing.T) {
	// JSON tool args use float64 for integers; target must not be ignored (was only .(string)).
	var gotChat string
	tool := NewMessageTool("", true)
	tool.SetChannelSender(func(_ context.Context, ch, chatID, content string) error {
		if ch != "telegram" {
			t.Errorf("channel = %q", ch)
		}
		gotChat = chatID
		return nil
	})
	ctx := context.Background()
	r := tool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  float64(-1001847298537),
		"message": "hello",
	})
	if r.IsError {
		t.Fatalf("unexpected error: %s", r.ForLLM)
	}
	if gotChat != "-1001847298537" {
		t.Errorf("sender saw chatID %q, want -1001847298537", gotChat)
	}
}

func TestArgString(t *testing.T) {
	tests := []struct {
		name string
		args map[string]any
		key  string
		want string
	}{
		{"string value", map[string]any{"k": "hello"}, "k", "hello"},
		{"string with spaces", map[string]any{"k": "  hi  "}, "k", "hi"},
		{"empty string", map[string]any{"k": ""}, "k", ""},
		{"missing key", map[string]any{}, "k", ""},
		{"nil value", map[string]any{"k": nil}, "k", ""},
		{"float64 integer", map[string]any{"k": float64(-1001847298537)}, "k", "-1001847298537"},
		{"float64 positive", map[string]any{"k": float64(42)}, "k", "42"},
		{"float64 zero", map[string]any{"k": float64(0)}, "k", "0"},
		{"float64 NaN", map[string]any{"k": math.NaN()}, "k", ""},
		{"int", map[string]any{"k": 123}, "k", "123"},
		{"int64", map[string]any{"k": int64(-999)}, "k", "-999"},
		{"json.Number", map[string]any{"k": json.Number("7654321")}, "k", "7654321"},
		{"bool fallback", map[string]any{"k": true}, "k", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argString(tt.args, tt.key)
			if got != tt.want {
				t.Errorf("argString(%v, %q) = %q, want %q", tt.args, tt.key, got, tt.want)
			}
		})
	}
}

func TestDeliveredMedia(t *testing.T) {
	dm := NewDeliveredMedia()

	if dm.IsDelivered("/tmp/test.csv") {
		t.Fatal("expected empty tracker to report false")
	}

	dm.Mark("/tmp/test.csv")
	if !dm.IsDelivered("/tmp/test.csv") {
		t.Fatal("expected marked path to be delivered")
	}

	if dm.IsDelivered("/tmp/other.csv") {
		t.Fatal("expected unmarked path to report false")
	}
}

func TestMessageToolCrossTargetGuard(t *testing.T) {
	type scenario struct {
		name         string
		sessionKey   string
		ctxChannel   string
		ctxChatID    string
		peerKind     string
		args         map[string]any
		wantErr      bool
		wantOutbound int // number of outbound messages published
		wantTargets  []string
	}

	const (
		agentDM    = "agent:a:telegram:direct:U1"
		agentGroup = "agent:a:telegram:group:-100G"
		agentCron  = "agent:a:cron:job-1"
		agentHB    = "agent:a:heartbeat"
		agentSub   = "agent:a:subagent:child-1"
		agentTeam  = "agent:a:team:T1:U1"
	)

	baseArgs := func(extra map[string]any) map[string]any {
		m := map[string]any{"action": "send", "message": "hello"}
		maps.Copy(m, extra)
		return m
	}

	scenarios := []scenario{
		{
			name:         "1_dm_same_target_pass",
			sessionKey:   agentDM,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "U1"}),
			wantOutbound: 1,
			wantTargets:  []string{"U1"},
		},
		{
			name:       "2_dm_cross_target_blocked",
			sessionKey: agentDM,
			ctxChannel: "telegram",
			ctxChatID:  "U1",
			peerKind:   "direct",
			args:       baseArgs(map[string]any{"channel": "telegram", "target": "-1003787954683"}),
			wantErr:    true,
		},
		{
			name:         "3_dm_cross_target_forward_pass",
			sessionKey:   agentDM,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100G", "forward": true, "forward_reason": "user asked to forward"}),
			wantOutbound: 2,
			wantTargets:  []string{"-100G", "U1"},
		},
		{
			name:         "4_group_same_target_pass",
			sessionKey:   agentGroup,
			ctxChannel:   "telegram",
			ctxChatID:    "-100G",
			peerKind:     "group",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100G"}),
			wantOutbound: 1,
			wantTargets:  []string{"-100G"},
		},
		{
			name:       "5_group_cross_target_blocked",
			sessionKey: agentGroup,
			ctxChannel: "telegram",
			ctxChatID:  "-100G",
			peerKind:   "group",
			args:       baseArgs(map[string]any{"channel": "telegram", "target": "-100G2"}),
			wantErr:    true,
		},
		{
			name:         "6_cron_free",
			sessionKey:   agentCron,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100X"}),
			wantOutbound: 1,
			wantTargets:  []string{"-100X"},
		},
		{
			name:         "7_heartbeat_free",
			sessionKey:   agentHB,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100X"}),
			wantOutbound: 1,
			wantTargets:  []string{"-100X"},
		},
		{
			name:         "7b_subagent_free",
			sessionKey:   agentSub,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100X"}),
			wantOutbound: 1,
			wantTargets:  []string{"-100X"},
		},
		{
			name:         "7c_team_free",
			sessionKey:   agentTeam,
			ctxChannel:   "telegram",
			ctxChatID:    "U1",
			peerKind:     "direct",
			args:         baseArgs(map[string]any{"channel": "telegram", "target": "-100X"}),
			wantOutbound: 1,
			wantTargets:  []string{"-100X"},
		},
	}

	// For same-target DM/group scenarios, text self-sends are blocked by the
	// pre-existing self-send guard (different from our new cross-target guard).
	// Use MEDIA: to bypass that — our guard doesn't care about message kind.
	sharedTmp := t.TempDir()
	mediaPath := filepath.Join(sharedTmp, "note.png")
	if err := os.WriteFile(mediaPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mediaMsg := "MEDIA:" + mediaPath
	// Apply MEDIA bypass to same-target scenarios.
	for i := range scenarios {
		if scenarios[i].name == "1_dm_same_target_pass" || scenarios[i].name == "4_group_same_target_pass" {
			scenarios[i].args["message"] = mediaMsg
		}
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			tool := NewMessageTool(sharedTmp, false)
			mb := bus.New()
			tool.SetMessageBus(mb)

			ctx := context.Background()
			ctx = WithToolSessionKey(ctx, sc.sessionKey)
			ctx = WithToolChannel(ctx, sc.ctxChannel)
			ctx = WithToolChatID(ctx, sc.ctxChatID)
			ctx = WithToolPeerKind(ctx, sc.peerKind)

			res := tool.Execute(ctx, sc.args)
			if sc.wantErr {
				if res == nil || !res.IsError {
					t.Fatalf("expected error result, got: %+v", res)
				}
				// Guard must not publish when blocked.
				got := drainBusNow(mb)
				if len(got) != 0 {
					t.Fatalf("expected 0 outbound on block, got %d: %+v", len(got), got)
				}
				return
			}
			if res != nil && res.IsError {
				t.Fatalf("unexpected error: %s", res.ForLLM)
			}
			got := drainBusNow(mb)
			if len(got) != sc.wantOutbound {
				t.Fatalf("outbound count: got %d want %d (%+v)", len(got), sc.wantOutbound, got)
			}
			for i, want := range sc.wantTargets {
				if got[i].ChatID != want {
					t.Errorf("outbound[%d].ChatID = %q, want %q", i, got[i].ChatID, want)
				}
			}
		})
	}
}

// drainBusNow reads all buffered outbound messages using a short timeout per
// read. Pre-cancelled ctx can't be used: select{msg,ctx.Done} picks randomly
// when both are ready, so buffered messages would be lost ~50% of the time.
func drainBusNow(mb *bus.MessageBus) []bus.OutboundMessage {
	var out []bus.OutboundMessage
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		msg, ok := mb.SubscribeOutbound(ctx)
		cancel()
		if !ok {
			return out
		}
		out = append(out, msg)
	}
}

// Test 10 — replay of production trace 019d9fcf-b433-7550-a9a6-62efa140128d:
// DM session, agent tried to send to a group topic ID. Guard must block.
func TestMessageToolCrossTargetGuard_TraceReplay(t *testing.T) {
	tool := NewMessageTool(t.TempDir(), false)
	mb := bus.New()
	tool.SetMessageBus(mb)

	ctx := context.Background()
	ctx = WithToolSessionKey(ctx, "agent:a:telegram:direct:U1")
	ctx = WithToolChannel(ctx, "telegram")
	ctx = WithToolChatID(ctx, "U1")
	ctx = WithToolPeerKind(ctx, "direct")

	res := tool.Execute(ctx, map[string]any{
		"action":  "send",
		"channel": "telegram",
		"target":  "-1003787954683:topic:1",
		"message": "here is the image",
	})
	if res == nil || !res.IsError {
		t.Fatalf("trace replay: expected ErrorResult, got: %+v", res)
	}
	if got := drainBusNow(mb); len(got) != 0 {
		t.Fatalf("trace replay: expected 0 outbound, got %d", len(got))
	}
}

// Sender-only deployment (no msgBus): notice falls back through t.sender so
// the origin chat still gets the audit breadcrumb. Verifies P1 fix.
func TestMessageToolCrossTargetGuard_NoticeFallbackSender(t *testing.T) {
	tool := NewMessageTool(t.TempDir(), false)
	type sent struct{ channel, target, message string }
	var calls []sent
	tool.SetChannelSender(func(_ context.Context, ch, tgt, msg string) error {
		calls = append(calls, sent{ch, tgt, msg})
		return nil
	})

	ctx := context.Background()
	ctx = WithToolSessionKey(ctx, "agent:a:telegram:direct:U1")
	ctx = WithToolChannel(ctx, "telegram")
	ctx = WithToolChatID(ctx, "U1")
	ctx = WithToolPeerKind(ctx, "direct")

	res := tool.Execute(ctx, map[string]any{
		"action": "send", "channel": "telegram", "target": "-100G",
		"forward": true, "forward_reason": "user asked forward",
		"message": "hello group",
	})
	if res == nil || res.IsError {
		t.Fatalf("expected success, got: %+v", res)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 sender calls (forward + notice), got %d: %+v", len(calls), calls)
	}
	if calls[0].target != "-100G" {
		t.Errorf("forward target: got %q want -100G", calls[0].target)
	}
	if calls[1].target != "U1" {
		t.Errorf("notice target: got %q want U1 (origin)", calls[1].target)
	}
	if !strings.Contains(calls[1].message, "user asked forward") {
		t.Errorf("notice missing reason: %q", calls[1].message)
	}
}

// Notice must NOT post when the forward itself fails. Verifies P2 fix.
func TestMessageToolCrossTargetGuard_NoNoticeOnSendFailure(t *testing.T) {
	tool := NewMessageTool(t.TempDir(), false)
	var calls int
	tool.SetChannelSender(func(_ context.Context, _, _, _ string) error {
		calls++
		return fmt.Errorf("boom")
	})

	ctx := context.Background()
	ctx = WithToolSessionKey(ctx, "agent:a:telegram:direct:U1")
	ctx = WithToolChannel(ctx, "telegram")
	ctx = WithToolChatID(ctx, "U1")
	ctx = WithToolPeerKind(ctx, "direct")

	res := tool.Execute(ctx, map[string]any{
		"action": "send", "channel": "telegram", "target": "-100G",
		"forward": true, "forward_reason": "r",
		"message": "hi",
	})
	if res == nil || !res.IsError {
		t.Fatalf("expected ErrorResult on send failure, got: %+v", res)
	}
	if calls != 1 {
		t.Errorf("expected only 1 sender call (forward, no notice), got %d", calls)
	}
}

func TestMessageTargetEnforced(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"", true},
		{"agent:a:telegram:direct:U1", true},
		{"agent:a:telegram:group:-100G", true},
		{"agent:a:ws:direct:conv-1", true},
		{"agent:a:main", true},
		{"agent:a:cron:job-1", false},
		{"agent:a:heartbeat", false},
		{"agent:a:heartbeat:12345", false},
		{"agent:a:subagent:child", false},
		{"agent:a:team:T1:U1", false},
	}
	for _, tc := range cases {
		if got := MessageTargetEnforced(tc.key); got != tc.want {
			t.Errorf("MessageTargetEnforced(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}
