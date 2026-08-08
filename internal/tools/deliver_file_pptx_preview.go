package tools

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// renderPptxSlidePreviews converts a delivered .pptx into one JPEG per slide via
// LibreOffice + poppler (soffice --headless --convert-to pdf, then pdftoppm),
// run INSIDE the sandbox container rather than the gateway's own process.
//
// That split is not incidental: LibreOffice crashes on startup under Alpine's
// musl libc (a known, documented incompatibility — verified directly, not
// assumed), which is what the gateway's own runtime image is built on. The
// sandbox image (Dockerfile.sandbox) is Debian, where the exact same
// soffice/pdftoppm invocation was verified to work and produce a real,
// correctly laid out render. So the gateway never runs soffice itself — it
// asks the sandbox to, the same way it already does for exec.
//
// Uses a scope key distinct from the session's own coding/exec container: a
// slow or wedged preview render must not block or share state with whatever
// the agent's own sandboxed tool calls are doing at the same time.
func renderPptxSlidePreviews(ctx context.Context, mgr sandbox.Manager, workspace, hostPptxPath string) ([]string, error) {
	// hostPptxPath was already resolved through any symlinks by
	// resolvePath/resolvePathWithAllowed (deliver_file.go's Execute always
	// passes its post-resolution `resolved`, never the raw model-supplied
	// path). workspace, by contrast, reaches here as whatever the caller had
	// on hand — which on macOS dev machines is routinely NOT symlink-resolved
	// (t.TempDir() and /tmp itself resolve through /var -> /private/var).
	// Comparing an unresolved root against an already-resolved child makes a
	// file that is genuinely inside the workspace look like it's outside it.
	if absWs, err := filepath.Abs(workspace); err == nil {
		if real, err := filepath.EvalSymlinks(absWs); err == nil {
			workspace = real
		}
	}
	hostPptxPath, err := safeWorkspacePath(workspace, hostPptxPath)
	if err != nil {
		return nil, fmt.Errorf("file is outside the sandbox-mounted workspace: %w", err)
	}
	// rel is used only to build a CONTAINER-side command argument below
	// (sb.Exec, not a host filesystem call) — the containment check itself
	// is safeWorkspacePath's HasPrefix check above, not this.
	rel, err := filepath.Rel(workspace, hostPptxPath)
	if err != nil {
		return nil, fmt.Errorf("compute container-relative path: %w", err)
	}

	// Bounds the whole render (container get + 4 execs), not just one call —
	// a hung soffice must not stall the agent's turn for minutes.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	cfg := mgr.BaseConfig()
	containerWorkdir := cfg.ContainerWorkdir()
	containerPptxPath := path.Join(containerWorkdir, filepath.ToSlash(rel))

	sessionKey := ToolSessionKeyFromCtx(ctx)
	scopeKey := "pptx-preview:shared"
	if sessionKey != "" {
		scopeKey = "pptx-preview:" + sessionKey
	}

	sb, err := mgr.Get(ctx, scopeKey, workspace, nil)
	if err != nil {
		return nil, fmt.Errorf("get sandbox: %w", err)
	}

	// Everything from here on is built from CONSTANTS plus a server-generated
	// UUID — never from the delivered file's own name. soffice names its PDF
	// output after its input, so converting the file in place under its real
	// name would carry a fragment of the original (model-supplied) filename
	// into every path constructed below. Copying it to a fixed "deck.pptx"
	// first avoids that entirely, rather than relying on filepath.Base()
	// (which is safe here, but a static analyzer has no way to know that
	// without re-deriving the same argument every time this file changes).
	relDir := ".slide-preview/" + uuid.New().String()
	containerOutDir := path.Join(containerWorkdir, relDir)
	hostOutDir, err := safeWorkspacePath(workspace, filepath.Join(workspace, filepath.FromSlash(relDir)))
	if err != nil {
		return nil, err
	}

	containerInputPptx := path.Join(containerOutDir, "deck.pptx")
	containerPdfPath := path.Join(containerOutDir, "deck.pdf")

	if res, err := sb.Exec(ctx, []string{"mkdir", "-p", containerOutDir}, containerWorkdir); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	} else if res.ExitCode != 0 {
		return nil, fmt.Errorf("mkdir: exit %d: %s", res.ExitCode, res.Stderr)
	}

	if res, err := sb.Exec(ctx, []string{"cp", containerPptxPath, containerInputPptx}, containerWorkdir); err != nil {
		return nil, fmt.Errorf("cp: %w", err)
	} else if res.ExitCode != 0 {
		return nil, fmt.Errorf("cp: exit %d: %s", res.ExitCode, res.Stderr)
	}

	// The sandbox container's root filesystem is read-only (Dockerfile.sandbox
	// runs as an unprivileged user under a hardened config — see
	// sandbox.DefaultConfig: ReadOnlyRoot, tmpfs only at /tmp, /var/tmp, /run).
	// LibreOffice bootstraps a user profile under $HOME (~/.config/libreoffice,
	// ~/.cache/dconf) on first launch, every time — /home/sandbox is part of
	// that read-only root, so it fails outright: "User installation could not
	// be completed... Read-only file system" (verified directly against the
	// real staging container, not assumed). containerOutDir is writable (it's
	// under the bind-mounted /workspace) and already unique per call, so
	// pointing HOME there gives soffice a real place to bootstrap into without
	// needing a second UUID or worrying about concurrent renders colliding.
	if res, err := sb.Exec(ctx,
		[]string{"soffice", "--headless", "--convert-to", "pdf", "--outdir", containerOutDir, containerInputPptx},
		containerWorkdir,
		sandbox.WithEnv(map[string]string{"HOME": containerOutDir}),
	); err != nil {
		return nil, fmt.Errorf("soffice: %w", err)
	} else if res.ExitCode != 0 {
		return nil, fmt.Errorf("soffice: exit %d: %s", res.ExitCode, res.Stderr)
	}

	if res, err := sb.Exec(ctx,
		[]string{"pdftoppm", "-jpeg", "-r", "100", containerPdfPath, path.Join(containerOutDir, "slide")},
		containerWorkdir,
	); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w", err)
	} else if res.ExitCode != 0 {
		return nil, fmt.Errorf("pdftoppm: exit %d: %s", res.ExitCode, res.Stderr)
	}

	// Re-derive the safe directory path fresh, right before the read, rather
	// than reusing the one validated ~20 lines up — the two most dangerous
	// filesystem calls in this file (a directory read whose results all get
	// handed back to the caller, and a delete) each get their own point-of-use
	// check, not a single check whose result is trusted at a distance.
	safeOutDir, err := safeWorkspacePath(workspace, hostOutDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(safeOutDir)
	if err != nil {
		return nil, fmt.Errorf("read rendered output (host/container workspace mount mismatch?): %w", err)
	}

	type numberedSlide struct {
		n    int
		path string
	}
	var slides []numberedSlide
	for _, e := range entries {
		name := e.Name()
		numStr := strings.TrimSuffix(strings.TrimPrefix(name, "slide-"), ".jpg")
		if numStr == name || !strings.HasSuffix(name, ".jpg") {
			continue // not one of ours (e.g. the intermediate .pdf)
		}
		n, err := strconv.Atoi(numStr)
		if err != nil {
			continue
		}
		// name came from a directory listing of a directory this function
		// created — it should always be inside safeOutDir by construction —
		// but a filesystem listing is never trusted blindly for a path that
		// is about to be handed back to the caller as a real file to attach.
		slidePath, err := safeWorkspacePath(workspace, filepath.Join(safeOutDir, name))
		if err != nil {
			continue
		}
		slides = append(slides, numberedSlide{n, slidePath})
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })

	// The intermediate PDF served no purpose past this point — only the
	// per-slide JPEGs get delivered.
	if pdfPath, err := safeWorkspacePath(workspace, filepath.Join(safeOutDir, "deck.pdf")); err == nil {
		_ = os.Remove(pdfPath)
	}

	if len(slides) == 0 {
		return nil, fmt.Errorf("soffice/pdftoppm reported success but produced no slide images")
	}
	out := make([]string, len(slides))
	for i, s := range slides {
		out[i] = s.path
	}
	return out, nil
}

