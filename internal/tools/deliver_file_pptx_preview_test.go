package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/sandbox"
)

// previewFakeSandbox lets each test script exactly what each Exec call does,
// including materializing host-side files the real soffice/pdftoppm run
// would have produced — renderPptxSlidePreviews discovers its output by
// reading the host directory afterward, so the fake has to actually write
// there rather than just return a canned exit code.
type previewFakeSandbox struct {
	onExec func(cmd []string) (*sandbox.ExecResult, error)
	// onExecWithOpts, when set, is used instead of onExec and additionally
	// receives the resolved ExecOption values (e.g. WithEnv) — most tests
	// don't care about these, but the HOME-env regression test does.
	onExecWithOpts func(cmd []string, opts sandbox.ExecOpts) (*sandbox.ExecResult, error)
}

func (f *previewFakeSandbox) Exec(_ context.Context, cmd []string, _ string, opts ...sandbox.ExecOption) (*sandbox.ExecResult, error) {
	if f.onExecWithOpts != nil {
		return f.onExecWithOpts(cmd, sandbox.ApplyExecOpts(opts))
	}
	return f.onExec(cmd)
}
func (f *previewFakeSandbox) Destroy(context.Context) error { return nil }
func (f *previewFakeSandbox) ID() string                    { return "fake-preview-container" }
func (f *previewFakeSandbox) ExecInteractive(context.Context, []string, string, ...sandbox.ExecOption) (sandbox.InteractiveSession, error) {
	return nil, errors.New("previewFakeSandbox: ExecInteractive not supported")
}

// realTempDir returns t.TempDir() resolved through any symlinks — matching
// what resolvePath/resolvePathWithAllowed always hands renderPptxSlidePreviews
// in production. Without this, a raw t.TempDir() on macOS (/var -> /private/var)
// would make a file that is genuinely inside the workspace look like it's
// outside it, purely from comparing a resolved path against an unresolved root.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", dir, err)
	}
	return real
}

type previewFakeManager struct{ sb sandbox.Sandbox }

func (m *previewFakeManager) Get(context.Context, string, string, *sandbox.Config) (sandbox.Sandbox, error) {
	return m.sb, nil
}
func (m *previewFakeManager) BaseConfig() sandbox.Config            { return sandbox.Config{} } // Workdir="" -> DefaultContainerWorkdir
func (m *previewFakeManager) Release(context.Context, string) error { return nil }
func (m *previewFakeManager) ReleaseAll(context.Context) error      { return nil }
func (m *previewFakeManager) Stop()                                 {}
func (m *previewFakeManager) Stats() map[string]any                 { return nil }

// TestRenderPptxSlidePreviews_RejectsOutsideWorkspace verifies a pptx path
// outside the workspace is rejected before any sandbox call is attempted —
// deliver_file already resolves paths INSIDE the workspace, but this
// function takes a plain string and must not trust it blindly.
func TestRenderPptxSlidePreviews_RejectsOutsideWorkspace(t *testing.T) {
	ws := realTempDir(t)
	calls := 0
	mgr := &previewFakeManager{sb: &previewFakeSandbox{onExec: func(cmd []string) (*sandbox.ExecResult, error) {
		calls++
		return &sandbox.ExecResult{ExitCode: 0}, nil
	}}}
	_, err := renderPptxSlidePreviews(context.Background(), mgr, ws, "/completely/different/deck.pptx")
	if err == nil {
		t.Fatal("expected an error for a path outside the workspace")
	}
	if calls != 0 {
		t.Errorf("expected zero sandbox exec calls, got %d — should reject before touching the sandbox", calls)
	}
}

