package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

// SendFileTool delivers an existing workspace file as a media attachment in the
// current chat session. It does NOT create or modify files — use write_file for that.
type SendFileTool struct {
	workspace       string
	restrict        bool
	allowedPrefixes []string
	deniedPrefixes  []string // path prefixes to deny access to (e.g. memory.db, config.json)
}

// NewSendFileTool creates a SendFileTool bound to the given workspace.
func NewSendFileTool(workspace string, restrict bool) *SendFileTool {
	return &SendFileTool{workspace: workspace, restrict: restrict}
}

// AllowPaths adds extra path prefixes that bypass restrict=true workspace boundary.
// Implements PathAllowable for consistent wiring with read_file, write_file, edit.
func (t *SendFileTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}

// DenyPaths adds path prefixes that send_file must reject (e.g. internal DB files).
// Implements PathDenyable for consistent wiring with read_file, write_file, edit, list_files.
func (t *SendFileTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func (t *SendFileTool) Name() string { return "send_file" }

func (t *SendFileTool) Description() string {
	return "Send an existing workspace file as an attachment in the current chat. " +
		"Use when the user asks to share or resend a file that already exists. " +
		"Does NOT create or modify the file — use write_file(deliver=true) to create and send a new file."
}

func (t *SendFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to send (relative to workspace, or absolute)",
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Optional text message accompanying the file",
			},
		},
		"required": []string{"path"},
	}
}

// Execute resolves and validates the path, checks for duplicate delivery, then
// returns a Result with Media populated for downstream pipeline delivery.
func (t *SendFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	path := argString(args, "path")
	if path == "" {
		return ErrorResult("path is required")
	}

	// Per-request workspace (multi-tenant: each user has own workspace in context).
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}

	// Resolve path with allowed-prefixes support (mirrors write_file pattern).
	allowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, t.restrict), allowed)
	if err != nil {
		return ErrorResult("cannot access path: " + err.Error())
	}

	// Deny-paths guard: reject access to internal files (memory.db, config.json, etc.).
	if err := checkDeniedPath(resolved, workspace, t.deniedPrefixes); err != nil {
		return ErrorResult(err.Error())
	}

	// Stat: file must exist and be a regular file (not a directory or device).
	fi, err := os.Stat(resolved)
	if err != nil {
		return ErrorResult(fmt.Sprintf("file not found: %s", path))
	}
	if !fi.Mode().IsRegular() {
		return ErrorResult(fmt.Sprintf("path is not a regular file: %s", path))
	}

	// Duplicate-delivery guard: block if already delivered in this turn.
	if dm := DeliveredMediaFromCtx(ctx); dm != nil && dm.IsDelivered(resolved) {
		return ErrorResult(fmt.Sprintf(
			"file already delivered in this turn: %s. Do not re-send the same file. "+
				"If user explicitly asked to resend, the next turn will reset delivery state.",
			filepath.Base(resolved)))
	}

	// Build result — caption overrides default message if provided.
	filename := filepath.Base(resolved)
	msg := fmt.Sprintf("Sent file: %s", filename)
	if caption := argString(args, "caption"); caption != "" {
		msg = caption
	}
	result := SilentResult(msg)
	result.Media = []bus.MediaFile{{
		Path:     resolved,
		Filename: filename,
		MimeType: mimeFromPath(resolved),
	}}

	// Mark delivered so subsequent send_file or message(MEDIA:) calls detect the duplicate.
	if dm := DeliveredMediaFromCtx(ctx); dm != nil {
		dm.Mark(resolved)
	}

	return result
}
