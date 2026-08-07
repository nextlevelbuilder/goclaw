package tools

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// DeliverFileTool surfaces an EXISTING workspace file to the user as a chat
// download link. write_file(deliver=true) only covers text the LLM writes
// inline; binary deliverables — .xlsx / .docx / .pdf / images / archives —
// are produced by exec'd code (python+openpyxl, pandas, libreoffice, …) and
// then have no delivery path, so the user never gets a link. This tool closes
// that gap by reusing the exact write_file delivery machinery: mirror the file
// into the media store (S3 on stage/prod) and attach it as Media, so the chat
// renders a signed /v1/files download URL that survives the workspace cleanup
// cron.
type DeliverFileTool struct {
	workspace       string
	restrict        bool
	allowedPrefixes []string
	deniedPrefixes  []string
	mediaUpload     MediaUploadFunc
	sandboxMgr      sandbox.Manager
}

// NewDeliverFileTool creates a DeliverFileTool bound to a workspace root.
func NewDeliverFileTool(workspace string, restrict bool) *DeliverFileTool {
	return &DeliverFileTool{workspace: workspace, restrict: restrict}
}

// SetMediaUploadFunc enables durable copy of delivered files to the media store
// (wired alongside WriteFileTool's hook). Without it, delivery falls back to the
// local workspace path (works immediately, pruned after the 7d cleanup cron).
func (t *DeliverFileTool) SetMediaUploadFunc(fn MediaUploadFunc) { t.mediaUpload = fn }

// SetSandboxManager enables per-slide JPEG previews for delivered .pptx files
// (rendered via LibreOffice + poppler, run inside the sandbox container — see
// renderPptxSlidePreviews). Without it, a delivered .pptx is still a fully
// working download; it just has no visual preview attached.
func (t *DeliverFileTool) SetSandboxManager(mgr sandbox.Manager) { t.sandboxMgr = mgr }

// AllowPaths / DenyPaths mirror WriteFileTool so deliver_file honours the same
// path boundaries as write/read.
func (t *DeliverFileTool) AllowPaths(prefixes ...string) {
	t.allowedPrefixes = append(t.allowedPrefixes, prefixes...)
}
func (t *DeliverFileTool) DenyPaths(prefixes ...string) {
	t.deniedPrefixes = append(t.deniedPrefixes, prefixes...)
}

func (t *DeliverFileTool) Name() string { return "deliver_file" }

func (t *DeliverFileTool) Description() string {
	return "Send an existing file from the workspace to the user as a downloadable attachment with a link. " +
		"Use this for files you generated with code/exec — spreadsheets (.xlsx, .csv), documents (.docx, .pdf), " +
		"images, archives (.zip), etc. — so the user can download them. The file must already exist on disk " +
		"(create it first via exec/write_file, then deliver it). Do not also send it via the message tool."
}

func (t *DeliverFileTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the existing file to deliver (relative to the workspace, or absolute).",
			},
		},
		"required": []string{"path"},
	}
}