// TestRenderPptxSlidePreviews_SoffceFailurePropagates verifies a non-zero
// soffice exit code is reported as an error (so deliver_file logs it and
// falls back to no preview) rather than silently returning an empty list
// that looks identical to "deck had zero slides."
func TestRenderPptxSlidePreviews_SoffceFailurePropagates(t *testing.T) {
	ws := realTempDir(t)
	pptx := filepath.Join(ws, "deck.pptx")
	if err := os.WriteFile(pptx, []byte("PK fake pptx"), 0644); err != nil {
		t.Fatal(err)
	}
	mgr := &previewFakeManager{sb: &previewFakeSandbox{onExec: func(cmd []string) (*sandbox.ExecResult, error) {
		if len(cmd) > 0 && cmd[0] == "soffice" {
			return &sandbox.ExecResult{ExitCode: 1, Stderr: "Fatal Error: no export filter"}, nil
		}
		return &sandbox.ExecResult{ExitCode: 0}, nil
	}}}
	_, err := renderPptxSlidePreviews(context.Background(), mgr, ws, pptx)
	if err == nil {
		t.Fatal("expected soffice's non-zero exit to surface as an error")
	}
	if !strings.Contains(err.Error(), "no export filter") {
		t.Errorf("error should carry soffice's stderr for diagnosis, got: %v", err)
	}
}

// TestRenderPptxSlidePreviews_DiscoversAndSortsSlides is the happy path: the
// fake sandbox materializes host-side JPEGs (as the real soffice/pdftoppm
// pipeline would, verified separately against real Docker containers — see
// the PR description) out of numeric order and with a stray non-slide file
// alongside them, and the function must return exactly the slide images, in
// slide order.
func TestRenderPptxSlidePreviews_DiscoversAndSortsSlides(t *testing.T) {
	ws := realTempDir(t)
	pptx := filepath.Join(ws, "deck.pptx")
	if err := os.WriteFile(pptx, []byte("PK fake pptx"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &previewFakeManager{sb: &previewFakeSandbox{onExec: func(cmd []string) (*sandbox.ExecResult, error) {
		if len(cmd) == 0 {
			return &sandbox.ExecResult{ExitCode: 1}, nil
		}
		switch cmd[0] {
		case "mkdir":
			// mkdir -p <containerOutDir> — containerOutDir is the last arg.
			containerOutDir := cmd[len(cmd)-1]
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(containerOutDir, "/workspace/"))
			if err := os.MkdirAll(hostOutDir, 0755); err != nil {
				return nil, err
			}
			return &sandbox.ExecResult{ExitCode: 0}, nil
		case "cp":
			// cp containerPptxPath containerOutDir/deck.pptx — nothing to
			// simulate: the fake soffice handler below writes deck.pdf
			// unconditionally, regardless of what cp actually copied.
			return &sandbox.ExecResult{ExitCode: 0}, nil
		case "soffice":
			// --outdir <dir> ... <input.pptx> — write the fake PDF the real
			// call would have produced, so pdftoppm has something to "read."
			var outDir string
			for i, a := range cmd {
				if a == "--outdir" && i+1 < len(cmd) {
					outDir = cmd[i+1]
				}
			}
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(outDir, "/workspace/"))
			return &sandbox.ExecResult{ExitCode: 0}, os.WriteFile(filepath.Join(hostOutDir, "deck.pdf"), []byte("%PDF fake"), 0644)
		case "pdftoppm":
			// Simulates a 3-slide deck. Deliberately written out of numeric
			// order (10 before 2) to exercise real numeric sort, not string
			// sort (which would put "slide-10" before "slide-2").
			outPrefix := cmd[len(cmd)-1] // .../slide
			hostOutDir := filepath.Dir(filepath.Join(ws, strings.TrimPrefix(outPrefix, "/workspace/")))
			for _, n := range []string{"1", "10", "2"} {
				if err := os.WriteFile(filepath.Join(hostOutDir, "slide-"+n+".jpg"), []byte("jpeg-"+n), 0644); err != nil {
					return nil, err
				}
			}
			// A stray file that must NOT be mistaken for a slide.
			if err := os.WriteFile(filepath.Join(hostOutDir, "slide-thumbnail-cache.jpg"), []byte("ignore me"), 0644); err != nil {
				return nil, err
			}
			return &sandbox.ExecResult{ExitCode: 0}, nil
		default:
			return &sandbox.ExecResult{ExitCode: 1, Stderr: "unexpected command: " + cmd[0]}, nil
		}
	}}}

	got, err := renderPptxSlidePreviews(context.Background(), mgr, ws, pptx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.SlidePaths) != 3 {
		t.Fatalf("expected 3 slide images, got %d: %v", len(got.SlidePaths), got.SlidePaths)
	}
	for i, want := range []string{"slide-1.jpg", "slide-2.jpg", "slide-10.jpg"} {
		if filepath.Base(got.SlidePaths[i]) != want {
			t.Errorf("slide %d = %s, want %s (numeric order, not lexical)", i, filepath.Base(got.SlidePaths[i]), want)
		}
	}
	for _, p := range got.SlidePaths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("returned path does not exist on disk: %s", p)
		}
	}
	// The intermediate PDF is kept (not deleted) precisely so deliver_file can
	// attach it as a sibling for the real client-side PDF-viewer engine.
	if got.PDFPath == "" {
		t.Fatal("expected PDFPath to be populated — the intermediate deck.pdf should be kept, not deleted")
	}
	if filepath.Base(got.PDFPath) != "deck.pdf" {
		t.Errorf("PDFPath = %s, want a deck.pdf", got.PDFPath)
	}
	if _, err := os.Stat(got.PDFPath); err != nil {
		t.Errorf("PDFPath does not exist on disk: %s", got.PDFPath)
	}
}

