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
			"attachments": map[string]any{
				"type":        "array",
				"description": "Optional batch of files to send in order. When set, each item must include path and may include caption.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Path to a file to send (relative to workspace, or absolute)",
						},
						"caption": map[string]any{
							"type":        "string",
							"description": "Optional caption for this attachment",
						},
					},
					"required": []string{"path"},
				},
			},
		},
	}
}

// Execute resolves and validates the path, checks for duplicate delivery, then
// returns a Result with Media populated for downstream pipeline delivery.
func (t *SendFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	requests, err := parseSendFileRequests(args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Per-request workspace (multi-tenant: each user has own workspace in context).
	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}

	allowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	media := make([]bus.MediaFile, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, req := range requests {
		resolved, err := resolvePathWithAllowed(req.Path, workspace, effectiveRestrict(ctx, t.restrict), allowed)
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
			return ErrorResult(fmt.Sprintf("file not found: %s", req.Path))
		}
		if !fi.Mode().IsRegular() {
			return ErrorResult(fmt.Sprintf("path is not a regular file: %s", req.Path))
		}

		if _, duplicate := seen[resolved]; duplicate {
			return ErrorResult(fmt.Sprintf("duplicate attachment path in batch: %s", filepath.Base(resolved)))
		}
		seen[resolved] = struct{}{}

		// Duplicate-delivery guard: block if already delivered in this turn.
		if dm := DeliveredMediaFromCtx(ctx); dm != nil && dm.IsDelivered(resolved) {
			return ErrorResult(fmt.Sprintf(
				"file already delivered in this turn: %s. Do not re-send the same file. "+
					"If user explicitly asked to resend, the next turn will reset delivery state.",
				filepath.Base(resolved)))
		}

		media = append(media, bus.MediaFile{
			Path:     resolved,
			Filename: filepath.Base(resolved),
			MimeType: mimeFromPath(resolved),
			Caption:  req.Caption,
		})
	}

	if dm := DeliveredMediaFromCtx(ctx); dm != nil {
		for _, mf := range media {
			dm.Mark(mf.Path)
		}
	}

	result := SilentResult(sendFileResultMessage(media))
	result.Media = media
	return result
}