// safeWorkspacePath returns candidate's absolute form, but only after
// confirming it resolves to somewhere strictly inside workspace — refusing
// otherwise. It is used at every filesystem read/delete this file performs,
// immediately before the operation, on the value it RETURNS rather than the
// caller's original variable: every path here should already be inside the
// workspace by construction (workspace plus a server-generated UUID, never
// anything derived from the original delivered filename), but that stays a
// verified fact rather than an assumption this way, and the sanitized value
// — not a separately-checked boolean — is what the filesystem call actually
// receives.
func safeWorkspacePath(workspace, candidate string) (string, error) {
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	// filepath.Abs cleans its result (removes ".", ".." and duplicate
	// separators), so a trailing-separator prefix check against it is a
	// real containment check, not just a string-prefix coincidence. Uses
	// strings.HasPrefix rather than filepath.Rel deliberately: CodeQL 2.26.2
	// stopped recognizing filepath.Rel as a path-injection sanitizer
	// (correctly — Rel's result can begin with ".." for a path outside the
	// base, but nothing forces a caller to check for that), while a
	// HasPrefix check against a cleaned absolute path is still recognized.
	prefix := absWorkspace + string(filepath.Separator)
	if !strings.HasPrefix(absCandidate, prefix) {
		return "", fmt.Errorf("path escapes workspace: %s", candidate)
	}
	return absCandidate, nil
}