// TestDeliverFile_PptxPreview_NilSandboxIsANoOp is the regression guard for
// the overwhelmingly common case: no sandbox configured at all. Every
// non-.pptx delivery, and every deployment that never calls
// SetSandboxManager, must behave EXACTLY as before this feature existed.
func TestDeliverFile_PptxPreview_NilSandboxIsANoOp(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "deck.pptx"), []byte("PK fake pptx"), 0644); err != nil {
		t.Fatal(err)
	}
	tool := NewDeliverFileTool(ws, true) // sandboxMgr never set
	res := tool.Execute(context.Background(), map[string]any{"path": "deck.pptx"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if len(res.Media) != 1 {
		t.Fatalf("Media len = %d, want 1 (just the .pptx itself, no preview attempted)", len(res.Media))
	}
}

// TestDeliverFile_PptxPreview_AttachesSlideImages verifies the wiring end of
// the feature: DeliverFileTool.Execute appends the rendered slide images —
// and the intermediate PDF, for the real client-side viewer engine — as
// additional Media entries alongside the original .pptx, each with the
// expected filename and MIME type.
func TestDeliverFile_PptxPreview_AttachesSlideImages(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "deck.pptx"), []byte("PK fake pptx"), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := &previewFakeManager{sb: &previewFakeSandbox{onExec: func(cmd []string) (*sandbox.ExecResult, error) {
		switch {
		case len(cmd) > 0 && cmd[0] == "mkdir":
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(cmd[len(cmd)-1], "/workspace/"))
			return &sandbox.ExecResult{ExitCode: 0}, os.MkdirAll(hostOutDir, 0755)
		case len(cmd) > 0 && cmd[0] == "cp":
			return &sandbox.ExecResult{ExitCode: 0}, nil
		case len(cmd) > 0 && cmd[0] == "soffice":
			var outDir string
			for i, a := range cmd {
				if a == "--outdir" && i+1 < len(cmd) {
					outDir = cmd[i+1]
				}
			}
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(outDir, "/workspace/"))
			return &sandbox.ExecResult{ExitCode: 0}, os.WriteFile(filepath.Join(hostOutDir, "deck.pdf"), []byte("%PDF"), 0644)
		case len(cmd) > 0 && cmd[0] == "pdftoppm":
			hostOutDir := filepath.Dir(filepath.Join(ws, strings.TrimPrefix(cmd[len(cmd)-1], "/workspace/")))
			return &sandbox.ExecResult{ExitCode: 0}, os.WriteFile(filepath.Join(hostOutDir, "slide-1.jpg"), []byte("jpeg"), 0644)
		default:
			return &sandbox.ExecResult{ExitCode: 1}, nil
		}
	}}}

	tool := NewDeliverFileTool(ws, true)
	tool.SetSandboxManager(mgr)
	res := tool.Execute(context.Background(), map[string]any{"path": "deck.pptx"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.ForLLM)
	}
	if len(res.Media) != 3 {
		t.Fatalf("Media len = %d, want 3 (the .pptx + one slide preview + the pdf): %+v", len(res.Media), res.Media)
	}
	if res.Media[0].Filename != "deck.pptx" {
		t.Errorf("Media[0] = %q, want the original deck.pptx first", res.Media[0].Filename)
	}
	if res.Media[1].Filename != "deck-slide-1.jpg" || res.Media[1].MimeType != "image/jpeg" {
		t.Errorf("Media[1] = %+v, want {Filename: deck-slide-1.jpg, MimeType: image/jpeg}", res.Media[1])
	}
	if res.Media[2].Filename != "deck.pdf" || res.Media[2].MimeType != "application/pdf" {
		t.Errorf("Media[2] = %+v, want {Filename: deck.pdf, MimeType: application/pdf}", res.Media[2])
	}
}