func (t *DeliverFileTool) Execute(ctx context.Context, args map[string]any) *Result {
	path, _ := args["path"].(string)
	if path == "" {
		return ErrorResult("path is required")
	}

	workspace := ToolWorkspaceFromCtx(ctx)
	if workspace == "" {
		workspace = t.workspace
	}

	// Sandbox path rewrite: code run via `exec` writes inside the sandbox at the
	// container workdir (/workspace), which is bind-mounted to the HOST
	// workspace — so the file really is on the host, just under a different
	// absolute path. The model often hands deliver_file that container-absolute
	// path (/workspace/foo.xlsx); rewrite it to workspace-relative so host-side
	// resolution finds it instead of rejecting it as out-of-bounds.
	cw := sandbox.DefaultContainerWorkdir
	if workspace != "" && !strings.HasPrefix(workspace, cw) && (path == cw || strings.HasPrefix(path, cw+"/")) {
		if rel := strings.TrimPrefix(strings.TrimPrefix(path, cw), "/"); rel != "" {
			path = rel
		}
	}

	allowed := allowedWithTeamWorkspace(ctx, t.allowedPrefixes)
	resolved, err := resolvePathWithAllowed(path, workspace, effectiveRestrict(ctx, t.restrict), allowed)
	if err != nil {
		return ErrorResult(err.Error())
	}

	fi, statErr := os.Stat(resolved)
	if statErr != nil {
		// Fallback: exact path missing — search the workspace for a file with
		// the same name (covers minor path drift, e.g. a sandbox path that
		// didn't map cleanly). Only accept a unique match.
		if found := findInWorkspace(workspace, filepath.Base(path)); found != "" {
			resolved = found
			fi, statErr = os.Stat(resolved)
		}
		if statErr != nil {
			return ErrorResult(fmt.Sprintf("file not found: %s — create it inside the workspace first (use a relative path, the file lands under the workspace), then deliver_file it", path))
		}
	}
	if err := checkDeniedPath(resolved, t.workspace, t.deniedPrefixes); err != nil {
		return ErrorResult(err.Error())
	}
	if fi.IsDir() {
		return ErrorResult(fmt.Sprintf("%s is a directory, not a file", path))
	}
	// Never surface code/scripts to the user — they want the result, not the
	// code that produced it. (Hard guard; the prompt nudge alone wasn't enough.)
	if isScriptFile(resolved) {
		return SilentResult(fmt.Sprintf(
			"Not delivered: %s is a script/code file. Only deliver the final user-facing output (the .xlsx/.docx/.pdf/.csv/image), never the code that generated it.",
			filepath.Base(resolved),
		))
	}

	// Mirror to the durable media store when wired, so the attachment survives
	// the workspace cleanup cron; otherwise deliver the local path directly.
	deliveredPath := resolved
	if t.mediaUpload != nil {
		if cachePath := uploadDeliveredToMediaStore(ctx, t.mediaUpload, resolved); cachePath != "" {
			deliveredPath = cachePath
		}
	}

	result := SilentResult(fmt.Sprintf(
		"File delivered to the user: %s (%d bytes). A download link is attached to the chat — do NOT send it again via the message tool.",
		filepath.Base(resolved), fi.Size(),
	))
	result.Media = []bus.MediaFile{{Path: deliveredPath, Filename: filepath.Base(resolved)}}

	// A delivered .pptx also gets a per-slide JPEG preview, best-effort — a
	// deck is the one delivered format with no reliable client-side visual
	// render (unlike .xlsx/.docx/.pdf), so without this the chat can only
	// show a text outline. Never blocks or fails the delivery itself: any
	// error here (sandbox unconfigured, soffice/pdftoppm missing or
	// crashing, timeout) just means no preview, not a failed deliver_file.
	if t.sandboxMgr != nil && strings.EqualFold(filepath.Ext(resolved), ".pptx") {
		workspace := ToolWorkspaceFromCtx(ctx)
		if workspace == "" {
			workspace = t.workspace
		}
		previews, err := renderPptxSlidePreviews(ctx, t.sandboxMgr, workspace, resolved)
		if err != nil {
			slog.Warn("deliver_file: pptx slide preview render failed", "file", filepath.Base(resolved), "error", err)
		}
		base := strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved))
		for i, imgPath := range previews {
			imgDeliveredPath := imgPath
			if t.mediaUpload != nil {
				if cachePath := uploadDeliveredToMediaStore(ctx, t.mediaUpload, imgPath); cachePath != "" {
					imgDeliveredPath = cachePath
					// durably uploaded — the local render copy is no longer needed.
					// safeWorkspacePath is redundant with renderPptxSlidePreviews'
					// own bounds checks on every path it returns, but this is a
					// filesystem delete: it gets its own independent, visible guard,
					// and deletes the value IT returns rather than trusting imgPath
					// never to have escaped between there and here.
					if safePath, err := safeWorkspacePath(workspace, imgPath); err == nil {
						_ = os.Remove(safePath)
					}
				}
			}
			result.Media = append(result.Media, bus.MediaFile{
				Path:     imgDeliveredPath,
				MimeType: "image/jpeg",
				Filename: fmt.Sprintf("%s-slide-%d.jpg", base, i+1),
			})
		}
	}

	// Track delivered path so the message tool's self-send guard detects dupes.
	if dm := DeliveredMediaFromCtx(ctx); dm != nil {
		dm.Mark(deliveredPath)
	}
	return result
}

// scriptExts are code/script file types we never attach to the chat as a
// user-facing deliverable (the user wants the output, not the generator).
var scriptExts = map[string]bool{
	".py": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".sh": true, ".bash": true, ".zsh": true, ".rb": true, ".pl": true,
	".php": true, ".ps1": true, ".lua": true, ".ipynb": true,
}

func isScriptFile(name string) bool {
	return scriptExts[strings.ToLower(filepath.Ext(name))]
}

// findInWorkspace returns the first file named `name` anywhere under root, or ""
// if none. Bounded fallback for deliver_file when the exact path doesn't resolve
// (e.g. a sandbox-absolute path that didn't map). Skips hidden/internal dirs.
func findInWorkspace(root, name string) string {
	if root == "" || name == "" {
		return ""
	}
	var match string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// Don't descend into internal/dot dirs (.media, .goclaw, memory, etc.).
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == name {
			match = p
			return filepath.SkipAll
		}
		return nil
	})
	return match
}