// TestRenderPptxSlidePreviews_SofficeGetsWritableHome is a regression guard
// for a real bug caught only by checking actual staging logs, not local
// testing: the sandbox container's root filesystem is read-only, and
// LibreOffice unconditionally tries to bootstrap a user profile under $HOME
// on every launch — /home/sandbox is part of that read-only root, so it
// failed outright ("User installation could not be completed... Read-only
// file system") every single time in the real deployed environment, despite
// working in every local Docker test that didn't reproduce the hardened
// --read-only + tmpfs config. The fix is a HOME override pointed at the
// (writable, bind-mounted) render directory; this test exists so removing
// that override — e.g. someone "cleaning up" the soffice Exec call — fails
// loudly instead of only failing on the next real deploy.
func TestRenderPptxSlidePreviews_SofficeGetsWritableHome(t *testing.T) {
	ws := realTempDir(t)
	pptx := filepath.Join(ws, "deck.pptx")
	if err := os.WriteFile(pptx, []byte("PK fake pptx"), 0644); err != nil {
		t.Fatal(err)
	}

	var sofficeHome string
	sofficeCalled := false
	mgr := &previewFakeManager{sb: &previewFakeSandbox{onExecWithOpts: func(cmd []string, opts sandbox.ExecOpts) (*sandbox.ExecResult, error) {
		if len(cmd) == 0 {
			return &sandbox.ExecResult{ExitCode: 1}, nil
		}
		switch cmd[0] {
		case "mkdir":
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(cmd[len(cmd)-1], "/workspace/"))
			return &sandbox.ExecResult{ExitCode: 0}, os.MkdirAll(hostOutDir, 0755)
		case "soffice":
			sofficeCalled = true
			if opts.Env != nil {
				sofficeHome = opts.Env["HOME"]
			}
			var outDir string
			for i, a := range cmd {
				if a == "--outdir" && i+1 < len(cmd) {
					outDir = cmd[i+1]
				}
			}
			hostOutDir := filepath.Join(ws, strings.TrimPrefix(outDir, "/workspace/"))
			return &sandbox.ExecResult{ExitCode: 0}, os.WriteFile(filepath.Join(hostOutDir, "deck.pdf"), []byte("%PDF"), 0644)
		case "pdftoppm":
			hostOutDir := filepath.Dir(filepath.Join(ws, strings.TrimPrefix(cmd[len(cmd)-1], "/workspace/")))
			return &sandbox.ExecResult{ExitCode: 0}, os.WriteFile(filepath.Join(hostOutDir, "slide-1.jpg"), []byte("jpeg"), 0644)
		default:
			return &sandbox.ExecResult{ExitCode: 0}, nil
		}
	}}}

	if _, err := renderPptxSlidePreviews(context.Background(), mgr, ws, pptx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sofficeCalled {
		t.Fatal("soffice was never invoked")
	}
	if sofficeHome == "" {
		t.Fatal("soffice ran with no HOME override — this is exactly the config that fails on the real read-only sandbox root")
	}
	if sofficeHome == "/home/sandbox" || sofficeHome == "~" {
		t.Fatalf("HOME = %q points at the read-only root, not a writable location", sofficeHome)
	}
}
